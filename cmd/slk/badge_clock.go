package main

import (
	"sync/atomic"
	"time"
)

// badgeEventClock records when the workspace's last badge_counts_updated
// arrived, as unix microseconds. The reconnect catch-up uses it to tell
// whether a badge event landed while its client.counts request was in
// flight: the event is newer than the HTTP snapshot, so the snapshot's
// total must not overwrite what the event already delivered.
//
// Stamp and read are single atomic loads/stores, safe from the socket
// read goroutine (OnBadgeCountsUpdated) and the reconnect goroutine
// concurrently.
type badgeEventClock struct {
	last atomic.Int64
}

// stamp records now as the time of the latest badge event.
func (c *badgeEventClock) stamp() {
	c.last.Store(time.Now().UnixMicro())
}

// current returns the stamp of the latest badge event (0 when none
// has arrived).
func (c *badgeEventClock) current() int64 {
	return c.last.Load()
}
