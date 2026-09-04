// internal/ui/msgs.go
//
// Inter-component message types for the bubbletea Update loop.
//
// Phase 1 of the SOLID refactor of internal/ui/app.go: this file collects
// every *Msg type that previously lived in app.go alongside the App
// struct. Moving them out keeps app.go focused on the App lifecycle
// (Init/Update/View) instead of also being the catalog of every event
// the program emits.
//
// No semantic change in this commit: same package, same declarations,
// same field tags. Anything that imported these by name continues to
// compile unchanged.
package ui

import (
	"image"
	"time"

	"github.com/gammons/slk/internal/cache"
	emojiutil "github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/searchresults"
	"github.com/gammons/slk/internal/ui/sidebar"
)

// EmojiImageReadyMsg re-exports emoji.EmojiImageReadyMsg so reducers
// can refer to it without an extra import. Dispatched when a previously
// cold-cache emoji finishes fetching and is now warm-renderable across
// every UI surface that renders emoji.
type EmojiImageReadyMsg = emojiutil.EmojiImageReadyMsg

// emojiInvalidateMsg is dispatched by the debounce timer scheduled from
// the EmojiImageReadyMsg reducer arm. When it lands, the App performs a
// single wholesale cache invalidation across every emoji-rendering
// surface. All EmojiImageReadyMsg arrivals during the debounce window
// collapse to this one invalidation — without coalescing a busy channel
// with N cold-cache emoji would produce N full rebuilds (a few hundred
// renderMessagePlain calls each) on the UI thread in rapid succession,
// presenting as a multi-second freeze.
//
// Lowercase (unexported) because no other package dispatches this — it
// is purely an internal debounce signal.
type emojiInvalidateMsg struct{}

// Messages sent between components
type (
	ChannelSelectedMsg struct {
		ID   string
		Name string
		// Type is the channel type ("channel", "private", "dm",
		// "group_dm"); used to render a type-aware glyph in the
		// message-pane header and status bar. May be empty when
		// callers don't yet know the type — the UI then falls
		// back to a default `#` glyph.
		Type string
		// FromHistory marks navigations synthesized by Ctrl+H /
		// Ctrl+K. The case ChannelSelectedMsg handler suppresses
		// pushing onto navHistory when this is true so back/forward
		// walks don't grow the stack on every step. Visit recording
		// is unaffected — going back to a channel still updates its
		// last-visited timestamp.
		FromHistory bool
	}
	MessagesLoadedMsg struct {
		ChannelID  string
		Messages   []messages.MessageItem
		LastReadTS string
		// MarkedTS is the ts the fetcher already marked the channel
		// read at, or "" if it marked nothing. ChannelService.Fetch
		// marks on entry and sets this; the reconnect refresh
		// (rtmEventHandler.refreshChannel in cmd/slk/main.go) reuses
		// this message but deliberately does not mark, and leaves it
		// empty. The reducer records it
		// as a self-mark so the resulting channel_marked echo does not
		// drag the divider off LastReadTS. See selfMarkDedup.
		MarkedTS string
	}
	OlderMessagesLoadedMsg struct {
		ChannelID string
		// AnchorTS is the OldestTS the fetch was keyed to (the
		// buffer's oldest message at dispatch time). The reducer
		// drops the result if the buffer's oldest no longer matches
		// — that means the buffer was replaced mid-flight (e.g. by a
		// jump-to-message FetchAround) and prepending would splice an
		// unrelated older block onto the new window.
		AnchorTS string
		Messages []messages.MessageItem
	}
	// MessagesAroundLoadedMsg delivers a history window fetched around
	// TargetTS (jump-to-message navigation: search matches, search
	// results, permalinks whose target is outside the loaded buffer).
	// The reducer replaces the pane buffer and selects TargetTS.
	MessagesAroundLoadedMsg struct {
		ChannelID string
		TargetTS  string
		Messages  []messages.MessageItem // ascending by TS, like MessagesLoadedMsg
		Err       error
	}
	// ChannelSearchResultsMsg delivers in-channel FTS results for the `/`
	// search. TSes are match timestamps newest-first; Terms are the folded
	// query terms for highlighting. Empty TSes = no matches. Gen echoes
	// App.searchGen at dispatch time (stamped UI-side in mode_search.go);
	// the reducer drops results from a superseded generation.
	ChannelSearchResultsMsg struct {
		ChannelID string
		Query     string
		Terms     []string
		TSes      []string
		Gen       uint64
		Err       error
	}
	// WorkspaceSearchResultsMsg delivers server-side search.messages
	// results for the ctrl+f modal. Total is Slack's reported total match
	// count (may exceed len(Items); v1 fetches the first page only).
	WorkspaceSearchResultsMsg struct {
		Query string
		Items []searchresults.Item
		Total int
		Err   error
	}
	NewMessageMsg struct {
		ChannelID string
		Message   messages.MessageItem
	}
	SendMessageMsg struct {
		ChannelID string
		Text      string
	}
	ThreadOpenedMsg struct {
		ChannelID string
		ThreadTS  string
		ParentMsg messages.MessageItem
	}
	ThreadRepliesLoadedMsg struct {
		ThreadTS string
		Replies  []messages.MessageItem
	}
	SendThreadReplyMsg struct {
		ChannelID string
		ThreadTS  string
		Text      string
	}
	ThreadReplySentMsg struct {
		ChannelID string
		ThreadTS  string
		LocalTS   string // optimistic-placeholder id; see MessageSentMsg.LocalTS
		Message   messages.MessageItem
	}
	// ThreadReplySendFailedMsg is returned when chat.postMessage for a
	// thread reply fails. Mirrors MessageSendFailedMsg.
	ThreadReplySendFailedMsg struct {
		ChannelID string
		ThreadTS  string
		LocalTS   string
		Reason    string
	}
	// ThreadsViewActivatedMsg is dispatched when the user picks the
	// synthetic Threads sidebar row. The App switches the message pane to
	// the threads-list view and (re)fetches the involved-threads list.
	ThreadsViewActivatedMsg struct{}
	// ThreadsListLoadedMsg carries a freshly loaded list of involved-thread
	// summaries for the named workspace. The App ignores it if it doesn't
	// match the active team.
	ThreadsListLoadedMsg struct {
		TeamID    string
		Summaries []cache.ThreadSummary
		// SubscriptionsAvailable reflects whether the most recent
		// subscription sync succeeded in fetching the authoritative
		// thread-subscription list. The threads view renders a banner
		// when false (Task 10 wires the renderer).
		SubscriptionsAvailable bool
	}
	// ThreadsListDirtyMsg is dispatched when something that could affect
	// the involved-threads list has changed (new message, mention, etc.)
	// and the list should be refetched. Ignored if not the active team.
	ThreadsListDirtyMsg struct {
		TeamID string
	}
	ConnectionStateMsg struct {
		State int // 0=connecting, 1=connected, 2=disconnected
	}
	ReactionAddedMsg struct {
		ChannelID string
		MessageTS string
		UserID    string
		Emoji     string
	}
	ReactionRemovedMsg struct {
		ChannelID string
		MessageTS string
		UserID    string
		Emoji     string
	}
	ReactionSentMsg struct {
		// Identifies the optimistic update to roll back if the API
		// call failed. Remove is the operation that was attempted, so
		// the rollback applies its inverse.
		ChannelID string
		MessageTS string
		Emoji     string
		UserID    string
		Remove    bool
		Err       error
	}
	ChannelMarkedReadMsg struct {
		ChannelID string
	}
	DMNameResolvedMsg struct {
		ChannelID   string
		DisplayName string
		// IsBot is true when the resolved peer is a Slack app or bot.
		// On first run (before the background users.list fetch has
		// landed), bot DMs are initially classified as Type="dm" and
		// the resolveUser path discovers the IsBot flag asynchronously
		// via users.info. The App handler flips Type to "app" when
		// this lands so the row hops into the Apps section live.
		IsBot bool
	}
	// UserResolvedMsg arrives asynchronously after main.go's per-workspace
	// userResolver completes a users.info round-trip for a previously-
	// unknown message author. The handler patches the in-memory display
	// name on the messagepane and threadPanel so rows authored by this
	// user re-render with the real name on the next View().
	UserResolvedMsg struct {
		TeamID      string
		UserID      string
		DisplayName string
		IsBot       bool
	}
	// UserExternalMsg flags a single user as external (Slack Connect /
	// shared-channel guest). Emitted by the user-resolution path when a
	// users.info response shows team_id != workspace TeamID. The App
	// updates externalUsers and re-pushes user-list state to the pickers.
	UserExternalMsg struct {
		UserID     string
		IsExternal bool
	}
	WorkspaceSwitchedMsg struct {
		TeamID   string
		TeamName string
		// Domain is the workspace's slack.com subdomain (e.g.
		// "truelist-workspace"), from auth.test. Used to decide
		// whether an archive permalink belongs to this workspace.
		Domain       string
		Theme        string // resolved theme name (per-workspace or global default)
		SidebarWidth int    // resolved sidebar width (per-workspace or global default)
		Channels     []sidebar.ChannelItem
		FinderItems  []channelfinder.Item
		UserNames    map[string]string
		// ExternalUsers maps userID -> true for users this workspace
		// considers Slack Connect / shared-channel guests. Hydrated from
		// cache.User.IsExternal so the mention picker can flag externals
		// at workspace-switch time, before any new userResolver lookups
		// fire. Order matters: the App handler applies this BEFORE
		// SetUserNames so the picker rebuild sees the external flags.
		ExternalUsers map[string]bool
		UserID        string
		CustomEmoji   map[string]string
		UserGroups    map[string]string
		// SectionsProvider supplies Slack-native sidebar sections for this
		// workspace. Nil means "use config-glob behavior" (the App's
		// sidebar reverts to its existing name-keyed buckets).
		SectionsProvider sidebar.SectionsProvider
	}
	// ReadStateChangedMsg is sent whenever the persistent read state changes,
	// so panels that read from cache.GetWorkspaceReadState re-render.
	// ChannelID may be "" if the change is multi-channel (e.g., batch update
	// from reconnect catch-up).
	ReadStateChangedMsg struct {
		WorkspaceID string
		ChannelID   string
	}
	// ConversationOpenedMsg is sent when Slack delivers an mpim_open,
	// im_created, group_joined, or channel_joined event. The TeamID
	// disambiguates events for inactive workspaces; only events whose
	// TeamID matches the currently-active workspace mutate the live
	// sidebar — others are persisted in the workspace's WorkspaceContext
	// for when the user switches in.
	ConversationOpenedMsg struct {
		TeamID string
		Item   sidebar.ChannelItem
	}
	// SectionsRefreshedMsg is sent when a workspace's Slack-native
	// section state has mutated (via channel_section_* WS events) and
	// the channel list needs to be re-bucketed in the sidebar. Channels
	// carries a fresh slice with updated Section fields. The App only
	// rebuckets if TeamID matches the active workspace; inactive
	// workspaces have already been mutated in-place in their
	// WorkspaceContext.
	SectionsRefreshedMsg struct {
		TeamID   string
		Channels []sidebar.ChannelItem
	}
	// ChannelMembershipMsg is published by membership.Manager when a
	// channel's member set has been loaded, updated by a delta event, or
	// refreshed after a TTL miss / reconnect. MemberIDs is the full member
	// list (not a delta). May be sent for non-active channels — the App
	// forwards to both compose pickers, which gate on activeChannelID.
	ChannelMembershipMsg struct {
		ChannelID string
		MemberIDs []string
	}
	WorkspaceReadyMsg struct {
		TeamID   string
		TeamName string
		// Domain is the workspace's slack.com subdomain (e.g.
		// "truelist-workspace"), from auth.test. Used to decide
		// whether an archive permalink belongs to this workspace.
		Domain       string
		Theme        string // resolved theme name (per-workspace or global default)
		SidebarWidth int    // resolved sidebar width (per-workspace or global default)
		Channels     []sidebar.ChannelItem
		FinderItems  []channelfinder.Item
		UserNames    map[string]string
		// ExternalUsers maps userID -> true for users this workspace
		// considers Slack Connect / shared-channel guests. Hydrated from
		// cache.User.IsExternal so the mention picker can flag externals
		// on startup, before any new userResolver lookups fire. Order
		// matters: the App handler applies this BEFORE SetUserNames so
		// the picker rebuild sees the external flags.
		ExternalUsers map[string]bool
		UserID        string
		CustomEmoji   map[string]string
		UserGroups    map[string]string
		// SectionsProvider supplies Slack-native sidebar sections for this
		// workspace. Nil means "use config-glob behavior" (the App's
		// sidebar reverts to its existing name-keyed buckets).
		SectionsProvider sidebar.SectionsProvider
		// InitialActive is true for exactly one WorkspaceReadyMsg per
		// program run: the workspace whose team ID matches the configured
		// default_workspace, or — if no default is configured — the first
		// workspace to successfully connect. main.go enforces the uniqueness
		// via sync.Once + atomic router (Task 14). App's handler treats
		// InitialActive=false as "workspace is up; threads-list kick only".
		InitialActive bool
		// LastChannelID is the workspace's most-recently-visited channel
		// (from the persisted channel_visits table). On the InitialActive
		// workspace the App opens this channel on startup instead of the
		// sidebar's first entry, so a restart lands the user back where
		// they were. Empty (e.g. first ever run) falls back to Channels[0].
		LastChannelID string
	}
	// CustomEmojisLoadedMsg is sent when a workspace's custom emoji list
	// finishes loading in the background, after WorkspaceReadyMsg has
	// already fired with whatever the goroutine had written so far. App
	// refreshes the active compose's emoji entry list if this matches the
	// active workspace.
	CustomEmojisLoadedMsg struct {
		TeamID      string
		CustomEmoji map[string]string
	}
	// UserGroupsLoadedMsg is sent when a workspace's usergroups.list
	// fetch finishes in the background. App refreshes render caches and
	// compose mention entries only when TeamID matches the active workspace.
	UserGroupsLoadedMsg struct {
		TeamID     string
		UserGroups map[string]string
	}
	WorkspaceFailedMsg struct {
		TeamName string
	}
	// RemoteChannelsFoundMsg carries the answer to one debounced
	// channels/search request: the public channels matching Query,
	// including ones the user has not joined.
	//
	// It replaced a background walk of conversations.list at boot —
	// 4 requests at Limit: 1000 on a measured two-workspace start,
	// growing with the workspace and paid by every user whether or not
	// they ever opened the finder. Now nothing is fetched until
	// somebody types.
	//
	// Gen echoes the debounce generation so the reducer can drop a
	// slow answer to a query the user has already moved past; TeamID
	// does the same for a workspace switch mid-flight.
	RemoteChannelsFoundMsg struct {
		TeamID string
		Query  string
		Gen    uint64
		Items  []channelfinder.Item
	}
	SpinnerTickMsg    struct{}
	LoadingTimeoutMsg struct{}
	UserTypingMsg     struct {
		ChannelID   string
		UserID      string
		WorkspaceID string
	}
	TypingExpiredMsg  struct{}
	PresenceChangeMsg struct {
		UserID   string
		Presence string
	}
	// StatusChangeMsg is sent when the authenticated user's own presence
	// or DND state changes for any workspace. The App routes it to the
	// status bar only when TeamID matches the active workspace; otherwise
	// it just updates the App's per-workspace status cache.
	StatusChangeMsg struct {
		TeamID     string
		Presence   string // "active" or "away"; "" means unknown/unchanged
		DNDEnabled bool
		DNDEndTS   time.Time
	}
	// ToastMsg sets a transient string in the status bar's toast slot. Used
	// for short error notices (e.g. failed status change). Auto-clears after
	// 3 seconds via a CopiedClearMsg tick scheduled by the App.
	ToastMsg struct{ Text string }
)

// autoScrollTickMsg is dispatched by tea.Tick while a drag is held near
// the top or bottom edge of a pane. Each tick scrolls the pane one line
// in the indicated direction, extends the selection to the new lastY,
// and (if still at an edge) schedules the next tick. The loop self-
// terminates when the cursor leaves the edge or the drag ends.
type autoScrollTickMsg struct{}

// threadFetchDebounceMsg is delivered after the user's threadsview selection
// stops moving for openThreadDebounceDelay. Carries the (channelID, threadTS,
// generation) the user had selected at scheduling time; if the App's current
// pendingThreadFetchGen has advanced past `gen`, the message is dropped — a
// later j/k has scheduled a fresh fetch.
type threadFetchDebounceMsg struct {
	channelID string
	threadTS  string
	gen       uint64
}

// MessageSentMsg is returned after a message is successfully sent.
// LocalTS, if non-empty, identifies the optimistic placeholder added
// when the user pressed Enter. The handler uses it to swap the
// placeholder for the authoritative Message in place. LocalTS may be
// empty for legacy/test callers that fire this message without a
// preceding SendMessageMsg.
type MessageSentMsg struct {
	ChannelID string
	LocalTS   string
	Message   messages.MessageItem
}

// MessageSendFailedMsg is returned when chat.postMessage fails. The
// App's handler removes the optimistic placeholder identified by
// LocalTS (if any) and shows a toast.
type MessageSendFailedMsg struct {
	ChannelID string
	LocalTS   string
	Reason    string
}

// EditMessageMsg is emitted when the user submits an edit. App.Update
// invokes the configured messageEditor and converts the result to
// MessageEditedMsg.
type EditMessageMsg struct {
	ChannelID string
	TS        string
	NewText   string
}

// DeleteMessageMsg is emitted when the user confirms a delete.
type DeleteMessageMsg struct {
	ChannelID string
	TS        string
}

// MessageEditedMsg carries the result of the chat.update API call.
type MessageEditedMsg struct {
	ChannelID string
	TS        string
	Err       error
}

// MessageDeletedMsg carries the result of the chat.delete API call.
type MessageDeletedMsg struct {
	ChannelID string
	TS        string
	Err       error
}

// OpenLinkMsg requests that a URL be opened. This is the single
// routing point for all link opens (issue #62): the reduceLinks
// reducer either navigates in-app (Slack archive permalinks for the
// active workspace) or launches the OS browser. Dispatched by the
// `o` keybinding (directly for single-link messages) and by the link
// picker modal.
type OpenLinkMsg struct{ URL string }

// DownloadFileMsg requests download + OS-open of a file attachment.
// Dispatched by the `d` keybinding (directly for single-file messages)
// and by the picker modal for multi-file messages. Handled by
// reduceFiles.
type DownloadFileMsg struct{ Attachment messages.Attachment }

// MarkUnreadMsg requests the App to mark the given message as unread.
// ThreadTS is "" for channel-level mark-unread; non-empty for thread-level
// (in which case ChannelID is the parent channel and BoundaryTS is the
// boundary within the thread). BoundaryTS is the ts that should become
// the new last_read watermark — i.e., the ts of the message immediately
// before the user's selection. UnreadCount is computed by the dispatcher
// from the loaded buffer at press time and is forwarded to the sidebar
// for an exact badge value (0 for thread-level, since the sidebar only
// tracks channel-level unreads).
type MarkUnreadMsg struct {
	ChannelID   string
	ThreadTS    string
	BoundaryTS  string
	UnreadCount int
}

// MessageMarkedUnreadMsg carries the result of a MarkUnreadFunc call.
// On success Err is nil and the App's Update arm applies the local
// state changes (move the unread boundary, update the sidebar badge,
// flip the threads-view row, emit a toast). On error Err is populated
// and the toast goes to the failure path; no local state mutates.
type MessageMarkedUnreadMsg struct {
	ChannelID   string
	ThreadTS    string
	BoundaryTS  string
	UnreadCount int
	Err         error
}

// ChannelMarkedRemoteMsg is dispatched by the WS event handler when
// Slack pushes a channel_marked / im_marked / group_marked / mpim_marked
// event (read state changed in another client, or via this client's own
// mark echoing back). The handler has already persisted the new
// last_read_ts to SQLite via cache.UpdateChannelReadState; the App's
// Update arm only updates the UI. No toast.
type ChannelMarkedRemoteMsg struct {
	ChannelID   string
	TS          string
	UnreadCount int
}

// ThreadMarkedRemoteMsg is dispatched by the WS event handler when
// Slack pushes a thread_marked event (a thread's read cursor moved in
// another client, or via slk's own subscriptions.thread.mark echoing
// back — the arm routes through applyThreadMarkEcho, which tells the
// two apart). LastRead is the thread's new read cursor; whether that
// means read or unread is decided by comparing it against the thread's
// newest known reply.
type ThreadMarkedRemoteMsg struct {
	ChannelID string
	ThreadTS  string
	LastRead  string
}

// ThreadMarkedLocalMsg reports the outcome of an slk-initiated
// subscriptions.thread.mark. Err is nil on success, in which case
// thread_subscriptions.last_read has already been advanced to TS.
type ThreadMarkedLocalMsg struct {
	ChannelID string
	ThreadTS  string
	TS        string
	Err       error
}

// WSMessageDeletedMsg is dispatched by the RTM event handler when a
// message_deleted event arrives. App.Update handles it by removing the
// message from both panes and the cache.
type WSMessageDeletedMsg struct {
	ChannelID string
	TS        string
}

// UploadProgressMsg is dispatched out-of-band by the uploader as
// each file completes. App updates the status-bar toast.
type UploadProgressMsg struct {
	Done  int
	Total int
}

// UploadResultMsg carries the final result of an upload batch.
type UploadResultMsg struct {
	Err error
}

// ChannelJoinedMsg is sent after the user successfully joins a channel from
// the channel finder. The App responds by adding the channel to the sidebar
// (so it appears in the user's regular channel list), marking it as joined in
// the finder, and switching to it.
type ChannelJoinedMsg struct {
	ID   string
	Name string
}

// ChannelJoinFailedMsg is sent when the join API call fails.
type ChannelJoinFailedMsg struct {
	ID   string
	Name string
	Err  error
}

// previewLoadedMsg is dispatched after a preview thumb has been fetched.
// The receiver constructs (or, when isCycle is set, swaps the image of)
// an imgpkg.Preview and stores it on the App.
type previewLoadedMsg struct {
	Name         string
	FileID       string
	Img          image.Image
	Path         string
	SiblingCount int
	SiblingIndex int
	// isCycle distinguishes a cycle (h/l/arrow) from a fresh open. On
	// cycle we mutate the existing overlay rather than constructing a
	// new one so transient state (e.g. position within wrap-around)
	// stays coherent.
	isCycle bool
}

// previewErrorMsg is dispatched if the preview fetch fails. The error
// is logged; the overlay is not opened.
type previewErrorMsg struct {
	Err error
}

// previewSpinnerTickMsg drives the loading-state spinner animation.
// While the preview overlay is open and in its loading state, the
// Update arm advances the spinner frame and re-schedules another tick;
// when the image lands (or the overlay closes), the chain stops.
type previewSpinnerTickMsg struct{}

// editEmptyToastMsg is delivered when the user tries to submit an
// edit with empty text.
type editEmptyToastMsg struct{}

// EnterNewMessageMsg is dispatched when the user presses Ctrl+N in
// ModeNormal. The reducer (reduceNewMessagePicker) handles it by snapshotting
// the workspace user list, opening the newmessagepicker, and switching
// to ModeNewMessage.
type EnterNewMessageMsg struct{}

// NewMessageOpenedMsg carries the result of a successful
// ChannelService.OpenConversation call. RequestID identifies which
// submit this is the response to so the reducer can drop late
// arrivals from cancelled submits. AlreadyOpen=true means Slack
// returned an existing DM/MPIM; the reducer skips the
// minimal-channel-record insert in that case (Task 12).
//
// Distinct from the existing ConversationOpenedMsg in this file,
// which is dispatched by the WS event handler for Slack's
// mpim_open / im_created events (channel side-effect of someone
// being added to a conversation). The new-message flow has its own
// type because it carries the in-flight RequestID and we need
// per-message routing in reduceNewMessagePicker.
type NewMessageOpenedMsg struct {
	ChannelID   string
	AlreadyOpen bool
	UserIDs     []string // copied through so the reducer can hydrate the cache record
	RequestID   uint64
}

// NewMessageFailedMsg carries an error from a failed
// ChannelService.OpenConversation call. The reducer surfaces Err in
// the modal's footer banner; the modal stays open with the user's
// selection intact so they can retry.
type NewMessageFailedMsg struct {
	RequestID uint64
	Err       error
}

// channelSearchDebounceMsg is delivered after the channel finder's
// query stops changing for channelSearchDebounceDelay. Carries the
// query and the generation at scheduling time; if the App's
// pendingChannelSearchGen has moved on, the message is dropped, which
// is what collapses a typing burst into one request.
//
// The debounce is a contract with edge.ChannelsSearch, not a nicety:
// the captured client issues two channels/search requests for a
// four-second typing session, and a per-keystroke finder would emit a
// request pattern no human hand produces.
type channelSearchDebounceMsg struct {
	query string
	gen   uint64
}
