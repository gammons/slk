package main

import (
	"sync"
	"time"
)

// onceTTL bounds a onceMap's memory without trusting clients to agree
// on clocks; a minute covers any cross-socket delivery skew by orders
// of magnitude.
const onceTTL = time.Minute

// onceMap remembers which keys an effect has already fired for, so a
// message frame delivered to more than one session of an Enterprise
// Grid runs that effect exactly once. The grid's event gate never drops
// the ACTIVE socket's copy, so an inactive sibling's copy can arrive
// first and run the effect, then the active copy runs it again;
// cache writes and notifications keyed per message need this guard or
// they double.
type onceMap struct {
	mu   sync.Mutex
	seen map[string]time.Time
	// now is swapped in tests.
	now func() time.Time
}

// once reports whether the effect should fire for key, and records it
// either way. o.now must be non-nil before the first call - the
// package-level maps initialize it to time.Now; tests may swap it
// before their first once call, single-threaded by construction.
func (o *onceMap) once(key string) bool {
	now := o.now()
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.seen == nil {
		o.seen = map[string]time.Time{}
	}
	if exp, ok := o.seen[key]; ok && now.Before(exp) {
		return false
	}
	if len(o.seen) >= 1024 {
		for k, exp := range o.seen {
			if !now.Before(exp) {
				delete(o.seen, k)
			}
		}
	}
	o.seen[key] = now.Add(onceTTL)
	return true
}

func (o *onceMap) reset() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seen = map[string]time.Time{}
}

// notifyOnce suppresses a duplicate desktop notification for one
// message, keyed by (channel, ts).
var notifyOnceMap = onceMap{now: time.Now}

func notifyOnce(channelID, ts string) bool {
	return notifyOnceMap.once(channelID + "/" + ts)
}

// mentionBumpedOnce suppresses a duplicate IncrementChannelMentionCount
// for one message, keyed the same way. A separate map from
// notifyOnce's: a notification and a badge are different effects, and
// sharing a key would let whichever fires first silence the other.
var mentionOnceMap = onceMap{now: time.Now}

func mentionBumpedOnce(channelID, ts string) bool {
	return mentionOnceMap.once(channelID + "/" + ts)
}
