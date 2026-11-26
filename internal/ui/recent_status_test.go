package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
)

var testRecent = []core.RecentStatus{
	{Text: "On a break", Emoji: ":coffee:", Created: 1000, Expire: 1000 + 3600},
	{Text: "Heads down", Created: 2000},
}

// recentApp is an App whose presence service answers RecentStatuses
// with list/err and records custom status calls.
func recentApp(t *testing.T, list []core.RecentStatus, err error) (*App, *[]customStatusCall) {
	t.Helper()
	a := newTestApp(t, withActiveTeam("T1"))
	var calls []customStatusCall
	a.SetPresenceService(core.NewPresenceService(nil, nil,
		func(text, emoji string, expiration int64) {
			calls = append(calls, customStatusCall{text, emoji, expiration})
		},
		func(string) ([]core.RecentStatus, error) { return list, err },
	))
	return a, &calls
}

// openStatusMenu presses Ctrl+S and returns the fetch result it fired,
// undelivered, so a test can deliver it later as a slow fetch would.
func openStatusMenu(t *testing.T, a *App) RecentStatusesMsg {
	t.Helper()
	_, cmd := a.Update(keyMod('s', tea.ModCtrl))
	for _, m := range drainBatch(cmd) {
		if r, ok := m.(RecentStatusesMsg); ok {
			return r
		}
	}
	t.Fatal("Ctrl+S fired no recent-status fetch")
	return RecentStatusesMsg{}
}

func statusMenuBox(a *App) string { return a.presenceMenu.ViewOverlay(120, 40, "") }

func statusMenuView(a *App) string { return stripANSI(statusMenuBox(a)) }

// menuHighlighted is the Ctrl+S menu's highlighted row. The filter
// input also starts with the ▌ marker, so the first marked line is skipped.
func menuHighlighted(t *testing.T, a *App) string {
	t.Helper()
	seen := false
	for _, line := range strings.Split(statusMenuView(a), "\n") {
		if !strings.Contains(line, "\u258c") {
			continue
		}
		if seen {
			return strings.TrimSpace(line)
		}
		seen = true
	}
	t.Fatal("no highlighted row")
	return ""
}

func TestRecentStatusesArriveWithoutMovingSelection(t *testing.T) {
	a, _ := recentApp(t, testRecent, nil)
	fetched := openStatusMenu(t, a)
	if v := statusMenuView(a); strings.Contains(v, "On a break") {
		t.Fatal("recent rows shown before the fetch landed")
	}
	for range 5 {
		pressKey(t, a, keyCode(tea.KeyDown))
	}
	if got := menuHighlighted(t, a); !strings.Contains(got, "Snooze for 20 minutes") {
		t.Fatalf("precondition: highlighted %q", got)
	}

	a.Update(fetched)

	v := statusMenuView(a)
	if !strings.Contains(v, "☕ On a break") || !strings.Contains(v, "Heads down") {
		t.Errorf("recent rows missing:\n%s", v)
	}
	if got := menuHighlighted(t, a); !strings.Contains(got, "Snooze for 20 minutes") {
		t.Errorf("after the rows arrived: highlighted %q, want it unmoved", got)
	}
	// The separator line is not a row: Up lands on the last recent one.
	pressKey(t, a, keyCode(tea.KeyUp))
	if got := menuHighlighted(t, a); !strings.Contains(got, "Heads down") {
		t.Errorf("Up from the first snooze row: highlighted %q, want Heads down", got)
	}
}

func TestRecentStatusReappliesWithSameLength(t *testing.T) {
	a, calls := recentApp(t, testRecent, nil)
	a.Update(openStatusMenu(t, a))
	typeAppKeys(t, a, "break")
	before := time.Now()
	pressKey(t, a, keyCode(tea.KeyEnter))
	after := time.Now()

	if a.mode != ModeNormal || len(*calls) != 1 {
		t.Fatalf("mode = %v calls = %v, want one call and the menu closed", a.mode, *calls)
	}
	c := (*calls)[0]
	if c.text != "On a break" || c.emoji != ":coffee:" {
		t.Errorf("set %q %q", c.text, c.emoji)
	}
	if lo, hi := before.Add(time.Hour).Unix(), after.Add(time.Hour).Unix(); c.expiration < lo || c.expiration > hi {
		t.Errorf("expiration = %d, want an hour from now (%d..%d)", c.expiration, lo, hi)
	}
}

func TestRecentStatusesCachedPerWorkspace(t *testing.T) {
	a, _ := recentApp(t, testRecent, nil)
	a.Update(openStatusMenu(t, a))
	pressKey(t, a, keyCode(tea.KeyEscape))

	openStatusMenu(t, a) // this fetch is not delivered
	if v := statusMenuView(a); !strings.Contains(v, "On a break") {
		t.Errorf("reopened menu should show the last list at once:\n%s", v)
	}
}

func TestRecentStatusesFromAnotherWorkspaceIgnored(t *testing.T) {
	a, _ := recentApp(t, nil, nil)
	openStatusMenu(t, a)
	a.Update(RecentStatusesMsg{Seq: a.recentStatusSeq, TeamID: "T2", Statuses: testRecent})
	if v := statusMenuView(a); strings.Contains(v, "On a break") {
		t.Errorf("rows from T2 shown on T1:\n%s", v)
	}
}

func TestRecentStatusesFailedFetchShowsNone(t *testing.T) {
	a, _ := recentApp(t, testRecent, nil)
	a.Update(openStatusMenu(t, a))
	// Offline now: the cached rows go too, as in Slack's own clients.
	_, cmd := a.Update(RecentStatusesMsg{Seq: a.recentStatusSeq, TeamID: "T1", Err: errors.New("offline")})
	for _, m := range drainBatch(cmd) {
		if _, ok := m.(ToastMsg); ok {
			t.Errorf("failed fetch showed a toast: %v", m)
		}
	}
	if v := statusMenuView(a); strings.Contains(v, "On a break") {
		t.Errorf("rows kept after a failed fetch:\n%s", v)
	}
}

// Each workspace has its own recent list: switching to one with none
// fetched yet must not show the previous workspace's rows.
func TestRecentStatusesNotCarriedAcrossWorkspaces(t *testing.T) {
	a, _ := recentApp(t, testRecent, nil)
	a.Update(openStatusMenu(t, a))
	pressKey(t, a, keyCode(tea.KeyEscape))

	a.activeTeamID = "T2" // as a workspace switch leaves it
	openStatusMenu(t, a)  // T2's fetch is not delivered
	if v := statusMenuView(a); strings.Contains(v, "On a break") {
		t.Errorf("T1's recent rows shown on T2:\n%s", v)
	}
}

// Reopening Ctrl+S fires a new fetch; a slower reply to the earlier one
// must not replace (or, failing, wipe) the newer list.
func TestRecentStatusesStaleReplyIgnored(t *testing.T) {
	a, _ := recentApp(t, testRecent, nil)
	first := openStatusMenu(t, a)
	pressKey(t, a, keyCode(tea.KeyEscape))
	second := openStatusMenu(t, a)

	a.Update(second)
	first.Statuses, first.Err = nil, errors.New("timeout")
	a.Update(first)
	if v := statusMenuView(a); !strings.Contains(v, "On a break") {
		t.Errorf("the earlier fetch's failure wiped the newer list:\n%s", v)
	}
}
