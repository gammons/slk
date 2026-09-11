package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/slack/edge"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/slack-go/slack"
)

// TestUserResolver_BoundsConcurrentRequests is the second half of the
// cold-cache fix.
//
// Removing the membership fan-out stops the 40,000-request burst at its
// source, but Request is still reachable from render paths, the
// unresolved-DM sweep and inbound messages, and it used to spawn one
// goroutine per call with nothing between it and the transport. On a
// cold cache that is a burst waiting for a big enough trigger. The
// count of requests is a product question; the rate they leave at is
// not, and a client that opens hundreds of connections at once looks
// like nothing a person is driving.
func TestUserResolver_BoundsConcurrentRequests(t *testing.T) {
	const requests = 60

	var inFlight, maxInFlight int32

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt32(&inFlight, 1)
		for {
			hi := atomic.LoadInt32(&maxInFlight)
			if cur <= hi || atomic.CompareAndSwapInt32(&maxInFlight, hi, cur) {
				break
			}
		}
		// A duration is semantically required here and no signal can
		// replace it. The property under test is a *safety* property —
		// the pool never has more than userResolverConcurrency round
		// trips open — and the only way to observe a violation is to
		// give an unbounded implementation room to pile up. This is a
		// stimulus, not a deadline: nothing is asserted against it, and
		// a loaded machine makes an unbounded implementation *more*
		// visible, never less, so it cannot be starved into a false
		// failure.
		time.Sleep(25 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		w.Header().Set("Content-Type", "application/json")
		// image_32 is deliberately absent: avatar.Cache.Preload
		// returns before touching its receiver when the URL is empty,
		// which is what lets this test pass a nil avatar cache.
		_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"U1","name":"someone","team_id":"T1","profile":{"display_name":"Someone"}}}`))
	}))
	defer srv.Close()

	db := newTestDB(t)
	watch := newResolvedWatch(requests, nil)
	r := newUserResolver("T1", newTestClient(t, srv), db, nil, watch.send, nil, nil)

	for i := 0; i < requests; i++ {
		r.Request(fmt.Sprintf("U%03d", i))
	}

	// No timeout by design: `go test` already imposes one, and a
	// wall-clock budget inside the test is exactly what made this
	// load-sensitive. A hang here produces a goroutine dump naming the
	// stuck test, which beats "expected 60 requests, got 57" — and a
	// pool that drops work rather than bounding it hangs here, which is
	// the failure the old 10s deadline poll was trying to catch.
	//
	// This is also the barrier for the cache writes: resolveOne upserts
	// the row and only then sends UserResolvedMsg, so 60 messages means
	// 60 goroutines are done touching the DB. Without it they outlive
	// the test and race t.TempDir's RemoveAll.
	<-watch.done // every users.info round trip resolved and was cached

	if got := atomic.LoadInt32(&maxInFlight); got > userResolverConcurrency {
		t.Errorf("peak concurrent users.info requests = %d; want at most %d — one goroutine per unresolved user is how a cold cache produced a 40,000-request burst", got, userResolverConcurrency)
	}
}

// TestUserResolver_RequestDoesNotBlockTheCaller pins the property the
// pool must not cost us: Request is called from render and event paths
// that cannot wait on the network.
func TestUserResolver_RequestDoesNotBlockTheCaller(t *testing.T) {
	// Enough to fill the pool several times over. Every one of these
	// must return even though nothing on the wire can complete.
	const requests = userResolverConcurrency * 4

	// release gates every handler. Nothing may finish until the test
	// closes it, so a Request that has returned provably did not wait
	// on a round trip. That is a happens-before relation, which is what
	// this test actually claims — the previous form asserted the same
	// thing inside a 2s budget, which load can starve.
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"U1","name":"n","team_id":"T1","profile":{"display_name":"N"}}}`))
	}))
	defer srv.Close()

	db := newTestDB(t)
	watch := newResolvedWatch(requests, nil)
	r := newUserResolver("T1", newTestClient(t, srv), db, nil, watch.send, nil, nil)

	returned := make(chan struct{})
	go func() {
		for i := 0; i < requests; i++ {
			r.Request(fmt.Sprintf("U%03d", i))
		}
		close(returned)
	}()

	// No timeout by design: `go test` already imposes one, and a
	// wall-clock budget inside the test is exactly what made this
	// load-sensitive. A hang here produces a goroutine dump naming the
	// stuck test — and it names the caller parked in RoundTrip, which
	// is a far better diagnosis of "Request blocked its caller" than a
	// 2s deadline that also fires on a busy machine. Request is called
	// from the render path and from WS event handlers, neither of which
	// may wait on a users.info round trip.
	<-returned // every Request returned while the transport is still blocked

	// Drain. Unblocking the handlers lets the resolveOne goroutines
	// finish their cache writes; without this they outlive the test
	// body and race t.TempDir's RemoveAll, which is the flake this file
	// actually reproduced ("directory not empty", ~1 run in 5).
	close(release)
	<-watch.done // every resolveOne finished writing its cache row
}

// resolvedWatch is a userResolver send callback that records every
// message and closes done once want of them are UserResolvedMsgs
// satisfying match (nil matches any).
//
// It is the barrier tests wait on instead of the clock. Both resolution
// paths write the cache row *before* sending UserResolvedMsg —
// resolveOne upserts then sends, applyEdgeUser upserts then sends — so
// a matching message proves the write landed. Waiting on
// fakeBatcher.calls() does not: fakeBatcher records the batch on entry
// and returns, while the resolver applies the records afterwards, so a
// call count races everything applyEdgeUser writes.
//
// It is also what keeps background goroutines from outliving the test
// body and racing t.TempDir's RemoveAll.
type resolvedWatch struct {
	mu    sync.Mutex
	sent  []tea.Msg
	match func(ui.UserResolvedMsg) bool
	want  int
	n     int
	done  chan struct{}
}

func newResolvedWatch(want int, match func(ui.UserResolvedMsg) bool) *resolvedWatch {
	return &resolvedWatch{match: match, want: want, done: make(chan struct{})}
}

func (w *resolvedWatch) send(m tea.Msg) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sent = append(w.sent, m)
	u, ok := m.(ui.UserResolvedMsg)
	if !ok || (w.match != nil && !w.match(u)) {
		return
	}
	w.n++
	if w.n == w.want {
		close(w.done)
	}
}

// messages returns every message sent so far. Call it only after
// receiving from done, which is the ordering barrier.
func (w *resolvedWatch) messages() []tea.Msg {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]tea.Msg(nil), w.sent...)
}

// fakeBatcher implements userBatcher and records each batch it was
// asked for.
type fakeBatcher struct {
	mu   sync.Mutex
	sent []map[string]int64
	res  []edge.User
	err  error
}

func (f *fakeBatcher) UsersInfo(_ context.Context, updatedIDs map[string]int64) ([]edge.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make(map[string]int64, len(updatedIDs))
	for k, v := range updatedIDs {
		cp[k] = v
	}
	f.sent = append(f.sent, cp)
	if f.err != nil {
		return nil, f.err
	}
	return f.res, nil
}

func (f *fakeBatcher) calls() []map[string]int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]int64(nil), f.sent...)
}

// edgeUserRecord builds an edge.User, whose Profile is an anonymous
// struct with no spellable type at a call site.
func edgeUserRecord(id, name, display, real, teamID string, version int64, isBot bool) edge.User {
	u := edge.User{ID: id, Name: name, Version: version, IsBot: isBot, TeamID: teamID}
	u.Profile.DisplayName = display
	u.Profile.RealName = real
	return u
}

func TestUserResolver_BatchesMissesThroughEdge(t *testing.T) {
	// Measured on the first working Grid session: 282 users.info
	// calls on a cold boot. Task 5's A/B later refined the
	// attribution: the Request queue coalesces the render/WS-path
	// misses this test pins, while the unresolved-DM sweep was the
	// dominant cold-boot source (batched separately, through
	// ResolveNow). edge users/info takes 80 ids a request and
	// returns full records inline — the same misses are ~4 requests.
	db := newTestDB(t)
	batcher := &fakeBatcher{res: []edge.User{
		edgeUserRecord("U001", "alice", "Alice", "", "T1", 1783337599010, false),
		edgeUserRecord("U002", "bob", "", "Bob Real", "T1", 1783337599011, false),
	}}
	watch := newResolvedWatch(2, nil)
	r := newUserResolver("T1", nil, db, nil, watch.send, batcher, nil)

	r.Request("U001")
	r.Request("U002")

	// Wait for the cache rows, not for the batch call.
	//
	// fakeBatcher records the call and returns; the resolver only then
	// loops applyEdgeUser, which is what writes the row
	// (UpsertUserFromEdge). Waiting on batcher.calls() therefore
	// returns *before* the state asserted below exists, and the
	// GetUser calls race the write — an intermittent "U001 was not
	// cached from the edge batch: sql: no rows in result set".
	// applyEdgeUser writes the row and *then* sends UserResolvedMsg,
	// per user, so waiting for both messages implies every write has
	// landed.
	//
	// No timeout by design: `go test` already imposes one, and a
	// wall-clock budget inside the test is exactly what made this
	// load-sensitive. A hang here produces a goroutine dump naming the
	// stuck test, which beats "expected 2 calls, got 1".
	<-watch.done // both edge users/info records applied to the cache

	calls := batcher.calls()
	if len(calls) != 1 {
		t.Fatalf("edge batches = %d; want 1 — misses inside the window coalesce (sent: %v)", len(calls), calls)
	}
	want := map[string]int64{"U001": 0, "U002": 0}
	if !reflect.DeepEqual(calls[0], want) {
		t.Errorf("batch = %v; want %v — 0 is the protocol's 'never seen, send the full record'", calls[0], want)
	}

	// Cached, so a second Request is a no-op.
	vers, err := db.UserVersions("T1")
	if err != nil {
		t.Fatalf("UserVersions: %v", err)
	}
	for _, id := range []string{"U001", "U002"} {
		if _, err := db.GetUser(id); err != nil {
			t.Fatalf("%s was not cached from the edge batch: %v", id, err)
		}
		if vers[id] == 0 {
			t.Errorf("%s cached with version 0; the batch returned one and conditional revalidation reads it", id)
		}
	}
	// Display-name chain: display when set, real otherwise.
	u1, _ := db.GetUser("U001")
	if u1.DisplayName != "Alice" {
		t.Errorf("U001 display = %q; want Alice", u1.DisplayName)
	}
	u2, _ := db.GetUser("U002")
	if u2.DisplayName != "Bob Real" {
		t.Errorf("U002 display = %q; want the real-name fallback", u2.DisplayName)
	}
	// One UserResolvedMsg per resolved user, same as the per-user path.
	resolved := 0
	for _, m := range watch.messages() {
		if _, ok := m.(ui.UserResolvedMsg); ok {
			resolved++
		}
	}
	if resolved != 2 {
		t.Errorf("UserResolvedMsg count = %d; want 2 — the UI patches display names live from these", resolved)
	}
	// Dedup end-to-end: a repeat Request resolves nothing further.
	//
	// Request's cache check is synchronous — GetUser hits, inflight is
	// released, and the call returns without queueing — so the absence
	// of a queued id is observable the moment Request returns. Asserting
	// on the queue rather than sleeping out the batch window is both
	// duration-free and strictly stronger: a Request that lost its cache
	// check would leave a pending entry and an armed flush timer here,
	// which no amount of waiting for a batch that has not been sent yet
	// can distinguish from "nothing queued".
	r.Request("U001")
	r.pendingMu.Lock()
	queued, armed := len(r.pending), r.flushTimer != nil
	r.pendingMu.Unlock()
	if queued != 0 || armed {
		t.Errorf("a repeat Request queued %d id(s) (flush armed: %v); the cache check must make it a no-op", queued, armed)
	}
	if n := len(batcher.calls()); n != 1 {
		t.Errorf("a repeat Request produced batch %d; the cache check must make it a no-op", n)
	}
}

func TestUserResolver_EdgeMissFallsBackToPerUser(t *testing.T) {
	// ids edge does not return are resolved the old way — absence
	// from the batch means "could not resolve", and a raw user id on
	// screen is the failure this whole path exists to avoid.
	db := newTestDB(t)
	batcher := &fakeBatcher{res: []edge.User{
		edgeUserRecord("U001", "alice", "Alice", "", "T1", 1, false),
	}}
	var profiles atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		profiles.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"U002","name":"bob","team_id":"T1","profile":{"display_name":"Bob"}}}`))
	}))
	defer srv.Close()

	watch := newResolvedWatch(1, func(m ui.UserResolvedMsg) bool { return m.UserID == "U002" })
	r := newUserResolver("T1", newTestClient(t, srv), db, nil, watch.send, batcher, nil)
	r.Request("U001")
	r.Request("U002")

	// No timeout by design: `go test` already imposes one, and a
	// wall-clock budget inside the test is exactly what made this
	// load-sensitive. A hang here produces a goroutine dump naming the
	// stuck test, which beats "U002 was never resolved per-user".
	<-watch.done // U002 resolved through the per-user users.info fallback

	if _, err := db.GetUser("U002"); err != nil {
		t.Fatal("U002 was absent from the edge batch and was never resolved per-user")
	}
	if got := profiles.Load(); got != 1 {
		t.Errorf("per-user users.info calls = %d; want 1 — only the id edge missed", got)
	}
}

func TestUserResolver_EmptyNameEdgeRecordFallsBackToPerUser(t *testing.T) {
	// Unobserved in captures, but symmetric to the sweep's guard: an
	// edge record with all three name fields empty must not be
	// cached (its empty DisplayName would satisfy Request's
	// cache-skip gate permanently) nor emitted (an empty
	// UserResolvedMsg would blank a rendered in-history name).
	db := newTestDB(t)
	batcher := &fakeBatcher{res: []edge.User{
		edgeUserRecord("U001", "", "", "", "T1", 1, false),
		edgeUserRecord("U002", "bob", "Bob", "", "T1", 1, false),
	}}
	var profiles atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		profiles.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"U001","name":"alice","team_id":"T1","profile":{"display_name":"Alice"}}}`))
	}))
	defer srv.Close()

	watch := newResolvedWatch(1, func(m ui.UserResolvedMsg) bool {
		return m.UserID == "U001" && m.DisplayName == "Alice"
	})
	r := newUserResolver("T1", newTestClient(t, srv), db, nil, watch.send, batcher, nil)
	r.Request("U001")
	r.Request("U002")

	// No timeout by design: `go test` already imposes one, and a
	// wall-clock budget inside the test is exactly what made this
	// load-sensitive. A hang here produces a goroutine dump naming the
	// stuck test, which beats an empty-vs-Alice diff on a loaded box.
	// U002's good record is applied inside flush, before the fallback
	// goroutine is even spawned, so this also orders the U002 asserts.
	<-watch.done // the per-user fallback resolved U001 as Alice

	u1, err := db.GetUser("U001")
	if err != nil || u1.DisplayName != "Alice" {
		t.Fatalf("U001 cached as %+v (err=%v); want the per-user fallback's Alice — the empty edge record must be treated as a miss", u1, err)
	}
	if got := profiles.Load(); got != 1 {
		t.Errorf("per-user users.info calls = %d; want 1 — only the empty-name id", got)
	}
	if u2, err := db.GetUser("U002"); err != nil || u2.DisplayName != "Bob" {
		t.Errorf("U002 cached as %+v (err=%v); the good record in the same batch must still apply", u2, err)
	}
	for _, m := range watch.messages() {
		if msg, ok := m.(ui.UserResolvedMsg); ok && msg.DisplayName == "" {
			t.Errorf("an empty-name UserResolvedMsg was sent for %s; that blanks a rendered in-history name", msg.UserID)
		}
	}
}

func TestUserResolver_EdgeErrorFallsBackToPerUser(t *testing.T) {
	db := newTestDB(t)
	batcher := &fakeBatcher{err: errors.New("ratelimited")}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"U001","name":"alice","team_id":"T1","profile":{"display_name":"Alice"}}}`))
	}))
	defer srv.Close()

	watch := newResolvedWatch(1, func(m ui.UserResolvedMsg) bool { return m.UserID == "U001" })
	r := newUserResolver("T1", newTestClient(t, srv), db, nil, watch.send, batcher, nil)
	r.Request("U001")

	// No timeout by design: `go test` already imposes one, and a
	// wall-clock budget inside the test is exactly what made this
	// load-sensitive. If the fallback never runs this hangs, and the
	// goroutine dump names the stuck test — strictly more useful than
	// "the per-user fallback never ran" five seconds after the fact.
	<-watch.done // the per-user fallback resolved U001 after the edge error

	if _, err := db.GetUser("U001"); err != nil {
		t.Fatal("the edge call failed and the per-user fallback never ran")
	}
}

func TestUserResolver_DegradedWorkspaceSkipsEdge(t *testing.T) {
	// Once boot has marked a workspace's edge broken, the resolver
	// must not spend even one call discovering it again.
	db := newTestDB(t)
	batcher := &fakeBatcher{res: []edge.User{
		edgeUserRecord("U001", "alice", "Alice", "", "T1", 1, false),
	}}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"U001","name":"alice","team_id":"T1","profile":{"display_name":"Alice"}}}`))
	}))
	defer srv.Close()

	watch := newResolvedWatch(1, nil)
	r := newUserResolver("T1", newTestClient(t, srv), db, nil, watch.send, batcher, func() bool { return true })
	r.Request("U001")

	// No timeout by design: `go test` already imposes one, and a
	// wall-clock budget inside the test is exactly what made this
	// load-sensitive. A hang here produces a goroutine dump naming the
	// stuck test, which beats "U001 was not resolved per-user".
	<-watch.done // U001 resolved on the degraded per-user path

	if n := len(batcher.calls()); n != 0 {
		t.Errorf("a degraded workspace made %d edge calls; want 0", n)
	}
	// Nothing can arrive late: a degraded Request goes straight to
	// resolveOne without queueing, so an empty queue with no armed
	// timer means no batch is coming. Asserting that beats sleeping out
	// the window, which could only ever say "not yet".
	r.pendingMu.Lock()
	queued, armed := len(r.pending), r.flushTimer != nil
	r.pendingMu.Unlock()
	if queued != 0 || armed {
		t.Errorf("a degraded workspace queued %d id(s) for edge (flush armed: %v); want none", queued, armed)
	}
	if _, err := db.GetUser("U001"); err != nil {
		t.Error("U001 was not resolved per-user on the degraded path")
	}
}

func TestUserResolver_ResolveNowBatchesAndApplies(t *testing.T) {
	db := newTestDB(t)
	batcher := &fakeBatcher{res: []edge.User{
		edgeUserRecord("U001", "alice", "Alice", "", "T1", 1783337599010, false),
		edgeUserRecord("U002", "bob", "", "Bob Real", "T1", 1783337599011, false),
	}}
	r := newUserResolver("T1", nil, db, nil, nil, batcher, nil)

	got := r.ResolveNow([]string{"U001", "U002"})

	if len(got) != 2 {
		t.Fatalf("ResolveNow returned %d records; want 2", len(got))
	}
	calls := batcher.calls()
	if len(calls) != 1 {
		t.Fatalf("edge batches = %d; want 1", len(calls))
	}
	if want := map[string]int64{"U001": 0, "U002": 0}; !reflect.DeepEqual(calls[0], want) {
		t.Errorf("batch = %v; want %v", calls[0], want)
	}
	// Applied: cache rows exist and carry versions, so the sweep's
	// callers and conditional revalidation both read them.
	for _, id := range []string{"U001", "U002"} {
		if _, err := db.GetUser(id); err != nil {
			t.Errorf("%s was not cached by ResolveNow: %v", id, err)
		}
	}
	versions, err := db.UserVersions("T1")
	if err != nil {
		t.Fatalf("UserVersions: %v", err)
	}
	if versions["U001"] != 1783337599010 {
		t.Errorf("U001 version = %d; want 1783337599010", versions["U001"])
	}
}

func TestUserResolver_ResolveNowReturnsNilWhenDegraded(t *testing.T) {
	db := newTestDB(t)
	batcher := &fakeBatcher{}
	r := newUserResolver("T1", nil, db, nil, nil, batcher, func() bool { return true })

	if got := r.ResolveNow([]string{"U001"}); got != nil {
		t.Errorf("a degraded workspace resolved %v through edge; want nil so the caller falls back per-user", got)
	}
	if n := len(batcher.calls()); n != 0 {
		t.Errorf("a degraded workspace made %d edge calls; want 0", n)
	}
}

func TestUserResolver_ResolveNowReturnsNilOnError(t *testing.T) {
	db := newTestDB(t)
	batcher := &fakeBatcher{err: errors.New("ratelimited")}
	r := newUserResolver("T1", nil, db, nil, nil, batcher, nil)

	if got := r.ResolveNow([]string{"U001"}); got != nil {
		t.Errorf("a failed edge call returned %v; want nil", got)
	}
	if _, err := db.GetUser("U001"); err == nil {
		t.Error("U001 was cached from a response the resolver rejected")
	}
}

func TestUserResolver_ResolveNowSkipsEmptyInput(t *testing.T) {
	db := newTestDB(t)
	batcher := &fakeBatcher{}
	r := newUserResolver("T1", nil, db, nil, nil, batcher, nil)

	if got := r.ResolveNow(nil); got != nil {
		t.Errorf("ResolveNow(nil) = %v; want nil", got)
	}
	if got := r.ResolveNow([]string{""}); got != nil {
		t.Errorf("ResolveNow([\"\"]) = %v; want nil — an updated_ids map containing \"\" is a request shape nothing observed produces", got)
	}
	if n := len(batcher.calls()); n != 0 {
		t.Errorf("empty input produced %d edge calls; want 0", n)
	}
}

func TestResolveDMNames(t *testing.T) {
	// The sweep maps resolutions to CHANNEL ids: DMNameResolvedMsg
	// renames the sidebar row and re-buckets app DMs, which
	// UserResolvedMsg (history patching) cannot do.
	db := newTestDB(t)
	batcher := &fakeBatcher{res: []edge.User{
		edgeUserRecord("U_ALICE", "alice", "Alice A", "", "T1", 1, false),
		edgeUserRecord("U_APP", "someapp", "Some App", "", "T1", 1, true),
	}}
	wctx := &WorkspaceContext{
		TeamID:            "T1",
		UserNames:         map[string]string{},
		UserNamesByHandle: map[string]string{},
		BotUserIDs:        map[string]bool{},
		UserResolver:      newUserResolver("T1", nil, db, nil, nil, batcher, nil),
		UnresolvedDMs: []UnresolvedDM{
			{ChannelID: "D_ALICE", UserID: "U_ALICE"},
			{ChannelID: "D_APP", UserID: "U_APP"},
		},
		Channels: []sidebar.ChannelItem{
			{ID: "D_ALICE", Name: "U_ALICE", Type: "dm", DMUserID: "U_ALICE"},
			{ID: "D_APP", Name: "U_APP", Type: "dm", DMUserID: "U_APP"},
		},
	}
	var mu sync.Mutex
	var sent []tea.Msg
	resolveDMNames(wctx, db, nil, func(m tea.Msg) {
		mu.Lock()
		sent = append(sent, m)
		mu.Unlock()
	})

	mu.Lock()
	defer mu.Unlock()
	var alice, app *ui.DMNameResolvedMsg
	for _, m := range sent {
		if dm, ok := m.(ui.DMNameResolvedMsg); ok {
			switch dm.ChannelID {
			case "D_ALICE":
				d := dm
				alice = &d
			case "D_APP":
				d := dm
				app = &d
			}
		}
	}
	if alice == nil || alice.DisplayName != "Alice A" {
		t.Errorf("D_ALICE got %+v; want a DMNameResolvedMsg naming Alice A", alice)
	}
	if alice != nil && alice.TeamID != "T1" {
		t.Errorf("D_ALICE TeamID = %q; want T1 so the UI ignores other workspaces", alice.TeamID)
	}
	if app == nil || app.DisplayName != "Some App" || !app.IsBot {
		t.Errorf("D_APP got %+v; want a DMNameResolvedMsg naming Some App with IsBot true — that flag re-buckets the row into the Apps section", app)
	}
	if !wctx.BotUserIDs["U_APP"] {
		t.Error("U_APP was not recorded in BotUserIDs")
	}
	if n := len(batcher.calls()); n != 1 {
		t.Errorf("the sweep made %d edge calls; want 1 for any number of DMs", n)
	}
	byID := map[string]sidebar.ChannelItem{}
	for _, ch := range wctx.Channels {
		byID[ch.ID] = ch
	}
	if got := byID["D_ALICE"]; got.Name != "Alice A" {
		t.Errorf("wctx.Channels D_ALICE Name = %q; want Alice A so a workspace switch still shows the resolved name", got.Name)
	}
	if got := byID["D_APP"]; got.Name != "Some App" || got.Type != "app" {
		t.Errorf("wctx.Channels D_APP = {Name:%q Type:%q}; want Some App / app", got.Name, got.Type)
	}
}

func TestSeedDMDisplayNames_FillsUserNamesFromEdge(t *testing.T) {
	db := newTestDB(t)
	batcher := &fakeBatcher{res: []edge.User{
		edgeUserRecord("U1", "alice", "Alice A", "", "T1", 1, false),
	}}
	wctx := &WorkspaceContext{
		TeamID:            "T1",
		UserNames:         map[string]string{},
		UserNamesByHandle: map[string]string{},
		BotUserIDs:        map[string]bool{},
		UserResolver:      newUserResolver("T1", nil, db, nil, nil, batcher, nil),
	}
	channels := []slack.Channel{{
		GroupConversation: slack.GroupConversation{
			Conversation: slack.Conversation{ID: "D1", IsIM: true, User: "U1"},
		},
	}}
	seedDMDisplayNames(wctx, channels)
	if got := wctx.UserNames["U1"]; got != "Alice A" {
		t.Errorf("UserNames[U1] = %q; want Alice A — sidebar DMs read this map at first paint", got)
	}
	if got := wctx.UserNamesByHandle["alice"]; got != "Alice A" {
		t.Errorf("UserNamesByHandle[alice] = %q; want Alice A", got)
	}
}
