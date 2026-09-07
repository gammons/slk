package membership

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/gammons/slk/internal/cache"
)

// fakeMemberAPI implements ConversationMemberAPI for tests.
//
// delay is slept *after* the call is recorded and the lock released, so
// a test can hold the Manager's post-call bookkeeping (lastFailed /
// lastFetched, which backgroundFetch writes only once the API returns)
// open for a deterministic window. Tests that assert on that
// bookkeeping must therefore never treat callCount as a barrier for it
// — see captureSink.pushed and awaitFailedFetchDone.
//
// The delay is a stimulus, not a deadline: nothing in this file is
// asserted against it, so a loaded machine can only widen the window it
// opens and make a false barrier more likely to be caught, never less.
type fakeMemberAPI struct {
	mu     sync.Mutex
	calls  int
	result []string
	err    error
	delay  time.Duration
}

func (f *fakeMemberAPI) GetUsersInConversation(ctx context.Context, channelID string) ([]string, error) {
	f.mu.Lock()
	f.calls++
	result, err, delay := f.result, f.err, f.delay
	f.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	return result, err
}

func (f *fakeMemberAPI) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// captureSink records ChannelMembershipMsg pushes and hands each one to
// a test through pushed.
//
// PushFunc is the *last* thing backgroundFetch invokes on the success
// path (manager.go: pushSnapshot is its final statement), so a receive
// from pushed is a barrier for everything that fetch wrote —
// ReplaceChannelMembers, m.members, m.lastFetched, and the delete of
// m.lastFailed all happen before it. The fake API is not such a
// barrier: it records its call on *entry*, before the API has even
// returned, which is the defect commit 8eaeba9 had to repair.
//
// Receives carry no timeout by design: `go test` already imposes one
// (default 10m), and a wall-clock budget inside a test is exactly what
// makes it load-sensitive. A push that never arrives hangs and produces
// a goroutine dump naming the stuck test, which beats "timed out
// waiting for 2 pushes" one second after the fact.
//
// The buffer is sized well past any test's push count (the largest here
// is 6) because production pushes into it: EnsureFresh calls PushFunc
// synchronously on the caller's goroutine, so a full buffer would
// deadlock a test against itself rather than drop a signal.
type captureSink struct {
	mu     sync.Mutex
	pushes []capturedPush
	pushed chan capturedPush
}
type capturedPush struct {
	channelID string
	memberIDs []string
}

func newCaptureSink() *captureSink {
	return &captureSink{pushed: make(chan capturedPush, 16)}
}

func (s *captureSink) Push(channelID string, memberIDs []string) {
	cp := capturedPush{channelID: channelID, memberIDs: append([]string(nil), memberIDs...)}
	s.mu.Lock()
	s.pushes = append(s.pushes, cp)
	s.mu.Unlock()
	// Sent outside the lock: snapshot() must stay callable from a test
	// that has not drained yet.
	s.pushed <- cp
}
func (s *captureSink) snapshot() []capturedPush {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]capturedPush, len(s.pushes))
	copy(out, s.pushes)
	return out
}

func newManagerForTest(t *testing.T) (*Manager, *fakeMemberAPI, *captureSink, *cache.DB) {
	t.Helper()
	db, err := cache.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_ = db.UpsertWorkspace(cache.Workspace{ID: "T1", Name: "Test"})
	api := &fakeMemberAPI{}
	sink := newCaptureSink()
	mgr := New("T1", api, db, sink.Push, nil /* userResolver */)
	return mgr, api, sink, db
}

func TestEnsureFreshCacheHitNoFetch(t *testing.T) {
	mgr, api, sink, db := newManagerForTest(t)
	defer db.Close()
	// Seed cache with recent meta.
	_ = db.ReplaceChannelMembers("T1", "C1", []string{"U1", "U2"}, time.Now().Unix())

	mgr.EnsureFresh(context.Background(), "C1")
	// EnsureFresh pushes the cached snapshot synchronously on this
	// goroutine, so this receive is already satisfied by the time
	// EnsureFresh returns; it names the event the assertions depend on
	// and leaves the channel empty. A fresh cache starts no background
	// fetch at all, so no second push is coming.
	<-sink.pushed

	if api.callCount() != 0 {
		t.Errorf("fresh cache should NOT trigger fetch; got %d calls", api.callCount())
	}
	pushes := sink.snapshot()
	if len(pushes) != 1 || pushes[0].channelID != "C1" {
		t.Errorf("expected 1 push for C1; got %+v", pushes)
	}
}

func TestEnsureFreshCacheMissTriggersFetch(t *testing.T) {
	mgr, api, sink, db := newManagerForTest(t)
	defer db.Close()
	api.result = []string{"U1", "U2", "U3"}

	mgr.EnsureFresh(context.Background(), "C1")
	<-sink.pushed // EnsureFresh's own synchronous push of the empty cache
	// No timeout by design: `go test` already imposes one, and a
	// wall-clock budget inside the test is exactly what made this
	// load-sensitive. backgroundFetch pushes as its final statement,
	// after ReplaceChannelMembers and after the in-memory set is
	// swapped, so this receive is a barrier for both assertions below.
	// The API call count is a barrier for neither: it increments on
	// entry, before the call has even returned.
	fetched := <-sink.pushed

	if api.callCount() != 1 {
		t.Errorf("expected 1 fetch call; got %d", api.callCount())
	}
	if len(fetched.memberIDs) != 3 {
		t.Errorf("final push had %d members; want 3", len(fetched.memberIDs))
	}

	// Cache persisted?
	got, _ := db.ListChannelMembers("T1", "C1")
	if len(got) != 3 {
		t.Errorf("expected 3 cached members; got %d", len(got))
	}
}

func TestEnsureFreshStaleTriggersFetch(t *testing.T) {
	mgr, api, sink, db := newManagerForTest(t)
	defer db.Close()
	// Seed cache as stale (yesterday).
	stale := time.Now().Add(-25 * time.Hour).Unix()
	_ = db.ReplaceChannelMembers("T1", "C1", []string{"U1"}, stale)
	api.result = []string{"U1", "U2"}

	mgr.EnsureFresh(context.Background(), "C1")
	<-sink.pushed // EnsureFresh's own synchronous push of the stale cache
	<-sink.pushed // the background fetch's push, sent after it persisted

	if api.callCount() != 1 {
		t.Errorf("stale cache should trigger fetch; got %d calls", api.callCount())
	}
}

// awaitFailedFetchDone blocks until the goroutine EnsureFresh spawned
// has recorded a failed fetch *and* exited.
//
// A failed fetch pushes nothing — backgroundFetch writes lastFailed and
// returns (manager.go) — so there is no signal to receive from and the
// manager's own state is the only barrier available. The conjunction is
// what makes it one: the in-flight sentinel is set *before* the API
// call and released by a deferred call *after* lastFailed is written,
// so "failed and not busy" cannot be observed before the goroutine ran,
// and cannot be observed while it is still running.
//
// Waiting for the sentinel too, rather than for lastFailed alone,
// closes a latent race that predates this change: a caller that
// re-triggers a fetch while the failed one is still unwinding gets it
// dropped at the `busy` branch instead of at the branch under test.
//
// This is a condition wait, not a deadline: it returns the instant the
// state holds, has no wall-clock budget, and so a loaded machine can
// only make it spin a few more times — it cannot false-fail. If the
// fetch never completes this hangs, and `go test`'s own timeout dumps
// the stuck goroutine, the same failure mode as a blocking receive.
func awaitFailedFetchDone(mgr *Manager, channelID string) {
	for {
		mgr.mu.Lock()
		_, failed := mgr.lastFailed[channelID]
		_, busy := mgr.fetching[channelID]
		mgr.mu.Unlock()
		if failed && !busy {
			return
		}
		runtime.Gosched()
	}
}

// failureRecordedAt reports the manager's lastFailed entry for a
// channel under the manager's own lock.
func failureRecordedAt(mgr *Manager, channelID string) (time.Time, bool) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	ts, ok := mgr.lastFailed[channelID]
	return ts, ok
}

func TestApplyJoinPersistsAndPushes(t *testing.T) {
	mgr, _, sink, db := newManagerForTest(t)
	defer db.Close()
	// Pre-seed C1 with U1 so the active set isn't empty.
	_ = db.ReplaceChannelMembers("T1", "C1", []string{"U1"}, time.Now().Unix())
	mgr.EnsureFresh(context.Background(), "C1")
	<-sink.pushed // EnsureFresh's synchronous push; a fresh cache starts no fetch

	mgr.ApplyJoin("C1", "U_NEW")

	// Persisted?
	got, _ := db.ListChannelMembers("T1", "C1")
	found := false
	for _, id := range got {
		if id == "U_NEW" {
			found = true
		}
	}
	if !found {
		t.Errorf("U_NEW not persisted; cache = %v", got)
	}
	// Pushed? ApplyJoin pushes on the caller's goroutine as its last
	// statement, after the row is upserted, so this receive is already
	// satisfied.
	joined := <-sink.pushed
	hasNew := false
	for _, id := range joined.memberIDs {
		if id == "U_NEW" {
			hasNew = true
		}
	}
	if !hasNew {
		t.Errorf("U_NEW missing from push: %v", joined.memberIDs)
	}
}

func TestApplyLeaveDeletesAndPushes(t *testing.T) {
	mgr, _, sink, db := newManagerForTest(t)
	defer db.Close()
	_ = db.ReplaceChannelMembers("T1", "C1", []string{"U1", "U2"}, time.Now().Unix())
	mgr.EnsureFresh(context.Background(), "C1")
	<-sink.pushed // EnsureFresh's synchronous push; a fresh cache starts no fetch

	mgr.ApplyLeave("C1", "U1")

	got, _ := db.ListChannelMembers("T1", "C1")
	for _, id := range got {
		if id == "U1" {
			t.Errorf("U1 still in cache after leave: %v", got)
		}
	}
}

func TestApplyJoinDoesNotBumpLastFullFetchAt(t *testing.T) {
	mgr, _, _, db := newManagerForTest(t)
	defer db.Close()
	originalTS := int64(12345)
	_ = db.ReplaceChannelMembers("T1", "C1", []string{"U1"}, originalTS)

	mgr.ApplyJoin("C1", "U_NEW")

	ts, _, _ := db.GetChannelMembershipMeta("T1", "C1")
	if ts != originalTS {
		t.Errorf("last_full_fetch_at = %d, want %d (deltas must not touch it)", ts, originalTS)
	}
}

func TestForceStaleDoesNotWipePersistedMembers(t *testing.T) {
	mgr, _, _, db := newManagerForTest(t)
	defer db.Close()

	// Seed cache without ever calling loadIntoMemory (cold in-memory).
	_ = db.ReplaceChannelMembers("T1", "C1", []string{"U1", "U2", "U3"}, time.Now().Unix())

	mgr.ForceStale("C1")

	got, _ := db.ListChannelMembers("T1", "C1")
	if len(got) != 3 {
		t.Errorf("members wiped: %v (cold-cache regression)", got)
	}
	ts, _, _ := db.GetChannelMembershipMeta("T1", "C1")
	if ts != 0 {
		t.Errorf("meta = %d, want 0", ts)
	}
}

func TestForceStaleCausesRefetch(t *testing.T) {
	mgr, api, sink, db := newManagerForTest(t)
	defer db.Close()
	// Seed as fresh.
	_ = db.ReplaceChannelMembers("T1", "C1", []string{"U1"}, time.Now().Unix())
	api.result = []string{"U1", "U2"}

	mgr.ForceStale("C1")
	mgr.EnsureFresh(context.Background(), "C1")

	<-sink.pushed              // EnsureFresh's synchronous push of the seeded set
	refetched := <-sink.pushed // the forced re-fetch's push, sent after it persisted

	// The receives above are the test: a ForceStale that did not
	// invalidate would produce no second push and hang here rather than
	// pass. These assertions name what arrived, so a re-fetch that
	// fired twice or pushed the pre-ForceStale set is reported instead
	// of being silently accepted.
	if c := api.callCount(); c != 1 {
		t.Errorf("ForceStale + EnsureFresh made %d fetch calls; want exactly 1", c)
	}
	if len(refetched.memberIDs) != 2 {
		t.Errorf("re-fetch pushed %d members; want 2", len(refetched.memberIDs))
	}
}

// fakeResolver records calls for the resolver-invocation test.
type fakeResolver struct {
	mu   sync.Mutex
	seen []string
}

func (r *fakeResolver) Request(userID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, userID)
}
func (r *fakeResolver) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.seen))
	copy(out, r.seen)
	return out
}

// TestBackgroundFetchDoesNotResolveEveryMember replaces a test that
// asserted the opposite.
//
// The old TestBackgroundFetchTriggersResolverForEachID pinned a
// Request call per member, and that was measured doing real damage: on
// a cold cache a 35-second boot started 40,523 users.info requests,
// one per distinct row in channel_members (40,527 of them). The
// resolver short-circuits on a cache hit, so the users.list sweep used
// to hide this by filling the cache first; deleting that sweep in Task
// 8 exposed it.
//
// It is also work the official client never does. Counted across all 8
// captures: /api/users.info 0, /api/conversations.members 0. It asks
// edge:users/list for one channel with count:30 and present_first:true
// and gets full user records inline, with no resolution step at all.
//
// Names for members slk has not met now come from the cache, from the
// boot response, and from on-demand resolution when a row is actually
// rendered. The member ID list itself is still fetched: it is one
// bounded call per channel and it is what the mention picker's
// in-channel ordering reads.
func TestBackgroundFetchDoesNotResolveEveryMember(t *testing.T) {
	db, _ := cache.New(":memory:")
	defer db.Close()
	_ = db.UpsertWorkspace(cache.Workspace{ID: "T1", Name: "Test"})

	// A channel big enough that a per-member fan-out is unmistakable.
	ids := make([]string, 500)
	for i := range ids {
		ids[i] = fmt.Sprintf("U%03d", i)
	}
	api := &fakeMemberAPI{result: ids}
	sink := newCaptureSink()
	resolver := &fakeResolver{}
	mgr := New("T1", api, db, sink.Push, resolver)

	mgr.EnsureFresh(context.Background(), "C1")
	<-sink.pushed // EnsureFresh's own synchronous push of the empty cache
	// The deleted fan-out sat between the API returning and
	// ReplaceChannelMembers (manager.go), so it is strictly *before*
	// this push. Receiving it therefore proves the fetch has already
	// run past the point where it used to call resolver.Request once
	// per member — an exact check, where the 100ms sleep it replaces
	// was a bet on how long a 500-member fan-out takes.
	<-sink.pushed

	if seen := resolver.snapshot(); len(seen) != 0 {
		t.Errorf("membership fetch resolved %d of %d members; want 0 — one request per member is what put 40,523 users.info calls into a cold-cache boot", len(seen), len(ids))
	}

	// The membership itself must still land: this deletes the
	// resolution, not the member list.
	members, err := db.ListChannelMembers("T1", "C1")
	if err != nil {
		t.Fatalf("ListChannelMembers: %v", err)
	}
	if len(members) != len(ids) {
		t.Errorf("cached %d members; want %d — the id list is still needed for in-channel ordering", len(members), len(ids))
	}
}

// TestBackgroundFetchFailureSuppressesImmediateRefetch pins the
// failure side of the fetch ledger. A failed conversations.members
// never bumps last_full_fetch_at, so without a failure record every
// EnsureFresh — and OnConnect's ForceStale+EnsureFresh pair fires on
// every websocket reconnect — re-issues the call. Measured live: the
// active channel was a DM the workspace's token could not see
// (channel_not_found), the socket flapped, and a 25-second session
// started 42 conversations.members requests, the exact amplification
// shape this package's TTL exists to prevent.
func TestBackgroundFetchFailureSuppressesImmediateRefetch(t *testing.T) {
	mgr, api, sink, db := newManagerForTest(t)
	defer db.Close()
	api.err = fmt.Errorf("channel_not_found")

	mgr.EnsureFresh(context.Background(), "C1")
	<-sink.pushed // EnsureFresh's own synchronous push of the empty cache

	// Wait for the failure to be *recorded* and the fetch to be done,
	// not merely for the API to be entered: it is the lastFailed record
	// that suppresses the retry below, and an unfinished fetch would
	// suppress it at the in-flight branch instead — a green test for
	// the wrong reason.
	awaitFailedFetchDone(mgr, "C1")

	// A reconnect force-stales the channel and asks again. The fetch
	// must not re-fire within the failure backoff window.
	mgr.ForceStale("C1")
	mgr.EnsureFresh(context.Background(), "C1")
	<-sink.pushed // ...and its own synchronous push

	// EnsureFresh hands the retry to a goroutine it keeps no handle on,
	// and a *suppressed* fetch leaves nothing to wait for: it takes the
	// backoff branch and returns without touching the API, the cache or
	// the sink. So the old test slept 100ms and hoped that goroutine
	// had run. Running one more attempt inline makes the suppression a
	// fact instead — backgroundFetch is exactly what EnsureFresh
	// spawns, and this call returns only once the decision has been
	// made. (The spawned one takes the same branch; it touches only the
	// manager's mutex, never the db this test closes.)
	mgr.backgroundFetch(context.Background(), "C1")

	if c := api.callCount(); c != 1 {
		t.Errorf("failed fetch re-issued within the backoff window: %d calls, want 1 — this is the reconnect-flap amplifier", c)
	}
}

// TestBackgroundFetchRetriesAfterBackoffExpiry: the backoff throttles
// retries, it does not cancel them. Once the window has passed a stale
// channel must be fetched again — a transient error must not wedge
// membership for the full 24h TTL.
func TestBackgroundFetchRetriesAfterBackoffExpiry(t *testing.T) {
	mgr, api, sink, db := newManagerForTest(t)
	defer db.Close()
	api.err = fmt.Errorf("channel_not_found")
	// Hold the window between "the API was entered" and "the failure
	// was recorded" open, so the interleaving that used to make this
	// test flaky is its default path rather than a rare one (8eaeba9).
	// Nothing is asserted against these 50ms.
	api.delay = 50 * time.Millisecond

	mgr.EnsureFresh(context.Background(), "C1")
	<-sink.pushed // EnsureFresh's own synchronous push of the empty cache

	// Wait for the failure to be *recorded*, not merely for the API to
	// be entered. backgroundFetch writes lastFailed after the call
	// returns, so backdating it below on a callCount barrier alone
	// races the write and gets clobbered — leaving the backoff live and
	// suppressing the retry this test exists to prove.
	awaitFailedFetchDone(mgr, "C1")

	// Simulate a failure far enough in the past that the backoff has
	// expired, then recover.
	mgr.mu.Lock()
	mgr.lastFailed["C1"] = time.Now().Add(-2 * FailureBackoff)
	mgr.mu.Unlock()
	api.err = nil
	api.result = []string{"U1"}

	mgr.EnsureFresh(context.Background(), "C1")
	<-sink.pushed // ...and its own synchronous push, still of the empty set

	// The retry's push, which backgroundFetch sends as its final
	// statement — after ReplaceChannelMembers, after m.members is
	// swapped and after delete(m.lastFailed, ...). That makes it a
	// barrier for both assertions below, so neither has to poll. A call
	// count would fire 50ms before any of it, which is what forced the
	// polling this replaces.
	recovered := <-sink.pushed

	if c := api.callCount(); c != 2 {
		t.Errorf("%d fetch calls; want 2 — the backoff throttles retries, it must not cancel them", c)
	}
	if len(recovered.memberIDs) != 1 || recovered.memberIDs[0] != "U1" {
		t.Errorf("retry pushed %v; want [U1]", recovered.memberIDs)
	}
	// A successful fetch clears the failure record, so the next
	// EnsureFresh is governed by the normal TTL, not the backoff.
	if _, stillMarked := failureRecordedAt(mgr, "C1"); stillMarked {
		t.Error("lastFailed not cleared by a successful fetch; a recovered channel would stay throttled")
	}
}

func TestEnsureFreshConcurrentDoesNotDuplicate(t *testing.T) {
	mgr, api, sink, db := newManagerForTest(t)
	defer db.Close()
	api.result = []string{"U1"}

	const callers = 5
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mgr.EnsureFresh(context.Background(), "C1")
			// EnsureFresh hands its fetch to a goroutine it keeps no
			// handle on, so wg.Wait() alone proves nothing about the
			// dedup — the old test slept 100ms in its place. Running
			// one more attempt inline gives the WaitGroup something to
			// await: once it returns, every caller's attempt has
			// *decided*, which is what the sleep stood in for. This is the
			// same function EnsureFresh spawns, reaching the same
			// dedup branches.
			mgr.backgroundFetch(context.Background(), "C1")
		}()
	}
	wg.Wait()

	// Exactly one attempt gets past the dedup — the checks and the
	// in-flight sentinel are set under one lock hold (manager.go), so
	// no two can both proceed — and it pushes as its last act. That
	// makes the push count exactly `callers` synchronous EnsureFresh
	// snapshots plus one, and draining them is also what keeps the
	// winning fetch from still writing to db while the deferred Close
	// runs.
	for i := 0; i < callers+1; i++ {
		<-sink.pushed
	}

	if c := api.callCount(); c > 1 {
		t.Errorf("expected at most 1 fetch under concurrent EnsureFresh; got %d", c)
	}
}
