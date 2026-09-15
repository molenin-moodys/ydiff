package browser

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- MatchName: case-insensitive substring matching, rune-range reporting ---

func TestMatchName_CaseInsensitiveSubstring(t *testing.T) {
	m, ok := MatchName("Handler.go", "handler")
	require.True(t, ok)
	assert.Equal(t, Match{Start: 0, End: 7}, m)
}

func TestMatchName_ReportsRuneRangeNotByteRange(t *testing.T) {
	// "résumé" has an accented rune whose UTF-8 encoding is 2 bytes, so a
	// byte-based range would be wrong for anything after it.
	m, ok := MatchName("résumé.md", "mé.md")
	require.True(t, ok)
	// rune indices: r-é-s-u-m-é-.-m-d -> "mé.md" starts at rune index 4
	assert.Equal(t, 4, m.Start)
	assert.Equal(t, 9, m.End)
}

func TestMatchName_NoMatch(t *testing.T) {
	_, ok := MatchName("main.go", "xyz")
	assert.False(t, ok)
}

func TestMatchName_EmptyQueryMatchesEverything(t *testing.T) {
	m, ok := MatchName("anything", "")
	require.True(t, ok)
	assert.Equal(t, Match{}, m)
}

func TestMatchName_MatchNotAtStart(t *testing.T) {
	m, ok := MatchName("app_browser_test.go", "browser")
	require.True(t, ok)
	assert.Equal(t, 4, m.Start)
	assert.Equal(t, 11, m.End)
}

// --- Filter lifecycle ---

func TestFilter_StartBeginsEditingWithEmptyQuery(t *testing.T) {
	var f Filter
	assert.False(t, f.Active())

	f.Start()
	assert.True(t, f.Active())
	assert.True(t, f.Editing())
	assert.Equal(t, "", f.Query())
}

func TestFilter_AppendAddsToQuery(t *testing.T) {
	var f Filter
	f.Start()
	f.Append('f')
	f.Append('o')
	f.Append('o')
	assert.Equal(t, "foo", f.Query())
}

func TestFilter_AppendIsNoOpWhenNotEditing(t *testing.T) {
	var f Filter
	f.Append('x') // filter never started
	assert.Equal(t, "", f.Query())
	assert.False(t, f.Active())
}

func TestFilter_BackspaceRemovesLastRune(t *testing.T) {
	var f Filter
	f.Start()
	f.Append('f')
	f.Append('o')
	f.Append('o')
	f.Backspace()
	assert.Equal(t, "fo", f.Query())
}

func TestFilter_BackspaceOnEmptyQueryIsNoOp(t *testing.T) {
	var f Filter
	f.Start()
	assert.NotPanics(t, func() { f.Backspace() })
	assert.Equal(t, "", f.Query())
}

func TestFilter_ApplyKeepsFilterActiveAndStopsEditing(t *testing.T) {
	var f Filter
	f.Start()
	f.Append('x')
	f.Apply()

	assert.True(t, f.Active(), "apply must keep the filter active")
	assert.False(t, f.Editing(), "apply must return to navigation, i.e. stop editing")
	assert.Equal(t, "x", f.Query())
}

func TestFilter_CancelClearsAndDeactivates(t *testing.T) {
	var f Filter
	f.Start()
	f.Append('x')
	f.Cancel()

	assert.False(t, f.Active())
	assert.Equal(t, "", f.Query())
}

func TestFilter_CancelAfterApplyAlsoClears(t *testing.T) {
	var f Filter
	f.Start()
	f.Append('x')
	f.Apply()
	f.Cancel()

	assert.False(t, f.Active())
	assert.Equal(t, "", f.Query())
}

// --- Nav integration: reset-on-navigation, narrowing, cursor safety ---

func TestNav_FilterAppliesOnlyToMiddleColumn(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "app"))
	mustMkdir(t, filepath.Join(root, "app", "foo"))
	mustMkdir(t, filepath.Join(root, "app", "bar"))
	mustMkdir(t, filepath.Join(root, "zzz-sibling"))

	nav, cmd := NewNav(filepath.Join(root, "app"), false, nil)
	runAndApply(t, nav, cmd)

	require.Len(t, nav.Parent().Listing.Entries, 2, "parent column lists app's siblings before filtering")

	nav.FilterStart()
	nav.FilterAppend('f')
	nav.FilterAppend('o')
	nav.FilterAppend('o')

	// middle column (current) is narrowed down to the single match
	visible := nav.VisibleEntries()
	require.Len(t, visible, 1)
	assert.Equal(t, "foo", visible[0].Name)

	// the parent column listing is untouched by the filter
	assert.Len(t, nav.Parent().Listing.Entries, 2, "parent column must be unaffected by the middle-column filter")
}

func TestNav_DirectoryChangeDropsFilter_OnEnter(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "app"))
	mustMkdir(t, filepath.Join(root, "app", "inner"))

	nav, cmd := NewNav(root, false, nil)
	runAndApply(t, nav, cmd)

	nav.FilterStart()
	nav.FilterAppend('a')
	require.True(t, nav.Filter().Active())

	nav.SetCursor(indexOf(t, nav.Current(), "app"))
	runAndApply(t, nav, nav.Enter())

	assert.False(t, nav.Filter().Active(), "entering a directory must drop the filter")
	assert.Equal(t, "", nav.Filter().Query())
}

func TestNav_DirectoryChangeDropsFilter_OnUp(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "app"))

	nav, cmd := NewNav(root, false, nil)
	runAndApply(t, nav, cmd)

	nav.SetCursor(indexOf(t, nav.Current(), "app"))
	runAndApply(t, nav, nav.Enter())
	require.Equal(t, filepath.Join(root, "app"), nav.Path())

	nav.FilterStart()
	nav.FilterAppend('x')
	require.True(t, nav.Filter().Active())

	runAndApply(t, nav, nav.Up())

	assert.False(t, nav.Filter().Active(), "going up must drop the filter too — either direction")
	assert.Equal(t, "", nav.Filter().Query())
}

func TestNav_FilterMatchingNothingLeavesEmptyValidListing(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "app"))
	mustMkdir(t, filepath.Join(root, "docs"))

	nav, cmd := NewNav(root, false, nil)
	runAndApply(t, nav, cmd)

	nav.FilterStart()
	for _, r := range "zzzznomatch" {
		nav.FilterAppend(r)
	}

	var visible []VisibleEntry
	assert.NotPanics(t, func() { visible = nav.VisibleEntries() })
	assert.Empty(t, visible)
	assert.Equal(t, 0, nav.Cursor(), "cursor must be a valid index (0) into the empty listing")

	// entering with no selection is a safe no-op, not a panic
	assert.NotPanics(t, func() {
		cmd := nav.Enter()
		assert.Nil(t, cmd)
	})
}

func TestNav_CursorClampsWhenFilterNarrowsListBeneathIt(t *testing.T) {
	root := t.TempDir()
	// five siblings so the cursor can start deep in the list
	mustMkdir(t, filepath.Join(root, "alpha"))
	mustMkdir(t, filepath.Join(root, "bravo"))
	mustMkdir(t, filepath.Join(root, "charlie"))
	mustMkdir(t, filepath.Join(root, "delta"))
	mustMkdir(t, filepath.Join(root, "echo"))

	nav, cmd := NewNav(root, false, nil)
	runAndApply(t, nav, cmd)

	lastIdx := len(nav.Current().Listing.Entries) - 1
	nav.SetCursor(lastIdx)
	require.Equal(t, lastIdx, nav.Cursor())

	nav.FilterStart()
	// only "alpha" contains "al" among the five directories
	nav.FilterAppend('a')
	nav.FilterAppend('l')

	visible := nav.VisibleEntries()
	require.Len(t, visible, 1)
	assert.Less(t, nav.Cursor(), len(visible), "cursor must be within the narrowed list's bounds")
	assert.GreaterOrEqual(t, nav.Cursor(), 0)
}

func TestNav_FilterAppendNarrowsLiveAsYouType(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "apple"))
	mustMkdir(t, filepath.Join(root, "banana"))
	mustMkdir(t, filepath.Join(root, "apricot"))

	nav, cmd := NewNav(root, false, nil)
	runAndApply(t, nav, cmd)

	nav.FilterStart()
	assert.Len(t, nav.VisibleEntries(), 3, "no query yet: everything visible")

	nav.FilterAppend('a')
	assert.Len(t, nav.VisibleEntries(), 3, "apple, apricot and banana all contain 'a' somewhere")

	nav.FilterAppend('p')
	visible := nav.VisibleEntries()
	names := make([]string, len(visible))
	for i, v := range visible {
		names[i] = v.Name
	}
	assert.ElementsMatch(t, []string{"apple", "apricot"}, names)
}

func TestNav_FilterApplyThenCancelClearsAndRestoresFullListing(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "apple"))
	mustMkdir(t, filepath.Join(root, "banana"))

	nav, cmd := NewNav(root, false, nil)
	runAndApply(t, nav, cmd)

	nav.FilterStart()
	nav.FilterAppend('a')
	nav.FilterApply()
	assert.False(t, nav.Filter().Editing())
	assert.True(t, nav.Filter().Active())
	assert.Len(t, nav.VisibleEntries(), 2, "both apple and banana contain 'a'")

	nav.FilterCancel()
	assert.False(t, nav.Filter().Active())
	assert.Len(t, nav.VisibleEntries(), 2, "with the filter cleared, both entries are visible unfiltered too")
}

func TestNav_VisibleEntriesReportsMatchRangeWhenFilterActive(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "browser"))

	nav, cmd := NewNav(root, false, nil)
	runAndApply(t, nav, cmd)

	nav.FilterStart()
	nav.FilterAppend('r')
	nav.FilterAppend('o')

	visible := nav.VisibleEntries()
	require.Len(t, visible, 1)
	assert.True(t, visible[0].Matched)
	assert.Equal(t, Match{Start: 1, End: 3}, visible[0].Match)
}
