package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gammons/slk/internal/ui/statusbar"
)

// TestToastClear_StaleTickLeavesLaterToast pins the CopiedClearMsg
// sequence (statusbar ToastSeq): when a second toast replaces the first
// before the first toast's tick fires, that tick must leave the second
// toast alone, and the second toast's own tick clears it. Before the
// sequence, the first tick cut the second toast short.
func TestToastClear_StaleTickLeavesLaterToast(t *testing.T) {
	a := newTestApp(t)
	_ = toastWithClear(a, "first toast", 2*time.Second)
	first := statusbar.CopiedClearMsg{Seq: a.statusbar.ToastSeq()}
	_ = toastWithClear(a, "second toast", 2*time.Second)
	second := statusbar.CopiedClearMsg{Seq: a.statusbar.ToastSeq()}

	a.Update(first)
	if got := statusbarText(a); !strings.Contains(got, "second toast") {
		t.Fatalf("status bar = %q after the first toast's tick, want the second toast", got)
	}
	a.Update(second)
	if got := statusbarText(a); strings.Contains(got, "second toast") {
		t.Errorf("status bar = %q after the second toast's tick, want it cleared", got)
	}
}

// TestToastClear_UploadProgressOutlivesEarlierExpiry covers a toast
// with no expiry of its own: upload progress. It replaces the toast
// without scheduling a clear, and the tick of the toast it replaced
// must not erase it. The upload result's toast then clears on its own
// tick.
func TestToastClear_UploadProgressOutlivesEarlierExpiry(t *testing.T) {
	a := newTestApp(t)
	_ = toastWithClear(a, "Theme: Dracula", 2*time.Second)
	themeClear := statusbar.CopiedClearMsg{Seq: a.statusbar.ToastSeq()}

	a.Update(UploadProgressMsg{Done: 1, Total: 3})
	a.Update(themeClear)
	if got := statusbarText(a); !strings.Contains(got, "Uploading 1/3") {
		t.Fatalf("status bar = %q after the theme toast's tick, want the upload progress", got)
	}

	a.Update(UploadResultMsg{})
	if got := statusbarText(a); !strings.Contains(got, "Sent") {
		t.Fatalf("status bar = %q after the upload result, want %q", got, "Sent")
	}
	a.Update(statusbar.CopiedClearMsg{Seq: a.statusbar.ToastSeq()})
	if got := statusbarText(a); strings.Contains(got, "Sent") {
		t.Errorf("status bar = %q after the result toast's tick, want it cleared", got)
	}
}

// TestCopiedClearAfter_TickCarriesToastSeq checks the tick end of the
// same contract: the message copiedClearAfter delivers carries the
// ToastSeq of the toast showing when it was scheduled.
func TestCopiedClearAfter_TickCarriesToastSeq(t *testing.T) {
	a := newTestApp(t)
	a.statusbar.SetToast("a toast")
	want := a.statusbar.ToastSeq()
	msg, ok := copiedClearAfter(a, time.Millisecond)().(statusbar.CopiedClearMsg)
	if !ok {
		t.Fatalf("tick delivered %T, want statusbar.CopiedClearMsg", msg)
	}
	if msg.Seq != want || want == 0 {
		t.Errorf("tick Seq = %d, want the toast's ToastSeq %d", msg.Seq, want)
	}
}
