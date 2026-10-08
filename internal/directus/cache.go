package directus

import (
	"context"
	"encoding/json"
	"net/url"
	"sync"
)

// listCache holds one full listing per system collection for the life of the
// provider process (one plan or one apply walk). Directus config is large
// (thousands of fields and permissions); a per-resource GET would be one
// request per object, so every resource of a kind reads from one shared list.
//
// Concurrent callers for the same path wait on a single in-flight request. A
// failed fetch is not cached, so the next caller retries. Writes invalidate the
// path they touched, so a read after a write in the same process sees it.
type listCache struct {
	mu      sync.Mutex
	entries map[string]*cacheEntry
}

type cacheEntry struct {
	done chan struct{}
	data json.RawMessage
	err  error
}

func newListCache() *listCache {
	return &listCache{entries: map[string]*cacheEntry{}}
}

// list returns the cached "data" array for path, fetching it once with
// limit=-1 (all rows) if absent.
func (c *Client) list(ctx context.Context, path string) (json.RawMessage, error) {
	lc := c.cache
	lc.mu.Lock()
	if e, ok := lc.entries[path]; ok {
		lc.mu.Unlock()
		select {
		case <-e.done:
			return e.data, e.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	e := &cacheEntry{done: make(chan struct{})}
	lc.entries[path] = e
	lc.mu.Unlock()

	var raw json.RawMessage
	e.err = c.do(ctx, "GET", path, url.Values{"limit": {"-1"}}, nil, &raw)
	e.data = raw
	if e.err != nil {
		lc.mu.Lock()
		if lc.entries[path] == e {
			delete(lc.entries, path)
		}
		lc.mu.Unlock()
	}
	close(e.done)
	return e.data, e.err
}

// invalidate drops the cached listing for path.
func (c *Client) invalidate(path string) {
	c.cache.mu.Lock()
	delete(c.cache.entries, path)
	c.cache.mu.Unlock()
}
