package main

import (
	"fmt"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/ui"
)

// TestUINameNotifier_DoesNotBlockTheCaller: Set runs inside bubbletea's
// Update loop (ReadCache / CacheRead are synchronous in reducers), and
// Program.Send blocks until that same loop receives. A notifier that
// sent synchronously would freeze the UI the first time a cached name
// was recorded during a channel switch. Model that with a send that
// never returns: Set must still return.
func TestUINameNotifier_DoesNotBlockTheCaller(t *testing.T) {
	received := make(chan tea.Msg, 1)
	block := make(chan struct{})
	defer close(block)
	send := func(m tea.Msg) {
		received <- m
		<-block // an Update loop that is busy running the caller
	}

	s := newUserNameStore(nil)
	s.NotifyFrom(0, uiNameNotifier("T1", send))

	done := make(chan struct{})
	go func() {
		s.Set("U1", "Alice")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Set blocked on the UI send: this deadlocks when Set runs on the Update goroutine")
	}

	select {
	case m := <-received:
		got, ok := m.(ui.UserResolvedMsg)
		if !ok || got.TeamID != "T1" || got.UserID != "U1" || got.DisplayName != "Alice" {
			t.Errorf("sent %#v; want UserResolvedMsg{TeamID: T1, UserID: U1, DisplayName: Alice}", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no UserResolvedMsg was sent")
	}
}

// TestUserNameStore_SnapshotIsIndependent pins the property the
// 2026-10-02 crash fix rests on: the map handed to the UI is a copy,
// not the store's backing map. If Snapshot ever returns the live map
// again, background writes become UI-goroutine races again.
func TestUserNameStore_SnapshotIsIndependent(t *testing.T) {
	s := newUserNameStore(map[string]string{"U1": "Alice"})

	snap := s.Snapshot()
	s.Set("U2", "Bob")
	if _, ok := snap["U2"]; ok {
		t.Fatal("a Set after Snapshot leaked into the snapshot: the UI and the store share a map")
	}

	snap["U3"] = "Carol"
	if _, ok := s.Get("U3"); ok {
		t.Fatal("a write to the snapshot leaked into the store: the UI and the store share a map")
	}

	if name, ok := s.Get("U1"); !ok || name != "Alice" {
		t.Errorf("Get(U1) = (%q, %v), want (\"Alice\", true)", name, ok)
	}
}

// TestUserNameStore_ConcurrentAccess runs Set, Get and Snapshot from
// many goroutines at once. Under -race a store without its lock fails
// here; without -race Go's map checks usually do.
func TestUserNameStore_ConcurrentAccess(t *testing.T) {
	s := newUserNameStore(nil)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				id := fmt.Sprintf("U%d_%d", g, i)
				s.Set(id, "name")
				_, _ = s.Get(id)
				_ = s.Snapshot()
			}
		}(g)
	}
	wg.Wait()
	if snap := s.Snapshot(); len(snap) != 8*200 {
		t.Errorf("len(Snapshot()) = %d, want %d", len(snap), 8*200)
	}
}

// TestUserNameStore_NilIsEmpty: several history helpers document that
// their name source may be absent. A nil store reads as empty and
// ignores writes instead of panicking.
func TestUserNameStore_NilIsEmpty(t *testing.T) {
	var s *userNameStore
	if _, ok := s.Get("U1"); ok {
		t.Error("nil store Get returned ok")
	}
	s.Set("U1", "Alice") // must not panic
	if snap := s.Snapshot(); snap == nil || len(snap) != 0 {
		t.Errorf("nil store Snapshot() = %v, want empty non-nil map", snap)
	}
	s.NotifyFrom(0, func(string, string) {}) // must not panic
}

// recordLearned returns a notifier that records what it was told.
func recordLearned() (func(string, string), func() map[string]string) {
	var mu sync.Mutex
	got := map[string]string{}
	return func(id, name string) {
			mu.Lock()
			got[id] = name
			mu.Unlock()
		}, func() map[string]string {
			mu.Lock()
			defer mu.Unlock()
			out := map[string]string{}
			for k, v := range got {
				out[k] = v
			}
			return out
		}
}

// TestUserNameStore_NotifyFromCoversTheHandoffGap: the UI is handed a
// Snapshot, and names learned afterwards must reach it as messages. A
// name Set after the Snapshot but before the notifier is installed
// would otherwise be in neither, and render as a raw ID forever.
func TestUserNameStore_NotifyFromCoversTheHandoffGap(t *testing.T) {
	s := newUserNameStore(map[string]string{"U1": "Alice"})
	_, since := s.SnapshotForUI()

	s.Set("U2", "Bob")      // learned in the gap: new
	s.Set("U1", "Alice B.") // learned in the gap: changed

	notify, got := recordLearned()
	s.NotifyFrom(since, notify)

	want := map[string]string{"U2": "Bob", "U1": "Alice B."}
	if g := got(); len(g) != 2 || g["U2"] != want["U2"] || g["U1"] != want["U1"] {
		t.Errorf("NotifyFrom reported %v; want exactly the gap's names %v", g, want)
	}
}

// TestUserNameStore_NotifiesNewAndChangedOnly: after NotifyFrom, every
// Set that changes what the UI would show is reported, and nothing
// else. Lookups re-Set known names constantly; reporting those would
// flood the UI loop with no-op patches.
func TestUserNameStore_NotifiesNewAndChangedOnly(t *testing.T) {
	s := newUserNameStore(map[string]string{"U1": "Alice"})
	notify, got := recordLearned()
	_, since := s.SnapshotForUI()
	s.NotifyFrom(since, notify)

	s.Set("U1", "Alice") // unchanged: silent
	if g := got(); len(g) != 0 {
		t.Fatalf("an unchanged Set notified: %v", g)
	}
	s.Set("U2", "Bob")
	s.Set("U1", "Alice B.")
	if g := got(); g["U2"] != "Bob" || g["U1"] != "Alice B." {
		t.Errorf("notified %v; want U2=Bob and U1=Alice B.", g)
	}
}

// TestUserNameStore_SeedIsNotReported: connect seeds thousands of
// cached users before the workspace is ready. The Ready snapshot carries
// them; none may also turn into per-user messages once notifying starts.
func TestUserNameStore_SeedIsNotReported(t *testing.T) {
	s := newUserNameStore(map[string]string{"U1": "Alice"})
	s.Set("U2", "Bob") // connect-time write, before any notifier
	_, since := s.SnapshotForUI()

	notify, got := recordLearned()
	s.NotifyFrom(since, notify)
	s.Set("U1", "Alice") // re-Set of a seeded name, unchanged
	if g := got(); len(g) != 0 {
		t.Errorf("seeded or unchanged names were reported: %v", g)
	}
}

// TestUserNameStore_MentionedNames: notifications resolve <@U…>
// mentions in one message. They get just those names, not a copy of a
// workspace-sized map per notification.
func TestUserNameStore_MentionedNames(t *testing.T) {
	s := newUserNameStore(map[string]string{"U1": "Alice", "U2": "Bob", "U3": "Carol"})
	got := s.MentionedNames("hi <@U1>, ping <@U3> and <@U9>")
	if len(got) != 2 || got["U1"] != "Alice" || got["U3"] != "Carol" {
		t.Errorf("MentionedNames = %v; want exactly U1=Alice, U3=Carol (U9 unknown, U2 not mentioned)", got)
	}
	if got := s.MentionedNames("no mentions"); len(got) != 0 {
		t.Errorf("MentionedNames(no mentions) = %v; want empty", got)
	}
	var nilStore *userNameStore
	if got := nilStore.MentionedNames("<@U1>"); got == nil || len(got) != 0 {
		t.Errorf("nil store MentionedNames = %v; want empty non-nil map", got)
	}
}
