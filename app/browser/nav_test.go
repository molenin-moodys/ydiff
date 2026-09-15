package browser

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runAndApply executes cmd (as returned by NewNav/Enter/Up) and applies every
// resulting message to nav. tea.Batch commands return a BatchMsg carrying the
// individual commands to run; this helper unwraps that one level, which is
// all that Nav's commands ever produce.
func runAndApply(t *testing.T, nav *Nav, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	for _, m := range flattenBatch(msg) {
		if loaded, ok := m.(LoadedMsg); ok {
			nav.Apply(loaded)
		}
	}
}

func flattenBatch(msg tea.Msg) []tea.Msg {
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for _, cmd := range batch {
		if cmd == nil {
			continue
		}
		out = append(out, flattenBatch(cmd())...)
	}
	return out
}

// indexOf returns the index of name within a column's listing, failing the
// test if it is not present.
func indexOf(t *testing.T, col *Column, name string) int {
	t.Helper()
	for i, e := range col.Listing.Entries {
		if e.Name == name {
			return i
		}
	}
	t.Fatalf("entry %q not found in listing of %q", name, col.Listing.Dir)
	return -1
}

func TestNav_EnterMovesPathDown(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "app"))
	mustMkdir(t, filepath.Join(root, "docs"))

	nav, cmd := NewNav(root, false, nil)
	runAndApply(t, nav, cmd)
	require.Equal(t, root, nav.Path())

	nav.SetCursor(indexOf(t, nav.Current(), "app"))
	cmd = nav.Enter()
	require.NotNil(t, cmd)
	runAndApply(t, nav, cmd)

	assert.Equal(t, filepath.Join(root, "app"), nav.Path())
}

func TestNav_UpMovesPathBack(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "app"))

	nav, cmd := NewNav(root, false, nil)
	runAndApply(t, nav, cmd)

	nav.SetCursor(indexOf(t, nav.Current(), "app"))
	runAndApply(t, nav, nav.Enter())
	require.Equal(t, filepath.Join(root, "app"), nav.Path())

	cmd = nav.Up()
	require.NotNil(t, cmd)
	runAndApply(t, nav, cmd)

	assert.Equal(t, root, nav.Path())
}

// TestNav_UpFromFilesystemRootIsNoOp is required by the design: going up from
// "/" must not error, must not panic, and must not empty out the path.
func TestNav_UpFromFilesystemRootIsNoOp(t *testing.T) {
	fsRoot := string(filepath.Separator)

	nav, cmd := NewNav(fsRoot, false, nil)
	runAndApply(t, nav, cmd)
	require.Equal(t, fsRoot, nav.Path())

	var upCmd tea.Cmd
	assert.NotPanics(t, func() {
		upCmd = nav.Up()
	})

	assert.Nil(t, upCmd, "Up at the filesystem root must not issue a request")
	assert.Equal(t, fsRoot, nav.Path(), "path must remain the root, never empty")
}

// TestNav_CursorMemory_UpRestoresPositionOnParent is the test that makes the
// browser feel right: entering "app/" then going back up must land the
// cursor ON "app/" in the parent listing, not reset to the top of the list.
func TestNav_CursorMemory_UpRestoresPositionOnParent(t *testing.T) {
	root := t.TempDir()
	// "app" is not first alphabetically after these siblings, so landing on
	// it after Up is meaningful evidence of memory, not a coincidental 0.
	mustMkdir(t, filepath.Join(root, "an-earlier-dir"))
	mustMkdir(t, filepath.Join(root, "app"))
	mustMkdir(t, filepath.Join(root, "zzz-later-dir"))

	nav, cmd := NewNav(root, false, nil)
	runAndApply(t, nav, cmd)

	appIndex := indexOf(t, nav.Current(), "app")
	require.NotZero(t, appIndex, "test setup must put app after index 0")

	nav.SetCursor(appIndex)
	runAndApply(t, nav, nav.Enter())
	require.Equal(t, filepath.Join(root, "app"), nav.Path())

	runAndApply(t, nav, nav.Up())

	require.Equal(t, root, nav.Path())
	assert.Equal(t, appIndex, nav.Cursor(), "cursor must land back on app/, not reset to 0")
}

// TestNav_EnterOnVanishedDirectory verifies that entering a directory that
// disappeared from the filesystem between listing and entering is handled
// without panicking: the load simply fails and the error is surfaced on the
// column, exactly like any other failed read.
func TestNav_EnterOnVanishedDirectory(t *testing.T) {
	root := t.TempDir()
	gone := filepath.Join(root, "gone")
	mustMkdir(t, gone)

	nav, cmd := NewNav(root, false, nil)
	runAndApply(t, nav, cmd)

	nav.SetCursor(indexOf(t, nav.Current(), "gone"))

	// remove the directory after listing, but before Enter's read runs
	require.NoError(t, os.RemoveAll(gone))

	var enterCmd tea.Cmd
	assert.NotPanics(t, func() {
		enterCmd = nav.Enter()
	})
	require.NotNil(t, enterCmd)

	assert.NotPanics(t, func() {
		runAndApply(t, nav, enterCmd)
	})

	assert.Equal(t, gone, nav.Path(), "path still moves down; the failure surfaces as a column error")
	assert.Error(t, nav.Current().Err)
}

// TestNav_ParentColumnDerivedFromCurrentPath verifies that the parent column
// always reflects filepath.Dir(current path), independent of what the
// current column shows.
func TestNav_ParentColumnDerivedFromCurrentPath(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	mustMkdir(t, child)
	mustMkdir(t, filepath.Join(child, "grandchild"))

	nav, cmd := NewNav(child, false, nil)
	runAndApply(t, nav, cmd)

	assert.Equal(t, child, nav.Current().Listing.Dir)
	assert.Equal(t, root, nav.Parent().Listing.Dir)

	names := make([]string, 0, len(nav.Parent().Listing.Entries))
	for _, e := range nav.Parent().Listing.Entries {
		names = append(names, e.Name)
	}
	assert.Contains(t, names, "child")
}
