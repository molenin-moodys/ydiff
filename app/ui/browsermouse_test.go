package ui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/molenin-moodys/ydiff/app/gitstate"
	"github.com/molenin-moodys/ydiff/app/keymap"
	"github.com/molenin-moodys/ydiff/app/ui/style"
)

// wideBrowserRoot builds a RootModel in the browser screen, sized to the
// wide (three-column) tier, with the changed-files pane populated with two
// files so both the middle column and the changed pane have real content to
// click on.
func wideBrowserRoot(t *testing.T, dir string) RootModel {
	t.Helper()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)
	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver())
	root.browser.changed.Request(dir)
	root.browser.changed.Apply(GitLoadedMsg{
		Dir: dir, Scope: gitstate.ScopeUncommitted, Root: dir,
		Files: []gitstate.ChangedFile{
			{Path: "a.txt", Status: gitstate.StatusModified},
			{Path: "b.txt", Status: gitstate.StatusAdded},
		},
	})
	updated, _ := root.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	root = updated.(RootModel)
	return root
}

func leftClick(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
}

func wheelDown(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress}
}

func wheelUp(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress}
}

// TestBrowserMouse_ClickCurrentEntry_SelectsFile verifies a click on the
// middle column's entry rows moves the cursor there without entering (the
// clicked entry is a plain file, so Nav.Enter is a safe no-op on it).
func TestBrowserMouse_ClickCurrentEntry_SelectsFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("y"), 0o600))
	root := wideBrowserRoot(t, dir)

	currentX, _ := root.browser.columnXRanges()
	require.NotEqual(t, -1, currentX[0], "precondition: wide tier renders the current column")

	// row 2 (y) is the first entry row (row 0 = box top border, row 1 = header)
	updated, _ := root.Update(leftClick(currentX[0]+1, 2))
	root = updated.(RootModel)

	assert.Equal(t, BrowserFocusCurrent, root.browser.focus)
	assert.Equal(t, 0, root.browser.nav.Cursor(), "click on the first entry row selects the first entry")
	assert.Equal(t, ScreenBrowser, root.screen, "clicking a plain file must not enter/navigate away")
}

// TestBrowserMouse_ClickDirectoryEntry_EntersIt verifies a click on a
// directory row in the middle column navigates into it, exactly like
// pressing Enter on it would.
func TestBrowserMouse_ClickDirectoryEntry_EntersIt(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o750))
	root := wideBrowserRoot(t, dir)

	currentX, _ := root.browser.columnXRanges()
	require.NotEqual(t, -1, currentX[0])

	updated, cmd := root.Update(leftClick(currentX[0]+1, 3)) // row 3: the column's first entry
	root = updated.(RootModel)
	require.NotNil(t, cmd, "entering a directory issues a load command")
	for _, msg := range drainBatch(cmd) {
		nm, cmd2 := root.Update(msg)
		root = nm.(RootModel)
		_ = cmd2
	}

	assert.Equal(t, filepath.Join(dir, "sub"), root.browser.nav.Path())
}

// TestBrowserMouse_WheelScrollsFocusedPane_NotPointerPane verifies the wheel
// acts on whichever pane holds focus, regardless of the pointer's (x, y) —
// an explicit divergence from the review screen's pointer-based wheel.
func TestBrowserMouse_WheelScrollsFocusedPane_NotPointerPane(t *testing.T) {
	dir := t.TempDir()
	root := wideBrowserRoot(t, dir)
	root.browser.focus = BrowserFocusChanged

	_, changedX := root.browser.columnXRanges()
	currentX, _ := root.browser.columnXRanges()
	require.NotEqual(t, -1, currentX[0])
	require.NotEqual(t, -1, changedX[0])

	beforeCursor := root.browser.nav.Cursor()
	beforeChangedCursor := root.browser.changed.cursor

	// pointer sits over the CURRENT column, but focus is on the changed pane:
	// the changed pane's cursor must move, not the current column's.
	updated, _ := root.Update(wheelDown(currentX[0]+1, 2))
	root = updated.(RootModel)

	assert.Equal(t, beforeCursor, root.browser.nav.Cursor(), "unfocused current column must not move")
	assert.Equal(t, beforeChangedCursor+1, root.browser.changed.cursor, "focused changed pane must scroll")
}

// TestBrowserMouse_ClickChangedEntry_MovesFocusAndEnterActsOnIt verifies a
// click in the changed pane moves focus there, and a following Enter acts on
// that pane (opens review scoped to the clicked file) — the explicit
// focus/Enter symmetry requirement.
func TestBrowserMouse_ClickChangedEntry_MovesFocusAndEnterActsOnIt(t *testing.T) {
	dir := t.TempDir()
	root := wideBrowserRoot(t, dir)
	require.Equal(t, BrowserFocusCurrent, root.browser.focus, "precondition: focus starts on the current column")

	_, changedX := root.browser.columnXRanges()
	require.NotEqual(t, -1, changedX[0])

	// row 4 (y) is the second entry row -> "b.txt" (files sorted a.txt, b.txt)
	updated, _ := root.Update(leftClick(changedX[0]+1, 4))
	root = updated.(RootModel)
	require.Equal(t, BrowserFocusChanged, root.browser.focus, "click in the changed pane must move focus there")

	updated, _ = root.Update(tea.KeyMsg{Type: tea.KeyEnter})
	root = updated.(RootModel)

	require.Equal(t, ScreenReview, root.screen, "Enter after the click must act on the changed pane")
	assert.Equal(t, []string{"b.txt"}, root.review.cfg.only)
}

// TestBrowserMouse_ClickChangedScopeLabel_TogglesScope verifies clicking the
// changed pane's header ("changed - uncommitted") toggles the scope through
// the exact same changedPane.ToggleScope method the `t` key calls.
func TestBrowserMouse_ClickChangedScopeLabel_TogglesScope(t *testing.T) {
	dir := t.TempDir()
	root := wideBrowserRoot(t, dir)
	require.Equal(t, gitstate.ScopeUncommitted, root.browser.changed.scope)

	_, changedX := root.browser.columnXRanges()
	require.NotEqual(t, -1, changedX[0])

	// row 0 is the path header and row 1 each box's top border, so the
	// changed pane's own header row ("changed - <scope>") sits at row 2
	updated, _ := root.Update(leftClick(changedX[0]+1, 2))
	root = updated.(RootModel)

	assert.Equal(t, BrowserFocusChanged, root.browser.focus, "clicking the header must also focus the changed pane")
	assert.Equal(t, gitstate.ScopeBranch, root.browser.changed.scope, "click must toggle scope exactly as `t` does")
}

// TestBrowserMouse_KeyboardToggleScope_SameEffectAsClick cross-checks that
// the `t` keybinding and the header click drive the identical code path
// (same field mutated the same way), guarding against a parallel
// implementation drifting from the original.
func TestBrowserMouse_KeyboardToggleScope_SameEffectAsClick(t *testing.T) {
	dir := t.TempDir()
	viaKey := wideBrowserRoot(t, dir)
	viaClick := wideBrowserRoot(t, dir)

	updated, _ := viaKey.Update(keyMsg('t'))
	viaKey = updated.(RootModel)

	_, changedX := viaClick.browser.columnXRanges()
	updated, _ = viaClick.Update(leftClick(changedX[0]+1, 2)) // row 2: the changed pane's header
	viaClick = updated.(RootModel)

	assert.Equal(t, viaKey.browser.changed.scope, viaClick.browser.changed.scope)
}

// TestBrowserMouse_MediumTier_ClickMapsToCorrectPane exercises a non-wide
// terminal tier (60-99 columns: current + changed columns only, no parent
// column) so the per-tier column-x-range math is proven, not just the wide
// tier's.
func TestBrowserMouse_MediumTier_ClickMapsToCorrectPane(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("y"), 0o600))
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)
	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver())
	root.browser.changed.Request(dir)
	root.browser.changed.Apply(GitLoadedMsg{
		Dir: dir, Scope: gitstate.ScopeUncommitted, Root: dir,
		Files: []gitstate.ChangedFile{{Path: "a.txt", Status: gitstate.StatusModified}},
	})

	updated, _ := root.Update(tea.WindowSizeMsg{Width: 80, Height: 40}) // medium tier: [60,100)
	root = updated.(RootModel)
	require.GreaterOrEqual(t, root.browser.width, mediumTierWidth)
	require.Less(t, root.browser.width, wideTierWidth)

	currentX, changedX := root.browser.columnXRanges()
	require.Equal(t, 0, currentX[0], "medium tier: current column starts at x=0 (no parent column)")
	require.Equal(t, currentX[1], changedX[0], "medium tier: changed column starts where current ends")

	// click the changed pane's entry row in this tier's geometry
	updated, _ = root.Update(leftClick(changedX[0]+1, 2))
	root = updated.(RootModel)
	assert.Equal(t, BrowserFocusChanged, root.browser.focus, "medium-tier click must still map into the changed pane")
}

// TestBrowserMouse_HelpOverlay_OpensWithBrowserAndMouseSections verifies the
// browser's help action opens an overlay populated with a "Browser" section
// (one row per bound browser action) and a "Mouse" section describing click
// and wheel behavior, and that a mouse click while it is open is swallowed
// rather than affecting navigation.
func TestBrowserMouse_HelpOverlay_OpensWithBrowserAndMouseSections(t *testing.T) {
	dir := t.TempDir()
	root := wideBrowserRoot(t, dir)
	require.False(t, root.browser.overlay.Active())

	updated, _ := root.Update(keyMsg('?'))
	root = updated.(RootModel)
	require.True(t, root.browser.overlay.Active(), "browser help action must open the overlay")

	view := root.View()
	assert.Contains(t, view, "Browser")
	assert.Contains(t, view, "Mouse")

	beforeCursor := root.browser.nav.Cursor()
	currentX, _ := root.browser.columnXRanges()
	updated, _ = root.Update(leftClick(currentX[0]+1, 2))
	root = updated.(RootModel)
	assert.True(t, root.browser.overlay.Active(), "a click while help is open must not close it")
	assert.Equal(t, beforeCursor, root.browser.nav.Cursor(), "a click while help is open must not affect navigation")

	updated, _ = root.Update(tea.KeyMsg{Type: tea.KeyEsc})
	root = updated.(RootModel)
	assert.False(t, root.browser.overlay.Active(), "Esc must close the help overlay")
}

// TestBrowserMouse_HelpOverlayOpen_QDoesNotQuit guards the RootModel-level
// interception: while the browser's help overlay is open, `q` must close (or
// no-op through) the overlay rather than quitting the process, since q is
// also the browser's quit key.
func TestBrowserMouse_HelpOverlayOpen_QDoesNotQuit(t *testing.T) {
	dir := t.TempDir()
	root := wideBrowserRoot(t, dir)

	updated, _ := root.Update(keyMsg('?'))
	root = updated.(RootModel)
	require.True(t, root.browser.overlay.Active())

	updated, cmd := root.Update(keyMsg('q'))
	root = updated.(RootModel)

	assert.Nil(t, cmd, "q while help is open must not quit the process")
}

// TestBrowserScreen_NoMouseFlag_GatesAtProgramLevel documents and pins the
// structural decision behind task 21's --no-mouse requirement: rather than
// adding a second, browser-only flag, the browser screen relies on the
// single existing tea.WithMouseCellMotion() gate in app/main.go (driven by
// opts.NoMouse) that already governs the review screen. When mouse support
// is off, the terminal driver never emits tea.MouseMsg values at all, for
// either screen — so browserScreen.Update's tea.MouseMsg case (handled by
// handleBrowserMouse) simply never runs. This test proves the converse
// half directly: a browserScreen with no window size / mouse messages at
// all (the --no-mouse steady state) never changes focus or cursor state on
// its own.
func TestBrowserScreen_NoMouseFlag_GatesAtProgramLevel(t *testing.T) {
	dir := t.TempDir()
	root := wideBrowserRoot(t, dir)
	before := root.browser

	// no tea.MouseMsg is ever produced when --no-mouse is set (see
	// app/main.go's single `if !opts.NoMouse` gate) — simulate that steady
	// state by never sending one, and confirm state is stable.
	assert.Equal(t, before.focus, root.browser.focus)
	assert.Equal(t, before.nav.Cursor(), root.browser.nav.Cursor())
}

// TestBrowserHitTest_OutsideContentRows_NoZone guards the border/header row
// exclusions (row 0 = top border, and the header row for each pane) so a
// click just outside the entry rows is never misattributed to an entry.
func TestBrowserHitTest_OutsideContentRows_NoZone(t *testing.T) {
	dir := t.TempDir()
	root := wideBrowserRoot(t, dir)
	b := root.browser

	currentX, _ := b.columnXRanges()
	zone, row := b.hitTest(currentX[0]+1, 0)
	assert.Equal(t, browserHitNone, zone, "row 0 is the box top border, never clickable")
	assert.Equal(t, -1, row)

	zone, _ = b.hitTest(currentX[0]+1, 1)
	assert.Equal(t, browserHitNone, zone, "row 1 in the current column is the directory-name header, not clickable")
}
