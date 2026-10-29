package messages

import (
	"regexp"
	"strings"
	"testing"
)

var sgr = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestRenderMessage_ReplyRowShowsAvatarsThenCount(t *testing.T) {
	msg := MessageItem{TS: "1.0", UserID: "U1", UserName: "priya", Text: "ship it",
		Timestamp: "3:40 PM", ReplyCount: 7, ReplyUsers: []string{"U1", "U2", "U3", "U4", "U5", "U6", "U7"}}
	m := New([]MessageItem{msg}, "general")
	m.SetReplyAvatarFunc(func(id string) string {
		if id == "U2" {
			return "" // not loaded yet
		}
		return "<" + id + ">"
	})
	got := sgr.ReplaceAllString(RenderMessage(msg, 80, m.renderContext(), RenderOptions{ReplyAffordance: true}).Content, "")
	if !strings.Contains(got, "<U1> <U3> <U4> <U5> <U6> 7 replies") {
		t.Errorf("want five ready avatars then the count, got:\n%s", got)
	}
}

func TestRenderMessage_ReplyRowFitsANarrowPane(t *testing.T) {
	msg := MessageItem{TS: "1.0", UserID: "U1", UserName: "priya", Text: "ship it",
		Timestamp: "3:40 PM", ReplyCount: 7, ReplyUsers: []string{"U1", "U3", "U4", "U5", "U6"}}
	m := New([]MessageItem{msg}, "general")
	m.SetReplyAvatarFunc(func(id string) string { return "<" + id + ">" })
	got := sgr.ReplaceAllString(RenderMessage(msg, 24, m.renderContext(), RenderOptions{ReplyAffordance: true}).Content, "")
	if !strings.Contains(got, "\n<U1> <U3> 7 replies") || strings.Contains(got, "<U4>") {
		t.Errorf("want only the avatars that fit 20 cells beside the count, got:\n%s", got)
	}
}

func TestRenderMessage_ReplyRowDefaultsToBracketedCount(t *testing.T) {
	msg := MessageItem{TS: "1.0", UserID: "U1", UserName: "priya", Text: "ship it",
		Timestamp: "3:40 PM", ReplyCount: 2, ReplyUsers: []string{"U1", "U2"}}
	m := New([]MessageItem{msg}, "general")
	got := sgr.ReplaceAllString(RenderMessage(msg, 80, m.renderContext(), RenderOptions{ReplyAffordance: true}).Content, "")
	if !strings.Contains(got, "[2 replies ->]") {
		t.Errorf("want the default reply row, got:\n%s", got)
	}
}

func TestIncrementReplyCount_AddsTheReplier(t *testing.T) {
	m := New([]MessageItem{{TS: "1.0", UserID: "U1", ReplyCount: 1, ReplyUsers: []string{"U1"}}}, "general")
	m.IncrementReplyCount("1.0", "2.0", "U2")
	m.IncrementReplyCount("1.0", "3.0", "U1")
	got := m.Messages()[0]
	if got.ReplyCount != 3 || strings.Join(got.ReplyUsers, ",") != "U1,U2" {
		t.Errorf("got count %d, users %v; want 3, [U1 U2]", got.ReplyCount, got.ReplyUsers)
	}
}
