package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/text"
	"github.com/gammons/slk/internal/ui/mentionpicker"
	"github.com/gammons/slk/internal/ui/messages"
)

// presenceSlashApp is slashApp with the status setter recorded and the
// workspace's cached presence set to presence ("" = no entry).
func presenceSlashApp(t *testing.T, presence string) (*App, *[]statusCall) {
	t.Helper()
	a, _ := slashApp(t, nil)
	if presence != "" {
		a.presence.Set("T1", presence, false, time.Time{})
	}
	statuses := &[]statusCall{}
	a.setStatusSetterForTest(func(action core.PresenceAction, mins int) {
		*statuses = append(*statuses, statusCall{action: action, mins: mins})
	})
	return a, statuses
}

func TestSlashPresence(t *testing.T) {
	cases := []struct {
		name, cached, typed string
		want                []statusCall
		toast               string
	}{
		{"/away when active goes away", "active", "/away", []statusCall{{action: core.PresenceSetAway}}, ""},
		{"/away when away comes back (toggle)", "away", "/away", []statusCall{{action: core.PresenceSetActive}}, ""},
		{"/away with nothing cached goes away", "", "/away", []statusCall{{action: core.PresenceSetAway}}, ""},
		{"/active", "away", "/active", []statusCall{{action: core.PresenceSetActive}}, ""},
		{"/status away", "active", "/status away", []statusCall{{action: core.PresenceSetAway}}, ""},
		{"/status Active is case-insensitive", "away", "/status Active", []statusCall{{action: core.PresenceSetActive}}, ""},
		{"bare /status is unsupported", "active", "/status", nil, "/status isn't supported in slk yet"},
		{"/status with a custom status is unsupported", "active", "/status :palm_tree: on holiday", nil, "/status isn't supported in slk yet"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, statuses := presenceSlashApp(t, tc.cached)
			a.compose.SetValue(tc.typed)
			_ = pressEnter(a)
			if len(*statuses) != len(tc.want) {
				t.Fatalf("SetStatus calls = %+v, want %+v", *statuses, tc.want)
			}
			for i := range tc.want {
				if (*statuses)[i] != tc.want[i] {
					t.Errorf("SetStatus[%d] = %+v, want %+v", i, (*statuses)[i], tc.want[i])
				}
			}
			if tc.toast != "" && !strings.Contains(statusbarText(a), tc.toast) {
				t.Errorf("status bar = %q, want %q", statusbarText(a), tc.toast)
			}
		})
	}
}

func TestSlashAwayUpdatesTheCachedPresence(t *testing.T) {
	a, _ := presenceSlashApp(t, "active")
	a.compose.SetValue("/away")
	_ = pressEnter(a)
	if p, _, _, _ := a.presence.Status("T1"); p != "away" {
		t.Errorf("cached presence = %q, want away", p)
	}
}

// Built-ins run locally because their names are keys of slashCommands:
// dispatchCommand calls a table entry and never the remote fallback
// (TestDispatchCommand_*). This is the guard against a built-in being
// routed to chat.command; extend the list with each new built-in.
func TestSlashBuiltinsAreLocal(t *testing.T) {
	for _, name := range []string{"/away", "/active", "/status", "/open", "/join", "/msg", "/dm", "/leave", "/close", "/part"} {
		if _, ok := slashCommands[name]; !ok {
			t.Errorf("%s is not in slashCommands, so it would run through chat.command", name)
		}
	}
}

// openSlashApp is slashApp with the finder holding joined general (C1)
// and random (C2), a server channel search that answers with unjoined
// design (C9), and joins and searches recorded.
func openSlashApp(t *testing.T) (*App, *[]runCall, *[]string, *[]string) {
	t.Helper()
	a, runs := slashApp(t, nil)
	a.channelFinder.SetItems([]core.ChannelFinderItem{
		{ID: "C1", Name: "general", Type: "channel", Joined: true},
		{ID: "C2", Name: "random", Type: "channel", Joined: true},
	})
	joins := &[]string{}
	searches := &[]string{}
	a.SetChannelService(core.NewChannelService(core.ChannelServiceFuncs{
		Join: func(id ids.ChannelID, name string) core.Msg {
			*joins = append(*joins, string(id))
			return ChannelJoinedMsg{ID: string(id), Name: name}
		},
		SearchRemote: func(query string) []core.ChannelFinderItem {
			*searches = append(*searches, query)
			if text.Fold(query) == "design" {
				return []core.ChannelFinderItem{{ID: "C9", Name: "design", Type: "channel", Joined: false}}
			}
			return nil
		},
	}))
	return a, runs, joins, searches
}

func TestSlashOpen_SelectsAJoinedChannelByName(t *testing.T) {
	a, runs, _, searches := openSlashApp(t)
	a.compose.SetValue("/open #random")
	sel := deliveredMsgs[ChannelSelectedMsg](t, pressEnter(a))
	if len(sel) != 1 || sel[0].ID != "C2" {
		t.Fatalf("ChannelSelectedMsg = %+v, want C2", sel)
	}
	if len(*runs) != 0 {
		t.Errorf("chat.command ran %d times, want 0", len(*runs))
	}
	if len(*searches) != 0 {
		t.Errorf("server search ran for %v, want none for a local hit", *searches)
	}
}

func TestSlashJoin_SelectsByMentionID(t *testing.T) {
	a, _, _, _ := openSlashApp(t)
	a.compose.SetValue("/join <#C2|random>")
	sel := deliveredMsgs[ChannelSelectedMsg](t, pressEnter(a))
	if len(sel) != 1 || sel[0].ID != "C2" {
		t.Fatalf("ChannelSelectedMsg = %+v, want C2", sel)
	}
}

func TestSlashOpen_JoinsAChannelTheUserIsNotIn(t *testing.T) {
	a, _, joins, searches := openSlashApp(t)
	a.compose.SetValue("/open #Design")
	lookups := deliveredMsgs[channelLookupMsg](t, pressEnter(a))
	if len(*searches) != 1 || (*searches)[0] != "Design" {
		t.Fatalf("searches = %v, want [Design]", *searches)
	}
	_, cmd := a.Update(lookups[0])
	_ = deliveredMsgs[ChannelJoinedMsg](t, cmd)
	if len(*joins) != 1 || (*joins)[0] != "C9" {
		t.Fatalf("joins = %v, want [C9]", *joins)
	}
}

// A mention of a channel the finder does not hold resolves by its label,
// through the server search, like a typed name.
func TestSlashOpen_MentionOfAnUnknownChannelSearchesByLabel(t *testing.T) {
	a, _, joins, searches := openSlashApp(t)
	a.compose.SetValue("/open <#C9|design>")
	lookups := deliveredMsgs[channelLookupMsg](t, pressEnter(a))
	if len(*searches) != 1 || (*searches)[0] != "design" {
		t.Fatalf("searches = %v, want [design]", *searches)
	}
	_, cmd := a.Update(lookups[0])
	_ = deliveredMsgs[ChannelJoinedMsg](t, cmd)
	if len(*joins) != 1 || (*joins)[0] != "C9" {
		t.Fatalf("joins = %v, want [C9]", *joins)
	}
}

func TestSlashOpen_LookupForAnotherWorkspaceIsDropped(t *testing.T) {
	a, _, joins, _ := openSlashApp(t)
	a.compose.SetValue("/open #Design")
	lookups := deliveredMsgs[channelLookupMsg](t, pressEnter(a))
	a.activeTeamID = "T2"
	cmd, handled := reduceSlashCommand(a, lookups[0])
	if !handled || cmd != nil {
		t.Fatalf("reduceSlashCommand = (%v, %v), want a dropped answer (nil, true)", cmd, handled)
	}
	if len(*joins) != 0 {
		t.Errorf("joins = %v, want none", *joins)
	}
	if strings.Contains(statusbarText(a), "No channel named") {
		t.Errorf("status bar = %q, want no toast", statusbarText(a))
	}
}

// The composer was cleared at Enter, so a lookup dropped after a
// workspace switch gives the command back as the draft of the
// conversation it was typed in, leaving the new workspace's composer alone.
func TestSlashOpen_LookupForAnotherWorkspaceGivesTheTextBack(t *testing.T) {
	a, _, _, _ := openSlashApp(t)
	a.compose.SetValue("/open #Design")
	lookups := deliveredMsgs[channelLookupMsg](t, pressEnter(a))
	a.activeTeamID = "T2"
	a.compose.SetDraftContext("T2", "C5", "")
	a.Update(lookups[0])
	if got := a.compose.Value(); got != "" {
		t.Errorf("T2 composer = %q, want untouched", got)
	}
	a.compose.SetDraftContext("T1", "C1", "")
	if got := a.compose.Value(); got != "/open #Design" {
		t.Errorf("back in T1/C1: composer = %q, want the command", got)
	}
}

func TestSlashOpen_ErrorsToast(t *testing.T) {
	cases := []struct {
		typed, want string
		lookup      bool
	}{
		{"/open #nope", "No channel named #nope", true},
		{"/open", "Usage: /open #channel", false},
		{"/join", "Usage: /join #channel", false},
		{"/open <#C8|nope>", "No channel named #nope", true},
	}
	for _, tc := range cases {
		t.Run(tc.typed, func(t *testing.T) {
			a, _, _, _ := openSlashApp(t)
			a.compose.SetValue(tc.typed)
			cmd := pressEnter(a)
			if tc.lookup {
				lookups := deliveredMsgs[channelLookupMsg](t, cmd)
				_, cmd = a.Update(lookups[0])
			}
			_ = cmd
			if !strings.Contains(statusbarText(a), tc.want) {
				t.Errorf("status bar = %q, want %q", statusbarText(a), tc.want)
			}
		})
	}
}

func TestMatchChannelByName(t *testing.T) {
	items := []core.ChannelFinderItem{
		{ID: "D7", Name: "design", Type: "dm"},
		{ID: "T0", Name: "threads", Type: "channel", Synthetic: true},
		{ID: "D2", Name: "alice", Type: "dm"},
		{ID: "G1", Name: "secret", Type: "private"},
		{ID: "C9", Name: "design", Type: "channel"},
		{ID: "C1", Name: "general", Type: "channel", Joined: true},
	}
	cases := []struct {
		name, query, wantID string
	}{
		{"exact", "general", "C1"},
		{"case-insensitive", "Design", "C9"},
		{"accent-insensitive", "Désign", "C9"},
		{"leading # ignored", "#design", "C9"},
		{"a DM with the same name is skipped", "design", "C9"},
		{"private channel matches", "secret", "G1"},
		{"synthetic destination is skipped", "threads", ""},
		{"DM-only name is a miss", "alice", ""},
		{"miss", "nope", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := matchChannelByName(items, tc.query)
			if tc.wantID == "" {
				if ok {
					t.Errorf("matchChannelByName(%q) = %+v, want no match", tc.query, got)
				}
				return
			}
			if !ok || got.ID != tc.wantID {
				t.Errorf("matchChannelByName(%q) = (%+v, %v), want %s", tc.query, got, ok, tc.wantID)
			}
		})
	}
}

// msgSlashApp is slashApp whose conversations.open is recorded and
// answers with DM D9.
func msgSlashApp(t *testing.T) (*App, *[]runCall, *[][]string) {
	t.Helper()
	a, runs := slashApp(t, nil)
	opens := &[][]string{}
	wireOpenConversation(a, func(userIDs []string, reqID uint64) tea.Cmd {
		*opens = append(*opens, userIDs)
		return func() tea.Msg {
			return NewMessageOpenedMsg{ChannelID: "D9", UserIDs: userIDs, RequestID: reqID, AlreadyOpen: true}
		}
	})
	return a, runs, opens
}

// draftIn returns compose's draft for (T1, channelID) by switching to it.
func draftIn(a *App, channelID string) string {
	a.compose.SetDraftContext("T1", channelID, "")
	return a.compose.Value()
}

func TestSlashMsg_OpensTheDMAndPrefills(t *testing.T) {
	a, runs, opens := msgSlashApp(t)
	a.compose.SetValue("/msg <@U9> are you free\nat 3?")
	opened := deliveredMsgs[NewMessageOpenedMsg](t, pressEnter(a))
	if len(*opens) != 1 || strings.Join((*opens)[0], ",") != "U9" {
		t.Fatalf("OpenConversation calls = %v, want [[U9]]", *opens)
	}
	a.Update(opened[0])
	if got := draftIn(a, "D9"); got != "are you free\nat 3?" {
		t.Errorf("D9 draft = %q, want the /msg text", got)
	}
	if len(*runs) != 0 {
		t.Errorf("chat.command ran %d times, want 0", len(*runs))
	}
}

func TestSlashMsg_TypedMentionIsTranslated(t *testing.T) {
	a, _, opens := msgSlashApp(t)
	a.compose.SetUsers([]mentionpicker.User{{ID: "U9", DisplayName: "alice", Username: "alice", InChannel: true}})
	a.compose.SetValue("/msg @alice hi")
	_ = deliveredMsgs[NewMessageOpenedMsg](t, pressEnter(a))
	if len(*opens) != 1 || (*opens)[0][0] != "U9" {
		t.Fatalf("OpenConversation calls = %v, want [[U9]]", *opens)
	}
}

func TestSlashDM_WithoutTextOnlyOpens(t *testing.T) {
	a, _, opens := msgSlashApp(t)
	a.compose.SetValue("/dm <@U9>")
	opened := deliveredMsgs[NewMessageOpenedMsg](t, pressEnter(a))
	a.Update(opened[0])
	if len(*opens) != 1 {
		t.Fatalf("OpenConversation calls = %v, want one", *opens)
	}
	if got := draftIn(a, "D9"); got != "" {
		t.Errorf("D9 draft = %q, want none", got)
	}
}

func TestSlashMsg_KeepsAnExistingDraft(t *testing.T) {
	a, _, _ := msgSlashApp(t)
	a.compose.SetDraftContext("T1", "D9", "")
	a.compose.SetValue("older draft")
	a.compose.SetDraftContext("T1", "C1", "")
	a.compose.SetValue("/msg <@U9> hi")
	opened := deliveredMsgs[NewMessageOpenedMsg](t, pressEnter(a))
	_, cmd := a.Update(opened[0])
	toasts := deliveredMsgs[ToastMsg](t, cmd)
	if len(toasts) != 1 || !strings.Contains(toasts[0].Text, "Kept your existing draft") {
		t.Errorf("toasts = %+v, want the kept-draft notice", toasts)
	}
	if got := draftIn(a, "D9"); got != "older draft" {
		t.Errorf("D9 draft = %q, want the older draft kept", got)
	}
}

// A discarded command never runs, so these tests assert only what is
// set synchronously: the status bar and the OpenConversation calls.
func TestSlashMsg_UsageErrors(t *testing.T) {
	cases := map[string]string{
		"/msg":             "Usage: /msg @someone [message]",
		"/msg hello there": "Usage: /msg @someone [message]",
		"/msg @nobody hi":  "No one called @nobody",
		"/dm <#C2|random>": "Usage: /dm @someone [message]",
	}
	for typed, want := range cases {
		t.Run(typed, func(t *testing.T) {
			a, _, opens := msgSlashApp(t)
			a.compose.SetValue(typed)
			_ = pressEnter(a)
			if !strings.Contains(statusbarText(a), want) {
				t.Errorf("status bar = %q, want %q", statusbarText(a), want)
			}
			if len(*opens) != 0 {
				t.Errorf("OpenConversation calls = %v, want none", *opens)
			}
		})
	}
}

// failOpenConversation makes the App's conversations.open fail.
func failOpenConversation(a *App) {
	wireOpenConversation(a, func(_ []string, reqID uint64) tea.Cmd {
		return func() tea.Msg {
			return NewMessageFailedMsg{RequestID: reqID, Err: errors.New("rate_limited")}
		}
	})
}

func TestSlashMsg_FailedOpenGivesTheTextBack(t *testing.T) {
	a, _, _ := msgSlashApp(t)
	failOpenConversation(a)
	a.compose.SetValue("/msg <@U9> are you free")
	failed := deliveredMsgs[NewMessageFailedMsg](t, pressEnter(a))
	if len(failed) != 1 {
		t.Fatalf("NewMessageFailedMsg delivered %d times, want 1", len(failed))
	}
	_, cmd := a.Update(failed[0])
	if got := a.compose.Value(); got != "/msg <@U9> are you free" {
		t.Errorf("compose = %q, want the typed command back", got)
	}
	toasts := deliveredMsgs[ToastMsg](t, cmd)
	if len(toasts) != 1 || !strings.Contains(toasts[0].Text, "Open DM failed") {
		t.Errorf("toasts = %+v, want the open-failed notice", toasts)
	}
}

func TestSlashMsg_FailedOpenKeepsNewerText(t *testing.T) {
	a, _, _ := msgSlashApp(t)
	failOpenConversation(a)
	a.compose.SetValue("/msg <@U9> hi")
	failed := deliveredMsgs[NewMessageFailedMsg](t, pressEnter(a))
	a.compose.SetValue("typed while it opened")
	a.Update(failed[0])
	if got := a.compose.Value(); got != "typed while it opened" {
		t.Errorf("compose = %q, want the newer text kept", got)
	}
}

// A failed /msg must not leave its pre-fill behind for the next
// conversation the new-message picker opens.
func TestSlashMsg_FailedOpenDoesNotPrefillThePickersNextOpen(t *testing.T) {
	a, _, _ := msgSlashApp(t)
	failOpenConversation(a)
	a.compose.SetValue("/msg <@U9> are you free")
	failed := deliveredMsgs[NewMessageFailedMsg](t, pressEnter(a))
	a.Update(failed[0])

	wireOpenConversation(a, func(userIDs []string, reqID uint64) tea.Cmd {
		return func() tea.Msg {
			return NewMessageOpenedMsg{ChannelID: "D9", UserIDs: userIDs, RequestID: reqID, AlreadyOpen: true}
		}
	})
	opened := deliveredMsgs[NewMessageOpenedMsg](t, a.openConversation([]string{"U9"}, msgPrefill{}))
	a.Update(opened[0])
	if got := draftIn(a, "D9"); got != "" {
		t.Errorf("D9 draft = %q, want none", got)
	}
}

// A built-in that does not run gives the text as typed back to the
// composer it came from, with the toast.
func TestSlashRefusalsGiveTheTextBack(t *testing.T) {
	cases := []struct {
		typed, toast string
		lookup       bool
	}{
		{"/msg hello there", "Usage: /msg @someone [message]", false},
		{"/msg @nobody hi", "No one called @nobody", false},
		{"/leave", "/leave isn't supported in slk yet", false},
		{"/status :palm_tree: on holiday", "/status isn't supported in slk yet", false},
		{"/open", "Usage: /open #channel", false},
		{"/open #nope", "No channel named #nope", true},
	}
	for _, tc := range cases {
		t.Run(tc.typed, func(t *testing.T) {
			a, _, _, _ := openSlashApp(t)
			a.compose.SetValue(tc.typed)
			cmd := pressEnter(a)
			if tc.lookup {
				lookups := deliveredMsgs[channelLookupMsg](t, cmd)
				a.Update(lookups[0])
			}
			if got := a.compose.Value(); got != tc.typed {
				t.Errorf("compose = %q, want the typed text back", got)
			}
			if !strings.Contains(statusbarText(a), tc.toast) {
				t.Errorf("status bar = %q, want %q", statusbarText(a), tc.toast)
			}
		})
	}
}

func TestSlashRefusalKeepsANewerDraft(t *testing.T) {
	a, _, _, _ := openSlashApp(t)
	a.compose.SetValue("/open #nope")
	lookups := deliveredMsgs[channelLookupMsg](t, pressEnter(a))
	a.compose.SetValue("typed while it looked")
	a.Update(lookups[0])
	if got := a.compose.Value(); got != "typed while it looked" {
		t.Errorf("compose = %q, want the newer text kept", got)
	}
	if !strings.Contains(statusbarText(a), "No channel named #nope") {
		t.Errorf("status bar = %q, want the toast", statusbarText(a))
	}
}

// An action dialog waiting on the user's typing opens when /msg's Enter
// clears the composer. The conversation it opens must not pull the user
// back into insert mode underneath the dialog.
func TestSlashMsg_OpenedConversationDoesNotEnterInsertUnderADialog(t *testing.T) {
	a, _, _ := msgSlashApp(t)
	a.messagepane.SetMessages([]messages.MessageItem{mentionEphemeral(ephTS)})
	a.compose.SetValue("/msg <@U9> hi")
	_ = a.enqueueActionPrompt(actionPromptRef{channelID: "C1", ts: ephTS})
	if a.shownPrompt != nil {
		t.Fatal("precondition: the dialog must wait while the user types")
	}

	opened := deliveredMsgs[NewMessageOpenedMsg](t, pressEnter(a))
	if a.shownPrompt == nil || a.mode != ModeConfirm {
		t.Fatalf("precondition: shownPrompt = %v, mode = %v; want the dialog open", a.shownPrompt, a.mode)
	}
	_, cmd := a.Update(opened[0])
	if a.shownPrompt != nil && a.mode == ModeInsert {
		t.Errorf("mode = ModeInsert with the action dialog still shown")
	}

	// The switch to the opened conversation takes the dialog's message
	// off screen; closing the dialog then lands the user in insert mode.
	for _, sel := range deliveredMsgs[ChannelSelectedMsg](t, cmd) {
		a.Update(sel)
	}
	if a.shownPrompt != nil {
		t.Fatalf("the dialog is still shown after switching to the DM")
	}
	if a.mode != ModeInsert {
		t.Errorf("mode = %v after the dialog closed, want ModeInsert in the opened DM", a.mode)
	}
}

func TestSlashBuiltin_FromTheThreadComposerOpensTheChannel(t *testing.T) {
	a, _, _, _ := openSlashApp(t)
	showThread(t, a)
	a.threadCompose.SetValue("/open #random")
	sel := deliveredMsgs[ChannelSelectedMsg](t, pressEnter(a))
	if len(sel) != 1 || sel[0].ID != "C2" {
		t.Fatalf("ChannelSelectedMsg = %+v, want C2", sel)
	}
	if got := a.threadCompose.Value(); got != "" {
		t.Errorf("thread compose = %q, want cleared", got)
	}
}

func TestSlashBuiltin_RefusalFromTheThreadComposerGivesTheTextBackThere(t *testing.T) {
	a, _, _, _ := openSlashApp(t)
	showThread(t, a)
	a.threadCompose.SetDraftContext("T1", "C1", "10.0")
	a.threadCompose.SetValue("/leave")
	_ = pressEnter(a)
	if got := a.threadCompose.Value(); got != "/leave" {
		t.Errorf("thread compose = %q, want the typed text back", got)
	}
	if got := a.compose.Value(); got != "" {
		t.Errorf("channel compose = %q, want untouched", got)
	}
	if !strings.Contains(statusbarText(a), "/leave isn't supported in slk yet") {
		t.Errorf("status bar = %q, want the toast", statusbarText(a))
	}
}

func TestSlashUnsupportedBuiltins(t *testing.T) {
	for _, name := range []string{"/leave", "/close", "/part"} {
		t.Run(name, func(t *testing.T) {
			a, _ := slashApp(t, nil)
			a.compose.SetValue(name)
			_ = pressEnter(a)
			if want := name + " isn't supported in slk yet"; !strings.Contains(statusbarText(a), want) {
				t.Errorf("status bar = %q, want %q", statusbarText(a), want)
			}
		})
	}
}

// A second /msg while the first conversation is still opening is
// refused and given back, rather than overwriting the first's text; the
// first still opens with its own text.
func TestSlashMsg_SecondWhileOpeningIsRefused(t *testing.T) {
	a, _, opens := msgSlashApp(t)
	a.compose.SetValue("/msg <@U9> one")
	first := pressEnter(a)

	a.SetMode(ModeInsert)
	_ = a.compose.Focus()
	a.compose.SetValue("/msg <@U8> two")
	_ = pressEnter(a)
	if len(*opens) != 1 {
		t.Fatalf("OpenConversation calls = %v, want only the first", *opens)
	}
	if got := a.compose.Value(); got != "/msg <@U8> two" {
		t.Errorf("compose = %q, want the refused command back", got)
	}
	if out := statusbarText(a); !strings.Contains(out, "Still opening a conversation") {
		t.Errorf("status bar = %q, want the still-opening toast", out)
	}

	opened := deliveredMsgs[NewMessageOpenedMsg](t, first)
	a.Update(opened[0])
	if got := draftIn(a, "D9"); got != "one" {
		t.Errorf("D9 draft = %q, want the first command's text", got)
	}
}

// A /msg whose open the new-message picker supersedes gets its text back.
func TestSlashMsg_SupersededByThePickerGivesTheTextBack(t *testing.T) {
	a, _, _ := msgSlashApp(t)
	a.compose.SetValue("/msg <@U9> hi")
	_ = pressEnter(a)
	_ = a.openConversation([]string{"U2"}, msgPrefill{})
	if got := a.compose.Value(); got != "/msg <@U9> hi" {
		t.Errorf("compose = %q, want the superseded command back", got)
	}
}

// Esc in the new-message picker cancels whatever open is in flight; a
// /msg's text comes back rather than vanishing with its dropped result.
func TestSlashMsg_PickerEscGivesTheTextBack(t *testing.T) {
	a, _, _ := msgSlashApp(t)
	a.compose.SetValue("/msg <@U9> hi")
	_ = pressEnter(a)
	a.newMessagePicker.Open()
	a.SetMode(ModeNewMessage)
	_ = handleNewMessageMode(a, keyCode(tea.KeyEscape))
	if got := a.compose.Value(); got != "/msg <@U9> hi" {
		t.Errorf("compose = %q, want the cancelled command back", got)
	}
}

// Request IDs are never reused: a late result of an old request must
// not pass as the current one after the counter would have reset.
func TestOpenConversation_RequestIDsAreNeverReused(t *testing.T) {
	a, _, _ := msgSlashApp(t)
	var reqs []uint64
	wireOpenConversation(a, func(userIDs []string, reqID uint64) tea.Cmd {
		reqs = append(reqs, reqID)
		return func() tea.Msg {
			return NewMessageOpenedMsg{ChannelID: "D9", UserIDs: userIDs, RequestID: reqID, AlreadyOpen: true}
		}
	})
	_ = a.openConversation([]string{"U1"}, msgPrefill{})
	b := a.openConversation([]string{"U2"}, msgPrefill{})
	a.Update(deliveredMsgs[NewMessageOpenedMsg](t, b)[0])
	_ = a.openConversation([]string{"U3"}, msgPrefill{})
	if reqs[2] == reqs[0] {
		t.Fatalf("request IDs %v: the third reuses the first's", reqs)
	}
	if newMessageResultIsCurrent(a, reqs[0]) {
		t.Errorf("a late result of request %d passes as current", reqs[0])
	}
}

// With stay_in_insert_after_send on, /open from the thread composer keeps
// insert mode, but the switch closes the thread: the channel composer must
// take over, so what is typed next lands in it.
func TestSlashOpen_FromThreadComposerKeepsTyping(t *testing.T) {
	a, _, _, _ := openSlashApp(t)
	a.SetStayInInsertAfterSend(true)
	a.SetMode(ModeInsert)
	showThread(t, a)
	a.threadCompose.SetValue("/open #random")

	for _, m := range deliveredMsgs[ChannelSelectedMsg](t, pressEnter(a)) {
		a.Update(m)
	}
	if a.activeChannelID != "C2" || a.threadVisible {
		t.Fatalf("active = %q, thread visible = %v; want #random with the thread closed", a.activeChannelID, a.threadVisible)
	}
	a.Update(keyPress('x'))
	if a.mode != ModeInsert || a.compose.Value() != "x" {
		t.Errorf("mode = %v, compose = %q; want INSERT with the typed x in the channel composer", a.mode, a.compose.Value())
	}
}

// The same switch into a channel already known to be read-only drops
// insert mode instead: the channel composer that would take over is
// disabled.
func TestSlashOpen_FromThreadComposerIntoReadOnlyChannelLeavesInsert(t *testing.T) {
	a, _, _, _ := openSlashApp(t)
	a.SetStayInInsertAfterSend(true)
	a.readOnlyChannels = map[string]bool{"C2": true}
	a.SetMode(ModeInsert)
	showThread(t, a)
	a.threadCompose.SetValue("/open #random")

	for _, m := range deliveredMsgs[ChannelSelectedMsg](t, pressEnter(a)) {
		a.Update(m)
	}
	if a.activeChannelID != "C2" || !a.composeDisabled {
		t.Fatalf("active = %q, composeDisabled = %v; want read-only #random", a.activeChannelID, a.composeDisabled)
	}
	if a.mode == ModeInsert {
		t.Error("insert mode carried into a disabled channel composer")
	}
}
