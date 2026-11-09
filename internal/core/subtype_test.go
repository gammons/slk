package core

import "testing"

func TestIsSystemNoticeSubtype(t *testing.T) {
	cases := map[string]bool{
		"channel_join":          true,
		"channel_leave":         true,
		"group_join":            true,
		"group_leave":           true,
		SubtypeHuddleThread:     true,
		"tabbed_canvas_updated": true,
		"":                      false,
		"bot_message":           false,
		"thread_broadcast":      false,
		"file_share":            false,
		"me_message":            false,
		"message_changed":       false,
		"channel_topic":         false,
	}
	for subtype, want := range cases {
		if got := IsSystemNoticeSubtype(subtype); got != want {
			t.Errorf("IsSystemNoticeSubtype(%q) = %v, want %v", subtype, got, want)
		}
	}
}
