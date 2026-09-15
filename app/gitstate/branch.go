package gitstate

import (
	"bytes"
	"fmt"
	"os/exec"
)

// BranchScope reports the files changed on the current branch since it diverged from
// its base: everything `git diff --name-status <base>...HEAD` would show. The base is
// resolved via ResolveBase(repo, override); if no base can be resolved, BranchScope
// returns ErrNoBase rather than an empty list — an empty list means "nothing changed
// since a known base", a different fact from "we don't know the base", and the UI
// needs to render those two cases differently.
//
// The three-dot range is deliberate: it diffs HEAD against the merge base (the fork
// point) rather than against base's current tip, so commits made on base after the
// branch diverged do not appear as "changes" on this branch.
//
// A branch with no commits since diverging from its base returns an empty, non-nil
// slice and a nil error.
func BranchScope(repo *Repo, override string) ([]ChangedFile, error) {
	base, err := ResolveBase(repo, override)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command("git", "-C", repo.Root,
		"diff", "--name-status", "-z", base+"...HEAD")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gitstate: git diff --name-status in %q: %w", repo.Root, err)
	}

	files, err := parseNameStatus(out)
	if err != nil {
		return nil, fmt.Errorf("gitstate: parse git diff --name-status output: %w", err)
	}
	return files, nil
}

// parseNameStatus parses the NUL-separated output of
// `git diff --name-status -z <base>...HEAD`.
//
// [decision] like parsePorcelainV2 in status.go, we use -z for the same reason: raw,
// unquoted paths (spaces, unicode and all) and unambiguous splitting on NUL, since paths
// cannot themselves contain a NUL byte. Verified empirically (not assumed): with -z,
// git NUL-separates every field of a record, including a rename/copy record's old and
// new path — it does not keep them tab-separated within one NUL-terminated record. So
// this parser reads a flat stream of NUL-separated tokens: a status code token, followed
// by one path token for an ordinary change, or two path tokens (old, then new) for a
// rename or copy.
func parseNameStatus(out []byte) ([]ChangedFile, error) {
	// trim a single trailing NUL so we don't produce a bogus empty trailing token.
	out = bytes.TrimSuffix(out, []byte{0})
	if len(out) == 0 {
		return []ChangedFile{}, nil
	}

	tokens := bytes.Split(out, []byte{0})

	var result []ChangedFile
	for i := 0; i < len(tokens); i++ {
		code := string(tokens[i])
		if code == "" {
			continue
		}

		switch code[0] {
		case 'R', 'C':
			// <status><score> <old> <new>: two more NUL-separated tokens follow.
			if i+2 >= len(tokens) {
				return nil, fmt.Errorf(
					"gitstate: git diff --name-status rename/copy record missing paths: %q", code)
			}
			oldPath := string(tokens[i+1])
			newPath := string(tokens[i+2])
			i += 2
			result = append(result, ChangedFile{
				OldPath: oldPath,
				Path:    newPath,
				Status:  StatusRenamed,
			})

		default:
			// <status>: one more NUL-separated token (the path) follows.
			if i+1 >= len(tokens) {
				return nil, fmt.Errorf(
					"gitstate: git diff --name-status record missing path: %q", code)
			}
			i++
			result = append(result, ChangedFile{
				Path:   string(tokens[i]),
				Status: statusFromNameStatusCode(code[0]),
			})
		}
	}

	if result == nil {
		result = []ChangedFile{}
	}
	return result, nil
}

// statusFromNameStatusCode reduces a `git diff --name-status` status letter (with any
// trailing similarity score already stripped by the caller) to a ChangedFile.Status.
func statusFromNameStatusCode(c byte) Status {
	switch c {
	case 'M':
		return StatusModified
	case 'A':
		return StatusAdded
	case 'D':
		return StatusDeleted
	default:
		return Status(string(c))
	}
}
