package browser

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Entry is one item in a directory listing.
type Entry struct {
	Name          string // base name, as returned by the filesystem
	IsDir         bool   // true for a directory, or a symlink that resolves to one
	IsSymlink     bool   // true for a symlink, whether or not it resolves
	SymlinkBroken bool   // true when IsSymlink and the target cannot be resolved
}

// Enterable reports whether the entry can be navigated into: an ordinary directory, or
// a symlink that resolves to one. Broken symlinks and plain files are not enterable.
func (e Entry) Enterable() bool {
	return e.IsDir
}

// Listing is the result of reading one directory: the directory itself and its entries,
// sorted directories-first then case-insensitively alphabetical within each group.
type Listing struct {
	Dir     string
	Entries []Entry
}

// Read lists the contents of dir. Hidden entries (dot-prefixed names) are excluded
// unless showHidden is set. Symlinks are followed only to determine whether they point
// at a directory and whether they are broken; a broken symlink is flagged rather than
// treated as an error.
//
// Read never panics and never terminates the process: any filesystem error (dir does
// not exist, is not a directory, or cannot be read) is returned as an error for the
// caller to render inline, alongside a zero-value Listing.
func Read(dir string, showHidden bool) (Listing, error) {
	f, err := os.Open(dir)
	if err != nil {
		return Listing{}, fmt.Errorf("browser: open %q: %w", dir, err)
	}
	defer f.Close() //nolint:errcheck // read-only handle, nothing to flush

	names, err := f.Readdirnames(-1)
	if err != nil {
		return Listing{}, fmt.Errorf("browser: read %q: %w", dir, err)
	}

	entries := make([]Entry, 0, len(names))
	for _, name := range names {
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}

		entry, ok := readEntry(dir, name)
		if !ok {
			// vanished between Readdirnames and Lstat (e.g. concurrent removal);
			// skip it rather than fail the whole listing.
			continue
		}
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	return Listing{Dir: dir, Entries: entries}, nil
}

// readEntry stats one directory child and classifies it. ok is false only when the
// entry could not be stat'd at all (e.g. it vanished); a broken symlink still yields
// ok == true, with SymlinkBroken set.
func readEntry(dir, name string) (entry Entry, ok bool) {
	full := filepath.Join(dir, name)

	lst, err := os.Lstat(full)
	if err != nil {
		return Entry{}, false
	}

	entry = Entry{Name: name}

	if lst.Mode()&os.ModeSymlink != 0 {
		entry.IsSymlink = true
		st, err := os.Stat(full) // follows the symlink
		if err != nil {
			entry.SymlinkBroken = true
		} else {
			entry.IsDir = st.IsDir()
		}
		return entry, true
	}

	entry.IsDir = lst.IsDir()
	return entry, true
}
