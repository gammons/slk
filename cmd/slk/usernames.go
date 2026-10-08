package main

import (
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/slackfmt"
	"github.com/gammons/slk/internal/ui"
)

// userNameStore is a workspace's user ID -> display name cache on the
// engine side (cmd/slk). It replaces a plain map[string]string that was
// shared, as one map object, between the UI goroutine and every
// background path: history fetchers running as bubbletea Cmds, the
// WebSocket event loop, and the unresolved-DM sweep.
//
// That sharing was a "fatal error: concurrent map read and map write"
// waiting to happen, and on 2026-10-02 it happened: fetchThreadReplies
// memoized a cached name into the map from its Cmd goroutine while
// the Threads view rendered mentions from the same map. Reproduced in
// one process by TestFetchThreadReplies_RacesThreadsViewRender.
//
// The rule now:
//   - Engine code reads and writes names only through this store, from
//     any goroutine.
//   - The UI never sees the store. It is handed a copy from SnapshotForUI
//     in WorkspaceReadyMsg / WorkspaceSwitchedMsg, which is the UI's from
//     then on (it writes it). Once NotifyFrom has been called with the
//     version SnapshotForUI returned alongside it (never the map itself),
//     every Set that adds or changes a name is reported to the notifier,
//     which sends it to the UI as UserResolvedMsg. So callers just Set;
//     reaching the UI is the store's job, not something each caller has
//     to remember.
//
// A nil *userNameStore reads as empty and drops writes, so helpers whose
// name source is optional (tests, cache-only renders) can pass nil.
type userNameStore struct {
	mu    sync.RWMutex
	names map[string]string
	// seq counts the Sets that added or changed a name; changed[id] is
	// the seq of id's latest such Set. That is how NotifyFrom finds the
	// names learned since a snapshot without a map to compare against:
	// the snapshot map belongs to the UI, which writes it, so reading it
	// here would be exactly the race this type exists to prevent.
	seq     uint64
	changed map[string]uint64
	// notify, once set by NotifyFrom, is told of every Set that adds or
	// changes a name. Called outside mu.
	notify func(userID, name string)
}

// newUserNameStore returns a store seeded with a copy of seed (which
// may be nil).
func newUserNameStore(seed map[string]string) *userNameStore {
	names := make(map[string]string, len(seed))
	for id, name := range seed {
		names[id] = name
	}
	return &userNameStore{names: names, changed: map[string]uint64{}}
}

// Get returns the display name recorded for userID.
func (s *userNameStore) Get(userID string) (string, bool) {
	if s == nil {
		return "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	name, ok := s.names[userID]
	return name, ok
}

// Set records a display name for userID and, if it is new or changed
// and a notifier is installed, reports it.
func (s *userNameStore) Set(userID, name string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	old, had := s.names[userID]
	if had && old == name {
		s.mu.Unlock()
		return
	}
	s.names[userID] = name
	s.seq++
	s.changed[userID] = s.seq
	notify := s.notify
	s.mu.Unlock()
	if notify != nil {
		notify(userID, name)
	}
}

// NotifyFrom installs notify and immediately reports every name added
// or changed after since, the version SnapshotForUI returned with the
// map the UI was handed. That closes the gap between taking the
// snapshot and the UI applying it: a name Set in between is in neither
// the snapshot nor (if the UI drops messages for a workspace it has not
// switched to yet) a message, so it is reported here instead.
//
// Installing the notifier and finding the gap happen under one lock, so
// a concurrent Set is either already in the gap or sees the notifier
// and reports itself. Call it once the UI has applied the snapshot;
// calling it again (each workspace switch) just repeats the catch-up.
func (s *userNameStore) NotifyFrom(since uint64, notify func(userID, name string)) {
	if s == nil {
		return
	}
	type entry struct{ id, name string }
	s.mu.Lock()
	s.notify = notify
	var gap []entry
	for id, at := range s.changed {
		if at > since {
			gap = append(gap, entry{id, s.names[id]})
		}
	}
	s.mu.Unlock()
	for _, e := range gap {
		notify(e.id, e.name)
	}
}

// uiNameNotifier is the notifier main.go installs: it reports a name to
// the UI as UserResolvedMsg. Two properties matter:
//
//   - The send is asynchronous. bubbletea's Program.Send blocks until
//     the Update loop takes the message, and Set is called from inside
//     that loop (ReadCache / CacheRead run synchronously in reducers,
//     and record SQLite hits). A synchronous send from there would wait
//     on itself forever.
//   - The TeamID is the store's workspace. A message for a workspace
//     that is not active is dropped by the reducer; that is fine,
//     because NotifyFrom re-reports the gap when the workspace is
//     switched to.
func uiNameNotifier(teamID string, send func(tea.Msg)) func(userID, name string) {
	return func(userID, name string) {
		go send(ui.UserResolvedMsg{TeamID: teamID, UserID: userID, DisplayName: name})
	}
}

// Snapshot returns an independent copy of every recorded name. The
// caller owns the result; nothing else holds a reference to it.
func (s *userNameStore) Snapshot() map[string]string {
	m, _ := s.SnapshotForUI()
	return m
}

// SnapshotForUI is Snapshot plus the store's version at that moment,
// which is what NotifyFrom needs to report exactly the names learned
// after the snapshot. The map goes to the UI and is the UI's from then
// on (it writes it); keep only the version.
func (s *userNameStore) SnapshotForUI() (map[string]string, uint64) {
	if s == nil {
		return map[string]string{}, 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.names))
	for id, name := range s.names {
		out[id] = name
	}
	return out, s.seq
}

// MentionedNames returns the recorded names of just the users mentioned
// in text, as a fresh map the caller owns. For one-off rendering (a
// desktop notification) where copying the whole store per call would
// be wasteful.
func (s *userNameStore) MentionedNames(text string) map[string]string {
	out := map[string]string{}
	for _, id := range slackfmt.MentionedUserIDs(text) {
		if name, ok := s.Get(id); ok {
			out[id] = name
		}
	}
	return out
}
