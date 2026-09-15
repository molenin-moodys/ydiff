package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigStore_PersistBrowserWidths_insertsWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte("wrap = true\n"), 0o600))

	cs := &configStore{path: path}
	require.NoError(t, cs.PersistBrowserWidths([3]int{15, 35, 50}))

	data, err := os.ReadFile(path) //nolint:gosec // test
	require.NoError(t, err)
	assert.Contains(t, string(data), "browser-widths = 15,35,50")
	assert.Contains(t, string(data), "wrap = true")
}

func TestConfigStore_PersistBrowserWidths_replacesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte("browser-widths = 15,35,50\nwrap = true\n"), 0o600))

	cs := &configStore{path: path}
	require.NoError(t, cs.PersistBrowserWidths([3]int{10, 20, 70}))

	data, err := os.ReadFile(path) //nolint:gosec // test
	require.NoError(t, err)
	assert.Contains(t, string(data), "browser-widths = 10,20,70")
	assert.NotContains(t, string(data), "15,35,50")
	assert.Contains(t, string(data), "wrap = true")
}

func TestConfigStore_PersistBrowserWidths_roundTripsThroughParseBrowserWidths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	cs := &configStore{path: path}
	want := [3]int{12, 40, 48}
	require.NoError(t, cs.PersistBrowserWidths(want))

	data, err := os.ReadFile(path) //nolint:gosec // test
	require.NoError(t, err)

	const prefix = "browser-widths = "
	s := string(data)
	idx := strings.Index(s, prefix)
	require.GreaterOrEqual(t, idx, 0, "expected %q in %q", prefix, s)
	value := s[idx+len(prefix):]
	if nl := strings.IndexByte(value, '\n'); nl >= 0 {
		value = value[:nl]
	}

	got, err := parseBrowserWidths(value)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestConfigStore_PersistBrowserWidths_emptyPathIsNoOp(t *testing.T) {
	cs := &configStore{path: ""}
	require.NoError(t, cs.PersistBrowserWidths([3]int{15, 35, 50}))
}
