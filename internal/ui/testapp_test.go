package ui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/gammons/slk/internal/ui/wintree"
	"github.com/gammons/slk/internal/ui/workspace"
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
	a := newTestApp(t, withSize(200, 50), withMessages(testMessageItems(3)...))
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
	winSize       *[2]int
	msgs          []messages.MessageItem
	hasMsgs       bool
	channels      []sidebar.ChannelItem
	hasChannels   bool
	workspaces    []workspace.WorkspaceItem
	hasWorkspaces bool
	mode          Mode
	activeChannel string
	activeTeam    string
	render        bool
	chanSvc       *ChannelServiceFuncs
	splits        []wintree.Dir
	threadSums    []cache.ThreadSummary
	hasThreadSums bool
	finderItems   []channelfinder.Item
	openFinder    bool
	view          View
}

type testOpt func(*testAppCfg)

// withSize sets a.width/a.height directly. It does NOT send a
// tea.WindowSizeMsg, so sub-models (messagepane, sidebar, thread, ...)
// keep whatever dimensions NewApp gave them. That matches every legacy
// ad-hoc builder except the two in app_bench_test.go, which need the
// dimensions propagated and therefore use withWindowSize instead.
//
// withSize(0, 0) is meaningful and used deliberately: it reproduces
// NewApp's unsized state, which several builders relied on.
//
// Clears any pending withWindowSize so the two are genuinely last-wins
// (see withWindowSize).
func withSize(w, h int) testOpt {
	return func(c *testAppCfg) { c.w, c.h, c.winSize = w, h, nil }
}

// withWindowSize is withSize's counterpart for tests that need the real
// resize path: it leaves a.width/a.height at NewApp's zero and delivers a
// tea.WindowSizeMsg through Update instead.
//
// The difference is observable, so the two are not interchangeable.
// Update's handler (app.go:669) sets a.forceSixelRepaint only when the
// reported size differs from the current a.width/a.height, so pre-assigning
// the fields and *then* sending the message would leave forceSixelRepaint
// false. Sending it against the zero size sets it true, which is what the
// legacy bench builders did. withWindowSize therefore zeroes c.w/c.h.
//
// Last-wins with withSize, in argument order: each clears the other's
// state, so exactly one of the two sizing behaviours survives.
func withWindowSize(w, h int) testOpt {
	return func(c *testAppCfg) {
		c.w, c.h = 0, 0
		c.winSize = &[2]int{w, h}
	}
}

// withMessages, withChannels and withWorkspaces each record a separate
// "was this option passed" flag rather than testing len(...) > 0 in
// buildTestApp. The distinction matters: the underlying setters are not
// no-ops for an empty argument list. App.SetWorkspaces(nil) still bumps
// the workspace rail (app.go:1911-1913), and the messagepane/sidebar
// setters still clear whatever was there. withMessages() with no
// arguments therefore means "call SetMessages with nothing", which is
// not the same as never calling it — and that is what the legacy
// builders, which called the setters unconditionally, actually did.

func withMessages(msgs ...messages.MessageItem) testOpt {
	return func(c *testAppCfg) { c.msgs, c.hasMsgs = msgs, true }
}

func withChannels(items ...sidebar.ChannelItem) testOpt {
	return func(c *testAppCfg) { c.channels, c.hasChannels = items, true }
}

// withWorkspaces routes through App.SetWorkspaces, which fills the
// workspace rail, refreshes its unread counts, and mirrors the set into
// the workspace finder. Applied before withChannels, matching the order
// the legacy builders used.
func withWorkspaces(items ...workspace.WorkspaceItem) testOpt {
	return func(c *testAppCfg) { c.workspaces, c.hasWorkspaces = items, true }
}

func withMode(m Mode) testOpt { return func(c *testAppCfg) { c.mode = m } }

func withActiveChannel(id string) testOpt {
	return func(c *testAppCfg) { c.activeChannel = id }
}

// withActiveTeam sets a.activeTeamID. Plain field assignment, like
// withActiveChannel — the App has no SetActiveTeam.
func withActiveTeam(id string) testOpt {
	return func(c *testAppCfg) { c.activeTeam = id }
}

// withView sets a.view (ViewChannels / ViewThreads). Plain field
// assignment, applied before withRender so the render reflects it.
func withView(v View) testOpt { return func(c *testAppCfg) { c.view = v } }

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

// withChannelFinderOpen seeds the channel finder's item list and opens
// the overlay. It does NOT set the mode; pair it with
// withMode(ModeChannelFinder), which newTestApp applies afterwards.
// Open-before-SetMode is the order both legacy builders used.
//
// App.SetChannelFinderItems is a one-line forwarder to
// channelFinder.SetItems (app.go:2068), so builders that called either
// one converge here.
func withChannelFinderOpen(items ...channelfinder.Item) testOpt {
	return func(c *testAppCfg) { c.finderItems, c.openFinder = items, true }
}

func withThreadsView(sums []cache.ThreadSummary) testOpt {
	return func(c *testAppCfg) { c.threadSums, c.hasThreadSums = sums, true }
}

// newTestApp builds an App for tests. Every option only records intent
// into testAppCfg; the effects are then applied in one fixed sequence
// regardless of the order the options were passed:
//
//	size → workspaces → channels → channelService → messages →
//	threadsView → activeChannel → activeTeam → channelFinder →
//	splits → mode → view → render
//
// So withMessages(...) before or after withChannels(...) produces the
// same App. Individual options are still last-wins (a second withSize
// overwrites the first), and withWindowSplit is deliberately
// order-sensitive: it appends, so splits apply in argument order.
//
// Takes testing.TB rather than *testing.T so benchmarks can use it too
// (app_bench_test.go's builders route through this).
func newTestApp(t testing.TB, opts ...testOpt) *App {
	t.Helper()
	return buildTestApp(opts...)
}

// buildTestApp is newTestApp without the testing.TB. It exists for the
// two legacy builders that take no *testing.T and whose signatures must
// not change, because changing them would edit test bodies at every call
// site: newPanelAtApp (app_panelat_test.go) and sixelTestApp
// (sixelpaint_test.go). Prefer newTestApp everywhere else — the TB is
// there so a future assertion inside the builder reports at the caller's
// line.
func buildTestApp(opts ...testOpt) *App {
	cfg := testAppCfg{w: 120, h: 30, mode: ModeNormal, view: ViewChannels}
	for _, o := range opts {
		o(&cfg)
	}

	a := NewApp()
	a.width, a.height = cfg.w, cfg.h
	if cfg.winSize != nil {
		// Update has a pointer receiver and mutates a in place.
		_, _ = a.Update(tea.WindowSizeMsg{Width: cfg.winSize[0], Height: cfg.winSize[1]})
	}

	if cfg.hasWorkspaces {
		a.SetWorkspaces(cfg.workspaces)
	}
	if cfg.hasChannels {
		a.SetChannels(cfg.channels)
	}
	if cfg.chanSvc != nil {
		a.SetChannelService(NewChannelService(*cfg.chanSvc))
	}
	if cfg.hasMsgs {
		a.messagepane.SetMessages(cfg.msgs)
	}
	if cfg.hasThreadSums {
		a.threadsView.SetSummaries(cfg.threadSums)
	}
	if cfg.activeChannel != "" {
		a.activeChannelID = cfg.activeChannel
	}
	if cfg.activeTeam != "" {
		a.activeTeamID = cfg.activeTeam
	}
	if cfg.openFinder {
		a.SetChannelFinderItems(cfg.finderItems)
		a.channelFinder.Open()
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
	// Unconditional: NewApp already sets ViewChannels, which is also the
	// cfg default, so this is a no-op unless withView asked otherwise.
	a.view = cfg.view
	if cfg.render {
		_ = a.View()
	}
	return a
}

// testMessageItems builds n plain messages with distinct TS values and
// greppable text ("msg-1", "msg-2", ...). Deliberately minimal: no
// reactions, attachments, or date grouping. Tests that need those
// build their own items.
//
// This is the single copy: an identical body previously lived in
// fanout_test.go under this same name. Callers there are unchanged.
func testMessageItems(n int) []messages.MessageItem {
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
