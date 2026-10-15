package messages

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

var huddleNames = map[string]string{
	"UFAKE0001": "Alice",
	"UFAKE0002": "Bob",
	"UFAKE0003": "Carol",
}

func huddleItem(room *HuddleRoom, replies int) MessageItem {
	return MessageItem{
		TS: "1700000000.000100", UserID: "USLACKBOT", UserName: "Slackbot",
		Timestamp: "10:30 AM", Subtype: "huddle_thread", Text: "A huddle started",
		ReplyCount: replies, HuddleRoom: room,
	}
}

func liveRoom() *HuddleRoom { return &HuddleRoom{CreatedBy: "UFAKE0001", DateStart: 1700000000} }

func endedRoom(secs int64) *HuddleRoom {
	return &HuddleRoom{CreatedBy: "UFAKE0001", DateStart: 1700000000, DateEnd: 1700000000 + secs, HasEnded: true}
}

func withParticipants(r *HuddleRoom, ids ...string) *HuddleRoom {
	c := *r
	c.Participants = ids
	return &c
}

func renderPlain(msg MessageItem, width int, opt RenderOptions) string {
	rc := New(nil, "general").renderContext()
	rc.UserNames = huddleNames
	return ansi.Strip(RenderMessage(HuddleView(msg, huddleNames), width, rc, opt).Content)
}

func TestFormatHuddleDuration(t *testing.T) {
	cases := map[time.Duration]string{
		0:                               "under a minute",
		59 * time.Second:                "under a minute",
		time.Minute:                     "1 min",
		23*time.Minute + 40*time.Second: "23 min",
		59 * time.Minute:                "59 min",
		time.Hour:                       "1h",
		65 * time.Minute:                "1h 5m",
		26*time.Hour + 3*time.Minute:    "26h 3m",
	}
	for d, want := range cases {
		if got := FormatHuddleDuration(d); got != want {
			t.Errorf("%v: got %q, want %q", d, got, want)
		}
	}
}

// No replies: a centred notice naming the starter and the huddle's facts.
func TestHuddleNoticeTexts(t *testing.T) {
	cases := []struct {
		name string
		msg  MessageItem
		want string
	}{
		{"live", huddleItem(liveRoom(), 0), "Alice started a huddle"},
		{"ended", huddleItem(endedRoom(23*60), 0), "Alice started a huddle · lasted 23 min"},
		{"sub-minute", huddleItem(endedRoom(30), 0), "Alice started a huddle · lasted under a minute"},
		{"ended with participants", huddleItem(withParticipants(endedRoom(3900), "UFAKE0001", "UFAKE0002", "UFAKE0003", "UFAKE0404"), 0),
			"Alice started a huddle · lasted 1h 5m · with Bob, Carol"},
		{"live with participants", huddleItem(withParticipants(liveRoom(), "UFAKE0002"), 0), "Alice started a huddle · with Bob"},
		{"unresolved starter", huddleItem(&HuddleRoom{CreatedBy: "UFAKE0404"}, 0), "A huddle started"},
		{"nil room", huddleItem(nil, 0), "A huddle started"},
		{"ended without valid end", huddleItem(&HuddleRoom{CreatedBy: "UFAKE0001", HasEnded: true}, 0), "Alice started a huddle"},
	}
	for _, c := range cases {
		got := strings.Join(strings.Fields(renderPlain(c.msg, 100, RenderOptions{})), " ")
		if got != "10:30 AM  "+c.want && got != "10:30 AM "+c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// With replies: a normal message by the starter, with the reply affordance.
func TestHuddleWithRepliesIsNormalMessageByStarter(t *testing.T) {
	cases := []struct {
		name string
		msg  MessageItem
		body string
	}{
		{"live", huddleItem(liveRoom(), 2), "Started a huddle"},
		{"ended", huddleItem(endedRoom(23*60), 2), "Started a huddle · lasted 23 min"},
		{"ended with participants", huddleItem(withParticipants(endedRoom(23*60), "UFAKE0002", "UFAKE0001"), 2),
			"Started a huddle · lasted 23 min · with Bob"},
	}
	for _, c := range cases {
		stored := c.msg
		v := HuddleView(c.msg, huddleNames)
		if v.UserID != "UFAKE0001" || v.UserName != "Alice" || v.Subtype != "" {
			t.Errorf("%s: view author/subtype = %q %q %q", c.name, v.UserID, v.UserName, v.Subtype)
		}
		if stored.UserID != "USLACKBOT" || stored.Subtype != "huddle_thread" || stored.Text != "A huddle started" {
			t.Errorf("%s: stored item changed: %+v", c.name, stored)
		}
		out := renderPlain(c.msg, 60, RenderOptions{ReplyAffordance: true})
		lines := strings.Split(out, "\n")
		if !strings.Contains(lines[0], "Alice") || strings.Contains(lines[0], "Slackbot") || leadingSpaces(lines[0]) > 2 {
			t.Errorf("%s: author line = %q", c.name, lines[0])
		}
		if !strings.Contains(out, c.body) {
			t.Errorf("%s: body %q missing:\n%s", c.name, c.body, out)
		}
		if !strings.Contains(lines[len(lines)-1], "[2 replies ->]") || leadingSpaces(lines[len(lines)-1]) > 2 {
			t.Errorf("%s: reply affordance = %q", c.name, lines[len(lines)-1])
		}
	}
}

// With replies but no usable room or starter: today's behaviour.
func TestHuddleWithRepliesFallsBack(t *testing.T) {
	for name, room := range map[string]*HuddleRoom{
		"nil room":     nil,
		"unresolved":   {CreatedBy: "UFAKE0404"},
		"empty author": {},
	} {
		msg := huddleItem(room, 2)
		if v := HuddleView(msg, huddleNames); v.UserID != "USLACKBOT" || v.Subtype != "huddle_thread" {
			t.Errorf("%s: view changed the message: %+v", name, v)
		}
	}
}

// Messages that are not huddle announcements are never touched.
func TestHuddleViewLeavesOtherMessagesAlone(t *testing.T) {
	bot := MessageItem{TS: "1.0", UserID: "USLACKBOT", UserName: "Slackbot", Text: "reminder", ReplyCount: 3,
		HuddleRoom: liveRoom()}
	if v := HuddleView(bot, huddleNames); v.UserID != "USLACKBOT" || v.Text != "reminder" {
		t.Errorf("slackbot message changed: %+v", v)
	}
	join := MessageItem{TS: "1.0", UserID: "USLACKBOT", Subtype: "channel_join", Text: "joined", ReplyCount: 1}
	if v := HuddleView(join, huddleNames); v.Subtype != "channel_join" {
		t.Errorf("join changed: %+v", v)
	}
}

func TestHuddleNarrowWidthAndLongNames(t *testing.T) {
	long := map[string]string{"UFAKE0001": strings.Repeat("Longname ", 12)}
	rc := New(nil, "general").renderContext()
	rc.UserNames = long
	// Only the notice wraps; an ordinary author line is clipped by the pane
	// for every message, so the message form is not asserted here.
	for _, replies := range []int{0} {
		for _, w := range []int{12, 24, 40} {
			msg := huddleItem(endedRoom(600), replies)
			out := ansi.Strip(RenderMessage(HuddleView(msg, long), w, rc, RenderOptions{ReplyAffordance: true}).Content)
			for _, line := range strings.Split(out, "\n") {
				if ansi.StringWidth(line) > max(w, 20) {
					t.Errorf("replies=%d width=%d: line too wide (%d): %q", replies, w, ansi.StringWidth(line), line)
				}
			}
		}
	}
}

func TestHuddleNoticeCopyMirrorAndSelected(t *testing.T) {
	msg := huddleItem(endedRoom(23*60), 0)
	v := HuddleView(msg, huddleNames)
	r := RenderMessage(v, 80, &RenderContext{UserNames: huddleNames}, RenderOptions{})
	rows := WrapRows(v, r, 80, NewWrapStyles(true))
	if len(rows.Plain) != 1 || rows.Plain[0].Text != strings.TrimSpace(rows.Plain[0].Text) ||
		!strings.Contains(rows.Plain[0].Text, "Alice started a huddle · lasted 23 min") {
		t.Errorf("plain mirror = %+v", rows.Plain)
	}
	if len(rows.Selected) != len(rows.Lines) {
		t.Errorf("selected rows = %d, lines = %d", len(rows.Selected), len(rows.Lines))
	}
}

// The channel pane: a huddle that gains its first reply flips from the
// centred notice to a message by its starter, and one that ends re-renders
// with its duration.
func TestChannelHuddleFlipsAndRefreshes(t *testing.T) {
	m := New([]MessageItem{huddleItem(liveRoom(), 0)}, "general")
	m.SetUserNames(huddleNames)
	view := func() string { return ansi.Strip(m.View(20, 80)) }
	out := view()
	if !strings.Contains(out, "Alice started a huddle") || strings.Contains(out, "replies") {
		t.Fatalf("notice form expected:\n%s", out)
	}
	m.SetHuddleRoom("1700000000.000100", endedRoom(23*60))
	if out = view(); !strings.Contains(out, "Alice started a huddle · lasted 23 min") {
		t.Fatalf("ended room did not re-render:\n%s", out)
	}
	m.IncrementReplyCount("1700000000.000100", "1700000001.000100")
	out = view()
	if !strings.Contains(out, "Started a huddle · lasted 23 min") || !strings.Contains(out, "[1 reply ->]") ||
		strings.Contains(out, "Alice started a huddle") {
		t.Fatalf("message form expected after the first reply:\n%s", out)
	}
	// The stored item keeps Slackbot as author and its subtype.
	if got := m.Messages()[0]; got.UserID != "USLACKBOT" || got.Subtype != "huddle_thread" {
		t.Errorf("stored item mutated: %+v", got)
	}
}

// Selection and thread gates read the stored item, which is unchanged.
func TestChannelHuddleSelectedMessageFormKeepsStoredAuthor(t *testing.T) {
	m := New([]MessageItem{huddleItem(endedRoom(60), 2)}, "general")
	m.SetUserNames(huddleNames)
	sel, ok := m.SelectedMessage()
	if !ok || sel.UserID != "USLACKBOT" || !IsSystemNoticeSubtype(sel.Subtype) || !CanOpenThread(sel) {
		t.Errorf("selected stored item = %+v", sel)
	}
	if out := ansi.Strip(m.View(20, 80)); !strings.Contains(out, "Alice") {
		t.Errorf("starter missing from view:\n%s", out)
	}
}
