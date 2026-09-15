// Package gitstate resolves a directory to its enclosing git repository and reports
// changed-file lists for it. It is a pure data package: it shells out to the `git`
// binary, parses its output, and returns plain Go values. It knows nothing about
// rendering, Bubble Tea, or any other UI concern — callers in package ui decide how to
// present what gitstate reports.
package gitstate

// Scope selects which changed files a Repo reports.
type Scope string

// The two scopes gitstate supports: the working tree against the index/HEAD, and the
// current branch against its base branch.
const (
	ScopeUncommitted Scope = "uncommitted"
	ScopeBranch      Scope = "branch"
)

// Status is a changed-file badge, matching the letters `git status --porcelain` and
// `git diff --name-status` use.
type Status string

// Status values reported for a ChangedFile.
const (
	StatusModified  Status = "M"
	StatusAdded     Status = "A"
	StatusDeleted   Status = "D"
	StatusRenamed   Status = "R"
	StatusUntracked Status = "??"
)

// ChangedFile is one entry in a changed-files list, produced either by an
// uncommitted-scope status or a branch-scope diff.
type ChangedFile struct {
	Path    string // current path, relative to the repository root
	OldPath string // previous path for renames; empty for every other status
	Status  Status
}
