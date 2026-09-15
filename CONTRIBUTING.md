# Contributing to ydiff

ydiff is a fork of [revdiff](https://github.com/umputun/revdiff) built for a specific
purpose (running inside environments where third-party binaries can't be installed) and
maintained accordingly — see [`UPSTREAM.md`](UPSTREAM.md) for the fork rationale and how
to pull future upstream fixes.

## Before You Start

### Check existing functionality

Before adding a feature, make sure it doesn't already exist. Run `ydiff --help`, check
[`README.md`](README.md) and [`docs/2026-09-14-ydiff-design.md`](docs/2026-09-14-ydiff-design.md),
and try the feature before proposing new code.

### Is it worth it?

Before submitting a PR, weigh what the feature adds against the code it introduces:

- **Does it belong in ydiff or upstream?** If a shared piece of the diff/review engine
  (chroma, bubbletea, the annotation model, etc.) is missing something, prefer fixing it
  in a way that stays easy to reconcile with revdiff — see `UPSTREAM.md` on pulling fixes.
- **Does it change the tool's scope?** ydiff is a filesystem browser plus a diff/file
  review TUI. PRs that expand it into something else (a general file manager, a staging
  tool) are out of scope for v1.
- **Is the code proportional to the value?** Keep it simple, keep it small.

## Development Setup

1. Clone the repository
2. Create a feature branch: `git checkout -b feature-name`
3. Make your changes
4. Run tests: `make test`
5. Run the linter: `make lint`
6. Format code: `make fmt`
7. Commit your changes
8. Push the branch and open a pull request

## Code Style

Follow the conventions in [`CLAUDE.md`](CLAUDE.md).

## Issues and PRs

Every issue and PR should clearly describe:

1. **What is the problem?** — what exactly is broken, missing, or inconvenient? Be
   specific. "It would be nice to have X" is not a problem statement.
2. **How does this solve it?** — why this approach, and how it addresses the root cause.

## Pull Request Process

1. Update `README.md` with details of changes if applicable.
2. The PR should build and pass tests (`go build ./...`, `go vet ./...`, `go test -race
   ./...`).
3. If the change diverges from `docs/2026-09-14-ydiff-design.md`, update that doc too.
