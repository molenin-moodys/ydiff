package browser

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// LoadedMsg is emitted when an asynchronous directory read finishes, whether it
// succeeded or failed. Path identifies which request this result answers, so a
// caller holding several in-flight or superseded requests (e.g. one per Miller
// column) can tell which one it belongs to and discard it if the user has since
// navigated away from that path.
type LoadedMsg struct {
	Path    string
	Listing Listing
	Err     error
}

// Load returns a tea.Cmd that reads path asynchronously (via Read) and reports
// the result as a LoadedMsg. showHidden is forwarded to Read unchanged. Load
// never panics: any filesystem error from Read is carried on the message as Err
// rather than surfacing here, so the caller can render it inline.
func Load(path string, showHidden bool) tea.Cmd {
	return func() tea.Msg {
		listing, err := Read(path, showHidden)
		return LoadedMsg{Path: path, Listing: listing, Err: err}
	}
}

// pendingThreshold is how long a load must be outstanding before Column.Pending
// reports true, so a placeholder is shown only for reads slow enough to be
// noticeable rather than on every keystroke.
const pendingThreshold = 150 * time.Millisecond

// Clock reports the current time. Production code uses time.Now; tests inject a
// fake so the ~150ms pending threshold can be exercised deterministically,
// without a real wall-clock sleep.
type Clock func() time.Time

// Column tracks the asynchronous load state for one browser column (one
// directory pane in the Miller-column layout): the path most recently
// requested, whether that request is still outstanding, and the outcome of the
// most recently applied result.
//
// Column itself knows nothing about git, diffs, or rendering — it only
// correlates requests with results by path and exposes state a renderer can
// read (Listing, Err, Pending).
type Column struct {
	clock Clock

	path        string // path of the most recent request; results for any other path are stale
	requestedAt time.Time
	loading     bool

	Listing Listing
	Err     error
}

// NewColumn creates a Column. A nil clock uses time.Now; tests should pass a
// fake clock to control the pending threshold deterministically.
func NewColumn(clock Clock) *Column {
	if clock == nil {
		clock = time.Now
	}
	return &Column{clock: clock}
}

// Request records path as the column's current request and returns the
// tea.Cmd that performs the read. Calling Request again before a prior
// request's result arrives supersedes it: the prior result will fail the path
// check in Apply and be discarded when it eventually arrives.
func (c *Column) Request(path string, showHidden bool) tea.Cmd {
	c.path = path
	c.requestedAt = c.clock()
	c.loading = true
	return Load(path, showHidden)
}

// Apply applies a LoadedMsg to the column, returning true if it was accepted.
// A message answering a path other than the column's current request is stale
// (the user navigated away before it arrived) and is discarded: it returns
// false and leaves the column's state untouched.
func (c *Column) Apply(msg LoadedMsg) bool {
	if msg.Path != c.path {
		return false
	}
	c.loading = false
	c.Listing = msg.Listing
	c.Err = msg.Err
	return true
}

// Pending reports whether the column's current request has been outstanding
// long enough (per the injected clock) to warrant showing a placeholder. It is
// false once a matching result has been applied, and false for a request that
// is still fresh.
func (c *Column) Pending() bool {
	return c.loading && c.clock().Sub(c.requestedAt) >= pendingThreshold
}
