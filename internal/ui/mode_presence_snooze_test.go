package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/presencemenu"
)

// statusCall records one invocation of the App's status setter.
type statusCall struct {
	action presencemenu.Action
	mins   int
}

// snoozeSetup returns a setup that types digits into the snooze buffer
// through the handler itself (so the buffer is reached the way
// production reaches it) and records every setStatusFn call into calls.
func snoozeSetup(digits string, calls *[]statusCall) func(*testing.T, *App) {
	return func(t *testing.T, a *App) {
		// Reset: runKeyCases gives each row a fresh App but `calls`
		// is captured by the whole table, so without this every row
		// would see its predecessors' invocations.
		*calls = nil
		a.SetStatusSetter(func(action presencemenu.Action, mins int) {
			*calls = append(*calls, statusCall{action: action, mins: mins})
		})
		for _, r := range digits {
			_ = dispatchModeKey(a, keyPress(r))
		}
		if got := a.presence.SnoozeBuf(); got != digits {
			t.Fatalf("precondition: snooze buffer = %q, want %q", got, digits)
		}
	}
}

// statusbarText is the status bar's rendered text with styling removed,
// which is the only way to observe a toast — statusbar.Model keeps it
// unexported and offers no getter.
func statusbarText(a *App) string {
	return stripANSI(a.statusbar.View(120))
}

const invalidSnoozeToast = "Invalid snooze duration"

// TestPresenceCustomSnoozeModeKeys characterizes
// handlePresenceCustomSnoozeMode (mode_presence_snooze.go:25). Unlike
// the finder-style handlers this one owns its whole key regime: there
// is no sub-model, so every arm is visible in the handler.
func TestPresenceCustomSnoozeModeKeys(t *testing.T) {
	var calls []statusCall
	opts := []testOpt{withActiveTeam("T1")}

	runKeyCases(t, ModePresenceCustomSnooze, []keyCase{
		{
			name:     "esc discards the buffer and returns to Normal",
			opts:     opts,
			setup:    snoozeSetup("12", &calls),
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.presence.SnoozeBuf(); got != "" {
					t.Errorf("snooze buffer = %q, want empty", got)
				}
				if _, _, _, ok := a.presence.Status("T1"); ok {
					t.Error("esc applied a presence status; it must not")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			name:     "enter on a valid duration snoozes and notifies the status setter",
			opts:     opts,
			setup:    snoozeSetup("30", &calls),
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.presence.SnoozeBuf(); got != "" {
					t.Errorf("snooze buffer = %q, want empty", got)
				}
				_, dnd, end, ok := a.presence.Status("T1")
				if !ok {
					t.Fatal("no cached status for T1 after enter")
				}
				if !dnd {
					t.Error("DNDEnabled = false, want true")
				}
				// 30 minutes from "now", with slack for the clock
				// advancing between Apply and this assertion.
				if d := time.Until(end); d < 29*time.Minute || d > 30*time.Minute+time.Minute {
					t.Errorf("DND ends in %v, want ~30m", d)
				}
				if len(calls) != 1 {
					t.Fatalf("setStatusFn called %d times, want 1", len(calls))
				}
				if calls[0].action != presencemenu.ActionSnooze || calls[0].mins != 30 {
					t.Errorf("setStatusFn(%v, %d), want (ActionSnooze, 30)", calls[0].action, calls[0].mins)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil on the success path", cmd)
				}
				if got := statusbarText(a); strings.Contains(got, invalidSnoozeToast) {
					t.Errorf("status bar shows the invalid-duration toast: %q", got)
				}
			},
		},
		{
			name:     "enter on an empty buffer toasts and schedules the clear tick",
			opts:     opts,
			setup:    snoozeSetup("", &calls),
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := statusbarText(a); !strings.Contains(got, invalidSnoozeToast) {
					t.Errorf("status bar = %q, want it to contain %q", got, invalidSnoozeToast)
				}
				if _, _, _, ok := a.presence.Status("T1"); ok {
					t.Error("an invalid duration still applied a presence status")
				}
				if len(calls) != 0 {
					t.Errorf("setStatusFn called %d times, want 0", len(calls))
				}
				if cmd == nil {
					t.Error("cmd = nil, want the toast-clear tick")
				}
			},
		},
		{
			// strconv.Atoi succeeds here, so this exercises the
			// `mins <= 0` half of the validation, not the err half.
			name:     "enter on a zero duration is rejected the same way",
			opts:     opts,
			setup:    snoozeSetup("0", &calls),
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := statusbarText(a); !strings.Contains(got, invalidSnoozeToast) {
					t.Errorf("status bar = %q, want it to contain %q", got, invalidSnoozeToast)
				}
				if len(calls) != 0 {
					t.Errorf("setStatusFn called %d times, want 0", len(calls))
				}
				if cmd == nil {
					t.Error("cmd = nil, want the toast-clear tick")
				}
			},
		},
		{
			name:     "backspace deletes one digit and stays in the mode",
			opts:     opts,
			setup:    snoozeSetup("12", &calls),
			key:      keyCode(tea.KeyBackspace),
			wantMode: ModePresenceCustomSnooze,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.presence.SnoozeBuf(); got != "1" {
					t.Errorf("snooze buffer = %q, want %q", got, "1")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			name:     "backspace on an empty buffer is inert",
			opts:     opts,
			setup:    snoozeSetup("", &calls),
			key:      keyCode(tea.KeyBackspace),
			wantMode: ModePresenceCustomSnooze,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.presence.SnoozeBuf(); got != "" {
					t.Errorf("snooze buffer = %q, want empty", got)
				}
			},
		},
		{
			name:     "a digit appends to the buffer",
			opts:     opts,
			setup:    snoozeSetup("1", &calls),
			key:      keyPress('5'),
			wantMode: ModePresenceCustomSnooze,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.presence.SnoozeBuf(); got != "15" {
					t.Errorf("snooze buffer = %q, want %q", got, "15")
				}
			},
		},
		{
			name:     "a non-digit printable key is ignored",
			opts:     opts,
			setup:    snoozeSetup("1", &calls),
			key:      keyPress('x'),
			wantMode: ModePresenceCustomSnooze,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.presence.SnoozeBuf(); got != "1" {
					t.Errorf("snooze buffer = %q, want %q unchanged", got, "1")
				}
			},
		},
		{
			// presenceController.AppendSnoozeDigit caps the buffer at
			// six characters so an absurd minute count cannot be typed.
			name:     "the seventh digit is dropped",
			opts:     opts,
			setup:    snoozeSetup("123456", &calls),
			key:      keyPress('7'),
			wantMode: ModePresenceCustomSnooze,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.presence.SnoozeBuf(); got != "123456" {
					t.Errorf("snooze buffer = %q, want %q unchanged", got, "123456")
				}
			},
		},
		{
			// keyPress(' ') carries Text " " but Key.String() answers
			// "space", which is two bytes, so AppendSnoozeDigit's
			// len(r) != 1 guard drops it. Nothing normalises it here,
			// unlike mode_new_message.go which maps tea.KeySpace to " ".
			name:     "space is dropped, not typed",
			opts:     opts,
			setup:    snoozeSetup("1", &calls),
			key:      keyPress(' '),
			wantMode: ModePresenceCustomSnooze,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.presence.SnoozeBuf(); got != "1" {
					t.Errorf("snooze buffer = %q, want %q unchanged", got, "1")
				}
			},
		},
	})
}
