package browser

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoad_ProducesMessageWithListingAndPath verifies that running the tea.Cmd
// returned by Load reports both the listing it read and the path it was asked
// to read, so callers can correlate a result with the request that produced it.
func TestLoad_ProducesMessageWithListingAndPath(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o600))

	cmd := Load(dir, false)
	require.NotNil(t, cmd)

	msg := cmd()
	loaded, ok := msg.(LoadedMsg)
	require.True(t, ok, "expected a LoadedMsg, got %T", msg)

	assert.Equal(t, dir, loaded.Path)
	require.NoError(t, loaded.Err)
	require.Len(t, loaded.Listing.Entries, 1)
	assert.Equal(t, "file.txt", loaded.Listing.Entries[0].Name)
}

// TestColumn_StaleResponseIsDiscarded is the most important test in this file: a
// listing that arrives for a path the user has already navigated away from must
// be dropped, not applied. Without this, a slow read for a path the column no
// longer cares about could clobber the directory currently being displayed.
func TestColumn_StaleResponseIsDiscarded(t *testing.T) {
	col := NewColumn(nil)

	dirA := t.TempDir()
	dirB := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dirB, "b.txt"), []byte("x"), 0o600))

	// request A, then navigate away to B before A's read arrives
	_ = col.Request(dirA, false)
	_ = col.Request(dirB, false)

	// the stale A result arrives late; it must be discarded
	applied := col.Apply(LoadedMsg{Path: dirA, Listing: Listing{Dir: dirA}})
	assert.False(t, applied, "stale response for an abandoned path must be discarded")
	assert.Empty(t, col.Listing.Dir, "column state must not be overwritten by the stale response")

	// the current B result arrives; it must be applied
	bListing, err := Read(dirB, false)
	require.NoError(t, err)
	applied = col.Apply(LoadedMsg{Path: dirB, Listing: bListing})
	assert.True(t, applied, "response for the currently requested path must be applied")
	assert.Equal(t, dirB, col.Listing.Dir)
	require.Len(t, col.Listing.Entries, 1)
	assert.Equal(t, "b.txt", col.Listing.Entries[0].Name)
}

// TestColumn_FailedLoadIsRenderableError verifies that a failing load reaches
// the column as an error the caller can render inline, rather than a panic or
// silent no-op.
func TestColumn_FailedLoadIsRenderableError(t *testing.T) {
	col := NewColumn(nil)
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	assert.NotPanics(t, func() {
		_ = col.Request(missing, false)
		listing, err := Read(missing, false)
		require.Error(t, err)
		applied := col.Apply(LoadedMsg{Path: missing, Listing: listing, Err: err})
		assert.True(t, applied)
	})

	require.Error(t, col.Err)
	assert.Empty(t, col.Listing.Entries)
}

// TestLoad_EndToEndThroughCmdReportsError runs the real tea.Cmd (not a
// hand-built LoadedMsg) against an unreadable path, confirming the command
// itself surfaces the error rather than panicking.
func TestLoad_EndToEndThroughCmdReportsError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	var msg tea.Msg
	assert.NotPanics(t, func() {
		msg = Load(missing, false)()
	})

	loaded, ok := msg.(LoadedMsg)
	require.True(t, ok)
	assert.Equal(t, missing, loaded.Path)
	assert.Error(t, loaded.Err)
}

// fakeClock is a controllable Clock for testing the pending-placeholder
// threshold without a real wall-clock sleep.
type fakeClock struct {
	now time.Time
}

func (f *fakeClock) advance(d time.Duration) {
	f.now = f.now.Add(d)
}

func (f *fakeClock) Clock() time.Time {
	return f.now
}

// TestColumn_PendingBecomesTrueAfterThreshold verifies the ~150ms placeholder
// threshold using an injected fake clock, never a real sleep: a request is not
// pending immediately, but becomes pending once the fake clock advances past
// the threshold while the load is still outstanding.
func TestColumn_PendingBecomesTrueAfterThreshold(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	col := NewColumn(clock.Clock)

	dir := t.TempDir()
	_ = col.Request(dir, false)

	assert.False(t, col.Pending(), "must not be pending immediately after the request")

	clock.advance(100 * time.Millisecond)
	assert.False(t, col.Pending(), "must not be pending before the threshold elapses")

	clock.advance(60 * time.Millisecond) // total 160ms, past the ~150ms threshold
	assert.True(t, col.Pending(), "must be pending once the threshold elapses")
}

// TestColumn_PendingClearsOnceApplied verifies the placeholder condition goes
// away once a matching result has been applied, even if the clock keeps moving.
func TestColumn_PendingClearsOnceApplied(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	col := NewColumn(clock.Clock)

	dir := t.TempDir()
	_ = col.Request(dir, false)
	clock.advance(200 * time.Millisecond)
	require.True(t, col.Pending())

	listing, err := Read(dir, false)
	require.NoError(t, err)
	require.True(t, col.Apply(LoadedMsg{Path: dir, Listing: listing}))

	clock.advance(time.Second)
	assert.False(t, col.Pending(), "must not be pending once the load has completed")
}
