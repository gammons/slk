package main

import "sync"

// railThreadsCache answers "does this workspace have an unread
// subscribed thread?" for the workspace rail without making the caller
// wait for SQLite.
//
// The rail reader (railUnreadWorkspaces) runs on the UI goroutine on
// every read-state event, including every message in a channel that is
// not open. Its thread half is cache.ListSubscribedThreads, five
// correlated subqueries per subscription. Since the subscription sync
// pages the whole list (up to 1000 per workspace), that query held the
// UI goroutine ~820ms per event in a 2026-10-05 debug log, and typed
// keys queued behind it.
//
// Unread returns the last computed answer immediately and schedules a
// recompute on a background goroutine. When a recompute changes the
// answer, the notifier installed by SetNotify is told the team ID;
// main.go sends ReadStateChangedMsg, the UI refreshes the rail, and
// that refresh's own recompute finds nothing changed, so it ends there.
// Until the first recompute finishes, a team reads as not unread.
//
// One worker per team at most: lookups that arrive while a query runs
// collapse into a single rerun with the newest self user ID.
type railThreadsCache struct {
	query func(teamID, selfUserID string) bool

	mu     sync.Mutex
	teams  map[string]*railThreadsTeam
	notify func(teamID string)
}

type railThreadsTeam struct {
	unread  bool
	self    string // self user ID for the next query
	running bool   // a worker owns this team
	again   bool   // a lookup arrived while the worker was querying
}

// newRailThreadsCache wraps query, normally railThreadsUnread(db).
func newRailThreadsCache(query func(teamID, selfUserID string) bool) *railThreadsCache {
	return &railThreadsCache{query: query, teams: map[string]*railThreadsTeam{}}
}

// SetNotify installs the function told of each team whose answer
// changes. Until it is set, changes are stored and served by Unread but
// not reported: main.go wires the reader before the bubbletea program
// that the notifier sends to exists.
func (c *railThreadsCache) SetNotify(notify func(teamID string)) {
	c.mu.Lock()
	c.notify = notify
	c.mu.Unlock()
}

// Unread has railThreadsUnread's signature, so it drops into
// railUnreadWorkspaces as the threadsUnread input. It never blocks on
// the query.
func (c *railThreadsCache) Unread(teamID, selfUserID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.teams[teamID]
	if t == nil {
		t = &railThreadsTeam{}
		c.teams[teamID] = t
	}
	t.self = selfUserID
	if t.running {
		t.again = true
	} else {
		t.running = true
		go c.refresh(teamID, t)
	}
	return t.unread
}

// refresh runs the query for one team until no lookup arrived during
// the last run, reporting each run that changes the answer. The worker
// keeps the team until its report is sent, so a team's queries and
// reports never overlap. The report is sent outside mu: main.go's
// notifier blocks until the UI goroutine takes the message, and the UI
// goroutine may be waiting for mu in Unread.
func (c *railThreadsCache) refresh(teamID string, t *railThreadsTeam) {
	for {
		c.mu.Lock()
		self := t.self
		t.again = false
		c.mu.Unlock()

		unread := c.query(teamID, self)

		c.mu.Lock()
		changed := unread != t.unread
		t.unread = unread
		notify := c.notify
		c.mu.Unlock()

		if changed && notify != nil {
			notify(teamID)
		}

		c.mu.Lock()
		if !t.again {
			t.running = false
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()
	}
}
