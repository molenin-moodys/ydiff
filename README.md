# ydiff

ydiff is a terminal tool that combines a yazi-style filesystem browser with a
revdiff-style diff reviewer, producing markdown annotations for a coding agent.

ydiff is a fork of [revdiff](https://github.com/umputun/revdiff) (MIT, © Umputun). The
diff review engine, annotation model, overlays and themes are derived from revdiff's
MIT-licensed source. See [`LICENSE-revdiff`](LICENSE-revdiff) for the original license
text and [`UPSTREAM.md`](UPSTREAM.md) for the fork point and how to pull future upstream
fixes.

The filesystem browser is new code, written from scratch on the same stack
(bubbletea/lipgloss). Its interaction model is inspired by [yazi](https://github.com/sxyazi/yazi)
(MIT) — layout and navigation ideas only. No yazi source is used or vendored: yazi is
written in Rust and cannot be linked into a Go binary.

Full design: [`docs/2026-09-14-ydiff-design.md`](docs/2026-09-14-ydiff-design.md).
Implementation plan: [`docs/plans/20260914-ydiff-fork-and-browser.md`](docs/plans/20260914-ydiff-fork-and-browser.md).

## Usage

```
ydiff [OPTIONS] [base] [against]
```

With no arguments and no diff-selecting flags, ydiff opens on the **browser** screen at
the current directory. Passing a ref, `--only`, `--stdin`, or similar routes straight to
the **review** screen instead:

| Invocation | Screen |
|---|---|
| `ydiff` | browser, at the current directory |
| `ydiff --only=plan.md` | review, single file |
| `ydiff main` / `ydiff main..feature` | review, that comparison |
| `ydiff --browser main` | browser, with `branch` scope preselected |

## The browser screen

Three columns, left to right: the parent directory, the current directory (where the
cursor lives), and a changed-files pane for whatever git repository the cursor is
currently standing in. Walking directories in the left two columns is independent from
the changed-files pane on the right — the pane always reflects the whole repository, not
just the directory you're standing in.

**Two scopes for the changed-files pane, toggled with `t`:**

| Scope | Shows |
|---|---|
| `uncommitted` | working tree, staged, and untracked changes (`git status`) |
| `branch` | everything since the branch diverged from its base (`git diff <base>...HEAD`) |

Base-branch resolution for `branch` scope, in order: `--base-branch` (or a per-repository
override, see [Flags](#flags) below), then `origin/HEAD`, then `origin/main`, then
`origin/master`. If none resolve, the pane says so plainly and `branch` scope stays
empty — it never falls back to guessing which branch you mean.

**Filtering:** press `/` to start typing a filter; it narrows the current-directory list
to entries whose name contains the substring, live as you type. `Enter` applies the
filter and returns to navigation with it still active. `Esc` cancels and clears it.
Changing directory, in either direction, drops the filter. While the filter is active,
every other letter is literal text, not a command — that's why `/` exists: it's what
keeps the rest of the letter range free for single-key commands the rest of the time.

**Refresh is explicit.** The changed-files pane is cached per repository root and scope,
recomputed on `r`, on scope toggle (`t`), and automatically when you return to the
browser from the review screen (where a file may have just been edited). There is no
filesystem watching.

### Key bindings — browser screen

No vim movement aliases here: arrows move the cursor, letters are commands.

| Key | Action |
|---|---|
| Up / Down (arrows) | move the cursor |
| Right / Enter | enter directory (current-directory column) |
| Enter *(on a changed file)* | open that file's diff in the review screen |
| Left | go up one level |
| `/` | start filter; typing narrows the list live; `Enter` applies (filter stays active); `Esc` cancels and clears it |
| `d` | open the review screen on the whole changeset |
| `t` | toggle `uncommitted` / `branch` scope |
| `r` | refresh the changed-files list |
| `.` | toggle hidden files |
| `Tab` | move focus between the current-directory column and the changed-files pane |
| `q` | quit |
| `?` | help |
| mouse | click to select or enter, wheel to scroll; the scope label in the changed-files header is clickable |

### Narrow-terminal rules

The browser adapts its column count to the terminal width. `--browser-widths` sets the
parent/current/changed proportions; at the medium tier (parent column dropped) the same
current/changed proportions are reused for the remaining two columns (see
[Flags](#flags)):

| Width | Columns shown |
|---|---|
| >= 100 columns | all three: parent, current, changed-files |
| 60–99 columns | current + changed-files only (parent column dropped) |
| < 60 columns | only the focused pane |

## The review screen

Inherited from revdiff: the diff viewport, chroma syntax highlighting, collapsed /
compact / wrap / word-diff modes, line numbers, blame gutter, search, hunk jumps, the
file-tree sidepane, mouse support, and every overlay (help, info, annotation list, theme
selector, file picker). You enter it from the browser via `d` (whole changeset) or
`Enter` on a changed file, or directly by passing diff arguments on the command line.
`q` pops back to the browser when that's where you came from, and exits the process
otherwise.

### Key bindings — review screen

| Key | Action |
|---|---|
| `j` / `k` / Down / Up | move cursor down / up |
| `pgdown` / `pgup` | page down / up |
| `ctrl+d` / `ctrl+u` | half page down / up |
| `home` / `end` | go to top / bottom |
| Left / Right | scroll left / scroll right (Right also focuses the diff pane) |
| `J` / `K` | scroll diff down / up |
| `n` / `N` / `p` | next / previous file or search match |
| `]` / `[` | next / previous hunk |
| `e` | open focused file in `$EDITOR` |
| `Tab` | toggle pane focus |
| `h` / `l` | focus tree pane / focus diff pane |
| `/` | search in diff |
| `a` / Enter | annotate line / select file |
| `A` | annotate file |
| `d` | delete annotation |
| `@` | annotation list |
| `ctrl+e` | open annotation in `$EDITOR` |
| `}` / `{` | next / previous annotation (across files) |
| `O` | flush annotations to output file |
| `v` | toggle collapsed view |
| `C` | toggle compact diff view |
| `w` | toggle word wrap |
| `t` | toggle tree pane |
| `L` | toggle line numbers |
| `B` | toggle blame gutter |
| `W` | toggle word-diff highlighting |
| `.` | toggle hunk in collapsed view |
| `space` | mark file as reviewed |
| `F` | show unreviewed files |
| `u` | show/hide untracked files |
| `f` | filter files |
| `T` | theme selector |
| `i` | show review info popup |
| `R` | reload diff from VCS |
| `q` | quit (or pop back to the browser) |
| `Q` | discard annotations and quit |
| `?` | show help |
| `esc` | dismiss / cancel |
| mouse | scroll wheel, click to select/annotate |

All keybindings are user-configurable — see `--keys` / `--dump-keys` below, and
`~/.config/ydiff/keybindings`.

**The annotation contract is preserved exactly**, because integrations depend on it:

```
## app/handler.go:43 (+)
use errors.Is() instead of direct comparison
```

Same markdown records, same `--output` file, same stdout fallback, same exit code 10
under `--exit-code-on-annotations`, same `--annotations` reload.

## Flags

Inherited from revdiff and unchanged: `--only`, `--output`, `--wrap`, `--staged`,
`--untracked`, `-A/--all-files`, `-I/--include`, `-X/--exclude`, the display and theming
flags, `--annotations`, `--exit-code-on-annotations`, `--config`, `--keys`, and the
positional `[base] [against]` forms. Run `ydiff --help` for the full list.

**Removed relative to revdiff:** `--vim-motion`, and the Mercurial/Jujutsu revset
translation behind the positional refs (this fork is git-only).

**Added for the browser and base-branch resolution:**

| Flag | Purpose |
|---|---|
| `--base-branch` | override for `branch`-scope base resolution (e.g. `origin/main`); wins over auto-detection and over any `--base-branch-repo` entry |
| `--base-branch-repo` | per-repository override, `repo-root-path:branch` (repeatable; config-file or CLI-map only, no env var); consulted only when `--base-branch` is empty |
| `--browser` | force the browser screen even when diff arguments are present |
| `--browser-widths` | the parent,current,changed column proportions for the browser, default `15,35,50` (normalized, need not sum to 100); at the medium tier the current/changed proportions carry over to the two remaining columns |

`--base-branch` is deliberately not `--base`: the positional arguments are already called
`base` and `against`, and this flag names the default branch used to find a fork point,
not the left side of a comparison.

Config file precedence, for anything settable via flag, env var, and config file: an
explicit CLI flag always wins; after that, in this fork's go-flags setup, a value set in
the config file wins over the same-named environment variable (this is upstream's
existing parsing order, not something changed here) — see `app/config.go`.

## Documentation index

- [`docs/2026-09-14-ydiff-design.md`](docs/2026-09-14-ydiff-design.md) — full design
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — package layout, data flow, interfaces
- [`CLAUDE.md`](CLAUDE.md) — conventions for working in this codebase
- [`UPSTREAM.md`](UPSTREAM.md) — fork point and how to pull future upstream fixes
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — contribution guidelines
