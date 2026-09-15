package gitstate

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolvedPath evaluates symlinks in p, mirroring what `git rev-parse --show-toplevel`
// does internally. On macOS, t.TempDir() lives under /var, which is itself a symlink to
// /private/var, so a literal comparison against the raw temp dir would fail even for a
// correct implementation.
func resolvedPath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	require.NoError(t, err)
	return r
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init", "--quiet", dir)
	require.NoError(t, cmd.Run())

	// a commit is required for worktree/submodule setups later; harmless for plain repos
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o600))
	run("add", "README.md")
	run("commit", "--quiet", "-m", "initial commit")
}

func TestResolve_NestedDirectory(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	nested := filepath.Join(dir, "a", "b", "c")
	require.NoError(t, os.MkdirAll(nested, 0o750))

	repo, err := Resolve(nested)
	require.NoError(t, err)
	assert.Equal(t, resolvedPath(t, dir), repo.Root)
}

func TestResolve_RootReportedCorrectly(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	repo, err := Resolve(dir)
	require.NoError(t, err)
	assert.Equal(t, resolvedPath(t, dir), repo.Root)
}

func TestResolve_NotARepository(t *testing.T) {
	dir := t.TempDir()

	repo, err := Resolve(dir)
	require.Error(t, err)
	assert.Nil(t, repo)
	assert.True(t, errors.Is(err, ErrNoRepository), "expected ErrNoRepository, got %v", err)
}

func TestResolve_VanishedDirectory(t *testing.T) {
	parent := t.TempDir()
	gone := filepath.Join(parent, "gone")
	require.NoError(t, os.Mkdir(gone, 0o750))
	require.NoError(t, os.Remove(gone))

	assert.NotPanics(t, func() {
		repo, err := Resolve(gone)
		require.Error(t, err)
		assert.Nil(t, repo)
		assert.False(t, errors.Is(err, ErrNoRepository), "vanished directory should not be reported as ErrNoRepository")
	})
}

func TestResolve_Worktree(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	worktreeDir := filepath.Join(t.TempDir(), "wt")
	cmd := exec.Command("git", "worktree", "add", "--quiet", "-b", "feature", worktreeDir)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git worktree add: %s", out)

	// .git in a worktree is a file, not a directory
	info, err := os.Stat(filepath.Join(worktreeDir, ".git"))
	require.NoError(t, err)
	assert.False(t, info.IsDir())

	repo, err := Resolve(worktreeDir)
	require.NoError(t, err)
	assert.Equal(t, resolvedPath(t, worktreeDir), repo.Root)
}

func TestResolve_Submodule(t *testing.T) {
	outer := t.TempDir()
	initRepo(t, outer)

	inner := t.TempDir()
	initRepo(t, inner)

	cmd := exec.Command("git", "-c", "protocol.file.allow=always",
		"submodule", "add", "--quiet", inner, "sub")
	cmd.Dir = outer
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git submodule add: %s", out)

	subDir := filepath.Join(outer, "sub")

	// .git in a submodule checkout is a file, not a directory
	info, err := os.Stat(filepath.Join(subDir, ".git"))
	require.NoError(t, err)
	assert.False(t, info.IsDir())

	repo, err := Resolve(subDir)
	require.NoError(t, err)
	assert.Equal(t, resolvedPath(t, subDir), repo.Root)
}
