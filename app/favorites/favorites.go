// Package favorites persists the browser screen's favorite directories: a
// small flat list of absolute paths a user has starred with Ctrl+F, so they
// can jump straight back with F regardless of where the browser's cursor
// currently is.
package favorites

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/molenin-moodys/ydiff/app/fsutil"
)

// Service persists a set of favorite directory paths to a flat text file,
// one absolute path per line. The format is deliberately plain (no INI, no
// JSON) so a user can hand-edit it, mirroring app/history's own preference
// for a human-readable store over a structured one.
type Service struct {
	path string // empty = defaultPath()
}

// New creates a favorites service backed by the file at path. An empty path
// defers to defaultPath() (~/.config/ydiff/favorites) on every call, so a
// Service can be constructed before the home directory is known to matter.
func New(path string) *Service {
	return &Service{path: path}
}

// resolvedPath returns s.path, or the default location when unset.
func (s *Service) resolvedPath() string {
	if s.path != "" {
		return s.path
	}
	return defaultPath()
}

// defaultPath returns ~/.config/ydiff/favorites, or "" if the home
// directory cannot be determined.
func defaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "ydiff", "favorites")
}

// List returns the favorite paths, sorted alphabetically (case-insensitive)
// by full path — a fixed, predictable order rather than insertion or
// recency order, so a path is always found in the same place in the list.
// A missing file is not an error: it means there are no favorites yet.
func (s *Service) List() ([]string, error) {
	path := s.resolvedPath()
	if path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path) //nolint:gosec // path is either user-supplied via --config-style flag or our own default, not attacker-controlled
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading favorites: %w", err)
	}

	return parseAndSort(string(data)), nil
}

// parseAndSort splits data into non-empty, trimmed, deduplicated lines and
// sorts them case-insensitively.
func parseAndSort(data string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}

// Toggle adds dir to the favorites if absent, or removes it if present,
// persisting the result. It returns added=true when dir ended up in the
// list (the add case), false when it was removed.
func (s *Service) Toggle(dir string) (added bool, err error) {
	current, err := s.List()
	if err != nil {
		return false, err
	}

	idx := indexOf(current, dir)
	if idx >= 0 {
		current = append(current[:idx], current[idx+1:]...)
		added = false
	} else {
		current = append(current, dir)
		added = true
	}

	if err := s.write(current); err != nil {
		return false, err
	}
	return added, nil
}

// Remove removes dir from the favorites if present, persisting the result.
// Removing an absent entry is a no-op, not an error.
func (s *Service) Remove(dir string) error {
	current, err := s.List()
	if err != nil {
		return err
	}
	idx := indexOf(current, dir)
	if idx < 0 {
		return nil
	}
	current = append(current[:idx], current[idx+1:]...)
	return s.write(current)
}

// IsFavorite reports whether dir is currently in the favorites list.
func (s *Service) IsFavorite(dir string) (bool, error) {
	current, err := s.List()
	if err != nil {
		return false, err
	}
	return indexOf(current, dir) >= 0, nil
}

// write persists paths (already deduplicated by the caller) to disk,
// sorted, one per line, creating the parent directory if needed.
func (s *Service) write(paths []string) error {
	path := s.resolvedPath()
	if path == "" {
		return fmt.Errorf("favorites: cannot determine home directory")
	}

	sorted := append([]string(nil), paths...)
	sort.Slice(sorted, func(i, j int) bool {
		return strings.ToLower(sorted[i]) < strings.ToLower(sorted[j])
	})

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("creating favorites directory: %w", err)
	}

	var b strings.Builder
	for _, p := range sorted {
		b.WriteString(p)
		b.WriteString("\n")
	}

	if err := fsutil.AtomicWriteFile(path, []byte(b.String())); err != nil {
		return fmt.Errorf("writing favorites: %w", err)
	}
	return nil
}

// indexOf returns the index of dir in paths, or -1 if absent.
func indexOf(paths []string, dir string) int {
	for i, p := range paths {
		if p == dir {
			return i
		}
	}
	return -1
}
