package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/mentionpicker"
	"github.com/gammons/slk/internal/ui/messages"
)

func TestParseSlashCommand(t *testing.T) {
	cases := []struct {
		in         string
		name, args string
		ok         bool
	}{
		{"/giphy cats", "/giphy", "cats", true},
		{"/giphy", "/giphy", "", true},
		{"/GIPHY cats", "/giphy", "cats", true},                             // name is lowercased
		{"/giphy   ", "/giphy", "", true},                                   // trailing spaces: no args
		{"/giphy \t\n ", "/giphy", "", true},                                // whitespace-only args
		{"/shrug line one\nline two", "/shrug", "line one\nline two", true}, // newline kept in args
		{"/giphy\ncats", "/giphy", "cats", true},
		{"/my-cmd_2 x", "/my-cmd_2", "x", true},
		{"/remind me  to  stretch", "/remind", "me  to  stretch", true}, // inner spacing kept
		{"/giphy\u00a0cats", "/giphy", "cats", true},                    // pasted non-breaking space separates too
		{"/giphy\u3000\u00a0cats", "/giphy", "cats", true},              // any Unicode space, any run of them
		{"/giphy\u00a0", "/giphy", "", true},                            // only Unicode space after the name: no args
		{" /giphy cats", "", "", false},                                 // a leading space escapes: sent as text
		{"/tmp/foo is full", "", "", false},
		{"/ hello", "", "", false},
		{"/", "", "", false},
		{"/giphy: cats", "", "", false},
		{"hello /giphy", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range cases {
		name, args, ok := parseSlashCommand(tc.in)
		if name != tc.name || args != tc.args || ok != tc.ok {
			t.Errorf("parseSlashCommand(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.in, name, args, ok, tc.name, tc.args, tc.ok)
		}
	}
}

// runCall records one CommandService.Run.
type runCall struct {
	teamID, channelID, threadTS, name, args string
}

// slashApp is an App in insert mode on C1 in team T1, its channel
// composer bound to (T1, C1), whose command runs are recorded and
// answered with err.
func slashApp(t *testing.T, err error) (*App, *[]runCall) {
	t.Helper()
	a := newTestApp(t, append(insertOpts(), withActiveTeam("T1"), withMode(ModeInsert))...)
	a.focusedPanel = PanelMessages
	a.compose.SetDraftContext("T1", "C1", "")
	calls := &[]runCall{}
	a.setCommandRunnerForTest(func(team string, ch ids.ChannelID, th ids.ThreadTS, name, args string) error {
		*calls = append(*calls, runCall{team, string(ch), string(th), name, args})
		return err
	})
	return a, calls
}

// pressEnter sends Enter through the real Update chain.
func pressEnter(a *App) tea.Cmd {
	_, cmd := a.Update(keyCode(tea.KeyEnter))
	return cmd
}

func TestSlashCommand_ChannelComposerRunsRemote(t *testing.T) {
	a, calls := slashApp(t, nil)
	a.compose.SetValue("/giphy cats")

	cmd := pressEnter(a)
	ran := deliveredMsgs[commandRanMsg](t, cmd)
	if len(ran) != 1 {
		t.Fatalf("commandRanMsg delivered %d times, want 1", len(ran))
	}
	want := runCall{teamID: "T1", channelID: "C1", threadTS: "", name: "/giphy", args: "cats"}
	if len(*calls) != 1 || (*calls)[0] != want {
		t.Fatalf("Run calls = %+v, want [%+v]", *calls, want)
	}
	if got := a.compose.Value(); got != "" {
		t.Errorf("compose = %q, want cleared", got)
	}
	if a.mode != ModeNormal {
		t.Errorf("mode = %v, want ModeNormal (left insert like a send)", a.mode)
	}
}

// The command runs in the workspace it was typed in, even when the user
// switches workspace before the run starts: the channel ID belongs to
// that workspace.
func TestSlashCommand_RunsInTheWorkspaceItWasTypedIn(t *testing.T) {
	a, calls := slashApp(t, nil)
	a.compose.SetValue("/giphy cats")
	cmd := pressEnter(a)
	a.activeTeamID = "T2"
	_ = deliveredMsgs[commandRanMsg](t, cmd)
	if len(*calls) != 1 || (*calls)[0].teamID != "T1" {
		t.Fatalf("Run calls = %+v, want one in T1", *calls)
	}
}

// With stay_in_insert_after_send on, a slash command leaves the composer
// focused in insert mode like a plain send, in either composer.
func TestSlashCommand_StaysInInsertWhenOptionOn(t *testing.T) {
	for _, thread := range []bool{false, true} {
		a, calls := slashApp(t, nil)
		a.SetStayInInsertAfterSend(true)
		c := &a.compose
		if thread {
			showThread(t, a)
			c = &a.threadCompose
		}
		c.SetValue("/giphy cats")

		cmd := pressEnter(a)
		_ = deliveredMsgs[commandRanMsg](t, cmd)
		if len(*calls) != 1 {
			t.Fatalf("thread=%v: Run calls = %+v, want one", thread, *calls)
		}
		if a.mode != ModeInsert || c.Value() != "" {
			t.Errorf("thread=%v: mode = %v, compose = %q; want ModeInsert and an emptied composer", thread, a.mode, c.Value())
		}
	}
}

func TestSlashCommand_ThreadComposerRunsInThread(t *testing.T) {
	a, calls := slashApp(t, nil)
	showThread(t, a)
	a.threadCompose.SetValue("/giphy cats")

	cmd := pressEnter(a)
	_ = deliveredMsgs[commandRanMsg](t, cmd)
	want := runCall{teamID: "T1", channelID: "C1", threadTS: "10.0", name: "/giphy", args: "cats"}
	if len(*calls) != 1 || (*calls)[0] != want {
		t.Fatalf("Run calls = %+v, want [%+v]", *calls, want)
	}
	if got := a.threadCompose.Value(); got != "" {
		t.Errorf("thread compose = %q, want cleared", got)
	}
}

func TestSlashCommand_UpperCaseNameRunsLowercased(t *testing.T) {
	a, calls := slashApp(t, nil)
	a.compose.SetValue("/GIPHY cats")
	_ = deliveredMsgs[commandRanMsg](t, pressEnter(a))
	if len(*calls) != 1 || (*calls)[0].name != "/giphy" {
		t.Fatalf("Run calls = %+v, want one /giphy", *calls)
	}
}

func TestSlashCommand_MultiLineArgsKeepNewline(t *testing.T) {
	a, calls := slashApp(t, nil)
	a.compose.SetValue("/shrug line one\nline two")
	_ = deliveredMsgs[commandRanMsg](t, pressEnter(a))
	if len(*calls) != 1 || (*calls)[0].args != "line one\nline two" {
		t.Fatalf("Run calls = %+v, want args with the newline", *calls)
	}
}

func TestSlashCommand_TrailingSpacesMeanNoArgs(t *testing.T) {
	a, calls := slashApp(t, nil)
	a.compose.SetValue("/giphy   ")
	_ = deliveredMsgs[commandRanMsg](t, pressEnter(a))
	if len(*calls) != 1 || (*calls)[0].args != "" {
		t.Fatalf("Run calls = %+v, want args \"\"", *calls)
	}
}

func TestSlashCommand_LeadingSpaceSendsAsMessage(t *testing.T) {
	a, calls := slashApp(t, nil)
	a.compose.SetValue(" /giphy cats")
	sent := deliveredMsgs[SendMessageMsg](t, pressEnter(a))
	if len(sent) != 1 || sent[0].Text != " /giphy cats" {
		t.Fatalf("SendMessageMsg = %+v, want the literal text", sent)
	}
	if len(*calls) != 0 {
		t.Errorf("Run called %d times, want 0", len(*calls))
	}
}

func TestSlashCommand_PathLikeTextSendsAsMessage(t *testing.T) {
	a, calls := slashApp(t, nil)
	a.compose.SetValue("/tmp/foo is full")
	sent := deliveredMsgs[SendMessageMsg](t, pressEnter(a))
	if len(sent) != 1 {
		t.Fatalf("SendMessageMsg delivered %d times, want 1", len(sent))
	}
	if len(*calls) != 0 {
		t.Errorf("Run called %d times, want 0", len(*calls))
	}
}

func TestSlashCommand_EditModeSavesLiterally(t *testing.T) {
	a, calls := slashApp(t, nil)
	a.editing.Begin("C1", "1.0", PanelMessages, "")
	a.compose.SetValue("/giphy cats")
	edits := deliveredMsgs[EditMessageMsg](t, pressEnter(a))
	if len(edits) != 1 || edits[0].NewText != "/giphy cats" {
		t.Fatalf("EditMessageMsg = %+v, want the literal edit", edits)
	}
	if len(*calls) != 0 {
		t.Errorf("Run called %d times, want 0", len(*calls))
	}
}

func TestSlashCommand_AttachmentsRefused(t *testing.T) {
	a, _ := slashApp(t, nil)
	a.compose.AddAttachment(core.PendingAttachment{Filename: "cat.png", Bytes: []byte{1}})
	a.compose.SetValue("/giphy cats")
	_ = pressEnter(a)
	if got := a.compose.Value(); got != "/giphy cats" {
		t.Errorf("compose = %q, want the text kept", got)
	}
	if n := len(a.compose.Attachments()); n != 1 {
		t.Errorf("attachments = %d, want 1 kept", n)
	}
	if out := statusbarText(a); !strings.Contains(out, "Files can't be sent with a command") {
		t.Errorf("status bar = %q, want the refusal toast", out)
	}
}

func TestSlashCommand_FailureToasts(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&core.CommandError{Code: "unknown_command"}, "/giphy is not a valid command"},
		{&core.CommandError{Code: "restricted_action"}, "/giphy failed: restricted_action"},
		{errors.ErrUnsupported, "/giphy failed: not available"},
		{errors.New("calling chat.command: timeout"), "/giphy failed: calling chat.command: timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			a, _ := slashApp(t, tc.err)
			a.compose.SetValue("/giphy cats")
			ran := deliveredMsgs[commandRanMsg](t, pressEnter(a))
			a.Update(ran[0])
			if out := statusbarText(a); !strings.Contains(out, tc.want) {
				t.Errorf("status bar = %q, want %q", out, tc.want)
			}
		})
	}
}

// The GIF a command posts arrives with user = you and bot_id = the app
// (ws_posted_gif.json). A command never marks the channel in flight, so
// reduceNewMessage's self-send guards must not swallow it.
func TestSlashCommand_PostedResultIsNotSwallowedAsSelfSend(t *testing.T) {
	a, _ := slashApp(t, nil)
	a.currentUserID = "U0EXAMPLE01"
	a.compose.SetValue("/giphy cats")
	ran := deliveredMsgs[commandRanMsg](t, pressEnter(a))
	a.Update(ran[0])
	a.Update(NewMessageMsg{ChannelID: "C1", Message: messages.MessageItem{
		TS: "99.000100", UserID: "U0EXAMPLE01", BotID: "B0EXAMPLE01", Text: "cats",
	}})
	msgs := a.messagepane.Messages()
	if len(msgs) == 0 {
		t.Fatal("no messages shown, want the posted GIF 99.000100")
	}
	if last := msgs[len(msgs)-1]; last.TS != "99.000100" {
		t.Fatalf("last message = %+v, want the posted GIF 99.000100", last)
	}
}

func TestSlashCommand_SuccessIsSilent(t *testing.T) {
	a, _ := slashApp(t, nil)
	a.compose.SetValue("/giphy cats")
	ran := deliveredMsgs[commandRanMsg](t, pressEnter(a))
	a.Update(ran[0])
	if out := statusbarText(a); strings.Contains(out, "/giphy") {
		t.Errorf("status bar = %q, want no toast on success", out)
	}
}

func TestSlashCommand_FailurePutsTextBack(t *testing.T) {
	a, _ := slashApp(t, &core.CommandError{Code: "unknown_command"})
	a.compose.SetValue("/notarealcommand hi")
	ran := deliveredMsgs[commandRanMsg](t, pressEnter(a))
	a.Update(ran[0])
	if got := a.compose.Value(); got != "/notarealcommand hi" {
		t.Errorf("compose = %q, want the command put back", got)
	}
}

func TestSlashCommand_FailurePutsTypedFormBack(t *testing.T) {
	// The composer shows what the user typed (@alice), not the wire
	// form (<@U9>) the command was run with.
	a, calls := slashApp(t, &core.CommandError{Code: "restricted_action"})
	a.compose.SetUsers([]mentionpicker.User{{ID: "U9", DisplayName: "alice", Username: "alice", InChannel: true}})
	a.compose.SetValue("/invite @alice")
	ran := deliveredMsgs[commandRanMsg](t, pressEnter(a))
	if (*calls)[0].args != "<@U9>" {
		t.Fatalf("args = %q, want the wire form <@U9>", (*calls)[0].args)
	}
	a.Update(ran[0])
	if got := a.compose.Value(); got != "/invite @alice" {
		t.Errorf("compose = %q, want the typed form", got)
	}
}

func TestSlashCommand_FailureKeepsNewerText(t *testing.T) {
	a, _ := slashApp(t, &core.CommandError{Code: "unknown_command"})
	a.compose.SetValue("/notarealcommand")
	ran := deliveredMsgs[commandRanMsg](t, pressEnter(a))
	a.compose.SetValue("typed while it ran")
	a.Update(ran[0])
	if got := a.compose.Value(); got != "typed while it ran" {
		t.Errorf("compose = %q, want the newer text kept", got)
	}
}

// The user moved to another conversation before the failure landed. The
// command becomes C1's stored draft; C2's composer is untouched.
func TestSlashCommand_FailureAfterMovingAwayStoresTheDraft(t *testing.T) {
	a, _ := slashApp(t, &core.CommandError{Code: "unknown_command"})
	a.compose.SetValue("/notarealcommand")
	ran := deliveredMsgs[commandRanMsg](t, pressEnter(a))
	a.compose.SetDraftContext("T1", "C2", "")
	a.Update(ran[0])
	if got := a.compose.Value(); got != "" {
		t.Errorf("C2 compose = %q, want untouched", got)
	}
	a.compose.SetDraftContext("T1", "C1", "")
	if got := a.compose.Value(); got != "/notarealcommand" {
		t.Errorf("back in C1: compose = %q, want the stored command", got)
	}
}

func TestSlashCommand_ThreadFailurePutsTextBackInThread(t *testing.T) {
	a, _ := slashApp(t, &core.CommandError{Code: "unknown_command"})
	showThread(t, a)
	a.threadCompose.SetDraftContext("T1", "C1", "10.0")
	a.threadCompose.SetValue("/notarealcommand")
	ran := deliveredMsgs[commandRanMsg](t, pressEnter(a))
	a.Update(ran[0])
	if got := a.threadCompose.Value(); got != "/notarealcommand" {
		t.Errorf("thread compose = %q, want the command put back", got)
	}
	if got := a.compose.Value(); got != "" {
		t.Errorf("channel compose = %q, want untouched", got)
	}
}

// withSlashBuiltin registers fn under name for one test.
func withSlashBuiltin(t *testing.T, name string, fn commandFunc) {
	t.Helper()
	prev, had := slashCommands[name]
	slashCommands[name] = fn
	t.Cleanup(func() {
		if had {
			slashCommands[name] = prev
		} else {
			delete(slashCommands, name)
		}
	})
}

func TestSlashCommand_BuiltinRunsInsteadOfRemote(t *testing.T) {
	a, _ := slashApp(t, nil)
	var got commandArgs
	withSlashBuiltin(t, "/testbuiltin", func(_ *App, args commandArgs) tea.Cmd {
		got = args
		return nil
	})
	a.compose.SetValue("/TestBuiltin  hello\nworld")
	_ = pressEnter(a)
	if got.Rest != "hello\nworld" {
		t.Errorf("built-in Rest = %q, want \"hello\\nworld\"", got.Rest)
	}
	if a.compose.Value() != "" || a.mode != ModeNormal {
		t.Errorf("compose = %q, mode = %v; want cleared and ModeNormal", a.compose.Value(), a.mode)
	}
}
