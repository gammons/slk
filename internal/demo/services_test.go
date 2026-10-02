package demo

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui"
)

// testDemo uses the themes scenario, which has only the base rules.
func testDemo(t *testing.T) *Demo {
	t.Helper()
	d, err := newDemo("themes", testNow, fixedClock)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func nextEvent(t *testing.T, d *Demo) Event {
	t.Helper()
	select {
	case ev := <-d.director.events:
		return ev
	default:
		t.Fatal("no event was reported to the director")
		return Event{}
	}
}

func TestChannelFetchMarksReadAndReportsTheOpen(t *testing.T) {
	d := testDemo(t)
	s := d.services()
	before := d.world.readStates()[chEngineering]
	if !before.HasUnread {
		t.Fatal("fixture: #engineering should start unread")
	}
	msg, ok := s.channels.Fetch(ids.ChannelID(chEngineering), "engineering").(ui.MessagesLoadedMsg)
	if !ok || len(msg.Messages) == 0 {
		t.Fatalf("Fetch = %#v", msg)
	}
	if msg.LastReadTS != before.LastReadTS {
		t.Errorf("LastReadTS = %q, want the pre-open cursor %q (it places the new-messages line)", msg.LastReadTS, before.LastReadTS)
	}
	if want := msg.Messages[len(msg.Messages)-1].TS; msg.MarkedTS != want {
		t.Errorf("MarkedTS = %q, want the latest message %q", msg.MarkedTS, want)
	}
	if d.world.readStates()[chEngineering].HasUnread {
		t.Error("#engineering is still unread after opening it")
	}
	if ev := nextEvent(t, d); ev.Kind != EventChannelOpened || ev.ChannelID != chEngineering || ev.TeamID != teamLumen {
		t.Errorf("event = %+v", ev)
	}
}

func TestChannelMarkReadAndLookup(t *testing.T) {
	d := testDemo(t)
	s := d.services()
	latest := ids.MessageTS(d.world.latestTS(chDesign))
	if msg, ok := s.channels.MarkRead(ids.ChannelID(chDesign), latest).(ui.ChannelMarkedReadMsg); !ok || msg.ChannelID != chDesign {
		t.Fatalf("MarkRead = %#v", msg)
	}
	if d.world.readStates()[chDesign].HasUnread {
		t.Error("#design still unread")
	}
	if ev := nextEvent(t, d); ev.Kind != EventMarkedRead || ev.ChannelID != chDesign {
		t.Errorf("event = %+v", ev)
	}
	if name, typ, ok := s.channels.Lookup(ids.ChannelID(chDesign)); !ok || name != "design" || typ != "channel" {
		t.Errorf("Lookup = %q %q %v", name, typ, ok)
	}
	if older, ok := s.channels.FetchOlder(ids.ChannelID(chDesign), "1.000000").(ui.OlderMessagesLoadedMsg); !ok || len(older.Messages) != 0 || older.AnchorTS != "1.000000" {
		t.Errorf("FetchOlder = %#v, want an empty page anchored where it was asked", older)
	}
}

func TestSendPostsAsTheUser(t *testing.T) {
	d := testDemo(t)
	msg, ok := d.services().messages.Send(ids.ChannelID(chGeneral), "hello **there**").(ui.MessageSentMsg)
	if !ok || msg.ChannelID != chGeneral || msg.Message.UserID != uAlex || msg.LocalTS != "" {
		t.Fatalf("Send = %#v", msg)
	}
	if strings.Contains(msg.Message.Text, "**") {
		t.Errorf("Text %q was not converted to Slack mrkdwn", msg.Message.Text)
	}
	msgs, _ := d.world.messages(chGeneral)
	if last := msgs[len(msgs)-1]; last.TS != msg.Message.TS || last.Text != msg.Message.Text {
		t.Errorf("World's latest = %+v", last)
	}
	if ev := nextEvent(t, d); ev.Kind != EventMessageSent || ev.ChannelID != chGeneral || ev.ThreadTS != "" {
		t.Errorf("event = %+v", ev)
	}
}

func TestSendReplyThreadsIt(t *testing.T) {
	d := testDemo(t)
	parent := threadIn(t, d.world, chEngineering)
	before := len(d.world.replies(chEngineering, parent))
	msg, ok := d.services().threads.SendReply(ids.ChannelID(chEngineering), ids.ThreadTS(parent), "on it", false).(ui.ThreadReplySentMsg)
	if !ok || msg.ThreadTS != parent || msg.Message.ThreadTS != parent || msg.Message.UserID != uAlex {
		t.Fatalf("SendReply = %#v", msg)
	}
	if got := len(d.world.replies(chEngineering, parent)); got != before+1 {
		t.Errorf("replies = %d, want %d", got, before+1)
	}
	if ev := nextEvent(t, d); ev.Kind != EventMessageSent || ev.ThreadTS != parent {
		t.Errorf("event = %+v", ev)
	}
}

func TestThreadFetchReturnsRepliesAndReportsTheOpen(t *testing.T) {
	d := testDemo(t)
	parent := threadIn(t, d.world, chDeploys)
	msg, ok := d.services().threads.Fetch(ids.ChannelID(chDeploys), ids.ThreadTS(parent)).(ui.ThreadRepliesLoadedMsg)
	if !ok || msg.ThreadTS != parent || len(msg.Replies) != 6 {
		t.Fatalf("Fetch = %#v, want the 6 deploy replies", msg)
	}
	if ev := nextEvent(t, d); ev.Kind != EventThreadOpened || ev.ChannelID != chDeploys || ev.ThreadTS != parent {
		t.Errorf("event = %+v", ev)
	}
}

func TestReactionsToggle(t *testing.T) {
	d := testDemo(t)
	s := d.services()
	ts := ids.MessageTS(d.world.latestTS(chGeneral))
	if err := s.reactions.Add(ids.ChannelID(chGeneral), ts, "tada"); err != nil {
		t.Fatal(err)
	}
	msgs, _ := d.world.messages(chGeneral)
	last := msgs[len(msgs)-1]
	if i := slices.IndexFunc(last.Reactions, func(r core.ReactionItem) bool { return r.Emoji == "tada" && r.HasReacted }); i < 0 {
		t.Fatalf("reactions = %+v, want the user's tada", last.Reactions)
	}
	if ev := nextEvent(t, d); ev.Kind != EventReactionAdded || ev.ChannelID != chGeneral {
		t.Errorf("event = %+v", ev)
	}
	if err := s.reactions.Remove(ids.ChannelID(chGeneral), ts, "tada"); err != nil {
		t.Fatal(err)
	}
	if msgs, _ = d.world.messages(chGeneral); slices.ContainsFunc(msgs[len(msgs)-1].Reactions, func(r core.ReactionItem) bool { return r.Emoji == "tada" }) {
		t.Error("tada still present after Remove")
	}
	if err := s.reactions.Add(ids.ChannelID(chGeneral), "1.000000", "tada"); !errors.Is(err, errUnavailable) {
		t.Errorf("Add on an unknown message: err = %v", err)
	}
	if got := s.reactions.LoadFrecent(5); len(got) != 5 || slices.ContainsFunc(got, func(e core.EmojiEntry) bool { return e.Unicode == "" }) {
		t.Errorf("LoadFrecent = %+v", got)
	}
}

func TestWorkspaceSwitch(t *testing.T) {
	d := testDemo(t)
	s := d.services()
	msg, ok := s.workspace.Switch(teamDriftwood).(ui.WorkspaceSwitchedMsg)
	if !ok || msg.TeamID != teamDriftwood || msg.TeamName != "Driftwood OSS" || msg.Theme != "catppuccin latte" || msg.UserID != uAlexDW {
		t.Fatalf("Switch = %#v", msg)
	}
	if msg.Channels[0].ID != chContributors || msg.UserNames[uRuth] != "Ruth Adeyemi" {
		t.Errorf("channels/users = %+v / %v", msg.Channels, msg.UserNames)
	}
	if _, ok := s.unread.ChannelReadStates()[chContributors]; !ok {
		t.Error("read states did not follow the switch")
	}
	if ev := nextEvent(t, d); ev.Kind != EventWorkspaceSwitched || ev.TeamID != teamDriftwood {
		t.Errorf("event = %+v", ev)
	}
	if got := s.workspace.Switch("TNOPE"); got != nil {
		t.Errorf("Switch(unknown) = %#v, want nil", got)
	}
}

func TestSearchAndThreadsList(t *testing.T) {
	s := testDemo(t).services()
	res, ok := s.search.SearchChannel(ids.ChannelID(chEngineering), "rate LIMITER").(ui.ChannelSearchResultsMsg)
	if !ok || len(res.TSes) != 1 || !slices.Equal(res.Terms, []string{"rate", "limiter"}) {
		t.Errorf("SearchChannel = %#v", res)
	}
	list, ok := s.threads.ListFetch(ids.TeamID(teamLumen)).(ui.ThreadsListLoadedMsg)
	if !ok || !list.SubscriptionsAvailable || len(list.Summaries) != 1 || list.Summaries[0].ChannelID != chDeploys {
		t.Errorf("ListFetch = %#v", list)
	}
}

func TestUnsupportedOperationsSaySo(t *testing.T) {
	s := testDemo(t).services()
	ctx := context.Background()
	if _, err := s.messages.Forward(ctx, teamLumen, chGeneral, "1.0", chRandom); !errors.Is(err, errUnavailable) {
		t.Errorf("Forward err = %v", err)
	}
	if _, err := s.messages.Permalink(ctx, chGeneral, "1.0"); !errors.Is(err, errUnavailable) {
		t.Errorf("Permalink err = %v", err)
	}
	if msg := s.messages.Edit(chGeneral, "1.0", "x").(ui.MessageEditedMsg); !errors.Is(msg.Err, errUnavailable) {
		t.Errorf("Edit = %#v", msg)
	}
	if msg := s.messages.Delete(chGeneral, "1.0").(ui.MessageDeletedMsg); !errors.Is(msg.Err, errUnavailable) {
		t.Errorf("Delete = %#v", msg)
	}
	if _, err := s.files.Download(ctx, "https://example.com/f", "f"); !errors.Is(err, errUnavailable) {
		t.Errorf("Download err = %v", err)
	}
	if cmd := s.files.Upload(chGeneral, "", "", nil); cmd == nil {
		t.Error("Upload returned no command; the compose box would wait forever")
	} else if msg := cmd().(ui.UploadResultMsg); !errors.Is(msg.Err, errUnavailable) {
		t.Errorf("Upload result = %#v", msg)
	}
}

func TestAvatarsAreServedPerUser(t *testing.T) {
	s := testDemo(t).services()
	if got, want := s.avatars.Avatar(uPriya), renderAvatar(uPriya, "Priya Shah"); got != want {
		t.Errorf("Avatar(Priya) = %q, want %q", got, want)
	}
	if got := s.avatars.Avatar("UNOBODY"); got != "" {
		t.Errorf("unknown user avatar = %q, want empty", got)
	}
}

func TestDemoProfiles_ReturnCast(t *testing.T) {
	d := testDemo(t)
	s := d.services()
	ctx := context.Background()

	var sawHalfHourZone bool
	for _, team := range d.world.teams {
		for _, u := range team.users {
			p, err := s.profiles.Profile(ctx, team.id, u.id)
			if err != nil {
				t.Fatalf("Profile(%s, %s) = %v", team.id, u.id, err)
			}
			if p.Title == "" {
				t.Errorf("Profile(%s) has no Title", u.id)
			}
			if p.TZ == "" {
				t.Errorf("Profile(%s) has no TZ", u.id)
			}
			if p.UserID != u.id || p.TeamID != team.id {
				t.Errorf("Profile(%s) UserID/TeamID = %q/%q", u.id, p.UserID, p.TeamID)
			}
			if p.TZOffset%3600 != 0 {
				sawHalfHourZone = true
			}
		}
	}
	if !sawHalfHourZone {
		t.Error("no cast member has a half-hour UTC offset (e.g. Asia/Kolkata)")
	}

	if _, err := s.profiles.Profile(ctx, teamLumen, "UNOBODY"); err == nil {
		t.Error("Profile(unknown user) = nil error, want an error")
	}
	if _, err := s.profiles.Profile(ctx, "TNOPE", uAlex); err == nil {
		t.Error("Profile(unknown team) = nil error, want an error")
	}
}
