package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slack/membership"
	"github.com/gammons/slk/internal/ui"
)

// captureSender records every tea.Msg dispatched to it. Substituted
// for *tea.Program in tests via the teaSender interface.
type captureSender struct {
	mu   sync.Mutex
	sent []tea.Msg
}

func (c *captureSender) Send(msg tea.Msg) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, msg)
}

func newTestDB(t *testing.T) *cache.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := cache.New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.UpsertWorkspace(cache.Workspace{ID: "T1", Name: "T"}); err != nil {
		t.Fatalf("UpsertWorkspace: %v", err)
	}
	return db
}

func TestReconnect_DedupeWindow(t *testing.T) {
	gate := &dedupeGate{window: 30 * time.Second}

	if !gate.tryStart(time.Unix(1000, 0)) {
		t.Fatal("first call should be allowed")
	}
	if gate.tryStart(time.Unix(1010, 0)) {
		t.Error("second call within 30s should be blocked")
	}
	if !gate.tryStart(time.Unix(1031, 0)) {
		t.Error("call after window should be allowed")
	}
}

// fakeCounts implements the narrow client surface reconnectSync needs
// and records every call it receives, in order, under the name of the
// endpoint it maps to. The endpoint names match the ones
// slackhttp.Counter reports, so a count here is comparable with a
// count from a real session.
type fakeCounts struct {
	mu      sync.Mutex
	calls   []string
	unreads []slackclient.UnreadInfo
	err     error
}

func (f *fakeCounts) GetUnreadCounts() ([]slackclient.UnreadInfo, slackclient.ThreadsAggregate, error) {
	f.mu.Lock()
	f.calls = append(f.calls, "client.counts")
	f.mu.Unlock()
	if f.err != nil {
		return nil, slackclient.ThreadsAggregate{}, f.err
	}
	return f.unreads, slackclient.ThreadsAggregate{}, nil
}

func (f *fakeCounts) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// seedWorkspaceChannels writes n channels, each with one cached
// message and a non-zero synced_at. The messages matter: the sweep
// this task deletes drove itself from "channels with cached messages",
// so a workspace seeded this way is one the old code would have
// fanned out over.
func seedWorkspaceChannels(t *testing.T, db *cache.DB, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("C%03d", i)
		ids = append(ids, id)
		if err := db.UpsertChannel(cache.Channel{
			ID: id, WorkspaceID: "T1", Name: id, Type: "channel",
		}); err != nil {
			t.Fatalf("UpsertChannel(%s): %v", id, err)
		}
		if err := db.UpsertMessage(cache.Message{
			TS: "1700000000.000100", ChannelID: id, WorkspaceID: "T1",
			UserID: "U1", Text: "cached",
		}); err != nil {
			t.Fatalf("UpsertMessage(%s): %v", id, err)
		}
		if err := db.SetChannelSyncedAt(id, 1700000000); err != nil {
			t.Fatalf("SetChannelSyncedAt(%s): %v", id, err)
		}
	}
	return ids
}

// runReconnect drives one reconnect pass over a workspace of n
// channels and returns the ordered list of API calls it made.
func runReconnect(t *testing.T, n int) []string {
	t.Helper()
	db := newTestDB(t)
	ids := seedWorkspaceChannels(t, db, n)

	fc := &fakeCounts{}
	var refreshed []string
	r := &reconnectSync{
		client:        fc,
		db:            db,
		workspaceID:   "T1",
		program:       &captureSender{},
		activeChannel: func() string { return ids[0] },
		refreshChannel: func(_ context.Context, channelID string) {
			fc.mu.Lock()
			fc.calls = append(fc.calls, "conversations.history")
			fc.mu.Unlock()
			refreshed = append(refreshed, channelID)
		},
	}
	if err := r.run(context.Background()); err != nil {
		t.Fatalf("reconnectSync.run: %v", err)
	}
	if len(refreshed) != 1 || refreshed[0] != ids[0] {
		t.Errorf("refreshed = %v; want exactly the active channel %s", refreshed, ids[0])
	}
	return fc.snapshot()
}

func TestReconnect_IsO1NotOChannels(t *testing.T) {
	// Success criterion 2. Reconnect used to fetch
	// conversations.history for every channel slk had ever cached, and
	// it ran from OnConnect — so every laptop sleep, wifi change and
	// VPN flap replayed the whole sweep. That is the scraper signature
	// several times a day, not once at boot.
	small := runReconnect(t, 3)
	large := runReconnect(t, 300)

	if !reflect.DeepEqual(small, large) {
		t.Errorf("reconnect on 3 channels made %v; on 300 it made %v — the cost must not depend on how many channels exist", small, large)
	}
	if len(large) > 3 {
		t.Errorf("reconnect made %d calls (%v); the budget is a small constant", len(large), large)
	}
	want := []string{"client.counts", "conversations.history"}
	if !reflect.DeepEqual(large, want) {
		t.Errorf("reconnect calls = %v; want %v — one counts refresh for every channel's unread state, plus the one channel the user is actually looking at", large, want)
	}
}

func TestReconnect_ClientSurfaceCannotEnumerate(t *testing.T) {
	// The guard behind the guard. TestReconnect_IsO1NotOChannels can
	// only see calls the fake is asked to make; this fails the moment
	// someone widens the interface enough to make a sweep possible
	// again. ListThreadSubscriptions is named explicitly because it
	// was the most expensive thing slk did on reconnect — measured at
	// 132 seconds against the channel phase's 2.7, hitting its
	// 1000-item hard cap every time.
	iface := reflect.TypeOf((*reconnectClient)(nil)).Elem()
	if iface.NumMethod() != 1 {
		names := make([]string, 0, iface.NumMethod())
		for i := 0; i < iface.NumMethod(); i++ {
			names = append(names, iface.Method(i).Name)
		}
		t.Errorf("reconnectClient declares %v; want only GetUnreadCounts — anything per-channel or per-thread here is an O(n) reconnect waiting to happen", names)
	}
	if _, ok := iface.MethodByName("GetUnreadCounts"); !ok {
		t.Error("reconnectClient has no GetUnreadCounts; client.counts is the one call that refreshes unread state for the whole workspace")
	}
}

func TestReconnect_RefreshesReadStateFromCounts(t *testing.T) {
	// Measured 2026-08-01: after a 90-second outage the socket
	// delivered ~160 presence_change events and nothing else — no
	// missed message, no channel_marked. slk's OnConnect never called
	// client.counts, which is exactly why a message posted during the
	// outage never appeared. This is a user-visible fix, not only a
	// fingerprint change.
	db := newTestDB(t)
	seedWorkspaceChannels(t, db, 2)

	sender := &captureSender{}
	fc := &fakeCounts{unreads: []slackclient.UnreadInfo{
		{ChannelID: "C000", HasUnread: true, LastRead: "1700000005.000100"},
	}}
	r := &reconnectSync{
		client: fc, db: db, workspaceID: "T1", program: sender,
		activeChannel:  func() string { return "" },
		refreshChannel: func(context.Context, string) {},
	}
	if err := r.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	state, err := db.GetChannelReadState("C000")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if !state.HasUnread || state.LastReadTS != "1700000005.000100" {
		t.Errorf("read state = %+v; want the counts response written through", state)
	}

	sender.mu.Lock()
	defer sender.mu.Unlock()
	var sawReadState bool
	for _, m := range sender.sent {
		if _, ok := m.(ui.ReadStateChangedMsg); ok {
			sawReadState = true
		}
	}
	if !sawReadState {
		t.Errorf("no ReadStateChangedMsg sent (%d msgs); the sidebar would keep rendering the pre-outage unread state", len(sender.sent))
	}
}

func TestReconnect_MarksEveryOtherChannelStale(t *testing.T) {
	// The other half of O(1): the channels that are NOT refreshed must
	// end up looking stale, or the deletion silently becomes "slk
	// never catches those channels up at all".
	db := newTestDB(t)
	ids := seedWorkspaceChannels(t, db, 3)

	fc := &fakeCounts{}
	r := &reconnectSync{
		client: fc, db: db, workspaceID: "T1", program: &captureSender{},
		activeChannel:  func() string { return ids[1] },
		refreshChannel: func(context.Context, string) {},
	}
	if err := r.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if got := db.GetChannelSyncedAt(ids[0]); got != 0 {
		t.Errorf("%s synced_at = %d; want 0 so its next open refetches", ids[0], got)
	}
	if got := db.GetChannelSyncedAt(ids[2]); got != 0 {
		t.Errorf("%s synced_at = %d; want 0", ids[2], got)
	}
	if got := db.GetChannelSyncedAt(ids[1]); got == 0 {
		t.Errorf("%s synced_at = 0; the active channel was just refreshed and staling it makes the very next render refetch what it already has", ids[1])
	}
}

func TestReconnect_CountsFailureStillRefreshesTheActiveChannel(t *testing.T) {
	// Unread badges are cosmetic; the messages on screen are not. A
	// ratelimited client.counts must not cost the user their catch-up.
	db := newTestDB(t)
	ids := seedWorkspaceChannels(t, db, 2)

	var refreshed []string
	r := &reconnectSync{
		client:        &fakeCounts{err: errors.New("ratelimited")},
		db:            db,
		workspaceID:   "T1",
		program:       &captureSender{},
		activeChannel: func() string { return ids[0] },
		refreshChannel: func(_ context.Context, channelID string) {
			refreshed = append(refreshed, channelID)
		},
	}
	if err := r.run(context.Background()); err != nil {
		t.Fatalf("run: counts failure must not be fatal, got %v", err)
	}
	if len(refreshed) != 1 {
		t.Errorf("refreshed %v; want the active channel refreshed despite the counts failure", refreshed)
	}
}

// fakeMemberAPI implements membership.ConversationMemberAPI and
// records how many full fetches it was asked to perform.
type fakeMemberAPI struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeMemberAPI) GetUsersInConversation(_ context.Context, _ string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return []string{"U1"}, nil
}

func (f *fakeMemberAPI) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func newMembershipHandler(t *testing.T, active bool) (*rtmEventHandler, *fakeMemberAPI, *cache.DB) {
	t.Helper()
	db := newTestDB(t)
	if err := db.ReplaceChannelMembers("T1", "C1", []string{"U1"}, time.Now().Unix()); err != nil {
		t.Fatalf("ReplaceChannelMembers: %v", err)
	}
	api := &fakeMemberAPI{}
	mgr := membership.New("T1", api, db, nil, nil)
	h := &rtmEventHandler{
		wsCtx:           &WorkspaceContext{Membership: mgr},
		isActive:        func() bool { return active },
		activeChannelID: func() string { return "C1" },
	}
	return h, api, db
}

// TestOnConnectMembershipRefreshSkipsInactiveWorkspace pins the
// cross-workspace bug behind the 42-conversations.members session.
// Every workspace's handler reads the GLOBAL UI active channel via
// app.ActiveChannelID(); without an isActive gate, OnConnect ran
// ForceStale+EnsureFresh for that channel on EVERY workspace's
// connect. The workspaces that don't own the channel fail with
// channel_not_found, and a failing fetch leaves the cache stale, so
// each reconnect re-fired the call — an unbounded amplifier with zero
// user interaction. Measured live: both workspaces' OnConnect fetched
// the same DM id 0.6 s apart; the non-owner's failed.
func TestOnConnectMembershipRefreshSkipsInactiveWorkspace(t *testing.T) {
	h, api, db := newMembershipHandler(t, false)
	before, _, _ := db.GetChannelMembershipMeta("T1", "C1")

	h.refreshActiveMembership()
	time.Sleep(100 * time.Millisecond)

	if c := api.callCount(); c != 0 {
		t.Errorf("background workspace fetched membership %d times for a channel it does not own; want 0", c)
	}
	after, _, _ := db.GetChannelMembershipMeta("T1", "C1")
	if after != before {
		t.Errorf("ForceStale zeroed a background workspace's membership meta (%d -> %d); staleness must only be forced by the workspace on screen", before, after)
	}
}

func TestOnConnectMembershipRefreshRunsForActiveWorkspace(t *testing.T) {
	h, api, _ := newMembershipHandler(t, true)

	h.refreshActiveMembership()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && api.callCount() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if c := api.callCount(); c != 1 {
		t.Errorf("active workspace's membership refresh fetched %d times; want 1", c)
	}
}

func TestReconnect_NoActiveChannelMakesNoHistoryCall(t *testing.T) {
	// A workspace the user has not looked at has no channel worth
	// spending a request on; inventing one would put slk back above
	// the budget for every background workspace on every flap.
	db := newTestDB(t)
	seedWorkspaceChannels(t, db, 3)

	fc := &fakeCounts{}
	refreshes := 0
	r := &reconnectSync{
		client: fc, db: db, workspaceID: "T1", program: &captureSender{},
		activeChannel:  func() string { return "" },
		refreshChannel: func(context.Context, string) { refreshes++ },
	}
	if err := r.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if refreshes != 0 {
		t.Errorf("refreshChannel called %d times with no active channel; want 0", refreshes)
	}
	if got := fc.snapshot(); len(got) != 1 {
		t.Errorf("calls = %v; want only client.counts", got)
	}
}
