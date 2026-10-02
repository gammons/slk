package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gammons/slk/internal/ui/statusbar"
)

// The tests here run the real clear ticks that toastWithClear (and so
// copiedClearAfter) schedules, and feed back the messages they deliver.
// They pass short durations so the ticks fire at once; Update's arms
// differ only in the duration they pass.

// TestToastClear_StaleTickLeavesLaterToast pins the CopiedClearMsg
// sequence (statusbar ToastSeq): when a second toast replaces the first
// before the first toast's tick fires, that tick must leave the second
// toast alone, and the second toast's own tick clears it. Before the
// sequence, the first tick cut the second toast short. The second toast
// can repeat the first one's text, as copying twice does.
func TestToastClear_StaleTickLeavesLaterToast(t *testing.T) {
	for _, tc := range []struct{ name, first, second string }{
		{"another text", "first toast", "second toast"},
		{"the same text", "Copied 5 chars", "Copied 5 chars"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(t)
			firstCmd := toastWithClear(a, tc.first, 10*time.Millisecond)
			secondCmd := toastWithClear(a, tc.second, 10*time.Millisecond)
			// Both ticks fire now, after the second toast showed.
			ticks := deliveredMsgs[statusbar.CopiedClearMsg](t, firstCmd, secondCmd)

			a.Update(ticks[0])
			if got := statusbarText(a); !strings.Contains(got, tc.second) {
				t.Fatalf("status bar = %q after the first toast's tick, want the second toast", got)
			}
			a.Update(ticks[1])
			if got := statusbarText(a); strings.Contains(got, tc.second) {
				t.Errorf("status bar = %q after the second toast's tick, want it cleared", got)
			}
		})
	}
}

// TestToastClear_UploadProgressOutlivesEarlierExpiry covers a toast
// with no expiry of its own: upload progress. It replaces the toast
// without scheduling a clear, and the tick of the toast it replaced
// must not erase it. The upload result's toast then replaces it and
// clears on its own real tick, which takes the production 2s.
func TestToastClear_UploadProgressOutlivesEarlierExpiry(t *testing.T) {
	a := newTestApp(t)
	themeCmd := toastWithClear(a, "Theme: Dracula", 10*time.Millisecond)

	a.Update(UploadProgressMsg{Done: 1, Total: 3})
	a.Update(deliveredMsgs[statusbar.CopiedClearMsg](t, themeCmd)[0])
	if got := statusbarText(a); !strings.Contains(got, "Uploading 1/3") {
		t.Fatalf("status bar = %q after the theme toast's tick, want the upload progress", got)
	}

	_, cmd := a.Update(UploadResultMsg{})
	if got := statusbarText(a); !strings.Contains(got, "Sent") {
		t.Fatalf("status bar = %q after the upload result, want %q", got, "Sent")
	}
	a.Update(deliveredMsgs[statusbar.CopiedClearMsg](t, cmd)[0])
	if got := statusbarText(a); strings.Contains(got, "Sent") {
		t.Errorf("status bar = %q after the result toast's tick, want it cleared", got)
	}
}

// TestCopiedMsg_ZeroShowsNothing: a copy of nothing shows no toast and
// schedules no clear. A clear scheduled then would belong to the toast
// on screen, here an upload's progress, and remove it.
func TestCopiedMsg_ZeroShowsNothing(t *testing.T) {
	a := newTestApp(t)
	a.Update(UploadProgressMsg{Done: 1, Total: 3})
	if _, cmd := a.Update(statusbar.CopiedMsg{N: 0}); cmd != nil {
		t.Errorf("cmd = %T, want nil: its tick would clear the upload progress", cmd)
	}
	if got := statusbarText(a); !strings.Contains(got, "Uploading 1/3") {
		t.Errorf("status bar = %q, want the upload progress untouched", got)
	}
}
