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
// listing (or to 0 for an empty or not-yet-loaded listing).
func (n *Nav) SetCursor(i int) {
	n.cursor = clampCursor(i, len(n.current.Listing.Entries))
}

// MoveCursor moves the cursor by delta entries, clamped to the bounds of the
// current listing.
func (n *Nav) MoveCursor(delta int) {
	n.SetCursor(n.cursor + delta)
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
func (n *Nav) Enter() tea.Cmd {
	entries := n.current.Listing.Entries
	if n.cursor < 0 || n.cursor >= len(entries) {
		return nil
	}

	entry := entries[n.cursor]
	if !entry.Enterable() {
		return nil
	}

	// remember where the cursor was in the directory we're leaving, so Up
	// can restore it later.
	n.memory[n.path] = n.cursor

	child := filepath.Join(n.path, entry.Name)
	n.path = child
	n.cursor = n.memory[child] // 0 if this path has never been visited

	return n.load()
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
	if pos, ok := n.memory[up]; ok {
		n.cursor = pos
	} else {
		n.cursor = 0
	}

	return n.load()
}
