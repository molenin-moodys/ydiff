package gitstate

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newBareRepo creates a bare git repository under t.TempDir() to act as an "origin"
// remote for the resolution tests below.
func newBareRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "--bare", "--quiet", dir)
	require.NoError(t, cmd.Run())
	return dir
}

// addOriginWithBranch adds bareDir as the "origin" remote of dir, pushes the current
// HEAD of dir to refs/heads/<branchName> on that remote, and fetches so that
// refs/remotes/origin/<branchName> exists locally. It does not touch origin/HEAD.
func addOriginWithBranch(t *testing.T, dir, bareDir, branchName string) {
	t.Helper()
	gitRun(t, dir, "remote", "add", "origin", bareDir)
	gitRun(t, dir, "push", "-q", "origin", "HEAD:refs/heads/"+branchName)
	gitRun(t, dir, "fetch", "-q", "origin")
}

// setOriginHead points the local refs/remotes/origin/HEAD symbolic ref at
// refs/remotes/origin/<branchName>, exactly as `git remote set-head origin -a` would
// have done had the bare remote's own HEAD been resolvable.
func setOriginHead(t *testing.T, dir, branchName string) {
	t.Helper()
	gitRun(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/"+branchName)
}

func TestResolveBase_ExplicitOverrideWins(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	bareDir := newBareRepo(t)
	addOriginWithBranch(t, dir, bareDir, "trunk")
	setOriginHead(t, dir, "trunk")

	repo, err := Resolve(dir)
	require.NoError(t, err)

	got, err := ResolveBase(repo, "some-explicit-base")
	require.NoError(t, err)
	assert.Equal(t, "some-explicit-base", got)
}

func TestResolveBase_OriginHead(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	bareDir := newBareRepo(t)
	// a distinctive branch name proves resolution actually came from origin/HEAD,
	// not from an accidental match against the origin/main or origin/master fallback.
	addOriginWithBranch(t, dir, bareDir, "trunk")
	setOriginHead(t, dir, "trunk")

	repo, err := Resolve(dir)
	require.NoError(t, err)

	got, err := ResolveBase(repo, "")
	require.NoError(t, err)
	assert.Equal(t, "origin/trunk", got)
}

func TestResolveBase_FallsBackToOriginMain(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	bareDir := newBareRepo(t)
	addOriginWithBranch(t, dir, bareDir, "main")
	// deliberately no origin/HEAD set

	repo, err := Resolve(dir)
	require.NoError(t, err)

	got, err := ResolveBase(repo, "")
	require.NoError(t, err)
	assert.Equal(t, "origin/main", got)
}

func TestResolveBase_FallsBackToOriginMaster(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	bareDir := newBareRepo(t)
	addOriginWithBranch(t, dir, bareDir, "master")
	// deliberately no origin/HEAD and no origin/main

	repo, err := Resolve(dir)
	require.NoError(t, err)

	got, err := ResolveBase(repo, "")
	require.NoError(t, err)
	assert.Equal(t, "origin/master", got)
}

func TestResolveBase_NoneResolve(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	bareDir := newBareRepo(t)
	// origin exists and has a remote-tracking branch, but neither origin/HEAD nor
	// origin/main nor origin/master is present.
	addOriginWithBranch(t, dir, bareDir, "develop")

	repo, err := Resolve(dir)
	require.NoError(t, err)

	got, err := ResolveBase(repo, "")
	require.ErrorIs(t, err, ErrNoBase)
	assert.Empty(t, got)
}

func TestResolveBase_NoRemote(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	repo, err := Resolve(dir)
	require.NoError(t, err)

	got, err := ResolveBase(repo, "")
	require.ErrorIs(t, err, ErrNoBase)
	assert.Empty(t, got)
	assert.True(t, errors.Is(err, ErrNoBase))
}
