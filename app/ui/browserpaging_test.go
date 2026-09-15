package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/molenin-moodys/ydiff/app/gitstate"
	"github.com/molenin-moodys/ydiff/app/keymap"
	"github.com/molenin-moodys/ydiff/app/ui/style"
)

// newPagingRoot builds a browser-rooted RootModel over a directory holding
// count files, sized so the visible pane is deliberately shorter than the
// listing — otherwise a page jump and an End jump would be indistinguishable.
func newPagingRoot(t *testing.T, count, height int) RootModel {
	t.Helper()

	dir := t.TempDir()
	for i := range count {
		name := fmt.Sprintf("file-%02d.txt", i)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600))
	}

	root := NewRootBrowser(
		newTestNav(t, dir), keymap.Default(), testModel(nil, nil),
		nil, gitstate.ScopeUncommitted, style.PlainResolver(),
	)
	updated, _ := root.Update(tea.WindowSizeMsg{Width: 120, Height: height})
	return updated.(RootModel) //nolint:errcheck // RootModel.Update always returns a RootModel
}

// cursorAfter presses keys and reports the resulting directory cursor.
//
// Note it mutates shared state: RootModel is copied by value, but
// browser.nav behind it is a pointer, so cursor position accumulates across
// calls on the same root. Each test therefore builds its own root and treats
// key presses as one continuous sequence.
func cursorAfter(t *testing.T, root RootModel, keys ...tea.KeyMsg) int {
	t.Helper()
	for _, k := range keys {
		updated, _ := root.Update(k)
		root = updated.(RootModel) //nolint:errcheck // RootModel.Update always returns a RootModel
	}
	return root.browser.nav.Cursor()
}

func TestBrowserPaging_PageDownMovesByPaneHeight(t *testing.T) {
	root := newPagingRoot(t, 40, 14) // paneContentHeight == height-browserChromeRows == 10
	require.Equal(t, 10, root.browser.paneContentHeight(), "precondition: pane shows 10 rows")

	got := cursorAfter(t, root, tea.KeyMsg{Type: tea.KeyPgDown})
	assert.Equal(t, 10, got, "page down moves the cursor exactly one visible pane")

	got = cursorAfter(t, root, tea.KeyMsg{Type: tea.KeyPgDown})
	assert.Equal(t, 20, got, "a second page down advances another pane")
}

func TestBrowserPaging_PageUpMovesBack(t *testing.T) {
	root := newPagingRoot(t, 40, 14)

	got := cursorAfter(t, root,
		tea.KeyMsg{Type: tea.KeyPgDown},
		tea.KeyMsg{Type: tea.KeyPgDown},
	)
	require.Equal(t, 20, got, "precondition: two pages down")

	got = cursorAfter(t, root, tea.KeyMsg{Type: tea.KeyPgUp})
	assert.Equal(t, 10, got, "page up is the exact inverse of page down")
}

// A page jump past either end must clamp rather than run off the list: an
// out-of-range cursor is an index-out-of-range waiting to happen the next
// time the view renders.
func TestBrowserPaging_ClampsAtBothEnds(t *testing.T) {
	root := newPagingRoot(t, 12, 14) // 12 entries, 10 visible rows

	got := cursorAfter(t, root, tea.KeyMsg{Type: tea.KeyPgUp})
	assert.Equal(t, 0, got, "page up from the top stays on the first entry")

	got = cursorAfter(t, root,
		tea.KeyMsg{Type: tea.KeyPgDown},
		tea.KeyMsg{Type: tea.KeyPgDown},
		tea.KeyMsg{Type: tea.KeyPgDown},
	)
	assert.Equal(t, 11, got, "paging past the end stops on the last entry")
}

func TestBrowserPaging_HomeAndEnd(t *testing.T) {
	root := newPagingRoot(t, 40, 14)

	got := cursorAfter(t, root, tea.KeyMsg{Type: tea.KeyEnd})
	assert.Equal(t, 39, got, "end jumps to the last entry")

	got = cursorAfter(t, root, tea.KeyMsg{Type: tea.KeyEnd}, tea.KeyMsg{Type: tea.KeyHome})
	assert.Equal(t, 0, got, "home jumps back to the first entry")
}

// Paging must walk the *filtered* listing, not the full one — otherwise End
// would land on an index that no longer exists on screen.
func TestBrowserPaging_RespectsActiveFilter(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"alpha.txt", "beta.txt", "match-a.go", "match-b.go", "match-c.go"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600))
	}

	root := NewRootBrowser(
		newTestNav(t, dir), keymap.Default(), testModel(nil, nil),
		nil, gitstate.ScopeUncommitted, style.PlainResolver(),
	)
	updated, _ := root.Update(tea.WindowSizeMsg{Width: 120, Height: 14})
	root = updated.(RootModel) //nolint:errcheck // RootModel.Update always returns a RootModel

	// Narrow to the three "match-" files and apply with Enter: while the
	// filter is still being edited, ResolveBrowser deliberately swallows
	// every key but Enter and Esc so letters stay literal filter text, so
	// End only becomes live once the filter is applied.
	for _, k := range []tea.KeyMsg{keyMsg('/'), keyMsg('m'), keyMsg('a'), keyMsg('t'), {Type: tea.KeyEnter}} {
		updated, _ = root.Update(k)
		root = updated.(RootModel) //nolint:errcheck // RootModel.Update always returns a RootModel
	}
	require.Len(t, root.browser.nav.VisibleEntries(), 3, "precondition: filter narrowed to three entries")

	got := cursorAfter(t, root, tea.KeyMsg{Type: tea.KeyEnd})
	assert.Equal(t, 2, got, "end clamps to the filtered listing, not the full one")
}

// The changed-files pane shares moveCursor, so paging must work there too
// once Tab has moved focus.
func TestBrowserPaging_AppliesToFocusedPane(t *testing.T) {
	root := newPagingRoot(t, 40, 14)

	updated, _ := root.Update(tea.KeyMsg{Type: tea.KeyTab})
	root = updated.(RootModel) //nolint:errcheck // RootModel.Update always returns a RootModel
	require.Equal(t, BrowserFocusChanged, root.browser.focus, "precondition: focus moved to the changed pane")

	before := root.browser.nav.Cursor()
	got := cursorAfter(t, root, tea.KeyMsg{Type: tea.KeyPgDown})
	assert.Equal(t, before, got, "paging with the changed pane focused must not move the directory cursor")
}

// The path header is the view's top row: the route is muted, the directory
// you are standing in is picked out, and the home prefix is abbreviated.
func TestBrowserView_PathHeaderShowsCurrentDirectory(t *testing.T) {
	root := newPagingRoot(t, 3, 14)
	dir := root.browser.nav.Path()

	lines := strings.Split(root.View(), "\n")
	require.GreaterOrEqual(t, len(lines), 2)

	assert.Contains(t, lines[0], filepath.Base(dir), "the top row names the current directory")
	assert.NotContains(t, lines[0], "│", "the top row is the path header, not a column box border")
}

// Left is the inverse of the Tab that moved focus into the changed pane: it
// returns focus rather than navigating the filesystem out from under a pane
// the cursor is not in.
func TestBrowserScreen_LeftFromChangedPane_ReturnsFocus(t *testing.T) {
	root := newPagingRoot(t, 3, 14)
	before := root.browser.nav.Path()

	updated, _ := root.Update(tea.KeyMsg{Type: tea.KeyTab})
	root = updated.(RootModel) //nolint:errcheck // RootModel.Update always returns a RootModel
	require.Equal(t, BrowserFocusChanged, root.browser.focus, "precondition: focus in the changed pane")

	updated, _ = root.Update(tea.KeyMsg{Type: tea.KeyLeft})
	root = updated.(RootModel) //nolint:errcheck // RootModel.Update always returns a RootModel

	assert.Equal(t, BrowserFocusCurrent, root.browser.focus, "left returns focus to the directory columns")
	assert.Equal(t, before, root.browser.nav.Path(), "left must not navigate up while the changed pane was focused")

	// with focus back on the columns, left resumes its normal meaning
	updated, _ = root.Update(tea.KeyMsg{Type: tea.KeyLeft})
	root = updated.(RootModel) //nolint:errcheck // RootModel.Update always returns a RootModel
	assert.Equal(t, filepath.Dir(before), root.browser.nav.Path(), "left now navigates up one level")
}
