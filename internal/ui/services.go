// internal/ui/services.go
//
// Service interfaces that group cohesive subsets of the App's
// collaborator callbacks. Wired by cmd/slk/main.go.
//
// Phase 3 of the SOLID refactor of internal/ui/app.go: introduces
// service interfaces (DIP + ISP) to replace the flat collection of
// XxxFunc callback fields that previously hung off App. Each interface
// groups related callbacks under one collaborator; App holds a single
// pointer per service instead of N raw functions.
//
// Migration strategy: one service per commit, smallest first. Each
// commit converts a related subset of XxxFunc fields + Set* methods
// to a single ServiceXxx interface + Set method. The XxxFunc type
// aliases stay alive as constructor parameter types (documentation
// value) and adapter input types until all services have migrated.
//
// Constructor shape:
//   - Services with ≤4 methods take positional func args
//     (NewReactionService(add, remove, loadFrecent, recordFrecent)).
//   - Services with ≥5 methods take a struct of named funcs
//     (NewThreadService(ThreadServiceFuncs{Fetch: fn, Mark: fn, ...})).
//     Lets tests omit unused methods without trailing nils and lets
//     readers see what each closure is doing at the call site.
package ui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/reactionpicker"
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
	LoadFrecent(limit int) []reactionpicker.EmojiEntry

	// RecordFrecent records emoji as recently used so future
	// LoadFrecent calls surface it. Called after every successful
	// reaction add.
	RecordFrecent(emoji string)
}

// NewReactionService builds a ReactionService from individual
// function closures. Any function may be nil; the resulting service
// no-ops that operation and returns the zero value for read paths.
// Used by both cmd/slk/main.go (production wiring) and tests (fake
// closures).
func NewReactionService(
	add ReactionAddFunc,
	remove ReactionRemoveFunc,
	loadFrecent FrecentLoadFunc,
	recordFrecent FrecentRecordFunc,
) ReactionService {
	return reactionAdapter{
		add:           add,
		remove:        remove,
		loadFrecent:   loadFrecent,
		recordFrecent: recordFrecent,
	}
}

// noopReactionService is the default ReactionService wired into App
// by NewApp so call sites can dispatch without nil-checks even when
// no service has been registered (typically in tests that don't
// exercise reaction paths).
var noopReactionService ReactionService = reactionAdapter{}

type reactionAdapter struct {
	add           ReactionAddFunc
	remove        ReactionRemoveFunc
	loadFrecent   FrecentLoadFunc
	recordFrecent FrecentRecordFunc
}

func (r reactionAdapter) Add(channelID ids.ChannelID, messageTS ids.MessageTS, emoji string) error {
	if r.add == nil {
		return nil
	}
	return r.add(channelID, messageTS, emoji)
}

func (r reactionAdapter) Remove(channelID ids.ChannelID, messageTS ids.MessageTS, emoji string) error {
	if r.remove == nil {
		return nil
	}
	return r.remove(channelID, messageTS, emoji)
}

func (r reactionAdapter) LoadFrecent(limit int) []reactionpicker.EmojiEntry {
	if r.loadFrecent == nil {
		return nil
	}
	return r.loadFrecent(limit)
}

func (r reactionAdapter) RecordFrecent(emoji string) {
	if r.recordFrecent == nil {
		return
	}
	r.recordFrecent(emoji)
}

// ThreadService is the App's interface to Slack's thread surfaces:
// fetching replies, marking threads read, posting replies, and loading
// the involved-threads list for the user's threads view. Includes
// ChannelLastRead because the thread panel needs the parent channel's
// last_read_ts to render its unread boundary — that's a thread-display
// concern even though the data is channel-scoped.
//
// Implementations are wired by cmd/slk/main.go. Build one via
// NewThreadService from a ThreadServiceFuncs struct so unused
// methods can be left nil without trailing positional nils.
type ThreadService interface {
	// Fetch retrieves replies for threadTS in channelID from Slack.
	// Returns a tea.Msg (typically ThreadRepliesLoadedMsg).
	Fetch(channelID ids.ChannelID, threadTS ids.ThreadTS) tea.Msg

	// CacheRead returns cached replies (or nil) so the thread panel
	// can populate without waiting for the network. A non-empty
	// return causes immediate render; the subsequent Fetch result
	// overwrites with authoritative data.
	CacheRead(channelID ids.ChannelID, threadTS ids.ThreadTS) []messages.MessageItem

	// Mark marks the thread as read on Slack's servers
	// (subscriptions.thread.mark). channelID is the parent channel,
	// threadTS is the parent message ts, ts is the latest reply ts
	// the user has now seen. Best-effort and non-blocking.
	Mark(channelID ids.ChannelID, threadTS ids.ThreadTS, ts ids.MessageTS)

	// SendReply posts a reply to threadTS in channelID. When broadcast
	// is true the reply is also posted to the parent channel feed
	// (reply_broadcast=true). Returns a tea.Msg (typically
	// ThreadReplySentMsg or ThreadReplySendFailedMsg).
	SendReply(channelID ids.ChannelID, threadTS ids.ThreadTS, text string, broadcast bool) tea.Msg

	// ListFetch loads the involved-threads list for the workspace
	// (Slack subscriptions.list). Returns a tea.Msg (typically
	// ThreadsListLoadedMsg).
	ListFetch(teamID ids.TeamID) tea.Msg

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

	// ChannelLastRead returns the parent channel's last_read_ts so
	// the thread panel can render a "── new ──" boundary. Optional;
	// returning "" disables the unread boundary in the thread panel.
	ChannelLastRead(channelID ids.ChannelID) string
}

// ThreadServiceFuncs is the closure bundle accepted by
// NewThreadService. Any field may be nil; the resulting service
// no-ops that operation (and returns the zero value for read paths).
type ThreadServiceFuncs struct {
	Fetch               ThreadFetchFunc
	CacheRead           ThreadCacheReadFunc
	Mark                ThreadMarkFunc
	SendReply           ThreadReplySendFunc
	ListFetch           ThreadsListFetchFunc
	EnsureSubscriptions func(teamID ids.TeamID)
	ChannelLastRead     func(channelID ids.ChannelID) string
}

// NewThreadService builds a ThreadService from a ThreadServiceFuncs
// bundle. Used by both cmd/slk/main.go (production wiring) and tests
// (fake closures).
func NewThreadService(fns ThreadServiceFuncs) ThreadService {
	return threadAdapter{fns: fns}
}

// noopThreadService is the default ThreadService wired into App by
// NewApp so call sites can dispatch without nil-checks even when
// SetThreadService hasn't been called.
var noopThreadService ThreadService = threadAdapter{}

type threadAdapter struct {
	fns ThreadServiceFuncs
}

func (t threadAdapter) Fetch(channelID ids.ChannelID, threadTS ids.ThreadTS) tea.Msg {
	if t.fns.Fetch == nil {
		return nil
	}
	return t.fns.Fetch(channelID, threadTS)
}

func (t threadAdapter) CacheRead(channelID ids.ChannelID, threadTS ids.ThreadTS) []messages.MessageItem {
	if t.fns.CacheRead == nil {
		return nil
	}
	return t.fns.CacheRead(channelID, threadTS)
}

func (t threadAdapter) Mark(channelID ids.ChannelID, threadTS ids.ThreadTS, ts ids.MessageTS) {
	if t.fns.Mark == nil {
		return
	}
	t.fns.Mark(channelID, threadTS, ts)
}

func (t threadAdapter) SendReply(channelID ids.ChannelID, threadTS ids.ThreadTS, text string, broadcast bool) tea.Msg {
	if t.fns.SendReply == nil {
		return nil
	}
	return t.fns.SendReply(channelID, threadTS, text, broadcast)
}

func (t threadAdapter) ListFetch(teamID ids.TeamID) tea.Msg {
	if t.fns.ListFetch == nil {
		return nil
	}
	return t.fns.ListFetch(teamID)
}

func (t threadAdapter) EnsureSubscriptions(teamID ids.TeamID) {
	if t.fns.EnsureSubscriptions == nil {
		return
	}
	t.fns.EnsureSubscriptions(teamID)
}

func (t threadAdapter) ChannelLastRead(channelID ids.ChannelID) string {
	if t.fns.ChannelLastRead == nil {
		return ""
	}
	return t.fns.ChannelLastRead(channelID)
}

// MessageService is the App's interface to Slack's per-message
// operations: send, edit, delete, mark-unread, and permalink lookup.
// Implementations are wired by cmd/slk/main.go.
//
// All methods are best-effort and nil-safe at the adapter level: an
// implementation built via NewMessageService with a nil component
// silently no-ops that operation (returning nil tea.Msg or
// ("", nil) for Permalink).
type MessageService interface {
	// Send dispatches chat.postMessage for channelID with text.
	// Returns a tea.Msg (typically MessageSentMsg or
	// MessageSendFailedMsg).
	Send(channelID ids.ChannelID, text string) tea.Msg

	// Edit dispatches chat.update for the message identified by
	// (channelID, ts), replacing its text with newText.
	// Returns a tea.Msg (typically MessageEditedMsg).
	Edit(channelID ids.ChannelID, ts ids.MessageTS, newText string) tea.Msg

	// Delete dispatches chat.delete for the message identified by
	// (channelID, ts). Returns a tea.Msg (typically MessageDeletedMsg).
	Delete(channelID ids.ChannelID, ts ids.MessageTS) tea.Msg

	// MarkUnread dispatches conversations.mark (channel-level) or
	// subscriptions.thread.mark (when threadTS != "") with the
	// rolled-back boundaryTS. unreadCount is forwarded to the result
	// for the sidebar's badge update. Returns a tea.Msg (typically
	// MessageMarkedUnreadMsg).
	MarkUnread(channelID ids.ChannelID, threadTS ids.ThreadTS, boundaryTS ids.MessageTS, unreadCount int) tea.Msg

	// Permalink resolves the Slack permalink URL for the message
	// identified by (channelID, ts). Used by the copy-permalink
	// keybind. Synchronous (HTTP); callers wrap in a goroutine to
	// avoid blocking the Update loop.
	Permalink(ctx context.Context, channelID ids.ChannelID, ts ids.MessageTS) (string, error)
}

// MessageServiceFuncs is the closure bundle accepted by
// NewMessageService. Any field may be nil; the resulting service
// no-ops that operation.
type MessageServiceFuncs struct {
	Send       MessageSendFunc
	Edit       MessageEditFunc
	Delete     MessageDeleteFunc
	MarkUnread MarkUnreadFunc
	Permalink  PermalinkFetchFunc
}

// NewMessageService builds a MessageService from a MessageServiceFuncs
// bundle. Used by cmd/slk/main.go (production wiring) and tests.
func NewMessageService(fns MessageServiceFuncs) MessageService {
	return messageAdapter{fns: fns}
}

// noopMessageService is the default MessageService wired into App by
// NewApp so call sites can dispatch without nil-checks even when
// SetMessageService hasn't been called.
var noopMessageService MessageService = messageAdapter{}

type messageAdapter struct {
	fns MessageServiceFuncs
}

func (m messageAdapter) Send(channelID ids.ChannelID, text string) tea.Msg {
	if m.fns.Send == nil {
		return nil
	}
	return m.fns.Send(channelID, text)
}

func (m messageAdapter) Edit(channelID ids.ChannelID, ts ids.MessageTS, newText string) tea.Msg {
	if m.fns.Edit == nil {
		return nil
	}
	return m.fns.Edit(channelID, ts, newText)
}

func (m messageAdapter) Delete(channelID ids.ChannelID, ts ids.MessageTS) tea.Msg {
	if m.fns.Delete == nil {
		return nil
	}
	return m.fns.Delete(channelID, ts)
}

func (m messageAdapter) MarkUnread(channelID ids.ChannelID, threadTS ids.ThreadTS, boundaryTS ids.MessageTS, unreadCount int) tea.Msg {
	if m.fns.MarkUnread == nil {
		return nil
	}
	return m.fns.MarkUnread(channelID, threadTS, boundaryTS, unreadCount)
}

func (m messageAdapter) Permalink(ctx context.Context, channelID ids.ChannelID, ts ids.MessageTS) (string, error) {
	if m.fns.Permalink == nil {
		return "", nil
	}
	return m.fns.Permalink(ctx, channelID, ts)
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
	// channelName is for log context. Returns a tea.Msg (typically
	// MessagesLoadedMsg).
	Fetch(channelID ids.ChannelID, channelName string) tea.Msg

	// FetchOlder loads messages older than oldestTS for the
	// channel-history backfill triggered by scroll-past-top.
	// Returns a tea.Msg (typically OlderMessagesLoadedMsg).
	FetchOlder(channelID ids.ChannelID, oldestTS ids.MessageTS) tea.Msg

	// FetchAround loads a history window centered on ts for
	// jump-to-message navigation. Returns a tea.Msg (typically
	// MessagesAroundLoadedMsg).
	FetchAround(channelID ids.ChannelID, ts ids.MessageTS) tea.Msg

	// ReadCache returns the local-cache snapshot of channelID's
	// recent messages, or nil if no cache exists. Used by
	// ChannelSelectedMsg's tiered render policy.
	ReadCache(channelID ids.ChannelID) []messages.MessageItem

	// SyncedAt returns the unix-seconds timestamp of the channel's
	// last authoritative cache-from-network sync, or 0 if never
	// synced. Used by ChannelSelectedMsg's tiered render policy to
	// decide between cache-only, cache-and-verify, and spinner-only
	// render.
	SyncedAt(channelID ids.ChannelID) int64

	// MarkRead dispatches conversations.mark + UpdateChannelReadState
	// to bring the channel's last_read_ts up to ts. Used by Tier 1
	// of ChannelSelectedMsg when cache is provably fresh. Returns
	// a tea.Msg (typically ChannelMarkedReadMsg).
	MarkRead(channelID ids.ChannelID, ts ids.MessageTS) tea.Msg

	// Lookup returns metadata (name, channelType) for channelID, or
	// ok=false if the channel is no longer available in the active
	// workspace. Used by navHistoryStore.Walk to skip stale entries.
	Lookup(channelID ids.ChannelID) (name, channelType string, ok bool)

	// Join sends conversations.join for channelID. channelName is
	// for log context. Returns a tea.Msg (typically ChannelJoinedMsg
	// or ChannelJoinFailedMsg).
	Join(channelID ids.ChannelID, channelName string) tea.Msg

	// RecordVisit persists a visit to channelID (SQLite write +
	// WorkspaceContext last-visited map update). Fired once per
	// ChannelSelectedMsg regardless of FromHistory.
	RecordVisit(channelID ids.ChannelID)

	// MembershipFetch asks membership.Manager to ensure-fresh the
	// member set for channelID. Fire-and-forget; results arrive
	// asynchronously via ChannelMembershipMsg.
	MembershipFetch(channelID ids.ChannelID)

	// OpenConversation dispatches conversations.open for userIDs (1
	// recipient = IM, 2-8 recipients = MPIM). Returns a tea.Cmd whose
	// resolved tea.Msg is NewMessageOpenedMsg on success or
	// NewMessageFailedMsg on error; both carry requestID so the
	// reducer can drop late results from cancelled submits.
	OpenConversation(userIDs []string, requestID uint64) tea.Cmd

	// SearchRemote asks the server which channels match query,
	// including ones the user has not joined, and blocks until it
	// answers. Callers run it from a tea.Cmd, debounced — see
	// App.scheduleChannelSearch.
	//
	// It replaced a background conversations.list walk that ran at
	// boot on every workspace, whether or not the finder was ever
	// opened. Returning nil (no client, or a failed request) leaves
	// the finder showing local matches only, which is what it showed
	// before this existed.
	SearchRemote(query string) []channelfinder.Item
}

// ChannelServiceFuncs is the closure bundle accepted by
// NewChannelService. Any field may be nil; the resulting service
// no-ops that operation.
type ChannelServiceFuncs struct {
	Fetch            ChannelFetchFunc
	FetchOlder       OlderMessagesFetchFunc
	FetchAround      func(channelID ids.ChannelID, ts ids.MessageTS) tea.Msg
	ReadCache        ChannelCacheReadFunc
	SyncedAt         func(channelID ids.ChannelID) int64
	MarkRead         func(channelID ids.ChannelID, ts ids.MessageTS) tea.Msg
	Lookup           ChannelLookupFunc
	Join             JoinChannelFunc
	RecordVisit      ChannelVisitRecorder
	MembershipFetch  func(channelID ids.ChannelID)
	OpenConversation func(userIDs []string, requestID uint64) tea.Cmd
	SearchRemote     func(query string) []channelfinder.Item
}

// NewChannelService builds a ChannelService from a
// ChannelServiceFuncs bundle.
func NewChannelService(fns ChannelServiceFuncs) ChannelService {
	return channelAdapter{fns: fns}
}

// noopChannelService is the default ChannelService wired into App
// by NewApp so call sites can dispatch without nil-checks.
var noopChannelService ChannelService = channelAdapter{}

type channelAdapter struct {
	fns ChannelServiceFuncs
}

func (c channelAdapter) Fetch(channelID ids.ChannelID, channelName string) tea.Msg {
	if c.fns.Fetch == nil {
		return nil
	}
	return c.fns.Fetch(channelID, channelName)
}

func (c channelAdapter) FetchOlder(channelID ids.ChannelID, oldestTS ids.MessageTS) tea.Msg {
	if c.fns.FetchOlder == nil {
		return nil
	}
	return c.fns.FetchOlder(channelID, oldestTS)
}

func (c channelAdapter) FetchAround(channelID ids.ChannelID, ts ids.MessageTS) tea.Msg {
	if c.fns.FetchAround == nil {
		return nil
	}
	return c.fns.FetchAround(channelID, ts)
}

func (c channelAdapter) ReadCache(channelID ids.ChannelID) []messages.MessageItem {
	if c.fns.ReadCache == nil {
		return nil
	}
	return c.fns.ReadCache(channelID)
}

func (c channelAdapter) SyncedAt(channelID ids.ChannelID) int64 {
	if c.fns.SyncedAt == nil {
		return 0
	}
	return c.fns.SyncedAt(channelID)
}

func (c channelAdapter) MarkRead(channelID ids.ChannelID, ts ids.MessageTS) tea.Msg {
	if c.fns.MarkRead == nil {
		return nil
	}
	return c.fns.MarkRead(channelID, ts)
}

func (c channelAdapter) Lookup(channelID ids.ChannelID) (name, channelType string, ok bool) {
	if c.fns.Lookup == nil {
		return "", "", false
	}
	return c.fns.Lookup(channelID)
}

func (c channelAdapter) Join(channelID ids.ChannelID, channelName string) tea.Msg {
	if c.fns.Join == nil {
		return nil
	}
	return c.fns.Join(channelID, channelName)
}

func (c channelAdapter) RecordVisit(channelID ids.ChannelID) {
	if c.fns.RecordVisit == nil {
		return
	}
	c.fns.RecordVisit(channelID)
}

func (c channelAdapter) MembershipFetch(channelID ids.ChannelID) {
	if c.fns.MembershipFetch == nil {
		return
	}
	c.fns.MembershipFetch(channelID)
}

func (c channelAdapter) SearchRemote(query string) []channelfinder.Item {
	if c.fns.SearchRemote == nil {
		return nil
	}
	return c.fns.SearchRemote(query)
}

func (c channelAdapter) OpenConversation(userIDs []string, requestID uint64) tea.Cmd {
	if c.fns.OpenConversation == nil {
		return nil
	}
	return c.fns.OpenConversation(userIDs, requestID)
}

// SearchService runs message searches. SearchChannel queries the local
// FTS cache for one channel; SearchWorkspace queries Slack's
// search.messages for the active workspace.
type SearchService interface {
	// SearchChannel returns a ChannelSearchResultsMsg for query in
	// channelID's cached history.
	SearchChannel(channelID ids.ChannelID, query string) tea.Msg
	// SearchWorkspace returns a WorkspaceSearchResultsMsg for query
	// across the active workspace (server-side).
	SearchWorkspace(query string) tea.Msg
}

// SearchServiceFuncs is the closure bundle accepted by
// NewSearchService. Any field may be nil; that operation no-ops.
type SearchServiceFuncs struct {
	SearchChannel   func(channelID ids.ChannelID, query string) tea.Msg
	SearchWorkspace func(query string) tea.Msg
}

// NewSearchService builds a SearchService from a SearchServiceFuncs
// bundle. Used by cmd/slk/main.go (production wiring) and tests.
func NewSearchService(fns SearchServiceFuncs) SearchService { return searchAdapter{fns: fns} }

// noopSearchService is the default SearchService wired into App by
// NewApp so call sites can dispatch without nil-checks even when
// SetSearchService hasn't been called.
var noopSearchService SearchService = searchAdapter{}

type searchAdapter struct{ fns SearchServiceFuncs }

func (s searchAdapter) SearchChannel(channelID ids.ChannelID, query string) tea.Msg {
	if s.fns.SearchChannel == nil {
		return nil
	}
	return s.fns.SearchChannel(channelID, query)
}

func (s searchAdapter) SearchWorkspace(query string) tea.Msg {
	if s.fns.SearchWorkspace == nil {
		return nil
	}
	return s.fns.SearchWorkspace(query)
}
