package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/molenin-moodys/ydiff/app/annotation"
	"github.com/molenin-moodys/ydiff/app/diff"
	"github.com/molenin-moodys/ydiff/app/gitstate"
	"github.com/molenin-moodys/ydiff/app/keymap"
)

// Task 18 guards a very specific failure mode: RootModel re-creating the
// review screen's annotation store on push (`d`) or pop (`q`), silently
// dropping whatever the reviewer already wrote. Upstream already builds one
// *annotation.Store in main.go and injects it into the review Model; these
// tests prove that single instance survives every screen transition the
// root model performs, and that it is exactly the one main.go reads back
// after the program exits to flush stdout/--output and history.

// annotateCurrentLine drives the real annotate-and-save flow through
// RootModel.Update (startAnnotation + typed text + Enter), the same path a
// live key sequence takes, rather than reaching into the store directly.
func annotateCurrentLine(t *testing.T, root RootModel, comment string) RootModel {
	t.Helper()
	require.Equal(t, ScreenReview, root.screen, "precondition: must be in review to annotate")

	rv := root.review
	rv.startAnnotation()
	rv.annot.input.SetValue(comment)
	root.review = rv

	updated, _ := root.Update(tea.KeyMsg{Type: tea.KeyEnter})
	newRoot, ok := updated.(RootModel)
	require.True(t, ok)
	require.False(t, newRoot.review.annot.annotating, "Enter must save and leave annotation mode")
	return newRoot
}

// TestRootModel_Store_ReturnsInjectedInstance asserts that RootModel.Store
// returns exactly the *annotation.Store instance the caller injected via the
// review Model, for both entry points.
func TestRootModel_Store_ReturnsInjectedInstance(t *testing.T) {
	t.Run("browser entry point", func(t *testing.T) {
		dir := t.TempDir()
		nav := newTestNav(t, dir)
		review := testModel(nil, nil)

		root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted)
		assert.Same(t, review.Store(), root.Store())
	})

	t.Run("straight-into-review entry point", func(t *testing.T) {
		review := testModel(nil, nil)
		root := NewRootReview(review)
		assert.Same(t, review.Store(), root.Store())
	})
}

// TestRootModel_AnnotationStore_SurvivesBrowserReviewRoundTrips is the test
// this task exists for: annotate in review, pop back to the browser, enter
// review on a different file, annotate again. If push/pop ever re-created
// the store, the first annotation would vanish silently and a reviewer
// would never know their comment was lost — exiting must produce ONE output
// containing BOTH annotations.
func TestRootModel_AnnotationStore_SurvivesBrowserReviewRoundTrips(t *testing.T) {
	linesA := []diff.DiffLine{{NewNum: 1, Content: "a1", ChangeType: diff.ChangeAdd}}
	linesB := []diff.DiffLine{{NewNum: 1, Content: "b1", ChangeType: diff.ChangeAdd}}

	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel([]string{"a.go", "b.go"}, map[string][]diff.DiffLine{"a.go": linesA, "b.go": linesB})
	review.tree = testNewFileTree([]string{"a.go", "b.go"})
	review.layout.focus = paneDiff
	review.file.name = "a.go"
	review.file.lines = linesA
	review.nav.diffCursor = 0

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted)
	storeAtConstruction := root.Store()

	// push into review (d) and annotate the first file
	updated, _ := root.Update(keyMsg('d'))
	root = updated.(RootModel) //nolint:errcheck // Update always returns a RootModel
	require.Equal(t, ScreenReview, root.screen, "precondition: d pushed the review screen")

	root = annotateCurrentLine(t, root, "comment on a")
	require.Same(t, storeAtConstruction, root.Store(), "store identity must hold immediately after annotating")

	annsA := root.Store().Get("a.go")
	require.Len(t, annsA, 1)
	assert.Equal(t, "comment on a", annsA[0].Comment)

	// pop back to the browser (q) — this must not touch the store at all
	updated, cmd := root.Update(keyMsg('q'))
	root = updated.(RootModel) //nolint:errcheck // Update always returns a RootModel
	require.Equal(t, ScreenBrowser, root.screen, "precondition: popped back to the browser")
	require.Nil(t, cmd, "popping back to the browser must not quit the process")
	require.Same(t, storeAtConstruction, root.Store(), "store must survive the pop unchanged")

	// simulate opening a different file from the browser, then push into
	// review again (d) and annotate it
	rv := root.review
	rv.file.name = "b.go"
	rv.file.lines = linesB
	rv.nav.diffCursor = 0
	rv.layout.focus = paneDiff
	root.review = rv

	updated, _ = root.Update(keyMsg('d'))
	root = updated.(RootModel) //nolint:errcheck // Update always returns a RootModel
	require.Equal(t, ScreenReview, root.screen, "precondition: d pushed the review screen again")
	require.Same(t, storeAtConstruction, root.Store(), "store must not be re-created on the second push")

	root = annotateCurrentLine(t, root, "comment on b")

	finalStore := root.Store()
	require.Same(t, storeAtConstruction, finalStore, "store must be the same instance across both round trips")

	// the first annotation must not have vanished: exiting now produces one
	// output containing both annotations, in the ## file:line (type) format
	// the planning plugin parses.
	output := finalStore.FormatOutput()
	assert.Contains(t, output, "## a.go:1 (+)")
	assert.Contains(t, output, "comment on a")
	assert.Contains(t, output, "## b.go:1 (+)")
	assert.Contains(t, output, "comment on b")

	parsed, err := annotation.Parse(strings.NewReader(output))
	require.NoError(t, err)
	require.Len(t, parsed, 2, "both annotations must round-trip through the same flush format main.go uses")
}

// TestRootModel_AnnotationStore_ExitCodeOnAnnotations_BrowserPath asserts
// that --exit-code-on-annotations (exit code 10 in main.go) is driven by the
// same store regardless of path: annotations that arrived via browser ->
// review -> annotate -> back -> quit must trigger it exactly as annotations
// entered by a straight-into-review session would. main.go's
// annotationExitCode is a pure function of FormatOutput()'s emptiness
// (`enabled && output != ""`); this test proves that condition is satisfied
// after a browser-path round trip, and is not satisfied when nothing was
// annotated.
func TestRootModel_AnnotationStore_ExitCodeOnAnnotations_BrowserPath(t *testing.T) {
	const exitCodeAnnotations = 10 // mirrors main.exitCodeAnnotations; the output contract, not the constant, is shared

	exitCode := func(enabled bool, output string) int {
		if enabled && output != "" {
			return exitCodeAnnotations
		}
		return 0
	}

	lines := []diff.DiffLine{{NewNum: 1, Content: "a1", ChangeType: diff.ChangeAdd}}
	dir := t.TempDir()
	nav := newTestNav(t, dir)
	review := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	review.tree = testNewFileTree([]string{"a.go"})
	review.layout.focus = paneDiff
	review.file.name = "a.go"
	review.file.lines = lines
	review.nav.diffCursor = 0

	root := NewRootBrowser(nav, keymap.Default(), review, nil, gitstate.ScopeUncommitted)

	// no annotations yet: browser -> review -> back to browser without annotating
	updated, _ := root.Update(keyMsg('d'))
	root = updated.(RootModel) //nolint:errcheck // Update always returns a RootModel
	updated, _ = root.Update(keyMsg('q'))
	root = updated.(RootModel) //nolint:errcheck // Update always returns a RootModel
	require.Equal(t, ScreenBrowser, root.screen)
	assert.Equal(t, 0, exitCode(true, root.Store().FormatOutput()), "no annotations must not trigger the exit code")

	// browser -> review -> annotate -> back -> quit
	updated, _ = root.Update(keyMsg('d'))
	root = updated.(RootModel) //nolint:errcheck // Update always returns a RootModel
	root = annotateCurrentLine(t, root, "flag this")
	updated, _ = root.Update(keyMsg('q'))
	root = updated.(RootModel) //nolint:errcheck // Update always returns a RootModel
	require.Equal(t, ScreenBrowser, root.screen)

	output := root.Store().FormatOutput()
	require.NotEmpty(t, output)
	assert.Equal(t, exitCodeAnnotations, exitCode(true, output),
		"annotations that arrived via the browser path must trigger --exit-code-on-annotations")
	assert.Equal(t, 0, exitCode(false, output), "the flag itself still gates the exit code")
}
