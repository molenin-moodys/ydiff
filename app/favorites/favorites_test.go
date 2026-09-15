package favorites

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestList_MissingFileIsEmptyNotError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist")
	got, err := New(path).List()
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestList_SortsAlphabeticallyCaseInsensitive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites")
	data := "/home/user/Zebra\n/home/user/apple\n/home/user/Banana\n"
	require.NoError(t, os.WriteFile(path, []byte(data), 0o600))

	got, err := New(path).List()
	require.NoError(t, err)
	assert.Equal(t, []string{"/home/user/apple", "/home/user/Banana", "/home/user/Zebra"}, got)
}

func TestList_DeduplicatesAndTrims(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites")
	data := "  /a  \n/a\n\n/b\n/a\n"
	require.NoError(t, os.WriteFile(path, []byte(data), 0o600))

	got, err := New(path).List()
	require.NoError(t, err)
	assert.Equal(t, []string{"/a", "/b"}, got)
}

func TestToggle_AddsWhenAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites")
	svc := New(path)

	added, err := svc.Toggle("/repo/one")
	require.NoError(t, err)
	assert.True(t, added)

	got, err := svc.List()
	require.NoError(t, err)
	assert.Equal(t, []string{"/repo/one"}, got)
}

func TestToggle_RemovesWhenPresent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites")
	svc := New(path)

	_, err := svc.Toggle("/repo/one")
	require.NoError(t, err)

	added, err := svc.Toggle("/repo/one")
	require.NoError(t, err)
	assert.False(t, added)

	got, err := svc.List()
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestToggle_PersistsAcrossServiceInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites")

	_, err := New(path).Toggle("/repo/one")
	require.NoError(t, err)

	got, err := New(path).List()
	require.NoError(t, err)
	assert.Equal(t, []string{"/repo/one"}, got)
}

func TestRemove_AbsentEntryIsNoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites")
	svc := New(path)
	_, err := svc.Toggle("/repo/one")
	require.NoError(t, err)

	require.NoError(t, svc.Remove("/repo/does-not-exist"))

	got, err := svc.List()
	require.NoError(t, err)
	assert.Equal(t, []string{"/repo/one"}, got)
}

func TestRemove_DeletesPresentEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites")
	svc := New(path)
	_, err := svc.Toggle("/repo/one")
	require.NoError(t, err)
	_, err = svc.Toggle("/repo/two")
	require.NoError(t, err)

	require.NoError(t, svc.Remove("/repo/one"))

	got, err := svc.List()
	require.NoError(t, err)
	assert.Equal(t, []string{"/repo/two"}, got)
}

func TestIsFavorite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites")
	svc := New(path)
	_, err := svc.Toggle("/repo/one")
	require.NoError(t, err)

	yes, err := svc.IsFavorite("/repo/one")
	require.NoError(t, err)
	assert.True(t, yes)

	no, err := svc.IsFavorite("/repo/other")
	require.NoError(t, err)
	assert.False(t, no)
}

func TestNew_EmptyPathDefersToDefaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	svc := New("")
	_, err := svc.Toggle("/repo/one")
	require.NoError(t, err)

	want := filepath.Join(home, ".config", "ydiff", "favorites")
	data, err := os.ReadFile(want) //nolint:gosec // test-only path from t.TempDir()
	require.NoError(t, err)
	assert.Contains(t, string(data), "/repo/one")
}

func TestWrite_CreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "favorites")
	svc := New(path)

	_, err := svc.Toggle("/repo/one")
	require.NoError(t, err)

	_, err = os.Stat(path)
	require.NoError(t, err)
}
