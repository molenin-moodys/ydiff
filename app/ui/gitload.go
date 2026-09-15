package ui

import (
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/molenin-moodys/ydiff/app/gitstate"
)

// GitLoadedMsg is emitted when an asynchronous gitstate changed-files load
// finishes, whether it succeeded or failed. Dir and Scope identify which
// request this result answers — mirroring browser.LoadedMsg's Path field —
// so a caller holding an outstanding request can tell whether this result
// still answers it (the user has not since navigated to a different
// directory or toggled scope) or is stale and must be discarded rather than
// rendered.
//
// Root is the resolved repository root, empty when Err is
// gitstate.ErrNoRepository (there is nothing to key a cache entry by).
// Branch is the resolved repository's current branch name for the browser's
// status bar ("4 changed - main" in the design mock-up); it is empty when
// resolution failed or the repository is in a detached HEAD state.
type GitLoadedMsg struct {
	Dir   string
	Scope gitstate.Scope

	Root   string
	Branch string
	Files  []gitstate.ChangedFile
	Err    error
}

// LoadGitState returns a tea.Cmd that resolves dir's enclosing repository
// and loads scope's changed-file list through cache, reporting the result
// as a GitLoadedMsg. dir need not be inside a repository: gitstate.Resolve's
// error (typically gitstate.ErrNoRepository) is carried on Err exactly like
// any other resolution failure, so the changed-files pane can render its own
// "not a repository" message instead of an empty or stale list.
//
// cache may be nil, in which case LoadGitState returns nil: there is
// nothing to load without a cache to load through (tests that do not
// exercise gitstate wiring pass a nil cache to NewRootBrowser for exactly
// this reason).
func LoadGitState(cache *gitstate.Cache, dir string, scope gitstate.Scope) tea.Cmd {
	if cache == nil {
		return nil
	}
	return func() tea.Msg {
		repo, err := gitstate.Resolve(dir)
		if err != nil {
			return GitLoadedMsg{Dir: dir, Scope: scope, Err: err}
		}

		files, err := cache.Get(repo.Root, scope)
		return GitLoadedMsg{
			Dir:    dir,
			Scope:  scope,
			Root:   repo.Root,
			Branch: currentBranch(repo.Root),
			Files:  files,
			Err:    err,
		}
	}
}

// NewGitLoader returns the gitstate.Loader a gitstate.Cache is constructed
// with: UncommittedStatus for ScopeUncommitted, BranchScope (against
// baseOverride, or gitstate's own resolution order when empty) for
// ScopeBranch. baseOverride corresponds to --base-branch / repo config,
// wired through by task 22; until then it is empty.
func NewGitLoader(baseOverride string) gitstate.Loader {
	return func(root string, scope gitstate.Scope) ([]gitstate.ChangedFile, error) {
		repo := &gitstate.Repo{Root: root}
		if scope == gitstate.ScopeBranch {
			return gitstate.BranchScope(repo, baseOverride)
		}
		return gitstate.UncommittedStatus(repo)
	}
}

// currentBranch reports root's current branch name via
// `git rev-parse --abbrev-ref HEAD`, or "" if that fails or the repository
// is in a detached-HEAD state (where git reports the literal string "HEAD").
func currentBranch(root string) string {
	cmd := exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" || branch == "HEAD" {
		return ""
	}
	return branch
}
