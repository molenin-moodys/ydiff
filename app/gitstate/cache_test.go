package gitstate

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingLoader returns a Loader that records how many times it was invoked, and lets
// tests control what a given (root, scope) call returns.
func countingLoader(results map[string][]ChangedFile, errs map[string]error) (*Cache, *int32) {
	var calls int32
	c := NewCache(func(root string, scope Scope) ([]ChangedFile, error) {
		atomic.AddInt32(&calls, 1)
		key := root + "|" + string(scope)
		if err, ok := errs[key]; ok && err != nil {
			return nil, err
		}
		return results[key], nil
	})
	return c, &calls
}

func TestCache_RepeatCallDoesNotReinvokeLoader(t *testing.T) {
	c, calls := countingLoader(
		map[string][]ChangedFile{"/repo|uncommitted": {{Path: "a.txt", Status: StatusModified}}},
		nil,
	)

	files1, err := c.Get("/repo", ScopeUncommitted)
	require.NoError(t, err)
	files2, err := c.Get("/repo", ScopeUncommitted)
	require.NoError(t, err)

	assert.Equal(t, files1, files2)
	assert.EqualValues(t, 1, atomic.LoadInt32(calls))
}

func TestCache_InvalidateForcesRecompute(t *testing.T) {
	c, calls := countingLoader(
		map[string][]ChangedFile{"/repo|uncommitted": {{Path: "a.txt", Status: StatusModified}}},
		nil,
	)

	_, err := c.Get("/repo", ScopeUncommitted)
	require.NoError(t, err)
	_, err = c.Get("/repo", ScopeUncommitted)
	require.NoError(t, err)
	assert.EqualValues(t, 1, atomic.LoadInt32(calls))

	c.Invalidate("/repo", ScopeUncommitted)

	_, err = c.Get("/repo", ScopeUncommitted)
	require.NoError(t, err)
	assert.EqualValues(t, 2, atomic.LoadInt32(calls))
}

func TestCache_DifferentScopesCacheIndependently(t *testing.T) {
	c, calls := countingLoader(
		map[string][]ChangedFile{
			"/repo|uncommitted": {{Path: "a.txt", Status: StatusModified}},
			"/repo|branch":      {{Path: "b.txt", Status: StatusAdded}},
		},
		nil,
	)

	filesUncommitted, err := c.Get("/repo", ScopeUncommitted)
	require.NoError(t, err)
	filesBranch, err := c.Get("/repo", ScopeBranch)
	require.NoError(t, err)

	assert.NotEqual(t, filesUncommitted, filesBranch)
	assert.EqualValues(t, 2, atomic.LoadInt32(calls))

	// repeating both scopes should not trigger additional loads
	_, err = c.Get("/repo", ScopeUncommitted)
	require.NoError(t, err)
	_, err = c.Get("/repo", ScopeBranch)
	require.NoError(t, err)
	assert.EqualValues(t, 2, atomic.LoadInt32(calls))
}

func TestCache_DifferentRootsCacheIndependently(t *testing.T) {
	c, calls := countingLoader(
		map[string][]ChangedFile{
			"/repo-a|uncommitted": {{Path: "a.txt", Status: StatusModified}},
			"/repo-b|uncommitted": {{Path: "b.txt", Status: StatusAdded}},
		},
		nil,
	)

	filesA, err := c.Get("/repo-a", ScopeUncommitted)
	require.NoError(t, err)
	filesB, err := c.Get("/repo-b", ScopeUncommitted)
	require.NoError(t, err)

	assert.NotEqual(t, filesA, filesB)
	assert.EqualValues(t, 2, atomic.LoadInt32(calls))

	_, err = c.Get("/repo-a", ScopeUncommitted)
	require.NoError(t, err)
	_, err = c.Get("/repo-b", ScopeUncommitted)
	require.NoError(t, err)
	assert.EqualValues(t, 2, atomic.LoadInt32(calls))
}

func TestCache_ErrorResultIsNotCached(t *testing.T) {
	var succeed int32 // 0 = fail, 1 = succeed
	c := NewCache(func(root string, scope Scope) ([]ChangedFile, error) {
		if atomic.LoadInt32(&succeed) == 0 {
			return nil, errors.New("transient git failure")
		}
		return []ChangedFile{{Path: "a.txt", Status: StatusModified}}, nil
	})

	_, err := c.Get("/repo", ScopeUncommitted)
	require.Error(t, err)

	// the failed call must not have been cached: flip the loader to succeed and
	// confirm the next Get recomputes (and succeeds) rather than replaying the error.
	atomic.StoreInt32(&succeed, 1)
	files, err := c.Get("/repo", ScopeUncommitted)
	require.NoError(t, err)
	assert.Len(t, files, 1)
}

// TestCache_ConcurrentReadWrite exercises the cache from many goroutines at once,
// mixing Get and Invalidate calls on overlapping keys, and must pass under `go test
// -race`. This mirrors how later tasks load gitstate through Bubble Tea commands,
// which run in their own goroutines, so the cache is written concurrently, not just
// read.
func TestCache_ConcurrentReadWrite(t *testing.T) {
	roots := []string{"/repo-a", "/repo-b", "/repo-c"}
	scopes := []Scope{ScopeUncommitted, ScopeBranch}

	c := NewCache(func(root string, scope Scope) ([]ChangedFile, error) {
		return []ChangedFile{{Path: fmt.Sprintf("%s-%s.txt", root, scope), Status: StatusModified}}, nil
	})

	const goroutines = 32
	const iterations = 100

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				root := roots[(g+i)%len(roots)]
				scope := scopes[(g+i)%len(scopes)]
				if i%7 == 0 {
					c.Invalidate(root, scope)
					continue
				}
				_, err := c.Get(root, scope)
				assert.NoError(t, err)
			}
		}(g)
	}
	wg.Wait()
}
