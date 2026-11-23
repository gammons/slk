package avatar

import (
	"testing"
	"time"
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

	c.Invalidate()

	if _, _, ok := c.GetSixel("U1"); ok {
		t.Error("GetSixel answered after Invalidate")
	}
	if got := c.Get("U1"); got != "" {
		t.Errorf("Get(U1) = %q after Invalidate, want empty", got)
	}

	// The inflight dedup was released too: a fresh Preload runs and
	// lands again.
	landed := make(chan struct{}, 1)
	c.SetOnReady(func(string) { landed <- struct{}{} })
	c.Preload("U1", url)
	select {
	case <-landed:
	case <-time.After(2 * time.Second):
		t.Fatal("Preload after Invalidate never completed")
	}
	if _, _, ok := c.GetSixel("U1"); !ok {
		t.Error("GetSixel did not answer after the post-Invalidate preload")
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
