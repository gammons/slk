package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
)

// setReadCall records one ActivityService.SetRead call.
type setReadCall struct {
	team ids.TeamID
	key  string
	read bool
}

// fakeSetRead records each call on calls and answers with err.
func fakeSetRead(calls chan setReadCall, err error) core.ActivitySetReadFunc {
	return func(team ids.TeamID, it core.ActivityItem, read bool) core.Msg {
		calls <- setReadCall{team, it.Key, read}
		return ActivityReadSetMsg{TeamID: string(team), Key: it.Key, Read: read, Err: err}
	}
}

// activityReadApp is an App in T1 with the Activity view open and
// focused on items. Its SetRead calls arrive on the returned channel and
// are answered with err.
func activityReadApp(t *testing.T, err error, items ...core.ActivityItem) (*App, chan setReadCall) {
	t.Helper()
	a := newTestApp(t, withSize(120, 30), withActiveTeam("T1"))
	calls := make(chan setReadCall, 8)
	a.SetActivityService(core.NewActivityService(core.ActivityServiceFuncs{SetRead: fakeSetRead(calls, err)}))
	a.Update(ActivityViewActivatedMsg{})
	a.Update(ActivityListLoadedMsg{TeamID: "T1", Items: items})
	// The badge comes from the workspace's activity total, not the
	// loaded page: seed it the way connectWorkspace does at boot, with
	// one unread item per unread fixture.
	unread := 0
	for _, it := range items {
		if it.IsUnread {
			unread++
		}
	}
	a.Update(ActivityBadgeUpdatedMsg{TeamID: "T1", Total: unread})
	return a, calls
}

func unreadMention(key string) core.ActivityItem {
	return core.ActivityItem{Key: key, Type: "at_user", ChannelID: "C1", TS: "1.1", FeedTS: "1.1", IsUnread: true}
}

func selectedActivity(t *testing.T, a *App) core.ActivityItem {
	t.Helper()
	it, ok := a.activityView.SelectedItem()
	if !ok {
		t.Fatal("no Activity item selected")
	}
	return it
}

func TestActivityU_TogglesReadState(t *testing.T) {
	a, calls := activityReadApp(t, nil, unreadMention("k1"))

	_, cmd := a.Update(keyPress('U'))
	if selectedActivity(t, a).IsUnread || a.sidebar.ActivityUnreadCount() != 0 {
		t.Fatalf("U did not mark the item read at once (badge %d)", a.sidebar.ActivityUnreadCount())
	}
	a.Update(deliveredMsgs[ActivityReadSetMsg](t, cmd)[0])
	if c := <-calls; c != (setReadCall{"T1", "k1", true}) {
		t.Errorf("first U asked Slack for %+v, want T1/k1 read", c)
	}

	_, cmd = a.Update(keyPress('U'))
	if !selectedActivity(t, a).IsUnread || a.sidebar.ActivityUnreadCount() != 1 {
		t.Fatalf("second U did not mark the item unread again (badge %d)", a.sidebar.ActivityUnreadCount())
	}
	a.Update(deliveredMsgs[ActivityReadSetMsg](t, cmd)[0])
	if c := <-calls; c.read {
		t.Errorf("second U asked Slack for read=%v, want false", c.read)
	}
}

// The channel pane sits hidden behind the Activity view; U used to mark
// its selected message unread.
func TestActivityU_DoesNotMarkHiddenChannelMessage(t *testing.T) {
	a, _ := activityReadApp(t, nil, unreadMention("k1"))
	a.activeChannelID = "C9"
	a.messagepane.SetMessages(testMessageItems(3)) // selects the last one

	_, cmd := a.Update(keyPress('U'))
	if cmd == nil {
		t.Fatal("U in the Activity view returned no command")
	}
	switch got := cmd().(type) {
	case ActivityReadSetMsg:
	case MarkUnreadMsg:
		t.Fatalf("U in the Activity view marked hidden channel %s unread", got.ChannelID)
	default:
		t.Fatalf("U in the Activity view produced %T, want ActivityReadSetMsg", got)
	}
}

func TestActivityReadSetError_RollsBackAndToasts(t *testing.T) {
	a, _ := activityReadApp(t, errors.New("invalid_arguments"), unreadMention("k1"))

	_, cmd := a.Update(keyPress('U'))
	a.Update(deliveredMsgs[ActivityReadSetMsg](t, cmd)[0])

	if !selectedActivity(t, a).IsUnread || a.sidebar.ActivityUnreadCount() != 1 {
		t.Errorf("a failed mark-read was not undone (badge %d)", a.sidebar.ActivityUnreadCount())
	}
	if got := statusbarText(a); !strings.Contains(got, "Couldn't mark activity item read: invalid_arguments") {
		t.Errorf("status bar = %q, want the failure toast", got)
	}
}

func TestActivityReadSet_OtherTeamIgnored(t *testing.T) {
	a, _ := activityReadApp(t, nil, unreadMention("k1"))
	a.Update(keyPress('U'))

	a.Update(ActivityReadSetMsg{TeamID: "T2", Key: "k1", Read: true, Err: errors.New("boom")})
	if selectedActivity(t, a).IsUnread {
		t.Error("another workspace's failure rolled back this workspace's item")
	}
}

// Two quick U presses must not race: Slack could apply them in either
// order. Only one request per item is in flight; the second press only
// moves the dot, and its state is sent once the first is confirmed.
func TestActivityU_QuickTogglesAreSentInOrder(t *testing.T) {
	a, calls := activityReadApp(t, nil, unreadMention("k1"))
	_, first := a.Update(keyPress('U'))
	_, second := a.Update(keyPress('U'))
	if second != nil {
		t.Fatal("second U sent a request while the first was still in flight")
	}
	if !selectedActivity(t, a).IsUnread {
		t.Error("second U did not move the dot back to unread at once")
	}

	_, next := a.Update(deliveredMsgs[ActivityReadSetMsg](t, first)[0])
	if c := <-calls; !c.read {
		t.Fatalf("first request = %+v, want read", c)
	}
	if next == nil {
		t.Fatal("the confirmed read did not send the newer unread state")
	}
	_, done := a.Update(deliveredMsgs[ActivityReadSetMsg](t, next)[0])
	if c := <-calls; c.read {
		t.Errorf("follow-up request = %+v, want unread", c)
	}
	if done != nil || !selectedActivity(t, a).IsUnread || a.sidebar.ActivityUnreadCount() != 1 {
		t.Errorf("after both settle: cmd=%v unread=%v badge=%d, want nil/true/1",
			done != nil, selectedActivity(t, a).IsUnread, a.sidebar.ActivityUnreadCount())
	}
}

// U, U, and the one request fails: Slack still holds the original state,
// which is also what the user asked for last, so nothing is undone.
func TestActivityU_ToggleBackDuringFailedRequestMatchesSlack(t *testing.T) {
	a, calls := activityReadApp(t, errors.New("ratelimited"), unreadMention("k1"))
	_, first := a.Update(keyPress('U'))
	a.Update(keyPress('U'))

	_, next := a.Update(deliveredMsgs[ActivityReadSetMsg](t, first)[0])
	<-calls
	if next != nil {
		t.Error("a failed request whose state was already toggled back sent another")
	}
	if !selectedActivity(t, a).IsUnread || a.sidebar.ActivityUnreadCount() != 1 {
		t.Errorf("item ended read (badge %d); Slack still has it unread", a.sidebar.ActivityUnreadCount())
	}
}

// The follow-up request fails: undo to what Slack confirmed (read), not
// to the opposite of the failed request's predecessor.
func TestActivityU_FailedFollowUpRollsBackToConfirmed(t *testing.T) {
	a, calls := activityReadApp(t, nil, unreadMention("k1"))
	a.SetActivityService(core.NewActivityService(core.ActivityServiceFuncs{
		SetRead: func(team ids.TeamID, it core.ActivityItem, read bool) core.Msg {
			calls <- setReadCall{team, it.Key, read}
			var err error
			if !read {
				err = errors.New("ratelimited")
			}
			return ActivityReadSetMsg{TeamID: string(team), Key: it.Key, Read: read, Err: err}
		},
	}))
	_, first := a.Update(keyPress('U'))
	a.Update(keyPress('U'))
	_, next := a.Update(deliveredMsgs[ActivityReadSetMsg](t, first)[0])
	a.Update(deliveredMsgs[ActivityReadSetMsg](t, next)[0])
	<-calls
	<-calls

	if selectedActivity(t, a).IsUnread || a.sidebar.ActivityUnreadCount() != 0 {
		t.Errorf("item ended unread (badge %d); Slack confirmed read", a.sidebar.ActivityUnreadCount())
	}
	if got := statusbarText(a); !strings.Contains(got, "Couldn't mark activity item unread") {
		t.Errorf("status bar = %q, want the failure toast", got)
	}
}

// A result that lands after a workspace switch still ends the request,
// so the item can be changed again once the user comes back.
func TestActivityU_ResultAfterWorkspaceSwitchFreesTheItem(t *testing.T) {
	a, calls := activityReadApp(t, nil, unreadMention("k1"))
	_, first := a.Update(keyPress('U'))
	result := deliveredMsgs[ActivityReadSetMsg](t, first)[0]
	<-calls

	a.activeTeamID = "T2"
	a.Update(result)
	a.activeTeamID = "T1"

	if _, cmd := a.Update(keyPress('U')); cmd == nil {
		t.Error("U after the earlier result landed in another workspace sent nothing")
	}
}

// Review focus 2: the feed is refetched between a toggle and its failure.
func TestActivityReadSetError_AfterRefetchIsHarmless(t *testing.T) {
	a, _ := activityReadApp(t, errors.New("boom"), unreadMention("k1"))
	_, cmd := a.Update(keyPress('U'))
	a.Update(ActivityListLoadedMsg{TeamID: "T1", Items: []core.ActivityItem{unreadMention("k2")}})

	a.Update(deliveredMsgs[ActivityReadSetMsg](t, cmd)[0])
	if it := selectedActivity(t, a); it.Key != "k2" || !it.IsUnread {
		t.Errorf("rollback for k1 changed the refetched list: %+v", it)
	}
	if n := a.sidebar.ActivityUnreadCount(); n != 1 {
		t.Errorf("badge = %d, want 1 (the refetched list's count)", n)
	}
}

// Review focus 3.
func TestActivityU_EmptyListIsNoOp(t *testing.T) {
	a, calls := activityReadApp(t, nil)
	if _, cmd := a.Update(keyPress('U')); cmd != nil {
		t.Error("U on an empty Activity list returned a command")
	}
	if len(calls) != 0 {
		t.Error("U on an empty Activity list called Slack")
	}
}

// Review focus 4: with the thread pane focused, U is about the thread's
// replies, not the Activity list. No reply is selected here, so it does
// nothing at all.
func TestActivityU_ThreadFocusedKeepsMessageMeaning(t *testing.T) {
	a, calls := activityReadApp(t, nil, unreadMention("k1"))
	a.focusedPanel = PanelThread

	if _, cmd := a.Update(keyPress('U')); cmd != nil {
		if _, ok := cmd().(ActivityReadSetMsg); ok {
			t.Error("U with the thread pane focused changed the Activity item")
		}
	}
	if len(calls) != 0 || !selectedActivity(t, a).IsUnread {
		t.Error("U with the thread pane focused touched the Activity item")
	}
}

func reminder(unread bool) core.ActivityItem {
	return core.ActivityItem{Key: "saved_reminder.1.1.0", Type: "saved_reminder", Subtype: "reminder",
		FeedTS: "1791803337.000000", IsUnread: unread}
}

// enterOnActivity presses Enter and feeds the resulting
// ActivitySelectedMsg back through Update, returning that step's command.
func enterOnActivity(t *testing.T, a *App) tea.Cmd {
	t.Helper()
	_, cmd := a.Update(keyCode(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter in the Activity view returned no command")
	}
	sel, ok := cmd().(ActivitySelectedMsg)
	if !ok {
		t.Fatal("Enter in the Activity view did not select the item")
	}
	_, cmd = a.Update(sel)
	return cmd
}

func TestActivityEnter_UnreadReminderMarksReadAndToasts(t *testing.T) {
	a, calls := activityReadApp(t, nil, reminder(true))

	cmd := enterOnActivity(t, a)
	if selectedActivity(t, a).IsUnread || a.sidebar.ActivityUnreadCount() != 0 {
		t.Errorf("Enter did not mark the reminder read (badge %d)", a.sidebar.ActivityUnreadCount())
	}
	if got := statusbarText(a); !strings.Contains(got, "Reminder: open Slack to see it") {
		t.Errorf("status bar = %q, want the reminder toast", got)
	}
	deliveredMsgs[ActivityReadSetMsg](t, cmd)
	if c := <-calls; c != (setReadCall{"T1", "saved_reminder.1.1.0", true}) {
		t.Errorf("Enter asked Slack for %+v, want the reminder marked read", c)
	}
}

// Enter on a reminder already read only explains; it must not call
// Slack. The command is the toast's 3 s clear tick, so watch for a call
// for a short while instead of waiting the tick out.
func TestActivityEnter_ReadReminderOnlyToasts(t *testing.T) {
	a, calls := activityReadApp(t, nil, reminder(false))

	cmd := enterOnActivity(t, a)
	if got := statusbarText(a); !strings.Contains(got, "Reminder: open Slack to see it") {
		t.Errorf("status bar = %q, want the reminder toast", got)
	}
	go deliverFirst(cmd, make(chan ActivityReadSetMsg, 1))
	select {
	case c := <-calls:
		t.Errorf("Enter on a read reminder asked Slack for %+v", c)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestActivityEnter_UnknownItemMarksReadAndToasts(t *testing.T) {
	a, calls := activityReadApp(t, nil, core.ActivityItem{Key: "x1", Type: "brand_new_type", FeedTS: "1.1", IsUnread: true})

	cmd := enterOnActivity(t, a)
	if got := statusbarText(a); !strings.Contains(got, "slk can't open this activity item yet") {
		t.Errorf("status bar = %q, want the can't-open toast", got)
	}
	deliveredMsgs[ActivityReadSetMsg](t, cmd)
	if c := <-calls; !c.read || c.key != "x1" {
		t.Errorf("Enter asked Slack for %+v, want x1 marked read", c)
	}
}

// A selection made before a workspace switch must not act in the new
// workspace: its item belongs to the old one.
func TestActivitySelected_FromOtherWorkspaceIgnored(t *testing.T) {
	a, calls := activityReadApp(t, nil, reminder(true))

	_, cmd := a.Update(ActivitySelectedMsg{TeamID: "T0", Item: reminder(true)})
	if cmd != nil {
		t.Error("a selection from another workspace returned a command")
	}
	if len(calls) != 0 || !selectedActivity(t, a).IsUnread {
		t.Error("a selection from another workspace changed this workspace's item")
	}
}

// A change queued behind an in-flight request must still reach Slack if
// the user switches workspace before that request returns, and it must
// go to the item's own workspace, not the one now on screen.
func TestActivityU_QueuedChangeSurvivesWorkspaceSwitch(t *testing.T) {
	a, calls := activityReadApp(t, nil, unreadMention("k1"))
	_, first := a.Update(keyPress('U'))
	a.Update(keyPress('U')) // queued: unread
	result := deliveredMsgs[ActivityReadSetMsg](t, first)[0]
	<-calls

	a.activeTeamID = "T2"
	_, next := a.Update(result)
	if next == nil {
		t.Fatal("the queued unread was dropped by the workspace switch")
	}
	deliveredMsgs[ActivityReadSetMsg](t, next)
	if c := <-calls; c != (setReadCall{"T1", "k1", false}) {
		t.Errorf("queued change sent as %+v, want T1/k1 unread", c)
	}
}
