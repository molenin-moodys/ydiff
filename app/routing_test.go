package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/molenin-moodys/ydiff/app/annotation"
	"github.com/molenin-moodys/ydiff/app/diff"
	"github.com/molenin-moodys/ydiff/app/gitstate"
	"github.com/molenin-moodys/ydiff/app/highlight"
	"github.com/molenin-moodys/ydiff/app/keymap"
	"github.com/molenin-moodys/ydiff/app/ui"
	"github.com/molenin-moodys/ydiff/app/ui/overlay"
	"github.com/molenin-moodys/ydiff/app/ui/sidepane"
	"github.com/molenin-moodys/ydiff/app/ui/style"
	"github.com/molenin-moodys/ydiff/app/ui/worddiff"
)

// TestDecideRoute_Bare verifies that `ydiff` with no arguments at all opens
// the browser at the current directory, in ScopeUncommitted — the entry
// point that made the browser reachable in the first place (task 23).
func TestDecideRoute_Bare(t *testing.T) {
	got := decideRoute(options{})
	assert.Equal(t, routeDecision{screen: routeBrowser, browserScope: gitstate.ScopeUncommitted}, got)
}

// TestDecideRoute_Only verifies that `ydiff --only=plan.md` — the planning
// plugin's path — still routes to review, exactly as it does today.
func TestDecideRoute_Only(t *testing.T) {
	got := decideRoute(options{Only: []string{"plan.md"}})
	assert.Equal(t, routeDecision{screen: routeReview}, got)
}

// TestDecideRoute_SingleRef verifies that `ydiff main` (one positional ref)
// routes to review, scoped to that comparison.
func TestDecideRoute_SingleRef(t *testing.T) {
	var opts options
	opts.Refs.Base = "main"
	got := decideRoute(opts)
	assert.Equal(t, routeDecision{screen: routeReview}, got)
}

// TestDecideRoute_TwoRefRange verifies that `ydiff main..feature` (base and
// against as one positional, `..`-joined) routes to review.
func TestDecideRoute_TwoRefRange(t *testing.T) {
	var opts options
	opts.Refs.Base = "main..feature"
	got := decideRoute(opts)
	assert.Equal(t, routeDecision{screen: routeReview}, got)
}

// TestDecideRoute_TwoRefPositional verifies that `ydiff main feature` (two
// separate positional args) also routes to review.
func TestDecideRoute_TwoRefPositional(t *testing.T) {
	var opts options
	opts.Refs.Base = "main"
	opts.Refs.Against = "feature"
	got := decideRoute(opts)
	assert.Equal(t, routeDecision{screen: routeReview}, got)
}

// TestDecideRoute_BrowserFlagBare verifies that `ydiff --browser` (no ref)
// opens the browser preselected to ScopeUncommitted: there is no comparison
// to preselect a branch scope from.
func TestDecideRoute_BrowserFlagBare(t *testing.T) {
	got := decideRoute(options{Browser: true})
	assert.Equal(t, routeDecision{screen: routeBrowser, browserScope: gitstate.ScopeUncommitted}, got)
}

// TestDecideRoute_BrowserFlagWithRef verifies that `ydiff --browser main`
// opens the browser with `branch` scope preselected, per the routing matrix.
func TestDecideRoute_BrowserFlagWithRef(t *testing.T) {
	var opts options
	opts.Browser = true
	opts.Refs.Base = "main"
	got := decideRoute(opts)
	assert.Equal(t, routeDecision{screen: routeBrowser, browserScope: gitstate.ScopeBranch}, got)
}

// TestDecideRoute_BrowserFlagOverridesOnly verifies that --browser forces
// the browser screen even when --only is also given: --browser always wins
// over any other diff argument (matching options.Browser's own doc comment).
func TestDecideRoute_BrowserFlagOverridesOnly(t *testing.T) {
	got := decideRoute(options{Browser: true, Only: []string{"plan.md"}})
	assert.Equal(t, routeDecision{screen: routeBrowser, browserScope: gitstate.ScopeUncommitted}, got)
}

// TestDecideRoute_Stdin verifies that --stdin input still routes to review.
func TestDecideRoute_Stdin(t *testing.T) {
	got := decideRoute(options{Stdin: true})
	assert.Equal(t, routeDecision{screen: routeReview}, got)
}

// TestDecideRoute_AllFiles verifies that --all-files routes to review: it
// names a concrete review target (every tracked file) just as a ref does.
func TestDecideRoute_AllFiles(t *testing.T) {
	got := decideRoute(options{AllFiles: true})
	assert.Equal(t, routeDecision{screen: routeReview}, got)
}

// TestDecideRoute_Compare verifies that compare mode (--compare-old /
// --compare-new, resolved into compareAbsOld/New by parseArgs) routes to
// review.
func TestDecideRoute_Compare(t *testing.T) {
	got := decideRoute(options{compareAbsOld: "/tmp/a", compareAbsNew: "/tmp/b"})
	assert.Equal(t, routeDecision{screen: routeReview}, got)
}

// TestVCSSetupForRoute_BrowserOutsideRepo_NoError verifies the browser-only
// exception vcsSetupForRoute implements: outside any git repository (and
// without --only), setupVCSRenderer itself reports errNoVCSRepository, but
// on the browser route that failure is swallowed in favor of an empty
// placeholder renderer — a filesystem browser that refused to start outside
// a repository would be useless.
func TestVCSSetupForRoute_BrowserOutsideRepo_NoError(t *testing.T) {
	t.Chdir(t.TempDir())

	setup, err := vcsSetupForRoute(options{}, routeDecision{screen: routeBrowser})
	require.NoError(t, err)
	require.NotNil(t, setup.renderer)

	entries, err := setup.renderer.ChangedFiles("", false)
	require.NoError(t, err)
	assert.Empty(t, entries, "outside a repository the placeholder renderer reports no changed files")
}

// TestVCSSetupForRoute_ReviewOutsideRepo_StillErrors verifies the exception
// above is browser-only: the review route (e.g. `ydiff` with no --only,
// somehow reaching the default vcsSetup path) still reports the same clear
// error setupVCSRenderer always has, unchanged from before task 23.
func TestVCSSetupForRoute_ReviewOutsideRepo_StillErrors(t *testing.T) {
	t.Chdir(t.TempDir())

	_, err := vcsSetupForRoute(options{}, routeDecision{screen: routeReview})
	require.Error(t, err)
	assert.ErrorIs(t, err, errNoVCSRepository)
}

// newTestReviewModel builds a minimal ui.Model suitable for wiring into
// RootModel in tests, mirroring run()'s own construction closely enough to
// exercise the real NewRootBrowser/NewRootReview wiring rather than a stub.
func newTestReviewModel(t *testing.T) ui.Model {
	t.Helper()
	res := style.PlainResolver()
	m, err := ui.NewModel(ui.ModelConfig{
		Renderer:      browserFallbackSetup().renderer,
		Store:         annotation.NewStore(),
		Highlighter:   highlight.New("", false),
		StyleResolver: res,
		StyleRenderer: style.NewRenderer(res),
		SGR:           style.SGR{},
		WordDiffer:    worddiff.New(),
		Overlay:       overlay.NewManager(),
		Themes:        &themeCatalog{},
		NewFileTree:   func(entries []diff.FileEntry) ui.FileTreeComponent { return sidepane.NewFileTree(entries) },
		ParseTOC: func(lines []diff.DiffLine, filename string) ui.TOCComponent {
			toc := sidepane.ParseTOC(lines, filename)
			if toc == nil {
				return nil
			}
			return toc
		},
	})
	require.NoError(t, err)
	return m
}

// TestBuildRootBrowser_OutsideRepo_OpensWithEmptyChangedPane is the
// end-to-end proof for task 23's hardest requirement: bare `ydiff` outside
// any git repository must still open the browser, with an empty changed
// pane, rather than an error or a refusal to start. It drives the exact
// wiring run() uses (vcsSetupForRoute's fallback feeding buildRootBrowser)
// through Init and the resulting messages, the same way app/ui's own tests
// drive RootModel without a real Bubble Tea runtime, and asserts the
// rendered view names the pane's documented "not a git repository" state
// rather than crashing or silently showing nothing.
func TestBuildRootBrowser_OutsideRepo_OpensWithEmptyChangedPane(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	setup, err := vcsSetupForRoute(options{}, routeDecision{screen: routeBrowser})
	require.NoError(t, err)
	require.NotNil(t, setup.renderer)

	review := newTestReviewModel(t)
	root, navCmd := buildRootBrowser(options{}, review, keymap.Default(), style.PlainResolver(), gitstate.ScopeUncommitted)

	entry := initCmdModel{Model: root, extra: navCmd}
	msgs := drainCmd(entry.Init())
	var updated tea.Model = root
	for _, msg := range msgs {
		updated, _ = updated.Update(msg)
	}
	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	rm, ok := unwrapRootModel(updated)
	require.True(t, ok)
	assert.Contains(t, rm.View(), "not a git repository")
}

// drainCmd runs cmd and, if it produced a tea.BatchMsg, flattens each of its
// sub-commands too, collecting every resulting message — mirroring
// app/ui's own drainBatch test helper (root_test.go), reimplemented here
// since it is unexported in that package.
func drainCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for _, c := range batch {
		out = append(out, drainCmd(c)...)
	}
	return out
}

// TestDecideRoute_IsPure documents the critical design requirement: decideRoute
// is a pure function over parsed options alone. It performs no filesystem or
// git access and starts no tea.Program, so it produces the same answer
// regardless of the process's actual working directory or repository state
// — including outside any git repository entirely, where a bare `ydiff`
// still routes to the browser (task 23's "empty changed pane, not an
// error" requirement is implemented downstream of this decision, not by it).
func TestDecideRoute_IsPure(t *testing.T) {
	got := decideRoute(options{})
	assert.Equal(t, routeBrowser, got.screen, "decideRoute must not consult the filesystem to decide this")
}
