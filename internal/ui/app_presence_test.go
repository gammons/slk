// internal/ui/app_presence_test.go
//
// Phase 0 characterization tests for the presence/DND state mutations
// on App: applyOptimisticStatus (called by the presence menu and
// custom-snooze flow) and the StatusChangeMsg handler in Update
// (called by the WS event router). These pin behavior for the future
// presenceController extraction.
package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gammons/slk/internal/ui/peerstatus"
	"github.com/gammons/slk/internal/ui/presencemenu"
)

func TestApplyOptimisticStatusSetActive(t *testing.T) {
	a := NewApp()
	a.activeTeamID = "T1"
	// Seed: was previously away with DND on, just to be sure we don't
	// stomp DND fields with this action.
	a.presence.byTeam["T1"] = workspaceStatus{
		Presence:   "away",
		DNDEnabled: true,
		DNDEndTS:   time.Now().Add(5 * time.Minute),
	}

	a.presence.Apply(a.activeTeamID, presencemenu.ActionSetActive, 0)

	got := a.presence.byTeam["T1"]
	if got.Presence != "active" {
		t.Errorf("Presence: want %q, got %q", "active", got.Presence)
	}
	if !got.DNDEnabled {
		t.Error("ActionSetActive must not clear DNDEnabled")
	}
	if got.DNDEndTS.IsZero() {
		t.Error("ActionSetActive must not clear DNDEndTS")
	}
}

func TestApplyOptimisticStatusSetAway(t *testing.T) {
	a := NewApp()
	a.activeTeamID = "T1"
	a.presence.byTeam["T1"] = workspaceStatus{Presence: "active"}

	a.presence.Apply(a.activeTeamID, presencemenu.ActionSetAway, 0)

	if got := a.presence.byTeam["T1"].Presence; got != "away" {
		t.Errorf("Presence: want %q, got %q", "away", got)
	}
}

func TestApplyOptimisticStatusSnoozeSetsDND(t *testing.T) {
	a := NewApp()
	a.activeTeamID = "T1"
	a.presence.byTeam["T1"] = workspaceStatus{Presence: "active"}

	before := time.Now()
	a.presence.Apply(a.activeTeamID, presencemenu.ActionSnooze, 30)
	after := time.Now()

	got := a.presence.byTeam["T1"]
	if !got.DNDEnabled {
		t.Fatal("expected DNDEnabled=true after Snooze")
	}
	// DNDEndTS must be ~30m from now (loose bounds to handle test jitter).
	earliest := before.Add(30 * time.Minute)
	latest := after.Add(30 * time.Minute)
	if got.DNDEndTS.Before(earliest) || got.DNDEndTS.After(latest) {
		t.Errorf("DNDEndTS=%v out of expected window [%v..%v]",
			got.DNDEndTS, earliest, latest)
	}
	// Presence is unchanged by Snooze.
	if got.Presence != "active" {
		t.Errorf("Snooze must not change Presence; got %q", got.Presence)
	}
}

func TestApplyOptimisticStatusEndDNDClearsDND(t *testing.T) {
	a := NewApp()
	a.activeTeamID = "T1"
	a.presence.byTeam["T1"] = workspaceStatus{
		Presence:   "active",
		DNDEnabled: true,
		DNDEndTS:   time.Now().Add(10 * time.Minute),
	}

	a.presence.Apply(a.activeTeamID, presencemenu.ActionEndDND, 0)

	got := a.presence.byTeam["T1"]
	if got.DNDEnabled {
		t.Error("DNDEnabled must be false after ActionEndDND")
	}
	if !got.DNDEndTS.IsZero() {
		t.Errorf("DNDEndTS must be zero after ActionEndDND; got %v", got.DNDEndTS)
	}
	// Presence is unchanged by EndDND.
	if got.Presence != "active" {
		t.Errorf("EndDND must not change Presence; got %q", got.Presence)
	}
}

func TestApplyOptimisticStatusInitializesMissingTeamEntry(t *testing.T) {
	a := NewApp()
	a.activeTeamID = "T1"
	// statusByTeam["T1"] does not exist yet.

	a.presence.Apply(a.activeTeamID, presencemenu.ActionSetAway, 0)

	got, ok := a.presence.byTeam["T1"]
	if !ok {
		t.Fatal("expected entry for T1 to be created on first apply")
	}
	if got.Presence != "away" {
		t.Errorf("Presence: want %q, got %q", "away", got.Presence)
	}
}

// Self-status architecture, round 3 of review: an earlier draft of
// this feature cached "your own" custom status in a second,
// client-only store (workspaceStatus.StatusText/StatusEmoji) written
// only when Apply ran a WFH/Vacation/ClearStatus action. Review caught
// that this meant the status vanished on restart, never reflected an
// edit made from the real Slack client, and that a failed API call
// still displayed as if it had succeeded (nothing corrected it, since
// no event comes back to say otherwise). All three are fixed by
// routing self-status through the SAME peers store already used for
// everyone else's custom status: Slack's status/profile events carry
// no self-filter, so the current user's own status flows through
// UserStatusChangeMsg/SetPeers exactly like a teammate's does — restart
// persistence, external edits, and expiry (via the existing
// peerStatusTickMsg tick) all come for free. Apply() no longer writes
// anything for WFH/Vacation/ClearStatus; those three only forward to
// presenceSvc (the real Slack API call) and wait for the real event.

func TestCustomStatusReadsFromPeerStore(t *testing.T) {
	a := NewApp()
	a.currentUserID = "U1"

	_, _ = a.Update(UserStatusChangeMsg{
		UserID: "U1",
		Emoji:  ":house:",
		Text:   "Working from home",
	})

	text, emoji := a.presence.CustomStatus("U1")
	if text != "Working from home" {
		t.Errorf("CustomStatus text = %q, want %q", text, "Working from home")
	}
	if emoji == "" {
		t.Error("expected a non-empty display glyph")
	}
}

func TestCustomStatusEmptyWhenExpired(t *testing.T) {
	a := NewApp()
	a.currentUserID = "U1"

	_, _ = a.Update(UserStatusChangeMsg{
		UserID:  "U1",
		Emoji:   ":palm_tree:",
		Text:    "On vacation",
		Expires: time.Now().Add(-time.Minute),
	})

	text, emoji := a.presence.CustomStatus("U1")
	if text != "" || emoji != "" {
		t.Errorf("expected empty status once expired, got text=%q emoji=%q", text, emoji)
	}
}

func TestCustomStatusEmptyForUnknownUser(t *testing.T) {
	a := NewApp()

	text, emoji := a.presence.CustomStatus("no-such-user")
	if text != "" || emoji != "" {
		t.Errorf("expected empty status for unknown user, got text=%q emoji=%q", text, emoji)
	}
}

// TestSelfStatusChangeRefreshesStatusBar: a UserStatusChangeMsg for the
// current user — the echo of our own Ctrl+S pick, or an edit made from
// the real Slack client — must update the status bar.
func TestSelfStatusChangeRefreshesStatusBar(t *testing.T) {
	a := NewApp()
	a.currentUserID = "U1"

	_, _ = a.Update(UserStatusChangeMsg{
		UserID: "U1",
		Emoji:  ":house:",
		Text:   "Working from home",
	})

	if !strings.Contains(a.statusbar.View(120), "Working from home") {
		t.Error("status bar must show self custom status after UserStatusChangeMsg")
	}
}

// TestSelfStatusExpiryClearsStatusBar closes the gap explicitly called
// out as out-of-scope in the original draft: self-status now shares
// the peers store's existing expiry tick, so a status that reached its
// deadline clears from the status bar the same way a peer's does, with
// no separate mechanism needed.
func TestSelfStatusExpiryClearsStatusBar(t *testing.T) {
	a := NewApp()
	a.currentUserID = "U1"
	_, _ = a.Update(UserStatusChangeMsg{
		UserID:  "U1",
		Emoji:   ":house:",
		Text:    "Working from home",
		Expires: time.Now().Add(time.Millisecond),
	})
	time.Sleep(2 * time.Millisecond)

	_, _ = a.Update(peerStatusTickMsg{})

	if strings.Contains(a.statusbar.View(120), "Working from home") {
		t.Error("status bar must clear self custom status once its deadline passes")
	}
}

// TestWorkspaceSwitchSeedsSelfCustomStatus replaces the per-team cache
// this PR originally used (found buggy in review) with the
// per-workspace peers store SetPeers already replaces wholesale on
// every switch: each workspace's self status is independent and comes
// from the server-fed snapshot, not a client-only cache.
func TestWorkspaceSwitchSeedsSelfCustomStatus(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	a.currentUserID = "U1"
	a.presence.peers = map[string]peerstatus.Status{
		"U1": peerstatus.Status{}.WithStatus(":house:", "Working from home", time.Time{}),
	}
	a.statusbar.SetCustomStatus(a.presence.CustomStatus("U1"))

	_, _ = a.Update(WorkspaceSwitchedMsg{TeamID: "T2", TeamName: "beta", UserID: "U2"})

	if strings.Contains(a.statusbar.View(120), "Working from home") {
		t.Error("status bar still shows the previous workspace's self status after switching")
	}
}

func TestWorkspaceSwitchRoundTripRestoresSelfCustomStatus(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	a.currentUserID = "U1"

	_, _ = a.Update(WorkspaceSwitchedMsg{
		TeamID: "T1", TeamName: "acme", UserID: "U1",
		UserStatuses: map[string]peerstatus.Status{
			"U1": peerstatus.Status{}.WithStatus(":house:", "Working from home", time.Time{}),
		},
	})
	_, _ = a.Update(WorkspaceSwitchedMsg{
		TeamID: "T2", TeamName: "beta", UserID: "U2",
		UserStatuses: map[string]peerstatus.Status{
			"U2": peerstatus.Status{}.WithStatus(":palm_tree:", "On vacation", time.Time{}),
		},
	})
	_, _ = a.Update(WorkspaceSwitchedMsg{
		TeamID: "T1", TeamName: "acme", UserID: "U1",
		UserStatuses: map[string]peerstatus.Status{
			"U1": peerstatus.Status{}.WithStatus(":house:", "Working from home", time.Time{}),
		},
	})

	out := a.statusbar.View(120)
	if !strings.Contains(out, "Working from home") {
		t.Errorf("expected T1's self status restored on switch-back, got: %q", out)
	}
	if strings.Contains(out, "On vacation") {
		t.Errorf("T2's self status leaked into T1's status bar: %q", out)
	}
}

// TestInitialActiveActivationSeedsSelfCustomStatus pins the race found
// in review: workspaces connect concurrently, and router.Add happens
// for every one of them regardless of InitialActive — so a non-default
// workspace can finish connecting, be switched to, and have a status
// set on it before the slower, configured-default workspace's
// WorkspaceReadyMsg arrives and claims InitialActive. That claim must
// still refresh the status bar from the newly-active workspace's own
// seeded peers, not leave the previous workspace's status on screen.
func TestInitialActiveActivationSeedsSelfCustomStatus(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T-other"))
	a.currentUserID = "U-other"
	a.presence.peers = map[string]peerstatus.Status{
		"U-other": peerstatus.Status{}.WithStatus(":house:", "Working from home", time.Time{}),
	}
	a.statusbar.SetCustomStatus(a.presence.CustomStatus("U-other"))

	_, _ = a.Update(WorkspaceReadyMsg{
		TeamID:        "T-default",
		InitialActive: true,
		UserID:        "U-default",
	})

	if strings.Contains(a.statusbar.View(120), "Working from home") {
		t.Error("status bar still shows the previous workspace's self status after InitialActive activation")
	}
}

func TestStatusChangeMsgUpdatesPerTeamCache(t *testing.T) {
	a := NewApp()
	a.activeTeamID = "T1"
	dndEnd := time.Now().Add(1 * time.Hour)

	// Status arrives for a DIFFERENT workspace: must update the cache
	// but must NOT touch the active workspace's status bar.
	_, _ = a.Update(StatusChangeMsg{
		TeamID:     "T2",
		Presence:   "away",
		DNDEnabled: true,
		DNDEndTS:   dndEnd,
	})

	got, ok := a.presence.byTeam["T2"]
	if !ok {
		t.Fatal("expected statusByTeam[T2] to be set")
	}
	if got.Presence != "away" || !got.DNDEnabled || !got.DNDEndTS.Equal(dndEnd) {
		t.Errorf("T2 cache mismatch: got %+v", got)
	}
	// T1 (active) must not have been written.
	if _, exists := a.presence.byTeam["T1"]; exists {
		t.Error("active T1 entry should not be created by a T2 status change")
	}
}

func TestStatusChangeMsgForActiveTeamStartsDNDTickerOnce(t *testing.T) {
	a := NewApp()
	a.activeTeamID = "T1"
	dndEnd := time.Now().Add(1 * time.Hour)

	// First StatusChangeMsg for the active team with DND enabled in the
	// future flips dndTickerOn and schedules a tick.
	_, cmd := a.Update(StatusChangeMsg{
		TeamID:     "T1",
		Presence:   "away",
		DNDEnabled: true,
		DNDEndTS:   dndEnd,
	})
	if !a.presence.dndTickerOn {
		t.Fatal("expected dndTickerOn=true after first DND-active StatusChangeMsg")
	}
	if cmd == nil {
		t.Fatal("expected a tea.Cmd (the DNDTick scheduler) to be returned")
	}

	// A second identical StatusChangeMsg must NOT re-schedule (the
	// dndTickerOn guard prevents parallel tick chains).
	_, cmd2 := a.Update(StatusChangeMsg{
		TeamID:     "T1",
		Presence:   "away",
		DNDEnabled: true,
		DNDEndTS:   dndEnd,
	})
	if cmd2 != nil {
		t.Error("second DND-active StatusChangeMsg must not schedule another tick")
	}
}

func TestStatusChangeMsgExpiredDNDDoesNotStartTicker(t *testing.T) {
	a := NewApp()
	a.activeTeamID = "T1"
	// DNDEndTS already in the past → no tick should be scheduled even
	// though DNDEnabled is true (the renderer falls back to presence).
	_, cmd := a.Update(StatusChangeMsg{
		TeamID:     "T1",
		Presence:   "away",
		DNDEnabled: true,
		DNDEndTS:   time.Now().Add(-1 * time.Minute),
	})
	if a.presence.dndTickerOn {
		t.Error("ticker must not start for already-expired DND")
	}
	if cmd != nil {
		t.Error("expired DND must not return a tick cmd")
	}
}
