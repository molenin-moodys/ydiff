package gitstate

import (
	"errors"
	"os/exec"
	"strings"
)

// ErrNoBase is returned by ResolveBase when no base branch can be resolved: no
// explicit override was given, origin/HEAD is not set, and neither origin/main nor
// origin/master exists. Callers distinguish this from other failures with
// errors.Is(err, ErrNoBase).
//
// ResolveBase never falls back to a guess when nothing resolves: a diff against the
// wrong base is worse than no diff at all, since a reviewer silently shown the wrong
// changeset may not notice. Callers (the UI) use ErrNoBase to tell "base not found"
// apart from "no changes since the base".
var ErrNoBase = errors.New("gitstate: base branch not found")

// ResolveBase determines which ref a branch-scope diff should be taken against.
//
// Resolution order, in this exact priority:
//  1. override, if non-empty — an explicit choice always wins.
//  2. `git rev-parse --abbrev-ref origin/HEAD` — the remote's default branch, as
//     recorded locally by `git remote set-head origin -a` (or an explicit
//     symbolic-ref) after a clone or fetch.
//  3. origin/main, if it exists.
//  4. origin/master, if it exists.
//
// If none of these resolve, ResolveBase returns ErrNoBase rather than guessing.
func ResolveBase(repo *Repo, override string) (string, error) {
	if override != "" {
		return override, nil
	}

	if ref, ok := resolveOriginHead(repo); ok {
		return ref, nil
	}

	for _, candidate := range [...]string{"origin/main", "origin/master"} {
		if refExists(repo, candidate) {
			return candidate, nil
		}
	}

	return "", ErrNoBase
}

// resolveOriginHead reports the branch refs/remotes/origin/HEAD points to, if that
// symbolic ref exists and resolves to something.
func resolveOriginHead(repo *Repo) (string, bool) {
	cmd := exec.Command("git", "-C", repo.Root, "rev-parse", "--abbrev-ref", "origin/HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}

	ref := strings.TrimSpace(string(out))
	if ref == "" || ref == "HEAD" {
		// "HEAD" back means git could not abbreviate a real remote-tracking branch
		// name — treat that as unresolved rather than a usable base.
		return "", false
	}
	return ref, true
}

// refExists reports whether ref names an existing object, without producing output on
// stderr for the common "it doesn't exist" case.
func refExists(repo *Repo, ref string) bool {
	cmd := exec.Command("git", "-C", repo.Root, "rev-parse", "--verify", "--quiet", ref)
	return cmd.Run() == nil
}
