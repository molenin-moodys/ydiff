package browser

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

// Nav tracks the browser's navigation state: the current directory, the
// cursor position within it, and the async load state for the current and
// parent Miller columns. It knows nothing about git, diffs, or rendering.
//
// Cursor positions are remembered per absolute path for the lifetime of the
// process only (an in-memory map): nothing is persisted to disk. This is what
// makes going back up feel right — the cursor lands back on the directory you
// just came from, instead of resetting to the top of the list.
type Nav struct {
	path       string
	cursor     int
	showHidden bool

	// memory maps an absolute path to the cursor index that was active the
	// last time that path was the current directory. It is populated on
	// Enter (recording where the cursor was in the directory being left) and
	// consulted on Up (to restore that position) and on Enter into a
	// previously-visited directory.
	memory map[string]int

	current *Column // listing for path
	parent  *Column // listing for filepath.Dir(path); has no cursor of its own

	filter Filter // substring filter over current's listing only; never applies to parent
}

// NewNav creates navigation state rooted at start and returns the tea.Cmd
// that loads its initial current and parent columns. clock is forwarded to
// both columns (nil uses time.Now; tests should inject a fake).
func NewNav(start string, showHidden bool, clock Clock) (*Nav, tea.Cmd) {
	n := &Nav{
		path:       start,
		showHidden: showHidden,
		memory:     make(map[string]int),
		current:    NewColumn(clock),
		parent:     NewColumn(clock),
	}
	return n, n.load()
}

// load issues (or re-issues) the requests for the current and parent
// columns against n.path, returning a single command that performs both.
func (n *Nav) load() tea.Cmd {
	return tea.Batch(
		n.current.Request(n.path, n.showHidden),
		n.parent.Request(filepath.Dir(n.path), n.showHidden),
	)
}

// Path returns the current directory.
func (n *Nav) Path() string { return n.path }

// Cursor returns the index of the selected entry within Current's listing.
func (n *Nav) Cursor() int { return n.cursor }

// Current returns the column for the current directory.
func (n *Nav) Current() *Column { return n.current }

// Parent returns the column for the parent directory. It has no cursor of
// its own: its role is to show where the current directory sits among its
// siblings, not to be navigated directly.
func (n *Nav) Parent() *Column { return n.parent }

// Apply routes a LoadedMsg to whichever of the current or parent column
// requested it, returning true if either column accepted it.
func (n *Nav) Apply(msg LoadedMsg) bool {
	appliedCurrent := n.current.Apply(msg)
	appliedParent := n.parent.Apply(msg)
	return appliedCurrent || appliedParent
}

// SetCursor moves the cursor to i, clamped to the bounds of the current
// listing as filtered (or to 0 for an empty or not-yet-loaded listing).
func (n *Nav) SetCursor(i int) {
	n.cursor = clampCursor(i, n.visibleCount())
}

// MoveCursor moves the cursor by delta entries, clamped to the bounds of the
// current (filtered) listing.
func (n *Nav) MoveCursor(delta int) {
	n.SetCursor(n.cursor + delta)
}

// VisibleEntry is one entry of the current column as shown after filtering:
// the entry itself, plus the matched rune range when the filter is active.
type VisibleEntry struct {
	Entry
	Match   Match
	Matched bool // meaningful only while the filter is active
}

// VisibleEntries returns the current column's entries after applying the
// filter: every entry, unfiltered, when the filter is inactive; otherwise
// only the entries whose name contains the filter query as a
// case-insensitive substring, each carrying its matched rune range. The
// filter never applies to the parent column.
func (n *Nav) VisibleEntries() []VisibleEntry {
	all := n.current.Listing.Entries

	if !n.filter.Active() {
		out := make([]VisibleEntry, len(all))
		for i, e := range all {
			out[i] = VisibleEntry{Entry: e}
		}
		return out
	}

	out := make([]VisibleEntry, 0, len(all))
	for _, e := range all {
		m, ok := MatchName(e.Name, n.filter.Query())
		if !ok {
			continue
		}
		out = append(out, VisibleEntry{Entry: e, Match: m, Matched: true})
	}
	return out
}

// visibleCount is VisibleEntries' length without allocating the slice, used
// for cursor clamping.
func (n *Nav) visibleCount() int {
	all := n.current.Listing.Entries
	if !n.filter.Active() {
		return len(all)
	}
	count := 0
	for _, e := range all {
		if _, ok := MatchName(e.Name, n.filter.Query()); ok {
			count++
		}
	}
	return count
}

// Filter returns the browser's substring filter state, for rendering and
// for the wrapper methods below. It applies only to this Nav's current
// column.
func (n *Nav) Filter() Filter { return n.filter }

// FilterStart begins editing a new filter over the current column,
// re-clamping the cursor since an empty query still narrows nothing but a
// stale cursor could otherwise point past a listing that has since shrunk.
func (n *Nav) FilterStart() {
	n.filter.Start()
	n.SetCursor(n.cursor)
}

// FilterAppend adds a rune to the filter query and immediately re-narrows
// the listing, keeping the cursor within its new bounds — the design's
// "typing narrows the middle column live".
func (n *Nav) FilterAppend(r rune) {
	n.filter.Append(r)
	n.SetCursor(n.cursor)
}

// FilterBackspace removes the last rune of the filter query and re-clamps
// the cursor against the now-wider (or unchanged) listing.
func (n *Nav) FilterBackspace() {
	n.filter.Backspace()
	n.SetCursor(n.cursor)
}

// FilterApply leaves editing mode while keeping the filter active, per the
// design: "apply, keep active, return to navigation".
func (n *Nav) FilterApply() {
	n.filter.Apply()
}

// FilterCancel clears the filter entirely and restores the full listing,
// re-clamping the cursor against it.
func (n *Nav) FilterCancel() {
	n.filter.Cancel()
	n.SetCursor(n.cursor)
}

func clampCursor(i, count int) int {
	switch {
	case count <= 0:
		return 0
	case i < 0:
		return 0
	case i >= count:
		return count - 1
	default:
		return i
	}
}

// Enter descends into the entry currently under the cursor, if any. It is a
// no-op (returning nil) when the current listing has not loaded, the cursor
// is out of range, or the selected entry is not enterable (a plain file or a
// broken symlink).
//
// Entering does not check the filesystem synchronously: the selected entry
// is trusted to be what the last listing reported. If it has since vanished
// (e.g. removed concurrently), the resulting Load simply fails and the
// column surfaces that as a renderable error via Column.Err — Enter itself
// never panics or blocks on it.
//
// Selection is taken from the filtered (visible) listing, since that is what
// the cursor indexes while a filter narrows it — but any directory change
// drops the filter (see Filter.Reset), so the cursor memory recorded here is
// translated back to the entry's position in the full, unfiltered listing.
func (n *Nav) Enter() tea.Cmd {
	entries := n.VisibleEntries()
	if n.cursor < 0 || n.cursor >= len(entries) {
		return nil
	}

	selected := entries[n.cursor].Entry
	if !selected.Enterable() {
		return nil
	}

	// remember where the cursor was in the directory we're leaving, so Up
	// can restore it later — recorded against the full listing, since the
	// filter (and thus any narrowing) is dropped by the directory change.
	n.memory[n.path] = indexByName(n.current.Listing.Entries, selected.Name)

	child := filepath.Join(n.path, selected.Name)
	n.path = child
	n.filter.Reset()           // any directory change drops the filter, in either direction
	n.cursor = n.memory[child] // 0 if this path has never been visited

	return n.load()
}

// indexByName returns the index of the entry named name within entries, or 0
// if not found (which should not happen: name comes from entries itself).
func indexByName(entries []Entry, name string) int {
	for i, e := range entries {
		if e.Name == name {
			return i
		}
	}
	return 0
}

// Up moves to the parent directory, if any. Going up from the filesystem
// root is a no-op (returning nil): it neither errors nor panics, and leaves
// the current path unchanged.
//
// The cursor lands on the directory just left, not at the top of the list:
// Enter recorded that position in memory when descending into it, and Up
// simply restores it.
func (n *Nav) Up() tea.Cmd {
	up := filepath.Dir(n.path)
	if up == n.path {
		return nil // already at the filesystem root
	}

	n.path = up
	n.filter.Reset() // any directory change drops the filter, in either direction
	if pos, ok := n.memory[up]; ok {
		n.cursor = pos
	} else {
		n.cursor = 0
	}

	return n.load()
}
