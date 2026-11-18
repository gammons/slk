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
	return suppressAppWith(t, core.ChannelServiceFuncs{
		ReadCache: func(ids.ChannelID) []messages.MessageItem { return channelHistory() },
		ReadCacheRange: func(ids.ChannelID, ids.MessageTS, ids.MessageTS) []messages.MessageItem {
			return channelHistory()
		},
	}, rules...)
}

// suppressAppWith is suppressApp with the given channel service.
func suppressAppWith(t *testing.T, fns core.ChannelServiceFuncs, rules ...core.SuppressRule) (*App, *fakeSuppression) {
	t.Helper()
	fake := &fakeSuppression{set: core.NewSuppression(rules)}
	a := newTestApp(t, withActiveTeam("T1"), withActiveChannel("C1"), withChannelService(fns))
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
	out, oldest, newest := messages.Visible(func(m messages.MessageItem) bool { return m.UserID == "U2" || m.UserID == "U3" }, in)
	if got := shownTS(in); got != "1.0,2.0,3.0,4.0" {
		t.Errorf("input changed: %s (the mark-read target is read from it)", got)
	}
	if got := shownTS(out); got != "1.0,3.0" || oldest != "2.0" || newest != "4.0" {
		t.Errorf("out = %s oldest = %s newest = %s", got, oldest, newest)
	}
}

// Review finding (Greptile, PR #492): when every message of the first
// page was hidden the pane was empty, AtTop was false, and scrolling up
// never fetched the older history behind it.
func TestAllHiddenPageStillFetchesOlderHistory(t *testing.T) {
	asked := make(chan ids.MessageTS, 1)
	a, _ := suppressAppWith(t, core.ChannelServiceFuncs{
		FetchOlder: func(_ ids.ChannelID, oldest ids.MessageTS) core.Msg {
			asked <- oldest
			return OlderMessagesLoadedMsg{ChannelID: "C1", AnchorTS: string(oldest)}
		},
	}, core.SuppressRule{AuthorID: "B1"})
	a.messagepane.SetMessages([]messages.MessageItem{suppressMsg("3.0", "B1"), suppressMsg("4.0", "B1")})
	if len(a.messagepane.Messages()) != 0 {
		t.Fatalf("precondition: shown = %s", shownTS(a.messagepane.Messages()))
	}
	if !a.messagepane.AtTop() || !a.messagepane.ViewportAtTop() {
		t.Fatal("an all-hidden pane must count as at the top")
	}
	cmd := a.maybeFetchOlderHistory(a.messagepane.AtTop())
	if cmd == nil {
		t.Fatal("no older-history fetch")
	}
	deliveredMsgs[OlderMessagesLoadedMsg](t, cmd)
	if got := <-asked; got != "3.0" {
		t.Errorf("fetched before %s, want 3.0 (the oldest hidden message)", got)
	}

	// A pane with nothing at all (no history yet) still does not fetch.
	empty := messages.New(nil, "c")
	if empty.AtTop() || empty.ViewportAtTop() {
		t.Error("an empty pane with nothing hidden must not count as at the top")
	}
}

// Review finding (Greptile, PR #492): removing a rule reloaded the pane
// from the newest cached page, dropping the older history the user had
// paged back to.
func TestRemovingRuleKeepsPagedBackHistory(t *testing.T) {
	older := []messages.MessageItem{suppressMsg("0.1", "U2"), suppressMsg("0.2", "B1")}
	var since, until ids.MessageTS
	a, _ := suppressAppWith(t, core.ChannelServiceFuncs{
		// The newest page only, as the real cache read returns.
		ReadCache: func(ids.ChannelID) []messages.MessageItem { return channelHistory() },
		ReadCacheRange: func(_ ids.ChannelID, from, to ids.MessageTS) []messages.MessageItem {
			since, until = from, to
			return append(cloneMessageItems(older), channelHistory()...)
		},
	}, core.SuppressRule{AuthorID: "B1"})
	a.messagepane.PrependMessages(cloneMessageItems(older)) // paged back
	if got := shownTS(a.messagepane.Messages()); got != "0.1,1.0,2.0,4.0" {
		t.Fatalf("precondition shown = %s", got)
	}
	cmdSuppressed(a, commandArgs{})
	pressKey(t, a, keyCode(tea.KeyBackspace))
	if since != "0.1" || until != "4.0" {
		t.Errorf("read the cache from %q to %q, want 0.1 to 4.0 (the pane's loaded range)", since, until)
	}
	if got := shownTS(a.messagepane.Messages()); got != "0.1,0.2,1.0,2.0,3.0,4.0" {
		t.Errorf("shown = %s, want the paged-back history kept and B1 back", got)
	}
}

// Review finding (Greptile, PR #492): changing a rule with the thread
// parent selected moved the cursor to the newest reply.
func TestRuleChangeKeepsThreadParentSelected(t *testing.T) {
	a, _ := suppressApp(t)
	parent := suppressMsg("1.0", "U3")
	parent.ThreadTS = "1.0"
	a.threadPanel.SetThread(parent, []messages.MessageItem{suppressMsg("1.1", "U3"), suppressMsg("1.2", "B1")}, "C1", "1.0")
	a.threadVisible = true
	a.threadPanel.SelectParent()
	a.addSuppression(SuppressAuthorMsg{TeamID: "T1", Name: "B1", Rule: core.SuppressRule{AuthorID: "B1"}})
	if got := shownTS(a.threadPanel.Replies()); got != "1.1" {
		t.Fatalf("replies = %s, want 1.1", got)
	}
	if sel, ok := a.threadPanel.SelectedMessage(); !ok || sel.TS != "1.0" {
		t.Errorf("selected = %q, want the parent 1.0", sel.TS)
	}
}

// Review finding (Greptile, PR #492, second round): the reload after a
// removal read the cache on the Update goroutine. It now runs in a Cmd,
// and its result is dropped for a window that changed channel while it
// ran.
func TestRuleRemovalReloadsOffTheUpdateLoop(t *testing.T) {
	reads := 0
	a, _ := suppressAppWith(t, core.ChannelServiceFuncs{
		ReadCache: func(ids.ChannelID) []messages.MessageItem { reads++; return channelHistory() },
		ReadCacheRange: func(ids.ChannelID, ids.MessageTS, ids.MessageTS) []messages.MessageItem {
			reads++
			return channelHistory()
		},
	}, core.SuppressRule{AuthorID: "B1"})
	cmdSuppressed(a, commandArgs{})
	_, cmd := a.Update(keyCode(tea.KeyBackspace))
	if reads != 0 {
		t.Fatalf("%d cache reads during Update, want none", reads)
	}
	if cmd == nil {
		t.Fatal("no reload command")
	}
	msgs := deliveredMsgs[suppressionReloadedMsg](t, cmd)

	// The window moved to another channel before the reload landed.
	a.activeChannelID = "C2"
	a.messagepane.SetMessages([]messages.MessageItem{suppressMsg("9.0", "U2")})
	a.Update(msgs[0])
	if got := shownTS(a.messagepane.Messages()); got != "9.0" {
		t.Errorf("shown = %s, want the stale reload dropped", got)
	}
}

// reloadInto shows shown in the pane, runs a rule-removal reload whose
// cache read answers cached, lets between change the App while the read
// is in flight, and applies the result.
func reloadInto(t *testing.T, shown, cached []messages.MessageItem, between func(a *App)) *App {
	t.Helper()
	a, _ := suppressAppWith(t, core.ChannelServiceFuncs{
		ReadCacheRange: func(ids.ChannelID, ids.MessageTS, ids.MessageTS) []messages.MessageItem {
			return cloneMessageItems(cached)
		},
	})
	a.messagepane.SetMessages(cloneMessageItems(shown))
	msgs := deliveredMsgs[suppressionReloadedMsg](t, a.reloadAfterRuleRemoval())
	if between != nil {
		between(a)
	}
	a.Update(msgs[0])
	return a
}

// The maintainer's review (PR #492): a removal re-reads the window's
// loaded range and replaces the pane with it, selection kept by TS.
func TestRuleRemovalReplacesPaneKeepingSelection(t *testing.T) {
	shown := []messages.MessageItem{suppressMsg("1.0", "U1"), suppressMsg("2.0", "U2"), suppressMsg("4.0", "U3")}
	a := reloadInto(t, shown, channelHistory(), func(a *App) {
		a.messagepane.SelectByTS("2.0")
	})
	if got := shownTS(a.messagepane.Messages()); got != "1.0,2.0,3.0,4.0" {
		t.Fatalf("shown = %s, want the cached range", got)
	}
	if sel, ok := a.messagepane.SelectedMessage(); !ok || sel.TS != "2.0" {
		t.Errorf("selected = %q, want 2.0 kept", sel.TS)
	}
}

// Paging older or a live arrival while the read ran lands outside the
// range it read, and is kept.
func TestRuleRemovalReloadKeepsMessagesOutsideTheRange(t *testing.T) {
	shown := []messages.MessageItem{suppressMsg("1.0", "U1"), suppressMsg("4.0", "U3")}
	a := reloadInto(t, shown, channelHistory(), func(a *App) {
		a.messagepane.PrependMessages([]messages.MessageItem{suppressMsg("0.5", "U2")})
		a.messagepane.AppendMessage(suppressMsg("5.0", "U2"))
	})
	if got := shownTS(a.messagepane.Messages()); got != "0.5,1.0,2.0,3.0,4.0,5.0" {
		t.Errorf("shown = %s, want the paged-in and arrived messages kept", got)
	}
}

// Only the latest reload is applied.
func TestSupersededReloadIsIgnored(t *testing.T) {
	a, _ := suppressAppWith(t, core.ChannelServiceFuncs{
		ReadCacheRange: func(ids.ChannelID, ids.MessageTS, ids.MessageTS) []messages.MessageItem {
			return channelHistory()
		},
	})
	a.messagepane.SetMessages([]messages.MessageItem{suppressMsg("1.0", "U1"), suppressMsg("4.0", "U3")})
	first := deliveredMsgs[suppressionReloadedMsg](t, a.reloadAfterRuleRemoval())
	second := deliveredMsgs[suppressionReloadedMsg](t, a.reloadAfterRuleRemoval())
	a.Update(first[0])
	if got := shownTS(a.messagepane.Messages()); got != "1.0,4.0" {
		t.Fatalf("shown = %s, want the superseded reload ignored", got)
	}
	a.Update(second[0])
	if got := shownTS(a.messagepane.Messages()); got != "1.0,2.0,3.0,4.0" {
		t.Errorf("shown = %s, want the latest reload applied", got)
	}
}

// Removing a rule restores the thread's hidden replies.
func TestRuleRemovalReloadsThread(t *testing.T) {
	parent := suppressMsg("1.0", "U3")
	parent.ThreadTS = "1.0"
	replies := []messages.MessageItem{suppressMsg("1.1", "U3"), suppressMsg("1.2", "B1")}
	a, fake := suppressApp(t, core.SuppressRule{AuthorID: "B1"})
	a.threads = core.NewThreadService(core.ThreadServiceFuncs{
		CacheRead: func(ids.ChannelID, ids.ThreadTS) []messages.MessageItem {
			return append([]messages.MessageItem{parent}, cloneMessageItems(replies)...)
		},
	})
	a.threadPanel.SetThread(parent, cloneMessageItems(replies), "C1", "1.0")
	a.threadVisible = true
	if got := shownTS(a.threadPanel.Replies()); got != "1.1" {
		t.Fatalf("precondition replies = %s", got)
	}
	fake.set = fake.set.Without(core.SuppressRule{AuthorID: "B1"})
	msgs := deliveredMsgs[suppressionReloadedMsg](t, a.reloadAfterRuleRemoval())
	a.Update(msgs[0])
	if got := shownTS(a.threadPanel.Replies()); got != "1.1,1.2" {
		t.Errorf("replies = %s, want B1's reply back", got)
	}
}

// Review finding (Greptile, PR #492, fourth round): refiltering after a
// rule was added reset the hidden range to what that refilter hid, so a
// message hidden earlier and newer than anything shown fell outside the
// range a later reload read.
func TestAddingRuleKeepsLoadedRange(t *testing.T) {
	a, _ := suppressApp(t, core.SuppressRule{AuthorID: "U3"}) // 4.0 hidden, the newest
	if a.messagepane.NewestTS() != "4.0" || a.messagepane.OldestTS() != "1.0" {
		t.Fatalf("precondition range %s..%s", a.messagepane.OldestTS(), a.messagepane.NewestTS())
	}
	a.addSuppression(SuppressAuthorMsg{TeamID: "T1", Name: "B1", Rule: core.SuppressRule{AuthorID: "B1"}})
	if got := shownTS(a.messagepane.Messages()); got != "1.0,2.0" {
		t.Fatalf("shown = %s", got)
	}
	if got := a.messagepane.NewestTS(); got != "4.0" {
		t.Errorf("NewestTS = %s after adding a rule, want 4.0 kept", got)
	}
}

// NewestTS, like OldestTS, counts hidden messages, so a reload after a
// rule removal covers a hidden message newer than anything shown.
func TestNewestTSCountsHiddenMessages(t *testing.T) {
	m := messages.New(nil, "c")
	m.SetHide(func(msg messages.MessageItem) bool { return msg.UserID == "B1" })
	m.SetMessages([]messages.MessageItem{suppressMsg("1.0", "U2"), suppressMsg("2.0", "B1")})
	if got := m.NewestTS(); got != "2.0" {
		t.Errorf("NewestTS = %s, want 2.0", got)
	}
	m.AppendMessage(suppressMsg("3.0", "B1"))
	if got := m.NewestTS(); got != "3.0" {
		t.Errorf("after a hidden arrival NewestTS = %s, want 3.0", got)
	}
	m.AppendMessage(suppressMsg("4.0", "U2"))
	if got := m.NewestTS(); got != "4.0" {
		t.Errorf("NewestTS = %s, want 4.0", got)
	}
}
