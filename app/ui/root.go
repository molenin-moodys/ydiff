package ui

import (
	"log"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/molenin-moodys/ydiff/app/annotation"
	"github.com/molenin-moodys/ydiff/app/browser"
	"github.com/molenin-moodys/ydiff/app/gitstate"
	"github.com/molenin-moodys/ydiff/app/keymap"
	"github.com/molenin-moodys/ydiff/app/ui/overlay"
	"github.com/molenin-moodys/ydiff/app/ui/style"
)

// Screen identifies which of the two top-level screens is currently active.
// RootModel owns exactly this one piece of mode state and routes Update/View
// to whichever screen it names.
type Screen int

const (
	// ScreenBrowser is the yazi-style filesystem/changed-files browser.
	ScreenBrowser Screen = iota
	// ScreenReview is the revdiff-style diff review screen.
	ScreenReview
)

// RootModel is the top-level bubbletea model. It owns the active Screen and
// delegates Update and View to it.
//
// Two entry points share this one model. Launched with diff arguments — the
// path Claude Code uses — the root starts directly in ScreenReview with
// hasBrowser false: there is no browser behind it, so `q` exits the process
// (see NewRootReview). Launched bare, the root starts in ScreenBrowser with
// hasBrowser true: review becomes a screen that is pushed (`d`) and popped
// (`q`), and `q` in the browser quits (see NewRootBrowser).
type RootModel struct {
	screen     Screen
	hasBrowser bool // false: no browser exists behind review; q in review exits the process

	browser browserScreen
	review  Model

	// reviewInited tracks whether review.Init() has already run. The
	// straight-into-review entry point inits eagerly (RootModel.Init runs
	// it), so this starts true there; the bare-browser entry point defers
	// it until the first push into review (`d`).
	reviewInited bool

	// gitCache and scope are used to invalidate the changed-files cache on
	// return from the review screen, since a file may have just been
	// edited there. gitCache may be nil (e.g. outside a git repository),
	// in which case invalidation is a no-op.
	gitCache *gitstate.Cache
	scope    gitstate.Scope
}

// NewRootReview builds a RootModel that starts directly in ScreenReview with
// no browser behind it. This is the entry point used when ydiff is launched
// with diff arguments (e.g. `ydiff main`, `ydiff --only=plan.md`): `q` exits
// the process rather than revealing a browser that was never there.
func NewRootReview(review Model) RootModel {
	return RootModel{
		screen:       ScreenReview,
		hasBrowser:   false,
		review:       review,
		reviewInited: true,
	}
}

// NewRootBrowser builds a RootModel that starts in ScreenBrowser, with
// review pushed and popped on top of it as the user opens (`d`) and leaves
// (`q`) it. This is the entry point used when ydiff is launched bare.
//
// gitCache and scope are consulted to invalidate the changed-files cache
// when returning from the review screen, and to load the changed-files pane
// (task 20); gitCache may be nil to disable both — the pane then renders
// its own "not a git repository"-style state without ever shelling out.
// resolver themes the browser screen's three columns and changed-files
// pane. A nil km falls back to keymap.Default(). widths are the
// parent/current/changed column proportions (task 22's --browser-widths
// flag); a zero value falls back to defaultBrowserWidths (15/35/50).
func NewRootBrowser(
	nav *browser.Nav, km *keymap.Keymap, review Model, gitCache *gitstate.Cache, scope gitstate.Scope, resolver style.Resolver,
	widths ...[3]int,
) RootModel {
	if km == nil {
		km = keymap.Default()
	}
	w := defaultBrowserWidths
	if len(widths) > 0 && widths[0] != ([3]int{}) {
		w = widths[0]
	}
	return RootModel{
		screen:     ScreenBrowser,
		hasBrowser: true,
		browser: browserScreen{
			nav:      nav,
			km:       km,
			resolver: resolver,
			changed:  newChangedPane(gitCache, scope),
			overlay:  overlay.NewManager(),
			widths:   w,
		},
		review:   review,
		gitCache: gitCache,
		scope:    scope,
	}
}

// WithBrowserWidthsPersister attaches p as the browser screen's
// BrowserWidthsPersister and returns the updated RootModel. It is a
// post-construction setter rather than another parameter on NewRootBrowser,
// whose signature already ends in a variadic widths ...[3]int and has call
// sites and tests that should not churn. A nil p (or never calling this at
// all) makes a divider-drag release a no-op beyond the in-memory width
// change — persistence stays purely additive.
func (r RootModel) WithBrowserWidthsPersister(p BrowserWidthsPersister) RootModel {
	r.browser.persist = p
	return r
}

// Init initializes whichever screen the root starts on. The bare-browser
// entry point's initial directory load command is issued by browser.NewNav
// itself and is the caller's responsibility to run alongside this one; the
// changed-files pane's own initial load is issued by browserScreen.Init.
func (r RootModel) Init() tea.Cmd {
	if r.screen == ScreenReview {
		return r.review.Init()
	}
	return r.browser.Init()
}

// Update routes msg to the active screen, with one exception: a
// tea.WindowSizeMsg always reaches both screens, so a resize while in
// review does not leave the browser stale once the user pops back to it.
func (r RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if wsMsg, ok := msg.(tea.WindowSizeMsg); ok {
		return r.updateBothOnResize(wsMsg)
	}

	if r.screen == ScreenBrowser {
		return r.updateBrowser(msg)
	}
	return r.updateReview(msg)
}

// updateBothOnResize forwards a window resize to both screens (when a
// browser exists), keeping whichever one is currently in the background in
// sync so it renders correctly the moment it becomes active again.
func (r RootModel) updateBothOnResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	if r.hasBrowser {
		bm, cmd := r.browser.Update(msg)
		r.browser = bm.(browserScreen) //nolint:errcheck // browserScreen.Update always returns a browserScreen
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	rm, cmd := r.review.Update(msg)
	r.review = rm.(Model) //nolint:errcheck // Model.Update always returns a Model
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	return r, tea.Batch(cmds...)
}

// updateBrowser handles messages while ScreenBrowser is active. It resolves
// keys through the browser keymap namespace to intercept the two
// screen-transition actions — browser_review (`d`) pushes the review
// screen, browser_quit (`q`) quits the process — before delegating
// everything else to the browser screen itself.
func (r RootModel) updateBrowser(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		// while the browser's own help overlay is open, every key belongs
		// to it (close on ?/Esc, swallow everything else) — never to the
		// screen-transition shortcuts below, or `q` would quit the process
		// instead of closing help, and `d`/changed-file Enter would push
		// review out from under an open overlay.
		if r.browser.overlay != nil && r.browser.overlay.Active() {
			bm, cmd := r.browser.Update(msg)
			r.browser = bm.(browserScreen) //nolint:errcheck // browserScreen.Update always returns a browserScreen
			return r, cmd
		}
		filterActive := r.browser.nav.Filter().Editing()
		switch r.browser.km.ResolveBrowser(keyMsg.String(), filterActive) {
		case keymap.ActionBrowserQuit:
			return r, tea.Quit
		case keymap.ActionBrowserReview:
			return r.enterReview(nil)
		case keymap.ActionBrowserEnter:
			if path, ok := r.browser.ReviewTarget(); ok {
				return r.enterReview([]string{path})
			}
			// not a changed-file target (a directory, the filter box, or no
			// changed-files match): fall through to the browser screen's own
			// handling below, which drives ordinary navigation instead.
		default:
			// fall through to the browser screen's own handling below
		}
	}

	bm, cmd := r.browser.Update(msg)
	r.browser = bm.(browserScreen) //nolint:errcheck // browserScreen.Update always returns a browserScreen
	return r, cmd
}

// enterReview pushes the review screen scoped to only (nil to review
// everything, as `d` always does). Review is initialized the first time
// this runs; on every later call — a repeated `d`, or a changed-file Enter
// after review has already been opened once — it instead re-triggers a
// reload so the review screen picks up the new scope. triggerReload (unlike
// applyReloadCleanup) never clears the annotation store, so annotations
// made in an earlier visit survive this re-scoping (task 18's invariant).
func (r RootModel) enterReview(only []string) (tea.Model, tea.Cmd) {
	r.review.cfg.only = only
	r.screen = ScreenReview
	if !r.reviewInited {
		r.reviewInited = true
		return r, r.review.Init()
	}
	return r, r.review.triggerReload()
}

// updateReview handles messages while ScreenReview is active. A key that
// would resolve to the review screen's quit action is intercepted here: with
// a browser behind it, `q` pops back to the browser (invalidating the
// changed-files cache, since a file may have just been edited) instead of
// exiting the process; with no browser behind it, the quit propagates
// exactly as it would in an unmodified review screen.
func (r RootModel) updateReview(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && r.review.isQuitKey(keyMsg) {
		if !r.hasBrowser {
			return r, tea.Quit
		}
		r.screen = ScreenBrowser
		r.invalidateGitCache()
		return r, nil
	}

	rm, cmd := r.review.Update(msg)
	r.review = rm.(Model) //nolint:errcheck // Model.Update always returns a Model
	return r, cmd
}

// invalidateGitCache drops the cached changed-files list for the repository
// the browser is currently standing in, forcing the next read to recompute
// it. Called on every return from the review screen: a file may have just
// been edited there. A no-op when no cache was supplied, or when the
// browser's current directory is not inside a git repository.
//
// The scope invalidated is the changed-files pane's own current scope
// (b.browser.changed.scope), not r.scope: r.scope only records the scope
// NewRootBrowser was constructed with, and the pane's scope moves
// independently of it once the user presses `t` (browserScreen.handleKey's
// ActionBrowserToggleScope case) — reading r.scope here instead would
// invalidate the wrong cache entry after a scope toggle.
func (r *RootModel) invalidateGitCache() {
	if r.gitCache == nil || r.browser.nav == nil {
		return
	}
	repo, err := gitstate.Resolve(r.browser.nav.Path())
	if err != nil {
		return
	}
	scope := r.scope
	if r.browser.changed != nil {
		scope = r.browser.changed.scope
	}
	r.gitCache.Invalidate(repo.Root, scope)
}

// Store returns the annotation store owned by the review screen. Both entry
// points inject one instance at construction (see NewRootReview,
// NewRootBrowser) and Update never replaces r.review, only mutates it in
// place — so this returns the exact same *annotation.Store regardless of
// which screen is active or how many times review has been pushed and
// popped. main.go reads it back through here (once the root model is wired
// into the entry point) to flush annotations to stdout/--output and to
// history after the program exits, exactly as it does today when reading a
// bare review Model directly.
func (r RootModel) Store() *annotation.Store {
	return r.review.Store()
}

// Discarded reports whether the user discarded annotations and quit,
// exactly as the review screen's own Model.Discarded would. Forwarded for
// the same reason as Store: main.go's post-run flush path (task 23) checks
// this on the final *RootModel* tea.Program.Run returns, and must see the
// same answer it would have read from a bare review Model before the
// browser entry point existed.
func (r RootModel) Discarded() bool {
	return r.review.Discarded()
}

// BrowserPath reports the directory the browser was last standing in, and
// false when this invocation had no browser at all (the straight-into-review
// entry point). main.go writes it to --cwd-file so a shell wrapper can cd
// there on exit, the way yazi's `y` function does.
func (r RootModel) BrowserPath() (string, bool) {
	if !r.hasBrowser || r.browser.nav == nil {
		return "", false
	}
	return r.browser.nav.Path(), true
}

// View renders whichever screen is currently active.
func (r RootModel) View() string {
	if r.screen == ScreenBrowser {
		return r.browser.View()
	}
	return r.review.View()
}

// isQuitKey reports whether msg would resolve to the review screen's quit
// action if Model.handleKey ran it directly. It replicates handleKey's guard
// order (confirm-discard, pending reload, pending chord, then the annotate /
// search / overlay modal dispatch) so RootModel can intercept the quit
// action for screen routing without misreading a `q` that a modal would
// otherwise consume as literal input or a different command.
func (m Model) isQuitKey(msg tea.KeyMsg) bool {
	if m.inConfirmDiscard || m.reload.pending || m.keys.chordPending != "" {
		return false
	}
	if m.annot.annotating || m.search.active || m.overlay.Active() {
		return false
	}
	return m.keymap.Resolve(msg.String()) == keymap.ActionQuit
}

// browserScreen is the routing-level wrapper around browser.Nav and the
// changed-files pane: the piece of state that satisfies tea.Model so
// RootModel can delegate Update/View to it, rendered via RenderBrowserView
// (browserview.go, task 19).
type browserScreen struct {
	nav *browser.Nav
	km  *keymap.Keymap

	changed  *changedPane
	focus    BrowserFocus
	resolver style.Resolver
	overlay  *overlay.Manager

	// widths are the parent/current/changed column proportions (task 22's
	// --browser-widths flag). A zero value means "use defaultBrowserWidths".
	widths [3]int

	// drag tracks an in-progress divider drag (task 4's mouse-driven pane
	// resize). It is plain value state on browserScreen like everything else
	// here: RootModel.updateBrowser assigns the updated browserScreen back
	// after every Update, so the drag survives across the press/motion/
	// release sequence of mouse events the same way widths or focus does.
	drag browserDrag

	// persist saves dragged column widths outside the process (task 5). nil
	// makes a drag release a no-op beyond the in-memory width change, so
	// screens built without WithBrowserWidthsPersister behave exactly as
	// before this task.
	persist BrowserWidthsPersister

	width, height int
}

// browserDrag is the in-progress state of a divider drag: which divider (if
// any) is currently being dragged. active is false between drags; divider is
// only meaningful while active is true.
type browserDrag struct {
	active  bool
	divider int
}

// BrowserWidthsPersister is the consumer-side interface app/ui declares for
// saving the browser's dragged column widths outside the process (task 22's
// --browser-widths flag persisted back to the config file). Per this
// project's architecture principle ("consumer-side interfaces for external
// deps"), app/ui only depends on this interface — the concrete
// implementation (patching an INI config file) lives in the main package,
// which never gets imported here.
type BrowserWidthsPersister interface {
	PersistBrowserWidths(widths [3]int) error
}

// persistWidthsCmd returns a tea.Cmd that saves b's current effective
// widths via b.persist, or nil when no persister is attached (the default —
// screens built without WithBrowserWidthsPersister never issue this
// command). A failed save is logged as a [WARN] and otherwise ignored: a
// broken config file write must never take the session down or block the
// UI, so this never returns an error-carrying message for the caller to
// react to.
func (b browserScreen) persistWidthsCmd() tea.Cmd {
	if b.persist == nil {
		return nil
	}
	widths := b.effectiveWidths()
	persist := b.persist
	return func() tea.Msg {
		if err := persist.PersistBrowserWidths(widths); err != nil {
			log.Printf("[WARN] persist browser widths: %v", err)
		}
		return nil
	}
}

// effectiveWidths returns b.widths, falling back to defaultBrowserWidths
// when it is unset (the zero value), so screens built without going through
// NewRootBrowser's widths parameter still render at the documented default.
func (b browserScreen) effectiveWidths() [3]int {
	if b.widths == ([3]int{}) {
		return defaultBrowserWidths
	}
	return b.widths
}

// Init issues the changed-files pane's initial load for the browser's
// starting directory. The filesystem column's own initial load comes from
// browser.NewNav itself, run by the caller alongside RootModel's own Init.
func (b browserScreen) Init() tea.Cmd {
	if b.changed == nil {
		return nil
	}
	return b.changed.Request(b.nav.Path())
}

// Update handles messages for the browser screen: window resizes update the
// stored dimensions, browser.LoadedMsg results are applied to the
// navigation state, GitLoadedMsg results are applied to the changed-files
// pane (discarded by changedPane.Apply if stale), and keys are resolved
// through the browser keymap namespace and dispatched accordingly.
func (b browserScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		b.width, b.height = msg.Width, msg.Height
		// The geometry a drag started from (column x-ranges, divider
		// positions) no longer exists once the terminal resizes, so any
		// in-progress drag is abandoned rather than resuming against stale
		// coordinates.
		b.drag = browserDrag{}
		return b, nil
	case browser.LoadedMsg:
		b.nav.Apply(msg)
		return b, nil
	case GitLoadedMsg:
		if b.changed != nil {
			b.changed.Apply(msg)
		}
		return b, nil
	case tea.KeyMsg:
		return b.handleKey(msg)
	case tea.MouseMsg:
		return b.handleBrowserMouse(msg)
	default:
		return b, nil
	}
}

// handleKey dispatches one browser-namespace action per key. Screen
// transitions (browser_review, browser_quit) and browser_enter's
// "open review scoped to this changed file" case are intercepted by
// RootModel (via ReviewTarget) before this is reached — browser_enter here
// only ever means "enter this directory" or "apply the filter", the cases
// ReviewTarget declined.
func (b browserScreen) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if b.overlay != nil && b.overlay.Active() {
		return b.handleBrowserOverlayKey(msg)
	}
	filterActive := b.nav.Filter().Editing()
	action := b.km.ResolveBrowser(msg.String(), filterActive)

	// while the filter is being typed, ResolveBrowser deliberately returns
	// the empty Action for every key except Enter/Esc (see its doc comment)
	// so ordinary letters reach here as literal filter input instead of
	// commands. Route them into the filter query before the action switch
	// below, which only ever sees Enter/Esc/empty while editing.
	if filterActive && action == "" {
		switch msg.Type {
		case tea.KeyRunes:
			for _, r := range msg.Runes {
				b.nav.FilterAppend(r)
			}
		case tea.KeyBackspace:
			b.nav.FilterBackspace()
		}
		return b, nil
	}

	switch action {
	case keymap.ActionBrowserUp:
		b.moveCursor(-1)
	case keymap.ActionBrowserDown:
		b.moveCursor(1)
	case keymap.ActionBrowserPageUp:
		b.moveCursor(-b.paneContentHeight())
	case keymap.ActionBrowserPageDown:
		b.moveCursor(b.paneContentHeight())
	case keymap.ActionBrowserHome:
		b.moveCursor(-cursorJumpToEdge)
	case keymap.ActionBrowserEnd:
		b.moveCursor(cursorJumpToEdge)
	case keymap.ActionBrowserEnter:
		if b.nav.Filter().Editing() {
			b.nav.FilterApply()
			return b, nil
		}
		if b.focus == BrowserFocusChanged {
			return b, nil // no changed-file match under the cursor: no-op
		}
		return b, b.navChanged(b.nav.Enter())
	case keymap.ActionBrowserUpLevel:
		// With the changed-files pane focused, left is the natural inverse
		// of the Tab that got you there: it returns focus to the directory
		// columns rather than navigating the filesystem out from under a
		// pane the cursor is not even in.
		if b.focus == BrowserFocusChanged {
			b.focus = BrowserFocusCurrent
			return b, nil
		}
		return b, b.navChanged(b.nav.Up())
	case keymap.ActionBrowserFilter:
		b.nav.FilterStart()
	case keymap.ActionBrowserDismiss:
		b.nav.FilterCancel()
	case keymap.ActionBrowserToggleHidden:
		return b, b.nav.ToggleHidden()
	case keymap.ActionBrowserToggleScope:
		if b.changed != nil {
			return b, b.changed.ToggleScope(b.nav.Path())
		}
	case keymap.ActionBrowserRefresh:
		if b.changed != nil {
			return b, b.changed.Refresh(b.nav.Path())
		}
	case keymap.ActionBrowserFocusPane:
		if b.focus == BrowserFocusCurrent {
			b.focus = BrowserFocusChanged
		} else {
			b.focus = BrowserFocusCurrent
		}
	case keymap.ActionBrowserHelp:
		if b.overlay != nil {
			b.overlay.OpenHelp(b.buildBrowserHelpSpec())
		}
	default:
		// browser_review, browser_quit, and browser_enter's changed-file
		// case are all intercepted by RootModel before this is reached:
		// no-op here.
	}
	return b, nil
}

// handleBrowserOverlayKey delegates a key to the active overlay (help, for
// now the only browser overlay) instead of ordinary browser navigation,
// mirroring how the review screen's overlay.Manager intercepts keys ahead of
// Model.handleKey. RootModel.updateBrowser guards the screen-transition keys
// the same way before this is ever reached.
func (b browserScreen) handleBrowserOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filterActive := b.nav.Filter().Editing()
	action := b.km.ResolveBrowser(msg.String(), filterActive)
	b.overlay.HandleKey(msg, action)
	return b, nil
}

// moveCursor moves whichever pane currently holds focus, so Up/Down behave
// identically in the middle column and the changed-files pane.
// cursorJumpToEdge is the delta Home and End pass to moveCursor. Both cursor
// implementations clamp to their list bounds — Nav against the *filtered*
// visible count, changedPane against its file count — so an oversized delta
// lands exactly on the first or last entry without either pane needing to
// expose its length here. Kept well below math.MaxInt32 so cursor+delta
// cannot overflow.
const cursorJumpToEdge = 1 << 30

func (b browserScreen) moveCursor(delta int) {
	if b.focus == BrowserFocusChanged {
		if b.changed != nil {
			b.changed.MoveCursor(delta)
		}
		return
	}
	b.nav.MoveCursor(delta)
}

// navChanged batches navCmd (a browser.Nav navigation command, or nil for a
// no-op move) alongside a changed-files reload for the navigation's
// destination directory. Enter and Up update nav's path synchronously
// before returning their (asynchronous, listing-only) load command — see
// Nav.Enter's and Nav.Up's doc comments — so b.nav.Path() already names the
// destination directory here, even though navCmd has not run yet. Batching
// a reload alongside every successful navigation this way is what makes
// repository re-resolution on navigation work, rather than only reloading
// on scope toggle/refresh.
func (b browserScreen) navChanged(navCmd tea.Cmd) tea.Cmd {
	if navCmd == nil || b.changed == nil {
		return navCmd
	}
	return tea.Batch(navCmd, b.changed.Request(b.nav.Path()))
}

// ReviewTarget reports the path Enter should open in review, and whether
// there is one, implementing the design's symmetry rule: Enter on a changed
// file opens review scoped to it, in the changed-files pane or in the
// middle column alike, and does nothing if the file is unchanged.
// RootModel.updateBrowser consults this before ordinary browser_enter
// handling runs, so a directory entry (declined here) still falls through
// to nav.Enter's ordinary "navigate into it" behavior.
func (b browserScreen) ReviewTarget() (path string, ok bool) {
	if b.changed == nil {
		return "", false
	}
	if b.focus == BrowserFocusChanged {
		return b.changed.Selected()
	}
	if b.nav.Filter().Editing() {
		return "", false
	}
	entries := b.nav.VisibleEntries()
	cursor := b.nav.Cursor()
	if cursor < 0 || cursor >= len(entries) {
		return "", false
	}
	entry := entries[cursor].Entry
	if entry.Enterable() {
		return "", false // directory: ordinary navigation instead
	}
	if b.changed.root == "" {
		return "", false // not currently inside a resolved repository
	}
	abs := filepath.Join(b.nav.Path(), entry.Name)
	rel, err := filepath.Rel(b.changed.root, abs)
	if err != nil {
		return "", false
	}
	return b.changed.MatchPath(filepath.ToSlash(rel))
}

// changedWidth returns the column width the changed-files pane will be
// rendered at, mirroring RenderBrowserView's own narrow-terminal tiering and
// distributeWidths math exactly (browserview.go) so the pre-rendered
// content b.changed.Render produces always matches the box it is placed
// into. widths are the parent/current/changed proportions RenderBrowserView
// is about to be called with.
func changedWidth(totalWidth int, widths [3]int) int {
	switch {
	case totalWidth >= wideTierWidth:
		available := max(totalWidth-6, 0)
		return distributeWidths(available, []int{widths[0], widths[1], widths[2]})[2]
	case totalWidth >= mediumTierWidth:
		available := max(totalWidth-4, 0)
		return distributeWidths(available, []int{widths[1], widths[2]})[1]
	default:
		return max(totalWidth-2, 0)
	}
}

// defaultBrowserWidths are the parent/current/changed column proportions
// used until the --browser-widths flag (a later task) supplies a real one.
var defaultBrowserWidths = [3]int{15, 35, 50}

// View renders the browser screen: the three-column Miller view plus status
// bar (RenderBrowserView, task 19), with the changed-files pane's content
// pre-rendered at the exact width that view will place it into.
func (b browserScreen) View() string {
	ph := b.paneContentHeight() // derived from browserChromeRows, mirroring RenderBrowserView exactly
	bodyHeight := max(ph-1, 0)  // minus the changed pane's own header line, mirroring renderChangedColumn

	widths := b.effectiveWidths()

	var content string
	var count int
	var scopeLabel, branch string
	if b.changed != nil {
		cw := changedWidth(b.width, widths)
		content = b.changed.Render(b.resolver, cw, bodyHeight, b.focus == BrowserFocusChanged)
		count = b.changed.count()
		scopeLabel = string(b.changed.scope)
		branch = b.changed.branch
	}

	out := RenderBrowserView(BrowserViewParams{
		Nav:            b.nav,
		Widths:         widths,
		Width:          b.width,
		Height:         b.height,
		Resolver:       b.resolver,
		Focus:          b.focus,
		ChangedContent: content,
		ChangedCount:   count,
		ScopeLabel:     scopeLabel,
		Branch:         branch,
	})
	if b.overlay != nil {
		out = b.overlay.Compose(out, overlay.RenderCtx{Width: b.width, Height: b.height, Resolver: b.resolver})
	}
	return out
}
