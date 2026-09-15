package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/molenin-moodys/ydiff/app/annotation"
	"github.com/molenin-moodys/ydiff/app/browser"
	"github.com/molenin-moodys/ydiff/app/gitstate"
	"github.com/molenin-moodys/ydiff/app/keymap"
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
// when returning from the review screen; gitCache may be nil to disable
// invalidation. A nil km falls back to keymap.Default().
func NewRootBrowser(nav *browser.Nav, km *keymap.Keymap, review Model, gitCache *gitstate.Cache, scope gitstate.Scope) RootModel {
	if km == nil {
		km = keymap.Default()
	}
	return RootModel{
		screen:     ScreenBrowser,
		hasBrowser: true,
		browser:    browserScreen{nav: nav, km: km},
		review:     review,
		gitCache:   gitCache,
		scope:      scope,
	}
}

// Init initializes whichever screen the root starts on. The bare-browser
// entry point's initial directory load command is issued by browser.NewNav
// itself and is the caller's responsibility to run alongside this one
// (there is nothing further for the browser screen to init here).
func (r RootModel) Init() tea.Cmd {
	if r.screen == ScreenReview {
		return r.review.Init()
	}
	return nil
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
		filterActive := r.browser.nav.Filter().Editing()
		switch r.browser.km.ResolveBrowser(keyMsg.String(), filterActive) {
		case keymap.ActionBrowserQuit:
			return r, tea.Quit
		case keymap.ActionBrowserReview:
			r.screen = ScreenReview
			var cmd tea.Cmd
			if !r.reviewInited {
				cmd = r.review.Init()
				r.reviewInited = true
			}
			return r, cmd
		default:
			// fall through to the browser screen's own handling below
		}
	}

	bm, cmd := r.browser.Update(msg)
	r.browser = bm.(browserScreen) //nolint:errcheck // browserScreen.Update always returns a browserScreen
	return r, cmd
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
func (r *RootModel) invalidateGitCache() {
	if r.gitCache == nil || r.browser.nav == nil {
		return
	}
	repo, err := gitstate.Resolve(r.browser.nav.Path())
	if err != nil {
		return
	}
	r.gitCache.Invalidate(repo.Root, r.scope)
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

// browserScreen is the routing-level wrapper around browser.Nav: the piece
// of the browser package's navigation state that satisfies tea.Model so
// RootModel can delegate Update/View to it. Rendering here is a minimal
// placeholder; RenderBrowserView (browserview.go, task 19) renders the real
// three-column view and is wired in here once the changed-files pane (task
// 20) has real content and focus-switching to hand it.
type browserScreen struct {
	nav *browser.Nav
	km  *keymap.Keymap

	width, height int
}

// Init satisfies tea.Model. The browser's initial directory-load command
// comes from browser.NewNav itself, run by the caller alongside RootModel's
// own Init; there is nothing further to init here.
func (b browserScreen) Init() tea.Cmd { return nil }

// Update handles messages for the browser screen: window resizes update the
// stored dimensions, browser.LoadedMsg results are applied to the
// navigation state, and keys are resolved through the browser keymap
// namespace and dispatched to the corresponding Nav method.
func (b browserScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		b.width, b.height = msg.Width, msg.Height
		return b, nil
	case browser.LoadedMsg:
		b.nav.Apply(msg)
		return b, nil
	case tea.KeyMsg:
		return b.handleKey(msg)
	default:
		return b, nil
	}
}

// handleKey dispatches one browser-namespace action per key. Screen
// transitions (browser_review, browser_quit) are intercepted by RootModel
// before this is reached; actions belonging to panes not yet wired (scope
// toggle, refresh, hidden-file toggle, pane focus, help) are left for the
// tasks that implement those panes.
func (b browserScreen) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filterActive := b.nav.Filter().Editing()
	switch b.km.ResolveBrowser(msg.String(), filterActive) {
	case keymap.ActionBrowserUp:
		b.nav.MoveCursor(-1)
	case keymap.ActionBrowserDown:
		b.nav.MoveCursor(1)
	case keymap.ActionBrowserEnter:
		if b.nav.Filter().Editing() {
			b.nav.FilterApply()
			return b, nil
		}
		return b, b.nav.Enter()
	case keymap.ActionBrowserUpLevel:
		return b, b.nav.Up()
	case keymap.ActionBrowserFilter:
		b.nav.FilterStart()
	case keymap.ActionBrowserDismiss:
		b.nav.FilterCancel()
	default:
		// browser_review, browser_quit (intercepted by RootModel), and every
		// action for a pane not yet wired: no-op here.
	}
	return b, nil
}

// View renders the browser screen. Placeholder until the three-column view
// (task 19) and changed-files pane (task 20) are wired in.
func (b browserScreen) View() string {
	return "ydiff — " + b.nav.Path()
}
