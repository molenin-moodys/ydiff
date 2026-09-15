package ui

import (
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

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted)
	require.Equal(t, ScreenBrowser, root.screen)

	updated, _ := root.Update(keyMsg('d'))
	root = updated.(RootModel)

	assert.Equal(t, ScreenReview, root.screen)
}

func TestRootModel_BrowserReview_QInReviewPopsToBrowser(t *testing.T) {
	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel(nil, nil)

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted)

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

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted)

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

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted)

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

	root := NewRootBrowser(nav, keymap.Default(), review, cache, gitstate.ScopeUncommitted)

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
