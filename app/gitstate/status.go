package gitstate

import (
	"bytes"
	"fmt"
	"os/exec"
)

// UncommittedStatus reports the changed files in repo's working tree and index —
// everything `git status` would show, uncommitted relative to HEAD. A clean repository
// returns an empty, non-nil slice and a nil error.
func UncommittedStatus(repo *Repo) ([]ChangedFile, error) {
	cmd := exec.Command("git", "-C", repo.Root,
		"status", "--porcelain=v2", "--untracked-files=normal", "-z")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gitstate: git status in %q: %w", repo.Root, err)
	}

	files, err := parsePorcelainV2(out)
	if err != nil {
		return nil, fmt.Errorf("gitstate: parse git status output: %w", err)
	}
	return files, nil
}

// parsePorcelainV2 parses the NUL-separated output of
// `git status --porcelain=v2 --untracked-files=normal -z`.
//
// [decision] we use the -z flag rather than parsing git's C-style quoting of the
// default (non -z) porcelain v2 output. With -z, paths are emitted as raw bytes
// (spaces, unicode and all) and are never quoted, and a rename record's two paths
// (new, then original) are simply two consecutive NUL-terminated fields instead of one
// quoted "new\told" pair. That removes an entire unquoting implementation (and its edge
// cases) in exchange for splitting on NUL, which is unambiguous because paths cannot
// contain a NUL byte.
func parsePorcelainV2(out []byte) ([]ChangedFile, error) {
	// trim a single trailing NUL so we don't produce a bogus empty trailing field.
	out = bytes.TrimSuffix(out, []byte{0})
	if len(out) == 0 {
		return []ChangedFile{}, nil
	}

	fields := bytes.Split(out, []byte{0})

	var result []ChangedFile
	for i := 0; i < len(fields); i++ {
		line := string(fields[i])
		if line == "" {
			continue
		}

		switch line[0] {
		case '1':
			cf, err := parseOrdinaryLine(line)
			if err != nil {
				return nil, err
			}
			result = append(result, cf)

		case '2':
			cf, err := parseRenameLine(line)
			if err != nil {
				return nil, err
			}
			i++
			if i >= len(fields) {
				return nil, fmt.Errorf("gitstate: porcelain v2 rename record missing original path: %q", line)
			}
			cf.OldPath = string(fields[i])
			result = append(result, cf)

		case 'u':
			cf, err := parseUnmergedLine(line)
			if err != nil {
				return nil, err
			}
			result = append(result, cf)

		case '?':
			path, ok := splitPrefix(line, "? ")
			if !ok {
				return nil, fmt.Errorf("gitstate: malformed porcelain v2 untracked line: %q", line)
			}
			result = append(result, ChangedFile{Path: path, Status: StatusUntracked})

		case '!':
			// ignored files: not requested (--ignored is not passed), but tolerate
			// them defensively rather than erroring on a line we simply don't need.
			continue

		default:
			return nil, fmt.Errorf("gitstate: unrecognized porcelain v2 record type: %q", line)
		}
	}

	if result == nil {
		result = []ChangedFile{}
	}
	return result, nil
}

// splitPrefix returns the remainder of s after prefix, and whether s actually had
// that prefix.
func splitPrefix(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || s[:len(prefix)] != prefix {
		return "", false
	}
	return s[len(prefix):], true
}

// parseOrdinaryLine parses a porcelain v2 "1 ..." (ordinary changed entry) line:
//
//	1 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <path>
func parseOrdinaryLine(line string) (ChangedFile, error) {
	const minFields = 9
	fields := splitFieldsN(line, minFields)
	if len(fields) < minFields {
		return ChangedFile{}, fmt.Errorf("gitstate: malformed porcelain v2 ordinary line (want %d fields, got %d): %q",
			minFields, len(fields), line)
	}
	xy := fields[1]
	if len(xy) != 2 {
		return ChangedFile{}, fmt.Errorf("gitstate: malformed porcelain v2 XY status %q: %q", xy, line)
	}
	path := fields[minFields-1]
	return ChangedFile{Path: path, Status: statusFromXY(xy[0], xy[1])}, nil
}

// parseRenameLine parses a porcelain v2 "2 ..." (renamed/copied entry) line, up to but
// not including the trailing original-path field, which arrives as the following
// NUL-separated token and is filled in by the caller:
//
//	2 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <X><score> <path>
func parseRenameLine(line string) (ChangedFile, error) {
	const minFields = 10
	fields := splitFieldsN(line, minFields)
	if len(fields) < minFields {
		return ChangedFile{}, fmt.Errorf("gitstate: malformed porcelain v2 rename line (want %d fields, got %d): %q",
			minFields, len(fields), line)
	}
	path := fields[minFields-1]
	return ChangedFile{Path: path, Status: StatusRenamed}, nil
}

// parseUnmergedLine parses a porcelain v2 "u ..." (unmerged entry) line:
//
//	u <XY> <sub> <m1> <m2> <m3> <mW> <h1> <h2> <h3> <path>
func parseUnmergedLine(line string) (ChangedFile, error) {
	const minFields = 11
	fields := splitFieldsN(line, minFields)
	if len(fields) < minFields {
		return ChangedFile{}, fmt.Errorf("gitstate: malformed porcelain v2 unmerged line (want %d fields, got %d): %q",
			minFields, len(fields), line)
	}
	path := fields[minFields-1]
	return ChangedFile{Path: path, Status: Status("U")}, nil
}

// splitFieldsN splits line into exactly n whitespace-separated fields, with the last
// field taking whatever remains (so a path containing spaces stays intact, since it is
// always the final field on the line).
func splitFieldsN(line string, n int) []string {
	fields := make([]string, 0, n)
	rest := line
	for len(fields) < n-1 {
		idx := indexByte(rest, ' ')
		if idx < 0 {
			break
		}
		fields = append(fields, rest[:idx])
		rest = rest[idx+1:]
	}
	fields = append(fields, rest)
	return fields
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// statusFromXY reduces a porcelain v2 two-letter XY status (index status X, worktree
// status Y) to a single ChangedFile.Status.
//
// [decision] when a file has both a staged change and a further, unstaged worktree
// change (e.g. staged as added, then modified again — X='A', Y='M'), we report the
// worktree status, since that reflects the file's current state most directly for the
// "uncommitted" scope as a whole. If the worktree side is unchanged ('.'), we fall back
// to the index status.
func statusFromXY(x, y byte) Status {
	c := y
	if c == '.' {
		c = x
	}
	switch c {
	case 'M':
		return StatusModified
	case 'A':
		return StatusAdded
	case 'D':
		return StatusDeleted
	case 'R':
		return StatusRenamed
	default:
		return Status(string(c))
	}
}
