package main

// gridConnectionExempt lists frame types that are never deduped because
// they belong to the connection that received them, carry no event at
// all, or repeat byte-identically as part of their normal cadence. The
// empty type covers frames that failed to parse as an event.
// presence_change is here for a different reason: it is a cheap
// set-value on shared state, and deduping it buys a narrow staleness -
// a socket that disconnects mid-TTL can leave an entry that swallows a
// sibling's byte-identical away-active-away round trip, stranding the
// presence on the wrong value on an inactive workspace.
var gridConnectionExempt = map[string]bool{
	"":                true,
	"hello":           true,
	"reconnect_url":   true,
	"user_typing":     true,
	"presence_change": true,
}

// gridPerWorkspaceFrames lists frame types whose handlers write
// per-workspace state. These must run on every connected session of
// the grid: each session's WorkspaceContext keeps its own copy of that
// state, and deduping the frame would update only whichever socket won
// the delivery race - leaving the others stale until the next boot,
// with a stale DND flag silently unsilencing notifications. The set is
// a denylist rather than an allowlist on purpose: a frame type not
// listed here is at worst processed redundantly, the pre-dedupe
// behaviour, while a type wrongly listed as shared would silently go
// stale.
var gridPerWorkspaceFrames = map[string]bool{
	// Mute store (wsCtx.MuteStore via OnPrefChange).
	"pref_change": true,
	// Own DND (wsCtx.DNDEnabled/DNDEndTS via OnDNDChange).
	"dnd_updated": true, "dnd_updated_user": true,
	// Own presence (wsCtx.Presence via OnSelfPresenceChange).
	"manual_presence_change": true,
	// Sidebar sections (wsCtx.SectionStore via OnChannelSection*).
	"channel_section_upserted":           true,
	"channel_section_deleted":            true,
	"channel_sections_channels_upserted": true,
	"channel_sections_channels_removed":  true,
	// Conversation lists (wsCtx.Channels / finder via
	// OnConversationOpened). The dispatcher maps all five types to it.
	"mpim_open": true, "im_created": true, "im_open": true,
	"group_joined": true, "channel_joined": true,
	// Thread subscriptions: UpsertThreadSubscription keys on
	// workspace_id, so each session must persist its own row - and an
	// unsubscribe must tombstone it on every session.
	"thread_subscribed": true, "thread_unsubscribed": true,
	// Thread read position: UpdateThreadLastRead* keys on
	// (workspace_id, channel, thread).
	"thread_marked": true,
	// Channel membership cache (wsCtx.Membership).
	"member_joined_channel": true, "member_left_channel": true,
	// Peer-status refresh queues (wsCtx.PeerStatus): a per-workspace
	// set insert that schedules that workspace's refetch, so every
	// session must see the invalidation.
	"user_invalidated": true, "dnd_invalidated": true,
}
