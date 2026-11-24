package avatar

import (
	"testing"
)

// Invalidate drops every rendered avatar so the lazy Preload path
// re-encodes them, exactly as it would for a user's first render. It is
// how a cell-size change re-points the renders that were encoded at the
// old pixel size.
func TestInvalidate_ClearsRendersAndReleasesInflight(t *testing.T) {
	c, url, done := testCache(t)
	defer done()
	c.SetSixel(true)
	c.PreloadSync("U1", url)

	if _, b, ok := c.GetSixel("U1"); !ok || len(b) == 0 {
		t.Fatal("expected a sixel avatar before Invalidate")
	}
	if c.Get("U1") == "" {
		t.Fatal("expected a half-block render before Invalidate")
	}

	// Seed an inflight entry the way a running Preload holds it (a
	// PreloadSync bypasses the set, so this is the only way to observe
	// its release deterministically): if Invalidate stopped clearing
	// it, the lazy Preload path would stay deduped out and the avatar
	// would never re-encode.
	c.inflight.Store("U2", struct{}{})

	c.Invalidate()

	if _, _, ok := c.GetSixel("U1"); ok {
		t.Error("GetSixel answered after Invalidate")
	}
	if got := c.Get("U1"); got != "" {
		t.Errorf("Get(U1) = %q after Invalidate, want empty", got)
	}
	if _, ok := c.inflight.Load("U2"); ok {
		t.Error("inflight kept an entry across Invalidate; the lazy Preload path would stay deduped out")
	}
}

// A preload that started before an Invalidate drops its result instead
// of re-storing a render that was encoded at the old pixel size.
func TestPreloadInner_DropsResultAfterInvalidate(t *testing.T) {
	c, url, done := testCache(t)
	defer done()
	c.SetSixel(true)

	started := make(chan struct{})
	release := make(chan struct{})
	workerDone := make(chan struct{})
	c.sizeURL = func(u string) string {
		close(started)
		<-release
		return u
	}

	go func() {
		defer close(workerDone)
		c.preloadInner("U1", url)
	}()
	<-started
	c.Invalidate()
	close(release)
	<-workerDone

	if got := c.Get("U1"); got != "" {
		t.Errorf("Get(U1) = %q, want the mid-flight preload dropped", got)
	}
	if _, _, ok := c.GetSixel("U1"); ok {
		t.Error("GetSixel answered for the mid-flight preload")
	}
}

// Without an intervening Invalidate, the same preload stores normally --
// the drop is the generation change, not the sizeURL hook.
func TestPreloadInner_StoresWithoutInvalidate(t *testing.T) {
	c, url, done := testCache(t)
	defer done()
	c.SetSixel(true)

	started := make(chan struct{})
	release := make(chan struct{})
	workerDone := make(chan struct{})
	c.sizeURL = func(u string) string {
		close(started)
		<-release
		return u
	}

	go func() {
		defer close(workerDone)
		c.preloadInner("U1", url)
	}()
	<-started
	close(release)
	<-workerDone

	if got := c.Get("U1"); got == "" {
		t.Error("Get(U1) empty, want the half-block render")
	}
	if _, _, ok := c.GetSixel("U1"); !ok {
		t.Error("GetSixel did not answer")
	}
}
