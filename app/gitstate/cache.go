package gitstate

import "sync"

// Loader computes the changed-file list for a repository root and scope, exactly as
// UncommittedStatus and BranchScope do. Cache calls a Loader on a cache miss; tests
// substitute a counting or error-injecting Loader in place of one that shells out to
// git.
type Loader func(root string, scope Scope) ([]ChangedFile, error)

// cacheKey identifies one cached result: a repository root plus the scope it was
// computed for. Different roots and different scopes cache independently.
type cacheKey struct {
	root  string
	scope Scope
}

// Cache memoizes Loader results per (root, scope), so repeated requests for the same
// repository and scope — e.g. redrawing the changed-files pane — do not re-invoke git.
// There is deliberately no filesystem watching or background refresh here: a cached
// entry stays until Invalidate is called explicitly, which the UI does when the user
// presses `r`, toggles scope, or returns from the review screen.
//
// Cache is safe for concurrent use: later tasks load through Bubble Tea commands,
// which run in their own goroutines, so Get and Invalidate may be called concurrently
// from multiple goroutines, not merely read concurrently.
type Cache struct {
	load Loader

	mu      sync.Mutex
	entries map[cacheKey][]ChangedFile
}

// NewCache creates a Cache that computes misses with load.
func NewCache(load Loader) *Cache {
	return &Cache{
		load:    load,
		entries: make(map[cacheKey][]ChangedFile),
	}
}

// Get returns the changed-file list for root and scope, computing and caching it via
// the Cache's Loader on a miss. A Loader error is returned to the caller but never
// cached: a transient git failure (or ErrNoBase) must not stick and poison every later
// call for the same key, so the next Get retries the Loader rather than replaying the
// error.
func (c *Cache) Get(root string, scope Scope) ([]ChangedFile, error) {
	key := cacheKey{root: root, scope: scope}

	c.mu.Lock()
	if files, ok := c.entries[key]; ok {
		c.mu.Unlock()
		return files, nil
	}
	c.mu.Unlock()

	// call the loader without holding the lock: it shells out to git, and other
	// keys should stay usable while that's in flight. A concurrent miss on the
	// same key may race and load twice; the last write wins, which is harmless
	// since both loads compute the same result.
	files, err := c.load(root, scope)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.entries[key] = files
	c.mu.Unlock()

	return files, nil
}

// Invalidate discards any cached result for root and scope, forcing the next Get to
// recompute it. Invalidating a key with nothing cached is a harmless no-op.
func (c *Cache) Invalidate(root string, scope Scope) {
	key := cacheKey{root: root, scope: scope}

	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}
