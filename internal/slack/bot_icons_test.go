package slackclient

import (
	"context"
	"strconv"
	"testing"
)

const fakeIcons = `{"image_36":"https://example.com/a36.png","image_48":"https://example.com/a48.png","image_72":"https://example.com/a72.png"}`

func TestRoomTap_HistoryCapturesBotIcons(t *testing.T) {
	c := roomTestClient(t, `{"ok":true,"messages":[
		{"type":"message","subtype":"bot_message","ts":"1.000100","bot_id":"B1","username":"Alice","icons":`+fakeIcons+`},
		{"type":"message","subtype":"bot_message","ts":"1.000200","bot_id":"B1","username":"Bob","icons":{"image_48":"https://example.com/b48.png"}},
		{"type":"message","subtype":"bot_message","ts":"1.000300","bot_id":"B1","username":"Carol","icons":{"emoji":":ghost:"}},
		{"type":"message","subtype":"bot_message","ts":"1.000400","bot_id":"B1","username":"Dave"}]}`)
	if _, err := c.GetHistory(context.Background(), "CFAKE0001", 50, ""); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{
		"B1/Alice": "https://example.com/a72.png",
		"B1/Bob":   "https://example.com/b48.png",
		"B1/Carol": "",
		"B1/Dave":  "",
		"B1":       "",
	} {
		if got := c.BotIcon(id); got != want {
			t.Errorf("BotIcon(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestBotIconStore_KeepsNewestIcon(t *testing.T) {
	c := roomTestClient(t, `{"ok":true,"messages":[
		{"type":"message","subtype":"bot_message","ts":"1.000300","bot_id":"B1","username":"Alice","icons":{"image_72":"https://example.com/new.png"}},
		{"type":"message","subtype":"bot_message","ts":"1.000100","bot_id":"B1","username":"Alice","icons":{"image_72":"https://example.com/old.png"}}]}`)
	if _, err := c.GetHistory(context.Background(), "CFAKE0001", 50, ""); err != nil {
		t.Fatal(err)
	}
	if got := c.BotIcon("B1/Alice"); got != "https://example.com/new.png" {
		t.Errorf("after a newest-first page BotIcon = %q, want the newest icon", got)
	}
	c.botIcons.recordFrame([]byte(`{"type":"message","subtype":"bot_message","ts":"1.000050","bot_id":"B1","username":"Alice","icons":{"image_72":"https://example.com/older.png"}}`))
	if got := c.BotIcon("B1/Alice"); got != "https://example.com/new.png" {
		t.Errorf("after an older message BotIcon = %q, want the newest icon", got)
	}
	c.botIcons.recordFrame([]byte(`{"type":"message","subtype":"bot_message","ts":"1.000400","bot_id":"B1","username":"Alice","icons":{"image_72":"https://example.com/live.png"}}`))
	if got := c.BotIcon("B1/Alice"); got != "https://example.com/live.png" {
		t.Errorf("after a newer message BotIcon = %q, want the live icon", got)
	}
}

func TestRoomTap_RepliesCapturesBotIcons(t *testing.T) {
	c := roomTestClient(t, `{"ok":true,"has_more":false,"messages":[
		{"type":"message","subtype":"bot_message","ts":"2.000100","thread_ts":"2.000100","bot_id":"B1","icons":`+fakeIcons+`}]}`)
	if _, err := c.GetReplies(context.Background(), "CFAKE0001", "2.000100"); err != nil {
		t.Fatal(err)
	}
	if got := c.BotIcon("B1"); got != "https://example.com/a72.png" {
		t.Errorf("BotIcon(B1) = %q, want the 72px icon", got)
	}
}

func TestBotIconStore_RecordFrame(t *testing.T) {
	s := newBotIconStore()
	s.recordFrame([]byte(`{"type":"message","subtype":"bot_message","channel":"C1","ts":"1.1","bot_id":"B1","username":"Alice","icons":` + fakeIcons + `}`))
	s.recordFrame([]byte(`{"type":"bot_added","bot":{"id":"B2","icons":` + fakeIcons + `}}`))
	if got := s.get("B1/Alice"); got != "https://example.com/a72.png" {
		t.Errorf("B1/Alice = %q, want the 72px icon", got)
	}
	if got := s.get("B2"); got != "" {
		t.Errorf("B2 = %q, want none from a bot_added frame", got)
	}
}

func TestBotIconStore_Bounded(t *testing.T) {
	s := newBotIconStore()
	m := iconMessage{BotID: "B1", TS: "1.000100"}
	m.Icons.Image72 = "https://example.com/i.png"
	for i := range botIconStoreMax + 1 {
		m.Username = strconv.Itoa(i)
		s.put(m)
	}
	if n := len(s.icons); n > botIconStoreMax {
		t.Errorf("store holds %d icons, want at most %d", n, botIconStoreMax)
	}
	if got := s.get("B1/" + strconv.Itoa(botIconStoreMax)); got == "" {
		t.Error("the newest sender was not kept")
	}
}

func TestBotIcon_NilClient(t *testing.T) {
	var c *Client
	if got := c.BotIcon("B1"); got != "" {
		t.Errorf("nil client BotIcon = %q, want empty", got)
	}
	if got := (&Client{}).BotIcon("B1"); got != "" {
		t.Errorf("client without a store BotIcon = %q, want empty", got)
	}
}
