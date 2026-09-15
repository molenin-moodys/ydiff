package main

import (
	"errors"
	"os"

	"github.com/molenin-moodys/ydiff/app/diff"
	"github.com/molenin-moodys/ydiff/app/ui"
)

// errNoVCSRepository is returned by makeNoVCSRenderer when no VCS was
// detected and --only was not given to fall back to standalone file review.
// Task 23's browser routing path (main.go's run) distinguishes this specific
// failure from any other setupVCSRenderer error: outside a repository, the
// bare-browser entry point substitutes an empty placeholder renderer instead
// of propagating the error, since a filesystem browser that refuses to start
// outside a repository would be useless. Every other caller (the review
// entry point, and setupVCSRenderer's own tests) still sees this as an
// ordinary error.
var errNoVCSRepository = errors.New("no git repository found (use --only to review standalone files)")

type vcsSetup struct {
	renderer           ui.Renderer
	vcsType            diff.VCSType
	gitRoot            string // set only when VCS is git; used by history module to run git commands
	workDir            string
	blamer             ui.Blamer
	untrackedFn        func() ([]string, error)
	untrackedRenamesFn func([]string) ([]diff.FileEntry, error) // git-only; pairs untracked renames with their origin
	commitLogger       diff.CommitLogger                        // VCS-backed commit log source; nil when VCS lacks the capability
}

// setupVCSRenderer detects the VCS and creates the appropriate renderer, blamer, and untracked function.
func setupVCSRenderer(opts options) (vcsSetup, error) {
	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		cwd = "."
	}
	vcsType, vcsRoot := diff.DetectVCS(cwd)

	switch vcsType {
	case diff.VCSGit:
		g := diff.NewGit(vcsRoot)
		r, workDir, err := makeGitRenderer(g, opts, vcsRoot)
		if err != nil {
			return vcsSetup{}, err
		}
		return vcsSetup{renderer: r, vcsType: diff.VCSGit, gitRoot: vcsRoot, workDir: workDir, blamer: g, untrackedFn: g.UntrackedFiles, untrackedRenamesFn: g.UntrackedRenames, commitLogger: g}, nil
	default:
		r, workDir, err := makeNoVCSRenderer(opts.Only, cwd)
		if err != nil {
			return vcsSetup{}, err
		}
		return vcsSetup{renderer: r, workDir: workDir}, nil
	}
}

// makeGitRenderer selects the appropriate git renderer based on flags.
// reuses the provided *Git instance as the default renderer to avoid double allocation.
func makeGitRenderer(g *diff.Git, opts options, repoRoot string) (ui.Renderer, string, error) { //nolint:unparam // error kept for consistency with makeNoVCSRenderer
	var r ui.Renderer
	switch {
	case opts.AllFiles:
		r = diff.NewDirectoryReader(repoRoot)
	case len(opts.Only) > 0:
		r = diff.NewFallbackRenderer(g, opts.Only, repoRoot)
	default:
		r = g
	}
	return wrapFilters(r, opts), repoRoot, nil
}

// wrapFilters applies include/exclude filters to a renderer based on opts.
func wrapFilters(r ui.Renderer, opts options) ui.Renderer {
	if len(opts.Include) > 0 {
		r = diff.NewIncludeFilter(r, opts.Include)
	}
	if len(opts.Exclude) > 0 {
		r = diff.NewExcludeFilter(r, opts.Exclude)
	}
	return r
}

// filterUntracked wraps an untracked-files function so its results honor the
// --include / --exclude prefixes. The raw VCS UntrackedFiles call bypasses the
// renderer's IncludeFilter/ExcludeFilter (those only filter ChangedFiles), so
// without this wrap untracked files leak into a scoped review. Returns fn
// unchanged when fn is nil or no prefixes are set.
func filterUntracked(fn func() ([]string, error), include, exclude []string) func() ([]string, error) {
	if fn == nil || (len(include) == 0 && len(exclude) == 0) {
		return fn
	}
	return func() ([]string, error) {
		paths, err := fn()
		if err != nil {
			return nil, err
		}
		return diff.FilterPaths(paths, include, exclude), nil
	}
}

// makeNoVCSRenderer creates a renderer when no VCS is detected.
// No-VCS mode requires --only, which is mutually exclusive with --include.
// --exclude is a no-op here (FileReader only returns the --only files).
func makeNoVCSRenderer(only []string, cwd string) (ui.Renderer, string, error) {
	if len(only) == 0 {
		return nil, "", errNoVCSRepository
	}
	return diff.NewFileReader(only, cwd), cwd, nil
}
