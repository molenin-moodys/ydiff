# Browser: drop column headers, make pane widths draggable and persistent

## Overview

Two user-reported problems with the browser screen (`ScreenBrowser`):

1. **Every Miller column prints its directory name as a header row.** Now that the
   view has a path header at the very top (added in commit `c0dbeae`), the
   per-column name is redundant — and worse, it *looks* selectable while it is
   not, so it reads as a broken row. The directory-name headers go away. The
   changed-files pane keeps its own `changed - <scope>` header: that is not a
   directory name, and clicking it toggles the scope (`browserHitChangedHeader`).
2. **Pane widths are fixed at `--browser-widths` and cannot be adjusted.** The
   user wants to drag the border between panes with the mouse, and wants the
   result remembered across runs.

A latent bug found while reading the code is fixed as part of this work:
`browserScreen.View()` computes its pane height as `b.height-3` while
`RenderBrowserView` uses `b.height-browserChromeRows` (4). The changed-files
pane's content is therefore rendered one row taller than the box it lands in;
`renderChangedColumn` silently truncates it, but `paneScrollWindow` is fed the
wrong height, so once the list scrolls, the row a click maps to can be off by
one.

## Context (from discovery)

- `app/ui/browserview.go` — `RenderBrowserView`, `renderColumnBox`,
  `renderParentColumn`, `renderCurrentColumn`, `renderChangedColumn`,
  `columnHeader`, `distributeWidths`, `browserChromeRows`, the width tiers.
- `app/ui/mouse.go` — `browserScreen.paneContentHeight`, `columnXRanges`,
  `hitTest`, `handleBrowserMouse`, `clickBrowser`, `clickCurrentEntry`,
  `clickChangedEntry`, `browserMouseHelpEntries`.
- `app/ui/root.go` — `browserScreen` struct, `effectiveWidths`,
  `defaultBrowserWidths`, `browserScreen.View`, `RootModel.updateBrowser`
  (which assigns the updated `browserScreen` back, so value-receiver mutations
  survive).
- `app/themes.go` — `themeCatalog.patchConfigTheme`, `scanThemeLines`,
  `defaultSectionInsertIdx`: an existing, well-tested INI single-key patcher
  that persists the selected theme into `~/.config/ydiff/config`.
- `app/config.go` — `BrowserWidths` flag (`default:"15,35,50"`),
  `parseBrowserWidths`, `options.ResolvedBrowserWidths`.
- `app/main.go` — `configPath` (line ~203) and `buildRootBrowser` (line ~450),
  both inside `run()`'s scope.
- Verified in the vendored go-flags: `ini.go:599` sets `opt.preventDefault =
  true` for every key read from the config file, and `parser.go:317`'s
  `clearDefault()` is a no-op for such options. A `browser-widths` line written
  into the config file therefore survives `p.ParseArgs`, exactly like `theme`
  does, despite the flag carrying a `default:` tag.

## Development Approach

- **testing approach**: TDD — every package touched here already has a matching
  `_test.go`, and the browser view/mouse math is pure enough to test directly.
- complete each task fully before moving to the next
- **every task MUST include new/updated tests**
- **all tests must pass before starting the next task**
- Run the suite with `go test ./...` (the `Makefile`'s `make test` adds the race
  detector and coverage). `/opt/homebrew/bin/go` is the toolchain.
- **Known environmental failure, pre-existing and NOT to be fixed**:
  `TestGit_FileBlame_UsesIndexForStagedDiffs` in `app/diff` fails on this
  machine's git 2.33.0. The gate is "no NEW failures", not "zero failures".
- `golangci-lint` is not installed on this machine. Report lint steps as
  skipped; never claim a lint run that did not happen.

## Testing Strategy

- **unit tests**: required for every task.
- No e2e/browser test framework in this project; `app/e2e_contract_test.go`
  drives the real bubbletea model in-process and must keep passing.

## Progress Tracking

- mark completed items with `[x]` immediately when done
- add newly discovered tasks with ➕ prefix
- document issues/blockers with ⚠️ prefix

## Solution Overview

**Headers.** `renderColumnBox` stops emitting a header line; the parent and
current columns hand their full `height` to `visibleWindow` instead of
`height-1`. `columnHeader` loses its last caller and is deleted with its test.
`renderChangedColumn` is untouched.

**Row math.** Removing the header shifts the current column's content up by one
row while the changed pane's stays put, so the two panes now have *different*
content offsets. `hitTest` gains that per-pane split; `clickCurrentEntry` uses
the full `paneContentHeight()` where it previously subtracted the header row.

**Dragging.** The border columns between two boxes become a grab band. A
left-press there starts a drag instead of selecting an entry; motion with the
button held recomputes the column widths from the pointer's x; release ends the
drag and persists. The new widths, measured in cells, are written straight back
into `browserScreen.widths` — `distributeWidths` normalizes proportions, so cell
counts *are* valid proportions and the ratio survives a terminal resize.

**Persistence.** `app/ui` must not do OS work (see CLAUDE.md's architecture
principles), so it declares a consumer-side `BrowserWidthsPersister` interface
and the `main` package implements it by patching `browser-widths = a,b,c` into
the INI config file — the same mechanism, generalized, that already persists the
selected theme.

## Technical Details

### Geometry after the change

With `browserChromeRows = 4`, for a screen of `height` rows:

| screen row | content |
|---|---|
| `0` | path header |
| `1` | every box's top border |
| `2 … ph+1` | box content rows (`ph = height-4`) |
| `ph+2` | every box's bottom border |
| `ph+3` | status bar |

Within a box, after this change:

| pane | first content row | row → index |
|---|---|---|
| parent | `2` | n/a (not clickable) |
| current | `2` | `y-2` |
| changed | `3` (row `2` is its `changed - <scope>` header) | `y-3` |

### Divider geometry

In the wide tier (`width >= wideTierWidth`), with
`cells = distributeWidths(width-6, widths)`:

- box 0 spans `[0, cells[0]+2)`, box 1 starts at `cells[0]+2`, box 2 starts at
  `cells[0]+cells[1]+4`.
- Divider `0` (parent|current) occupies the two screen columns
  `{cells[0]+1, cells[0]+2}` — box 0's right border and box 1's left border.
- Divider `1` (current|changed) occupies `{cells[0]+cells[1]+3,
  cells[0]+cells[1]+4}`.

In the medium tier (`mediumTierWidth <= width < wideTierWidth`), with
`cells = distributeWidths(width-4, []int{widths[1], widths[2]})`, only divider
`1` exists, at `{cells[0]+1, cells[0]+2}`.

Below `mediumTierWidth` there is a single pane and no divider.

A divider is grabbable on any row the box borders span: `1 <= y <= ph+2`.

### Drag arithmetic

Let `k` be the index, *within the visible boxes*, of the box on the left of the
dragged divider; `leftEdge` the x of that box's left edge; `available` the tier's
content budget (`width-6` or `width-4`); `cells` the current cell widths.

```
newLeft   = x - leftEdge - 1                      // x is the box's right border
pairTotal = cells[k] + cells[k+1]
if pairTotal < 2*minColumnWidth: no-op            // no room to give
newLeft   = clamp(newLeft, minColumnWidth, pairTotal-minColumnWidth)
cells[k], cells[k+1] = newLeft, pairTotal-newLeft
```

`minColumnWidth = 8` — enough for a truncated name plus its `/` marker.

Write-back:

- **wide tier**: `widths = [cells[0], cells[1], cells[2]]`, exact.
- **medium tier**: `widths[1], widths[2] = cells[0], cells[1]`, and the hidden
  parent proportion is rescaled to keep its old share:
  `widths[0] = max(1, oldWidths[0] * (cells[0]+cells[1]) / (oldWidths[1]+oldWidths[2]))`.
  Guard the denominator against zero.

### Persistence contract

```go
// app/ui
type BrowserWidthsPersister interface {
    PersistBrowserWidths(widths [3]int) error
}
```

Injected through `RootModel.WithBrowserWidthsPersister(p) RootModel` — a
post-construction setter rather than another parameter on `NewRootBrowser`,
whose signature already ends in a variadic `widths ...[3]int` and has call sites
and tests that should not churn. A nil persister makes the release a no-op.

The `main`-side implementation patches `browser-widths = a,b,c` into the INI
config file resolved by `resolveFlagPath(os.Args[1:], "config", "YDIFF_CONFIG",
defaultConfigPath)`. An explicit `--browser-widths` on the command line still
wins on the next launch, as with every other config-file key.

## What Goes Where

- **Implementation Steps**: everything below.
- **Post-Completion**: manual check of the drag in a real terminal; the user
  decides whether to push.

## Implementation Steps

### Task 1: Drop the per-column directory headers

**Files:**
- Modify: `app/ui/browserview.go`
- Modify: `app/ui/browserview_test.go`
- Modify: `app/ui/browserpaging_test.go` (only if a test there asserts a header)

- [x] delete the `header string` parameter from `renderColumnBox` and stop
      rendering a header line in it; the box's first content row is now the
      first entry (or the `loading…` / `error: …` / `(empty)` placeholder)
- [x] in `renderParentColumn` and `renderCurrentColumn`, pass the full `height`
      to `visibleWindow` instead of `max(height-1, 0)`, and drop the
      `columnHeader(...)` calls
- [x] delete `columnHeader` (it has no remaining callers) and its test
- [x] leave `renderChangedColumn` exactly as it is — its `changed - <scope>`
      header is a control, not a directory name
- [x] update every existing test in `app/ui/browserview_test.go` that asserts a
      directory-name header row or depends on the old one-row header offset
- [x] add a test asserting that rendering a directory whose name would have been
      the header (e.g. a dir named `fixtures` holding a file named `a.txt`)
      produces no row containing the directory's own name
- [x] add a test asserting the current column now shows `ph` entry rows where it
      previously showed `ph-1`
- [x] run `go test ./app/ui/...` — must pass before task 2

### Task 2: Re-align the mouse row math and fix the pane-height mismatch

**Files:**
- Modify: `app/ui/mouse.go`
- Modify: `app/ui/root.go`
- Modify: `app/ui/browsermouse_test.go`

- [x] in `browserScreen.View` (`app/ui/root.go`) replace the hardcoded
      `max(b.height-3, 1)` with `b.paneContentHeight()` so the changed pane's
      body height is derived from the same `browserChromeRows` constant the
      view and the hit test use; keep `bodyHeight := max(ph-1, 0)` for the
      changed pane's own header row
- [x] in `browserScreen.hitTest`, map the current column's content rows from
      `y-2` (they no longer sit below a header) and keep the changed pane at
      `y == 2` → `browserHitChangedHeader`, `y > 2` → `browserHitChanged` with
      index `y-3`
- [x] in `clickCurrentEntry`, pass the full `b.paneContentHeight()` to
      `paneScrollWindow` instead of `max(b.paneContentHeight()-1, 0)`; leave
      `clickChangedEntry`'s `-1` alone (its header is still there)
- [x] write a test that a click on the current column's first content row
      (`y == 2`) selects the first visible entry
- [x] write a test that a click on the changed pane at `y == 2` still toggles
      the scope and at `y == 3` selects its first file
- [x] write a test that a scrolled current column maps a click to the right
      absolute index (cursor far enough down that `paneScrollWindow` offsets)
- [x] update the existing coordinate expectations in
      `app/ui/browsermouse_test.go` that the row shift invalidates
- [x] run `go test ./app/ui/...` — must pass before task 3

### Task 3: Divider hit-testing and the width-recompute arithmetic

**Files:**
- Modify: `app/ui/mouse.go`
- Create: `app/ui/browserresize_test.go`

- [ ] add `const minColumnWidth = 8` with a comment explaining the choice
- [ ] add `func (b browserScreen) dividerAt(x, y int) int` returning the global
      divider index (`0` = parent|current, `1` = current|changed) or `-1`,
      implementing the geometry in this plan's Technical Details; it must
      mirror `columnXRanges`'s tier logic rather than re-deriving it loosely,
      and must return `-1` below `mediumTierWidth`
- [ ] add `func (b *browserScreen) resizeDividerTo(divider, x int) bool`
      implementing the drag arithmetic and write-back from this plan; returns
      true when `b.widths` actually changed
- [ ] write table tests for `dividerAt`: both dividers in the wide tier, only
      divider 1 in the medium tier, `-1` in the narrow tier, `-1` outside the
      box rows, `-1` on ordinary content columns
- [ ] write table tests for `resizeDividerTo`: a plain drag left and right, the
      `minColumnWidth` clamp at both ends, a no-op when the pair has no room, a
      drag that leaves widths unchanged returning false, and a medium-tier drag
      preserving the hidden parent column's share
- [ ] write a test that the widths produced by a drag, fed back through
      `distributeWidths`, reproduce the dragged-to cell widths exactly (the
      property that makes cell counts usable as proportions)
- [ ] run `go test ./app/ui/...` — must pass before task 4

### Task 4: Wire the drag through the browser's mouse handling

**Files:**
- Modify: `app/ui/root.go`
- Modify: `app/ui/mouse.go`
- Modify: `app/ui/browserresize_test.go`

- [ ] add a `drag browserDrag` field to `browserScreen` (`app/ui/root.go`) where
      `browserDrag` is `struct{ active bool; divider int }`, documenting that
      `RootModel.updateBrowser` assigns the updated screen back so the state
      survives between events
- [ ] in `handleBrowserMouse`, on `tea.MouseButtonLeft` + `tea.MouseActionPress`,
      start a drag when `dividerAt` returns `>= 0` and only otherwise fall
      through to `clickBrowser`
- [ ] handle `tea.MouseActionMotion` with `tea.MouseButtonLeft` while
      `drag.active` by calling `resizeDividerTo`
- [ ] handle `tea.MouseActionRelease` by clearing `drag.active` and returning
      the persist command (task 5 supplies it; until then return `nil` and wire
      it in task 5)
- [ ] clear `drag.active` on `tea.WindowSizeMsg` — the geometry the drag started
      from no longer exists
- [ ] write a test driving press → motion → release through
      `handleBrowserMouse` and asserting the widths change
- [ ] write a test that a press on an ordinary entry row still selects, i.e. the
      drag path did not swallow normal clicks
- [ ] write a test that motion without a preceding press on a divider is a no-op
- [ ] run `go test ./app/ui/...` — must pass before task 5

### Task 5: The persistence seam in app/ui

**Files:**
- Modify: `app/ui/root.go`
- Modify: `app/ui/mouse.go`
- Modify: `app/ui/browserresize_test.go`

- [ ] declare `BrowserWidthsPersister` in `app/ui/root.go` with a doc comment
      explaining the consumer-side-interface rule it follows
- [ ] add the `persist BrowserWidthsPersister` field to `browserScreen` and the
      `RootModel.WithBrowserWidthsPersister(p) RootModel` setter
- [ ] add `browserScreen.persistWidthsCmd() tea.Cmd` returning nil when the
      persister is nil, otherwise a command that calls `PersistBrowserWidths`
      and logs a `[WARN]` line on error (a failed write must never take the
      session down or block the UI)
- [ ] return that command from the drag-release branch added in task 4
- [ ] write a test with a fake persister asserting it receives the dragged
      widths after release
- [ ] write a test asserting that a release with no drag in progress does not
      call the persister
- [ ] write a test asserting a nil persister is safe (no panic, nil command)
- [ ] run `go test ./app/ui/...` — must pass before task 6

### Task 6: Generalize the INI config patcher and implement the persister

**Files:**
- Modify: `app/themes.go`
- Modify: `app/themes_test.go`
- Create: `app/configstore.go`
- Create: `app/configstore_test.go`

- [ ] extract the key-agnostic core of `patchConfigTheme` into a reusable
      `patchConfigKey(path, key, value string) error`, with `scanThemeLines`
      becoming `scanConfigKeyLines(lines []string, key string)`; the
      stray-healing behaviour and the default-scope insertion rule carry over
      unchanged
- [ ] keep `themeCatalog.patchConfigTheme(name string)` as a thin wrapper over
      `patchConfigKey` so the existing tests in `app/themes_test.go` keep
      working untouched
- [ ] add `configStore` in `app/configstore.go` holding the config path, with
      `PersistBrowserWidths(widths [3]int) error` formatting `"a,b,c"` and
      calling `patchConfigKey(..., "browser-widths", ...)`
- [ ] add a compile-time assertion that `*configStore` satisfies
      `ui.BrowserWidthsPersister`
- [ ] write a test that persisting into a config file that has no
      `browser-widths` line inserts one in the default scope
- [ ] write a test that persisting over an existing `browser-widths` line
      replaces it in place
- [ ] write a test that a value written by `PersistBrowserWidths` round-trips
      through `parseBrowserWidths`
- [ ] write a test that an empty config path is a silent no-op
- [ ] run `go test ./app/...` — must pass before task 7

### Task 7: Wire the persister at the composition root

**Files:**
- Modify: `app/main.go`
- Modify: `app/main_test.go` (or the nearest existing test file for `run()` wiring)

- [ ] give `buildRootBrowser` a `configPath string` parameter (the caller at
      `app/main.go:280` already has `configPath` in scope from line ~203) and
      have it attach `&configStore{path: configPath}` via
      `WithBrowserWidthsPersister`
- [ ] leave the persister off when `configPath` is empty
- [ ] write a test asserting `buildRootBrowser` returns a RootModel whose
      browser screen carries a non-nil persister when a config path is given
      (add an exported-for-test accessor in `app/ui` only if one is genuinely
      needed; prefer testing the observable behaviour instead)
- [ ] run `go test ./app/...` — must pass before task 8

### Task 8: Verify acceptance criteria

- [ ] build: `go build ./...`
- [ ] no browser column shows a directory name as a header row any more
- [ ] the changed-files pane still shows `changed - <scope>` and clicking it
      still toggles the scope
- [ ] clicking an entry in either pane selects the right entry, including when
      the pane is scrolled
- [ ] dragging either divider resizes the panes and respects `minColumnWidth`
- [ ] the dragged widths land in the config file and are picked up on the next
      launch
- [ ] run the full suite: `go test ./...` — the only failure permitted is the
      pre-existing `TestGit_FileBlame_UsesIndexForStagedDiffs`
- [ ] `go vet ./...` clean; report `golangci-lint` as skipped (not installed)

### Task 9: [Final] Update documentation

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md`

- [ ] README: document mouse-dragging the pane borders and that the result is
      saved to `~/.config/ydiff/config` as `browser-widths`
- [ ] README: drop any mention of the per-column directory header from the
      browser section
- [ ] CLAUDE.md: record the per-pane content-row offsets (current `y-2`, changed
      `y-3`) in the browser-screen gotcha, since that is exactly the kind of
      coupling that silently breaks
- [ ] CLAUDE.md: record that `browserChromeRows` is the single source of the
      pane-height math and that `browserScreen.View` must not re-derive it
- [ ] move this plan to `docs/plans/completed/`

## Post-Completion

**Manual verification:**
- drag both dividers in a real terminal at wide and medium widths
- confirm the widths survive a restart
- confirm the browser still looks right in a terminal narrower than 60 columns

**External:**
- nothing has been pushed; pushing remains the user's call
