package browser

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.Mkdir(path, 0o755))
}

func mustWriteFile(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte("data\n"), 0o600))
}

func TestRead_DirectoriesBeforeFilesCaseInsensitive(t *testing.T) {
	dir := t.TempDir()

	// dirs: case-insensitive order should be apple, Zeta (not ASCII order, where
	// uppercase Z sorts before lowercase a).
	mustMkdir(t, filepath.Join(dir, "Zeta"))
	mustMkdir(t, filepath.Join(dir, "apple"))

	// files: case-insensitive order should be Banana.txt, cherry.txt.
	mustWriteFile(t, filepath.Join(dir, "cherry.txt"))
	mustWriteFile(t, filepath.Join(dir, "Banana.txt"))

	listing, err := Read(dir, false)
	require.NoError(t, err)
	require.Len(t, listing.Entries, 4)

	names := make([]string, len(listing.Entries))
	for i, e := range listing.Entries {
		names[i] = e.Name
	}
	assert.Equal(t, []string{"apple", "Zeta", "Banana.txt", "cherry.txt"}, names)

	assert.True(t, listing.Entries[0].IsDir)
	assert.True(t, listing.Entries[1].IsDir)
	assert.False(t, listing.Entries[2].IsDir)
	assert.False(t, listing.Entries[3].IsDir)
}

func TestRead_HiddenEntriesExcludedByDefault(t *testing.T) {
	dir := t.TempDir()

	mustWriteFile(t, filepath.Join(dir, "visible.txt"))
	mustWriteFile(t, filepath.Join(dir, ".hidden"))

	listing, err := Read(dir, false)
	require.NoError(t, err)
	require.Len(t, listing.Entries, 1)
	assert.Equal(t, "visible.txt", listing.Entries[0].Name)

	listing, err = Read(dir, true)
	require.NoError(t, err)
	require.Len(t, listing.Entries, 2)
	names := []string{listing.Entries[0].Name, listing.Entries[1].Name}
	assert.Contains(t, names, ".hidden")
	assert.Contains(t, names, "visible.txt")
}

func TestRead_SymlinkToDirectoryIsEnterable(t *testing.T) {
	dir := t.TempDir()

	target := filepath.Join(dir, "target")
	mustMkdir(t, target)

	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(target, link))

	listing, err := Read(dir, false)
	require.NoError(t, err)
	require.Len(t, listing.Entries, 2)

	var linkEntry Entry
	found := false
	for _, e := range listing.Entries {
		if e.Name == "link" {
			linkEntry = e
			found = true
		}
	}
	require.True(t, found, "expected to find the symlink entry")

	assert.True(t, linkEntry.IsSymlink)
	assert.True(t, linkEntry.IsDir)
	assert.False(t, linkEntry.SymlinkBroken)
	assert.True(t, linkEntry.Enterable())
}

func TestRead_BrokenSymlinkIsFlaggedAndNotEnterable(t *testing.T) {
	dir := t.TempDir()

	link := filepath.Join(dir, "broken")
	require.NoError(t, os.Symlink(filepath.Join(dir, "does-not-exist"), link))

	listing, err := Read(dir, false)
	require.NoError(t, err)
	require.Len(t, listing.Entries, 1)

	entry := listing.Entries[0]
	assert.Equal(t, "broken", entry.Name)
	assert.True(t, entry.IsSymlink)
	assert.True(t, entry.SymlinkBroken)
	assert.False(t, entry.IsDir)
	assert.False(t, entry.Enterable())
}

func TestRead_UnreadableDirectoryReturnsErrorNotPanic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits do not restrict directory reads on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores permission bits")
	}

	parent := t.TempDir()
	locked := filepath.Join(parent, "locked")
	mustMkdir(t, locked)

	require.NoError(t, os.Chmod(locked, 0o000))
	// restore permissions so t.TempDir() cleanup can remove the directory afterward.
	t.Cleanup(func() {
		_ = os.Chmod(locked, 0o700)
	})

	assert.NotPanics(t, func() {
		listing, err := Read(locked, false)
		assert.Error(t, err)
		assert.Empty(t, listing.Entries)
	})
}
