package core

import (
	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/ids"
)

// The values that cross between the engine and the TUI. Most began life
// in a TUI package, which still exposes them under their old names as
// aliases.

// ForwardResult identifies the message Slack posted in the destination.
// Text is the source permalink; preview attachments arrive via WebSocket.
type ForwardResult struct {
	TS   ids.MessageTS
	Text string
}

// MessageItem is one message as the TUI displays it.
type MessageItem struct {
	TS          string
	UserName    string
	UserID      string
	Text        string
	Timestamp   string // formatted display time (e.g. "3:04 PM")
	DateStr     string // date string for grouping (e.g. "2026-04-23")
	ThreadTS    string
	ReplyCount  int
	Reactions   []ReactionItem
	Attachments []Attachment
	IsEdited    bool
	// Subtype mirrors Slack's `subtype` field on a message event.
	// Currently we only act on "thread_broadcast" (a thread reply that
	// was also sent to the channel) so we can render a label above it.
	Subtype string

	// Blocks holds parsed Slack Block Kit blocks. Rendered between
	// the body Text and the file Attachments by Phase 5.
	Blocks []blocks.Block

	// LegacyAttachments holds parsed entries from the legacy
	// `attachments` field (color stripe + title + fields style bot
	// cards). Rendered after Blocks.
	LegacyAttachments []blocks.LegacyAttachment
}

// Attachment represents a file or image attached to a message.
// Kind is "image" for image/* mimetypes, "file" otherwise.
// URL is the user-facing permalink (preferred) or fallback to url_private.
type Attachment struct {
	Kind string // "image" or "file"
	Name string // display filename / title
	URL  string // permalink (preferred) or url_private

	// DownloadURL is the auth-gated url_private, used by the `d`
	// download keybinding. Size is the file size in bytes (0 when
	// Slack didn't provide one); shown in the file picker.
	DownloadURL string
	Size        int64

	// Populated only for Kind == "image":
	FileID string      // Slack file ID for cache key
	Mime   string      // e.g. "image/png"
	Thumbs []ThumbSpec // sorted ascending; empty for non-image
}

// ThumbSpec is one Slack thumbnail variant.
//
// This is intentionally distinct from image.ThumbSpec in the internal/image
// package to avoid coupling message data to the image package's internal
// type. A converter helper bridges the two where needed.
type ThumbSpec struct {
	URL string
	W   int
	H   int
}

type ReactionItem struct {
	Emoji      string // emoji name without colons, e.g. "thumbsup"
	Count      int
	HasReacted bool     // whether the current user has reacted with this emoji
	UserIDs    []string // user IDs who reacted with this emoji
}

// ChannelFinderItem represents a searchable channel/DM entry.
type ChannelFinderItem struct {
	ID       string
	Name     string
	Type     string // channel, dm, group_dm, private, threads
	Presence string // for DMs: active, away
	Joined   bool   // true if the user is already a member; false for browseable public channels
	// LastVisited is the unix timestamp (seconds) of the user's most
	// recent visit to this channel; 0 means never visited. Drives the
	// recency-based sort used by filter(): empty-query order is by
	// LastVisited DESC, and on a query LastVisited breaks ties within
	// a match tier.
	LastVisited int64
	// Synthetic marks non-channel destinations (e.g. "Threads") that
	// the finder pins above real channels under empty-query and that
	// callers route differently (e.g. activating a view rather than
	// opening a channel). These items are preserved across SetItems
	// and SetBrowseable mutations so the finder always offers them.
	Synthetic bool
}

// EmojiEntry represents an emoji with its name and Unicode character.
type EmojiEntry struct {
	Name    string // e.g. "thumbsup"
	Unicode string // e.g. "\U0001f44d"
}

// PendingAttachment is a file (or in-memory image) waiting to be
// uploaded with the next send. Bytes and Path are mutually exclusive:
// Bytes is set for clipboard-pasted images; Path is set for
// file-path-pasted files (read at upload time, not at attach time).
type PendingAttachment struct {
	Filename string
	Bytes    []byte // non-nil for clipboard images
	Path     string // non-empty for file-path attachments
	Mime     string
	Size     int64
}

// PresenceAction is the high-level operation the user picked in the
// presence menu.
type PresenceAction int

const (
	PresenceSetActive PresenceAction = iota
	PresenceSetAway
	PresenceSnooze       // SnoozeMinutes is set
	PresenceCustomSnooze // opens the custom-snooze input; never reaches the engine
	PresenceEndDND
)

// ThemeScope identifies whether a theme selection should be saved to
// the active workspace or to the global default.
type ThemeScope int

const (
	ThemeScopeGlobal ThemeScope = iota
	ThemeScopeWorkspace
)

// ReadState captures the per-channel read-state values that drive the
// unread dot, the mention badge, and the "new messages" line. It is the
// canonical type for passing read state across package boundaries.
type ReadState struct {
	LastReadTS string
	HasUnread  bool
	// MentionCount is the number of unread direct mentions: an explicit
	// @user or an @here/@channel/@everyone broadcast. For DMs and group
	// DMs (Slack's ims and mpims), client.counts is believed to report
	// every unread message here, which is what makes the sidebar badge
	// match the official client without a client-side branch — that
	// reading is unverified against a live capture; see UnreadInfo's doc
	// in internal/slack/client.go. Rendering gates it on HasUnread.
	MentionCount int
}

// ThreadSummary is one row in the Threads view: a thread the user is
// involved in (authored, replied to, or @-mentioned in). Computed from
// the local cache; v1 has no Slack-side authoritative data.
type ThreadSummary struct {
	ChannelID    string
	ChannelName  string
	ChannelType  string // "channel" | "private" | "dm" | "group_dm"
	ThreadTS     string
	ParentUserID string
	ParentText   string
	ParentTS     string
	ReplyCount   int // number of replies (does not count the parent)
	LastReplyTS  string
	LastReplyBy  string
	Unread       bool
}

// ActivityItem is a flattened, UI-friendly representation of a single
// activity.feed entry. The per-type extraction happens in the Slack
// client so the UI never touches the raw (and wildly type-dependent)
// JSON.
type ActivityItem struct {
	Key       string // stable dedupe/selection id
	Type      string // at_user, at_channel, thread_v2, message_reaction, dm, bot_dm_bundle, ...
	IsUnread  bool
	IsBot     bool
	FeedTS    string // sort key (newest first)
	ChannelID string // resolved target channel (from message OR bundle payload)
	TS        string // resolved target message ts (may be "")
	ThreadTS  string // set for thread_v2 (== thread root)
	AuthorID  string // message author (mentions); "" when unknown
	Reaction  string // emoji short name, for message_reaction only
}

// ActivityMessage is a hydrated message body for one activity ref
// (channel + ts). The Activity feed itself returns only references; the
// body text and author are fetched separately via messages.list.
type ActivityMessage struct {
	Text   string
	UserID string
}

// ActivityMsgKey is the map key for a hydrated activity message,
// combining channel ID and message ts. Shared by the Slack client and
// the activity view so both sides agree on the lookup key.
func ActivityMsgKey(channelID, ts string) string {
	return channelID + "\x00" + ts
}

// Theme holds the user's color overrides from config.
type Theme struct {
	Primary     string `toml:"primary"`
	Accent      string `toml:"accent"`
	Warning     string `toml:"warning"`
	Error       string `toml:"error"`
	Background  string `toml:"background"`
	Surface     string `toml:"surface"`
	SurfaceDark string `toml:"surface_dark"`
	Text        string `toml:"text"`
	TextMuted   string `toml:"text_muted"`
	Border      string `toml:"border"`
}
