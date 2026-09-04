package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

func TestNewApp_StartsFocused(t *testing.T) {
	// Terminals report focus transitions only, never current state. A
	// terminal without focus-event support sends nothing at all, so
	// defaulting to focused is what preserves today's behavior there.
	if !NewApp().terminalFocused {
		t.Error("terminalFocused must default to true")
	}
}

func TestBlurMsg_ClearsFocus(t *testing.T) {
	app := NewApp()
	_, _ = app.Update(tea.BlurMsg{})
	if app.terminalFocused {
		t.Error("terminalFocused must be false after BlurMsg")
	}
}

func TestFocusMsg_RestoresFocus(t *testing.T) {
	app := NewApp()
	_, _ = app.Update(tea.BlurMsg{})
	_, _ = app.Update(tea.FocusMsg{})
	if !app.terminalFocused {
		t.Error("terminalFocused must be true after FocusMsg")
	}
}

func TestView_EnablesFocusReporting(t *testing.T) {
	app := NewApp()
	app.width, app.height = 80, 24
	if !app.View().ReportFocus {
		t.Error("View must set ReportFocus so the terminal sends focus events")
	}
}

// reduceFocus must CLAIM every message type it owns, so no later
// reducer in the chain (and not the residual switch) gets a second
// look at it. The flag is assigned before the return in each arm, so
// the state assertions above pass even if the claim flag is wrong --
// this is the only test that pins the flag itself.
func TestReduceFocus_ClaimsOnlyItsOwnMessages(t *testing.T) {
	owned := []tea.Msg{tea.FocusMsg{}, tea.BlurMsg{}, markFlushMsg{}}
	for _, msg := range owned {
		if _, handled := reduceFocus(NewApp(), msg); !handled {
			t.Errorf("reduceFocus(%T) handled = false, want true", msg)
		}
	}
	if _, handled := reduceFocus(NewApp(), ChannelMarkedReadMsg{}); handled {
		t.Error("reduceFocus claimed ChannelMarkedReadMsg, which reduceChannels owns")
	}
}

// markCapture wires fake mark services and records every call.
func markCapture(t *testing.T) (*App, *[]string) {
	t.Helper()
	app := NewApp()
	app.markFlushDebounce = time.Millisecond
	calls := &[]string{}
	app.SetChannelService(NewChannelService(ChannelServiceFuncs{
		MarkRead: func(channelID ids.ChannelID, ts ids.MessageTS) tea.Msg {
			*calls = append(*calls, "ch:"+string(channelID)+"/"+string(ts))
			return nil
		},
	}))
	app.SetThreadService(NewThreadService(ThreadServiceFuncs{
		Mark: func(channelID ids.ChannelID, threadTS ids.ThreadTS, ts ids.MessageTS) tea.Cmd {
			*calls = append(*calls, "th:"+string(channelID)+"/"+string(threadTS)+"/"+string(ts))
			return nil
		},
	}))
	return app, calls
}

// feed runs cmd to completion and pushes every resulting message back
// through app.Update, so a scheduled flush tick actually fires within
// the test. Reuses the existing drainBatch helper in
// internal/ui/app_selection_test.go:29. Depth-bounded so a
// self-rescheduling cmd cannot loop forever.
func feed(t *testing.T, app *App, cmd tea.Cmd, depth int) {
	t.Helper()
	if cmd == nil || depth > 5 {
		return
	}
	for _, msg := range drainBatch(cmd) {
		if msg == nil {
			continue
		}
		_, next := app.Update(msg)
		feed(t, app, next, depth+1)
	}
}

func TestFocusedArrival_MarksActiveChannelRead(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "5.000000", UserID: "U2"},
	})
	feed(t, app, cmd, 0)

	if len(*calls) != 1 || (*calls)[0] != "ch:C1/5.000000" {
		t.Fatalf("calls = %v, want one channel mark at 5.000000", *calls)
	}
}

func TestBlurredArrival_DoesNotMarkUntilFocusReturns(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"
	_, _ = app.Update(tea.BlurMsg{})

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "5.000000", UserID: "U2"},
	})
	feed(t, app, cmd, 0)

	if len(*calls) != 0 {
		t.Fatalf("blurred arrival must not mark read, got %v", *calls)
	}

	_, cmd = app.Update(tea.FocusMsg{})
	feed(t, app, cmd, 0)

	if len(*calls) != 1 || (*calls)[0] != "ch:C1/5.000000" {
		t.Fatalf("calls after refocus = %v, want the staged mark to flush", *calls)
	}
}

// The blur-mid-flight race: the arrival was focused, so a tick is
// armed, but the user leaves before it fires. The tick must re-park
// the slot rather than issue the mark -- the user stopped looking
// between the two events.
func TestBlurBeforeFlushTickFires_KeepsMarkStaged(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "5.000000", UserID: "U2"},
	})
	_, _ = app.Update(tea.BlurMsg{}) // ...before the tick lands
	feed(t, app, cmd, 0)

	if len(*calls) != 0 {
		t.Fatalf("a tick firing while blurred must not mark read, got %v", *calls)
	}
	if app.pendingChannelMark.ts != "5.000000" {
		t.Fatalf("pendingChannelMark.ts = %q, want the slot re-parked at 5.000000",
			app.pendingChannelMark.ts)
	}

	_, cmd = app.Update(tea.FocusMsg{})
	feed(t, app, cmd, 0)

	if len(*calls) != 1 || (*calls)[0] != "ch:C1/5.000000" {
		t.Fatalf("calls after refocus = %v, want the re-parked mark to flush", *calls)
	}
}

// A blurred arrival stages its slot but must arm no timer: the only
// way out of a blurred slot is the FocusMsg catch-up. Without the
// focus check in scheduleMarkFlush a blurred burst would arm one
// timer per debounce interval, each firing only to re-park the slot.
func TestBlurredArrival_ArmsNoFlushTick(t *testing.T) {
	app, _ := markCapture(t)
	app.activeChannelID = "C1"
	_, _ = app.Update(tea.BlurMsg{})

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "5.000000", UserID: "U2"},
	})

	for _, msg := range drainBatch(cmd) {
		if _, ok := msg.(markFlushMsg); ok {
			t.Fatal("a blurred arrival armed a flush tick; it must wait for FocusMsg")
		}
	}
	// The slot must still be staged -- otherwise the assertion above
	// would hold for the wrong reason.
	if app.pendingChannelMark.ts != "5.000000" {
		t.Fatalf("pendingChannelMark.ts = %q, want the arrival staged at 5.000000",
			app.pendingChannelMark.ts)
	}
}

// The unread dot is the "remains unread locally" half of #159: while
// blurred, the sidebar must repaint from the has_unread the WS handler
// just wrote. While focused it must NOT, or the dot flashes on every
// message in the channel the user is reading.
func TestActiveChannelArrival_NotifiesReadStateOnlyWhenBlurred(t *testing.T) {
	for _, tc := range []struct {
		name    string
		blur    bool
		wantMin int
	}{
		{name: "focused", blur: false, wantMin: 0},
		{name: "blurred", blur: true, wantMin: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, _ := markCapture(t)
			app.activeChannelID = "C1"
			if tc.blur {
				_, _ = app.Update(tea.BlurMsg{})
			}
			// SetStatusReporter is invoked by every
			// notifyReadStateChanged call; see app.go:3150.
			var reports int
			app.SetStatusReporter(func(int, int, string, string) { reports++ })

			// Deliberately does NOT feed the returned cmd: this
			// counts only the repaints the arrival itself triggers,
			// not the one the eventual mark-read echo causes.
			_, _ = app.Update(NewMessageMsg{
				ChannelID: "C1",
				Message:   messages.MessageItem{TS: "5.000000", UserID: "U2"},
			})

			if reports < tc.wantMin {
				t.Errorf("read-state notifications = %d, want at least %d", reports, tc.wantMin)
			}
			if !tc.blur && reports != 0 {
				t.Errorf("read-state notifications = %d, want 0: a focused arrival must not flash the dot", reports)
			}
		})
	}
}

func TestBurstCoalescesToNewestTS(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"

	// Only the FIRST arrival arms a tick; markFlushScheduled makes the
	// rest return nil. Collect every cmd and feed them after the whole
	// burst has landed, so the test does not depend on which one
	// carries the tick.
	var cmds []tea.Cmd
	for _, ts := range []string{"1.000000", "2.000000", "3.000000"} {
		_, cmd := app.Update(NewMessageMsg{
			ChannelID: "C1",
			Message:   messages.MessageItem{TS: ts, UserID: "U2"},
		})
		cmds = append(cmds, cmd)
	}
	for _, cmd := range cmds {
		feed(t, app, cmd, 0)
	}

	if len(*calls) != 1 || (*calls)[0] != "ch:C1/3.000000" {
		t.Fatalf("calls = %v, want a single mark at the newest ts", *calls)
	}
}

// markFlushScheduled bounds the conversations.mark RATE, not just the
// number of live timers. TestBurstCoalescesToNewestTS cannot see this:
// there the whole burst lands before any tick fires, so the slot is
// empty by the time the extra ticks arrive and they cost nothing.
//
// Under a stream that outpaces the debounce interval the ticks
// interleave with the arrivals instead, and every tick lands on a slot
// the next arrival has already refilled -- one mark per message
// against a Tier-3 endpoint. This loop models that timing discretely:
// each step delivers one arrival and then fires the tick armed on the
// PREVIOUS step, i.e. arrivals run at twice the debounce rate.
func TestSustainedStreamMarksPerWindowNotPerMessage(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"

	const steps = 8
	var dueNextStep tea.Cmd
	for i := 0; i < steps; i++ {
		_, cmd := app.Update(NewMessageMsg{
			ChannelID: "C1",
			Message:   messages.MessageItem{TS: fmt.Sprintf("%d.000000", i), UserID: "U2"},
		})
		fireNow := dueNextStep
		dueNextStep = cmd
		feed(t, app, fireNow, 0)
	}
	feed(t, app, dueNextStep, 0)

	// The exact sequence, not just a count. Odd steps are the ones that
	// fire a tick, and each flush carries the ts of the arrival that
	// landed in that same step. Asserting the full sequence pins three
	// things at once: the rate cap (one mark per window, not steps-1),
	// newest-wins within each window, and — because a short sequence
	// fails just as loudly as a long one — the markFlushScheduled reset
	// in reduceFocus's markFlushMsg arm, without which auto-marking
	// stops dead after the first flush.
	want := []string{
		"ch:C1/1.000000",
		"ch:C1/3.000000",
		"ch:C1/5.000000",
		"ch:C1/7.000000",
	}
	if got := strings.Join(*calls, " "); got != strings.Join(want, " ") {
		t.Fatalf("marks = %v\nwant  = %v\n(%d arrivals; one mark per debounce window, each at that window's newest ts)",
			*calls, want, steps)
	}
}

// Out-of-order delivery must not roll the cursor backward: the slot
// keeps the newest ts it has seen, so the older arrival is dropped.
func TestOutOfOrderArrivalDoesNotRollCursorBack(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"

	var cmds []tea.Cmd
	for _, ts := range []string{"9.000000", "4.000000"} {
		_, cmd := app.Update(NewMessageMsg{
			ChannelID: "C1",
			Message:   messages.MessageItem{TS: ts, UserID: "U2"},
		})
		cmds = append(cmds, cmd)
	}
	for _, cmd := range cmds {
		feed(t, app, cmd, 0)
	}

	if len(*calls) != 1 || (*calls)[0] != "ch:C1/9.000000" {
		t.Fatalf("calls = %v, want the newest ts to win over the late older one", *calls)
	}
}

// The thread slot is newest-wins on its own, independent of the
// channel slot: a burst of replies coalesces to one mark, and a late
// older reply does not roll the thread cursor backward.
func TestThreadBurstCoalescesToNewestTS(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"
	app.threadVisible = true
	app.threadPanel.SetThread(messages.MessageItem{TS: "1.000000"}, nil, "C1", "1.000000")

	var cmds []tea.Cmd
	for _, ts := range []string{"2.000000", "9.000000", "4.000000"} {
		_, cmd := app.Update(NewMessageMsg{
			ChannelID: "C1",
			Message:   messages.MessageItem{TS: ts, ThreadTS: "1.000000", UserID: "U2"},
		})
		cmds = append(cmds, cmd)
	}
	for _, cmd := range cmds {
		feed(t, app, cmd, 0)
	}

	if len(*calls) != 1 || (*calls)[0] != "th:C1/1.000000/9.000000" {
		t.Fatalf("calls = %v, want a single thread mark at the newest ts", *calls)
	}
}

func TestPlainThreadReplyDoesNotMarkChannel(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "5.000000", ThreadTS: "1.000000", UserID: "U2"},
	})
	feed(t, app, cmd, 0)

	for _, c := range *calls {
		if strings.HasPrefix(c, "ch:") {
			t.Fatalf("a plain thread reply must not advance the channel cursor, got %v", *calls)
		}
	}
}

func TestBroadcastReplyMarksChannel(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message: messages.MessageItem{
			TS: "5.000000", ThreadTS: "1.000000", Subtype: "thread_broadcast", UserID: "U2",
		},
	})
	feed(t, app, cmd, 0)

	var found bool
	for _, c := range *calls {
		if c == "ch:C1/5.000000" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a broadcast reply must advance the channel cursor, got %v", *calls)
	}
}

func TestFocusedReplyInOpenThreadMarksThread(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"
	app.threadVisible = true
	app.threadPanel.SetThread(messages.MessageItem{TS: "1.000000"}, nil, "C1", "1.000000")

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "5.000000", ThreadTS: "1.000000", UserID: "U2"},
	})
	feed(t, app, cmd, 0)

	if len(*calls) != 1 || (*calls)[0] != "th:C1/1.000000/5.000000" {
		t.Fatalf("calls = %v, want one thread mark", *calls)
	}
}

// A thread reply produces TWO independent cmds -- the mark-flush tick
// and the threads-list re-query tick. The tail must batch them; an
// early return on either one silently drops the other.
func TestThreadReplyArrival_ArmsFlushAndThreadsDirty(t *testing.T) {
	app, _ := markCapture(t)
	app.activeChannelID = "C1"
	app.activeTeamID = "T1" // scheduleThreadsDirty returns nil without one
	app.threadsDirtyDebounce = time.Millisecond
	app.threadVisible = true
	app.threadPanel.SetThread(messages.MessageItem{TS: "1.000000"}, nil, "C1", "1.000000")

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "5.000000", ThreadTS: "1.000000", UserID: "U2"},
	})

	var sawFlush, sawDirty bool
	for _, msg := range drainBatch(cmd) {
		switch msg.(type) {
		case markFlushMsg:
			sawFlush = true
		case ThreadsListDirtyMsg:
			sawDirty = true
		}
	}
	if !sawFlush {
		t.Error("no markFlushMsg tick: the pending thread mark would never be issued")
	}
	if !sawDirty {
		t.Error("no ThreadsListDirtyMsg tick: the involved-threads list would go stale")
	}
}

func TestReplyInClosedThreadDoesNotMark(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"
	app.threadVisible = false

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "5.000000", ThreadTS: "1.000000", UserID: "U2"},
	})
	feed(t, app, cmd, 0)

	if len(*calls) != 0 {
		t.Fatalf("a reply in a thread the user is not viewing must not mark, got %v", *calls)
	}
}

func TestBlurredReplyInOpenThreadDoesNotMarkUntilFocusReturns(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"
	app.threadVisible = true
	app.threadPanel.SetThread(messages.MessageItem{TS: "1.000000"}, nil, "C1", "1.000000")
	_, _ = app.Update(tea.BlurMsg{})

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "5.000000", ThreadTS: "1.000000", UserID: "U2"},
	})
	feed(t, app, cmd, 0)

	if len(*calls) != 0 {
		t.Fatalf("a blurred reply must not mark the thread read, got %v", *calls)
	}

	_, cmd = app.Update(tea.FocusMsg{})
	feed(t, app, cmd, 0)

	if len(*calls) != 1 || (*calls)[0] != "th:C1/1.000000/5.000000" {
		t.Fatalf("calls after refocus = %v, want the staged thread mark to flush", *calls)
	}
}

func TestInactiveChannelArrivalDoesNotMark(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C_OTHER"

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "5.000000", UserID: "U2"},
	})
	feed(t, app, cmd, 0)

	if len(*calls) != 0 {
		t.Fatalf("a message in a non-active channel must not mark, got %v", *calls)
	}
}

func TestFocusedArrivalLeavesDividerInPlace(t *testing.T) {
	app, _ := markCapture(t)
	// modelsForChannel routes the focused window by activeChannelID
	// (winmodels.go:60), and app.messagepane IS the focused window's
	// model, so this alone puts the arrival into the pane.
	app.activeChannelID = "C1"
	app.messagepane.SetLastReadTS("1.000000")

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "5.000000", UserID: "U2"},
	})
	feed(t, app, cmd, 0)

	// Guard the guard: prove the arrival actually landed in the pane,
	// so this test fails if the routing above ever stops working.
	if n := len(app.messagepane.Messages()); n == 0 {
		t.Fatal("arrival never reached the pane; the divider assertion would be vacuous")
	}
	if got := app.messagepane.LastReadTS(); got != "1.000000" {
		t.Errorf("messagepane LastReadTS = %q, want the divider left at 1.000000", got)
	}
}
