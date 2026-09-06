package ui

import (
	"fmt"
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/gammons/slk/internal/ui/wintree"
)

func TestNewTestApp_Defaults(t *testing.T) {
	a := newTestApp(t)
	if a == nil {
		t.Fatal("newTestApp returned nil")
	}
	if a.width != 120 || a.height != 30 {
		t.Errorf("default size = %dx%d, want 120x30", a.width, a.height)
	}
	if a.mode != ModeNormal {
		t.Errorf("default mode = %v, want ModeNormal", a.mode)
	}
}

func TestNewTestApp_WithSizeAndMessages(t *testing.T) {
	a := newTestApp(t, withSize(200, 50), withMessages(sampleMessages(3)...))
	if a.width != 200 || a.height != 50 {
		t.Errorf("size = %dx%d, want 200x50", a.width, a.height)
	}
	if got := len(a.messagepane.Messages()); got != 3 {
		t.Errorf("message count = %d, want 3", got)
	}
}

func TestNewTestApp_WithRenderPopulatesLayout(t *testing.T) {
	a := newTestApp(t, withRender())
	if a.layout.sidebarEnd == 0 {
		t.Error("withRender did not populate layout bands")
	}
}

// The remaining options get one test each. Beyond proving they work,
// this keeps the `unused` linter quiet until Task 2 converts the 15
// ad-hoc builders onto them.

func TestNewTestApp_WithChannelsAndActiveChannel(t *testing.T) {
	a := newTestApp(t,
		withChannels(
			sidebar.ChannelItem{ID: "C1", Name: "general", Type: "channel"},
			sidebar.ChannelItem{ID: "C2", Name: "random", Type: "channel"},
		),
		withActiveChannel("C1"),
	)
	got := a.sidebar.Items()
	if len(got) != 2 {
		t.Fatalf("sidebar item count = %d, want 2", len(got))
	}
	// Identity, not just arity: a mis-wired option that populates the
	// sidebar with the right number of wrong items must fail here.
	if got[0].ID != "C1" || got[0].Name != "general" {
		t.Errorf("sidebar item[0] = {ID:%q Name:%q}, want {ID:\"C1\" Name:\"general\"}", got[0].ID, got[0].Name)
	}
	if got[1].ID != "C2" || got[1].Name != "random" {
		t.Errorf("sidebar item[1] = {ID:%q Name:%q}, want {ID:\"C2\" Name:\"random\"}", got[1].ID, got[1].Name)
	}
	if a.activeChannelID != "C1" {
		t.Errorf("activeChannelID = %q, want %q", a.activeChannelID, "C1")
	}
}

func TestNewTestApp_WithMode(t *testing.T) {
	a := newTestApp(t, withMode(ModeInsert))
	if a.mode != ModeInsert {
		t.Errorf("mode = %v, want ModeInsert", a.mode)
	}
}

func TestNewTestApp_WithChannelService(t *testing.T) {
	a := newTestApp(t, withChannelService(ChannelServiceFuncs{
		SyncedAt: func(ids.ChannelID) int64 { return 42 },
	}))
	if got := a.channels.SyncedAt("C1"); got != 42 {
		t.Errorf("SyncedAt = %d, want 42 (injected service not wired)", got)
	}
}

func TestNewTestApp_WithWindowSplit(t *testing.T) {
	a := newTestApp(t, withSize(200, 50), withWindowSplit(wintree.SplitSideBySide))
	if got := a.wins.Len(); got != 2 {
		t.Errorf("window count = %d, want 2", got)
	}
}

func TestNewTestApp_WithThreadsView(t *testing.T) {
	a := newTestApp(t, withThreadsView([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "1.0", ReplyCount: 2},
	}))
	got := a.threadsView.Summaries()
	if len(got) != 1 {
		t.Fatalf("thread summary count = %d, want 1", len(got))
	}
	// Identity, not just arity: the summary must be the one we passed.
	if got[0].ChannelID != "C1" || got[0].ThreadTS != "1.0" || got[0].ReplyCount != 2 {
		t.Errorf("summary[0] = {ChannelID:%q ThreadTS:%q ReplyCount:%d}, want {\"C1\" \"1.0\" 2}",
			got[0].ChannelID, got[0].ThreadTS, got[0].ReplyCount)
	}
}

// testAppCfg is the accumulated configuration a testOpt mutates.
// Zero value is the default App: 120x30, Normal mode, no data.
type testAppCfg struct {
	w, h          int
	msgs          []messages.MessageItem
	channels      []sidebar.ChannelItem
	mode          Mode
	activeChannel string
	render        bool
	chanSvc       *ChannelServiceFuncs
	splits        []wintree.Dir
	threadSums    []cache.ThreadSummary
	hasThreadSums bool
}

type testOpt func(*testAppCfg)

// withSize sets a.width/a.height directly. It does NOT send a
// tea.WindowSizeMsg, so sub-models (messagepane, sidebar, thread, ...)
// keep whatever dimensions NewApp gave them. That matches most of the
// legacy ad-hoc builders, but differs from makeBenchApp in
// app_bench_test.go, which sizes via Update(tea.WindowSizeMsg{...}) and
// therefore does propagate. A test that needs propagation must send the
// message itself after construction:
//
//	a := newTestApp(t, withSize(200, 50))
//	_, _ = a.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
//
// (Update has a pointer receiver and mutates a in place, so the
// returned tea.Model can be discarded.)
func withSize(w, h int) testOpt { return func(c *testAppCfg) { c.w, c.h = w, h } }

func withMessages(msgs ...messages.MessageItem) testOpt {
	return func(c *testAppCfg) { c.msgs = msgs }
}

func withChannels(items ...sidebar.ChannelItem) testOpt {
	return func(c *testAppCfg) { c.channels = items }
}

func withMode(m Mode) testOpt { return func(c *testAppCfg) { c.mode = m } }

func withActiveChannel(id string) testOpt {
	return func(c *testAppCfg) { c.activeChannel = id }
}

// withRender calls View() once so a.layout bands and pane caches are
// populated. Required by any test that does mouse hit-testing.
func withRender() testOpt { return func(c *testAppCfg) { c.render = true } }

func withChannelService(f ChannelServiceFuncs) testOpt {
	return func(c *testAppCfg) { c.chanSvc = &f }
}

// withWindowSplit splits the window tree once per call, in order.
func withWindowSplit(dir wintree.Dir) testOpt {
	return func(c *testAppCfg) { c.splits = append(c.splits, dir) }
}

func withThreadsView(sums []cache.ThreadSummary) testOpt {
	return func(c *testAppCfg) { c.threadSums, c.hasThreadSums = sums, true }
}

// newTestApp builds an App for tests. Every option only records intent
// into testAppCfg; the effects are then applied in one fixed sequence
// regardless of the order the options were passed:
//
//	size → channels → channelService → messages → threadsView →
//	activeChannel → splits → mode → render
//
// So withMessages(...) before or after withChannels(...) produces the
// same App. Individual options are still last-wins (a second withSize
// overwrites the first), and withWindowSplit is deliberately
// order-sensitive: it appends, so splits apply in argument order.
//
// Takes testing.TB rather than *testing.T so benchmarks can use it too
// (app_bench_test.go's builders route through this in Task 2).
func newTestApp(t testing.TB, opts ...testOpt) *App {
	t.Helper()
	cfg := testAppCfg{w: 120, h: 30, mode: ModeNormal}
	for _, o := range opts {
		o(&cfg)
	}

	a := NewApp()
	a.width, a.height = cfg.w, cfg.h

	if len(cfg.channels) > 0 {
		a.SetChannels(cfg.channels)
	}
	if cfg.chanSvc != nil {
		a.SetChannelService(NewChannelService(*cfg.chanSvc))
	}
	if len(cfg.msgs) > 0 {
		a.messagepane.SetMessages(cfg.msgs)
	}
	if cfg.hasThreadSums {
		a.threadsView.SetSummaries(cfg.threadSums)
	}
	if cfg.activeChannel != "" {
		a.activeChannelID = cfg.activeChannel
	}
	for _, d := range cfg.splits {
		_ = a.splitWindow(d)
	}
	// withMode(ModeNormal) is deliberately a no-op: NewApp already starts
	// in ModeNormal, and SetMode is not a plain field assignment (see
	// app.go) — it disarms a pending ctrl+w chord and restores the help
	// hint, clears a.cmdline when leaving ModeCommand, clears selections
	// when entering ModeInsert, and always pushes the mode into the
	// statusbar. Firing those on a freshly built App would contaminate
	// the precondition the test is trying to establish, so we skip the
	// call when the requested mode is already the default.
	//
	// Consequence for callers: passing a Mode variable that happens to
	// equal ModeNormal gets you nothing. A test that genuinely wants
	// SetMode's side effects must call it itself after construction:
	//
	//	a := newTestApp(t)
	//	a.SetMode(ModeNormal)
	if cfg.mode != ModeNormal {
		a.SetMode(cfg.mode)
	}
	if cfg.render {
		_ = a.View()
	}
	return a
}

// sampleMessages builds n plain messages with distinct TS values and
// greppable text ("msg-1", "msg-2", ...). Deliberately minimal: no
// reactions, attachments, or date grouping. Tests that need those
// build their own items.
func sampleMessages(n int) []messages.MessageItem {
	out := make([]messages.MessageItem, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, messages.MessageItem{
			TS:        fmt.Sprintf("%d.0", i),
			UserID:    "U1",
			UserName:  "alice",
			Text:      fmt.Sprintf("msg-%d", i),
			Timestamp: "1:00 PM",
		})
	}
	return out
}
