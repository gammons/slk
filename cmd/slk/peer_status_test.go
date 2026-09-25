package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/gammons/slk/internal/cache"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slack/edge"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/peerstatus"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/slack-go/slack"
)

const peerStatusTestNow = int64(1700000000)

// newTestPeerStatus builds a refresher whose users/info fake gives every
// user a meeting status and whose dnd.teamInfo fake snoozes U1 for ten
// minutes. The window is an hour so tests flush by hand.
func newTestPeerStatus(known ...string) (p *peerStatusRefresher, sender *captureSender, resolved, dndAsked *[][]string) {
	sender = &captureSender{}
	resolved, dndAsked = &[][]string{}, &[][]string{}
	knownSet := make(map[string]bool, len(known))
	for _, id := range known {
		knownSet[id] = true
	}
	p = newPeerStatusRefresher("T1",
		"USELF",
		func(id string) bool { return knownSet[id] },
		func(ids []string) []edge.User {
			*resolved = append(*resolved, sortedIDs(ids))
			out := make([]edge.User, 0, len(ids))
			for _, id := range ids {
				var u edge.User
				u.ID = id
				u.Profile.StatusEmoji = ":calendar:"
				u.Profile.StatusText = "In a meeting"
				u.Profile.StatusExpiration = peerStatusTestNow + 3600
				u.Profile.HuddleState = "in_a_huddle"
				u.Profile.HuddleStateExpirationTS = peerStatusTestNow + 900
				out = append(out, u)
				// Mirrors applyEdgeUser, which the real ResolveNow
				// calls per returned user: the resolver itself emits
				// the status change, not flush.
				sender.Send(ui.UserStatusChangeMsg{
					TeamID:        "T1",
					UserID:        id,
					Emoji:         u.Profile.StatusEmoji,
					Text:          u.Profile.StatusText,
					Expires:       statusExpiry(u.Profile.StatusExpiration),
					Huddle:        u.Profile.HuddleState,
					HuddleExpires: statusExpiry(u.Profile.HuddleStateExpirationTS),
				})
			}
			return out
		},
		func(_ context.Context, ids []string) (map[string]slack.DNDStatus, error) {
			*dndAsked = append(*dndAsked, sortedIDs(ids))
			out := make(map[string]slack.DNDStatus, len(ids))
			for _, id := range ids {
				var st slack.DNDStatus
				if id == "U1" {
					st.SnoozeEnabled = true
					st.SnoozeEndTime = int(peerStatusTestNow + 600)
				}
				out[id] = st
			}
			return out, nil
		},
		func(id string) (*slack.User, error) {
			return nil, errors.New("newTestPeerStatus: no fallback profile fixture; tests exercising it build their own refresher")
		},
		nil, // db; tests exercising the fallback path build their own refresher
		sender.Send)
	p.window = time.Hour
	p.now = func() time.Time { return time.Unix(peerStatusTestNow, 0) }
	return p, sender, resolved, dndAsked
}

func sortedIDs(ids []string) []string {
	s := slices.Clone(ids)
	slices.Sort(s)
	return s
}

func statusMsgs(c *captureSender) []ui.UserStatusChangeMsg {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []ui.UserStatusChangeMsg
	for _, m := range c.sent {
		if s, ok := m.(ui.UserStatusChangeMsg); ok {
			out = append(out, s)
		}
	}
	return out
}

func dndMsgs(c *captureSender) []ui.UserDNDChangeMsg {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []ui.UserDNDChangeMsg
	for _, m := range c.sent {
		if d, ok := m.(ui.UserDNDChangeMsg); ok {
			out = append(out, d)
		}
	}
	return out
}

func TestPeerStatus_CoalescesKnownPeersIntoOneRefetch(t *testing.T) {
	p, sender, resolved, _ := newTestPeerStatus("U1", "U2")
	p.InvalidateUser("U1")
	p.InvalidateUser("U2")
	p.InvalidateUser("U1")
	p.InvalidateUser("USELF")   // own changes arrive as user_change, with the state
	p.InvalidateUser("UNKNOWN") // left to first-sight resolution if it later appears
	p.InvalidateUser("")
	p.timer.Stop()
	p.flush()

	if want := [][]string{{"U1", "U2"}}; !slices.EqualFunc(*resolved, want, slices.Equal) {
		t.Fatalf("users/info refetches = %v; want %v", *resolved, want)
	}
	got := statusMsgs(sender)
	if len(got) != 2 {
		t.Fatalf("got %d status msgs; want 2", len(got))
	}
	for _, m := range got {
		if m.Emoji != ":calendar:" || m.Text != "In a meeting" || !m.Expires.Equal(time.Unix(peerStatusTestNow+3600, 0)) {
			t.Errorf("status msg = %+v; want the refetched meeting status", m)
		}
		if m.Huddle != "in_a_huddle" || !m.HuddleExpires.Equal(time.Unix(peerStatusTestNow+900, 0)) {
			t.Errorf("status msg = %+v; want the refetched huddle state", m)
		}
	}
}

// newFallbackTestPeerStatus builds a refresher backed by a real cache DB
// (so fallback writes can be asserted) with U1 and U2 pre-seeded, plus
// a recorder for the per-user fallback calls it receives. resolveEdge
// controls which ids the edge batch "returns"; it may use sender to
// mirror applyEdgeUser's own emit for ids it resolves.
func newFallbackTestPeerStatus(t *testing.T, sender *captureSender, resolveEdge func(ids []string) []edge.User) (
	p *peerStatusRefresher, db *cache.DB, profileCalls *[]string,
) {
	t.Helper()
	db = newTestDB(t)
	if err := db.UpsertWorkspace(cache.Workspace{ID: "T1", Name: "T1"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"U1", "U2"} {
		if err := db.UpsertUser(cache.User{ID: id, WorkspaceID: "T1", Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	profileCalls = &[]string{}
	p = newPeerStatusRefresher("T1", "USELF",
		func(string) bool { return true },
		resolveEdge,
		func(context.Context, []string) (map[string]slack.DNDStatus, error) { return nil, nil },
		func(id string) (*slack.User, error) {
			*profileCalls = append(*profileCalls, id)
			u := &slack.User{ID: id}
			u.Profile.StatusEmoji = ":palm_tree:"
			u.Profile.StatusText = "Vacation (fallback)"
			return u, nil
		},
		db,
		sender.Send,
	)
	p.window = time.Hour
	p.now = func() time.Time { return time.Unix(peerStatusTestNow, 0) }
	return p, db, profileCalls
}

// TestPeerStatus_FlushFallsBackWhenEdgeReturnsNil: when resolveNow
// returns nil, every queued id is refreshed through the per-user
// users.info fallback, each exactly once.
func TestPeerStatus_FlushFallsBackWhenEdgeReturnsNil(t *testing.T) {
	sender := &captureSender{}
	p, db, profileCalls := newFallbackTestPeerStatus(t, sender, func(ids []string) []edge.User { return nil })
	p.InvalidateUser("U1")
	p.InvalidateUser("U2")
	p.timer.Stop()
	p.flush()

	if got := sortedIDs(*profileCalls); !slices.Equal(got, []string{"U1", "U2"}) {
		t.Fatalf("fallback profile calls = %v; want both ids", got)
	}
	got := statusMsgs(sender)
	byUser := map[string]int{}
	for _, m := range got {
		byUser[m.UserID]++
		if m.Emoji != ":palm_tree:" || m.Text != "Vacation (fallback)" {
			t.Errorf("status msg = %+v; want the fallback profile's status", m)
		}
	}
	if byUser["U1"] != 1 || byUser["U2"] != 1 {
		t.Fatalf("status msg counts = %v; want exactly one each for U1 and U2", byUser)
	}
	for _, id := range []string{"U1", "U2"} {
		u, err := db.GetUser(id)
		if err != nil {
			t.Fatal(err)
		}
		if u.StatusEmoji != ":palm_tree:" || u.StatusText != "Vacation (fallback)" {
			t.Errorf("%s cache row not updated by the fallback: %+v", id, u)
		}
	}
}

// TestPeerStatus_FlushFallsBackOnlyForIDsEdgeMissed: edge resolves U1
// but omits U2, so only U2 takes the per-user fallback, and each user
// gets exactly one status message.
func TestPeerStatus_FlushFallsBackOnlyForIDsEdgeMissed(t *testing.T) {
	sender := &captureSender{}
	p, db, profileCalls := newFallbackTestPeerStatus(t, sender, func(ids []string) []edge.User {
		var out []edge.User
		for _, id := range ids {
			if id != "U1" {
				continue
			}
			var u edge.User
			u.ID = id
			u.Profile.StatusEmoji = ":calendar:"
			u.Profile.StatusText = "In a meeting"
			out = append(out, u)
			// Mirrors applyEdgeUser's own emit for the id edge did
			// resolve.
			sender.Send(ui.UserStatusChangeMsg{TeamID: "T1", UserID: id, Emoji: u.Profile.StatusEmoji, Text: u.Profile.StatusText})
		}
		return out
	})
	p.InvalidateUser("U1")
	p.InvalidateUser("U2")
	p.timer.Stop()
	p.flush()

	if got := *profileCalls; !slices.Equal(got, []string{"U2"}) {
		t.Fatalf("fallback profile calls = %v; want [U2] only", got)
	}
	got := statusMsgs(sender)
	byUser := map[string]ui.UserStatusChangeMsg{}
	counts := map[string]int{}
	for _, m := range got {
		byUser[m.UserID] = m
		counts[m.UserID]++
	}
	if counts["U1"] != 1 || counts["U2"] != 1 {
		t.Fatalf("status msg counts = %v; want exactly one each for U1 and U2", counts)
	}
	if m := byUser["U1"]; m.Emoji != ":calendar:" {
		t.Errorf("U1 status = %+v; want the edge-resolved status, not overwritten by fallback", m)
	}
	if m := byUser["U2"]; m.Emoji != ":palm_tree:" {
		t.Errorf("U2 status = %+v; want the fallback profile's status", m)
	}
	if u, err := db.GetUser("U2"); err != nil || u.StatusEmoji != ":palm_tree:" {
		t.Errorf("U2 cache row = %+v, err=%v; want fallback status persisted", u, err)
	}
}

func TestPeerStatus_TimerArmsOncePerWindow(t *testing.T) {
	p, _, _, _ := newTestPeerStatus("U1", "U2")
	p.InvalidateUser("U1")
	first := p.timer
	p.InvalidateDND("U2")
	if first == nil || p.timer != first {
		t.Error("a second invalidation inside the window must join the armed flush, not arm another")
	}
	p.timer.Stop()
}

func TestPeerStatus_DNDRefetchReportsEachUser(t *testing.T) {
	p, sender, _, asked := newTestPeerStatus("U1", "U2")
	p.InvalidateDND("U1")
	p.InvalidateDND("U2")
	p.InvalidateDND("USELF")
	p.timer.Stop()
	p.flush()

	if want := [][]string{{"U1", "U2"}}; !slices.EqualFunc(*asked, want, slices.Equal) {
		t.Fatalf("dnd.teamInfo calls = %v; want %v", *asked, want)
	}
	byUser := map[string]ui.UserDNDChangeMsg{}
	for _, m := range dndMsgs(sender) {
		byUser[m.UserID] = m
	}
	if m := byUser["U1"]; !m.Enabled || !m.EndTS.Equal(time.Unix(peerStatusTestNow+600, 0)) {
		t.Errorf("U1 = %+v; want DND until the snooze end", m)
	}
	if m, ok := byUser["U2"]; !ok || m.Enabled || !m.EndTS.IsZero() {
		t.Errorf("U2 = %+v (reported=%v); want reported as not in DND", m, ok)
	}
}

func TestPeerStatus_RefreshDNDSkipsSelfAndSurvivesErrors(t *testing.T) {
	p, sender, _, asked := newTestPeerStatus()
	p.RefreshDND(context.Background(), []string{"USELF", ""})
	if len(*asked) != 0 {
		t.Errorf("asked dnd.teamInfo about %v; self and empty ids must send nothing", *asked)
	}

	p.dndTeamInfo = func(context.Context, []string) (map[string]slack.DNDStatus, error) {
		return nil, errors.New("rate limited")
	}
	p.RefreshDND(context.Background(), []string{"U1"})
	if n := len(dndMsgs(sender)); n != 0 {
		t.Errorf("a failed dnd.teamInfo reported %d DND states; want none", n)
	}
}

func TestPeerStatus_FetchDNDReturnsStatesWithoutSending(t *testing.T) {
	p, sender, _, _ := newTestPeerStatus()
	got := p.FetchDND(context.Background(), []string{"U1", "U2", "USELF"})
	if len(got) != 2 {
		t.Fatalf("FetchDND = %v; want U1 and U2", got)
	}
	if d := got["U1"]; !d.Enabled || !d.EndTS.Equal(time.Unix(peerStatusTestNow+600, 0)) {
		t.Errorf("U1 = %+v", d)
	}
	if n := len(dndMsgs(sender)); n != 0 {
		t.Errorf("FetchDND sent %d messages; it must only return them", n)
	}
}

func TestPeerStatus_FetchDNDBatchesAtSlackLimit(t *testing.T) {
	p, _, _, asked := newTestPeerStatus()
	const users = 51
	ids := make([]string, users)
	for i := range ids {
		ids[i] = fmt.Sprintf("U%02d", i)
	}
	got := p.FetchDND(context.Background(), append(ids, ids[0], "USELF", ""))

	if len(*asked) != 2 {
		t.Fatalf("dnd.teamInfo calls = %d; want 2 for 51 users", len(*asked))
	}
	if len((*asked)[0]) != 50 || len((*asked)[1]) != 1 {
		t.Fatalf("dnd.teamInfo batch sizes = %d, %d; want 50, 1", len((*asked)[0]), len((*asked)[1]))
	}
	if len(got) != len(ids) {
		t.Fatalf("merged DND states = %d; want %d", len(got), len(ids))
	}
}

// TestPeerStatus_FetchDNDKeepsEarlierBatchesWhenALaterOneFails: a
// failed batch's users are absent from the result; it does not drop
// states another batch already returned.
func TestPeerStatus_FetchDNDKeepsEarlierBatchesWhenALaterOneFails(t *testing.T) {
	p, _, _, asked := newTestPeerStatus()
	const users = 51
	ids := make([]string, users)
	for i := range ids {
		ids[i] = fmt.Sprintf("U%02d", i)
	}
	p.dndTeamInfo = func(_ context.Context, batchIDs []string) (map[string]slack.DNDStatus, error) {
		*asked = append(*asked, sortedIDs(batchIDs))
		if len(*asked) == 2 {
			return nil, errors.New("ratelimited")
		}
		out := make(map[string]slack.DNDStatus, len(batchIDs))
		for _, id := range batchIDs {
			out[id] = slack.DNDStatus{}
		}
		return out, nil
	}

	got := p.FetchDND(context.Background(), ids)

	if len(*asked) != 2 {
		t.Fatalf("dnd.teamInfo calls = %d; want 2 for 51 users", len(*asked))
	}
	if len(got) != 50 {
		t.Fatalf("FetchDND = %d states; want the first batch's 50 kept despite the second failing", len(got))
	}
	for _, id := range ids[:50] {
		if _, ok := got[id]; !ok {
			t.Errorf("missing %s from the first (successful) batch's states", id)
		}
	}
	if _, ok := got[ids[50]]; ok {
		t.Errorf("%s belongs to the failed batch; want it absent, not present", ids[50])
	}
}

// TestPeerStatus_FlushRefreshesDNDBeforeProfileFallback: a burst's DND
// is fetched before the per-user users.info fallback, which can be slow.
func TestPeerStatus_FlushRefreshesDNDBeforeProfileFallback(t *testing.T) {
	p, _, _, dndAsked := newTestPeerStatus("U1", "U2")
	p.resolveNow = func([]string) []edge.User { return nil }
	var dndAlreadyAsked []bool
	p.getUserProfile = func(string) (*slack.User, error) {
		dndAlreadyAsked = append(dndAlreadyAsked, len(*dndAsked) > 0)
		return nil, errors.New("unavailable")
	}
	p.InvalidateUser("U1")
	p.InvalidateDND("U2")
	p.timer.Stop()
	p.flush()

	if !slices.Equal(dndAlreadyAsked, []bool{true}) {
		t.Fatalf("DND asked before each profile fallback = %v; want [true]", dndAlreadyAsked)
	}
}

func TestCachedPeerStatuses(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertWorkspace(cache.Workspace{ID: "T1", Name: "T1"}); err != nil {
		t.Fatal(err)
	}
	for _, u := range []cache.User{
		{ID: "U1", WorkspaceID: "T1", Name: "alice", StatusEmoji: ":calendar:", StatusText: "In a meeting", StatusExpiration: peerStatusTestNow},
		{ID: "U2", WorkspaceID: "T1", Name: "bob"},
	} {
		if err := db.UpsertUser(u); err != nil {
			t.Fatal(err)
		}
	}
	got := cachedPeerStatuses(db, "T1")
	if len(got) != 1 {
		t.Fatalf("cachedPeerStatuses = %v; want U1 only", got)
	}
	if st := got["U1"]; st.Emoji != ":calendar:" || st.Text != "In a meeting" || !st.Expires.Equal(time.Unix(peerStatusTestNow, 0)) {
		t.Errorf("U1 = %+v", st)
	}
	if got := cachedPeerStatuses(nil, "T1"); got == nil {
		t.Error("cachedPeerStatuses(nil) returned nil; callers add to the map")
	}
}

func TestWithPeerStatuses_AppliesToDMsAndLeavesInputAlone(t *testing.T) {
	stale := peerstatus.Status{Emoji: ":old:"}
	channels := []sidebar.ChannelItem{
		{ID: "D1", Type: "dm", DMUserID: "U1", Status: stale},
		{ID: "D2", Type: "dm", DMUserID: "U2", Status: stale},
		{ID: "C1", Type: "channel"},
	}
	fresh := peerstatus.Status{Emoji: ":calendar:", DND: true}

	got := withPeerStatuses(channels, map[string]peerstatus.Status{"U1": fresh})

	if got[0].Status != fresh {
		t.Errorf("D1 not given the fresh status: %+v", got[0].Status)
	}
	if got[1].Status != (peerstatus.Status{}) {
		t.Errorf("D2 kept a status statuses no longer holds: %+v", got[1].Status)
	}
	if got[2].Status != (peerstatus.Status{}) {
		t.Errorf("non-DM row got a status: %+v", got[2].Status)
	}
	if channels[0].Status != stale {
		t.Error("withPeerStatuses modified its input, which is shared with another goroutine")
	}
}

// Slack does not reliably send user_invalidated when a huddle ends, so
// users seen in a huddle are refetched until a result says they left.
func TestPeerStatus_RechecksHuddlersUntilTheHuddleEnds(t *testing.T) {
	p, _, resolved, _ := newTestPeerStatus("U1", "U2")
	p.huddleInterval = time.Hour
	state := map[string]string{"U1": "in_a_huddle", "U2": "default_unset"}
	p.resolveNow = func(ids []string) []edge.User {
		*resolved = append(*resolved, sortedIDs(ids))
		var out []edge.User
		for _, id := range ids {
			var u edge.User
			u.ID = id
			u.Profile.HuddleState = state[id]
			out = append(out, u)
		}
		return out
	}

	p.InvalidateUser("U1")
	p.InvalidateUser("U2")
	p.timer.Stop()
	p.flush()
	if _, ok := p.huddlers["U1"]; !ok || len(p.huddlers) != 1 {
		t.Fatalf("huddlers = %v; want only U1", p.huddlers)
	}
	if p.huddleTimer == nil {
		t.Fatal("recheck not armed while a user is in a huddle")
	}
	p.huddleTimer.Stop()

	// The huddle ends without any event; the recheck finds out.
	state["U1"] = "default_unset"
	p.recheckHuddles()
	p.timer.Stop()
	p.huddleTimer.Stop()
	p.flush()

	if want := [][]string{{"U1", "U2"}, {"U1"}}; !slices.EqualFunc(*resolved, want, slices.Equal) {
		t.Errorf("refetches = %v; want %v", *resolved, want)
	}
	if len(p.huddlers) != 0 {
		t.Errorf("huddlers = %v; want empty once the refetch reports the huddle ended", p.huddlers)
	}

	// With nobody left, a stale timer firing requests nothing and stops.
	before := len(*resolved)
	p.recheckHuddles()
	if p.timer != nil || p.huddleTimer != nil || len(*resolved) != before {
		t.Error("recheck with no huddlers queued work or re-armed")
	}
}

func TestPeerStatus_SeedHuddlesPicksInHuddlePeersOnly(t *testing.T) {
	p, _, _, _ := newTestPeerStatus()
	p.huddleInterval = time.Hour
	p.SeedHuddles(map[string]peerstatus.Status{
		"U1":    {Huddle: "in_a_huddle"},
		"U2":    {Huddle: "default_unset", Emoji: ":calendar:"},
		"USELF": {Huddle: "in_a_huddle"},
	})
	if _, ok := p.huddlers["U1"]; !ok || len(p.huddlers) != 1 {
		t.Errorf("huddlers = %v; want only U1", p.huddlers)
	}
	if p.huddleTimer == nil {
		t.Error("seeding a huddler did not arm the recheck")
	} else {
		p.huddleTimer.Stop()
	}
}

func TestPeerStatus_NilRefresherDropsInvalidations(t *testing.T) {
	var p *peerStatusRefresher
	p.InvalidateUser("U1")
	p.InvalidateDND("U1")
	p.SeedHuddles(map[string]peerstatus.Status{"U1": {Huddle: "in_a_huddle"}})
	p.RefreshDND(context.Background(), []string{"U1"})
}

func TestStatusExpiry(t *testing.T) {
	if got := statusExpiry(0); !got.IsZero() {
		t.Errorf("statusExpiry(0) = %v; want zero, Slack's 0 means never expires", got)
	}
	if got := statusExpiry(peerStatusTestNow); !got.Equal(time.Unix(peerStatusTestNow, 0)) {
		t.Errorf("statusExpiry(%d) = %v", peerStatusTestNow, got)
	}
}

func TestRTMEventHandler_OnUserStatusChangeWritesCache(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertWorkspace(cache.Workspace{ID: "T1", Name: "T1"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertUser(cache.User{ID: "U1", WorkspaceID: "T1", Name: "alice"}); err != nil {
		t.Fatal(err)
	}
	h := &rtmEventHandler{db: db}
	h.OnUserStatusChange("U1", slackclient.UserStatus{
		Emoji: ":palm_tree:", Text: "Vacation", Expiration: peerStatusTestNow,
		HuddleState: "in_a_huddle", HuddleExpiration: peerStatusTestNow + 60,
	})

	u, err := db.GetUser("U1")
	if err != nil {
		t.Fatal(err)
	}
	if u.StatusEmoji != ":palm_tree:" || u.StatusText != "Vacation" || u.StatusExpiration != peerStatusTestNow {
		t.Errorf("cached status = %q %q %d; want the event's", u.StatusEmoji, u.StatusText, u.StatusExpiration)
	}
	if u.HuddleState != "in_a_huddle" || u.HuddleExpiration != peerStatusTestNow+60 {
		t.Errorf("cached huddle = %q %d; want the event's", u.HuddleState, u.HuddleExpiration)
	}
}
