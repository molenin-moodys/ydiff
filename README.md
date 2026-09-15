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
(MIT) — layout and navigation ideas only; no yazi source is used or vendored.

Full design: [`docs/2026-09-14-ydiff-design.md`](docs/2026-09-14-ydiff-design.md).
Implementation plan: [`docs/plans/20260914-ydiff-fork-and-browser.md`](docs/plans/20260914-ydiff-fork-and-browser.md).

Documentation covering the browser, keybindings and CLI flags will be filled in as those
pieces land (see the plan above).
