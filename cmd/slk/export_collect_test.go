package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gammons/slk/internal/export"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/slack-go/slack"
)

const exportTestTZ = "America/New_York"

// exportTestWindow is 2026-04-01 to 2026-07-01 in New York, widened by
// overlap days.
func exportTestWindow(t *testing.T, overlap int) export.Window {
	t.Helper()
	win, err := export.NewWindow("2026-04-01", "2026-07-01", exportTestTZ, overlap, time.Now())
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}
	return win
}

// exportTS renders the Slack timestamp for a New York wall-clock time.
func exportTS(t *testing.T, wall string) string {
	t.Helper()
	loc, err := time.LoadLocation(exportTestTZ)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	at, err := time.ParseInLocation("2006-01-02 15:04:05", wall, loc)
	if err != nil {
		t.Fatalf("parse %q: %v", wall, err)
	}
	return fmt.Sprintf("%d.000100", at.Unix())
}

// fakeExportSource serves a fixed channel the way Slack does: history
// holds top-level messages and thread broadcasts, newest first and
// paged, and each thread's replies come back parent-first. Both honour
// inclusive oldest/latest bounds.
type fakeExportSource struct {
	history  []slack.Message
	threads  map[string][]slack.Message
	pageSize int

	walks        []string
	replyFetches []string
	repliesErr   error
}

// inBounds applies inclusive Slack timestamp bounds; "" is open.
func inBounds(ts, oldest, latest string) bool {
	return (oldest == "" || ts >= oldest) && (latest == "" || ts <= latest)
}

func (f *fakeExportSource) WalkHistory(ctx context.Context, channelID, oldest, latest string, visit func([]slack.Message) error) error {
	f.walks = append(f.walks, oldest+".."+latest)
	var matched []slack.Message
	for _, m := range f.history {
		if inBounds(m.Timestamp, oldest, latest) {
			matched = append(matched, m)
		}
	}
	slices.SortFunc(matched, compareMessagesByTS)
	slices.Reverse(matched)
	for page := range slices.Chunk(matched, f.pageSize) {
		if err := visit(page); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeExportSource) GetRepliesBetween(ctx context.Context, channelID, threadTS, oldest, latest string) ([]slack.Message, error) {
	f.replyFetches = append(f.replyFetches, threadTS)
	if f.repliesErr != nil {
		return nil, f.repliesErr
	}
	thread := f.threads[threadTS]
	// The parent always leads, in or out of bounds, as Slack sends it.
	out := []slack.Message{thread[0]}
	for _, m := range thread[1:] {
		if inBounds(m.Timestamp, oldest, latest) {
			out = append(out, m)
		}
	}
	return out, nil
}

// topLevel builds a message with no thread.
func topLevel(ts, text string) slack.Message {
	return slack.Message{Msg: slack.Msg{Timestamp: ts, User: "U1", Text: text}}
}

// threadParent builds a thread's parent message.
func threadParent(ts, text, latestReply string, replyCount int) slack.Message {
	return slack.Message{Msg: slack.Msg{Timestamp: ts, ThreadTimestamp: ts, User: "U1", Text: text, ReplyCount: replyCount, LatestReply: latestReply}}
}

// threadReply builds a reply within a thread.
func threadReply(ts, parentTS, text string) slack.Message {
	return slack.Message{Msg: slack.Msg{Timestamp: ts, ThreadTimestamp: parentTS, User: "U2", Text: text}}
}

// summarize flattens conversations to "parent[reply,reply]" strings,
// with "*" marking a context-only parent.
func summarize(convs []rawConversation) []string {
	var out []string
	for _, conv := range convs {
		var replies []string
		for _, r := range conv.Replies {
			replies = append(replies, r.Text)
		}
		s := conv.Parent.Text + "[" + strings.Join(replies, ",") + "]"
		if conv.ParentIsContext {
			s = "*" + s
		}
		out = append(out, s)
	}
	return out
}

// newExportFixture builds a channel that exercises every selection
// rule around a 2026-04-01..2026-07-01 window with no overlap.
func newExportFixture(t *testing.T) *fakeExportSource {
	t.Helper()
	var (
		ancientTS  = exportTS(t, "2024-01-10 09:00:00") // thread revived inside the window
		staleTS    = exportTS(t, "2025-06-01 09:00:00") // thread that went quiet before it
		afterTS    = exportTS(t, "2026-03-01 09:00:00") // thread next replied to after it
		earlyTS    = exportTS(t, "2026-03-31 23:59:59") // standalone, one second too early
		firstTS    = exportTS(t, "2026-04-01 00:00:00") // standalone, exactly at since
		inThreadTS = exportTS(t, "2026-06-30 15:00:00") // thread straddling until
		untilTS    = exportTS(t, "2026-07-01 00:00:00") // standalone, exactly at until

		ancientReplyOld = threadReply(exportTS(t, "2024-01-10 10:00:00"), ancientTS, "ancient-reply-old")
		ancientReplyIn  = threadReply(exportTS(t, "2026-05-05 12:00:00"), ancientTS, "ancient-reply-in")
		staleReply      = threadReply(exportTS(t, "2025-06-02 09:00:00"), staleTS, "stale-reply")
		afterReply      = threadReply(exportTS(t, "2026-08-01 09:00:00"), afterTS, "after-reply")
		inReply         = threadReply(exportTS(t, "2026-06-30 16:00:00"), inThreadTS, "in-reply")
		inReplyLate     = threadReply(exportTS(t, "2026-07-02 09:00:00"), inThreadTS, "in-reply-late")
	)
	// A thread broadcast also shows up in channel history.
	broadcast := ancientReplyIn
	broadcast.SubType = "thread_broadcast"

	ancient := threadParent(ancientTS, "ancient", ancientReplyIn.Timestamp, 2)
	stale := threadParent(staleTS, "stale", staleReply.Timestamp, 1)
	after := threadParent(afterTS, "after", afterReply.Timestamp, 1)
	inThread := threadParent(inThreadTS, "in-thread", inReplyLate.Timestamp, 2)

	return &fakeExportSource{
		pageSize: 2,
		history: []slack.Message{
			ancient, stale, after,
			topLevel(earlyTS, "early"),
			topLevel(firstTS, "first"),
			broadcast,
			inThread,
			topLevel(untilTS, "at-until"),
		},
		threads: map[string][]slack.Message{
			ancientTS:  {ancient, ancientReplyOld, ancientReplyIn},
			staleTS:    {stale, staleReply},
			afterTS:    {after, afterReply},
			inThreadTS: {inThread, inReply, inReplyLate},
		},
	}
}

func TestCollectConversations_SelectsByWindow(t *testing.T) {
	src := newExportFixture(t)
	convs, err := collectConversations(context.Background(), src, "C1", exportTestWindow(t, 0), io.Discard)
	if err != nil {
		t.Fatalf("collectConversations: %v", err)
	}
	want := []string{
		"*ancient[ancient-reply-in]", // older parent kept as context; its old reply is not
		"first[]",                    // since is inclusive
		"in-thread[in-reply]",        // the reply past until is dropped
	}
	if got := summarize(convs); !slices.Equal(got, want) {
		t.Errorf("conversations = %q\nwant %q", got, want)
	}
}

func TestCollectConversations_OverlapWidensBothEnds(t *testing.T) {
	src := newExportFixture(t)
	convs, err := collectConversations(context.Background(), src, "C1", exportTestWindow(t, 2), io.Discard)
	if err != nil {
		t.Fatalf("collectConversations: %v", err)
	}
	want := []string{
		"*ancient[ancient-reply-in]",
		"early[]",
		"first[]",
		"in-thread[in-reply,in-reply-late]",
		"at-until[]",
	}
	if got := summarize(convs); !slices.Equal(got, want) {
		t.Errorf("conversations = %q\nwant %q", got, want)
	}
}

func TestCollectConversations_FetchesRepliesOnlyWhereTheyCanMatter(t *testing.T) {
	src := newExportFixture(t)
	win := exportTestWindow(t, 0)
	if _, err := collectConversations(context.Background(), src, "C1", win, io.Discard); err != nil {
		t.Fatalf("collectConversations: %v", err)
	}

	// One walk over the window, one over everything before it.
	wantWalks := []string{win.OldestTS() + ".." + win.LatestTS(), ".." + win.OldestTS()}
	if !slices.Equal(src.walks, wantWalks) {
		t.Errorf("walks = %q, want %q", src.walks, wantWalks)
	}

	// "stale" went quiet before the window, so latest_reply rules it out
	// without a request. "after" cannot be ruled out that way: its
	// latest reply is past the window, which says nothing about earlier
	// ones, so it is fetched and then dropped for having none in range.
	slices.Sort(src.replyFetches)
	want := []string{exportTS(t, "2024-01-10 09:00:00"), exportTS(t, "2026-03-01 09:00:00"), exportTS(t, "2026-06-30 15:00:00")}
	if !slices.Equal(src.replyFetches, want) {
		t.Errorf("reply fetches = %q, want %q", src.replyFetches, want)
	}
}

func TestCollectConversations_KeepsOlderThreadThatOmitsLatestReply(t *testing.T) {
	src := newExportFixture(t)
	for i, m := range src.history {
		if m.Text == "ancient" {
			src.history[i].LatestReply = ""
		}
	}
	convs, err := collectConversations(context.Background(), src, "C1", exportTestWindow(t, 0), io.Discard)
	if err != nil {
		t.Fatalf("collectConversations: %v", err)
	}
	if got := summarize(convs); !slices.Contains(got, "*ancient[ancient-reply-in]") {
		t.Errorf("thread without latest_reply was dropped: %q", got)
	}
}

func TestCollectConversations_PropagatesFetchError(t *testing.T) {
	src := newExportFixture(t)
	src.repliesErr = errors.New("thread_not_found")
	_, err := collectConversations(context.Background(), src, "C1", exportTestWindow(t, 0), io.Discard)
	if !errors.Is(err, src.repliesErr) {
		t.Errorf("err = %v, want the replies error", err)
	}
}

func TestWindowReplies_DropsParentDuplicatesAndOutOfWindow(t *testing.T) {
	win := exportTestWindow(t, 0)
	parentTS := exportTS(t, "2026-04-02 09:00:00")
	first := threadReply(exportTS(t, "2026-04-02 10:00:00"), parentTS, "first")
	second := threadReply(exportTS(t, "2026-04-02 11:00:00"), parentTS, "second")
	late := threadReply(exportTS(t, "2026-07-01 00:00:00"), parentTS, "late")
	parent := threadParent(parentTS, "parent", late.Timestamp, 3)

	// Slack can repeat the parent on each page; pages arrive unsorted here.
	got := windowReplies([]slack.Message{parent, second, parent, first, second, late}, parentTS, win)
	var texts []string
	for _, m := range got {
		texts = append(texts, m.Text)
	}
	if want := []string{"first", "second"}; !slices.Equal(texts, want) {
		t.Errorf("replies = %q, want %q", texts, want)
	}
}

func TestExportMessageItem(t *testing.T) {
	m := slack.Message{Msg: slack.Msg{
		Timestamp:       "1700000002.000100",
		ThreadTimestamp: "1700000001.000100",
		User:            "U1",
		Text:            "hello",
		SubType:         "thread_broadcast",
		Reactions:       []slack.ItemReaction{{Name: "tada", Count: 2, Users: []string{"U2", "U3"}}},
		Files:           []slack.File{{Title: "spec.pdf", Mimetype: "application/pdf", Permalink: "https://example.slack.com/files/spec.pdf"}},
	}}
	got := exportMessageItem(m, map[string]string{"U1": "alice"}, nil)

	if got.TS != m.Timestamp || got.ThreadTS != m.ThreadTimestamp || got.Text != "hello" || got.Subtype != "thread_broadcast" {
		t.Errorf("core fields = %+v", got)
	}
	if got.UserID != "U1" || got.UserName != "alice" {
		t.Errorf("author = %q/%q, want U1/alice", got.UserID, got.UserName)
	}
	if len(got.Reactions) != 1 || got.Reactions[0].Emoji != "tada" || got.Reactions[0].Count != 2 {
		t.Errorf("reactions = %+v", got.Reactions)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].Name != "spec.pdf" || got.Attachments[0].URL != "https://example.slack.com/files/spec.pdf" {
		t.Errorf("attachments = %+v", got.Attachments)
	}
	if got.DateStr != "" || got.Timestamp != "" {
		t.Errorf("display times must be left to WriteChannel, got %q %q", got.DateStr, got.Timestamp)
	}
}

func TestExportMessageItem_BotAndUnknownAuthors(t *testing.T) {
	bot := exportMessageItem(slack.Message{Msg: slack.Msg{BotID: "B1", Username: "deploybot"}}, map[string]string{}, nil)
	if bot.UserID != "B1" || bot.UserName != "deploybot" {
		t.Errorf("bot author = %q/%q, want B1/deploybot", bot.UserID, bot.UserName)
	}
	unknown := exportMessageItem(slack.Message{Msg: slack.Msg{User: "U404"}}, map[string]string{}, nil)
	if unknown.UserID != "U404" || unknown.UserName != "U404" {
		t.Errorf("unknown author = %q/%q, want the raw ID for both", unknown.UserID, unknown.UserName)
	}
}

// fakeProfiles resolves users from a fixed table and scripts failures.
type fakeProfiles struct {
	users map[string]*slack.User
	errs  map[string][]error
	calls []string
}

func (f *fakeProfiles) GetUserProfile(userID string) (*slack.User, error) {
	f.calls = append(f.calls, userID)
	if queue := f.errs[userID]; len(queue) > 0 {
		f.errs[userID] = queue[1:]
		return nil, queue[0]
	}
	if u, ok := f.users[userID]; ok {
		return u, nil
	}
	return nil, errors.New("user_not_found")
}

// slackUser builds a user with the given name fields.
func slackUser(display, realName, handle string) *slack.User {
	u := &slack.User{Name: handle, RealName: realName}
	u.Profile.DisplayName = display
	return u
}

func TestResolveExportNames_AuthorsAndMentions(t *testing.T) {
	convs := []export.Conversation{{
		Parent: messages.MessageItem{UserID: "U1", UserName: "U1", Text: "ping <@U2> and <@U1>"},
		Replies: []messages.MessageItem{
			{UserID: "U3", UserName: "carol", Text: "cc <@U404>"},
			{UserID: "B1", UserName: "deploybot", Text: "done"},
		},
	}}
	names := map[string]string{"U3": "carol"}
	profiles := &fakeProfiles{users: map[string]*slack.User{
		"U1": slackUser("alice", "Alice A", "aa"),
		"U2": slackUser("", "Bob B", "bb"),
	}}

	if err := resolveExportNames(context.Background(), convs, names, nil, profiles); err != nil {
		t.Fatalf("resolveExportNames: %v", err)
	}
	if got := convs[0].Parent.UserName; got != "alice" {
		t.Errorf("parent author = %q, want alice", got)
	}
	if names["U1"] != "alice" || names["U2"] != "Bob B" {
		t.Errorf("names = %v, want U1=alice and U2 falling back to the real name", names)
	}
	if _, ok := names["U404"]; ok {
		t.Errorf("unresolvable ID must not be given a name: %v", names)
	}
	// Each unknown ID is asked for once; a known name (U3) and a bot
	// author that already has one (B1) are not asked for at all.
	if want := []string{"U1", "U2", "U404"}; !slices.Equal(profiles.calls, want) {
		t.Errorf("profile lookups = %q, want %q", profiles.calls, want)
	}
}

func TestResolveExportNames_RetriesAfterRateLimit(t *testing.T) {
	convs := []export.Conversation{{Parent: messages.MessageItem{UserID: "U1", UserName: "U1"}}}
	names := map[string]string{}
	profiles := &fakeProfiles{
		users: map[string]*slack.User{"U1": slackUser("alice", "", "")},
		errs:  map[string][]error{"U1": {fmt.Errorf("getting user info: %w", &slack.RateLimitedError{RetryAfter: time.Millisecond})}},
	}

	if err := resolveExportNames(context.Background(), convs, names, nil, profiles); err != nil {
		t.Fatalf("resolveExportNames: %v", err)
	}
	if convs[0].Parent.UserName != "alice" || len(profiles.calls) != 2 {
		t.Errorf("author = %q after %d lookups, want alice after 2", convs[0].Parent.UserName, len(profiles.calls))
	}
}

func TestResolveExportNames_CancelledDuringRateLimitWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	convs := []export.Conversation{{Parent: messages.MessageItem{UserID: "U1", UserName: "U1"}}}
	profiles := &fakeProfiles{errs: map[string][]error{"U1": {&slack.RateLimitedError{RetryAfter: time.Hour}}}}

	err := resolveExportNames(ctx, convs, map[string]string{}, nil, profiles)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestProfileDisplayName(t *testing.T) {
	cases := []struct {
		user *slack.User
		want string
	}{
		{slackUser("alice", "Alice A", "aa"), "alice"},
		{slackUser("", "Alice A", "aa"), "Alice A"},
		{slackUser("", "", "aa"), "aa"},
		{slackUser("", "", ""), ""},
	}
	for _, tc := range cases {
		if got := profileDisplayName(tc.user); got != tc.want {
			t.Errorf("profileDisplayName = %q, want %q", got, tc.want)
		}
	}
}
