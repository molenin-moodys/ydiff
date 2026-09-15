package gitstate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchScope_ForkPointAsymmetry(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	gitRun(t, dir, "branch", "-m", "main") // give the default branch a stable, known name

	// branch off the fork point, then commit something only the branch has
	gitRun(t, dir, "checkout", "-q", "-b", "feature")
	writeFile(t, dir, "feature-only.txt", "feature work\n")
	gitRun(t, dir, "add", "feature-only.txt")
	gitRun(t, dir, "commit", "-q", "-m", "add feature-only file")

	// go back to main and commit something *after* the fork point that the branch
	// never picked up
	gitRun(t, dir, "checkout", "-q", "main")
	writeFile(t, dir, "main-only.txt", "main work after fork\n")
	gitRun(t, dir, "add", "main-only.txt")
	gitRun(t, dir, "commit", "-q", "-m", "add main-only file")

	gitRun(t, dir, "checkout", "-q", "feature")

	repo, err := Resolve(dir)
	require.NoError(t, err)

	files, err := BranchScope(repo, "main")
	require.NoError(t, err)

	var paths []string
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	assert.Contains(t, paths, "feature-only.txt")
	assert.NotContains(t, paths, "main-only.txt")
}

func TestBranchScope_RenameRecordsBothPaths(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	gitRun(t, dir, "branch", "-m", "main")

	// a large-enough, high-similarity file so git's default rename detection actually
	// fires (a tiny file falls below the similarity threshold and shows as D+A instead)
	var content string
	for i := 0; i < 50; i++ {
		content += "line\n"
	}
	writeFile(t, dir, "old-name.txt", content)
	gitRun(t, dir, "add", "old-name.txt")
	gitRun(t, dir, "commit", "-q", "-m", "add old-name.txt")

	gitRun(t, dir, "checkout", "-q", "-b", "feature")
	gitRun(t, dir, "mv", "old-name.txt", "new-name.txt")
	writeFile(t, dir, "new-name.txt", content+"one more line\n")
	gitRun(t, dir, "commit", "-q", "-am", "rename old-name.txt to new-name.txt")

	repo, err := Resolve(dir)
	require.NoError(t, err)

	files, err := BranchScope(repo, "main")
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, StatusRenamed, files[0].Status)
	assert.Equal(t, "old-name.txt", files[0].OldPath)
	assert.Equal(t, "new-name.txt", files[0].Path)
}

func TestBranchScope_NoCommitsSinceForkPoint(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	gitRun(t, dir, "branch", "-m", "main")
	gitRun(t, dir, "checkout", "-q", "-b", "feature")
	// no commits made on feature since branching off main

	repo, err := Resolve(dir)
	require.NoError(t, err)

	files, err := BranchScope(repo, "main")
	require.NoError(t, err)
	assert.Empty(t, files)
	assert.NotNil(t, files)
}

func TestBranchScope_UnresolvedBaseYieldsErrNoBase(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	// no remote at all, so ResolveBase has nothing to resolve to and no explicit
	// override is given either

	repo, err := Resolve(dir)
	require.NoError(t, err)

	files, err := BranchScope(repo, "")
	require.ErrorIs(t, err, ErrNoBase)
	assert.Nil(t, files)
}
