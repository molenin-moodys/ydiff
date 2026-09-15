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

// TestNav_ToggleHidden_ChangesListingAndRestoresCursor verifies that
// ToggleHidden actually changes what the listing contains (the acceptance
// review's defect 2: the field existed but nothing ever flipped it) and that
// the cursor lands back on the same entry by name rather than resetting to
// the top.
func TestNav_ToggleHidden_ChangesListingAndRestoresCursor(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".hidden-dir"))
	mustMkdir(t, filepath.Join(root, "visible-a"))
	mustMkdir(t, filepath.Join(root, "visible-b"))

	nav, cmd := NewNav(root, false, nil)
	runAndApply(t, nav, cmd)

	names := func() []string {
		var out []string
		for _, e := range nav.VisibleEntries() {
			out = append(out, e.Name)
		}
		return out
	}
	require.NotContains(t, names(), ".hidden-dir", "precondition: hidden by default")

	nav.SetCursor(indexOf(t, nav.Current(), "visible-b"))

	toggleCmd := nav.ToggleHidden()
	require.NotNil(t, toggleCmd)
	runAndApply(t, nav, toggleCmd)

	assert.Contains(t, names(), ".hidden-dir", "toggling on must reveal the dotfile directory")
	assert.Equal(t, "visible-b", names()[nav.Cursor()], "cursor must stay on the previously selected entry")

	toggleCmd = nav.ToggleHidden()
	runAndApply(t, nav, toggleCmd)

	assert.NotContains(t, names(), ".hidden-dir", "toggling off must hide the dotfile directory again")
	assert.Equal(t, "visible-b", names()[nav.Cursor()], "cursor must still track the same entry after toggling back off")
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

// TestNav_GoTo_JumpsToArbitraryPath verifies the favorites popup's use case:
// jumping straight to an unrelated directory, not just a child or the parent.
func TestNav_GoTo_JumpsToArbitraryPath(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	mustMkdir(t, filepath.Join(other, "sub"))

	nav, cmd := NewNav(root, false, nil)
	runAndApply(t, nav, cmd)

	goCmd := nav.GoTo(other)
	require.NotNil(t, goCmd)
	runAndApply(t, nav, goCmd)

	assert.Equal(t, other, nav.Path())
	assert.Equal(t, 0, nav.Cursor())
}

// TestNav_GoTo_SamePathIsNoOp mirrors Up's filesystem-root guard: jumping to
// the directory already current must not reload or reset the cursor.
func TestNav_GoTo_SamePathIsNoOp(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "app"))

	nav, cmd := NewNav(root, false, nil)
	runAndApply(t, nav, cmd)
	nav.SetCursor(indexOf(t, nav.Current(), "app"))

	goCmd := nav.GoTo(root)

	assert.Nil(t, goCmd, "jumping to the current path must be a no-op")
	assert.Equal(t, indexOf(t, nav.Current(), "app"), nav.Cursor(), "cursor must be untouched")
}

// TestNav_GoTo_RestoresCursorMemory verifies GoTo consults the same per-path
// cursor memory Up does: returning to a previously-visited directory lands
// back where the cursor was, not at the top of the list.
func TestNav_GoTo_RestoresCursorMemory(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	mustMkdir(t, filepath.Join(root, "an-earlier-dir"))
	mustMkdir(t, filepath.Join(root, "app"))
	mustMkdir(t, filepath.Join(root, "zzz-later-dir"))

	nav, cmd := NewNav(root, false, nil)
	runAndApply(t, nav, cmd)

	appIndex := indexOf(t, nav.Current(), "app")
	require.NotZero(t, appIndex, "test setup must put app after index 0")
	nav.SetCursor(appIndex)

	runAndApply(t, nav, nav.GoTo(other))
	require.Equal(t, other, nav.Path())

	runAndApply(t, nav, nav.GoTo(root))

	require.Equal(t, root, nav.Path())
	assert.Equal(t, appIndex, nav.Cursor(), "cursor must land back on app/, restored from memory")
}

// TestNav_GoTo_DropsActiveFilter verifies GoTo follows the same
// filter-resets-on-directory-change rule as Enter and Up.
func TestNav_GoTo_DropsActiveFilter(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()

	nav, cmd := NewNav(root, false, nil)
	runAndApply(t, nav, cmd)
	nav.FilterStart()
	nav.FilterAppend('x')
	require.True(t, nav.Filter().Active())

	runAndApply(t, nav, nav.GoTo(other))

	assert.False(t, nav.Filter().Active(), "GoTo must drop any active filter")
}
