package diff

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectVCS_Git(t *testing.T) {
	dir := t.TempDir()
	err := os.Mkdir(filepath.Join(dir, ".git"), 0o750)
	require.NoError(t, err)

	vcs, root := DetectVCS(dir)
	assert.Equal(t, VCSGit, vcs)
	assert.Equal(t, dir, root)
}

// TestDetectVCS_RealGitInit resolves a repository created with the actual `git init`
// binary, rather than a bare ".git" directory stand-in, so the positive case is
// verified against a real repository as the VCS interface will encounter in practice.
func TestDetectVCS_RealGitInit(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "--quiet", dir)
	require.NoError(t, cmd.Run())

	vcs, root := DetectVCS(dir)
	assert.Equal(t, VCSGit, vcs)
	assert.Equal(t, dir, root)
}

func TestDetectVCS_WalksUp(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o750))

	sub := filepath.Join(dir, "deep", "nested")
	require.NoError(t, os.MkdirAll(sub, 0o750))

	vcs, root := DetectVCS(sub)
	assert.Equal(t, VCSGit, vcs)
	assert.Equal(t, dir, root)
}

func TestDetectVCS_GitWorktree(t *testing.T) {
	// in git worktrees and submodules, .git is a file (not a directory)
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: /some/other/path\n"), 0o600)
	require.NoError(t, err)

	vcs, root := DetectVCS(dir)
	assert.Equal(t, VCSGit, vcs)
	assert.Equal(t, dir, root)
}

// TestDetectVCS_None covers the unsupported/absent VCS case: a directory with no
// .git anywhere up its tree resolves as VCSNone with an empty root, which is the
// distinguishable "no VCS here" signal that setupVCSRenderer turns into a clear
// error (see TestSetupVCSRenderer_NoVCSReportsClearError in renderer_setup_test.go).
func TestDetectVCS_None(t *testing.T) {
	dir := t.TempDir()
	vcs, root := DetectVCS(dir)
	assert.Equal(t, VCSNone, vcs)
	assert.Empty(t, root)
}
