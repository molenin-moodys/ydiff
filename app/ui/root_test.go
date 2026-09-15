package ui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/molenin-moodys/ydiff/app/browser"
	"github.com/molenin-moodys/ydiff/app/gitstate"
	"github.com/molenin-moodys/ydiff/app/keymap"
	"github.com/molenin-moodys/ydiff/app/ui/style"
)

// keyMsg builds a tea.KeyMsg for a single-rune key, matching the pattern
// used throughout app/ui's other tests.
func keyMsg(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// newTestNav creates browser navigation state rooted at dir, running its
// initial load command synchronously so the nav is immediately usable
// without a real Bubble Tea runtime.
func newTestNav(t *testing.T, dir string) *browser.Nav {
	t.Helper()
	nav, cmd := browser.NewNav(dir, false, nil)
	if cmd != nil {
		for _, msg := range drainBatch(cmd) {
			if lm, ok := msg.(browser.LoadedMsg); ok {
				nav.Apply(lm)
			}
		}
	}
	return nav
}

// drainBatch runs a tea.Cmd and, if it produced a tea.BatchMsg, runs each of
// its sub-commands too, collecting every resulting message. Bubble Tea's own
// runtime does this internally; tests driving Update directly need the same
// unwrapping to apply a Nav's initial load synchronously.
func drainBatch(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for _, c := range batch {
		out = append(out, drainBatch(c)...)
	}
	return out
}

// initGitRepo creates a minimal git repository in dir, for tests that
// exercise gitstate cache invalidation on return from the review screen.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := c.CombinedOutput()
		require.NoErrorf(t, err, "git %v: %s", args, out)
	}
	run("init", "--quiet", ".")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o600))
	run("add", "README.md")
	run("commit", "--quiet", "-m", "initial commit")
}

func TestRootModel_BrowserReview_DPushesReviewScreen(t *testing.T) {
	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver())
	require.Equal(t, ScreenBrowser, root.screen)

	updated, _ := root.Update(keyMsg('d'))
	root = updated.(RootModel)

	assert.Equal(t, ScreenReview, root.screen)
}

func TestRootModel_BrowserReview_QInReviewPopsToBrowser(t *testing.T) {
	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver())

	updated, _ := root.Update(keyMsg('d'))
	root = updated.(RootModel)
	require.Equal(t, ScreenReview, root.screen, "precondition: d pushed the review screen")

	updated, cmd := root.Update(keyMsg('q'))
	root = updated.(RootModel)

	assert.Equal(t, ScreenBrowser, root.screen, "q in review must pop back to the browser, not quit")
	assert.Nil(t, cmd, "popping back to the browser must not quit the process")
}

func TestRootModel_Browser_QQuits(t *testing.T) {
	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver())

	_, cmd := root.Update(keyMsg('q'))

	require.NotNil(t, cmd, "q in the browser must quit")
	msg := cmd()
	assert.IsType(t, tea.QuitMsg{}, msg)
}

// TestRootModel_StraightIntoReview_QExitsProcess is the entry-point test:
// launched straight into review (the path Claude Code uses, with diff
// arguments), q exits the process rather than revealing a browser that was
// never there.
func TestRootModel_StraightIntoReview_QExitsProcess(t *testing.T) {
	review := testModel(nil, nil)

	root := NewRootReview(review)
	require.Equal(t, ScreenReview, root.screen)
	require.False(t, root.hasBrowser)

	_, cmd := root.Update(keyMsg('q'))

	require.NotNil(t, cmd, "q with no browser behind review must quit")
	msg := cmd()
	assert.IsType(t, tea.QuitMsg{}, msg, "q must exit the process, not reveal a browser")
}

// TestRootModel_WindowResize_ReachesBothScreens asserts that a resize while
// in review is also applied to the browser, so popping back to it afterward
// does not show a stale layout.
func TestRootModel_WindowResize_ReachesBothScreens(t *testing.T) {
	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver())

	updated, _ := root.Update(keyMsg('d'))
	root = updated.(RootModel)
	require.Equal(t, ScreenReview, root.screen, "precondition: now in review")

	updated, _ = root.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	root = updated.(RootModel)

	assert.Equal(t, 100, root.browser.width, "resize while in review must still reach the browser")
	assert.Equal(t, 40, root.browser.height)
	assert.Equal(t, 100, root.review.layout.width, "resize must also reach the active review screen")
	assert.Equal(t, 40, root.review.layout.height)
}

// TestRootModel_ReturnFromReview_InvalidatesGitCache verifies the gitstate
// cache invalidation on return from the review screen: a file may have just
// been edited there, so the changed-files list must be recomputed.
func TestRootModel_ReturnFromReview_InvalidatesGitCache(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)

	var loadCalls int
	cache := gitstate.NewCache(func(root string, scope gitstate.Scope) ([]gitstate.ChangedFile, error) {
		loadCalls++
		return nil, nil
	})

	root := NewRootBrowser(nav, keymap.Default(), review, cache, gitstate.ScopeUncommitted, style.PlainResolver())

	repo, err := gitstate.Resolve(dir)
	require.NoError(t, err)

	_, err = cache.Get(repo.Root, gitstate.ScopeUncommitted)
	require.NoError(t, err)
	require.Equal(t, 1, loadCalls, "precondition: cache populated once")

	_, err = cache.Get(repo.Root, gitstate.ScopeUncommitted)
	require.NoError(t, err)
	require.Equal(t, 1, loadCalls, "precondition: second Get is served from cache")

	// push into review, then pop back with q
	updated, _ := root.Update(keyMsg('d'))
	root = updated.(RootModel)
	updated, _ = root.Update(keyMsg('q'))
	root = updated.(RootModel)
	require.Equal(t, ScreenBrowser, root.screen, "precondition: popped back to the browser")

	_, err = cache.Get(repo.Root, gitstate.ScopeUncommitted)
	require.NoError(t, err)
	assert.Equal(t, 2, loadCalls, "return from review must invalidate the cache, forcing a recompute")
}

// TestRootModel_View_RendersRealBrowserView is task 20's carry-over
// acceptance criterion from task 19: the browser screen must render
// RenderBrowserView's real three-column layout, not the placeholder string
// browserScreen.View used to return.
func TestRootModel_View_RendersRealBrowserView(t *testing.T) {
	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver())
	root.Init() // marks the changed-files pane as loading; nil gitCache means no cmd actually runs
	updated, _ := root.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	root = updated.(RootModel)

	view := root.View()
	assert.NotEqual(t, "ydiff — "+dir, view, "must not be the task-19 placeholder")
	assert.Contains(t, view, "changed", "the changed-files pane's header must be present")
	// a nil gitCache means LoadGitState never actually loads (see
	// changedPane.Request), so the pane stays in its loading state forever
	// here rather than ever resolving to "not a git repository" — it is
	// still a real, non-empty rendering rather than the old placeholder.
	assert.Contains(t, view, changedPaneMsgLoading)
}

// TestRootModel_TabTogglesFocus verifies Tab (browser_focus_pane) switches
// focus between the middle column and the changed-files pane, and back.
func TestRootModel_TabTogglesFocus(t *testing.T) {
	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver())
	require.Equal(t, BrowserFocusCurrent, root.browser.focus)

	updated, _ := root.Update(tea.KeyMsg{Type: tea.KeyTab})
	root = updated.(RootModel)
	assert.Equal(t, BrowserFocusChanged, root.browser.focus)

	updated, _ = root.Update(tea.KeyMsg{Type: tea.KeyTab})
	root = updated.(RootModel)
	assert.Equal(t, BrowserFocusCurrent, root.browser.focus)
}

// TestRootModel_EnterOnChangedFileInMiddleColumn_OpensScopedReview and its
// sibling below exercise the design's symmetry requirement: Enter behaves
// the same whether the cursor is on a changed file in the middle column or
// in the changed-files pane itself, and does nothing for an unchanged file.
func TestRootModel_EnterOnChangedFileInMiddleColumn_OpensScopedReview(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "changed.txt"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "unchanged.txt"), []byte("y"), 0o600))
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver())
	root.browser.changed.Request(dir)
	root.browser.changed.Apply(GitLoadedMsg{
		Dir: dir, Scope: gitstate.ScopeUncommitted, Root: dir,
		Files: []gitstate.ChangedFile{{Path: "changed.txt", Status: gitstate.StatusModified}},
	})

	nav.SetCursor(0) // "changed.txt" sorts before "unchanged.txt"
	updated, _ := root.Update(tea.KeyMsg{Type: tea.KeyEnter})
	root = updated.(RootModel)

	require.Equal(t, ScreenReview, root.screen, "Enter on a changed file must open review")
	assert.Equal(t, []string{"changed.txt"}, root.review.cfg.only)
}

func TestRootModel_EnterOnUnchangedFileInMiddleColumn_NoOp(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "changed.txt"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "unchanged.txt"), []byte("y"), 0o600))
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver())
	root.browser.changed.Request(dir)
	root.browser.changed.Apply(GitLoadedMsg{
		Dir: dir, Scope: gitstate.ScopeUncommitted, Root: dir,
		Files: []gitstate.ChangedFile{{Path: "changed.txt", Status: gitstate.StatusModified}},
	})

	nav.SetCursor(1) // "unchanged.txt"
	updated, _ := root.Update(tea.KeyMsg{Type: tea.KeyEnter})
	root = updated.(RootModel)

	assert.Equal(t, ScreenBrowser, root.screen, "Enter on an unchanged file must be a no-op")
}

func TestRootModel_EnterInChangedPane_OpensScopedReview(t *testing.T) {
	dir := t.TempDir()
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
	root.browser.focus = BrowserFocusChanged
	root.browser.changed.MoveCursor(1) // select "b.txt"

	updated, _ := root.Update(tea.KeyMsg{Type: tea.KeyEnter})
	root = updated.(RootModel)

	require.Equal(t, ScreenReview, root.screen)
	assert.Equal(t, []string{"b.txt"}, root.review.cfg.only)
}

// TestRootModel_BrowserFilter_TypedRunesNarrowTheQuery is the acceptance-review
// defect-1 regression test: `/` opens the filter box, but typed characters
// used to be silently discarded because no caller routed tea.KeyRunes to
// Nav.FilterAppend. Driving the keys through RootModel.Update (the same path
// a real keypress takes) must grow the query, not leave it permanently empty.
func TestRootModel_BrowserFilter_TypedRunesNarrowTheQuery(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.md"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.txt"), []byte("x"), 0o600))
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver())

	updated, _ := root.Update(keyMsg('/'))
	root = updated.(RootModel)
	require.True(t, root.browser.nav.Filter().Editing(), "precondition: / opens the filter for editing")

	updated, _ = root.Update(keyMsg('r'))
	root = updated.(RootModel)
	updated, _ = root.Update(keyMsg('e'))
	root = updated.(RootModel)

	assert.Equal(t, "re", root.browser.nav.Filter().Query(),
		"typed runes must append to the filter query through handleKey, not be discarded")

	updated, _ = root.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	root = updated.(RootModel)
	assert.Equal(t, "r", root.browser.nav.Filter().Query(), "backspace must remove the last rune of the query")
}

// TestRootModel_BrowserToggleHidden_RevealsDotfiles is the acceptance-review
// defect-2 regression test: `.` is bound to ActionBrowserToggleHidden and
// advertised in help, but used to be a no-op because Nav had no
// ToggleHidden method and handleKey never called one. Driving `.` through
// RootModel.Update must actually change what the listing contains.
func TestRootModel_BrowserToggleHidden_RevealsDotfiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".hidden"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("x"), 0o600))
	nav := newTestNav(t, dir) // showHidden defaults to false
	review := testModel(nil, nil)

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver())

	names := func() []string {
		var out []string
		for _, e := range root.browser.nav.VisibleEntries() {
			out = append(out, e.Name)
		}
		return out
	}
	require.NotContains(t, names(), ".hidden", "precondition: hidden file not shown by default")

	updated, cmd := root.Update(keyMsg('.'))
	root = updated.(RootModel)
	for _, msg := range drainBatch(cmd) {
		if lm, ok := msg.(browser.LoadedMsg); ok {
			root.browser.nav.Apply(lm)
		}
	}

	assert.Contains(t, names(), ".hidden", "toggling hidden must reveal dotfiles")
	assert.Contains(t, names(), "visible.txt", "toggling hidden must not lose already-visible entries")

	// toggling back off must hide it again
	updated, cmd = root.Update(keyMsg('.'))
	root = updated.(RootModel)
	for _, msg := range drainBatch(cmd) {
		if lm, ok := msg.(browser.LoadedMsg); ok {
			root.browser.nav.Apply(lm)
		}
	}
	assert.NotContains(t, names(), ".hidden", "toggling hidden again must hide dotfiles once more")
}

// fakeFavoritesService is a minimal in-memory FavoritesService for tests
// that don't need real file persistence — just observable Toggle/List/Remove
// behavior. listErr, toggleErr and removeErr let a test force each method's
// error path.
type fakeFavoritesService struct {
	items     []string
	listErr   error
	toggleErr error
	removeErr error
}

func (f *fakeFavoritesService) List() ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]string, len(f.items))
	copy(out, f.items)
	return out, nil
}

func (f *fakeFavoritesService) Toggle(dir string) (bool, error) {
	if f.toggleErr != nil {
		return false, f.toggleErr
	}
	for i, item := range f.items {
		if item == dir {
			f.items = append(f.items[:i], f.items[i+1:]...)
			return false, nil
		}
	}
	f.items = append(f.items, dir)
	return true, nil
}

func (f *fakeFavoritesService) Remove(dir string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	for i, item := range f.items {
		if item == dir {
			f.items = append(f.items[:i], f.items[i+1:]...)
			return nil
		}
	}
	return nil
}

func TestRootModel_Browser_CtrlFTogglesFavoriteAndSetsHint(t *testing.T) {
	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)
	svc := &fakeFavoritesService{}

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver()).
		WithFavoritesService(svc)

	updated, _ := root.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	root = updated.(RootModel)

	assert.Equal(t, []string{dir}, svc.items, "Ctrl+F must add the current directory")
	assert.Equal(t, "added to favorites", root.browser.hint)
}

func TestRootModel_Browser_CtrlFTwiceRemoves(t *testing.T) {
	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)
	svc := &fakeFavoritesService{}

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver()).
		WithFavoritesService(svc)

	updated, _ := root.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	root = updated.(RootModel)
	updated, _ = root.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	root = updated.(RootModel)

	assert.Empty(t, svc.items, "second Ctrl+F must remove it again")
	assert.Equal(t, "removed from favorites", root.browser.hint)
}

func TestRootModel_Browser_CtrlFNilServiceIsNoOp(t *testing.T) {
	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver())

	var updated tea.Model
	assert.NotPanics(t, func() {
		updated, _ = root.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	})
	root = updated.(RootModel)
	assert.Empty(t, root.browser.hint)
}

func TestRootModel_Browser_FOpensFavoritesOverlay(t *testing.T) {
	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)
	svc := &fakeFavoritesService{items: []string{"/repo/one", "/repo/two"}}

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver()).
		WithFavoritesService(svc)

	updated, _ := root.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	root = updated.(RootModel)
	updated, _ = root.Update(keyMsg('f'))
	root = updated.(RootModel)

	require.True(t, root.browser.overlay.Active())
	view := root.browser.View()
	assert.Contains(t, view, "/repo/one")
	assert.Contains(t, view, "/repo/two")
}

func TestRootModel_Browser_FavoritesEnterNavigatesAndCloses(t *testing.T) {
	dir := t.TempDir()
	target := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)
	svc := &fakeFavoritesService{items: []string{target}}

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver()).
		WithFavoritesService(svc)

	updated, _ := root.Update(keyMsg('f'))
	root = updated.(RootModel)
	require.True(t, root.browser.overlay.Active())

	updated, cmd := root.Update(tea.KeyMsg{Type: tea.KeyEnter})
	root = updated.(RootModel)
	for _, msg := range drainBatch(cmd) {
		if lm, ok := msg.(browser.LoadedMsg); ok {
			root.browser.nav.Apply(lm)
		}
	}

	assert.False(t, root.browser.overlay.Active(), "Enter on a favorite must close the popup")
	assert.Equal(t, target, root.browser.nav.Path())
}

func TestRootModel_Browser_FavoritesDeleteRemovesAndKeepsOpen(t *testing.T) {
	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)
	svc := &fakeFavoritesService{items: []string{"/repo/one", "/repo/two"}}

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver()).
		WithFavoritesService(svc)

	updated, _ := root.Update(keyMsg('f'))
	root = updated.(RootModel)

	updated, _ = root.Update(tea.KeyMsg{Type: tea.KeyDelete})
	root = updated.(RootModel)

	assert.True(t, root.browser.overlay.Active(), "deleting a favorite must not close the popup")
	assert.Equal(t, []string{"/repo/two"}, svc.items)
	view := root.browser.View()
	assert.NotContains(t, view, "/repo/one")
	assert.Contains(t, view, "/repo/two")
}

func TestRootModel_Browser_FavoritesNilServiceOpensEmptyPopup(t *testing.T) {
	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver())

	var updated tea.Model
	assert.NotPanics(t, func() {
		updated, _ = root.Update(keyMsg('f'))
	})
	root = updated.(RootModel)
	require.True(t, root.browser.overlay.Active(), "F must still open the popup, empty, with a nil service")
}

func TestRootModel_Browser_FavoritesListErrorSetsHintNotPanic(t *testing.T) {
	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)
	svc := &fakeFavoritesService{listErr: errors.New("boom")}

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver()).
		WithFavoritesService(svc)

	var updated tea.Model
	assert.NotPanics(t, func() {
		updated, _ = root.Update(keyMsg('f'))
	})
	root = updated.(RootModel)
	assert.Contains(t, root.browser.hint, "boom")
}

// stubThemeCatalog is a minimal in-memory ThemeCatalog for tests — no
// filesystem, just observable Entries/Resolve/Persist behavior. entriesErr
// and persistErr let a test force each method's error path.
type stubThemeCatalog struct {
	entries    []ThemeEntry
	specs      map[string]ThemeSpec
	entriesErr error
	persistErr error
	persisted  []string
}

func (f *stubThemeCatalog) Entries() ([]ThemeEntry, error) {
	if f.entriesErr != nil {
		return nil, f.entriesErr
	}
	return f.entries, nil
}

func (f *stubThemeCatalog) Resolve(name string) (ThemeSpec, bool) {
	spec, ok := f.specs[name]
	return spec, ok
}

func (f *stubThemeCatalog) Persist(name string) error {
	if f.persistErr != nil {
		return f.persistErr
	}
	f.persisted = append(f.persisted, name)
	return nil
}

func TestRootModel_Browser_TOpensThemeSelector(t *testing.T) {
	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)
	cat := &stubThemeCatalog{entries: []ThemeEntry{{Name: "nord"}, {Name: "dracula"}}}

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver()).
		WithThemeCatalog(cat, "", false)

	updated, _ := root.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	root = updated.(RootModel)
	updated, _ = root.Update(keyMsg('T'))
	root = updated.(RootModel)

	require.True(t, root.browser.overlay.Active())
	view := root.browser.View()
	assert.Contains(t, view, "nord")
	assert.Contains(t, view, "dracula")
}

func TestRootModel_Browser_TNilCatalogIsNoOp(t *testing.T) {
	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver())

	var updated tea.Model
	assert.NotPanics(t, func() {
		updated, _ = root.Update(keyMsg('T'))
	})
	root = updated.(RootModel)
	assert.False(t, root.browser.overlay.Active(), "T with no catalog must be a no-op, not an empty popup")
}

func TestRootModel_Browser_ThemeArrowPreviewsWithoutPersisting(t *testing.T) {
	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)
	nordColors := style.Colors{Accent: "#ff0000"}
	cat := &stubThemeCatalog{
		entries: []ThemeEntry{{Name: "nord"}, {Name: "dracula"}},
		specs:   map[string]ThemeSpec{"dracula": {Colors: nordColors}},
	}

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver()).
		WithThemeCatalog(cat, "nord", false)

	updated, _ := root.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	root = updated.(RootModel)
	updated, _ = root.Update(keyMsg('T'))
	root = updated.(RootModel)

	updated, _ = root.Update(tea.KeyMsg{Type: tea.KeyDown})
	root = updated.(RootModel)

	assert.Empty(t, cat.persisted, "moving the cursor must only preview, never persist")
	assert.True(t, root.browser.overlay.Active(), "preview must not close the popup")
}

func TestRootModel_Browser_ThemeEnterConfirmsAndPersists(t *testing.T) {
	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)
	cat := &stubThemeCatalog{
		entries: []ThemeEntry{{Name: "nord"}},
		specs:   map[string]ThemeSpec{"nord": {Colors: style.Colors{Accent: "#00ff00"}}},
	}

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver()).
		WithThemeCatalog(cat, "", false)

	updated, _ := root.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	root = updated.(RootModel)
	updated, _ = root.Update(keyMsg('T'))
	root = updated.(RootModel)

	updated, _ = root.Update(tea.KeyMsg{Type: tea.KeyEnter})
	root = updated.(RootModel)

	assert.False(t, root.browser.overlay.Active(), "Enter must close the popup")
	assert.Equal(t, []string{"nord"}, cat.persisted)
}

func TestRootModel_Browser_ThemeEscCancelsWithoutPersisting(t *testing.T) {
	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)
	cat := &stubThemeCatalog{
		entries: []ThemeEntry{{Name: "nord"}},
		specs:   map[string]ThemeSpec{"nord": {Colors: style.Colors{Accent: "#00ff00"}}},
	}

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver()).
		WithThemeCatalog(cat, "", false)

	updated, _ := root.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	root = updated.(RootModel)
	updated, _ = root.Update(keyMsg('T'))
	root = updated.(RootModel)

	updated, _ = root.Update(tea.KeyMsg{Type: tea.KeyDown}) // preview
	root = updated.(RootModel)
	updated, _ = root.Update(tea.KeyMsg{Type: tea.KeyEsc})
	root = updated.(RootModel)

	assert.False(t, root.browser.overlay.Active(), "Esc must close the popup")
	assert.Empty(t, cat.persisted, "Esc must not persist the previewed theme")
}

func TestRootModel_Browser_ThemeNoColorsOverridesResolver(t *testing.T) {
	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)
	cat := &stubThemeCatalog{
		entries: []ThemeEntry{{Name: "nord"}},
		specs:   map[string]ThemeSpec{"nord": {Colors: style.Colors{Accent: "#00ff00"}}},
	}

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver()).
		WithThemeCatalog(cat, "", true) // noColors=true

	updated, _ := root.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	root = updated.(RootModel)
	updated, _ = root.Update(keyMsg('T'))
	root = updated.(RootModel)
	updated, _ = root.Update(tea.KeyMsg{Type: tea.KeyEnter})
	root = updated.(RootModel)

	assert.Equal(t, style.PlainResolver(), root.browser.resolver,
		"noColors must keep the resolver plain even after confirming a theme")
}

func TestRootModel_Browser_ThemeEntriesErrorSetsHintNotPanic(t *testing.T) {
	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)
	cat := &stubThemeCatalog{entriesErr: errors.New("boom")}

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted, style.PlainResolver()).
		WithThemeCatalog(cat, "", false)

	updated, _ := root.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	root = updated.(RootModel)

	var updated2 tea.Model
	assert.NotPanics(t, func() {
		updated2, _ = root.Update(keyMsg('T'))
	})
	root = updated2.(RootModel)
	assert.Contains(t, root.browser.hint, "boom")
	assert.False(t, root.browser.overlay.Active())
}
