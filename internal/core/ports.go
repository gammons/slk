// Service ports the TUI calls. Implementations are wired by cmd/slk.
//
// Each port's adapter, constructor, and closure-bundle types live in
// adapters.go — this file holds only the interfaces themselves, so the
// package's public contract can be read without the wiring plumbing.
package core

import (
	"context"
	"image"
	"io/fs"

	"github.com/gammons/slk/internal/ids"
	imgpkg "github.com/gammons/slk/internal/image"
)

// ReactionService is the App's interface to the Slack reaction API
// and the user's recent-emoji-use history (frecency). Implementations
// are wired by cmd/slk/main.go.
//
// All methods are best-effort and nil-safe at the adapter level: an
// implementation built via NewReactionService with a nil component
// silently no-ops that operation.
type ReactionService interface {
	// Add adds emoji to messageTS in channelID. Returns an error if
	// the Slack API call fails; App turns that into a status-bar toast.
	Add(channelID ids.ChannelID, messageTS ids.MessageTS, emoji string) error

	// Remove removes the current user's emoji reaction from messageTS
	// in channelID.
	Remove(channelID ids.ChannelID, messageTS ids.MessageTS, emoji string) error

	// LoadFrecent returns up to limit emoji entries from the user's
	// recent-use history, ordered by frecency. May return nil; the
	// reaction picker handles an empty slice as "no recents yet".
	LoadFrecent(limit int) []EmojiEntry

	// RecordFrecent records emoji as recently used so future
	// LoadFrecent calls surface it. Called after every successful
	// reaction add.
	RecordFrecent(emoji string)
}

// ThreadService is the App's interface to Slack's thread surfaces:
// fetching replies, marking threads read, posting replies, and loading
// the involved-threads list for the user's threads view. Includes
// ThreadLastRead because the thread panel needs the thread's own
// last-read cursor to render its unread boundary.
//
// Implementations are wired by cmd/slk/main.go. Build one via
// NewThreadService from a ThreadServiceFuncs struct so unused
// methods can be left nil without trailing positional nils.
type ThreadService interface {
	// Fetch retrieves replies for threadTS in channelID from Slack.
	// Returns a Msg (typically ThreadRepliesLoadedMsg).
	Fetch(channelID ids.ChannelID, threadTS ids.ThreadTS) Msg

	// CacheRead returns cached replies (or nil) so the thread panel
	// can populate without waiting for the network. A non-empty
	// return causes immediate render; the subsequent Fetch result
	// overwrites with authoritative data.
	CacheRead(channelID ids.ChannelID, threadTS ids.ThreadTS) []MessageItem

	// Mark marks the thread as read on Slack's servers
	// (subscriptions.thread.mark) and, on success, advances the local
	// thread_subscriptions cursor. channelID is the parent channel,
	// threadTS is the parent message ts, ts is the latest reply ts the
	// user has now seen. Returns a Cmd yielding ThreadMarkedLocalMsg.
	Mark(channelID ids.ChannelID, threadTS ids.ThreadTS, ts ids.MessageTS) Cmd

	// SendReply posts a reply to threadTS in channelID. When broadcast
	// is true the reply is also posted to the parent channel feed
	// (reply_broadcast=true). Returns a Msg (typically
	// ThreadReplySentMsg or ThreadReplySendFailedMsg).
	SendReply(channelID ids.ChannelID, threadTS ids.ThreadTS, text string, broadcast bool) Msg

	// ListFetch loads the involved-threads list for the workspace
	// (Slack subscriptions.list). Returns a Msg (typically
	// ThreadsListLoadedMsg).
	ListFetch(teamID ids.TeamID) Msg

	// EnsureSubscriptions kicks the workspace's throttled thread-
	// subscription sync (subscriptions.thread.getView) in the
	// background. Called on workspace-ready and on Threads-view
	// activation; the implementation collapses those to at most one
	// network sweep per throttle window, so callers fire
	// unconditionally.
	//
	// Separate from ListFetch because ListFetch is cache-only and runs
	// on workspace-ready (for the sidebar's Threads badge), whereas
	// this may issue subscriptions.thread.getView, which paginates to
	// a 1000-item hard cap — measured at ~62 requests per workspace.
	EnsureSubscriptions(teamID ids.TeamID)

	// ThreadLastRead returns the thread's own last-read cursor from
	// thread_subscriptions so the thread panel can render a "── new ──"
	// boundary. The parent channel's cursor is NOT a substitute: plain
	// thread replies never advance it, so it is systematically stale and
	// puts the divider too early. Optional; "" disables the boundary.
	ThreadLastRead(channelID ids.ChannelID, threadTS ids.ThreadTS) string
}

// ActivityService is the App's interface to Slack's Activity feed
// (activity.feed): the notified-items list surfaced by the Activity
// view — @mentions, thread replies, reactions to your messages, and
// DMs. Implementations are wired by cmd/slk/main.go.
//
// Marking items read is out of scope: opening an item marks the
// underlying conversation read via existing paths.
type ActivityService interface {
	// Fetch loads the first page of the Activity feed for teamID.
	// limit is the max items; unreadOnly requests the server-side
	// unread filter. Returns a Msg (typically ActivityListLoadedMsg).
	Fetch(teamID ids.TeamID, limit int, unreadOnly bool) Msg

	// Hydrate fetches message bodies for the Activity page's refs
	// (activity.feed returns refs only). refs maps channel ID to the
	// wanted message timestamps. Returns a Msg (typically
	// ActivityBodiesLoadedMsg), or nil when there's nothing to fetch.
	Hydrate(teamID ids.TeamID, refs map[string][]string) Msg
}

// MessageService is the App's interface to Slack's per-message
// operations: send, forward, edit, delete, mark-unread, and permalink lookup.
// Implementations are wired by cmd/slk/main.go.
//
// Methods are nil-safe at the adapter level. A nil Forward component
// returns an unsupported error; other nil components silently no-op
// (returning nil Msg or ("", nil) for Permalink).
type MessageService interface {
	// Send dispatches chat.postMessage for channelID with text.
	// Returns a Msg (typically MessageSentMsg or
	// MessageSendFailedMsg).
	Send(channelID ids.ChannelID, text string) Msg

	// Forward posts the source message's permalink to destinationChannelID
	// with Slack's native preview. teamID is captured when forwarding begins,
	// not read from the active workspace when this synchronous call executes.
	// Callers run it off the Update loop; it does not produce send/compose events.
	// On success, returns the actual posted timestamp and permalink text.
	Forward(ctx context.Context, teamID string, sourceChannelID ids.ChannelID, ts ids.MessageTS, destinationChannelID ids.ChannelID) (ForwardResult, error)

	// Edit dispatches chat.update for the message identified by
	// (channelID, ts), replacing its text with newText.
	// Returns a Msg (typically MessageEditedMsg).
	Edit(channelID ids.ChannelID, ts ids.MessageTS, newText string) Msg

	// Delete dispatches chat.delete for the message identified by
	// (channelID, ts). Returns a Msg (typically MessageDeletedMsg).
	Delete(channelID ids.ChannelID, ts ids.MessageTS) Msg

	// MarkUnread dispatches conversations.mark (channel-level) or
	// subscriptions.thread.mark (when threadTS != "") with the
	// rolled-back boundaryTS. unreadCount is forwarded to the result
	// for the sidebar's badge update. Returns a Msg (typically
	// MessageMarkedUnreadMsg).
	MarkUnread(channelID ids.ChannelID, threadTS ids.ThreadTS, boundaryTS ids.MessageTS, unreadCount int) Msg

	// Permalink resolves the Slack permalink URL for the message
	// identified by (channelID, ts). Used by the copy-permalink
	// keybind. Synchronous (HTTP); callers wrap in a goroutine to
	// avoid blocking the Update loop.
	Permalink(ctx context.Context, channelID ids.ChannelID, ts ids.MessageTS) (string, error)
}

// ChannelService is the App's interface to the Slack channels API,
// the local SQLite channel cache, and per-channel session bookkeeping
// (visit timestamps, navigation-history lookups, membership fetches).
// Implementations are wired by cmd/slk/main.go.
//
// Largest service in the App. Mixes three concerns that happen to
// share the channel-as-domain-object boundary:
//   - Slack API: Fetch, FetchOlder, MarkRead, Join.
//   - Local cache: ReadCache, SyncedAt.
//   - Session bookkeeping: Lookup, RecordVisit, MembershipFetch.
//
// All methods are best-effort and nil-safe at the adapter level.
type ChannelService interface {
	// Fetch loads the most-recent messages for channelID from Slack.
	// channelName is for log context. Returns a Msg (typically
	// MessagesLoadedMsg).
	Fetch(channelID ids.ChannelID, channelName string) Msg

	// FetchOlder loads messages older than oldestTS for the
	// channel-history backfill triggered by scroll-past-top.
	// Returns a Msg (typically OlderMessagesLoadedMsg).
	FetchOlder(channelID ids.ChannelID, oldestTS ids.MessageTS) Msg

	// FetchAround loads a history window centered on ts for
	// jump-to-message navigation. Returns a Msg (typically
	// MessagesAroundLoadedMsg).
	FetchAround(channelID ids.ChannelID, ts ids.MessageTS) Msg

	// ReadCache returns the local-cache snapshot of channelID's
	// recent messages, or nil if no cache exists. Used by
	// ChannelSelectedMsg's tiered render policy.
	ReadCache(channelID ids.ChannelID) []MessageItem

	// SyncedAt returns the unix-seconds timestamp of the channel's
	// last authoritative cache-from-network sync, or 0 if never
	// synced. Used by ChannelSelectedMsg's tiered render policy to
	// decide between cache-only, cache-and-verify, and spinner-only
	// render.
	SyncedAt(channelID ids.ChannelID) int64

	// MarkRead dispatches conversations.mark + UpdateChannelReadState
	// to bring the channel's last_read_ts up to ts. Used by Tier 1
	// of ChannelSelectedMsg when cache is provably fresh. Returns
	// a Msg (typically ChannelMarkedReadMsg).
	MarkRead(channelID ids.ChannelID, ts ids.MessageTS) Msg

	// Lookup returns metadata (name, channelType) for channelID, or
	// ok=false if the channel is no longer available in the active
	// workspace. Used by navHistoryStore.Walk to skip stale entries.
	Lookup(channelID ids.ChannelID) (name, channelType string, ok bool)

	// Join sends conversations.join for channelID. channelName is
	// for log context. Returns a Msg (typically ChannelJoinedMsg
	// or ChannelJoinFailedMsg).
	Join(channelID ids.ChannelID, channelName string) Msg

	// RecordVisit persists a visit to channelID (SQLite write +
	// WorkspaceContext last-visited map update). Fired once per
	// ChannelSelectedMsg regardless of FromHistory.
	RecordVisit(channelID ids.ChannelID)

	// MembershipFetch asks membership.Manager to ensure-fresh the
	// member set for channelID. Fire-and-forget; results arrive
	// asynchronously via ChannelMembershipMsg.
	MembershipFetch(channelID ids.ChannelID)

	// OpenConversation dispatches conversations.open for userIDs (1
	// recipient = IM, 2-8 recipients = MPIM). Returns a Cmd whose
	// resolved Msg is NewMessageOpenedMsg on success or
	// NewMessageFailedMsg on error; both carry requestID so the
	// reducer can drop late results from cancelled submits.
	OpenConversation(userIDs []string, requestID uint64) Cmd

	// SearchRemote asks the server which channels match query,
	// including ones the user has not joined, and blocks until it
	// answers. Callers run it from a Cmd, debounced — see
	// App.scheduleChannelSearch.
	//
	// It replaced a background conversations.list walk that ran at
	// boot on every workspace, whether or not the finder was ever
	// opened. Returning nil (no client, or a failed request) leaves
	// the finder showing local matches only, which is what it showed
	// before this existed.
	SearchRemote(query string) []ChannelFinderItem
}

// SearchService runs message searches. SearchChannel queries the local
// FTS cache for one channel; SearchWorkspace queries Slack's
// search.messages for the active workspace.
type SearchService interface {
	// SearchChannel returns a ChannelSearchResultsMsg for query in
	// channelID's cached history.
	SearchChannel(channelID ids.ChannelID, query string) Msg
	// SearchWorkspace returns a WorkspaceSearchResultsMsg for query
	// across the active workspace (server-side).
	SearchWorkspace(query string) Msg
}

// FileService moves files between the user and Slack.
type FileService interface {
	// Upload sends attachments to channelID (threadTS for a thread
	// reply); caption goes on the last one. The returned Cmd yields
	// UploadResultMsg; progress arrives separately as UploadProgressMsg.
	Upload(channelID, threadTS, caption string, attachments []PendingAttachment) Cmd

	// Download saves the auth-gated file at url locally and returns its
	// path. name is the file's display name.
	Download(ctx context.Context, url, name string) (string, error)
}

// ClipboardFormat selects what DesktopService.ReadClipboard returns.
type ClipboardFormat int

const (
	ClipboardText ClipboardFormat = iota
	ClipboardImage
)

// DesktopService is the host OS: launching apps, the system clipboard,
// the filesystem, and the external status command.
type DesktopService interface {
	// Open hands target (a URL or file path) to the OS default handler.
	Open(target string) error

	// ReadClipboard returns the clipboard contents in format f, or nil.
	ReadClipboard(f ClipboardFormat) []byte

	// Stat describes the file at path.
	Stat(path string) (fs.FileInfo, error)

	// SaveThread writes the thread as Markdown to the export directory
	// and returns the file's path.
	SaveThread(parent MessageItem, replies []MessageItem, userNames, channelNames map[string]string, channelName string) (string, error)

	// ReportStatus mirrors the unread state onto the user's configured
	// status command, if any.
	ReportStatus(unread, otherUnread int, workspace, title string)
}

// EditorService runs the user's external editor over a compose draft,
// handed over in a temp file.
type EditorService interface {
	// WriteDraft saves text to a new temp file and returns its path.
	WriteDraft(text string) (path string, err error)

	// Edit returns a Cmd that suspends the TUI while argv edits path.
	// done builds the message delivered once the editor exits; its
	// error has an ExitCode method when the editor ran and exited
	// non-zero.
	Edit(argv []string, path string, done func(err error) Msg) Cmd

	// TakeDraft reads the edited draft back and removes the file, even
	// when the read fails.
	TakeDraft(path string) (string, error)
}

// PresenceService sets the user's own status and broadcasts typing.
type PresenceService interface {
	// SetStatus applies a presence-menu choice; snoozeMinutes is set
	// for PresenceSnooze.
	SetStatus(action PresenceAction, snoozeMinutes int)

	// SendTyping broadcasts a typing indicator for channelID. Called off
	// the Update goroutine.
	SendTyping(channelID string)
}

// SettingsService persists the user's display preferences.
type SettingsService interface {
	SaveTheme(name string, scope ThemeScope)
	SaveSidebarWidth(width int)
}

// UnreadService reads the unread state the sidebar and workspace rail
// render. Both methods are local reads, called at render time.
type UnreadService interface {
	// ChannelReadStates returns the active workspace's per-channel read
	// state, keyed by channel ID.
	ChannelReadStates() map[string]ReadState

	// UnreadWorkspaces returns the IDs of workspaces with at least one
	// channel their sidebar would show as unread.
	UnreadWorkspaces() []string
}

// WorkspaceService switches the active workspace.
type WorkspaceService interface {
	// Switch makes teamID active and returns WorkspaceSwitchedMsg (or
	// nil if the workspace isn't connected).
	Switch(teamID string) Msg
}

// AvatarService renders user avatars for the message panes.
type AvatarService interface {
	// Avatar returns the rendered half-block avatar for userID, or ""
	// while it isn't available yet.
	Avatar(userID string) string
}

// ImageFetcher downloads and caches remote images for inline rendering
// and keeps pre-encoded renders of them. *image.Fetcher implements it.
//
// Deliberate exemption from the boundary internal/ui/boundary_test.go
// enforces elsewhere: this port's vocabulary
// (imgpkg.FetchRequest/FetchResult/Protocol/Render/KittyRenderer) is
// borrowed directly from internal/image rather than mirrored as core
// types, because internal/image's own protocol-negotiation surface
// (sixel/Kitty capability detection, incremental decode) would have to
// be duplicated in core to avoid it. Revisit if that cost stops being
// worth it — see TestCoreDoesNotImportTUIOrDirectIO.
type ImageFetcher interface {
	Fetch(ctx context.Context, req imgpkg.FetchRequest) (imgpkg.FetchResult, error)
	Cached(key string, target image.Point) (image.Image, bool)
	Prerendered(key string, cellTarget image.Point, proto imgpkg.Protocol) (imgpkg.Render, bool)
	ConfigurePrerender(proto imgpkg.Protocol)
	ConfigurePrerenderKitty(kr *imgpkg.KittyRenderer)
}
