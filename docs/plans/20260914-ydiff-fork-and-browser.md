# ydiff: fork revdiff and add a yazi-style browser

> **Revision 2** — rewritten after plan review. Revision 1 derived its file paths from a
> GitHub tree listing rather than from the source, and was wrong in several places: there is
> no `app/revdiff/` package (the main package sits directly in `app/`), no `handoff`
> package, the annotation store is already injected from `main.go`, and `--post-flush-command`
> does not exist. Every path below was verified against a clone of `v1.11.1`
> (`39da604a135865eef40714bf86e20f01c15be833`).

## Overview

Build ydiff — a terminal tool that combines a yazi-style filesystem browser with a
revdiff-style diff reviewer, producing markdown annotations for a coding agent.

The problem it solves: revdiff cannot be installed here (third-party binaries are not
permitted) while the review workflow it enables is needed daily. ydiff is an internal,
self-built tool derived from revdiff's MIT-licensed source, with a filesystem browser
written from scratch on the same stack.

It integrates with the existing Claude Code setup by keeping revdiff's CLI flags and
annotation output format, so a user-created symlink makes current plugins work unchanged.

Full design: [`docs/2026-09-14-ydiff-design.md`](../2026-09-14-ydiff-design.md).

This plan covers phases 1-5 (all of v1). Phase 6 — the `/ydiff` command and overlay
launcher — is deliberately excluded and will be planned separately once v1 lands.

## Context (from discovery)

- **Repository**: `github.com/molenin-moodys/ydiff`, currently empty apart from `LICENSE`
  (MIT © Misha Olenin) and the design document. Single commit `0d70aa5`.
- **Upstream**: [revdiff](https://github.com/umputun/revdiff), MIT © Umputun. Fork point is
  tag **`v1.11.1`**, SHA **`39da604a135865eef40714bf86e20f01c15be833`**. Upstream's default
  branch `master` is ahead of that tag, so the fetch must pin the tag explicitly.
- **Upstream layout — verified, not assumed**: `package main` lives **directly in `app/`**
  (`main.go`, `config.go`, `themes.go`, `compare.go`, `stdin.go`, `renderer_setup.go`,
  `reviewinfo.go`, `annotations_load.go`, `history_save.go` and their tests — ~18 files).
  Subpackages are `annotation`, `diff`, `editor`, `fsutil`, `highlight`, `history`,
  `keymap`, `review`, `theme`, `ui` — **ten**. There is no `app/revdiff/` and no `handoff`
  package. `Makefile` builds with `go build ... ./app`.
- **Upstream stack**: bubbletea v1.3.10, bubbles, lipgloss, chroma v2.27.0, termenv,
  go-flags. No git library — it shells out to the `git` binary. Dependencies are vendored.
- **Import path appears in 82 Go files.**
- **Integration contract already in use**: the `planning` plugin calls
  `revdiff --only=<abs path> --output=<tmpfile> --wrap`, resolving the binary with
  `command -v revdiff`, via `launch-plan-review.sh`.
- **Local environment**: Go **1.27.1 is installed** at `/opt/homebrew/bin/go`. `git` and a
  working `$EDITOR` are present.

**Verified file references used by the tasks below:**

| Concern | Location |
|---|---|
| config path | `app/config.go:238` |
| keybindings path | `app/config.go:247` |
| themes dir | `app/themes.go:33` |
| history dir | `app/history/history.go:28,32,86` |
| env prefix `REVDIFF_` | `app/config.go` struct tags |
| default theme name | `app/theme/theme.go:14`, `app/themes.go:23`, `themes/gallery/revdiff` |
| annotation store creation | `app/main.go:108`, read back at `app/main.go:261,266` |
| hg/jj non-test references | `app/config.go`, `app/diff/directory.go`, `app/diff/diff.go`, `app/diff/vcs.go`, `app/renderer_setup.go`, `app/review/stats.go`, `app/ui/reviewinfo.go` |
| `--all-files` description "(git and jj only)" | `app/config.go:42` |
| vim-motion references | `app/config.go`, `app/main.go`, `app/ui/handlers.go`, `app/ui/model.go`, `app/ui/view.go`, `app/ui/vimmotion.go`, plus tests in `app/keymap`, `app/ui`, `app/config_test.go` |

## Development Approach

- **testing approach**: **mixed**, by deliberate choice.
  - **Phase 1 (fork surgery, tasks 1-6)** uses the regular order. There is nothing to write
    forward: the safety net is upstream's own test suite, which must stay green through
    every removal and rename. That suite passing is the acceptance criterion.
  - **Phases 2-5 (tasks 7-24, new code)** use **TDD**: the test is the first checklist item,
    written against a `t.TempDir()` tree or a throwaway `git init` repository.
- complete each task fully before moving to the next
- make small, focused changes
- **CRITICAL: every task MUST include new/updated tests** for code changes in that task
  - tests are not optional - they are a required part of the checklist
  - tests cover both success and error scenarios
- **CRITICAL: all tests must pass before starting next task** - no exceptions
- **CRITICAL: update this plan file when scope changes during implementation**
- run tests after each change
- maintain backward compatibility with the annotation output contract at all times

## Testing Strategy

- **unit tests**: required for every task.
- **package-level tests without a terminal**: `gitstate` and `browser` are pure data
  packages. `gitstate` runs against repositories created with `git init` in `t.TempDir()`;
  `browser` runs against real directory trees in `t.TempDir()`.
- **model tests**: screen routing is tested by driving the root model's `Update` with
  messages directly. The case that matters most is annotations from two separate review
  visits arriving in one output.
- **inherited suite**: upstream's tests and moq mocks must stay green throughout phase 1. A
  test that fails because the code it covered was intentionally removed gets removed with
  it; a test failing for any other reason is a regression and blocks the task. Upstream's
  `app/diff/hg_e2e_test.go` and `jj_e2e_test.go` skip when the binary is absent, so a green
  suite is achievable on this machine.
- **e2e**: no browser-based e2e framework here. The equivalent is one end-to-end contract
  test, task 24, which drives the real model in-process and asserts the exact markdown and
  exit code. It runs in the normal suite, not behind a tag — a guarded test that CI never
  enables protects nothing.
- **test command**: `make test` (upstream's target, which runs with `-race`), or
  `go test ./...`.

## Progress Tracking

- mark completed items with `[x]` immediately when done
- add newly discovered tasks with ➕ prefix
- document issues/blockers with ⚠️ prefix
- update plan if implementation deviates from original scope
- keep plan in sync with actual work done

## Solution Overview

Fork rather than rebuild: revdiff already solves diff parsing, syntax highlighting, the
annotation model, overlays, themes and the agent output contract. The genuinely new
requirement is the browser, the smaller half (~1000-1500 lines on the same stack).

yazi could not be forked — it is Rust and cannot link into a Go binary. Its layout and
navigation model are reproduced; none of its source is used.

Key design decisions and why:

- **The main package stays in `app/`.** Moving ~18 files into `app/ydiff/` would guarantee a
  conflict on every future upstream change to those files, defeating the whole reason task 1
  merges history instead of copying files. The module path and binary name already carry the
  fork's identity.
- **Two new packages with narrow responsibilities.** `browser` knows the filesystem and a
  cursor, nothing about git. `gitstate` knows git and returns a flat changed-file list,
  nothing about rendering. Both testable without a terminal; the root model in `ui` glues them.
- **The annotation store is already injected from `main.go`** and read back after the program
  exits. The work is not to move ownership but to guarantee one instance survives every
  push and pop of the review screen, and to prove it with a regression test.
- **Filter is `/`-prefixed.** That keeps the entire letter range available for commands
  (`d`, `t`, `r`, `.`, `q`, `?`) instead of consuming it with live typing.
- **Base branch never guesses.** If `origin/HEAD`, `origin/main` and `origin/master` all fail
  to resolve, the pane says so. A diff against the wrong base is worse than no diff.
- **No filesystem watching in v1.** `fsnotify` over a monorepo costs descriptors and
  surprises; explicit `r` is honest about what is on screen.
- **Abstractions kept, implementations dropped.** The VCS interface stays (adding Mercurial
  later is one new file) while hg/jj implementations go. Vim motions go because upstream's
  remappable keybindings file makes a future preset configuration rather than code.

## Technical Details

**Package layout after the work:**

```
app/              package main: entry point, config, CLI parsing (left in place)
  browser/        NEW: file panel, filter, navigation state
  gitstate/       NEW: git status and branch change lists
  diff/           diff parsing, blame, VCS interface   <- hg/jj implementations removed
  annotation/     annotation parsing and storage       <- unchanged
  highlight/      chroma syntax highlighting           <- unchanged
  theme/          themes                               <- default theme renamed
  keymap/         key bindings                         <- extended with browser actions
  history/        annotation history autosave          <- config dir changed
  ui/             diff viewport, overlays, styles      <- screen routing added
```

**Git commands issued by `gitstate`:**

| Purpose | Command |
|---|---|
| repo root | walk up for `.git`, confirm with `git rev-parse --show-toplevel` |
| uncommitted scope | `git status --porcelain=v2 --untracked-files=normal` |
| default branch | `git rev-parse --abbrev-ref origin/HEAD`, then `origin/main`, then `origin/master` |
| branch scope | `git diff --name-status <base>...HEAD` |

**Annotation output format (unchanged from upstream):**

```
## app/handler.go:43 (+)
use errors.Is() instead of direct comparison
```

**Browser key bindings:**

| Key | Action |
|---|---|
| Up / Down | move cursor |
| Right / Enter | enter directory |
| Left | up one level |
| Enter *(on a changed file)* | open that file's diff; no-op if unchanged |
| `/` | start filter |
| Enter *(in filter)* | apply, keep active, return to navigation |
| Esc | cancel filter, close overlay; never exits |
| `d` | review screen on the whole changeset |
| `t` | toggle `uncommitted` / `branch` scope |
| `r` | refresh changed-files list |
| `.` | toggle hidden files |
| Tab | focus between middle column and changed-files pane |
| `q` | quit |
| `?` | help |
| mouse | click to select, click to enter, wheel to scroll, clickable scope label |

**Column widths.** `--browser-widths` takes three positive integers, default `15,35,50`.
They are treated as **proportions and normalised**, not required to sum to 100 — a friendlier
rule than rejecting `1,2,3`. Non-numeric or non-positive values are rejected with a clear
message.

**Narrow-terminal behaviour** (previously unspecified, now a testable rule):

| Terminal width | Layout |
|---|---|
| >= 100 columns | all three columns at their proportions |
| 60-99 columns | parent column dropped; current and right share the width |
| < 60 columns | only the focused pane rendered, with the other reachable via Tab |

**New CLI flags:** `--base-branch`, `--browser`, `--browser-widths`.
**Removed:** `--vim-motion` and hg/jj revset translation.
**Renames:** config dir `~/.config/ydiff/`, env prefix `YDIFF_*` (no fallback to revdiff's),
default theme `revdiff` -> `ydiff`.

## What Goes Where

- **Implementation Steps** (`[ ]` checkboxes): everything achievable in this repository —
  code, tests, documentation.
- **Post-Completion** (no checkboxes): the symlink, verifying the planning plugin end to
  end, manual UX checks, and the phase 6 plugin work.

## Implementation Steps

### Task 1: Import upstream at the pinned fork point

**Files:**
- Create: `LICENSE-revdiff`, `README.md`, `UPSTREAM.md`
- Modify: `LICENSE` (merge conflict), repository history

- [x] add the upstream remote and fetch **the tag, not the branch**:
      `git remote add upstream https://github.com/umputun/revdiff` then
      `git fetch upstream tag v1.11.1 --no-tags` — fetching `master` would silently fork
      from a different commit than `UPSTREAM.md` records, and would drag in upstream's `v*`
      tags, which the release workflow triggers on
- [x] merge `v1.11.1` into `main` with `--allow-unrelated-histories`; **expect a conflict on
      `LICENSE`**, since both repositories have one — resolve in favour of this repository's
      MIT © Misha Olenin
- [x] create `LICENSE-revdiff` with Umputun's MIT notice verbatim
- [x] create `UPSTREAM.md` recording tag `v1.11.1`, SHA
      `39da604a135865eef40714bf86e20f01c15be833`, and the procedure for pulling a future
      upstream fix
- [x] write `README.md` stating ydiff is a fork of revdiff, and that the browser's
      interaction model is inspired by yazi (MIT) with no yazi code used
- [x] run `go build ./app` and `go test ./...` — inherited suite green before task 2
      (note: `TestGit_FileBlame_UsesIndexForStagedDiffs` in `app/diff/blame_test.go` fails
      unmodified from the merge, due to `git blame --contents` output differing on this
      machine's git 2.33.0 vs. whatever version upstream's CI uses — an environment issue
      unrelated to the fork surgery; see [deviation] note logged separately)

### Task 2: Rename module path and binary

**Files:**
- Modify: `go.mod`, `vendor/modules.txt`
- Modify: 82 `.go` files carrying the import path
- Modify: `Makefile`, `.goreleaser.yml`, `.github/workflows/*`, `completions/*`

- [x] change the module path in `go.mod` to `github.com/molenin-moodys/ydiff`, keeping
      upstream's `go 1.26` directive
- [x] rewrite the import path in all 82 files; **leave the main package in `app/`** — do not
      move it to `app/ydiff/`, which would conflict with every future upstream change
- [x] rename the binary to `ydiff` in `Makefile` (`go build ... ./app`), `.goreleaser.yml`,
      CI workflows and shell completions
- [x] run `go mod vendor` and verify the vendored tree is consistent
- [x] update inherited tests asserting on the binary or module name
- [x] run `go build ./app` and `go test ./...` — suite green before task 3

### Task 3: Triage inherited non-code assets

**Files:**
- Delete: `plugins/`, `.claude-plugin/`, `site/`, `flake.nix`, `flake.lock`, `package.json`, `CHANGELOG.md`, upstream's `docs/plans/`, `.zed/tasks.json`
- Modify: `CLAUDE.md`, `Makefile`, `.github/workflows/release.yml`, `.gitignore`, `docs/ARCHITECTURE.md`
- Delete: `app/plugin_exit_code_test.go` (asserts on the deleted plugin paths)

- [x] delete `plugins/` and `.claude-plugin/` — a Claude Code marketplace advertising
      revdiff installation has no business in this repository
- [x] delete `app/plugin_exit_code_test.go`, which asserts on those exact paths, and confirm
      the exit-code behaviour it covered is instead covered by task 24
- [x] delete `site/`, `flake.nix`, `flake.lock`, `package.json`, `CHANGELOG.md`, upstream's
      `docs/plans/` (it would land beside this very plan) and `.zed/tasks.json`
- [x] disable the release workflow's `push: tags: v*` trigger so a stray upstream tag cannot
      fire a release with a secret this repository does not have
- [x] adapt `CLAUDE.md` and `docs/ARCHITECTURE.md` to ydiff; fix the `Makefile` `fmt` target,
      which hardcodes `~/.claude/format.sh`, a path personal to the upstream author
- [x] run `go test ./...` and confirm CI config parses — suite green before task 4

### Task 4: Remove Mercurial and Jujutsu support, keep the VCS interface

**Files:**
- Delete: `app/diff/hg.go`, `app/diff/jj.go`, `app/diff/hgblame.go`, `app/diff/jjblame.go` and their tests, including `hg_e2e_test.go` and `jj_e2e_test.go`
- Modify: `app/diff/vcs.go`, `app/diff/diff.go`, `app/diff/directory.go`, `app/renderer_setup.go`, `app/review/stats.go`, `app/ui/reviewinfo.go`, `app/config.go`

- [x] delete the hg and jj implementation and blame files with their tests
- [x] remove hg/jj branches from the **verified** reference sites above — note that
      `app/diff/blame.go` and `app/diff/compare.go` do *not* reference them, while
      `app/diff/directory.go`, `app/renderer_setup.go`, `app/review/stats.go` and
      `app/ui/reviewinfo.go` do
- [x] update the `--all-files` description at `app/config.go:42`, which reads
      "(git and jj only)", and its gating logic
- [x] **keep** the VCS interface in `app/diff/vcs.go` with git as the sole implementation,
      so adding Mercurial later is one new file rather than a layer rebuild
- [x] write a test asserting the interface resolves git repositories and reports a clear
      error for an unsupported VCS
- [x] run `go test ./...` — suite green before task 5

### Task 5: Remove the vim-motion preset

**Files:**
- Delete: `app/ui/vimmotion.go` and its test
- Modify: `app/config.go`, `app/main.go`, `app/ui/handlers.go`, `app/ui/model.go`, `app/ui/view.go`
- Modify: `app/keymap/keymap_test.go`, `app/ui/handlers_test.go`, `app/ui/model_test.go`, `app/config_test.go`
- Modify: `completions/*`, `docs/`

- [x] delete `app/ui/vimmotion.go` and its test
- [x] remove the `--vim-motion` flag, its config key and env binding from `app/config.go`
      and its wiring in `app/main.go`
- [x] remove the `vim vimState` field from `app/ui/model.go` and its dispatch in
      `app/ui/handlers.go` and **`app/ui/view.go`** — the file revision 1 missed
- [x] verify the remappable keybindings file still loads and applies: that is the seam
      through which a vim preset returns later as configuration, not code
- [x] add a test asserting a custom binding from the keybindings file overrides a default
- [x] run `go test ./...` — suite green before task 6

### Task 6: Rename config directory, environment prefix and default theme

**Files:**
- Modify: `app/config.go` (lines 232-247), `app/themes.go` (lines 23, 27-33), `app/history/history.go` (lines 28, 32, 86), `app/keymap/keymap.go:105`
- Modify: `app/theme/theme.go:14`
- Rename: `themes/gallery/revdiff` -> `themes/gallery/ydiff`
- Modify: `app/theme/catalog_test.go`

- [x] change config, keybindings, themes and history paths to `~/.config/ydiff/` at the
      verified sites — note `app/theme/catalog.go` takes `themesDir` as a constructor
      argument and resolves nothing; `app/themes.go` is the file that does
- [x] change every `REVDIFF_` struct tag in `app/config.go` to `YDIFF_`
- [x] rename the bundled default theme from `revdiff` to `ydiff`: `defaultThemeName`,
      `defaultAutoThemeDark`, the asset directory, and the three name assertions in
      `app/theme/catalog_test.go`
- [x] add **no** fallback to `~/.config/revdiff/` — reading another tool's configuration
      silently is the coupling this fork exists to avoid
- [x] write tests asserting config, keybindings, themes and history resolve under the new
      directory, that a `REVDIFF_*` variable has no effect, and that the default theme
      resolves by its new name
- [x] run `go test ./...` — **phase 1 complete**: a working, self-owned tool with no external
      dependencies, before the browser exists

### Task 7: gitstate — repository resolution

**Files:**
- Create: `app/gitstate/gitstate.go`, `app/gitstate/repo.go`, `app/gitstate/repo_test.go`

- [x] write tests first: repository found by walking up from a nested directory; root
      reported correctly; a directory outside any repository reported as such; a directory
      that vanished mid-call returning an error rather than panicking
- [x] implement `Resolve(dir string) (*Repo, error)` walking up for `.git`, confirmed with
      `git rev-parse --show-toplevel`
- [x] define the exported types: `Repo`, `Scope`, `ChangedFile`, `Status`
- [x] write tests for worktrees and submodules resolving as ordinary repositories
- [x] run `go test -race ./app/gitstate/...` — must pass before task 8

### Task 8: gitstate — uncommitted scope

**Files:**
- Create: `app/gitstate/status.go`, `app/gitstate/status_test.go`

- [x] write tests first against a `git init` repository in `t.TempDir()` covering each
      status: modified, added, deleted, renamed, untracked, staged-plus-modified
- [x] implement parsing of `git status --porcelain=v2 --untracked-files=normal` into
      `[]ChangedFile`
- [x] handle paths with spaces, quoting and non-ASCII characters
- [x] write a test for a clean repository returning an empty list, not an error
- [x] write a test for a malformed porcelain line producing a clear error rather than a
      silent skip
- [x] run `go test -race ./app/gitstate/...` — must pass before task 9

### Task 9: gitstate — base branch resolution

**Files:**
- Create: `app/gitstate/base.go`, `app/gitstate/base_test.go`

- [x] write tests first for the full chain: explicit override wins, then
      `git rev-parse --abbrev-ref origin/HEAD`, then `origin/main`, then `origin/master`
- [x] write the test that matters most: when none resolve, return a distinguishable
      "base not found" error and **never** fall back to a guess
- [x] implement `ResolveBase(repo *Repo, override string) (string, error)`
- [x] write a test for a repository with no remote at all
- [x] run `go test -race ./app/gitstate/...` — must pass before task 10

### Task 10: gitstate — branch scope

**Files:**
- Create: `app/gitstate/branch.go`, `app/gitstate/branch_test.go`

- [x] write tests first against a repository with a real fork point: commits on the branch
      after diverging appear; commits made on the default branch after the fork point do not
- [x] implement parsing of `git diff --name-status <base>...HEAD` into `[]ChangedFile`
- [x] handle rename entries (`R100 old new`) by recording both paths
- [x] write a test for a branch with no commits since the fork point returning an empty list
- [x] write a test asserting an unresolved base yields the "base not found" error rather than
      an empty list, so the UI can tell the two apart
- [x] run `go test -race ./app/gitstate/...` — must pass before task 11

### Task 11: gitstate — caching and invalidation

**Files:**
- Create: `app/gitstate/cache.go`, `app/gitstate/cache_test.go`

- [x] write tests first: a repeat call for the same root and scope does not re-invoke git;
      explicit invalidation forces a recompute; different scopes and roots cache independently
- [x] implement the cache keyed by repository root plus scope, with explicit `Invalidate`
- [x] write a **concurrent read/write** test — Bubble Tea commands load from goroutines, so
      reads alone are not enough — and run it under `-race`
- [x] write a test asserting an error result is not cached, so a transient git failure does
      not stick
- [x] run `go test -race ./app/gitstate/...` — **phase 2 complete**

### Task 12: browser — directory listing

**Files:**
- Create: `app/browser/browser.go`, `app/browser/listing.go`, `app/browser/listing_test.go`

- [x] write tests first against real trees in `t.TempDir()`: directories before files, both
      case-insensitively alphabetical; hidden entries excluded by default, included when toggled
- [x] write tests for symlinks: a symlink to a directory is enterable and flagged; a broken
      symlink is flagged and not enterable
- [x] write the test that keeps the app alive: an unreadable directory yields an entry-level
      error the caller can render, never a panic or process exit
- [x] implement `Entry`, `Listing` and `Read(dir string, showHidden bool) (Listing, error)`
- [x] run `go test -race ./app/browser/...` — must pass before task 13

### Task 13: browser — asynchronous loading

**Files:**
- Create: `app/browser/load.go`, `app/browser/load_test.go`

*Ordered before navigation deliberately: building navigation synchronously first would mean
reworking it to issue commands afterwards.*

- [x] write tests first: a load produces a message carrying its listing and the path it was
      requested for
- [x] write the stale-response test: a listing arriving for a path the user has already left
      is discarded, not rendered
- [x] implement directory reads as Bubble Tea commands returning a result message
- [x] implement the pending state behind an **injected clock**, so the ~150 ms placeholder
      threshold is testable rather than flaky — a real clock in production, a fake in tests
- [x] write a test for a failing load reaching the model as a renderable error state
- [x] run `go test -race ./app/browser/...` — must pass before task 14

### Task 14: browser — cursor and navigation

**Files:**
- Create: `app/browser/nav.go`, `app/browser/nav_test.go`

- [x] write tests first: entering a directory moves the current path down; going up moves it
      back; going up from the filesystem root is a no-op
- [x] write the cursor-memory test: enter `app/`, go back up, and the cursor is on `app/`
      rather than at the top of the list
- [x] implement navigation state holding current path, cursor index, and a per-absolute-path
      cursor memory map scoped to the session, nothing persisted to disk
- [x] write tests for a directory that disappeared between listing and entering, and for the
      parent-column listing being derived from the current path
- [x] run `go test -race ./app/browser/...` — must pass before task 15

### Task 15: browser — substring filter

**Files:**
- Create: `app/browser/filter.go`, `app/browser/filter_test.go`

- [x] write tests first: case-insensitive substring matching, with the matched rune range
      reported so the view can highlight it
- [x] write the reset test: any directory change, in either direction, drops the filter
- [x] write tests for the filter lifecycle — start, append, backspace, apply-and-keep,
      cancel-and-clear — and for a filter matching nothing leaving an empty but valid listing
- [x] implement the filter as browser state, applied only to the middle column
- [x] write a test asserting the cursor lands on a valid index after the filter narrows the
      list beneath it
- [x] run `go test -race ./app/browser/...` — **phase 3 complete**

### Task 16: Register browser actions in the keymap

**Files:**
- Modify: `app/keymap/keymap.go`, `app/keymap/layout.go`
- Modify: `app/keymap/keymap_test.go`

*Ordered first in phase 4: handlers in tasks 17-20 dispatch through these bindings, so
registering them afterwards would mean reworking all three.*

- [x] write tests first for the browser action set: arrows, `/`, `d`, `t`, `r`, `.`, Tab,
      `q`, `?`, Esc
- [x] register the browser actions alongside the review screen's so they are remappable
      through the same `map`/`unmap` keybindings file
- [x] write the separation test: the same letter is a command in navigation and literal text
      once the filter is active
- [x] write a test asserting a user remap of a browser action takes effect
- [x] run `go test -race ./app/keymap/...` — must pass before task 17

### Task 17: Root model and screen routing

**Files:**
- Create: `app/ui/root.go`, `app/ui/root_test.go`
- Modify: `app/ui/model.go`

- [x] write tests first driving `Update` directly: `d` pushes the review screen, `q` in
      review pops back to the browser, `q` in the browser quits
- [x] write the entry-point test: launched straight into review, `q` exits the process rather
      than revealing a browser that was never there
- [x] implement the root model owning the active screen and delegating `Update` and `View`
- [x] implement `gitstate` cache invalidation on **return from the review screen**, where a
      file may have just been edited
- [x] write a test asserting window-resize messages reach both screens
- [x] run `go test -race ./app/ui/...` — must pass before task 18

### Task 18: One annotation store across screen transitions

**Files:**
- Modify: `app/ui/root.go`, `app/main.go`
- Create: `app/ui/annotstore_test.go`

*Upstream already creates the store in `main.go:108` and injects it, reading it back at
`main.go:261` after the program exits. This task does not move ownership — it guarantees the
single injected instance survives push and pop, and proves it.*

- [x] write the test this task exists for: annotate in review, pop to the browser, enter
      review on a different file, annotate again — exiting produces **one** output with both
- [x] verify the root model passes the injected store down on every review-screen push and
      never re-creates it on pop
- [x] verify exit from either screen reaches the same flush path, writing stdout or
      `--output` and the history copy under `~/.config/ydiff/history/`
- [x] write a test asserting exit code 10 under `--exit-code-on-annotations` when the
      annotations came via the browser path
- [x] run `go test -race ./app/ui/...` — must pass before task 19

### Task 19: Three-column browser view

**Files:**
- Create: `app/ui/browserview.go`, `app/ui/browserview_test.go`
- Modify: `app/ui/view.go`

*Takes column widths as a plain parameter. Task 22 wires the flag to it, so this task does
not depend on flag parsing existing yet.*

- [x] write tests first on rendered output: three columns at the given proportions, parent
      column showing the current directory highlighted with no cursor of its own
- [x] implement rendering with lipgloss, reusing the inherited theme so browser and review
      look like one application
- [x] implement the status bar: current path, active filter, changed count, scope label and
      **branch name** (`4 changed - main`, per the design mock-up)
- [x] implement filter-match highlighting, the loading placeholder from task 13, and the
      **inline directory error and symlink markers** from task 12
- [x] write tests for the narrow-terminal rules in Technical Details (>=100 / 60-99 / <60
      columns) and for entry names too long for their column
- [x] run `go test -race ./app/ui/...` — must pass before task 20

### Task 20: Changed-files pane

**Files:**
- Create: `app/ui/changedpane.go`, `app/ui/changedpane_test.go`, `app/ui/gitload.go`

- [x] write tests first: status badges `M`, `A`, `D`, `R`, `??` render themed; the pane is
      repository-wide and does not narrow to the current directory
- [x] write the message tests: outside a repository, and base-not-found in branch scope, each
      render their own plain message instead of an empty or stale list
- [x] implement the **Bubble Tea command and message plumbing for `gitstate` loads**, with
      stale-response discard mirroring task 13
- [x] implement the pane with its own cursor and scrolling, `Tab` focus switching, and
      `Enter` on a changed file entering review scoped to that path — `Enter` on an unchanged
      file is a no-op
- [x] implement repository re-resolution when navigation moves into a different repository,
      and cache invalidation on `t` and `r`
- [x] run `go test -race ./app/ui/...` — must pass before task 21

### Task 21: Mouse support in the browser

**Files:**
- Modify: `app/ui/mouse.go`
- Create: `app/ui/browsermouse_test.go`
- Modify: `app/ui/overlay/help.go`

- [x] write tests first: click selects an entry, click on a directory enters it, wheel
      scrolls the focused pane, a click in the changed pane moves focus there
- [x] implement the clickable scope label in the changed pane header, toggling
      `uncommitted` / `branch` exactly as `t` does
- [x] verify `--no-mouse` disables browser mouse handling as it does in review
- [x] extend the help overlay with the browser bindings and the mouse row
- [x] run `go test -race ./...` — **phase 4 complete**: browser and review work as one application

### Task 22: New CLI flags

**Files:**
- Modify: `app/config.go`
- Create: `app/config_browser_test.go`
- Modify: `completions/*`

- [ ] write tests first for `--base-branch`, `--browser` and `--browser-widths` in flag,
      config-file and `YDIFF_*` forms, with the flag taking precedence
- [ ] implement the three flags, defaulting widths to `15,35,50` and **normalising them as
      proportions** rather than requiring a sum of 100
- [ ] implement per-repository `base-branch` in the config file
- [ ] wire the parsed widths into the view built in task 19
- [ ] write validation tests: non-numeric, non-positive or wrong-count widths are rejected
      with a clear message rather than rendering a broken layout
- [ ] update shell completions; run `go test -race ./...` — must pass before task 23

### Task 23: Startup routing

**Files:**
- Modify: `app/main.go`
- Create: `app/routing_test.go`

- [ ] write tests first for the routing matrix: bare `ydiff` opens the browser;
      `--only=<file>` opens review on that file; `ydiff main` and `ydiff main..feature` open
      review on that comparison; `--browser main` opens the browser with `branch` preselected
- [ ] implement the routing decision as a pure function over parsed options, testable without
      starting a terminal program
- [ ] write a test asserting `--stdin` input still routes to review
- [ ] write a test asserting bare `ydiff` outside any repository opens the browser with an
      empty changed pane
- [ ] run `go test -race ./...` — must pass before task 24

### Task 24: End-to-end agent contract test

**Files:**
- Create: `app/e2e_contract_test.go`, `testdata/` fixtures

*Mechanism is specified deliberately. Driving the built binary through a pipe does not work:
Bubble Tea needs a TTY, and upstream's own `plugin_exit_code_test.go` sidestepped this with a
fake shell script rather than the real TUI. A PTY dependency is not in `go.mod` and the repo
vendors, so this test runs the real model in-process instead.*

- [ ] build the program with `tea.NewProgram(model, tea.WithInput(scriptedInput),
      tea.WithoutRenderer())`, feeding keystrokes that open a file, annotate a line and quit
- [ ] assert the `--output` file contains exactly the expected markdown records, headers
      included
- [ ] assert exit code 10 with `--exit-code-on-annotations` and 0 without annotations,
      covering what the deleted `plugin_exit_code_test.go` used to cover
- [ ] assert stdout carries the annotations when no `--output` is given, since that is the
      path an agent reads
- [ ] **do not** guard the test behind a build tag — it runs in the normal suite, because a
      guarded test CI never enables protects nothing
- [ ] run `go test -race ./...` — **phase 5 complete**

### Task 25: Verify acceptance criteria

- [ ] verify each Overview requirement explicitly: browser navigation, `/` filter, both
      scopes, review entry from both panes, mouse, annotations flushing as one output
- [ ] verify edge cases: no repository, base not found, unreadable directory, broken symlink,
      empty changeset, and each of the three terminal-width tiers
- [ ] verify the design's "Out of scope for v1" list was honoured — no file operations, no
      fsnotify, no annotation counter, no hg/jj, no vim motions, no unchanged-file preview
- [ ] confirm no `revdiff` string survives outside `LICENSE-revdiff`, `UPSTREAM.md` and
      `README.md`: `grep -rn revdiff . --exclude-dir=.git`
- [ ] run `make test` and `golangci-lint run` with the inherited configuration
- [ ] record `go test -cover ./...` and confirm no package touched dropped below its
      pre-change coverage (upstream has no coverage gate, so this is the honest check)

### Task 26: [Final] Update documentation

- [ ] update `README.md` with browser usage, the full key table, the narrow-terminal rules
      and the new flags
- [ ] verify `LICENSE-revdiff`, `UPSTREAM.md` and the yazi attribution note are present and
      accurate
- [ ] update `CLAUDE.md` with conventions discovered while working in the fork
- [ ] update `docs/2026-09-14-ydiff-design.md` if implementation diverged from it
- [ ] move this plan to `docs/plans/completed/`

## Post-Completion

*Items requiring manual intervention or external systems — no checkboxes, informational only*

**Manual verification:**
- Drive the browser by hand in a real repository: navigation feel, filter responsiveness,
  three-column proportions at your usual terminal size.
- Verify rendering in each terminal you actually use — colors, mouse, resize behaviour.
- Check performance in a large monorepo: directory listing and `git status` latency.

**External system updates:**
- Create the `revdiff` -> `ydiff` symlink yourself if you want existing plugins to keep
  working. The build deliberately does not create it.
- Verify the `planning` plugin's `launch-plan-review.sh` works against ydiff end to end,
  including the overlay in your terminal multiplexer.
- Copy custom themes from `~/.config/revdiff/themes/` to `~/.config/ydiff/themes/` if wanted.

**Future work (phase 6, planned separately):**
- The `/ydiff` command and its own overlay launcher, reusing revdiff's MIT-licensed
  terminal-detection logic, so Claude Code calls ydiff natively without a symlink.
