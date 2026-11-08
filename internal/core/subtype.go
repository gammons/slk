package core

// SubtypeHuddleThread is the message subtype Slack gives the message that
// announces a huddle starting; it carries the huddle's `room` object.
const SubtypeHuddleThread = "huddle_thread"

// IsSystemNoticeSubtype reports whether subtype is a system message --
// join/leave, a huddle starting (SubtypeHuddleThread), a canvas update
// (tabbed_canvas_updated) -- as opposed to an authored chat message.
//
// This is the single list: live delivery (internal/slack keeps the
// subtype across a message_changed edit), rendering (both panes draw it
// as a one-line muted notice through messages.RenderMessage), and
// cmd/slk's unread/mention/notification gating all call it.
func IsSystemNoticeSubtype(subtype string) bool {
	switch subtype {
	case "channel_join", "channel_leave", "group_join", "group_leave",
		SubtypeHuddleThread, "tabbed_canvas_updated":
		return true
	}
	return false
}
