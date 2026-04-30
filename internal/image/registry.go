package image

import (
	"fmt"
	"image"
	"sync"
	"sync/atomic"
)

// Registry mints stable kitty image IDs per (cache key, target cells) pair.
// Lookup returns (id, fresh): fresh is true the first time the ID is
// requested, indicating the caller must transmit the image bytes.
type Registry struct {
	next atomic.Uint32
	mu   sync.Mutex
	ids  map[string]uint32
}

// NewRegistry constructs a registry. IDs start at 1 (kitty rejects 0).
func NewRegistry() *Registry {
	r := &Registry{ids: map[string]uint32{}}
	r.next.Store(1)
	return r
}

// Lookup returns a stable ID for the given (key, target) pair.
// fresh is true on the first call for a given pair.
func (r *Registry) Lookup(key string, target image.Point) (id uint32, fresh bool) {
	k := registryKey(key, target)
	r.mu.Lock()
	defer r.mu.Unlock()
	if id, ok := r.ids[k]; ok {
		return id, false
	}
	id = r.next.Add(1)
	r.ids[k] = id
	return id, true
}

func registryKey(key string, target image.Point) string {
	return fmt.Sprintf("%s|%dx%d", key, target.X, target.Y)
}
