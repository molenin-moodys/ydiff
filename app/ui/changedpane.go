package ui

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/molenin-moodys/ydiff/app/gitstate"
	"github.com/molenin-moodys/ydiff/app/ui/style"
)

// changedPaneStatus classifies what the changed-files pane currently has to
// show, so the design's two distinct "there is nothing to show you" cases —
// outside a repository, and a branch scope whose base cannot be resolved —
// each render their own plain message rather than being conflated with an
// ordinary empty list.
type changedPaneStatus int

const (
	changedPaneOK changedPaneStatus = iota
	changedPaneNoRepo
	changedPaneBaseNotFound
	changedPaneErr
)

// Plain messages the changed-files pane renders for each non-OK status.
// "nothing changed" (an OK status with zero files) is deliberately a
// different string from "not a git repository" and "base branch not
// found...": the design is emphatic that silently showing an empty list
// when the base failed to resolve would let a reviewer believe there is
// nothing to review.
const (
	changedPaneMsgLoading  = "loading…"
	changedPaneMsgNoRepo   = "not a git repository"
	changedPaneMsgNoBase   = "base branch not found - pass --base-branch"
	changedPaneMsgEmpty    = "nothing changed"
	changedPaneErrorPrefix = "error: "
)

// changedPane is the changed-files pane's state: the repository-wide
// changed-file list gitstate reports for the browser's current scope, its
// own cursor, and the asynchronous load bookkeeping — request/response
// correlation and stale-response discard — mirroring browser.Column's
// design from task 13.
//
// The pane is deliberately repository-wide: it never narrows to the
// directory the browser's cursor happens to be standing in.
type changedPane struct {
	cache *gitstate.Cache
	scope gitstate.Scope

	requestedDir   string
	requestedScope gitstate.Scope
	loading        bool

	root   string // resolved repository root of what is currently displayed; "" when not in a repo
	branch string
	files  []gitstate.ChangedFile
	status changedPaneStatus
	err    error

	cursor int
}

// newChangedPane creates a changedPane backed by cache (which may be nil,
// disabling gitstate loads entirely — see LoadGitState), starting in
// scope.
func newChangedPane(cache *gitstate.Cache, scope gitstate.Scope) *changedPane {
	return &changedPane{cache: cache, scope: scope}
}

// Request issues (or re-issues) a load for dir under the pane's current
// scope, recording it as the outstanding request so Apply can recognize —
// and discard — a result that arrives after the user has since navigated
// elsewhere or toggled scope. Called whenever the browser's current
// directory changes, so a navigation into a different repository re-resolves
// and switches the pane to it.
func (p *changedPane) Request(dir string) tea.Cmd {
	p.requestedDir = dir
	p.requestedScope = p.scope
	p.loading = true
	return LoadGitState(p.cache, dir, p.scope)
}

// Apply applies a GitLoadedMsg to the pane, returning true if it was
// accepted. A message answering a directory or scope other than the pane's
// current outstanding request is stale — the user navigated away or
// toggled scope before it arrived — and is discarded: it returns false and
// leaves the pane's state untouched.
func (p *changedPane) Apply(msg GitLoadedMsg) bool {
	if msg.Dir != p.requestedDir || msg.Scope != p.requestedScope {
		return false
	}
	p.loading = false
	p.root = msg.Root
	p.branch = msg.Branch

	switch {
	case errors.Is(msg.Err, gitstate.ErrNoRepository):
		p.status = changedPaneNoRepo
		p.files = nil
		p.err = nil
	case errors.Is(msg.Err, gitstate.ErrNoBase):
		p.status = changedPaneBaseNotFound
		p.files = nil
		p.err = nil
	case msg.Err != nil:
		p.status = changedPaneErr
		p.err = msg.Err
		p.files = nil
	default:
		p.status = changedPaneOK
		p.err = nil
		p.files = msg.Files
	}

	p.cursor = clampPaneCursor(p.cursor, len(p.files))
	return true
}

// ToggleScope switches between the uncommitted and branch scopes, resets
// the cursor (the two scopes' lists are unrelated), and returns the command
// that reloads dir under the new scope.
func (p *changedPane) ToggleScope(dir string) tea.Cmd {
	if p.scope == gitstate.ScopeUncommitted {
		p.scope = gitstate.ScopeBranch
	} else {
		p.scope = gitstate.ScopeUncommitted
	}
	p.cursor = 0
	return p.Request(dir)
}

// Refresh invalidates the cached result for the pane's current repository
// root and scope (a no-op when the pane is not currently in a repository)
// and returns the command that reloads dir.
func (p *changedPane) Refresh(dir string) tea.Cmd {
	if p.cache != nil && p.root != "" {
		p.cache.Invalidate(p.root, p.scope)
	}
	return p.Request(dir)
}

// MoveCursor moves the pane's cursor by delta entries, clamped to the
// bounds of the current file list.
func (p *changedPane) MoveCursor(delta int) {
	p.cursor = clampPaneCursor(p.cursor+delta, len(p.files))
}

// Selected returns the repository-relative path of the changed file
// currently under the cursor, and true if there is one. It is false when
// the pane has no OK-status result, or has no entries, or (impossible in
// balanced state, but checked defensively) the cursor is out of range.
func (p *changedPane) Selected() (string, bool) {
	if p.status != changedPaneOK || p.cursor < 0 || p.cursor >= len(p.files) {
		return "", false
	}
	return p.files[p.cursor].Path, true
}

// MatchPath reports whether relPath (a path relative to the pane's current
// repository root) names a file in the pane's changed-file list, returning
// it unchanged when it does. Used by the browser's middle column to decide
// whether Enter on a plain file should open review scoped to it (per the
// design: "Enter on a changed file... does nothing if the file is
// unchanged") — the same rule the changed-files pane itself trivially
// satisfies, since every entry it lists is a changed file.
func (p *changedPane) MatchPath(relPath string) (string, bool) {
	if p.status != changedPaneOK {
		return "", false
	}
	for _, f := range p.files {
		if f.Path == relPath {
			return f.Path, true
		}
	}
	return "", false
}

// count returns the number of changed files currently listed — 0 whenever
// status is not changedPaneOK, matching the status bar's "N changed" text
// having nothing to count while loading, outside a repository, or with an
// unresolved base.
func (p *changedPane) count() int {
	if p.status != changedPaneOK {
		return 0
	}
	return len(p.files)
}

// clampPaneCursor clamps i to a valid index into a list of length count (0
// for an empty list), mirroring browser's own cursor clamping.
func clampPaneCursor(i, count int) int {
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

// Render renders the changed-files pane's body: one themed status-badge
// line per changed file, scrolled so the cursor stays within height lines,
// or the pane's own plain message when there is nothing to list as such
// (loading, not a repository, base not found, an ordinary error, or simply
// nothing changed).
func (p *changedPane) Render(resolver style.Resolver, width, height int, focused bool) string {
	switch {
	case p.loading:
		return resolver.Style(style.StyleKeyStatusDefault).Render(truncateLeftToWidth(changedPaneMsgLoading, width))
	case p.status == changedPaneNoRepo:
		return resolver.Style(style.StyleKeyStatusDefault).Render(truncateLeftToWidth(changedPaneMsgNoRepo, width))
	case p.status == changedPaneBaseNotFound:
		return resolver.Style(style.StyleKeyStatusDeleted).Render(truncateLeftToWidth(changedPaneMsgNoBase, width))
	case p.status == changedPaneErr:
		msg := sanitizeFilenameForDisplay(changedPaneErrorPrefix + p.err.Error())
		return resolver.Style(style.StyleKeyStatusDeleted).Render(truncateRightToWidth(msg, width))
	case len(p.files) == 0:
		return resolver.Style(style.StyleKeyStatusDefault).Render(truncateLeftToWidth(changedPaneMsgEmpty, width))
	}

	start, end := paneScrollWindow(len(p.files), height, p.cursor)
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		lines = append(lines, p.renderRow(resolver, p.files[i], width, focused && i == p.cursor))
	}
	return strings.Join(lines, "\n")
}

// renderRow renders one changed-file row: a themed two-character status
// badge (M , A , D , R , ??) followed by the path — the rename's old path
// too, since a bare new path alone can be a non sequitur without it — with
// a background highlight when it is the cursor row and the pane is focused.
func (p *changedPane) renderRow(resolver style.Resolver, f gitstate.ChangedFile, width int, cursor bool) string {
	badge := string(f.Status)
	if len(badge) < 2 {
		badge += strings.Repeat(" ", 2-len(badge))
	}
	badgeStyle := resolver.Style(statusStyleKey(f.Status))

	display := f.Path
	if f.Status == gitstate.StatusRenamed && f.OldPath != "" {
		display = f.OldPath + " -> " + f.Path
	}

	name := sanitizeFilenameForDisplay(display)
	nameBudget := max(width-3, 1) // badge (2 chars) + 1 space
	name = truncateLeftToWidth(name, nameBudget)

	line := badgeStyle.Render(badge) + " " + name
	if cursor {
		return resolver.Style(style.StyleKeyFileSelected).Width(width).Render(line)
	}
	return resolver.Style(style.StyleKeyFileEntry).Width(width).Render(line)
}

// statusStyleKey maps a gitstate.Status badge to the themed style already
// used for exactly this concept elsewhere in the application (the review
// screen's file tree): added and untracked read as additions, deleted reads
// as a removal, and modified/renamed fall back to the default status color —
// the same grouping style.Renderer.FileStatusMark uses.
func statusStyleKey(s gitstate.Status) style.StyleKey {
	switch s {
	case gitstate.StatusAdded, gitstate.StatusUntracked:
		return style.StyleKeyStatusAdded
	case gitstate.StatusDeleted:
		return style.StyleKeyStatusDeleted
	default: // StatusModified, StatusRenamed, and any future status
		return style.StyleKeyStatusDefault
	}
}

// paneScrollWindow returns the [start, end) half-open range of indices to
// display out of count entries when height rows are available, scrolled so
// that focus (the cursor) stays within the window. Mirrors browserview.go's
// visibleWindow logic over plain indices rather than []rowSpec.
func paneScrollWindow(count, height, focus int) (start, end int) {
	if height <= 0 || count == 0 {
		return 0, 0
	}
	if count <= height {
		return 0, count
	}

	offset := 0
	if focus >= height {
		offset = focus - height + 1
	}
	if offset+height > count {
		offset = count - height
	}
	if offset < 0 {
		offset = 0
	}
	return offset, offset + height
}
