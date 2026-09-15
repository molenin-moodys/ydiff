package gitstate

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNoRepository is returned by Resolve when dir is not inside a git repository —
// no .git entry (directory or file, as in a worktree or submodule) is found walking up
// from dir to the filesystem root. Callers distinguish this from other failures with
// errors.Is(err, ErrNoRepository).
var ErrNoRepository = errors.New("gitstate: not a git repository")

// Repo is a resolved git repository: a root directory that git itself confirmed.
type Repo struct {
	Root string // absolute path to the repository's top-level directory
}

// Resolve walks up from dir looking for a .git entry — a directory in an ordinary
// repository, a file in a worktree or submodule — then confirms the finding by asking
// git for the repository's top-level directory via `git rev-parse --show-toplevel`.
//
// If dir is not inside a git repository, Resolve returns ErrNoRepository rather than a
// generic error, so callers can tell "no repository here" apart from an actual failure
// (e.g. dir no longer existing, or git failing unexpectedly).
func Resolve(dir string) (*Repo, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("gitstate: resolve absolute path for %q: %w", dir, err)
	}

	if _, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("gitstate: %q: %w", dir, err)
	}

	found := abs
	for {
		if _, err := os.Stat(filepath.Join(found, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(found)
		if parent == found {
			return nil, ErrNoRepository
		}
		found = parent
	}

	cmd := exec.Command("git", "-C", found, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gitstate: git rev-parse --show-toplevel in %q: %w", found, err)
	}

	return &Repo{Root: strings.TrimSpace(string(out))}, nil
}
