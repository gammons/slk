package image

import (
	"container/list"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Cache is a size-bounded LRU disk cache for image bytes.
//
// Files are stored at <dir>/<key>.<ext>. Eviction is LRU by mtime: on Put,
// oldest entries are deleted until total disk usage <= cap. A single entry
// larger than cap is admitted but flagged for eviction on the next sweep.
type Cache struct {
	dir   string
	capB  int64
	mu    sync.Mutex
	items map[string]*item
	lru   *list.List // front = most recently used
	total int64
}

type item struct {
	key   string
	path  string
	size  int64
	atime time.Time
	elem  *list.Element
}

// NewCache creates (and indexes) a cache at dir with capMB megabyte cap.
// Existing files in dir are picked up at startup.
func NewCache(dir string, capMB int64) (*Cache, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	c := &Cache{
		dir:   dir,
		capB:  capMB * 1024 * 1024,
		items: map[string]*item{},
		lru:   list.New(),
	}
	if err := c.loadIndex(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Cache) loadIndex() error {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return err
	}
	type entryInfo struct {
		name  string
		size  int64
		atime time.Time
	}
	var infos []entryInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		st, err := e.Info()
		if err != nil {
			continue
		}
		infos = append(infos, entryInfo{e.Name(), st.Size(), st.ModTime()})
	}
	// Sort oldest first so PushFront yields a list with newest at front.
	sort.Slice(infos, func(i, j int) bool { return infos[i].atime.Before(infos[j].atime) })
	for _, info := range infos {
		key := strings.TrimSuffix(info.name, filepath.Ext(info.name))
		it := &item{
			key:   key,
			path:  filepath.Join(c.dir, info.name),
			size:  info.size,
			atime: info.atime,
		}
		it.elem = c.lru.PushFront(it)
		c.items[key] = it
		c.total += info.size
	}
	return nil
}

// Get returns the path to a cached entry and refreshes its LRU position.
func (c *Cache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	it, ok := c.items[key]
	if !ok {
		return "", false
	}
	now := time.Now()
	_ = os.Chtimes(it.path, now, now)
	it.atime = now
	c.lru.MoveToFront(it.elem)
	return it.path, true
}

// Put writes data to the cache under key with the given extension (no dot)
// and returns the on-disk path. Triggers LRU eviction to stay under cap.
//
// Eviction sweeps BEFORE the new entry is added: oldest entries are removed
// until existing total <= cap. The freshly-added entry is then admitted even
// if it pushes total back above cap — this gives "oversize-single-entry"
// semantics; the next Put will evict it as the oldest.
func (c *Cache) Put(key, ext string, data []byte) (string, error) {
	if ext == "" {
		ext = "bin"
	}
	path := filepath.Join(c.dir, key+"."+ext)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Replace existing entry if present (do this before sweep so the old
	// version doesn't get counted twice and isn't preferentially evicted).
	if old, ok := c.items[key]; ok {
		c.total -= old.size
		c.lru.Remove(old.elem)
		delete(c.items, key)
	}

	// Pre-sweep: trim oldest entries until existing total fits in cap.
	c.evictLocked()

	now := time.Now()
	it := &item{key: key, path: path, size: int64(len(data)), atime: now}
	it.elem = c.lru.PushFront(it)
	c.items[key] = it
	c.total += it.size

	return path, nil
}

// evictLocked removes oldest entries (LRU back) while total exceeds cap.
// Caller must hold c.mu.
func (c *Cache) evictLocked() {
	for c.total > c.capB && c.lru.Len() > 0 {
		back := c.lru.Back()
		if back == nil {
			return
		}
		it := back.Value.(*item)
		c.lru.Remove(back)
		delete(c.items, it.key)
		c.total -= it.size
		_ = os.Remove(it.path)
	}
}

// Delete removes the entry for key from the cache (in-memory index, LRU
// list, and on-disk file). Safe to call when the key is not present;
// returns true if an entry was actually removed.
//
// Used by the fetcher to evict cache entries whose contents fail to decode
// (e.g., when an auth-failure response was previously persisted as if it
// were image bytes), so the next Fetch re-downloads.
func (c *Cache) Delete(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	it, ok := c.items[key]
	if !ok {
		return false
	}
	c.lru.Remove(it.elem)
	delete(c.items, key)
	c.total -= it.size
	_ = os.Remove(it.path)
	return true
}

// Stats returns current totals (for logging).
func (c *Cache) Stats() (entries int, totalBytes int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.Len(), c.total
}

// String formats cache stats for logs.
func (c *Cache) String() string {
	n, b := c.Stats()
	return fmt.Sprintf("image.Cache{dir=%s entries=%d bytes=%d cap=%d}", c.dir, n, b, c.capB)
}
