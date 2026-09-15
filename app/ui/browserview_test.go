package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/molenin-moodys/ydiff/app/browser"
	"github.com/molenin-moodys/ydiff/app/ui/style"
)

// browserFixture is a small on-disk tree used to exercise the three-column
// view: parentDir contains currentDir plus a sibling directory, and
// currentDir contains one file and one subdirectory.
type browserFixture struct {
	parentDir  string
	currentDir string
	sibling    string
}

func newBrowserFixture(t *testing.T) browserFixture {
	t.Helper()
	base := t.TempDir()
	parent := filepath.Join(base, "proj")
	sibling := filepath.Join(parent, "sibling")
	current := filepath.Join(parent, "current")

	require.NoError(t, os.MkdirAll(sibling, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(current, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(current, "readme.md"), []byte("x"), 0o600))

	return browserFixture{parentDir: parent, currentDir: current, sibling: sibling}
}

// flattenBatchMsg unwraps a tea.BatchMsg (as returned by browser.NewNav,
// Nav.Enter and Nav.Up, which batch the current and parent column requests)
// one or more levels deep.
func flattenBatchMsg(msg tea.Msg) []tea.Msg {
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, cmd := range batch {
			if cmd == nil {
				continue
			}
			out = append(out, flattenBatchMsg(cmd())...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// applyNavCmd runs cmd and applies every browser.LoadedMsg it produces to nav,
// so tests see a fully loaded Nav rather than one still Pending.
func applyNavCmd(t *testing.T, nav *browser.Nav, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	for _, m := range flattenBatchMsg(cmd()) {
		if loaded, ok := m.(browser.LoadedMsg); ok {
			nav.Apply(loaded)
		}
	}
}

// newLoadedNav builds a Nav rooted at dir with both columns already loaded.
func newLoadedNav(t *testing.T, dir string) *browser.Nav {
	t.Helper()
	nav, cmd := browser.NewNav(dir, false, nil)
	applyNavCmd(t, nav, cmd)
	return nav
}

func testColors() style.Colors {
	return style.Colors{
		Accent:     "#61afef",
		Border:     "#5c6370",
		Normal:     "#abb2bf",
		Muted:      "#5c6370",
		SelectedFg: "#282c34",
		SelectedBg: "#61afef",
		AddFg:      "#98c379",
		RemoveFg:   "#e06c7c",
		SearchFg:   "#000000",
		SearchBg:   "#ffff00",
	}
}

func baseParams(t *testing.T, dir string) BrowserViewParams {
	t.Helper()
	return BrowserViewParams{
		Nav:            newLoadedNav(t, dir),
		Widths:         [3]int{15, 35, 50},
		Width:          120,
		Height:         20,
		Resolver:       style.NewResolver(testColors()),
		Focus:          BrowserFocusCurrent,
		ChangedContent: "M  file.go\nA  new.go",
		ChangedCount:   2,
		ScopeLabel:     "uncommitted",
		Branch:         "main",
	}
}

func TestRenderBrowserView_ThreeColumns(t *testing.T) {
	fx := newBrowserFixture(t)
	p := baseParams(t, fx.currentDir)

	out := RenderBrowserView(p)
	plain := ansi.Strip(out)

	// parent column: no directory-name header (redundant with the path
	// header above the columns); highlights the current directory among its
	// siblings, with no cursor marker.
	assert.Contains(t, plain, "sibling", "parent column lists the current directory's sibling")

	// middle column: the live directory's own entries, no header row.
	assert.Contains(t, plain, "readme.md")
	assert.Contains(t, plain, "sub")

	// right column: frame plus the pre-rendered content this task accepts
	// but does not compute (task 20 supplies real gitstate-backed content).
	assert.Contains(t, plain, "changed - uncommitted")
	assert.Contains(t, plain, "M  file.go")
	assert.Contains(t, plain, "A  new.go")

	// status bar: path, changed count, scope label, and branch name (the
	// design mock-up reads "4 changed - main").
	// the path may be left-truncated to fit the status bar (e.g. a long
	// t.TempDir() path under a 120-column bar), so check its meaningful tail
	// rather than the full string.
	assert.Contains(t, plain, filepath.Base(fx.currentDir), "status bar shows (at least the tail of) the current path")
	assert.Contains(t, plain, "2 changed (uncommitted) - main")
}

func TestRenderBrowserView_ParentColumnHasNoCursor(t *testing.T) {
	fx := newBrowserFixture(t)
	p := baseParams(t, fx.currentDir)

	// White-box: parentRows() (which feeds the left column) must never set
	// the cursor field on any row — only currentRows() (the middle column)
	// does. This is the actual behavioral guarantee behind "the parent
	// column shows the current directory highlighted with no cursor of its
	// own": highlight (accent, bold) and cursor (the theme's selected-row
	// background) are deliberately different rowSpec fields rendered by
	// different branches of renderRow, and only one column's rows ever
	// carries the cursor one.
	parentRows := p.parentRows()
	require.NotEmpty(t, parentRows, "fixture must have parent siblings to make this check meaningful")
	sawHighlight := false
	for _, r := range parentRows {
		assert.False(t, r.cursor, "parent column rows must never carry a cursor")
		if r.highlight {
			sawHighlight = true
		}
	}
	assert.True(t, sawHighlight, "the parent column highlights the entry for the current directory")

	// The middle column, in contrast, does carry a cursor on the entry at
	// the nav's cursor position.
	currentRows := p.currentRows()
	require.NotEmpty(t, currentRows)
	assert.True(t, currentRows[p.Nav.Cursor()].cursor, "the current column's row at the cursor position is marked as the cursor row")

	// The two are rendered through different style-key branches in
	// renderRow (StyleKeyFileSelected vs. StyleKeyDirEntry.Bold), which is
	// the actual distinguishing mechanism; that difference is a color/weight
	// difference lipgloss only emits when attached to a real terminal, so it
	// is not asserted here as an ANSI-string comparison (consistent with how
	// sidepane's own FileSelected usage is tested elsewhere in this repo).

	// Sanity: the view still renders without error for this fixture.
	_ = RenderBrowserView(p)
}

func TestRenderBrowserView_FilterMatchHighlighting(t *testing.T) {
	fx := newBrowserFixture(t)
	nav := newLoadedNav(t, fx.currentDir)
	nav.FilterStart()
	nav.FilterAppend('r')
	nav.FilterAppend('e')
	nav.FilterApply()

	p := baseParams(t, fx.currentDir)
	p.Nav = nav

	out := RenderBrowserView(p)
	plain := ansi.Strip(out)

	assert.Contains(t, plain, "readme.md", "the matching entry stays visible")
	assert.NotContains(t, plain, "sub\n", "the filter narrows the middle column to matching entries only")

	searchFg := string(p.Resolver.Color(style.ColorKeySearchFg))
	assert.Contains(t, out, searchFg, "the matched range is colored with the theme's search-match foreground")

	assert.Contains(t, plain, "filter: re", "status bar shows the active filter query")
}

func TestRenderBrowserView_LoadingPlaceholder(t *testing.T) {
	fx := newBrowserFixture(t)

	start := time.Now()
	now := start
	clock := func() time.Time { return now }

	nav, cmd := browser.NewNav(fx.currentDir, false, clock)
	_ = cmd // deliberately not applied: the column stays "loading"
	now = start.Add(200 * time.Millisecond)

	p := baseParams(t, fx.currentDir)
	p.Nav = nav

	out := RenderBrowserView(p)
	plain := ansi.Strip(out)
	assert.Contains(t, plain, "loading", "a column whose read has been pending long enough shows a placeholder")
}

// TestRenderBrowserView_InlineDirectoryError uses a real permission-denied
// directory (chmod 0o000), not merely a missing one: the design promises
// that "a directory that cannot be read renders its error inline in the
// column", and a missing-path fixture only proves the panic-free path for
// ENOENT, not for EACCES — the two failures are not guaranteed to produce
// the same renderable error shape.
func TestRenderBrowserView_InlineDirectoryError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits do not restrict directory reads on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores permission bits")
	}

	base := t.TempDir()
	locked := filepath.Join(base, "locked")
	require.NoError(t, os.Mkdir(locked, 0o755))
	require.NoError(t, os.Chmod(locked, 0o000))
	// restore permissions so t.TempDir() cleanup can remove the directory afterward.
	t.Cleanup(func() {
		_ = os.Chmod(locked, 0o700)
	})

	nav := newLoadedNav(t, locked)

	p := baseParams(t, base)
	p.Nav = nav

	out := RenderBrowserView(p)
	plain := ansi.Strip(out)
	assert.Contains(t, plain, "error", "an unreadable directory renders its error inline instead of crashing")
}

func TestRenderBrowserView_SymlinkMarkers(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "parent")
	current := filepath.Join(parent, "current")
	require.NoError(t, os.MkdirAll(current, 0o755))

	realDir := filepath.Join(current, "realdir")
	require.NoError(t, os.Mkdir(realDir, 0o755))
	require.NoError(t, os.Symlink(realDir, filepath.Join(current, "link-ok")))
	require.NoError(t, os.Symlink(filepath.Join(current, "nowhere"), filepath.Join(current, "link-broken")))

	nav := newLoadedNav(t, current)
	p := baseParams(t, current)
	p.Nav = nav

	out := RenderBrowserView(p)
	plain := ansi.Strip(out)
	assert.Contains(t, plain, "link-ok ->", "an enterable symlink is marked")
	assert.Contains(t, plain, "link-broken ✗", "a broken symlink is marked and dimmed")
}

func TestRenderBrowserView_NarrowTerminalTiers(t *testing.T) {
	fx := newBrowserFixture(t)

	t.Run(">=100 columns renders all three panes", func(t *testing.T) {
		p := baseParams(t, fx.currentDir)
		p.Width = 100
		out := ansi.Strip(RenderBrowserView(p))
		assert.Contains(t, out, "sibling", "parent column present")
		assert.Contains(t, out, "readme.md", "current column present")
		assert.Contains(t, out, "changed - uncommitted", "changed column present")
	})

	// TestRenderBrowserView_NarrowTerminalTiers's boundary sub-tests below
	// pin the exact tier edges (59/60/99/100) the acceptance review flagged:
	// the surrounding sub-tests only ever probed 100 and mid-range widths
	// (80, 50), so flipping browserview.go's `>= mediumTierWidth` to `>`
	// (or the equivalent off-by-one at wideTierWidth) would pass every one
	// of them undetected.
	t.Run("at exactly 100 columns renders all three panes (wide-tier boundary)", func(t *testing.T) {
		p := baseParams(t, fx.currentDir)
		p.Width = wideTierWidth
		out := ansi.Strip(RenderBrowserView(p))
		assert.Contains(t, out, "sibling", "100 must already be in the wide tier: parent column present")
		assert.Contains(t, out, "readme.md", "current column present")
		assert.Contains(t, out, "changed - uncommitted", "changed column present")
	})

	t.Run("at exactly 99 columns drops the parent column (just below wide-tier boundary)", func(t *testing.T) {
		p := baseParams(t, fx.currentDir)
		p.Width = wideTierWidth - 1
		out := ansi.Strip(RenderBrowserView(p))
		assert.NotContains(t, out, "sibling", "99 must already be in the medium tier: parent column gone")
		assert.Contains(t, out, "readme.md", "current column still shown")
		assert.Contains(t, out, "changed - uncommitted", "changed column still shown")
	})

	t.Run("60-99 columns drops the parent column", func(t *testing.T) {
		p := baseParams(t, fx.currentDir)
		p.Width = 80
		out := ansi.Strip(RenderBrowserView(p))
		assert.NotContains(t, out, "sibling", "parent column's sibling entry is gone")
		assert.Contains(t, out, "readme.md", "current column still shown")
		assert.Contains(t, out, "changed - uncommitted", "changed column still shown")
	})

	t.Run("at exactly 60 columns still renders parent+current+changed (medium-tier boundary)", func(t *testing.T) {
		p := baseParams(t, fx.currentDir)
		p.Width = mediumTierWidth
		out := ansi.Strip(RenderBrowserView(p))
		assert.NotContains(t, out, "sibling", "60 is still the medium tier: parent column gone")
		assert.Contains(t, out, "readme.md", "current column present")
		assert.Contains(t, out, "changed - uncommitted", "changed column present")
	})

	t.Run("at exactly 59 columns renders only the focused pane (just below medium-tier boundary)", func(t *testing.T) {
		p := baseParams(t, fx.currentDir)
		p.Width = mediumTierWidth - 1
		p.Focus = BrowserFocusCurrent
		out := ansi.Strip(RenderBrowserView(p))
		assert.Contains(t, out, "readme.md", "59 must already be in the narrow tier: focused pane present")
		assert.NotContains(t, out, "changed - uncommitted", "59 must already be in the narrow tier: unfocused pane gone")
	})

	t.Run("below 60 columns renders only the focused pane", func(t *testing.T) {
		p := baseParams(t, fx.currentDir)
		p.Width = 50
		p.Focus = BrowserFocusCurrent
		out := ansi.Strip(RenderBrowserView(p))
		assert.Contains(t, out, "readme.md", "focused (current) pane is shown")
		assert.NotContains(t, out, "changed - uncommitted", "unfocused changed pane is not rendered")
		assert.NotContains(t, out, "sibling", "unfocused parent pane is not rendered")

		p.Focus = BrowserFocusChanged
		out = ansi.Strip(RenderBrowserView(p))
		assert.Contains(t, out, "changed - uncommitted", "focused (changed) pane is shown")
		assert.NotContains(t, out, "readme.md", "unfocused current pane is not rendered")
	})

	t.Run("every rendered line fits within the requested width", func(t *testing.T) {
		for _, w := range []int{120, 100, 99, 80, 60, 59, 50, 30} {
			p := baseParams(t, fx.currentDir)
			p.Width = w
			out := RenderBrowserView(p)
			for _, line := range strings.Split(out, "\n") {
				assert.LessOrEqual(t, lipgloss.Width(line), w, "width %d: line %q must fit", w, line)
			}
		}
	})
}

func TestRenderBrowserView_LongEntryNameTruncatesWithoutPanic(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "parent")
	current := filepath.Join(parent, "current")
	require.NoError(t, os.MkdirAll(current, 0o755))

	longASCII := strings.Repeat("very-long-file-name-", 10) + ".txt"
	longMultibyte := strings.Repeat("こんにちは世界", 10) + ".txt"
	require.NoError(t, os.WriteFile(filepath.Join(current, longASCII), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(current, longMultibyte), []byte("x"), 0o600))

	nav := newLoadedNav(t, current)
	p := baseParams(t, current)
	p.Nav = nav
	p.Width = 60

	require.NotPanics(t, func() {
		out := RenderBrowserView(p)
		for _, line := range strings.Split(out, "\n") {
			assert.LessOrEqual(t, lipgloss.Width(line), p.Width, "truncated line must still fit the column width")
		}
	})
}

// TestRenderBrowserView_NoDirectoryNameHeaderRow guards against the
// regression this task fixes: the middle column no longer prints its own
// directory's name as a (non-selectable, misleadingly header-like) row. The
// fixture's current directory is named "fixtures" — exactly the kind of name
// that used to appear as the middle column's header line — and holds a
// single, differently-named file. This checks renderCurrentColumn directly:
// the parent column legitimately lists "fixtures" as the (highlighted) entry
// for the current directory, which is real content, not a header, so that
// column is intentionally not asserted on here.
func TestRenderBrowserView_NoDirectoryNameHeaderRow(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "parent")
	current := filepath.Join(parent, "fixtures")
	require.NoError(t, os.MkdirAll(current, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(current, "a.txt"), []byte("x"), 0o600))

	nav := newLoadedNav(t, current)
	p := baseParams(t, current)
	p.Nav = nav

	out := ansi.Strip(p.renderCurrentColumn(20, 10, true))
	assert.NotContains(t, out, "fixtures", "the current column must not print its own directory name as a header row")
	assert.Contains(t, out, "a.txt", "the current column still shows its real entries")
}

// TestRenderBrowserView_CurrentColumnUsesFullHeight asserts the current
// column now renders ph entry rows (no header row eating one), where it
// previously rendered ph-1.
func TestRenderBrowserView_CurrentColumnUsesFullHeight(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "parent")
	current := filepath.Join(parent, "current")
	require.NoError(t, os.MkdirAll(current, 0o755))
	for i := range 20 {
		require.NoError(t, os.WriteFile(filepath.Join(current, fmt.Sprintf("file-%02d.txt", i)), []byte("x"), 0o600))
	}

	nav := newLoadedNav(t, current)
	p := baseParams(t, current)
	p.Nav = nav

	const ph = 5
	out := ansi.Strip(p.renderCurrentColumn(20, ph, true))
	lines := strings.Split(out, "\n")
	// the bordered box adds a top and bottom border row around its content.
	content := lines[1 : len(lines)-1]
	require.Len(t, content, ph, "current column content must fill the full height with entry rows, no header row")
	for _, line := range content {
		assert.Contains(t, line, "file-", "every content row is an entry row, not a header or blank filler")
	}
}

func TestDistributeWidths(t *testing.T) {
	t.Run("normalizes proportions that do not sum to 100", func(t *testing.T) {
		widths := distributeWidths(100, []int{1, 2, 3})
		total := 0
		for _, w := range widths {
			total += w
		}
		assert.Equal(t, 100, total, "the full available width is always distributed")
	})

	t.Run("degenerate proportions split evenly", func(t *testing.T) {
		widths := distributeWidths(30, []int{0, 0, 0})
		total := 0
		for _, w := range widths {
			total += w
		}
		assert.Equal(t, 30, total)
	})

	t.Run("negative available clamps to zero", func(t *testing.T) {
		widths := distributeWidths(-5, []int{15, 35, 50})
		for _, w := range widths {
			assert.GreaterOrEqual(t, w, 0)
		}
	})
}
