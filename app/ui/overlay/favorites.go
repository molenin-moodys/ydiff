package overlay

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/molenin-moodys/ydiff/app/keymap"
	"github.com/molenin-moodys/ydiff/app/ui/style"
)

const (
	favPopupMaxWidth  = 140 // maximum popup width
	favPopupMinWidth  = 20  // minimum popup width
	favPopupMargin    = 10  // horizontal margin from terminal edges
	favPopupBorderPad = 4   // border (2) + padding (2) for content width
)

// favoritesOverlay is the browser screen's favorite-directories popup: a
// scrollable list of paths, mirroring annotListOverlay's shape (cursor,
// offset, last-known dimensions for click mapping) but with three actions
// instead of one — Enter jumps to a path (OutcomeFavoriteChosen), Delete
// removes it (OutcomeFavoriteDeleteRequested, handled by the browser screen
// which owns the persistence and pushes the refreshed list back via
// Manager.UpdateFavorites), Esc/browser_dismiss closes it.
type favoritesOverlay struct {
	items      []string
	cursor     int
	offset     int
	height     int // last known terminal height, updated on render
	popupWidth int // last known popup width, updated on render; used by handleLeftClick
}

func (f *favoritesOverlay) open(spec FavoritesSpec) {
	f.items = spec.Items
	f.cursor = 0
	f.offset = 0
}

// update replaces the item list in place (see Manager.UpdateFavorites),
// clamping the cursor so a deletion that shortens the list never leaves it
// pointing past the end.
func (f *favoritesOverlay) update(spec FavoritesSpec) {
	f.items = spec.Items
	if len(f.items) == 0 {
		f.cursor, f.offset = 0, 0
		return
	}
	f.cursor = min(f.cursor, len(f.items)-1)
	f.offset = min(f.offset, f.cursor)
}

func (f *favoritesOverlay) render(ctx RenderCtx, mgr *Manager) string {
	f.height = ctx.Height
	popupWidth := max(min(ctx.Width-favPopupMargin, favPopupMaxWidth), favPopupMinWidth)
	f.popupWidth = popupWidth

	title := fmt.Sprintf(" favorites (%d) ", len(f.items))
	accentFg := string(ctx.Resolver.Color(style.ColorKeyAccentFg))
	paneBg := string(ctx.Resolver.Color(style.ColorKeyDiffPaneBg))
	edge := borderEdgeText{popupWidth: popupWidth, accentFg: accentFg, paneBg: paneBg}

	if len(f.items) == 0 {
		box := f.emptyBox(popupWidth, ctx.Resolver)
		box = mgr.injectBorderTitle(box, title, edge)
		return box
	}

	maxVisibleItems := f.maxVisible(ctx.Height)
	contentWidth := popupWidth - favPopupBorderPad

	var lines []string
	for i := f.offset; i < len(f.items) && i < f.offset+maxVisibleItems; i++ {
		lines = append(lines, f.formatItem(f.items[i], contentWidth, i == f.cursor, ctx.Resolver))
	}
	content := strings.Join(lines, "\n")

	box := f.boxStyle(popupWidth, ctx.Resolver).Render(content)
	box = mgr.injectBorderTitle(box, title, edge)
	box = mgr.injectBorderFooter(box, " enter: jump  delete: remove  esc: close ", edge)
	return box
}

func (f *favoritesOverlay) emptyBox(popupWidth int, resolver Resolver) string {
	text := "no favorites yet — press ctrl+f on a directory to add it"
	innerWidth := popupWidth - favPopupBorderPad
	centered := text
	if pad := (innerWidth - lipgloss.Width(text)) / 2; pad > 0 {
		centered = strings.Repeat(" ", pad) + text
	}
	if lipgloss.Width(centered) > innerWidth {
		centered = ansi.Truncate(centered, innerWidth, "...")
	}
	return f.boxStyle(popupWidth, resolver).Render(centered)
}

func (f *favoritesOverlay) boxStyle(width int, resolver Resolver) lipgloss.Style {
	return resolver.Style(style.StyleKeyAnnotListBorder).
		Padding(1, 1).
		Width(width)
}

func (f *favoritesOverlay) formatItem(path string, width int, selected bool, resolver Resolver) string {
	display := path
	if lipgloss.Width(display) > width {
		display = ansi.Truncate(display, max(width-3, 1), "...")
	}

	if selected {
		selStyle := resolver.Style(style.StyleKeyFileSelected)
		styled := selStyle.Render("> " + display)
		w := lipgloss.Width(styled)
		if w < width {
			styled += selStyle.Render(strings.Repeat(" ", width-w))
		}
		return styled
	}
	return "  " + display
}

func (f *favoritesOverlay) maxVisible(height int) int {
	return max(min(len(f.items), height-6), 1)
}

// handleKey mirrors annotListOverlay.handleKey's shape: Enter picks the
// current item, Up/Down (browser namespace — this popup only ever opens
// from the browser screen) move the cursor, Delete asks the browser screen
// to remove the current item (the overlay does not mutate its own list —
// persistence and the follow-up UpdateFavorites call are the browser
// screen's job, per CLAUDE.md's rule against OS-level work in the ui
// package), and browser_dismiss/Esc closes the popup.
func (f *favoritesOverlay) handleKey(msg tea.KeyMsg, action keymap.Action) Outcome {
	switch {
	case msg.Type == tea.KeyEnter:
		if len(f.items) == 0 {
			return Outcome{Kind: OutcomeClosed}
		}
		return Outcome{Kind: OutcomeFavoriteChosen, FavoritePath: f.items[f.cursor]}

	case msg.Type == tea.KeyDelete:
		if len(f.items) == 0 {
			return Outcome{Kind: OutcomeNone}
		}
		return Outcome{Kind: OutcomeFavoriteDeleteRequested, FavoritePath: f.items[f.cursor]}

	case action == keymap.ActionBrowserDismiss || msg.Type == tea.KeyEsc:
		return Outcome{Kind: OutcomeClosed}
	}

	switch action {
	case keymap.ActionBrowserUp:
		f.moveCursorBy(-1)
	case keymap.ActionBrowserDown:
		f.moveCursorBy(1)
	}
	return Outcome{Kind: OutcomeNone}
}

// moveCursorBy shifts the cursor by delta, clamped to [0, len(items)-1], and
// keeps it inside the visible scroll window — same policy as
// annotListOverlay.moveCursorBy.
func (f *favoritesOverlay) moveCursorBy(delta int) {
	if len(f.items) == 0 {
		return
	}
	f.cursor = min(max(f.cursor+delta, 0), len(f.items)-1)
	if f.cursor < f.offset {
		f.offset = f.cursor
	}
	maxVis := f.maxVisible(f.height)
	if f.cursor >= f.offset+maxVis {
		f.offset = f.cursor - maxVis + 1
	}
}

// handleMouse mirrors annotListOverlay.handleMouse: wheel steps the cursor,
// left-click maps a row to an item and jumps to it (same as Enter).
func (f *favoritesOverlay) handleMouse(msg tea.MouseMsg) Outcome {
	if msg.Action != tea.MouseActionPress {
		return Outcome{Kind: OutcomeNone}
	}
	switch msg.Button {
	case tea.MouseButtonWheelDown:
		f.moveCursorBy(f.wheelStep(msg.Shift))
		return Outcome{Kind: OutcomeNone}
	case tea.MouseButtonWheelUp:
		f.moveCursorBy(-f.wheelStep(msg.Shift))
		return Outcome{Kind: OutcomeNone}
	case tea.MouseButtonLeft:
		return f.handleLeftClick(msg.X, msg.Y)
	default:
		return Outcome{Kind: OutcomeNone}
	}
}

func (f *favoritesOverlay) wheelStep(shift bool) int {
	if !shift {
		return 1
	}
	return max(f.maxVisible(f.height)/2, 1)
}

// handleLeftClick mirrors annotListOverlay.handleLeftClick's chrome
// accounting (border + padding on all sides).
func (f *favoritesOverlay) handleLeftClick(localX, localY int) Outcome {
	const contentTop = 2
	const horizChromeCols = 2
	if localX < horizChromeCols || localX >= f.popupWidth-horizChromeCols {
		return Outcome{Kind: OutcomeNone}
	}
	relRow := localY - contentTop
	maxVis := f.maxVisible(f.height)
	if relRow < 0 || relRow >= maxVis {
		return Outcome{Kind: OutcomeNone}
	}
	itemIdx := f.offset + relRow
	if itemIdx < 0 || itemIdx >= len(f.items) {
		return Outcome{Kind: OutcomeNone}
	}
	f.cursor = itemIdx
	return Outcome{Kind: OutcomeFavoriteChosen, FavoritePath: f.items[itemIdx]}
}
