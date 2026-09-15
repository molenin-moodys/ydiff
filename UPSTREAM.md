# Upstream

ydiff is a fork of [revdiff](https://github.com/umputun/revdiff) (MIT © Umputun).

- **Fork point:** tag `v1.11.1`, commit `39da604a135865eef40714bf86e20f01c15be833`.
- **How it was imported:** `git remote add upstream https://github.com/umputun/revdiff`,
  then `git fetch upstream tag v1.11.1 --no-tags` (the tag explicitly, not `master`,
  which was already ahead of it), then
  `git merge --allow-unrelated-histories v1.11.1` into this repository's `main`
  history. The only conflict was `LICENSE`, resolved in favour of this repository's
  own MIT notice (© Misha Olenin); the original upstream license text is preserved
  verbatim in [`LICENSE-revdiff`](LICENSE-revdiff).
- **What changed after the merge:** see
  [`docs/plans/20260914-ydiff-fork-and-browser.md`](docs/plans/20260914-ydiff-fork-and-browser.md)
  for the full list of renames and removals (module path, binary name, config
  directory, environment variable prefix, default theme, dropped Mercurial/Jujutsu
  support, dropped vim-motion preset) and the new browser functionality added on top.

## Pulling a future upstream fix

To bring in a specific upstream fix or feature after this fork point:

1. Fetch the exact tag or commit you want, not `master`:
   `git fetch upstream tag <vX.Y.Z> --no-tags` (or `git fetch upstream <sha>` for an
   untagged commit).
2. Merge (not rebase) it into this repository:
   `git merge upstream/<ref-or-tag>` — do **not** use `--allow-unrelated-histories`
   again; the histories are joined now.
3. Expect conflicts anywhere this fork renamed or removed code (module path,
   `REVDIFF_*` → `YDIFF_*` env vars, config directory, hg/jj support, vim motions).
   Resolve in favour of this fork's renames/removals unless the upstream change is
   itself the fix you are pulling in, in which case reapply the rename on top of it.
4. Run `go build ./app` and `go test ./...` (or `make test`) before committing the
   merge.
5. Never fetch or merge upstream's `master` branch directly — it is not pinned and
   may include changes not yet evaluated for this fork.
