# ydiff — design

**Date:** 2026-09-14
**Status:** approved, ready for implementation planning

## Summary

ydiff is a terminal application that combines a yazi-style filesystem browser with
a revdiff-style diff reviewer. You walk directories in the left columns; the right
column lists what changed in the repository you are standing in. From there you drop
into a full diff review screen, leave inline comments, and those comments come back
out as markdown for a coding agent to act on.

It exists because revdiff cannot be installed in this environment — third-party
binaries are not permitted — while the workflow it enables is needed. ydiff is an
internal, self-built tool derived from revdiff's MIT-licensed source.

## Decisions

| Decision | Choice |
|---|---|
| Language | Go, toolchain 1.27.1; `go.mod` keeps upstream's `go 1.26` directive until a newer language feature is needed |
| Base | Fork of [revdiff](https://github.com/umputun/revdiff) v1.11.1 (MIT, © Umputun) |
| Browser | Written from scratch on Bubble Tea; yazi's interaction model, none of yazi's code (yazi is Rust) |
| TUI stack | bubbletea + bubbles + lipgloss, chroma for highlighting — all inherited |
| Git access | Shelling out to the `git` binary, as upstream does |
| VCS support | git only; Mercurial and Jujutsu implementations removed, the VCS interface kept |
| Module path | `github.com/molenin-moodys/ydiff` |
| Claude Code | Flag-compatible with revdiff; a `/ydiff` command and overlay launcher come later |

### Why fork rather than build fresh

revdiff already solves the hard half: diff parsing, syntax highlighting, the annotation
model, overlays, themes, mouse handling, and the agent output contract. The genuinely new
requirement is the filesystem browser, which is the smaller half. Forking gets a polished
reviewer on day one; the browser is roughly 1000–1500 lines on the same stack.

yazi could not be forked for the browser half: it is written in Rust and cannot be linked
into a Go binary. Its layout and navigation model are reproduced; its source is not used.

## Architecture

### Package layout

```
app/              entry point, config, CLI parsing (package main, left in place)
  browser/      NEW: file panel, filter, navigation state
  gitstate/     NEW: git status and branch change lists
  diff/         diff parsing, blame, VCS interface      <- hg/jj implementations removed
  annotation/   annotation parsing and storage          <- unchanged
  highlight/    chroma syntax highlighting              <- unchanged
  theme/        themes                                  <- unchanged
  keymap/       key bindings                            <- extended with browser actions
  history/      annotation history autosave             <- unchanged
  ui/           diff viewport, overlays, styles         <- screen routing added
```

Responsibilities are deliberately narrow. `browser` knows about the filesystem and a
cursor; it knows nothing about git or diffs. `gitstate` knows about git and returns a flat
list of changed files; it knows nothing about rendering. The root Bubble Tea model in `ui`
owns both screens and routes between them. Both new packages are testable without a
terminal.

### Screens and state machine

```
                    ydiff <refs|--only>          ydiff (no diff args)
                            |                            |
                            v                            v
                    +---------------+            +---------------+
         Enter ---->| ScreenReview  |            | ScreenBrowser |
                    | (revdiff view)|<---Enter---|  (file panel) |
                    +-------+-------+   on file  +-------+-------+
                            | q                          | q
                            v                            v
              back to browser, or exit            flush + exit
              if launched straight into review
```

Two entry points, one binary. With diff arguments — the path Claude Code uses — ydiff
starts directly in `ScreenReview` and `q` exits the process, so the existing integration
contract is unchanged. Invoked bare, it starts in `ScreenBrowser` and review becomes a
screen that is pushed and popped.

**The annotation store lives at the root model, not inside the review screen.** Reviewing
file A, commenting, returning to the browser, then reviewing file B all belong to one
session and flush together on exit. A store owned by the review screen would die on every
pop and silently lose comments. Exit from either screen flushes annotations to stdout or
`--output`, and writes the history copy under `~/.config/ydiff/history/`.

## The browser

### Layout

Three Miller columns. Default widths 15 / 35 / 50 percent, overridable with
`--browser-widths` and in config. The right column is widest because a changed-files list
is what you actually read.

```
+- soft --------+- ydiff ---------------+- changed - uncommitted ----+
|   proj-a      |   app/                |  M  app/ui/model.go        |
| > ydiff       |   internal/           |  M  app/ydiff/main.go      |
|   proj-b      | > README.md           |  A  app/browser/pane.go    |
|               |   go.mod              |  ?? notes.txt              |
+---------------+-----------------------+----------------------------+
| ~/s/soft/ydiff         filter: read          4 changed - main      |
+--------------------------------------------------------------------+
```

The left column shows the parent directory with the current one highlighted — orientation
only, no cursor of its own. The middle column is the live one. The right column comes from
`gitstate`.

### Listing rules

- Directories first, then files; both case-insensitively alphabetical.
- Hidden entries excluded by default, toggled with `.`.
- Symlinks to directories are enterable and marked; broken symlinks render dimmed and are
  not enterable.
- A directory that cannot be read renders its error inline in the column. A filesystem
  error never terminates ydiff.
- Cursor position is remembered per absolute path for the session, so going up returns the
  cursor to the directory you came out of. Nothing is persisted to disk.
- Directory reads run as Bubble Tea commands so a slow network mount stalls one column
  rather than freezing the UI. A read outstanding for more than ~150 ms shows a placeholder.

### Filtering

`/` starts a filter; typing narrows the middle column live. Matching is case-insensitive
substring, with matched runs highlighted in the entry name — chosen over fuzzy matching
because it is obvious why an entry matched. `Enter` applies the filter and returns to
navigation with it still active; `Esc` cancels and clears it. Any directory change, in
either direction, drops the filter.

The `/` prefix is what keeps the whole letter range available for commands.

### Key bindings

| Key | Action |
|---|---|
| Up / Down | move the cursor |
| Right / Enter | enter directory |
| Left | go up one level |
| Enter *(on a changed file)* | open that file's diff; does nothing if the file is unchanged |
| `/` | start filter |
| Enter *(in filter)* | apply, keep filter active, return to navigation |
| Esc | cancel filter, close overlay; never exits |
| `d` | open the review screen on the whole changeset |
| `t` | toggle `uncommitted` / `branch` scope |
| `r` | refresh the changed-files list |
| `.` | toggle hidden files |
| Tab | move focus between the middle column and the changed-files pane |
| `q` | quit |
| `?` | help |
| mouse | click to select, click to enter, wheel to scroll; the scope label in the changed pane header is clickable |

No vim movement aliases: arrows move, letters are commands. Both panes behave the same
way under `Enter`, so `Tab` makes them symmetric.

## The git layer

`gitstate` is a pure data package. It shells out to `git`, parses the output, and returns
changed files. It is tested against throwaway repositories built with `git init` in
`t.TempDir()`.

**Repository resolution follows the cursor.** The repository is resolved from the middle
column's current directory by walking up for `.git`. Navigating into a different
repository switches the right pane to it; navigating outside any repository says so
plainly rather than showing stale data. The list is always repository-wide — it does not
narrow to the directory you are standing in.

**Two scopes, toggled with `t`:**

| Scope | Command | Shows |
|---|---|---|
| `uncommitted` | `git status --porcelain=v2 --untracked-files=normal` | working tree, staged, untracked |
| `branch` | `git diff --name-status <base>...HEAD` | everything since the branch diverged |

**Base resolution**, in order: `--base-branch` or repo config, then
`git rev-parse --abbrev-ref origin/HEAD`, then `origin/main`, then `origin/master`. If
none resolve, the pane renders `base branch not found - pass --base-branch` and the branch
scope stays empty. It never falls back to a guess: a diff against the wrong base is worse
than no diff.

**Statuses** render as single-letter badges — `M`, `A`, `D`, `R`, `??` — colored by the
inherited theme so the application looks like one thing rather than two glued together.

**Refresh is explicit.** Results are cached per repository root and scope, recomputed on
`r`, on scope toggle, and on return from the review screen, where a file may have just
been edited. No filesystem watching in v1: `fsnotify` over a large monorepo costs
descriptors and surprises, and an explicit `r` is honest about what is on screen.

**Submodules and worktrees** are treated as ordinary repositories — whatever `git` reports
from that directory is what is shown. Aggregating nested submodules is out of scope.

## The review screen

Inherited from revdiff whole: the diff viewport, chroma highlighting, collapsed / compact /
wrap / word-diff modes, line numbers, blame gutter, `/` search with `n` and `N`, hunk jumps
on `[` and `]`, the file-tree sidepane, mouse support, and every overlay — help, info,
annotation list, theme selector, file picker. Its letter bindings are unchanged; the
browser's filter lives on a different screen, so nothing collides.

The fork changes only entry and exit. The screen takes a scope on entry — the whole
changeset via `d`, or a single path via `Enter` on a changed file. `q` pops back to the
browser when that is where it was entered from, and exits the process when ydiff was
launched straight into review.

**The annotation contract is preserved exactly**, because the integration depends on it:

```
## app/handler.go:43 (+)
use errors.Is() instead of direct comparison
```

Same markdown records, same `--output` file, same stdout fallback, same exit code 10 under
`--exit-code-on-annotations`, same `--annotations` reload.

**Renames that follow from the fork:** the config directory becomes `~/.config/ydiff/`
(config, keybindings, themes, history) and environment variables become `YDIFF_*`. There is
deliberately no fallback to `~/.config/revdiff/` — silently reading another tool's
configuration is the kind of quiet coupling that makes a fork confusing a year later.
Existing custom themes copy across with one `cp`.

The bundled default theme is renamed from `revdiff` to `ydiff` (`defaultThemeName` in
`app/theme/theme.go`, `defaultAutoThemeDark` in `app/themes.go`, and the asset directory
`themes/gallery/revdiff`), for the same reason the binary is not named after another tool.

Upstream's non-Go assets do not belong to this fork and are removed: `plugins/` and
`.claude-plugin/` (a Claude Code marketplace advertising revdiff), `site/`, `flake.nix`,
`package.json`, upstream's `CHANGELOG.md` and `docs/plans/`. Upstream's `CLAUDE.md` and
`Makefile` are adapted rather than deleted, and the release workflow is disabled so
upstream's `v*` tags, which arrive with the fetch, cannot trigger a release.

## CLI surface

Inherited and unchanged: `--only`, `--output`, `--wrap`, `--staged`, `--untracked`,
`-A/--all-files`, `-I/--include`, `-X/--exclude`, the display and theming flags,
`--annotations`, `--exit-code-on-annotations`, `--config`, `--keys`, and the positional
`[base] [against]` forms.

**Removed:** `--vim-motion`, and the Mercurial/Jujutsu revset translation behind the
positional refs.

**Added:**

| Flag | Purpose |
|---|---|
| `--base-branch` | override for branch-scope base resolution; also settable per repository in config |
| `--base-branch-repo` | per-repository override for `--base-branch`, `repo-root-path:branch` (repeatable); implemented as `options.BaseBranchRepos`, a config-file/CLI-map field with no env var, consulted only when `--base-branch` is empty — see `options.resolveBaseBranch` in `app/config.go` |
| `--browser` | force the browser screen even when diff arguments are present |
| `--browser-widths` | the three column percentages, default `15,35,50` |

`--base-branch` is deliberately not `--base`: upstream's positional arguments are already
called `base` and `against`, and the flag names the default branch used to find a fork
point, not the left side of a comparison.

**Implementation note — config precedence:** the plain intent above ("also settable per
repository in config") landed as `--base-branch-repo`, added during implementation as the
actual per-repository mechanism (the paragraph above only sketched "also settable... in
config" without naming a flag). Separately, this go-flags setup parses the config file into
`opts` *before* `flags.ParseArgs` runs, and go-flags only applies an `env`-tagged value
when the field is still at its zero value — so a config-file value wins over the same-named
environment variable, while an explicit CLI flag still wins over both. This is pre-existing
upstream parsing behavior, not something this fork changed, but it is easy to assume the
opposite (flags > env > config, the usual convention) so it's recorded here.

**Startup routing:**

| Invocation | Screen |
|---|---|
| `ydiff` | browser, at the current directory |
| `ydiff --only=plan.md` | review, single file — the planning plugin's path |
| `ydiff main` / `ydiff main..feature` | review, that comparison |
| `ydiff --browser main` | browser, with `branch` scope preselected |

**Exit codes:** 0 normally, 10 for annotations under `--exit-code-on-annotations`, 1 for
errors — matching upstream.

## Claude Code integration

Today the `planning` plugin invokes the reviewer as:

```
revdiff --only=<abs plan path> --output=<tmpfile> --wrap
```

resolving the binary with `command -v revdiff` and opening it in a terminal overlay
(agterm, tmux, zellij, kitty, wezterm, ghostty, iTerm2, and others). Because ydiff keeps
those flag names and that output format, a symlink named `revdiff` pointing at `ydiff`
makes every existing plugin work unchanged.

**The symlink is created by the user, never by the build or an installer.** The tool does
not name itself after another tool.

Phase two replaces that with a `/ydiff` command and its own overlay launcher, reusing the
terminal-detection logic from revdiff's MIT-licensed plugin.

## Testing

**The inherited suite is the safety net for the fork surgery.** Upstream ships tests and
mocks across `app/diff`, `app/ui`, `app/annotation`, `app/fsutil` and elsewhere. The module
rename, VCS stripping and vim-motion removal must land with that suite green — that is what
proves the surgery removed only what was intended.

**New tests follow the package boundaries:**

- `browser`, against real trees in `t.TempDir()`: ordering, hidden toggle, substring filter
  and its reset on navigation, cursor memory, symlink handling, unreadable directories.
- `gitstate`, against repositories created with `git init` in `t.TempDir()`: porcelain v2
  parsing, every status code, base resolution through the full fallback chain, missing
  `origin/HEAD`, and the not-a-repository case.
- Screen routing at the root model, driving `Update` with messages directly: push into
  review, pop back, and above all annotations from two separate review visits arriving in
  one output.

**One end-to-end test guards the agent contract:** build the binary, run it against a
fixture with `--only` and `--output`, assert the exact markdown and the exit code. If that
test breaks, the planning plugin breaks.

## Attribution

Shipped in the first commit, not added later.

- `LICENSE` — MIT © Misha Olenin, for new code.
- `LICENSE-revdiff` — Umputun's MIT notice verbatim. Legally required for a derivative work.
- `README.md` — states that ydiff is a fork of revdiff, and that the browser's interaction
  model is inspired by yazi (MIT) with no yazi code used.
- The upstream commit SHA the fork was taken from is recorded, so pulling a future revdiff
  fix stays possible rather than archaeological.

## Delivery phases

| # | Phase | Done when |
|---|---|---|
| 1 | Fork surgery: module rename, strip hg/jj and vim-motion, config dir to `~/.config/ydiff` | suite green, `ydiff` builds and behaves as before |
| 2 | `gitstate`: status and branch scopes, base resolution | package tested, no UI yet |
| 3 | `browser`: three columns, navigation, filter, cursor memory | package tested, no UI yet |
| 4 | Screen routing, shared annotation store, `d` and `Enter` entries | browser and review work as one application |
| 5 | CLI additions and startup routing | `--base-branch`, `--browser`, `--browser-widths` |
| 6 | *(later)* `/ydiff` command and overlay launcher | Claude Code calls ydiff natively |

Phase 1 is a real milestone: a working, fully self-owned tool with no external
dependencies, before the browser exists at all.

## Out of scope for v1

- File operations: create, delete, rename, copy, move, multi-select. yazi already does
  these and is installed.
- Filesystem watching for automatic refresh.
- A visible annotation counter in the browser status bar.
- Mercurial and Jujutsu support — the VCS interface is kept so adding one later is a single
  new file.
- Vim motion presets — removable now because upstream's remappable keybindings file makes a
  future preset configuration rather than code.
- Preview of unchanged files in the right pane. The pane shows the repository's changed
  files only; contextual preview was explicitly deferred.
