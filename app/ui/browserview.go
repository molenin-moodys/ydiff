package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/molenin-moodys/ydiff/app/browser"
	"github.com/molenin-moodys/ydiff/app/ui/style"
)

// Narrow-terminal tiers, per the design's Technical Details table: at or
// above wideTierWidth all three Miller columns render at their proportions;
// at or above mediumTierWidth but below wideTierWidth the parent column is
// dropped and the current/changed columns share the width; below
// mediumTierWidth only the focused pane renders, full width.
const (
	wideTierWidth   = 100
	mediumTierWidth = 60
)

// browserChromeRows is how many rows RenderBrowserView spends on something
// other than a column's content: the path header at the top, each column
// box's own top and bottom border, and the status bar. Pane height and the
// mouse hit-test row math both derive from it, so they cannot drift apart.
const browserChromeRows = 4

// BrowserFocus identifies which pane holds keyboard focus in the browser
// screen. It decides which column's frame renders with the active border
// color and, below mediumTierWidth, which single pane is shown (the other
// stays reachable via Tab).
type BrowserFocus int

const (
	// BrowserFocusCurrent is the live middle column, the browser's default focus.
	BrowserFocusCurrent BrowserFocus = iota
	// BrowserFocusChanged is the changed-files pane (task 20's Tab target).
	BrowserFocusChanged
)

// BrowserViewParams collects everything RenderBrowserView needs. Column
// widths are a plain parameter here rather than parsed from a flag: the
// --browser-widths flag and its config/env plumbing are wired in a later
// task, which will pass its resolved value through Widths unchanged. The
// changed-files pane's content, count, scope label and branch likewise come
// from gitstate in a later task; here they are plain inputs so this view can
// render the pane's frame and be fully tested without a gitstate dependency.
type BrowserViewParams struct {
	Nav *browser.Nav

	// Widths are the parent/current/changed column proportions (default
	// 15/35/50). They are normalized internally and need not sum to 100.
	Widths [3]int

	Width, Height int
	Resolver      style.Resolver
	Focus         BrowserFocus

	// ChangedContent is the changed-files pane's pre-rendered body, one line
	// per row, supplied by task 20. An empty string renders an empty pane.
	ChangedContent string
	ChangedCount   int
	ScopeLabel     string // e.g. "uncommitted" or "branch"; empty when not yet known
	Branch         string // empty when not yet known or not inside a repository
}

// RenderBrowserView renders the three-column Miller-style browser screen:
// the parent directory (orientation only, no cursor of its own), the live
// current directory, and the changed-files pane, followed by a status bar.
// Column count adapts to Width per the narrow-terminal tiers above.
func RenderBrowserView(p BrowserViewParams) string {
	ph := max(p.Height-browserChromeRows, 1)

	var panes string
	switch {
	case p.Width >= wideTierWidth:
		panes = p.renderThreeColumns(ph)
	case p.Width >= mediumTierWidth:
		panes = p.renderTwoColumns(ph)
	default:
		panes = p.renderSingleColumn(ph)
	}

	statusW := max(p.Width, 0)
	status := p.Resolver.Style(style.StyleKeyStatusBar).Width(statusW).Render(p.statusBarText())
	return lipgloss.JoinVertical(lipgloss.Left, p.renderPathHeader(), panes, status)
}

// renderPathHeader renders the current directory path as the view's top row,
// with the leading directories muted and the directory you are standing in
// picked out — so the eye lands on where it is, with the route there still
// readable. The home directory is abbreviated to "~", and an over-long path
// is truncated from the left, keeping the end (the part that identifies the
// directory) rather than the root.
func (p BrowserViewParams) renderPathHeader() string {
	path := abbreviateHome(p.Nav.Path())
	width := max(p.Width, 0)

	parent, current := filepath.Dir(path), filepath.Base(path)
	if parent == "." || current == "" || parent == path {
		// a root-like path ("/", "~") has no parent segment to mute
		return p.Resolver.Style(style.StyleKeyFileSelected).Width(width).
			Render(truncateLeftToWidth(sanitizeFilenameForDisplay(path), width))
	}
	if !strings.HasSuffix(parent, string(filepath.Separator)) {
		parent += string(filepath.Separator)
	}

	lead := p.Resolver.Style(style.StyleKeyStatusDefault).Render(sanitizeFilenameForDisplay(parent))
	tail := p.Resolver.Style(style.StyleKeyDirEntry).Render(sanitizeFilenameForDisplay(current))
	header := lead + tail
	if lipgloss.Width(header) > width {
		// styling survives truncation poorly, so fall back to one style
		return p.Resolver.Style(style.StyleKeyDirEntry).Width(width).
			Render(truncateLeftToWidth(sanitizeFilenameForDisplay(path), width))
	}
	return lipgloss.NewStyle().Width(width).Render(header)
}

// abbreviateHome rewrites a leading home directory as "~", the way yazi and
// most shells display it, so the header stays short and scannable.
func abbreviateHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || home == string(filepath.Separator) {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rest
	}
	return path
}

func (p BrowserViewParams) renderThreeColumns(ph int) string {
	available := max(p.Width-6, 0) // 3 boxes x 2 border columns each
	widths := distributeWidths(available, []int{p.Widths[0], p.Widths[1], p.Widths[2]})

	left := p.renderParentColumn(widths[0], ph)
	mid := p.renderCurrentColumn(widths[1], ph, p.Focus == BrowserFocusCurrent)
	right := p.renderChangedColumn(widths[2], ph, p.Focus == BrowserFocusChanged)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, mid, right)
}

func (p BrowserViewParams) renderTwoColumns(ph int) string {
	available := max(p.Width-4, 0) // 2 boxes x 2 border columns each
	widths := distributeWidths(available, []int{p.Widths[1], p.Widths[2]})

	mid := p.renderCurrentColumn(widths[0], ph, p.Focus == BrowserFocusCurrent)
	right := p.renderChangedColumn(widths[1], ph, p.Focus == BrowserFocusChanged)
	return lipgloss.JoinHorizontal(lipgloss.Top, mid, right)
}

func (p BrowserViewParams) renderSingleColumn(ph int) string {
	available := max(p.Width-2, 0) // 1 box
	if p.Focus == BrowserFocusChanged {
		return p.renderChangedColumn(available, ph, true)
	}
	return p.renderCurrentColumn(available, ph, true)
}

// distributeWidths splits available columns among len(props) panes
// proportionally, normalizing props (which need not sum to 100, matching
// --browser-widths' rule in the design) and assigning the rounding
// remainder to the last pane so the total always equals available exactly.
// Non-positive proportions are treated as 0; if every proportion is
// non-positive, available is split evenly.
func distributeWidths(available int, props []int) []int {
	available = max(available, 0)

	sum := 0
	for _, v := range props {
		if v > 0 {
			sum += v
		}
	}

	out := make([]int, len(props))
	if sum <= 0 {
		base := available / len(props)
		for i := range out {
			out[i] = base
		}
		out[len(out)-1] += available - base*len(props)
		return out
	}

	used := 0
	for i, v := range props {
		if i == len(props)-1 {
			out[i] = available - used
			continue
		}
		w := available * max(v, 0) / sum
		out[i] = w
		used += w
	}
	return out
}

// rowSpec is one rendered row of a Miller column: an entry plus the
// rendering hints (cursor, the parent column's current-directory highlight,
// filter-match range) that renderRow needs.
type rowSpec struct {
	entry browser.Entry

	cursor    bool // draw the current column's cursor (background highlight)
	highlight bool // draw the parent column's current-directory accent (no cursor of its own)

	matched bool
	match   browser.Match
}

// visibleWindow returns the slice of rows to display when height rows are
// available, scrolled so that focusIdx (the cursor for the current column,
// the highlighted current-directory entry for the parent column; -1 for
// neither) stays within the window. Returns rows unchanged when it already
// fits.
func visibleWindow(rows []rowSpec, height, focusIdx int) []rowSpec {
	if height <= 0 || len(rows) == 0 {
		return nil
	}
	if len(rows) <= height {
		return rows
	}

	offset := 0
	if focusIdx >= height {
		offset = focusIdx - height + 1
	}
	if offset+height > len(rows) {
		offset = len(rows) - height
	}
	offset = max(offset, 0)
	return rows[offset : offset+height]
}

// parentRows builds the parent column's rows from its raw (unfiltered)
// listing, marking the entry that matches the current directory's base name
// for the accent highlight — never a cursor, per the design.
func (p BrowserViewParams) parentRows() []rowSpec {
	entries := p.Nav.Parent().Listing.Entries
	currentBase := filepath.Base(p.Nav.Path())

	rows := make([]rowSpec, len(entries))
	for i, e := range entries {
		rows[i] = rowSpec{entry: e, highlight: e.Name == currentBase}
	}
	return rows
}

// currentRows builds the current column's rows from the filtered (visible)
// listing, carrying the cursor position and any filter-match range.
func (p BrowserViewParams) currentRows() []rowSpec {
	visible := p.Nav.VisibleEntries()
	cursor := p.Nav.Cursor()

	rows := make([]rowSpec, len(visible))
	for i, v := range visible {
		rows[i] = rowSpec{entry: v.Entry, cursor: i == cursor, matched: v.Matched, match: v.Match}
	}
	return rows
}

// renderParentColumn renders the left column: the parent directory's
// listing, with the entry for the current directory highlighted and no
// cursor of its own. It reads from Nav.Parent() only, so the loading
// placeholder (task 13) and inline error (task 12) apply the same as any
// other column.
func (p BrowserViewParams) renderParentColumn(width, height int) string {
	col := p.Nav.Parent()

	rows := p.parentRows()
	highlightIdx := -1
	for i, r := range rows {
		if r.highlight {
			highlightIdx = i
			break
		}
	}
	visible := visibleWindow(rows, height, highlightIdx)

	// the parent column never has a cursor, so its border never renders active.
	return p.renderColumnBox(col, visible, width, height, false)
}

// renderCurrentColumn renders the middle column: the live directory the
// cursor moves within, after the substring filter (if active).
func (p BrowserViewParams) renderCurrentColumn(width, height int, active bool) string {
	col := p.Nav.Current()

	rows := p.currentRows()
	visible := visibleWindow(rows, height, p.Nav.Cursor())

	return p.renderColumnBox(col, visible, width, height, active)
}

// renderChangedColumn renders the right column's frame: a header naming the
// active scope, and whatever body content the caller supplied. Task 20
// supplies the real gitstate-backed content; this task only builds and tests
// the frame around it.
func (p BrowserViewParams) renderChangedColumn(width, height int, active bool) string {
	header := "changed"
	if p.ScopeLabel != "" {
		header += " - " + p.ScopeLabel
	}
	headerLine := p.Resolver.Style(style.StyleKeyDirEntry).Render(truncateLeftToWidth(header, width))

	bodyHeight := max(height-1, 0)
	lines := strings.Split(p.ChangedContent, "\n")
	if p.ChangedContent == "" {
		lines = nil
	}
	if len(lines) > bodyHeight {
		lines = lines[:bodyHeight]
	}

	content := headerLine
	if len(lines) > 0 {
		content += "\n" + strings.Join(lines, "\n")
	}

	return p.boxStyle(active).Width(width).Height(height).Render(content)
}

// renderColumnBox assembles one Miller column's box: either the loading
// placeholder (task 13's Column.Pending), the directory's inline error (task
// 12), an empty-directory message, or the rendered rows — wrapped in the
// theme's tree-pane border style so the browser and the review screen's file
// tree look like one application. There is no per-column header line: the
// directory name is redundant with the path header above the columns.
func (p BrowserViewParams) renderColumnBox(col *browser.Column, rows []rowSpec, width, height int, active bool) string {
	var lines []string

	switch {
	case col.Pending():
		lines = append(lines, p.Resolver.Style(style.StyleKeyStatusDefault).Render(truncateLeftToWidth("loading…", width)))
	case col.Err != nil:
		msg := sanitizeFilenameForDisplay("error: " + col.Err.Error())
		lines = append(lines, p.Resolver.Style(style.StyleKeyStatusDeleted).Render(truncateRightToWidth(msg, width)))
	case len(rows) == 0:
		lines = append(lines, p.Resolver.Style(style.StyleKeyStatusDefault).Render(truncateLeftToWidth("(empty)", width)))
	default:
		for _, r := range rows {
			lines = append(lines, p.renderRow(r, width))
		}
	}

	return p.boxStyle(active).Width(width).Height(height).Render(strings.Join(lines, "\n"))
}

// boxStyle returns the tree-pane border style, active (accent border) or
// inactive (muted border) — the same StyleKey pair the review screen's file
// tree uses, so a focused browser column and a focused file tree read as the
// same kind of thing.
func (p BrowserViewParams) boxStyle(active bool) lipgloss.Style {
	if active {
		return p.Resolver.Style(style.StyleKeyTreePaneActive)
	}
	return p.Resolver.Style(style.StyleKeyTreePane)
}

// renderRow renders one entry: a trailing marker for directories ("/"),
// enterable symlinks ("->") or broken symlinks ("✗", dimmed and not
// enterable per task 12), the filter-match range highlighted when the row
// fits without truncation, and a background highlight for the cursor row or
// an accent highlight for the parent column's current-directory row.
func (p BrowserViewParams) renderRow(r rowSpec, width int) string {
	name := sanitizeFilenameForDisplay(r.entry.Name)

	suffix := ""
	switch {
	case r.entry.IsSymlink && r.entry.SymlinkBroken:
		suffix = " ✗"
	case r.entry.IsSymlink && r.entry.IsDir:
		suffix = " ->"
	case r.entry.IsDir:
		suffix = "/"
	}

	avail := max(width-runewidth.StringWidth(suffix), 1)
	fits := runewidth.StringWidth(name) <= avail

	var display string
	switch {
	case fits && r.matched && r.match.End > r.match.Start:
		restore := p.Resolver.Color(style.ColorKeyNormalFg)
		if r.entry.IsDir {
			restore = p.Resolver.Color(style.ColorKeyAccentFg)
		}
		display = highlightMatchInline(name, r.match, p.Resolver.Color(style.ColorKeySearchFg), restore) + suffix
	default:
		display = truncateLeftToWidth(name, avail) + suffix
	}

	entryStyle := p.Resolver.Style(style.StyleKeyFileEntry)
	if r.entry.IsDir {
		entryStyle = p.Resolver.Style(style.StyleKeyDirEntry)
	}
	if r.entry.IsSymlink && r.entry.SymlinkBroken {
		entryStyle = entryStyle.Faint(true)
	}

	switch {
	case r.cursor:
		return p.Resolver.Style(style.StyleKeyFileSelected).Width(width).Render(display)
	case r.highlight:
		return p.Resolver.Style(style.StyleKeyDirEntry).Bold(true).Width(width).Render(display)
	default:
		return entryStyle.Width(width).Render(display)
	}
}

// highlightMatchInline colors name's [m.Start, m.End) rune range (already
// validated non-empty by the caller) with matchFg, restoring restoreFg (or
// ResetFg when empty) immediately after — a raw ANSI foreground-only
// sequence with no full reset, matching style.Renderer's convention so the
// embedding row style's background (via StyleKeyFileSelected or a themed
// TreeBg) is not cut short partway through the line.
func highlightMatchInline(name string, m browser.Match, matchFg, restoreFg style.Color) string {
	runes := []rune(name)
	start, end := max(m.Start, 0), min(m.End, len(runes))
	if start >= end {
		return name
	}

	var b strings.Builder
	b.WriteString(string(runes[:start]))
	if matchFg != "" {
		b.WriteString(string(matchFg))
	}
	b.WriteString(string(runes[start:end]))
	if restoreFg != "" {
		b.WriteString(string(restoreFg))
	} else {
		b.WriteString(string(style.ResetFg))
	}
	b.WriteString(string(runes[end:]))
	return b.String()
}

// truncateRightToWidth right-truncates s with a trailing "…" so it fits in
// budget visual columns, preserving the meaningful start (e.g. an "error: "
// prefix) instead of the tail that truncateLeftToWidth keeps. Returns s
// unchanged when it already fits, "" when budget <= 0, "…" when budget == 1.
func truncateRightToWidth(s string, budget int) string {
	if lipgloss.Width(s) <= budget {
		return s
	}
	if budget <= 0 {
		return ""
	}
	if budget == 1 {
		return "…"
	}

	headBudget := budget - 1 // 1 cell for the trailing "…"
	w := 0
	cutIdx := 0
	for i, r := range s {
		rw := runewidth.RuneWidth(r)
		if w+rw > headBudget {
			break
		}
		w += rw
		cutIdx = i + len(string(r))
	}
	return s[:cutIdx] + "…"
}

// statusBarText builds the browser status bar: current path and active
// filter on the left, changed count, scope label and branch name on the
// right (design mock-up: "4 changed - main").
func (p BrowserViewParams) statusBarText() string {
	left := sanitizeFilenameForDisplay(p.Nav.Path())
	if f := p.Nav.Filter(); f.Active() {
		left += "   filter: " + sanitizeFilenameForDisplay(f.Query())
	}

	right := fmt.Sprintf("%d changed", p.ChangedCount)
	if p.ScopeLabel != "" {
		right += " (" + p.ScopeLabel + ")"
	}
	if p.Branch != "" {
		right += " - " + sanitizeFilenameForDisplay(p.Branch)
	}

	// 2 cells for the status bar's own Padding(0, 1) on each side, 1 cell for
	// a minimum single-space gap between the path/filter section and the
	// changed-count/scope/branch section.
	inner := max(p.Width-2, 0)
	rightW := lipgloss.Width(right)
	if rightW > inner {
		// The right section alone doesn't fit: keep as much of it as
		// possible (its meaningful end — the branch name — matters most)
		// and drop the left section entirely rather than overflow the bar,
		// which would otherwise wrap onto a second line.
		return truncateLeftToWidth(right, inner)
	}

	leftBudget := inner - rightW - 1
	left = truncateLeftToWidth(left, max(leftBudget, 0))

	padding := inner - lipgloss.Width(left) - rightW
	minPadding := 0
	if left != "" {
		minPadding = 1
	}
	if padding < minPadding {
		padding = minPadding
	}
	return left + strings.Repeat(" ", padding) + right
}
