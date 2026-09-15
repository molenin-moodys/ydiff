package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/molenin-moodys/ydiff/app/keymap"
	"github.com/molenin-moodys/ydiff/app/ui/overlay"
	"github.com/molenin-moodys/ydiff/app/ui/sidepane"
)

// wheelStep is the number of lines one wheel notch scrolls by. Shift+wheel
// uses half the viewport height instead. Shared with overlay.WheelStep so
// overlay popup scroll feels the same as diff-pane scroll.
const wheelStep = overlay.WheelStep

// wheelRenderDelay is the idle window after the last diff-pane wheel event
// before the deferred cursor pin + SetContent(renderDiff()) runs. Each wheel
// event updates the viewport YOffset synchronously and defers both the cursor
// pin and the diff render to a single tea.Tick so a burst of wheel events
// produces one pin + render at burst-end instead of per-event work. Short
// enough that the cursor highlight reappears quickly after the user stops
// scrolling, long enough to coalesce a typical trackpad flick into one render.
const wheelRenderDelay = 30 * time.Millisecond

// wheelState tracks the coalescing state for diff-pane wheel events.
//
// gen is bumped on every wheel event that actually shifts YOffset (i.e.
// scrollDiffViewportBy returned true — at-edge no-ops do NOT bump). The
// in-flight tea.Tick captures the gen at scheduling time so the resulting
// debounce msg can tell whether the burst has advanced past it.
//
// renderPending stays true while a render is owed (set by wheel events,
// cleared by flushWheelPending).
//
// tickInFlight gates tea.Tick scheduling: only ONE tick is alive at a time,
// regardless of how many wheel events arrive. The first wheel of a burst
// schedules a tick; subsequent wheels in the same burst just bump gen. If the
// tick fires with stale gen (burst still going), handleWheelDebounce
// reschedules a new tick for the current gen. This keeps the debounce-msg
// count proportional to burst duration (one per wheelRenderDelay), not to
// wheel event count — drops thousands of redundant Update+View cycles on
// trackpad/free-spin bursts and stops the wheel-up from queueing behind them.
type wheelState struct {
	gen           int
	renderPending bool
	tickInFlight  bool
}

// wheelDebounceMsg is the deferred flush trigger for diff-pane wheel events.
// gen captures the wheel generation at scheduling time so handleWheelDebounce
// can distinguish a settled burst (gen matches → flush) from an in-progress
// burst (gen advanced → reschedule a fresh tick for the current gen).
type wheelDebounceMsg struct {
	gen int
}

// hitZone identifies which interactive area a mouse event targets.
type hitZone int

const (
	hitNone   hitZone = iota // outside any interactive area (borders, gaps, out-of-bounds)
	hitTree                  // tree pane (or TOC pane when mdTOC is active)
	hitDiff                  // diff pane body (below the diff header)
	hitStatus                // status bar row(s)
	hitHeader                // diff header row (file path) — currently a no-op zone
)

// statusBarHeight returns the number of rows occupied by the status bar.
// 0 when the status bar is hidden, otherwise 1.
func (m Model) statusBarHeight() int {
	if m.cfg.noStatusBar {
		return 0
	}
	return 1
}

// diffTopRow returns the first screen row (0-based y) of diff viewport content.
// accounts for the pane top border (row 0) and the diff header row (row 1),
// so the viewport always starts at row 2 regardless of whether the tree pane
// is visible.
func (m Model) diffTopRow() int {
	return 2
}

// treeTopRow returns the first screen row (0-based y) of tree pane content.
// accounts for the pane top border only — unlike diff, the tree pane has no
// internal header row, so content starts at row 1.
func (m Model) treeTopRow() int {
	return 1
}

// hitTest classifies a screen coordinate into a hitZone for mouse-event routing.
// the classification is pure arithmetic over m.layout state and does not
// inspect any dynamic UI content. ordering matters: status bar is checked
// first (y at bottom), then x is used to split tree vs diff columns, and
// finally y is used within each column to reject the diff header row or tree
// top border.
func (m Model) hitTest(x, y int) hitZone {
	if x < 0 || y < 0 || x >= m.layout.width || y >= m.layout.height {
		return hitNone
	}
	if sbh := m.statusBarHeight(); sbh > 0 && y >= m.layout.height-sbh {
		return hitStatus
	}
	// pane bottom border row sits just above the status bar (or the last
	// row when the status bar is hidden). clicks on the border must not
	// map into the viewport — without this guard, clickDiff would compute
	// a row one past the visible content.
	if y == m.layout.height-m.statusBarHeight()-1 {
		return hitNone
	}

	// tree block spans columns [0, treeWidth+1] when visible: left border +
	// treeWidth content columns + right border = treeWidth+2 columns total.
	// diff block picks up at column treeWidth+2.
	if !m.treePaneHidden() && x < m.layout.treeWidth+2 {
		if y < m.treeTopRow() {
			return hitNone
		}
		return hitTree
	}

	if y == 0 {
		return hitNone // diff pane top border — mirror of treeTopRow() guard above
	}
	if y < m.diffTopRow() {
		return hitHeader
	}
	return hitDiff
}

// handleMouse routes a tea.MouseMsg through the modal-state checks and into
// per-button dispatch. mouse events are only generated when
// tea.WithMouseCellMotion is enabled (i.e. --no-mouse is off), so this
// handler never runs in the opted-out path. wheel routing is by pointer
// position, not by current focus — this matches terminal conventions where
// scrolling follows the cursor.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// swallow during modal states — input belongs to the modal, not the
	// viewport beneath. hints are preserved here so the modal prompt (e.g.
	// reload's "press y to confirm") stays visible while the event is
	// discarded. in the keyboard path the prompt is also replaced by a new
	// hint from handlePendingReload, but mouse events don't transition the
	// modal, so dropping the hint would leave an invisible modal.
	if m.inConfirmDiscard || m.reload.pending || m.annot.annotating || m.search.active {
		return m, nil
	}
	if m.overlay.Active() {
		return m.handleOverlayMouse(msg)
	}

	// reload, output, compact-mode, and editor hints persist for exactly one
	// render cycle; any mouse event that reaches this point dismisses them,
	// mirroring handleKey.
	m.reload.hint = ""
	m.output.hint = ""
	m.compact.hint = ""
	m.editorState.hint = ""

	zone := m.hitTest(msg.X, msg.Y)

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if msg.Action != tea.MouseActionPress {
			return m, nil // guard against non-press wheel emissions for symmetry with left-click
		}
		return m.handleWheel(zone, -m.wheelStepFor(msg.Shift))
	case tea.MouseButtonWheelDown:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		return m.handleWheel(zone, m.wheelStepFor(msg.Shift))
	case tea.MouseButtonWheelLeft, tea.MouseButtonWheelRight:
		// horizontal wheel is intentionally swallowed — horizontal scroll
		// stays keyboard-driven so users keep a single mental model.
		return m, nil
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return m, nil // ignore release and motion while holding
		}
		switch zone {
		case hitTree:
			return m.clickTree(msg.Y)
		case hitDiff:
			return m.clickDiff(msg.Y)
		case hitNone, hitStatus, hitHeader:
			return m, nil
		}
		return m, nil
	default:
		// right, middle, back, forward, none — no-op for this pass.
		return m, nil
	}
}

// handleOverlayMouse routes a mouse event to the active overlay. wheel events
// drive the overlay's own scroll/cursor navigation; clicks and other buttons
// are consumed so they don't leak through to the panes underneath. outcomes
// that need model-side side effects (annotation jump, theme preview/confirm)
// are dispatched through the same helpers as the keyboard path. Canceled and
// Closed branches mirror the keyboard dispatch for symmetry but the current
// overlay mouse handlers never emit them — a mouse click either confirms
// (themeselect) or is a no-op.
func (m Model) handleOverlayMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	out := m.overlay.HandleMouse(msg)
	switch out.Kind {
	case overlay.OutcomeAnnotationChosen:
		return m.jumpToAnnotationTarget(out.AnnotationTarget)
	case overlay.OutcomeThemePreview:
		m.previewThemeByName(out.ThemeChoice.Name)
	case overlay.OutcomeThemeConfirmed:
		m.confirmThemeByName(out.ThemeChoice.Name)
	case overlay.OutcomeThemeCanceled:
		m.cancelThemeSelect()
	case overlay.OutcomeClosed, overlay.OutcomeNone:
	}
	return m, nil
}

// wheelStepFor returns the wheel scroll step for the diff pane. Plain wheel
// scrolls by the wheelStep constant; Shift+wheel scrolls by half the
// viewport height to match the keyboard half-page shortcut. The tree/TOC
// path in handleWheel ignores the magnitude and uses single-step cursor
// navigation regardless, so this only governs the diff-pane delta.
func (m Model) wheelStepFor(shift bool) int {
	if !shift {
		return wheelStep
	}
	return max(1, m.layout.viewport.Height/2)
}

// handleWheel routes a vertical wheel event to the pane under the pointer.
// delta is positive for wheel-down, negative for wheel-up. the pane is
// selected by the hit zone, not the current pane focus: users expect the
// wheel to act on whichever pane the pointer is over.
//
// diff-pane wheel scrolls the viewport only; the diff cursor stays on its
// current logical line unless the line is scrolled out of view, in which
// case the cursor is pinned to the topmost or bottommost visible line so
// the highlight stays on screen. this matches less/vim mouse behavior and
// keeps the cursor from being yanked along with the wheel.
//
// when the diff pane scrolls, both the cursor pin and the SetContent
// (renderDiff()) call are deferred via a tea.Tick debounce — see
// wheelRenderDelay. the only per-event work is SetYOffset (and the modal/zone
// checks that were already free), so each event is O(1). this coalesces a
// burst of wheel events (a trackpad flick or free-spin wheel) into a single
// pin+render at burst-end, so an opposite-direction wheel event isn't blocked
// behind a backlog of expensive per-event operations on large diffs.
// fixes #179.
func (m Model) handleWheel(zone hitZone, delta int) (tea.Model, tea.Cmd) {
	switch zone {
	case hitDiff:
		if !m.scrollDiffViewportBy(delta) {
			return m, nil
		}
		m.wheel.renderPending = true
		m.wheel.gen++
		gen := m.wheel.gen

		// schedule a tick only when none is in flight; subsequent wheels in the
		// same burst just bump gen. an in-flight tick fires at its own
		// wallclock deadline and reschedules itself if it lands on a stale gen
		// (see handleWheelDebounce). this keeps message count proportional to
		// burst duration, not wheel event count — without this guard each
		// wheel event spawns a tea.Tick goroutine and a debounce Msg, every
		// one of which forces another Update + View cycle (~2ms each).
		if m.wheel.tickInFlight {
			return m, nil
		}
		m.wheel.tickInFlight = true
		return m, tea.Tick(wheelRenderDelay, func(time.Time) tea.Msg {
			return wheelDebounceMsg{gen: gen}
		})
	case hitTree:
		// tree/TOC wheel = direct cursor navigation, one entry per notch.
		// no debounce, no shift-half-page tricks (those are diff-pane things);
		// the tree is small and cheap so single-step matches j/k semantics.
		motion := sidepane.MotionDown
		if delta < 0 {
			motion = sidepane.MotionUp
		}
		if m.file.mdTOC != nil {
			m.file.mdTOC.Move(motion)
			m.file.mdTOC.EnsureVisible(m.treePageSize())
			m.syncDiffToTOCCursor()
			return m, nil
		}
		m.tree.Move(motion)
		m.pendingAnnotJump = nil
		m.nav.pendingHunkJump = nil
		return m.loadSelectedIfChanged()
	case hitNone, hitStatus, hitHeader:
		// no-op zones — wheel outside the interactive panes is ignored.
	}
	return m, nil
}

// handleWheelDebounce processes a deferred tick for a wheel burst. branches:
//   - renderPending is false: an external path (handleKey, handleResize,
//     handleBlameLoaded) already flushed; clear tickInFlight, no-op.
//   - msg.gen lags m.wheel.gen: the burst is still going (new wheels bumped
//     gen since this tick was scheduled). reschedule a fresh tick for the
//     current gen and keep tickInFlight=true so the next wheel doesn't double-
//     schedule.
//   - msg.gen matches m.wheel.gen: the burst has been idle for at least
//     wheelRenderDelay. flush the deferred work (pin + diff render) and
//     clear tickInFlight so the next burst's first wheel schedules fresh.
func (m Model) handleWheelDebounce(msg wheelDebounceMsg) (tea.Model, tea.Cmd) {
	// renderPending cleared by some other path (handleKey flushed, resize
	// flushed, etc.): the in-flight tick is done, no more rescheduling.
	if !m.wheel.renderPending {
		m.wheel.tickInFlight = false
		return m, nil
	}
	// gen has advanced past msg.gen: the burst is still going. reschedule a
	// new tick for the current gen and stay tickInFlight=true. without this
	// reschedule the burst would never flush after the original tick fires
	// stale.
	if msg.gen != m.wheel.gen {
		curGen := m.wheel.gen
		return m, tea.Tick(wheelRenderDelay, func(time.Time) tea.Msg {
			return wheelDebounceMsg{gen: curGen}
		})
	}
	// gen matches: burst has been idle for wheelRenderDelay. flushWheelPending
	// clears both renderPending and tickInFlight, so the next burst's first
	// wheel reschedules a fresh tick.
	m.flushWheelPending()
	return m, nil
}

// clickTree handles a left-click press in the tree (or TOC) pane. the click
// both focuses the pane and selects the entry under the pointer — same as
// pressing j/k to land on the entry. when the entry is a file, the diff
// load is triggered via loadSelectedIfChanged; on a directory row or an
// out-of-range row the click just moves the cursor with no load (mirrors
// j-landing semantics).
func (m Model) clickTree(y int) (tea.Model, tea.Cmd) {
	row := y - m.treeTopRow()
	m.layout.focus = paneTree
	if m.file.mdTOC != nil {
		if !m.file.mdTOC.SelectByVisibleRow(row) {
			return m, nil
		}
		m.file.mdTOC.EnsureVisible(m.treePageSize())
		m.syncDiffToTOCCursor()
		return m, nil
	}
	if !m.tree.SelectByVisibleRow(row) {
		return m, nil
	}
	m.pendingAnnotJump = nil
	m.nav.pendingHunkJump = nil
	return m.loadSelectedIfChanged()
}

// scrollDiffViewportBy shifts the diff viewport's YOffset by delta. delta > 0
// scrolls down, delta < 0 scrolls up. returns true when YOffset changed (the
// caller should schedule a deferred cursor pin + render); returns false when
// no file is loaded or the clamped target equals the current offset.
//
// the expensive cursor pin (pinDiffCursorTo loops O(cursor_idx) via
// cursorVisualRange) and the full-diff render (SetContent(renderDiff()))
// are both deferred to handleWheelDebounce so each wheel event is O(1).
// during a wheel burst the cursor index in m.nav.diffCursor stays stale and
// the rendered string still highlights the old cursor row, matching less/vim
// behavior; the deferred work runs at burst-end (wheelRenderDelay of wheel
// idle) or when handleKey, handleResize, or handleBlameLoaded flushes early.
func (m *Model) scrollDiffViewportBy(delta int) bool {
	if m.file.name == "" {
		return false
	}
	maxOffset := max(0, m.layout.viewport.TotalLineCount()-m.layout.viewport.Height)
	current := m.layout.viewport.YOffset
	target := max(0, min(current+delta, maxOffset))
	if target == current {
		return false
	}
	m.layout.viewport.SetYOffset(target)
	return true
}

// flushWheelPending applies the deferred cursor pin + diff re-render owed by
// an in-flight wheel burst, then clears renderPending AND tickInFlight.
// callers invoke this before any action that reads m.nav.diffCursor or
// relies on a fresh diff content string. Production call sites:
//   - handleKey (model.go) — cursor-relative key actions (j/k, search,
//     annotate) must see the pinned cursor before they read m.nav.diffCursor
//   - handleResize (model.go) — syncViewportToCursor must anchor at the
//     wheeled-to position, not the pre-burst cursor
//   - handleBlameLoaded (loaders.go) — same syncViewportToCursor rationale
//   - handleWheelDebounce (mouse.go) — the matching-gen tick path
//
// no-op when no render is pending. when pinDiffCursorTo returns false (the
// cursor stayed in view through the burst, no pin needed), syncTOCActiveSection
// and SetContent are skipped — the existing rendered string already has the
// correct cursor highlight and the TOC active section is keyed off the
// (unchanged) cursor index, so a re-render would be redundant. Only the
// state flags are always cleared.
//
// clearing tickInFlight here means a new wheel event arriving after an
// external flush can schedule a fresh tick immediately (rather than waiting
// for the previously-scheduled tick to drain through the !renderPending
// branch of handleWheelDebounce); any already-scheduled tick that fires
// after the flush hits the !renderPending or stale-gen branches and is
// harmless.
func (m *Model) flushWheelPending() {
	if !m.wheel.renderPending {
		return
	}
	if m.pinDiffCursorTo(m.layout.viewport.YOffset) {
		m.syncTOCActiveSection()
		m.layout.viewport.SetContent(m.renderDiff())
	}
	m.wheel.renderPending = false
	m.wheel.tickInFlight = false
}

// pinDiffCursorTo moves the diff cursor onto the visible viewport range when it
// would otherwise be off-screen at newOffset; when the cursor marker is already
// within the viewport, it is left alone. when the cursor sits above the viewport,
// it is pinned to the topmost visible row; when below, to the bottommost visible
// row. returns true when the cursor actually changed position (idx or annotation
// flag), so callers know whether a re-render is required.
//
// the in-view check uses cursorTop (first visual row, where the cursor marker is
// rendered) rather than cursorBottom. in wrap mode a diff line spans multiple rows;
// using cursorBottom would incorrectly treat the cursor as visible when only its
// tail wrap-continuation rows are in the viewport while the marker row is above it.
//
// for the top-pin case, if the cursor line straddles the viewport top boundary
// (cursorTop < viewTop <= cursorBottom) the target row is advanced past the
// cursor line's last visual row so that the next line — whose marker is inside
// the viewport — is selected. when the entire wrapped span exceeds the viewport
// height the advance is clamped to viewBottom and the function returns false
// (no visible alternative exists).
func (m *Model) pinDiffCursorTo(newOffset int) bool {
	if len(m.file.lines) == 0 {
		return false
	}
	cursorTop, cursorBottom := m.cursorVisualRange()
	viewTop := newOffset
	viewBottom := newOffset + m.layout.viewport.Height - 1
	if cursorTop >= viewTop && cursorTop <= viewBottom {
		return false // cursor marker already visible
	}
	targetRow := viewBottom
	if cursorTop < viewTop {
		// cursor is above the viewport; in wrap mode the cursor line may have
		// continuation rows visible at viewTop — advance past them.
		if cursorBottom >= viewTop {
			targetRow = min(cursorBottom+1, viewBottom)
		} else {
			targetRow = viewTop
		}
	}
	idx, onAnnot := m.visualRowToDiffLine(targetRow)
	if idx == m.nav.diffCursor && onAnnot == m.annot.cursorOnAnnotation {
		return false
	}
	m.nav.diffCursor = idx
	m.annot.cursorOnAnnotation = onAnnot
	return true
}

// clickDiff handles a left-click press in the diff viewport. the click
// focuses the diff pane and moves the diff cursor to the logical line
// under the pointer. when the click lands on an injected annotation
// sub-row, cursorOnAnnotation is set so subsequent navigation treats the
// cursor as being on the annotation rather than the diff line above it.
func (m Model) clickDiff(y int) (tea.Model, tea.Cmd) {
	if m.file.name == "" {
		return m, nil // no file loaded — nothing to focus or point at
	}
	row := (y - m.diffTopRow()) + m.layout.viewport.YOffset
	idx, onAnnot := m.visualRowToDiffLine(row)
	m.layout.focus = paneDiff
	m.nav.diffCursor = idx
	m.annot.cursorOnAnnotation = onAnnot
	m.syncViewportToCursor()
	m.syncTOCActiveSection()
	return m, nil
}

// --- Browser screen mouse support (task 21) -------------------------------
//
// The browser screen has its own hit-testing rather than sharing hitTest
// above: it has no diff viewport and a different column layout (Miller
// columns whose count and x-offsets depend on the narrow-terminal tier from
// browserview.go), but the same top-level gating applies — mouse messages
// only ever reach here when tea.WithMouseCellMotion is enabled, i.e.
// --no-mouse is off (see app/main.go), exactly as for the review screen.
// There is no separate browser-only mouse flag.
//
// Unlike the review screen's wheel (which follows the pointer, matching
// terminal convention), the browser's wheel scrolls whichever pane
// currently holds focus. With only two focusable panes and Tab already the
// established way to move between them, introducing a second,
// pointer-based notion of "the active pane" for wheel alone would fight the
// keyboard model instead of complementing it — especially below
// mediumTierWidth, where only the focused pane is even on screen.

// browserHitZone identifies which browser-screen area a mouse event targets.
type browserHitZone int

const (
	browserHitNone          browserHitZone = iota
	browserHitCurrent                      // the live middle column's entry rows
	browserHitChangedHeader                // the changed pane's "changed - <scope>" header line
	browserHitChanged                      // the changed pane's entry rows
)

// paneContentHeight returns the content-row budget shared by every Miller
// column box: total height minus the status bar row and the box's own
// top/bottom border rows. Mirrors RenderBrowserView's `ph` exactly.
func (b browserScreen) paneContentHeight() int {
	return max(b.height-browserChromeRows, 1)
}

// columnXRanges returns the [start, end) screen-column ranges the current
// and changed-files columns occupy, mirroring browserview.go's
// renderThreeColumns/renderTwoColumns/renderSingleColumn tiering and
// distributeWidths math exactly, so a click always lands on the pane it
// visibly shows. A range of [-1,-1] means that pane is not currently
// rendered (dropped by the tier, or not the focused pane below
// mediumTierWidth).
func (b browserScreen) columnXRanges() (currentX, changedX [2]int) {
	notRendered := [2]int{-1, -1}
	widths := b.effectiveWidths()
	cells, first := browserColumnCells(b.width, widths, b.focus)

	switch {
	case b.width >= wideTierWidth:
		parentBoxW := cells[0] + 2
		currentBoxW := cells[1] + 2
		currentX = [2]int{parentBoxW, parentBoxW + currentBoxW}
		changedX = [2]int{parentBoxW + currentBoxW, b.width}
		return currentX, changedX
	case b.width >= mediumTierWidth:
		currentBoxW := cells[0] + 2
		currentX = [2]int{0, currentBoxW}
		changedX = [2]int{currentBoxW, b.width}
		return currentX, changedX
	default:
		if first == 2 {
			return notRendered, [2]int{0, b.width}
		}
		return [2]int{0, b.width}, notRendered
	}
}

// dividerAt classifies a screen coordinate as sitting on a draggable divider
// between two Miller-column boxes, returning its global index (0 =
// parent|current, 1 = current|changed) or -1 when (x, y) is not on a
// divider. It mirrors columnXRanges' tier logic (and, transitively,
// browserview.go's renderThreeColumns/renderTwoColumns box math) rather than
// re-deriving the geometry independently, so the two never drift apart. A
// divider spans every row the column boxes' borders occupy: row 1 (top
// border) through ph+2 (bottom border), where ph is paneContentHeight().
// Below mediumTierWidth there is a single, undivided pane, so this always
// returns -1 there.
func (b browserScreen) dividerAt(x, y int) int {
	if b.width <= 0 || b.height <= 0 {
		return -1
	}
	ph := b.paneContentHeight()
	if y < 1 || y > ph+2 {
		return -1
	}

	widths := b.effectiveWidths()
	cells, _ := browserColumnCells(b.width, widths, b.focus)
	switch {
	case b.width >= wideTierWidth:
		if x == cells[0]+1 || x == cells[0]+2 {
			return 0
		}
		if x == cells[0]+cells[1]+3 || x == cells[0]+cells[1]+4 {
			return 1
		}
		return -1
	case b.width >= mediumTierWidth:
		if x == cells[0]+1 || x == cells[0]+2 {
			return 1
		}
		return -1
	default:
		return -1
	}
}

// resizeDividerTo recomputes column widths from a divider drag's pointer
// position and writes the result back into b.widths. It reports whether
// b.widths actually changed — a drag that lands back on the same cell
// widths (or targets a tier/divider combination that does not exist) is a
// no-op and returns false, so a caller driving a persistence command off the
// return value never fires one for a non-event.
//
// The pointer position is converted to boundary space and clamped, then
// cascaded, by cascadeResize — see its doc comment for the arithmetic. This
// replaces an earlier pair-conserving scheme (resize the dragged divider's
// two neighbor cells, holding their sum fixed) that could never let a
// starved pair reclaim cells from the third column, the one actually
// hogging them; a pair rehydrated below 2*minColumnWidth (e.g. persisted at
// the minimum on a wide terminal, then reopened much narrower) left every
// subsequent drag on that divider permanently rejected. The cascading clamp
// has no such dead zone: it only refuses a drag when the terminal cannot
// seat every column at minColumnWidth at all.
//
// In the medium tier only divider 1 exists (the parent column is off
// screen), so the write-back preserves the hidden parent's proportion by
// rescaling it against the pair's new combined width, guarding against a
// zero denominator when the previous pair had no width at all.
func (b *browserScreen) resizeDividerTo(divider, x int) bool {
	oldWidths := b.effectiveWidths()

	switch {
	case b.width >= wideTierWidth:
		if divider < 0 || divider > 1 {
			return false
		}
		available := max(b.width-6, 0)
		cells := distributeBrowserWidths(available, []int{oldWidths[0], oldWidths[1], oldWidths[2]})

		newCells, ok := cascadeResize(cells, divider, x)
		if !ok {
			return false
		}

		newWidths := [3]int{newCells[0], newCells[1], newCells[2]}
		if newWidths == oldWidths {
			return false
		}
		b.widths = newWidths
		return true

	case b.width >= mediumTierWidth:
		if divider != 1 {
			return false
		}
		available := max(b.width-4, 0)
		cells := distributeBrowserWidths(available, []int{oldWidths[1], oldWidths[2]})

		newCells, ok := cascadeResize(cells, 0, x)
		if !ok {
			return false
		}

		denom := oldWidths[1] + oldWidths[2]
		parent := max(1, oldWidths[0])
		if denom > 0 {
			parent = max(1, oldWidths[0]*(newCells[0]+newCells[1])/denom)
		}
		newWidths := [3]int{parent, newCells[0], newCells[1]}
		if newWidths == oldWidths {
			return false
		}
		b.widths = newWidths
		return true

	default:
		return false
	}
}

// cascadeResize repositions divider j (0-based, indexing the *visible*
// cells slice — not b.widths' global index) to pointer column x, and
// reports the resulting cell widths, or ok=false when available cannot seat
// every column at minColumnWidth at all.
//
// Screen columns count each divider as occupying 2 cells (the border column
// plus the one beside it, matching dividerAt's 2-wide grab band), so x
// converts to "boundary space" (the cumulative-sum position of divider j)
// as p = x - 2*j - 1. That matches the geometry the render/hit-test side
// already uses: for j == 0 this is the previous scheme's `x - 0 - 1`
// (leftEdge 0), and for j == 1 it is `leftEdge = cells[0]+2, x - leftEdge -
// 1` = `x - cells[0] - 3`, i.e. boundary space `cells[0]+cells[1] = x - 3`
// — independent of cells[0], as required.
//
// p is clamped to [lo, hi], where lo/hi are the coarsest bounds that still
// leave room for every column (0..j) and (j+1..n-1) respectively to reach
// minColumnWidth; boundary j is then pushed to that clamped position, and
// every other boundary is cascaded outward just enough to keep its own pair
// of columns at minColumnWidth — pushing left of j left, and right of j
// right — so a divider dragged toward a neighbor first squeezes that
// neighbor down to the floor and only then, if still short of room, borrows
// from the column beyond it. This is what lets a starved pair reclaim cells
// from the third column, which the old pair-conserving scheme could never
// do.
func cascadeResize(cells []int, j, x int) (out []int, ok bool) {
	n := len(cells)
	total := 0
	for _, c := range cells {
		total += c
	}

	bounds := make([]int, n-1)
	sum := 0
	for i := 0; i < n-1; i++ {
		sum += cells[i]
		bounds[i] = sum
	}

	p := x - 2*j - 1
	lo := minColumnWidth * (j + 1)
	hi := total - minColumnWidth*(n-1-j)
	if lo > hi {
		return nil, false
	}
	bounds[j] = clampInt(p, lo, hi)

	for i := j - 1; i >= 0; i-- {
		bounds[i] = min(bounds[i], bounds[i+1]-minColumnWidth)
	}
	for i := j + 1; i <= n-2; i++ {
		bounds[i] = max(bounds[i], bounds[i-1]+minColumnWidth)
	}

	out = make([]int, n)
	prev := 0
	for i := 0; i < n-1; i++ {
		out[i] = bounds[i] - prev
		prev = bounds[i]
	}
	out[n-1] = total - prev
	return out, true
}

// clampInt restricts v to [lo, hi]. Used by cascadeResize; the caller
// guarantees lo <= hi before calling.
func clampInt(v, lo, hi int) int {
	return max(lo, min(v, hi))
}

// hitTest classifies a browser-screen screen coordinate into a
// browserHitZone plus the entry row within that pane (0-based, before
// scroll-offset translation; -1 when the zone has no per-row meaning, e.g.
// the changed pane's scope header). Row 0 is the path header and row 1 is
// every box's top border. The current column no longer has a header row of
// its own (task 1 dropped the per-column directory name), so its content
// starts at row 2; the changed pane still has its "changed - <scope>" header
// at row 2, so its content starts at row 3 — mirroring diffTopRow's
// border+header accounting in the review screen's own hitTest.
func (b browserScreen) hitTest(x, y int) (zone browserHitZone, row int) {
	if b.width <= 0 || b.height <= 0 || x < 0 || y < 0 || x >= b.width || y >= b.height {
		return browserHitNone, -1
	}
	// Row 0 is the path header and row 1 every box's top border, so box
	// content starts at row 2.
	ph := b.paneContentHeight()
	if y <= 1 || y > ph+1 {
		return browserHitNone, -1 // path header, top border, or bottom border/status bar and beyond
	}

	currentX, changedX := b.columnXRanges()
	inRange := func(r [2]int) bool { return r[0] >= 0 && x >= r[0] && x < r[1] }

	switch {
	case inRange(currentX):
		return browserHitCurrent, y - 2
	case inRange(changedX):
		if y == 2 {
			return browserHitChangedHeader, -1
		}
		return browserHitChanged, y - 3
	default:
		return browserHitNone, -1 // parent column (no cursor of its own) or a gap
	}
}

// handleBrowserMouse routes a tea.MouseMsg to the browser screen: overlay
// dispatch takes priority (mirrors Model.handleOverlayMouse), then wheel and
// left-click are handled; every other button is a no-op, matching the
// review screen's handleMouse.
//
// Left-button handling additionally drives a divider drag (task 4): a press
// that lands on a divider (dividerAt >= 0) starts a drag instead of falling
// through to clickBrowser's ordinary entry-selection behavior; motion events
// while a drag is active recompute the widths via resizeDividerTo, latching
// b.drag.changed once any motion actually moves them; release ends the drag
// and issues persistWidthsCmd (task 5) to save the result via the screen's
// BrowserWidthsPersister, if one is attached — but only when b.drag.changed,
// so a bare click on a divider (no motion at all) never rewrites the config
// file. Motion or release with no drag in progress is a no-op — there is
// nothing to swallow a normal click-drag-elsewhere sequence into.
//
// A release is recognized regardless of which button tea reports it against:
// a terminal without SGR extended mouse mode (mode 1006) reports a release
// via the X10 fallback encoding as Button: MouseButtonNone, which would
// otherwise never reach the MouseButtonLeft case below and leave the drag
// stuck active — so any later button-held motion anywhere on screen would
// keep resizing the divider. A fresh press also unconditionally clears any
// stale drag state before deciding whether it lands on a divider, for the
// same reason.
func (b browserScreen) handleBrowserMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if b.overlay != nil && b.overlay.Active() {
		return b.handleBrowserOverlayMouse(msg)
	}

	if msg.Action == tea.MouseActionRelease && b.drag.active {
		changed := b.drag.changed
		b.drag = browserDrag{}
		if changed {
			return b, b.persistWidthsCmd()
		}
		return b, nil
	}

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if msg.Action != tea.MouseActionPress {
			return b, nil
		}
		return b.handleBrowserWheel(-1)
	case tea.MouseButtonWheelDown:
		if msg.Action != tea.MouseActionPress {
			return b, nil
		}
		return b.handleBrowserWheel(1)
	case tea.MouseButtonLeft:
		switch msg.Action {
		case tea.MouseActionPress:
			b.drag = browserDrag{}
			if d := b.dividerAt(msg.X, msg.Y); d >= 0 {
				b.drag = browserDrag{active: true, divider: d}
				return b, nil
			}
			return b.clickBrowser(msg.X, msg.Y)
		case tea.MouseActionMotion:
			if !b.drag.active {
				return b, nil
			}
			if b.resizeDividerTo(b.drag.divider, msg.X) {
				b.drag.changed = true
			}
			return b, nil
		default:
			return b, nil
		}
	default:
		return b, nil
	}
}

// handleBrowserOverlayMouse routes a mouse event to the browser's active
// overlay (currently only the help overlay, which has no scrollable state
// and simply swallows the event) so it never leaks through to the panes
// underneath.
func (b browserScreen) handleBrowserOverlayMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	b.overlay.HandleMouse(msg)
	return b, nil
}

// handleBrowserWheel scrolls whichever pane currently holds focus by one
// entry per notch, delta > 0 for wheel-down, delta < 0 for wheel-up — see
// the package doc comment above for why focus, not pointer position,
// decides the target pane here.
func (b browserScreen) handleBrowserWheel(delta int) (tea.Model, tea.Cmd) {
	if b.focus == BrowserFocusChanged {
		if b.changed != nil {
			b.changed.MoveCursor(delta)
		}
		return b, nil
	}
	b.nav.MoveCursor(delta)
	return b, nil
}

// clickBrowser dispatches a left-click press at (x, y) to whichever
// browser-screen zone it hit.
func (b browserScreen) clickBrowser(x, y int) (tea.Model, tea.Cmd) {
	zone, row := b.hitTest(x, y)
	switch zone {
	case browserHitCurrent:
		return b.clickCurrentEntry(row)
	case browserHitChangedHeader:
		return b.clickChangedScopeLabel()
	case browserHitChanged:
		return b.clickChangedEntry(row)
	default:
		return b, nil
	}
}

// clickCurrentEntry handles a left-click on the middle column's entry rows.
// row is translated to an absolute index into VisibleEntries via the same
// scroll-window math renderCurrentColumn used to decide what row is even
// visible there. The click always focuses the current column and moves its
// cursor to the entry (this is the "click selects an entry" behavior); when
// the entry is a directory, Nav.Enter is a no-op for anything else it is
// called on unconditionally — the design's "click on a directory enters it"
// falls out of that for free, exactly like pressing Enter would.
func (b browserScreen) clickCurrentEntry(row int) (tea.Model, tea.Cmd) {
	entries := b.nav.VisibleEntries()
	body := b.paneContentHeight()
	offset, _ := paneScrollWindow(len(entries), body, b.nav.Cursor())
	idx := offset + row
	if idx < 0 || idx >= len(entries) {
		return b, nil
	}
	b.focus = BrowserFocusCurrent
	b.nav.SetCursor(idx)
	return b, b.navChanged(b.nav.Enter())
}

// clickChangedEntry handles a left-click on the changed-files pane's entry
// rows: it focuses the changed pane and moves its cursor to the clicked
// entry, so a following Enter (browser_enter) opens review scoped to it —
// the same focus/Enter symmetry Tab plus keyboard Enter already give the
// two panes. Direct field access is safe here: changedPane is defined in
// this same package (changedpane.go).
func (b browserScreen) clickChangedEntry(row int) (tea.Model, tea.Cmd) {
	if b.changed == nil {
		return b, nil
	}
	body := max(b.paneContentHeight()-1, 0)
	offset, _ := paneScrollWindow(len(b.changed.files), body, b.changed.cursor)
	idx := offset + row
	if idx < 0 || idx >= len(b.changed.files) {
		return b, nil
	}
	b.focus = BrowserFocusChanged
	b.changed.cursor = idx
	return b, nil
}

// clickChangedScopeLabel handles a left-click on the changed pane's header
// ("changed - uncommitted" / "changed - branch"): it focuses the changed
// pane and toggles the scope through changedPane.ToggleScope, the exact
// same method ActionBrowserToggleScope (`t`) calls — same cache
// invalidation, no parallel implementation.
func (b browserScreen) clickChangedScopeLabel() (tea.Model, tea.Cmd) {
	if b.changed == nil {
		return b, nil
	}
	b.focus = BrowserFocusChanged
	return b, b.changed.ToggleScope(b.nav.Path())
}

// browserHelpEntry pairs a browser action with its help description. Kept
// in the ui package (rather than added to keymap.defaultDescriptions)
// because browser actions live in their own binding namespace
// (Keymap.browserBindings) that keymap.HelpSections() does not walk —
// see ResolveBrowser's doc comment in app/keymap/keymap.go.
type browserHelpEntry struct {
	action keymap.Action
	desc   string
}

// browserHelpEntries is the ordered list backing the browser screen's help
// overlay "Browser" section — one entry per browser action, in the same
// order as the design's key-bindings table.
var browserHelpEntries = []browserHelpEntry{
	{keymap.ActionBrowserUp, "move cursor"},
	{keymap.ActionBrowserDown, "move cursor"},
	{keymap.ActionBrowserPageUp, "move cursor one page up"},
	{keymap.ActionBrowserPageDown, "move cursor one page down"},
	{keymap.ActionBrowserHome, "jump to the first entry"},
	{keymap.ActionBrowserEnd, "jump to the last entry"},
	{keymap.ActionBrowserEnter, "enter directory / open diff / apply filter"},
	{keymap.ActionBrowserUpLevel, "up one level"},
	{keymap.ActionBrowserFilter, "start filter"},
	{keymap.ActionBrowserDismiss, "cancel filter / close overlay; never quits"},
	{keymap.ActionBrowserReview, "review screen on the whole changeset"},
	{keymap.ActionBrowserToggleScope, "toggle uncommitted / branch scope"},
	{keymap.ActionBrowserRefresh, "refresh changed-files list"},
	{keymap.ActionBrowserToggleHidden, "toggle hidden files"},
	{keymap.ActionBrowserFocusPane, "focus between panes"},
	{keymap.ActionBrowserQuit, "quit"},
	{keymap.ActionBrowserHelp, "show help"},
	{keymap.ActionBrowserToggleFavorite, "star/unstar current directory"},
	{keymap.ActionBrowserFavorites, "open favorites (enter: jump, delete: remove)"},
}

// browserMouseHelpEntries is the browser help overlay's "Mouse" section —
// informational rows with no key binding behind them, discoverable the same
// way the review screen's own "mouse" row is (see the design's Browser key
// bindings table).
var browserMouseHelpEntries = []overlay.HelpEntry{
	{Keys: "Click", Description: "select entry / enter directory / move focus to that pane"},
	{Keys: "Click (changed header)", Description: "toggle uncommitted / branch scope"},
	{Keys: "Wheel", Description: "scroll the focused pane"},
	{Keys: "Drag (column border)", Description: "resize the columns; saved to the config file"},
}

// buildBrowserHelpSpec builds the browser screen's help overlay content: a
// "Browser" section listing every bound browser action (an action with no
// key currently bound to it, e.g. after a user `unmap`, is omitted, mirroring
// Model.buildHelpSpec's own HelpSections behavior), followed by the "Mouse"
// section every browser binding lacks a key for.
func (b browserScreen) buildBrowserHelpSpec() overlay.HelpSpec {
	var entries []overlay.HelpEntry
	for _, e := range browserHelpEntries {
		keys := b.km.KeysForBrowser(e.action)
		if len(keys) == 0 {
			continue
		}
		entries = append(entries, overlay.HelpEntry{
			Keys:        strings.Join(keys, " / "),
			Description: e.desc,
		})
	}

	return overlay.HelpSpec{Sections: []overlay.HelpSection{
		{Title: "Browser", Entries: entries},
		{Title: "Mouse", Entries: browserMouseHelpEntries},
	}}
}
