package main

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui"
)

func TestWorkspaceContextCustomEmojiDefaultsEmpty(t *testing.T) {
	var wctx WorkspaceContext
	if got := wctx.CustomEmoji(); got == nil || len(got) != 0 {
		t.Errorf("CustomEmoji() before load = %v, want empty non-nil map", got)
	}
}

// SetCustomEmoji replaces rather than merges: emoji.list returns the
// workspace's whole set, so the bootstrap subset it lands on top of is
// a strict subset and merging would only keep stale entries alive.
func TestWorkspaceContextCustomEmojiReplaces(t *testing.T) {
	wctx := &WorkspaceContext{}
	wctx.SetCustomEmoji(map[string]string{"from-boot": "https://example.test/boot.png"})
	wctx.SetCustomEmoji(map[string]string{"from-list": "https://example.test/list.png"})

	got := wctx.CustomEmoji()
	if _, ok := got["from-boot"]; ok {
		t.Errorf("CustomEmoji() = %v, want the bootstrap entry replaced, not merged", got)
	}
	if got["from-list"] != "https://example.test/list.png" {
		t.Errorf("CustomEmoji()[from-list] = %q, want the emoji.list URL", got["from-list"])
	}
}

// The emoji.list fetch publishes the map from a background goroutine
// while the workspace-switch cmd reads it to build WorkspaceSwitchedMsg.
// Run under -race to catch a regression back to a plain map field.
func TestWorkspaceContextCustomEmojiConcurrentAccess(t *testing.T) {
	wctx := &WorkspaceContext{}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			wctx.SetCustomEmoji(map[string]string{"party-parrot": "https://example.test/parrot.gif"})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = wctx.CustomEmoji()["party-parrot"]
		}
	}()
	wg.Wait()
	if got := wctx.CustomEmoji()["party-parrot"]; got != "https://example.test/parrot.gif" {
		t.Errorf("CustomEmoji()[party-parrot] = %q, want the published URL", got)
	}
}

// fakeEmojiLister records ListCustomEmoji calls so a test can assert
// the fetch actually happened, not merely that state changed.
type fakeEmojiLister struct {
	mu     sync.Mutex
	calls  int
	result map[string]string
	err    error
}

func (f *fakeEmojiLister) ListCustomEmoji(_ context.Context) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.result, f.err
}

func (f *fakeEmojiLister) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// captureEmojiSender records messages dispatched into the tea loop.
type captureEmojiSender struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (c *captureEmojiSender) Send(m tea.Msg) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, m)
}

func (c *captureEmojiSender) snapshot() []tea.Msg {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]tea.Msg(nil), c.msgs...)
}

// TestFetchWorkspaceEmoji_RunsEvenWhenBootstrapPublishedASubset is the
// regression guard for this whole change.
//
// conversations.view returns only the emoji the restored conversation
// uses. The previous code treated a non-empty CustomEmoji() as "already
// fetched" and returned early, so every other channel rendered custom
// emoji as literal :name:. Re-adding that short-circuit must fail here.
func TestFetchWorkspaceEmoji_RunsEvenWhenBootstrapPublishedASubset(t *testing.T) {
	wctx := &WorkspaceContext{}
	// Bootstrap publishes the one emoji the restored channel used.
	wctx.SetCustomEmoji(map[string]string{"from-boot": "https://example.test/boot.png"})

	api := &fakeEmojiLister{result: map[string]string{
		"from-boot":    "https://example.test/boot.png",
		"party-parrot": "https://example.test/parrot.gif",
	}}
	sender := &captureEmojiSender{}

	fetchWorkspaceEmoji(context.Background(), wctx, api, newTestDB(t), sender, "T1")

	if got := api.callCount(); got != 1 {
		t.Fatalf("ListCustomEmoji calls = %d, want 1 — a non-empty bootstrap subset must NOT suppress emoji.list", got)
	}
	if got := wctx.CustomEmoji(); len(got) != 2 || got["party-parrot"] == "" {
		t.Errorf("CustomEmoji() = %v, want the full workspace set", got)
	}
}

// A failed emoji.list must leave the bootstrap subset intact rather
// than clearing it: a partial set renders more emoji than none.
func TestFetchWorkspaceEmoji_ErrorKeepsBootstrapSubset(t *testing.T) {
	wctx := &WorkspaceContext{}
	wctx.SetCustomEmoji(map[string]string{"from-boot": "https://example.test/boot.png"})

	api := &fakeEmojiLister{err: errors.New("ratelimited")}
	sender := &captureEmojiSender{}

	fetchWorkspaceEmoji(context.Background(), wctx, api, newTestDB(t), sender, "T1")

	if got := wctx.CustomEmoji()["from-boot"]; got != "https://example.test/boot.png" {
		t.Errorf("CustomEmoji()[from-boot] = %q, want the bootstrap entry preserved on error", got)
	}
	if msgs := sender.snapshot(); len(msgs) != 0 {
		t.Errorf("sent %d messages on error, want 0 — a failed fetch must not tell the UI to re-render", len(msgs))
	}
}

// The UI only picks up the new set when the follow-up message lands,
// and it must be tagged with the workspace it belongs to.
func TestFetchWorkspaceEmoji_SendsLoadedMsgForItsTeam(t *testing.T) {
	wctx := &WorkspaceContext{}
	api := &fakeEmojiLister{result: map[string]string{"party-parrot": "https://example.test/parrot.gif"}}
	sender := &captureEmojiSender{}

	fetchWorkspaceEmoji(context.Background(), wctx, api, newTestDB(t), sender, "T_OTHER")

	msgs := sender.snapshot()
	if len(msgs) != 1 {
		t.Fatalf("sent %d messages, want 1", len(msgs))
	}
	loaded, ok := msgs[0].(ui.CustomEmojisLoadedMsg)
	if !ok {
		t.Fatalf("message type = %T, want ui.CustomEmojisLoadedMsg", msgs[0])
	}
	if loaded.TeamID != "T_OTHER" {
		t.Errorf("TeamID = %q, want T_OTHER — an untagged message would repaint the wrong workspace", loaded.TeamID)
	}
	if loaded.CustomEmoji["party-parrot"] == "" {
		t.Errorf("CustomEmoji = %v, want the fetched set", loaded.CustomEmoji)
	}
}

// A nil sender must not panic: the fetch is best-effort and runs in a
// bare goroutine, so a panic there would take the process down.
func TestFetchWorkspaceEmoji_NilSenderDoesNotPanic(t *testing.T) {
	wctx := &WorkspaceContext{}
	api := &fakeEmojiLister{result: map[string]string{"a": "b"}}
	fetchWorkspaceEmoji(context.Background(), wctx, api, newTestDB(t), nil, "T1")
	if wctx.CustomEmoji()["a"] != "b" {
		t.Error("CustomEmoji() should still be published with a nil sender")
	}
}

func seedEmojiCache(t *testing.T, db *cache.DB, fetchedAt time.Time) {
	t.Helper()
	emojis := map[string]string{"cached": "https://example.test/cached.png", "shared": "https://example.test/old.png"}
	if err := db.ReplaceCustomEmoji("T1", emojis, fetchedAt.Unix()); err != nil {
		t.Fatal(err)
	}
}

func TestBootCustomEmoji_Freshness(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	tests := []struct {
		name      string
		fetchedAt time.Time
		want      time.Duration
	}{
		{"within the TTL", now.Add(-customEmojiTTL + time.Minute), time.Minute},
		{"past the TTL", now.Add(-customEmojiTTL), 0},
		{"clock went backwards", now.Add(time.Hour), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newTestDB(t)
			seedEmojiCache(t, db, tt.fetchedAt)
			if _, freshFor := bootCustomEmoji(db, "T1", nil, now); freshFor != tt.want {
				t.Errorf("freshFor = %v, want %v", freshFor, tt.want)
			}
		})
	}
}

func TestBootCustomEmoji_SubsetOverlaysCache(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	seedEmojiCache(t, db, now)

	got, _ := bootCustomEmoji(db, "T1", map[string]string{"shared": "https://example.test/new.png"}, now)
	want := map[string]string{"cached": "https://example.test/cached.png", "shared": "https://example.test/new.png"}
	if !maps.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// The #160 regression in its cached form: rows without a recorded
// emoji.list, or no rows at all, must not skip the fetch.
func TestBootCustomEmoji_NeverFetchedIsNotFresh(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertCustomEmoji("T1", "from-event", "https://example.test/e.png"); err != nil {
		t.Fatal(err)
	}
	got, freshFor := bootCustomEmoji(db, "T1", map[string]string{"from-boot": "https://example.test/b.png"}, time.Now())
	if freshFor != 0 {
		t.Errorf("freshFor = %v without a recorded emoji.list, want 0", freshFor)
	}
	if len(got) != 2 {
		t.Errorf("got %v, want the event row and the subset", got)
	}
}

func TestBootCustomEmoji_ReadFailureFallsBackToFetch(t *testing.T) {
	db := newTestDB(t)
	seedEmojiCache(t, db, time.Now())
	db.Close()

	subset := map[string]string{"from-boot": "https://example.test/b.png"}
	got, freshFor := bootCustomEmoji(db, "T1", subset, time.Now())
	if freshFor != 0 {
		t.Errorf("freshFor = %v after a failed read, want 0", freshFor)
	}
	if !maps.Equal(got, subset) {
		t.Errorf("got %v, want the subset", got)
	}
}

func TestFetchWorkspaceEmoji_PersistsTheSet(t *testing.T) {
	db := newTestDB(t)
	wctx := &WorkspaceContext{}
	api := &fakeEmojiLister{result: map[string]string{"a": "b"}}

	fetchWorkspaceEmoji(context.Background(), wctx, api, db, nil, "T1")

	got, _, fresh := reloadEmojiCache(t, db)
	if !maps.Equal(got, api.result) || !fresh {
		t.Errorf("cache = %v, fresh = %v, want the fetched set, fresh", got, fresh)
	}
}

func TestFetchWorkspaceEmoji_ErrorKeepsTheCache(t *testing.T) {
	db := newTestDB(t)
	old := time.Now().Add(-2 * customEmojiTTL)
	seedEmojiCache(t, db, old)
	api := &fakeEmojiLister{err: errors.New("network down")}

	fetchWorkspaceEmoji(context.Background(), &WorkspaceContext{}, api, db, nil, "T1")

	got, ts, _ := reloadEmojiCache(t, db)
	want := map[string]string{"cached": "https://example.test/cached.png", "shared": "https://example.test/old.png"}
	if !maps.Equal(got, want) || ts != old.Unix() {
		t.Errorf("cache = %v, fetched_at = %d, want both unchanged", got, ts)
	}
}

func reloadEmojiCache(t *testing.T, db *cache.DB) (map[string]string, int64, bool) {
	t.Helper()
	got, freshFor := bootCustomEmoji(db, "T1", nil, time.Now())
	ts, _, err := db.CustomEmojiFetchedAt("T1")
	if err != nil {
		t.Fatal(err)
	}
	return got, ts, freshFor > 0
}

func newEmojiHandler(t *testing.T, published map[string]string) (*rtmEventHandler, *captureEmojiSender, *cache.DB) {
	t.Helper()
	db := newTestDB(t)
	wctx := &WorkspaceContext{TeamID: "T1"}
	wctx.SetCustomEmoji(published)
	sender := &captureEmojiSender{}
	return &rtmEventHandler{workspaceID: "T1", wsCtx: wctx, db: db, program: sender}, sender, db
}

func TestOnEmojiChanged_Deltas(t *testing.T) {
	published := map[string]string{"keep": "u1", "gone": "u2"}
	before := maps.Clone(published)
	h, sender, db := newEmojiHandler(t, published)
	if err := db.ReplaceCustomEmoji("T1", published, 100); err != nil {
		t.Fatal(err)
	}

	h.OnEmojiChanged("add", "new", nil, "alias:keep")
	h.OnEmojiChanged("remove", "", []string{"gone"}, "")

	want := map[string]string{"keep": "u1", "new": "alias:keep"}
	if got := h.wsCtx.CustomEmoji(); !maps.Equal(got, want) {
		t.Errorf("published = %v, want %v", got, want)
	}
	if got, _ := db.CustomEmoji("T1"); !maps.Equal(got, want) {
		t.Errorf("cache = %v, want %v", got, want)
	}
	if !maps.Equal(published, before) {
		t.Errorf("mutated the published map: %v", published)
	}
	msgs := sender.snapshot()
	if len(msgs) != 2 {
		t.Fatalf("sent %d messages, want 2", len(msgs))
	}
	if last := msgs[1].(ui.CustomEmojisLoadedMsg); last.TeamID != "T1" || !maps.Equal(last.CustomEmoji, want) {
		t.Errorf("last message = %+v", last)
	}
	// The map the first message handed the UI must not change under it.
	if first := msgs[0].(ui.CustomEmojisLoadedMsg).CustomEmoji; !maps.Equal(first, map[string]string{"keep": "u1", "gone": "u2", "new": "alias:keep"}) {
		t.Errorf("first message's map changed to %v", first)
	}
}

func TestOnEmojiChanged_UnknownSubtypeRefetches(t *testing.T) {
	for _, tt := range []struct {
		name           string
		subtype, name_ string
		names          []string
	}{
		{"unknown subtype", "rename", "", nil},
		{"add without a name", "add", "", nil},
		{"remove without names", "remove", "", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h, sender, db := newEmojiHandler(t, map[string]string{"old": "u1"})
			if err := db.ReplaceCustomEmoji("T1", map[string]string{"old": "u1"}, time.Now().Unix()); err != nil {
				t.Fatal(err)
			}
			api := &fakeEmojiLister{result: map[string]string{"renamed": "u1"}}
			h.emojiLister = api

			h.OnEmojiChanged(tt.subtype, tt.name_, tt.names, "u1")

			deadline := time.Now().Add(5 * time.Second)
			for len(sender.snapshot()) == 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if api.callCount() != 1 {
				t.Fatalf("ListCustomEmoji calls = %d, want 1", api.callCount())
			}
			if got := h.wsCtx.CustomEmoji(); !maps.Equal(got, api.result) {
				t.Errorf("published = %v, want the refetched set", got)
			}
		})
	}
}

// A refetch that fails leaves the cache stale, so the next launch
// refetches instead of trusting a list known to be out of date.
func TestOnEmojiChanged_FailedRefetchLeavesCacheStale(t *testing.T) {
	h, _, db := newEmojiHandler(t, map[string]string{"old": "u1"})
	if err := db.ReplaceCustomEmoji("T1", map[string]string{"old": "u1"}, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	api := &fakeEmojiLister{err: errors.New("network down")}
	h.emojiLister = api

	h.OnEmojiChanged("rename", "", nil, "")

	deadline := time.Now().Add(5 * time.Second)
	for api.callCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, _, fresh := reloadEmojiCache(t, db); fresh {
		t.Error("cache still fresh after a failed refetch")
	}
	if got, _ := db.CustomEmoji("T1"); got["old"] != "u1" {
		t.Errorf("cache = %v, want the rows kept", got)
	}
}

// The startup gate end to end: a bootstrap subset with nothing cached
// fetches at once; a fresh cache waits.
func TestRefreshWorkspaceEmoji_FetchesUnlessFresh(t *testing.T) {
	for _, tt := range []struct {
		name   string
		cached bool
		want   int
	}{
		{"subset only", false, 1},
		{"fresh cache", true, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := newTestDB(t)
			if tt.cached {
				seedEmojiCache(t, db, time.Now())
			}
			_, freshFor := bootCustomEmoji(db, "T1", map[string]string{"from-boot": "https://example.test/b.png"}, time.Now())
			api := &fakeEmojiLister{result: map[string]string{"a": "b"}}
			sender := &captureEmojiSender{}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				refreshWorkspaceEmoji(ctx, &WorkspaceContext{}, api, db, sender, "T1", freshFor)
				close(done)
			}()

			deadline := time.Now().Add(200 * time.Millisecond)
			for api.callCount() < tt.want && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			time.Sleep(20 * time.Millisecond) // room for an unwanted call to show up
			cancel()
			<-done
			if got := api.callCount(); got != tt.want {
				t.Errorf("ListCustomEmoji calls = %d, want %d", got, tt.want)
			}
		})
	}
}

// gatedEmojiLister returns results in order, holding the first call
// until release is closed.
type gatedEmojiLister struct {
	mu      sync.Mutex
	results []map[string]string
	calls   int
	started chan struct{}
	release chan struct{}
}

func (g *gatedEmojiLister) ListCustomEmoji(_ context.Context) (map[string]string, error) {
	g.mu.Lock()
	i := g.calls
	g.calls++
	g.mu.Unlock()
	if i == 0 {
		close(g.started)
		<-g.release
	}
	return g.results[i], nil
}

// A delta applied while emoji.list is in flight must survive: the list
// may predate it, so it is fetched again.
func TestFetchWorkspaceEmoji_RefetchesWhenADeltaLandsMidFetch(t *testing.T) {
	h, _, db := newEmojiHandler(t, map[string]string{"from-boot": "u0"})
	api := &gatedEmojiLister{
		results: []map[string]string{{"a": "u1"}, {"a": "u1", "new": "u2"}},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	done := make(chan struct{})
	go func() {
		fetchWorkspaceEmoji(context.Background(), h.wsCtx, api, db, h.program, "T1")
		close(done)
	}()
	<-api.started
	h.OnEmojiChanged("add", "new", nil, "u2")
	close(api.release)
	<-done

	want := map[string]string{"a": "u1", "new": "u2"}
	if api.calls != 2 {
		t.Errorf("ListCustomEmoji calls = %d, want 2", api.calls)
	}
	if got := h.wsCtx.CustomEmoji(); !maps.Equal(got, want) {
		t.Errorf("published = %v, want %v", got, want)
	}
	if got, _ := db.CustomEmoji("T1"); !maps.Equal(got, want) {
		t.Errorf("cache = %v, want %v", got, want)
	}
}

// A delta the cache failed to store must not leave rows that pass as
// fresh: the next launch would trust them and lose the emoji.
func TestOnEmojiChanged_FailedWriteMarksCacheStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	db, err := cache.New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.ReplaceCustomEmoji("T1", map[string]string{"old": "u1"}, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	// Break the row table from a second connection; the meta table stays.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(`DROP TABLE custom_emoji`); err != nil {
		t.Fatal(err)
	}

	wctx := &WorkspaceContext{TeamID: "T1"}
	wctx.SetCustomEmoji(map[string]string{"old": "u1"})
	h := &rtmEventHandler{workspaceID: "T1", wsCtx: wctx, db: db, program: &captureEmojiSender{}}
	h.OnEmojiChanged("add", "new", nil, "u2")

	if ts, ok, err := db.CustomEmojiFetchedAt("T1"); err != nil || !ok || ts != 0 {
		t.Errorf("CustomEmojiFetchedAt = %d, %v, %v, want 0, true, nil", ts, ok, err)
	}
	if got := wctx.CustomEmoji()["new"]; got != "u2" {
		t.Errorf("published new = %q, want the delta applied in memory anyway", got)
	}
}

// A refetch-triggering event during an in-flight fetch must win: the
// in-flight list may predate the change, so it is fetched again rather
// than published over the event's newer list.
func TestOnEmojiChanged_RefetchSupersedesAFetchInFlight(t *testing.T) {
	h, sender, db := newEmojiHandler(t, map[string]string{"old": "u1"})
	newer := map[string]string{"renamed": "u1"}
	api := &gatedEmojiLister{
		results: []map[string]string{{"old": "u1"}, newer, newer},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	h.emojiLister = api
	done := make(chan struct{})
	go func() {
		fetchWorkspaceEmoji(context.Background(), h.wsCtx, api, db, h.program, "T1")
		close(done)
	}()
	<-api.started
	h.OnEmojiChanged("rename", "", nil, "")
	deadline := time.Now().Add(5 * time.Second)
	for len(sender.snapshot()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(api.release)
	<-done

	if got := h.wsCtx.CustomEmoji(); !maps.Equal(got, newer) {
		t.Errorf("published = %v, want %v", got, newer)
	}
	if got, _ := db.CustomEmoji("T1"); !maps.Equal(got, newer) {
		t.Errorf("cache = %v, want %v", got, newer)
	}
}

// A burst of refetch-triggering events shares one refetch instead of
// each starting its own.
func TestOnEmojiChanged_CoalescesRefetches(t *testing.T) {
	h, _, _ := newEmojiHandler(t, map[string]string{"old": "u1"})
	results := make([]map[string]string, 20)
	for i := range results {
		results[i] = map[string]string{"renamed": "u1"}
	}
	api := &gatedEmojiLister{results: results, started: make(chan struct{}), release: make(chan struct{})}
	h.emojiLister = api

	h.OnEmojiChanged("rename", "", nil, "")
	<-api.started
	for range 5 {
		h.OnEmojiChanged("rename", "", nil, "")
	}
	close(api.release)

	w := h.wsCtx
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w.emojiMu.Lock()
		running := w.emojiRefetching
		w.emojiMu.Unlock()
		if !running {
			break
		}
		time.Sleep(time.Millisecond)
	}
	api.mu.Lock()
	calls := api.calls
	api.mu.Unlock()
	// The held call is discarded (the burst bumped emojiSeq), one retry
	// publishes, and the pending flag runs one more fetch.
	if calls > 3 {
		t.Errorf("ListCustomEmoji calls = %d, want at most 3 for a burst of 6", calls)
	}
	if got := w.CustomEmoji(); !maps.Equal(got, results[0]) {
		t.Errorf("published = %v, want the refetched set", got)
	}
}

// A reconnect may have missed emoji_changed events, so it marks the
// cache stale; the first connect does not.
func TestOnConnect_ReconnectMarksEmojiCacheStale(t *testing.T) {
	for _, tt := range []struct {
		name      string
		connected bool
		wantFresh bool
	}{
		{"first connect", false, true},
		{"reconnect", true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h, _, db := newEmojiHandler(t, nil)
			if err := db.ReplaceCustomEmoji("T1", map[string]string{"a": "u1"}, time.Now().Unix()); err != nil {
				t.Fatal(err)
			}
			h.connected = tt.connected
			h.OnConnect()
			if _, _, fresh := reloadEmojiCache(t, db); fresh != tt.wantFresh {
				t.Errorf("fresh after OnConnect = %v, want %v", fresh, tt.wantFresh)
			}
		})
	}
}

// A fetch begun before a disconnect may hold a list that missed events
// sent while the socket was down; after a reconnect it is fetched again
// rather than saved as fresh.
func TestOnConnect_ReconnectSupersedesAFetchInFlight(t *testing.T) {
	h, _, db := newEmojiHandler(t, map[string]string{"old": "u1"})
	h.connected = true
	newer := map[string]string{"old": "u1", "missed": "u2"}
	api := &gatedEmojiLister{
		results: []map[string]string{{"old": "u1"}, newer},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	done := make(chan struct{})
	go func() {
		fetchWorkspaceEmoji(context.Background(), h.wsCtx, api, db, h.program, "T1")
		close(done)
	}()
	<-api.started
	h.OnConnect()
	close(api.release)
	<-done

	if api.calls != 2 {
		t.Errorf("ListCustomEmoji calls = %d, want 2", api.calls)
	}
	if got, _ := db.CustomEmoji("T1"); !maps.Equal(got, newer) {
		t.Errorf("cache = %v, want %v", got, newer)
	}
}
