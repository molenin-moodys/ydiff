package gitstate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitRun runs a git command in dir, failing the test on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := c.CombinedOutput()
	require.NoErrorf(t, err, "git %v: %s", args, out)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

func TestUncommittedStatus_CleanRepo(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	repo, err := Resolve(dir)
	require.NoError(t, err)

	files, err := UncommittedStatus(repo)
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestUncommittedStatus_Modified(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	writeFile(t, dir, "README.md", "hello\nworld\n")

	repo, err := Resolve(dir)
	require.NoError(t, err)

	files, err := UncommittedStatus(repo)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "README.md", files[0].Path)
	assert.Empty(t, files[0].OldPath)
	assert.Equal(t, StatusModified, files[0].Status)
}

func TestUncommittedStatus_Added(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	writeFile(t, dir, "new.txt", "content\n")
	gitRun(t, dir, "add", "new.txt")

	repo, err := Resolve(dir)
	require.NoError(t, err)

	files, err := UncommittedStatus(repo)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "new.txt", files[0].Path)
	assert.Equal(t, StatusAdded, files[0].Status)
}

func TestUncommittedStatus_Deleted(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	require.NoError(t, os.Remove(filepath.Join(dir, "README.md")))

	repo, err := Resolve(dir)
	require.NoError(t, err)

	files, err := UncommittedStatus(repo)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "README.md", files[0].Path)
	assert.Equal(t, StatusDeleted, files[0].Status)
}

func TestUncommittedStatus_Renamed(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	// leave the content untouched so the rename is a perfect match: git's default
	// rename detection in `git status` only fires above a similarity threshold.
	gitRun(t, dir, "mv", "README.md", "RENAMED.md")

	repo, err := Resolve(dir)
	require.NoError(t, err)

	files, err := UncommittedStatus(repo)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "RENAMED.md", files[0].Path)
	assert.Equal(t, "README.md", files[0].OldPath)
	assert.Equal(t, StatusRenamed, files[0].Status)
}

func TestUncommittedStatus_Untracked(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	writeFile(t, dir, "untracked.txt", "content\n")

	repo, err := Resolve(dir)
	require.NoError(t, err)

	files, err := UncommittedStatus(repo)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "untracked.txt", files[0].Path)
	assert.Equal(t, StatusUntracked, files[0].Status)
}

func TestUncommittedStatus_StagedPlusModified(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	writeFile(t, dir, "README.md", "staged change\n")
	gitRun(t, dir, "add", "README.md")
	writeFile(t, dir, "README.md", "staged change\nplus a further worktree edit\n")

	repo, err := Resolve(dir)
	require.NoError(t, err)

	files, err := UncommittedStatus(repo)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "README.md", files[0].Path)
	// staged (index) change is 'M' and worktree change is also 'M' here; the
	// reported status should reflect that the file is modified either way.
	assert.Equal(t, StatusModified, files[0].Status)
}

func TestUncommittedStatus_PathWithSpacesAndUnicode(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	name := "a file with spaces and üñïçødé.txt"
	writeFile(t, dir, name, "content\n")

	repo, err := Resolve(dir)
	require.NoError(t, err)

	files, err := UncommittedStatus(repo)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, name, files[0].Path)
	assert.Equal(t, StatusUntracked, files[0].Status)
}

func TestParsePorcelainV2_MalformedLine(t *testing.T) {
	_, err := parsePorcelainV2([]byte("1 M.\x00"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "porcelain")
}

func TestParsePorcelainV2_UnknownRecordType(t *testing.T) {
	_, err := parsePorcelainV2([]byte("x some garbage line\x00"))
	require.Error(t, err)
}
