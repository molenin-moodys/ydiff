package browser

import "strings"

// FilterState is the lifecycle stage of the browser's substring filter.
type FilterState int

const (
	// FilterOff means no filter is active: the middle column shows every entry.
	FilterOff FilterState = iota
	// FilterEditing means the filter is being typed: keystrokes append to or
	// remove from the query, and the middle column narrows live as they do.
	FilterEditing
	// FilterApplied means the filter has been applied (Enter, in the design)
	// and control has returned to navigation, but the query keeps narrowing
	// the middle column until it is cancelled or the directory changes.
	FilterApplied
)

// Filter is the browser's live substring filter over the middle (current)
// column. Matching is case-insensitive substring — deliberately not fuzzy,
// so it stays obvious why an entry matched. Filter knows nothing about git,
// diffs or rendering, and nothing about key bindings: it only tracks the
// query text and its lifecycle. Binding the `/` key and typed characters to
// these methods is package ui's job (via keymap), not this package's.
//
// The zero value is a valid, inactive filter.
type Filter struct {
	state FilterState
	query string
}

// Active reports whether the filter is currently narrowing the listing,
// whether it is still being typed (FilterEditing) or has been applied and
// kept active (FilterApplied).
func (f Filter) Active() bool { return f.state != FilterOff }

// Editing reports whether the filter is currently accepting keystrokes.
func (f Filter) Editing() bool { return f.state == FilterEditing }

// Query returns the current filter text.
func (f Filter) Query() string { return f.query }

// Start begins editing a new filter, discarding any previous query.
func (f *Filter) Start() {
	f.state = FilterEditing
	f.query = ""
}

// Append adds a rune to the filter query. It is a no-op unless the filter is
// currently being edited.
func (f *Filter) Append(r rune) {
	if f.state != FilterEditing {
		return
	}
	f.query += string(r)
}

// Backspace removes the last rune of the query. It is a no-op unless the
// filter is being edited, and a no-op on an already-empty query.
func (f *Filter) Backspace() {
	if f.state != FilterEditing || f.query == "" {
		return
	}
	runes := []rune(f.query)
	f.query = string(runes[:len(runes)-1])
}

// Apply leaves editing mode while keeping the filter active with its current
// query — "apply, keep active, return to navigation" in the design. It is a
// no-op unless the filter is currently being edited.
func (f *Filter) Apply() {
	if f.state != FilterEditing {
		return
	}
	f.state = FilterApplied
}

// Cancel clears the query and deactivates the filter entirely, from either
// FilterEditing or FilterApplied.
func (f *Filter) Cancel() {
	f.state = FilterOff
	f.query = ""
}

// Reset unconditionally drops the filter. It is called on any directory
// change, in either direction: a filter that narrowed one directory's
// listing has no bearing on another, so nothing carries over.
func (f *Filter) Reset() {
	f.state = FilterOff
	f.query = ""
}

// Match is the rune range within an entry's name that matched the filter
// query — rune indices, not byte indices, since names may contain
// non-ASCII characters whose UTF-8 encoding spans more than one byte.
type Match struct {
	Start, End int
}

// MatchName reports whether name contains query as a case-insensitive
// substring and, if so, the matched rune range within name. An empty query
// matches every name, with a zero-value Match (nothing to highlight).
func MatchName(name, query string) (Match, bool) {
	if query == "" {
		return Match{}, true
	}

	nameRunes := []rune(strings.ToLower(name))
	queryRunes := []rune(strings.ToLower(query))

	idx := indexRunes(nameRunes, queryRunes)
	if idx < 0 {
		return Match{}, false
	}
	return Match{Start: idx, End: idx + len(queryRunes)}, true
}

// indexRunes returns the index of the first occurrence of needle within
// haystack, both already normalized by the caller, or -1 if absent.
func indexRunes(haystack, needle []rune) int {
	if len(needle) == 0 {
		return 0
	}
	if len(needle) > len(haystack) {
		return -1
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if runesEqual(haystack[i:i+len(needle)], needle) {
			return i
		}
	}
	return -1
}

func runesEqual(a, b []rune) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
