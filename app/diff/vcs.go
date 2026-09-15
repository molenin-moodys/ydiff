package diff

import (
	"os"
	"path/filepath"
)

// VCSType identifies a version control system.
type VCSType string

const (
	VCSGit  VCSType = "git"
	VCSNone VCSType = ""
)

// DetectVCS walks up from startDir looking for a .git entry.
// returns the VCS type and repo root path. If no VCS is found, returns VCSNone and empty string.
//
// Git is the sole supported VCS; the VCSType/DetectVCS abstraction is kept separate from the
// rest of the codebase (rather than hardcoding "git" at every call site) so that adding another
// VCS back later is a matter of extending this function and adding a new Renderer implementation,
// not reworking the callers.
func DetectVCS(startDir string) (VCSType, string) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return VCSNone, ""
	}

	for {
		// .git can be a file in worktrees/submodules
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return VCSGit, dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return VCSNone, ""
		}
		dir = parent
	}
}
