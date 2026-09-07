package main

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/gammons/slk/internal/cache"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/slack-go/slack"
)

// fakeSubscriptions implements threadSubscriptionLister and counts its
// calls, so a caller that fetches the list more than once per trigger
// is visible.
type fakeSubscriptions struct {
	mu       sync.Mutex
	response []slackclient.ThreadSubscriptionView
	err      error
	calls    int
}

func (f *fakeSubscriptions) ListThreadSubscriptions(ctx context.Context) ([]slackclient.ThreadSubscriptionView, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}

// subView constructs a ThreadSubscriptionView from primitives so these
// tests stay readable.
func subView(channel, threadTS, lastRead, text, user string, active bool) slackclient.ThreadSubscriptionView {
	return slackclient.ThreadSubscriptionView{
		Subscription: slackclient.ThreadSubscription{
			ChannelID: channel, ThreadTS: threadTS, LastRead: lastRead, Active: active,
		},
		RootMessage: slack.Message{
			Msg: slack.Msg{
				Timestamp:       threadTS,
				ThreadTimestamp: threadTS,
				User:            user,
				Text:            text,
				Channel:         channel,
			},
		},
	}
}

func newSubscriptionSync(db *cache.DB, fake *fakeSubscriptions, cb func(bool)) *threadSubscriptionSync {
	return &threadSubscriptionSync{client: fake, db: db, workspaceID: "T1", availableCb: cb}
}

// TestThreadSubscriptions_PopulatesTable verifies the sync fetches the
// workspace's subscription list and writes each active item into the
// thread_subscriptions table.
func TestThreadSubscriptions_PopulatesTable(t *testing.T) {
	db := newTestDB(t)
	fake := &fakeSubscriptions{response: []slackclient.ThreadSubscriptionView{
		subView("C1", "1700000100.000000", "1700000150.000000", "p1", "U2", true),
		subView("C2", "1700000200.000000", "1700000250.000000", "p2", "U3", true),
	}}
	if err := newSubscriptionSync(db, fake, nil).sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	got, err := db.ListActiveThreadSubscriptions("T1")
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 subscriptions in DB, got %d", len(got))
	}
}

// TestThreadSubscriptions_UpsertsRootMessageIntoMessagesCache verifies
// every root_msg from the view response is upserted into the messages
// cache, so the threads view can render parents without a separate
// conversations.replies fetch per thread.
func TestThreadSubscriptions_UpsertsRootMessageIntoMessagesCache(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	fake := &fakeSubscriptions{response: []slackclient.ThreadSubscriptionView{
		subView("C1", "1700000100.000000", "1700000150.000000", "parent X", "U2", true),
	}}
	if err := newSubscriptionSync(db, fake, nil).sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}

	msgs, err := db.GetThreadReplies("C1", "1700000100.000000")
	if err != nil {
		t.Fatalf("GetThreadReplies: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("want 1 cached message (the parent), got %d", len(msgs))
	}
	if msgs[0].Text != "parent X" || msgs[0].UserID != "U2" {
		t.Fatalf("root_msg fields not preserved: %+v", msgs[0])
	}
}

// TestThreadSubscriptions_ReconcilesUnsubscribes verifies a local
// subscription absent from the server's fresh list is tombstoned.
func TestThreadSubscriptions_ReconcilesUnsubscribes(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertThreadSubscription("T1", "C1", "1700000100.000000", "1700000150.000000", true); err != nil {
		t.Fatalf("UpsertThreadSubscription: %v", err)
	}
	fake := &fakeSubscriptions{response: []slackclient.ThreadSubscriptionView{
		subView("C2", "1700000300.000000", "1700000350.000000", "p2", "U3", true),
	}}
	if err := newSubscriptionSync(db, fake, nil).sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	got, err := db.ListActiveThreadSubscriptions("T1")
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	if len(got) != 1 || got[0].ChannelID != "C2" {
		t.Fatalf("expected only C2 active after reconcile, got %+v", got)
	}
}

// TestThreadSubscriptions_ErrorTriggersAvailabilityCallback verifies an
// API error fires availableCb(false) and surfaces the error.
func TestThreadSubscriptions_ErrorTriggersAvailabilityCallback(t *testing.T) {
	db := newTestDB(t)
	var calls []bool
	cb := func(available bool) { calls = append(calls, available) }
	fake := &fakeSubscriptions{err: errors.New("network kaboom")}

	if err := newSubscriptionSync(db, fake, cb).sync(context.Background()); err == nil {
		t.Fatalf("expected error, got nil")
	}
	if len(calls) != 1 || calls[0] {
		t.Fatalf("expected one callback with available=false, got %v", calls)
	}
}

// TestThreadSubscriptions_SuccessTriggersAvailabilityCallback verifies a
// successful pass fires availableCb(true) exactly once.
func TestThreadSubscriptions_SuccessTriggersAvailabilityCallback(t *testing.T) {
	db := newTestDB(t)
	var calls []bool
	cb := func(available bool) { calls = append(calls, available) }
	if err := newSubscriptionSync(db, &fakeSubscriptions{}, cb).sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(calls) != 1 || !calls[0] {
		t.Fatalf("expected one callback with available=true, got %v", calls)
	}
}

// zeroStagger neutralises the cross-workspace startup stagger for
// tests that don't exercise it, and restores it afterwards. Without
// this the package-global slot counter would hand later tests delays
// of minutes.
func zeroStagger(t *testing.T) {
	t.Helper()
	oldStep := threadSubsStaggerStep
	threadSubsStagger.Store(0)
	threadSubsStaggerStep = 0
	t.Cleanup(func() {
		threadSubsStaggerStep = oldStep
		threadSubsStagger.Store(0)
	})
}

// awaitGateRelease blocks until the sync goroutine has released the
// gate, which it does — through ensureThreadSubscriptions' deferred
// gate.done() — as the very last thing it executes.
//
// Tests that need a barrier *after* that goroutine is finished have
// nothing else to wait on. onDone is one deferred call too early: a
// trigger issued the instant it fires still finds running=true and is
// dropped on the floor, and a failed sync never calls onDone at all.
// Nothing production invokes after gate.done(), so there is no channel
// to receive from; the gate's own flag is the state, and reading it
// under the gate's mutex is the barrier.
//
// This is a condition wait, not a wall-clock budget: it returns the
// instant the flag clears and never on a deadline, so a loaded machine
// can only make it spin a few more times — it cannot false-fail. If the
// goroutine never exits, this hangs and `go test`'s own timeout dumps
// the goroutine that is stuck, the same failure mode as a blocking
// channel receive.
func awaitGateRelease(g *threadSubsGate) {
	for {
		g.mu.Lock()
		running := g.running
		g.mu.Unlock()
		if !running {
			return
		}
		runtime.Gosched()
	}
}

func TestThreadSubsGate_FirstSyncAdmittedOnce(t *testing.T) {
	g := &threadSubsGate{window: time.Hour}
	now := time.Now()

	first, ok := g.tryStart(now)
	if !ok || !first {
		t.Fatalf("first tryStart = (%v, %v); want (true, true)", first, ok)
	}
	g.done()

	if _, ok := g.tryStart(now.Add(30 * time.Minute)); ok {
		t.Fatal("tryStart inside the window admitted a second sync")
	}
	first, ok = g.tryStart(now.Add(2 * time.Hour))
	if !ok {
		t.Fatal("tryStart after the window did not admit a re-sync")
	}
	if first {
		t.Error("re-sync reported first=true; the startup stagger must apply exactly once per workspace")
	}
	g.done()
}

func TestThreadSubsGate_BlocksWhileRunning(t *testing.T) {
	// A second trigger landing mid-sync (view opened while the boot
	// sync is still paginating) must not double the request volume,
	// even when the throttle window has technically elapsed.
	g := &threadSubsGate{window: 0}
	if _, ok := g.tryStart(time.Now()); !ok {
		t.Fatal("first tryStart not admitted")
	}
	if _, ok := g.tryStart(time.Now().Add(time.Hour)); ok {
		t.Fatal("tryStart admitted while a sync is still running")
	}
	g.done()
	if _, ok := g.tryStart(time.Now().Add(time.Hour)); !ok {
		t.Fatal("tryStart after done() not admitted")
	}
}

// The Threads view refetches its list on activation and on every
// ThreadsListDirtyMsg — including the one this sync itself sends — so
// an ungated trigger here would either loop or fire on every
// interaction. subscriptions.thread.getView paginates to a 1000-item
// hard cap, measured at 62 requests per workspace on a real account.
func TestEnsureThreadSubscriptions_FetchesOnceWithinWindow(t *testing.T) {
	zeroStagger(t)
	db := newTestDB(t)
	fake := &fakeSubscriptions{response: []slackclient.ThreadSubscriptionView{
		subView("C1", "1700000100.000000", "1700000150.000000", "p1", "U2", true),
	}}
	gate := &threadSubsGate{window: time.Hour}
	done := make(chan struct{}, 8)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ensureThreadSubscriptions(context.Background(), gate,
				newSubscriptionSync(db, fake, nil),
				func() { done <- struct{}{} })
		}()
	}
	wg.Wait()

	// No timeout by design: `go test` already imposes one, and a
	// wall-clock budget inside the test is exactly what made this
	// load-sensitive. A hang here produces a goroutine dump naming the
	// stuck test, which beats "the first trigger never completed a
	// subscription sync" two seconds after the fact.
	//
	// onDone is the barrier and the fake's entry point is not:
	// ensureThreadSubscriptions calls onDone only after sync has
	// reconciled the table and upserted every root_msg, so receiving
	// here proves no goroutine is still writing to the cache that
	// t.Cleanup is about to close.
	<-done // the one admitted trigger finished its sync

	fake.mu.Lock()
	calls := fake.calls
	fake.mu.Unlock()
	if calls != 1 {
		t.Errorf("ListThreadSubscriptions called %d times; want 1 — boot, activation and dirty-refresh all trigger this", calls)
	}
	if len(done) != 0 {
		t.Errorf("%d extra completions; the notify must fire once, or every one re-triggers the list fetch", len(done))
	}
}

// The gate is a throttle, not a latch: after the window elapses a
// trigger (wake-from-sleep, WS reconnect) syncs again. This is what
// freshens threads after the app sat offline with a dead socket.
func TestEnsureThreadSubscriptions_ResyncsAfterWindow(t *testing.T) {
	zeroStagger(t)
	db := newTestDB(t)
	fake := &fakeSubscriptions{response: []slackclient.ThreadSubscriptionView{
		subView("C1", "1700000100.000000", "1700000150.000000", "p1", "U2", true),
	}}
	gate := &threadSubsGate{window: 20 * time.Millisecond}
	done := make(chan struct{}, 2)

	ensureThreadSubscriptions(context.Background(), gate,
		newSubscriptionSync(db, fake, nil),
		func() { done <- struct{}{} })
	// No timeout by design: `go test` already imposes one, and a
	// wall-clock budget inside the test is exactly what made this
	// load-sensitive. A hang here produces a goroutine dump naming the
	// stuck test, which beats "first sync never completed".
	<-done                 // the first sync completed
	awaitGateRelease(gate) // ...and the goroutine released the gate

	// A duration is semantically required here and no signal can
	// replace it: the property under test is that the throttle
	// *window* elapses, and the gate reads the real clock
	// (tryStart(time.Now())) with no seam a test may inject through.
	// The bias is one-sided — time.Sleep is guaranteed to sleep at
	// least its argument, and the gate recorded `last` before this
	// line — so load can only leave the window more elapsed, never
	// less. It cannot false-fail. The other reason the old
	// time.Sleep(40ms) was needed, giving the first goroutine room to
	// release the gate, is now awaitGateRelease's job above; that half
	// *was* load-sensitive and would have silently dropped the second
	// trigger.
	time.Sleep(gate.window)

	ensureThreadSubscriptions(context.Background(), gate,
		newSubscriptionSync(db, fake, nil),
		func() { done <- struct{}{} })
	<-done // the post-window re-sync completed

	fake.mu.Lock()
	calls := fake.calls
	fake.mu.Unlock()
	if calls != 2 {
		t.Errorf("ListThreadSubscriptions called %d times; want 2 (initial + post-window re-sync)", calls)
	}
}

// On boot every workspace becomes ready at once; on Enterprise Grid
// that would fire N paginated getView sweeps in the same second. The
// first sync per workspace waits slot*step so the sweeps spread out.
// The stagger applies to the first sync only — later re-syncs are
// already thinned out by the window and must not wait.
func TestEnsureThreadSubscriptions_FirstSyncStaggers(t *testing.T) {
	oldStep := threadSubsStaggerStep
	t.Cleanup(func() {
		threadSubsStaggerStep = oldStep
		threadSubsStagger.Store(0)
	})

	db := newTestDB(t)
	fake := &fakeSubscriptions{response: []slackclient.ThreadSubscriptionView{
		subView("C1", "1700000100.000000", "1700000150.000000", "p1", "U2", true),
	}}
	done := make(chan struct{}, 2)

	// The stagger holds the fetch back. A slot worth two hours cannot
	// elapse, so cancelling the context is the only exit from the
	// wait — and a sync that fetched anyway never waited. Proving it
	// by cancellation rather than by sleeping is what makes this
	// deterministic: the old form slept 50ms into a 200ms slot and
	// failed on any machine that overshot the remaining 150ms, which
	// is precisely the load-sensitive assertion this PR exists to
	// remove.
	threadSubsStaggerStep = time.Hour
	threadSubsStagger.Store(2) // this workspace drew slot 2 → a 2h delay
	blocked := &threadSubsGate{window: 0}
	ctx, cancel := context.WithCancel(context.Background())
	ensureThreadSubscriptions(ctx, blocked,
		newSubscriptionSync(db, fake, nil),
		func() { done <- struct{}{} })
	cancel()
	awaitGateRelease(blocked) // the staggered goroutine abandoned its wait
	fake.mu.Lock()
	early := fake.calls
	fake.mu.Unlock()
	if early != 0 {
		t.Fatalf("the first sync fetched %d time(s) without waiting out its stagger slot", early)
	}

	// A slot that does elapse still runs the sync — the stagger delays
	// the first fetch, it does not cancel it. Nothing is asserted
	// against the 2ms this waits; the receive below returns when the
	// sync completes, however long the slot actually took.
	threadSubsStaggerStep = time.Millisecond
	threadSubsStagger.Store(2)
	gate := &threadSubsGate{window: 0}
	ensureThreadSubscriptions(context.Background(), gate,
		newSubscriptionSync(db, fake, nil),
		func() { done <- struct{}{} })
	<-done                 // the staggered first sync completed
	awaitGateRelease(gate) // ...and released the gate for the re-sync

	// Re-sync: no stagger, runs immediately even though the slot
	// counter says this process has booted workspaces. Restoring the
	// hour-long step makes that an assertion rather than a hope — a
	// re-sync that staggered would draw a slot worth hours and never
	// reach onDone, hanging here instead of quietly passing the way it
	// did under a 2s deadline that a 300ms stagger fits inside.
	threadSubsStaggerStep = time.Hour
	ensureThreadSubscriptions(context.Background(), gate,
		newSubscriptionSync(db, fake, nil),
		func() { done <- struct{}{} })
	<-done // the un-staggered re-sync completed

	fake.mu.Lock()
	calls := fake.calls
	fake.mu.Unlock()
	if calls != 2 {
		t.Errorf("ListThreadSubscriptions called %d times; want 2 (staggered first + immediate re-sync)", calls)
	}
}

func TestEnsureThreadSubscriptions_FailureDoesNotNotify(t *testing.T) {
	// A ThreadsListDirtyMsg after a failed fetch would send the view
	// back to the same cache it already rendered, for nothing.
	zeroStagger(t)
	db := newTestDB(t)
	fake := &fakeSubscriptions{err: errors.New("boom")}
	gate := &threadSubsGate{window: time.Hour}
	notified := make(chan struct{}, 1)

	ensureThreadSubscriptions(context.Background(), gate,
		newSubscriptionSync(db, fake, nil),
		func() { notified <- struct{}{} })

	// The fetch fails, so onDone never fires and there is no signal to
	// receive; gate.done() is the last thing the goroutine executes,
	// which makes the released gate the only barrier available.
	//
	// It is also strictly stronger than what it replaces. The old form
	// polled for the call count under a 300ms deadline and then waited
	// 100ms to "give the goroutine room to do the wrong thing" — both
	// bets on how long late is. Once the goroutine has exited, a
	// notification can no longer arrive at all, so the check below is
	// exact.
	awaitGateRelease(gate) // the sync goroutine failed and exited

	fake.mu.Lock()
	calls := fake.calls
	fake.mu.Unlock()
	if calls != 1 {
		t.Fatalf("ListThreadSubscriptions called %d times; want 1 — the sync never ran", calls)
	}
	select {
	case <-notified:
		t.Error("notified the UI after a failed fetch")
	default:
	}
}
