package main

import (
	"os"
	"path/filepath"
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

// TestConfigStore_PersistBrowserWidths_roundTripsThroughParseArgs verifies the
// feature's actual premise (plan: "a browser-widths line written into the
// config file survives p.ParseArgs") end to end through go-flags itself,
// not just by feeding the raw substring to parseBrowserWidths — mirroring
// TestPatchConfigTheme_testdataRoundTrip's precedent for the "theme" key.
// Exercises both the headerless insert path (no config file yet) and the
// replace-existing path.
func TestConfigStore_PersistBrowserWidths_roundTripsThroughParseArgs(t *testing.T) {
	t.Run("insert into a fresh config file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config")
		cs := &configStore{path: path}
		want := [3]int{12, 40, 48}
		require.NoError(t, cs.PersistBrowserWidths(want))

		opts, err := parseArgs([]string{"--config", path})
		require.NoError(t, err, "patched file must parse without a go-flags error")
		assert.Equal(t, want, opts.ResolvedBrowserWidths())
	})

	t.Run("replace an existing value", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config")
		require.NoError(t, os.WriteFile(path, []byte("browser-widths = 15,35,50\nwrap = true\n"), 0o600))
		cs := &configStore{path: path}
		want := [3]int{10, 20, 70}
		require.NoError(t, cs.PersistBrowserWidths(want))

		opts, err := parseArgs([]string{"--config", path})
		require.NoError(t, err, "patched file must parse without a go-flags error")
		assert.Equal(t, want, opts.ResolvedBrowserWidths())
	})
}

func TestConfigStore_PersistBrowserWidths_emptyPathIsNoOp(t *testing.T) {
	cs := &configStore{path: ""}
	require.NoError(t, cs.PersistBrowserWidths([3]int{15, 35, 50}))
}
