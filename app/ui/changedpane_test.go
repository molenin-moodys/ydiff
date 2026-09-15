package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/molenin-moodys/ydiff/app/gitstate"
	"github.com/molenin-moodys/ydiff/app/ui/style"
)

func TestChangedPane_OutsideRepository_RendersOwnMessage(t *testing.T) {
	dir := t.TempDir() // no .git anywhere above a temp dir in CI

	cache := gitstate.NewCache(NewGitLoader(""))
	p := newChangedPane(cache, gitstate.ScopeUncommitted)

	cmd := p.Request(dir)
	require.NotNil(t, cmd)
	msgs := drainBatch(cmd)
	require.Len(t, msgs, 1)
	msg, ok := msgs[0].(GitLoadedMsg)
	require.True(t, ok)

	require.True(t, p.Apply(msg))
	assert.Equal(t, changedPaneNoRepo, p.status)
	assert.Equal(t, 0, p.count())

	rendered := p.Render(style.PlainResolver(), 80, 10, false)
	assert.Contains(t, rendered, changedPaneMsgNoRepo)
	assert.NotContains(t, rendered, changedPaneMsgNoBase)
}

func TestChangedPane_BranchScopeBaseNotFound_RendersOwnMessage(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir) // a local-only repo: no origin, so ResolveBase always fails

	cache := gitstate.NewCache(NewGitLoader(""))
	p := newChangedPane(cache, gitstate.ScopeBranch)

	cmd := p.Request(dir)
	require.NotNil(t, cmd)
	msgs := drainBatch(cmd)
	require.Len(t, msgs, 1)
	msg, ok := msgs[0].(GitLoadedMsg)
	require.True(t, ok)
	require.ErrorIs(t, msg.Err, gitstate.ErrNoBase)

	require.True(t, p.Apply(msg))
	assert.Equal(t, changedPaneBaseNotFound, p.status)
	assert.Equal(t, 0, p.count())

	rendered := p.Render(style.PlainResolver(), 80, 10, false)
	assert.Contains(t, rendered, changedPaneMsgNoBase)
	assert.NotContains(t, rendered, changedPaneMsgNoRepo)
	assert.NotContains(t, rendered, changedPaneMsgEmpty)
}

func TestChangedPane_NothingChanged_RendersDistinctEmptyMessage(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	cache := gitstate.NewCache(NewGitLoader(""))
	p := newChangedPane(cache, gitstate.ScopeUncommitted)

	cmd := p.Request(dir)
	msgs := drainBatch(cmd)
	msg := msgs[0].(GitLoadedMsg) //nolint:errcheck // asserted above in sibling tests
	require.NoError(t, msg.Err)
	require.True(t, p.Apply(msg))

	assert.Equal(t, changedPaneOK, p.status)
	assert.Empty(t, p.files)

	rendered := p.Render(style.PlainResolver(), 80, 10, false)
	assert.Contains(t, rendered, changedPaneMsgEmpty)
}

func TestChangedPane_Apply_DiscardsStaleResponse(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	initGitRepo(t, dirA)
	initGitRepo(t, dirB)

	cache := gitstate.NewCache(NewGitLoader(""))
	p := newChangedPane(cache, gitstate.ScopeUncommitted)

	cmdA := p.Request(dirA)
	// the user navigates to dirB before dirA's load arrives
	p.Request(dirB)

	msgsA := drainBatch(cmdA)
	msgA := msgsA[0].(GitLoadedMsg) //nolint:errcheck

	accepted := p.Apply(msgA)
	assert.False(t, accepted, "a result answering a request the pane has since moved on from must be discarded")
	assert.True(t, p.loading, "the pane must still be waiting for dirB's result")
	assert.Equal(t, dirB, p.requestedDir)
}

func TestChangedPane_Render_StatusBadgesThemed(t *testing.T) {
	p := newChangedPane(nil, gitstate.ScopeUncommitted)
	p.status = changedPaneOK
	p.files = []gitstate.ChangedFile{
		{Path: "added.txt", Status: gitstate.StatusAdded},
		{Path: "deleted.txt", Status: gitstate.StatusDeleted},
		{Path: "modified.txt", Status: gitstate.StatusModified},
		{Path: "renamed-new.txt", OldPath: "renamed-old.txt", Status: gitstate.StatusRenamed},
		{Path: "untracked.txt", Status: gitstate.StatusUntracked},
	}

	rendered := p.Render(style.PlainResolver(), 80, 10, false)
	lines := strings.Split(rendered, "\n")
	require.Len(t, lines, len(p.files))

	assert.True(t, strings.HasPrefix(lines[0], "A "))
	assert.Contains(t, lines[0], "added.txt")
	assert.True(t, strings.HasPrefix(lines[1], "D "))
	assert.Contains(t, lines[1], "deleted.txt")
	assert.True(t, strings.HasPrefix(lines[2], "M "))
	assert.Contains(t, lines[2], "modified.txt")
	assert.True(t, strings.HasPrefix(lines[3], "R "))
	assert.Contains(t, lines[3], "renamed-old.txt -> renamed-new.txt")
	assert.True(t, strings.HasPrefix(lines[4], "??"))
	assert.Contains(t, lines[4], "untracked.txt")
}

func TestChangedPane_RepoWide_NotNarrowedToCursorDirectory(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	// a *tracked* file changed inside sub, so `git status --untracked-files=normal`
	// reports its own path rather than collapsing the new directory to "sub/".
	require.NoError(t, os.WriteFile(filepath.Join(sub, "in-sub.txt"), []byte("x\n"), 0o600))
	trackedCmd := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		require.NoErrorf(t, err, "git %v: %s", args, out)
	}
	trackedCmd("add", "sub/in-sub.txt")
	trackedCmd("commit", "--quiet", "-m", "add sub/in-sub.txt",
		"--author=test <test@example.com>")
	require.NoError(t, os.WriteFile(filepath.Join(sub, "in-sub.txt"), []byte("x changed\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "at-root.txt"), []byte("y\n"), 0o600))

	cache := gitstate.NewCache(NewGitLoader(""))
	p := newChangedPane(cache, gitstate.ScopeUncommitted)

	// the browser's cursor stands inside "sub", not at the repository root
	cmd := p.Request(sub)
	msgs := drainBatch(cmd)
	msg := msgs[0].(GitLoadedMsg) //nolint:errcheck
	require.NoError(t, msg.Err)
	require.True(t, p.Apply(msg))

	var paths []string
	for _, f := range p.files {
		paths = append(paths, f.Path)
	}
	assert.Contains(t, paths, "at-root.txt", "the pane must list changes outside the cursor's own directory")
	assert.Contains(t, paths, "sub/in-sub.txt")
}

func TestChangedPane_SelectedAndMatchPath(t *testing.T) {
	p := newChangedPane(nil, gitstate.ScopeUncommitted)
	p.status = changedPaneOK
	p.files = []gitstate.ChangedFile{
		{Path: "a.txt", Status: gitstate.StatusModified},
		{Path: "b.txt", Status: gitstate.StatusAdded},
	}

	path, ok := p.Selected()
	require.True(t, ok)
	assert.Equal(t, "a.txt", path)

	p.MoveCursor(1)
	path, ok = p.Selected()
	require.True(t, ok)
	assert.Equal(t, "b.txt", path)

	p.MoveCursor(1) // clamps at the last entry
	path, ok = p.Selected()
	require.True(t, ok)
	assert.Equal(t, "b.txt", path)

	matched, ok := p.MatchPath("a.txt")
	assert.True(t, ok)
	assert.Equal(t, "a.txt", matched)

	_, ok = p.MatchPath("unchanged.txt")
	assert.False(t, ok, "Enter on an unchanged file must be a no-op")
}

func TestChangedPane_MatchPath_FalseWhenNotOK(t *testing.T) {
	p := newChangedPane(nil, gitstate.ScopeUncommitted)
	p.status = changedPaneNoRepo
	p.files = []gitstate.ChangedFile{{Path: "a.txt", Status: gitstate.StatusModified}}

	_, ok := p.MatchPath("a.txt")
	assert.False(t, ok)

	_, ok = p.Selected()
	assert.False(t, ok)
}

func TestChangedPane_ToggleScope_SwitchesAndReloads(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	cache := gitstate.NewCache(NewGitLoader(""))
	p := newChangedPane(cache, gitstate.ScopeUncommitted)
	p.cursor = 3 // pretend a prior list had a cursor position

	cmd := p.ToggleScope(dir)
	assert.Equal(t, gitstate.ScopeBranch, p.scope)
	assert.Equal(t, 0, p.cursor, "toggling scope resets the cursor: the two scopes' lists are unrelated")
	require.NotNil(t, cmd)

	msgs := drainBatch(cmd)
	msg := msgs[0].(GitLoadedMsg) //nolint:errcheck
	assert.Equal(t, gitstate.ScopeBranch, msg.Scope)
}

func TestChangedPane_Refresh_InvalidatesCache(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	var loads int
	loader := func(root string, scope gitstate.Scope) ([]gitstate.ChangedFile, error) {
		loads++
		return gitstate.UncommittedStatus(&gitstate.Repo{Root: root})
	}
	cache := gitstate.NewCache(loader)
	p := newChangedPane(cache, gitstate.ScopeUncommitted)

	cmd := p.Request(dir)
	msg := drainBatch(cmd)[0].(GitLoadedMsg) //nolint:errcheck
	require.True(t, p.Apply(msg))
	assert.Equal(t, 1, loads)

	// a second, unrelated Request for the same dir/scope must hit the cache
	msg2 := drainBatch(p.Request(dir))[0].(GitLoadedMsg) //nolint:errcheck
	require.True(t, p.Apply(msg2))
	assert.Equal(t, 1, loads, "Get must serve the cached result, not reload")

	refreshCmd := p.Refresh(dir)
	msg3 := drainBatch(refreshCmd)[0].(GitLoadedMsg) //nolint:errcheck
	require.True(t, p.Apply(msg3))
	assert.Equal(t, 2, loads, "Refresh must invalidate the cache before reloading")
}

func TestChangedPane_NilCache_NeverPanics(t *testing.T) {
	p := newChangedPane(nil, gitstate.ScopeUncommitted)

	assert.NotPanics(t, func() {
		cmd := p.Request(t.TempDir())
		assert.Nil(t, cmd, "LoadGitState returns nil for a nil cache")
		p.ToggleScope(t.TempDir())
		p.Refresh(t.TempDir())
		p.Render(style.PlainResolver(), 40, 5, false)
	})
}
