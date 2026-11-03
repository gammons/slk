package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// fakeSuppression is an in-memory SuppressionService for team T1 whose
// Add and Remove answer err.
type fakeSuppression struct {
	set *core.Suppression
	err error
}

func (f *fakeSuppression) service() core.SuppressionService {
	return core.NewSuppressionService(
		func(teamID string) *core.Suppression {
			if teamID != "T1" {
				return nil
			}
			return f.set
		},
		func(_ string, r core.SuppressRule) error {
			if f.err != nil {
				return f.err
			}
			f.set = f.set.With(r)
			return nil
		},
		func(_ string, r core.SuppressRule) error {
			if f.err != nil {
				return f.err
			}
			f.set = f.set.Without(r)
			return nil
		},
	)
}

func suppressMsg(ts, user string) messages.MessageItem {
	return messages.MessageItem{TS: ts, UserID: user, UserName: "name-" + user, Text: "text " + ts}
}

// channelHistory is C1's cached history in the suppression tests: one
// message each from U1 (the current user), U2, bot B1 and U3.
func channelHistory() []messages.MessageItem {
	return []messages.MessageItem{
		suppressMsg("1.0", "U1"),
		suppressMsg("2.0", "U2"),
		suppressMsg("3.0", "B1"),
		suppressMsg("4.0", "U3"),
	}
}

// suppressApp is an App in T1 on C1, user U1, with the given rules and
// C1's cached history as channelHistory, shown in the pane.
func suppressApp(t *testing.T, rules ...core.SuppressRule) (*App, *fakeSuppression) {
	t.Helper()
	fake := &fakeSuppression{set: core.NewSuppression(rules)}
	a := newTestApp(t, withActiveTeam("T1"), withActiveChannel("C1"),
		withChannelService(core.ChannelServiceFuncs{
			ReadCache: func(ids.ChannelID) []messages.MessageItem { return channelHistory() },
		}))
	a.currentUserID = "U1"
	a.SetSuppressionService(fake.service())
	a.messagepane.SetMessages(channelHistory())
	return a, fake
}

func shownTS(msgs []messages.MessageItem) string {
	var ts []string
	for _, m := range msgs {
		ts = append(ts, m.TS)
	}
	return strings.Join(ts, ",")
}

func TestSuppressedAuthorsAreNotShown(t *testing.T) {
	a, _ := suppressApp(t,
		core.SuppressRule{AuthorID: "B1"},                  // everywhere
		core.SuppressRule{AuthorID: "U3", ChannelID: "C1"}, // here
		core.SuppressRule{AuthorID: "U2", ChannelID: "C9"}, // elsewhere
		core.SuppressRule{AuthorID: "U1"},                  // the user: never
	)
	if got := shownTS(a.messagepane.Messages()); got != "1.0,2.0" {
		t.Errorf("shown = %s, want 1.0,2.0", got)
	}

	// A live arrival by a hidden author is dropped too.
	a.Update(NewMessageMsg{ChannelID: "C1", Message: suppressMsg("5.0", "B1")})
	a.Update(NewMessageMsg{ChannelID: "C1", Message: suppressMsg("6.0", "U2")})
	if got := shownTS(a.messagepane.Messages()); got != "1.0,2.0,6.0" {
		t.Errorf("after arrivals shown = %s, want 1.0,2.0,6.0", got)
	}
}

func TestSuppressionRulesBelongToTheirWorkspace(t *testing.T) {
	a, _ := suppressApp(t, core.SuppressRule{AuthorID: "U2"})
	a.activeTeamID = "T2" // the fake has rules for T1 only
	a.messagepane.SetMessages(channelHistory())
	if got := shownTS(a.messagepane.Messages()); got != "1.0,2.0,3.0,4.0" {
		t.Errorf("shown = %s: T1's rule applied in T2", got)
	}
}

func TestSuppressedRepliesAreNotShownInThread(t *testing.T) {
	a, _ := suppressApp(t, core.SuppressRule{AuthorID: "U2", ChannelID: "C1"})
	parent := suppressMsg("1.0", "U2") // a suppressed author's parent still shows: the user opened it
	parent.ThreadTS = "1.0"
	replies := []messages.MessageItem{suppressMsg("1.1", "U3"), suppressMsg("1.2", "U2")}
	a.threadPanel.SetThread(parent, replies, "C1", "1.0")
	if got := shownTS(a.threadPanel.Replies()); got != "1.1" {
		t.Errorf("replies = %s, want 1.1", got)
	}
	if a.threadPanel.ParentMsg().TS != "1.0" {
		t.Error("the parent must stay")
	}
	a.threadPanel.AddReply(suppressMsg("1.3", "U2"))
	a.threadPanel.AddReply(suppressMsg("1.4", "U3"))
	if got := shownTS(a.threadPanel.Replies()); got != "1.1,1.4" {
		t.Errorf("after AddReply replies = %s, want 1.1,1.4", got)
	}
	// The same author in another channel's thread is not hidden.
	a.threadPanel.SetThread(parent, replies, "C2", "1.0")
	if got := shownTS(a.threadPanel.Replies()); got != "1.1,1.2" {
		t.Errorf("C2 replies = %s, want 1.1,1.2", got)
	}
}

func TestSplitWindowFollowsItsOwnChannelRules(t *testing.T) {
	a, _ := suppressApp(t, core.SuppressRule{AuthorID: "U2", ChannelID: "C1"})
	_ = a.splitWindow(0)
	// The focused (new) window is now on C2; the old one stays on C1.
	a.activeChannelID = "C2"
	a.setFocusedWindowChannel("C2", "two", "channel")
	a.messagepane.SetMessages(channelHistory())
	if got := shownTS(a.messagepane.Messages()); got != "1.0,2.0,3.0,4.0" {
		t.Errorf("C2 window shown = %s: C1's rule applied", got)
	}
	for _, m := range a.modelsForChannel("C1") {
		m.SetMessages(channelHistory())
		if got := shownTS(m.Messages()); got != "1.0,3.0,4.0" {
			t.Errorf("C1 window shown = %s, want 1.0,3.0,4.0", got)
		}
	}
}

func TestSuppressCommandHidesAuthorEverywhere(t *testing.T) {
	a, fake := suppressApp(t)
	focusMessages(t, a)
	a.messagepane.SelectByTS("3.0") // B1's message
	pressKey(t, a, keyPress(':'))
	for _, r := range "suppress" {
		pressKey(t, a, keyPress(r))
	}
	pressKey(t, a, keyCode(tea.KeyEnter))
	if !dialogShows(a, "Hide messages from name-B1?") {
		t.Fatalf("prompt not shown:\n%s", a.confirmPrompt.View())
	}
	// Press "everywhere" and deliver its answer without running the
	// toast's clear tick (pressKey would wipe the toast).
	_, cmd := a.Update(keyPress('2'))
	for _, m := range deliveredMsgs[SuppressAuthorMsg](t, cmd) {
		a.Update(m)
	}
	want := core.SuppressRule{AuthorID: "B1"}
	if !fake.set.Has(want) || fake.set.Len() != 1 {
		t.Fatalf("rules = %+v, want only %+v", fake.set.Rules(), want)
	}
	if got := shownTS(a.messagepane.Messages()); got != "1.0,2.0,4.0" {
		t.Errorf("shown = %s, want B1's message gone at once", got)
	}
	if got := statusbarText(a); !strings.Contains(got, "Hiding name-B1 everywhere") {
		t.Errorf("status bar = %q, want the hiding toast", got)
	}
}

func TestSuppressCommandHereOnly(t *testing.T) {
	a, fake := suppressApp(t)
	focusMessages(t, a)
	a.messagepane.SelectByTS("2.0")
	cmdSuppress(a, commandArgs{})
	_, cmd := a.Update(keyPress('1'))
	for _, m := range deliveredMsgs[SuppressAuthorMsg](t, cmd) {
		a.Update(m)
	}
	if r := fake.set.Rules(); len(r) != 1 || r[0].AuthorID != "U2" || r[0].ChannelID != "C1" {
		t.Fatalf("rules = %+v, want U2 in C1", r)
	}
}

func TestSuppressCommandRefuses(t *testing.T) {
	a, fake := suppressApp(t)
	focusMessages(t, a)
	a.messagepane.SelectByTS("1.0") // own
	if cmd := cmdSuppress(a, commandArgs{}); cmd == nil || a.confirmPrompt.IsVisible() {
		t.Error("own message: want a toast and no prompt")
	}
	a.focusedPanel = PanelSidebar
	if cmd := cmdSuppress(a, commandArgs{}); cmd == nil || a.confirmPrompt.IsVisible() {
		t.Error("no message pane focused: want a toast and no prompt")
	}
	if fake.set.Len() != 0 {
		t.Errorf("rules = %+v, want none", fake.set.Rules())
	}
}

func TestSuppressAddFailureKeepsMessages(t *testing.T) {
	a, fake := suppressApp(t)
	fake.err = errors.New("disk full")
	cmd := a.addSuppression(SuppressAuthorMsg{TeamID: "T1", Name: "U2", Rule: core.SuppressRule{AuthorID: "U2"}})
	if !strings.Contains(statusbarText(a), "disk full") {
		t.Errorf("toast = %q", statusbarText(a))
	}
	_ = cmd
	if got := shownTS(a.messagepane.Messages()); got != "1.0,2.0,3.0,4.0" {
		t.Errorf("shown = %s", got)
	}
}

func TestSuppressedListRemovesRuleAndRestoresMessages(t *testing.T) {
	a, fake := suppressApp(t, core.SuppressRule{AuthorID: "B1"}, core.SuppressRule{AuthorID: "U3", ChannelID: "C1"})
	if got := shownTS(a.messagepane.Messages()); got != "1.0,2.0" {
		t.Fatalf("precondition shown = %s", got)
	}
	cmdSuppressed(a, commandArgs{})
	if a.mode != ModeSuppressed || !a.suppressList.IsVisible() {
		t.Fatal("list not open")
	}
	box := a.suppressList.View(a.width, a.height)
	if !strings.Contains(box, "B1") || !strings.Contains(box, "everywhere") {
		t.Errorf("list does not show the workspace rule:\n%s", box)
	}
	pressKey(t, a, keyCode(tea.KeyBackspace)) // removes B1, the first row
	if fake.set.Len() != 1 || fake.set.Has(core.SuppressRule{AuthorID: "B1"}) {
		t.Fatalf("rules = %+v", fake.set.Rules())
	}
	if !a.suppressList.IsVisible() {
		t.Error("the list should stay open")
	}
	if got := shownTS(a.messagepane.Messages()); got != "1.0,2.0,3.0" {
		t.Errorf("shown = %s, want B1's message back from the cache", got)
	}
	pressKey(t, a, keyCode(tea.KeyEscape))
	if a.mode != ModeNormal || a.suppressList.IsVisible() {
		t.Error("Esc should close the list")
	}
}

func TestSuppressedListRemoveFailureShowsNote(t *testing.T) {
	a, fake := suppressApp(t, core.SuppressRule{AuthorID: "B1"})
	fake.err = core.ErrSuppressRuleNotEditable
	cmdSuppressed(a, commandArgs{})
	pressKey(t, a, keyCode(tea.KeyDelete))
	if fake.set.Len() != 1 {
		t.Error("rule should remain")
	}
	if box := a.suppressList.View(a.width, a.height); !strings.Contains(box, "config.toml") {
		t.Errorf("want the edit-by-hand note:\n%s", box)
	}
}

func TestOldestTSCountsHiddenMessages(t *testing.T) {
	m := messages.New(nil, "c")
	m.SetHide(func(msg messages.MessageItem) bool { return msg.UserID == "B1" })
	m.SetMessages([]messages.MessageItem{suppressMsg("3.0", "U2"), suppressMsg("4.0", "U2")})
	// A page of older history that is all hidden must still move the
	// older-history cursor, or scrolling up would fetch it forever.
	m.PrependMessages([]messages.MessageItem{suppressMsg("1.0", "B1"), suppressMsg("2.0", "B1")})
	if got := m.OldestTS(); got != "1.0" {
		t.Errorf("OldestTS = %s, want 1.0", got)
	}
	if got := shownTS(m.Messages()); got != "3.0,4.0" {
		t.Errorf("shown = %s", got)
	}
}

func TestVisibleNeverWritesItsInput(t *testing.T) {
	in := channelHistory()
	out, oldest := messages.Visible(func(m messages.MessageItem) bool { return m.UserID == "U2" || m.UserID == "U3" }, in)
	if got := shownTS(in); got != "1.0,2.0,3.0,4.0" {
		t.Errorf("input changed: %s (the mark-read target is read from it)", got)
	}
	if got := shownTS(out); got != "1.0,3.0" || oldest != "2.0" {
		t.Errorf("out = %s oldest = %s", got, oldest)
	}
}
