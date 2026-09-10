package wake

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// hasMonotonicReading reports whether t carries a monotonic clock
// reading. time.Time.String appends " m=±<seconds>" when — and only
// when — a monotonic reading is present (documented in the time
// package), so this is a stable way to detect it without unsafe access
// to unexported fields.
func hasMonotonicReading(t time.Time) bool {
	return strings.Contains(t.String(), " m=")
}

// fakeClock returns whatever its current field says. Tests advance
// the clock via Advance or Set. The mutex is necessary for the
// TestRun_* tests where the detector goroutine reads concurrently
// with the test goroutine; serialized Step tests don't strictly need
// it but pay nothing for it.
type fakeClock struct {
	mu      sync.Mutex
	current time.Time
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.current
}

func (f *fakeClock) Set(t time.Time) {
	f.mu.Lock()
	f.current = t
	f.mu.Unlock()
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	f.current = f.current.Add(d)
	f.mu.Unlock()
}

func TestNew_DefaultsToRealClock(t *testing.T) {
	d := New(time.Second, time.Second, func(time.Duration) {})
	if d.now == nil {
		t.Fatal("New should default now to a non-nil function")
	}
	t1 := d.now()
	// This sleep is deliberately NOT replaced with a signal, and is not
	// a wall-clock budget. The property under test is precisely that
	// real time elapses between two d.now() calls — a fake clock cannot
	// express it, because the whole point is that New wired d.now to
	// the real one. time.Sleep guarantees *at least* 1ms passes and
	// time.Now's resolution is sub-millisecond everywhere Go runs, so
	// the bias is one-sided: an overlong sleep on a loaded machine only
	// makes the assertion below more true. It can false-pass (if now
	// were wired to some other advancing clock), never false-fail.
	time.Sleep(time.Millisecond)
	t2 := d.now()
	if !t2.After(t1) {
		t.Errorf("now() did not advance — not wired to real clock? t1=%v t2=%v", t1, t2)
	}
}

func TestStep_NoJump_NoCallback(t *testing.T) {
	fc := &fakeClock{current: time.Unix(1000, 0)}
	fired := 0
	d := &Detector{
		interval:  10 * time.Second,
		threshold: 5 * time.Second,
		now:       fc.Now,
		onWake:    func(time.Duration) { fired++ },
	}
	d.Step() // seed baseline at t=1000
	fc.Advance(10 * time.Second)
	d.Step() // elapsed = 10s, not > 15s threshold
	if fired != 0 {
		t.Errorf("callback fired %d times on normal tick; want 0", fired)
	}
}

func TestStep_JumpAboveThreshold_FiresCallback(t *testing.T) {
	fc := &fakeClock{current: time.Unix(1000, 0)}
	var got time.Duration
	fired := 0
	d := &Detector{
		interval:  10 * time.Second,
		threshold: 5 * time.Second,
		now:       fc.Now,
		onWake:    func(e time.Duration) { fired++; got = e },
	}
	d.Step()
	fc.Advance(2 * time.Minute)
	d.Step()
	if fired != 1 {
		t.Errorf("fired = %d, want 1", fired)
	}
	if got != 2*time.Minute {
		t.Errorf("elapsed = %v, want 2m", got)
	}
}

func TestStep_JumpBelowThreshold_NoCallback(t *testing.T) {
	fc := &fakeClock{current: time.Unix(1000, 0)}
	fired := 0
	d := &Detector{
		interval:  10 * time.Second,
		threshold: 5 * time.Second,
		now:       fc.Now,
		onWake:    func(time.Duration) { fired++ },
	}
	d.Step()
	// 14s is greater than interval (10s) but less than interval+threshold (15s).
	// This is the "scheduling jitter" zone we explicitly tolerate.
	fc.Advance(14 * time.Second)
	d.Step()
	if fired != 0 {
		t.Errorf("callback fired %d times in jitter zone; want 0", fired)
	}
}

func TestStep_ExactlyAtThreshold_NoCallback(t *testing.T) {
	// Boundary: elapsed == interval+threshold uses strict >. A jump of
	// exactly the threshold should NOT fire; only strictly larger.
	fc := &fakeClock{current: time.Unix(1000, 0)}
	fired := 0
	d := &Detector{
		interval:  10 * time.Second,
		threshold: 5 * time.Second,
		now:       fc.Now,
		onWake:    func(time.Duration) { fired++ },
	}
	d.Step()
	fc.Advance(15 * time.Second)
	d.Step()
	if fired != 0 {
		t.Errorf("callback fired %d times at exact threshold; want 0 (strict >)", fired)
	}
}

func TestStep_OneNanosecondAboveThreshold_FiresCallback(t *testing.T) {
	// Companion to TestStep_ExactlyAtThreshold: one ns over the
	// boundary must fire. Together these tests pin down the strict-> contract.
	fc := &fakeClock{current: time.Unix(1000, 0)}
	fired := 0
	d := &Detector{
		interval:  10 * time.Second,
		threshold: 5 * time.Second,
		now:       fc.Now,
		onWake:    func(time.Duration) { fired++ },
	}
	d.Step()
	fc.Advance(15*time.Second + 1)
	d.Step()
	if fired != 1 {
		t.Errorf("callback fired %d times at threshold+1ns; want 1", fired)
	}
}

func TestStep_MultipleJumps_FireEach(t *testing.T) {
	fc := &fakeClock{current: time.Unix(1000, 0)}
	var elapsed []time.Duration
	d := &Detector{
		interval:  10 * time.Second,
		threshold: 5 * time.Second,
		now:       fc.Now,
		onWake:    func(e time.Duration) { elapsed = append(elapsed, e) },
	}
	d.Step() // seed
	fc.Advance(60 * time.Second)
	d.Step() // jump 1: 60s
	fc.Advance(120 * time.Second)
	d.Step() // jump 2: 120s
	if len(elapsed) != 2 {
		t.Fatalf("fired %d times, want 2: %v", len(elapsed), elapsed)
	}
	if elapsed[0] != 60*time.Second {
		t.Errorf("first jump = %v, want 60s", elapsed[0])
	}
	if elapsed[1] != 120*time.Second {
		t.Errorf("second jump = %v, want 120s", elapsed[1])
	}
}

func TestStep_NormalTickAfterJump_DoesNotRefire(t *testing.T) {
	// After a jump fires, the next normal tick should NOT re-fire
	// (the baseline `last` must have advanced to the post-jump time).
	fc := &fakeClock{current: time.Unix(1000, 0)}
	fired := 0
	d := &Detector{
		interval:  10 * time.Second,
		threshold: 5 * time.Second,
		now:       fc.Now,
		onWake:    func(time.Duration) { fired++ },
	}
	d.Step() // seed
	fc.Advance(60 * time.Second)
	d.Step() // fires
	if fired != 1 {
		t.Fatalf("expected fire on jump")
	}
	fc.Advance(10 * time.Second)
	d.Step() // normal tick, must not fire
	if fired != 1 {
		t.Errorf("callback re-fired on normal tick after jump; baseline not advanced")
	}
}

func TestStep_FirstCallSeedsWithoutFiring(t *testing.T) {
	// The very first Step seeds last; even if `now` returns wildly
	// in the future, no callback should fire because there's no
	// baseline to compare to.
	fc := &fakeClock{current: time.Unix(9_999_999, 0)}
	fired := 0
	d := &Detector{
		interval:  10 * time.Second,
		threshold: 5 * time.Second,
		now:       fc.Now,
		onWake:    func(time.Duration) { fired++ },
	}
	d.Step()
	if fired != 0 {
		t.Errorf("callback fired %d times on first Step; want 0 (seeding only)", fired)
	}
	if !d.initialized {
		t.Errorf("first Step did not set initialized=true")
	}
	if !d.last.Equal(fc.Now()) {
		t.Errorf("first Step did not store baseline; last=%v want=%v", d.last, fc.Now())
	}
}

func TestStep_StripsMonotonicReading_SoWallJumpsAreDetected(t *testing.T) {
	// Regression test for the suspend-detection bug.
	//
	// In production d.now is time.Now, whose results carry a monotonic
	// clock reading. time.Time.Sub uses the monotonic clock alone when
	// both operands have a reading. On Linux/macOS the monotonic clock
	// does NOT advance while the system is suspended, so if Step stores
	// a monotonic-carrying time, the wall-clock jump on wake is invisible
	// to Sub and onWake never fires — exactly the "doesn't sync after
	// lid close" symptom.
	//
	// The fix is for Step to strip the monotonic reading (Round(0)) so
	// elapsed is computed from the wall clock. This test drives Step with
	// the REAL clock (the only source of monotonic readings) and asserts
	// the stored baseline has no monotonic reading.
	d := New(10*time.Second, 5*time.Second, func(time.Duration) {})

	if !hasMonotonicReading(d.now()) {
		t.Skip("real clock does not carry a monotonic reading on this platform; " +
			"the bug this guards against cannot occur here")
	}

	d.Step() // seeds d.last from the real clock

	if hasMonotonicReading(d.last) {
		t.Fatalf("Step stored a baseline with a monotonic reading (%v); "+
			"Sub will then use the monotonic clock, which is frozen during "+
			"OS suspend, so wall-clock jumps on wake go undetected", d.last)
	}
}

func TestRun_ContextCancellation_ReturnsCleanly(t *testing.T) {
	// Drive Run with a real ticker until we have observed it tick a few
	// times, then cancel. Confirm the goroutine exits.
	d := New(10*time.Millisecond, 5*time.Millisecond, func(time.Duration) {})

	// Count clock observations. Step calls d.now() exactly once per
	// invocation, so N receives from stepped means Run has executed N
	// Steps — the loop demonstrably spinning, rather than "50ms was
	// probably long enough for three ticks". The send is non-blocking
	// so Run is never throttled by a test that stopped listening.
	stepped := make(chan struct{}, 8)
	realNow := d.now
	d.now = func() time.Time {
		now := realNow()
		select {
		case stepped <- struct{}{}:
		default:
		}
		return now
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		d.Run(ctx)
		close(done)
	}()

	// Three Steps: Run's immediate seeding Step plus two ticker-driven
	// ones.
	//
	// No timeout by design: `go test` already imposes one (default 10m),
	// and a wall-clock budget inside the test is exactly what made this
	// load-sensitive. A hang here produces a goroutine dump naming the
	// stuck test, which beats "Run did not return within 1s".
	for i := 0; i < 3; i++ {
		<-stepped
	}
	cancel()
	<-done // Run observed ctx.Done and returned
}

func TestRun_FiresOnSimulatedJump(t *testing.T) {
	// Integration check that Run actually calls Step on every tick.
	// We can't synchronize a real ticker with manual fake-clock
	// advances without races, so we keep the test scope narrow:
	// confirm that *after* simulating a large clock jump on the fake
	// clock, the callback eventually fires. The Step tests cover the
	// exact-threshold and below-threshold cases — this test only
	// proves Run wires Step to the ticker.
	fc := &fakeClock{current: time.Unix(1000, 0)}
	fired := make(chan time.Duration, 10)
	d := New(5*time.Millisecond, 5*time.Millisecond, func(e time.Duration) {
		fired <- e
	})

	// seeded closes once the first clock observation has *returned* its
	// value. The return, not the call, is the barrier we need: Step
	// stores exactly the value it read, so advancing the fake clock any
	// time after the read is observed cannot corrupt the baseline.
	// Signalling before fc.Now() ran would let fc.Advance land first,
	// seeding the baseline at the post-jump time — the jump would then
	// be invisible and this test would wait forever. (Signalling from
	// the fake's entry point instead of after the production read is
	// the exact defect this PR exists to remove.)
	seeded := make(chan struct{})
	var once sync.Once
	d.now = func() time.Time {
		now := fc.Now()
		once.Do(func() { close(seeded) })
		return now
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		d.Run(ctx)
		close(done)
	}()
	defer func() {
		cancel()
		<-done // no detector goroutine outlives this test body
	}()

	<-seeded // Run's first Step has read the baseline off the fake clock

	// Simulate a wake: advance the fake clock by 2 seconds. The next
	// tick calls Step, observes the jump, fires onWake.
	fc.Advance(2 * time.Second)

	// No timeout by design: `go test` already imposes one (default 10m),
	// and a wall-clock budget inside the test is exactly what made this
	// load-sensitive. A hang here produces a goroutine dump naming the
	// stuck test, which beats "callback not fired within 500ms".
	e := <-fired
	if e < 2*time.Second {
		t.Errorf("callback elapsed = %v, want >= 2s", e)
	}
}
