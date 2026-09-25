// Adapters, constructors, and closure-bundle types backing the ports
// declared in ports.go. Implementations are wired by cmd/slk.
//
// Each port can be built from plain closures with its NewXxxService
// constructor; nil closures generally make methods no-op returning the
// zero value, which is how tests fake a single method. Exceptions such as
// MessageService.Forward return an unsupported error.
//
// Constructor shape:
//   - Services with ≤4 methods take positional func args
//     (NewReactionService(add, remove, loadFrecent, recordFrecent)).
//   - Services with ≥5 methods take a struct of named funcs
//     (NewThreadService(ThreadServiceFuncs{Fetch: fn, Mark: fn, ...})).
package core

import (
	"context"
	"errors"
	"io/fs"

	"github.com/gammons/slk/internal/ids"
)

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

func (r reactionAdapter) LoadFrecent(limit int) []EmojiEntry {
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
	ThreadLastRead      func(channelID ids.ChannelID, threadTS ids.ThreadTS) string
}

// NewThreadService builds a ThreadService from a ThreadServiceFuncs
// bundle. Used by both cmd/slk/main.go (production wiring) and tests
// (fake closures).
func NewThreadService(fns ThreadServiceFuncs) ThreadService {
	return threadAdapter{fns: fns}
}

type threadAdapter struct {
	fns ThreadServiceFuncs
}

func (t threadAdapter) Fetch(channelID ids.ChannelID, threadTS ids.ThreadTS) Msg {
	if t.fns.Fetch == nil {
		return nil
	}
	return t.fns.Fetch(channelID, threadTS)
}

func (t threadAdapter) CacheRead(channelID ids.ChannelID, threadTS ids.ThreadTS) []MessageItem {
	if t.fns.CacheRead == nil {
		return nil
	}
	return t.fns.CacheRead(channelID, threadTS)
}

func (t threadAdapter) Mark(channelID ids.ChannelID, threadTS ids.ThreadTS, ts ids.MessageTS) Cmd {
	if t.fns.Mark == nil {
		return nil
	}
	return t.fns.Mark(channelID, threadTS, ts)
}

func (t threadAdapter) SendReply(channelID ids.ChannelID, threadTS ids.ThreadTS, text string, broadcast bool) Msg {
	if t.fns.SendReply == nil {
		return nil
	}
	return t.fns.SendReply(channelID, threadTS, text, broadcast)
}

func (t threadAdapter) ListFetch(teamID ids.TeamID) Msg {
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

func (t threadAdapter) ThreadLastRead(channelID ids.ChannelID, threadTS ids.ThreadTS) string {
	if t.fns.ThreadLastRead == nil {
		return ""
	}
	return t.fns.ThreadLastRead(channelID, threadTS)
}

// NewActivityService builds an ActivityService from closures. Either
// may be nil; the resulting service no-ops that operation (returns a
// nil Msg).
func NewActivityService(fetch ActivityFetchFunc, hydrate ActivityHydrateFunc) ActivityService {
	return activityAdapter{fetch: fetch, hydrate: hydrate}
}

type activityAdapter struct {
	fetch   ActivityFetchFunc
	hydrate ActivityHydrateFunc
}

func (a activityAdapter) Fetch(teamID ids.TeamID, limit int, unreadOnly bool) Msg {
	if a.fetch == nil {
		return nil
	}
	return a.fetch(teamID, limit, unreadOnly)
}

func (a activityAdapter) Hydrate(teamID ids.TeamID, refs map[string][]string) Msg {
	if a.hydrate == nil {
		return nil
	}
	return a.hydrate(teamID, refs)
}

// MessageServiceFuncs is the closure bundle accepted by
// NewMessageService. Nil Forward returns an unsupported error; other nil
// fields make the resulting service no-op that operation.
type MessageServiceFuncs struct {
	Send       MessageSendFunc
	Forward    MessageForwardFunc
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

type messageAdapter struct {
	fns MessageServiceFuncs
}

func (m messageAdapter) Send(channelID ids.ChannelID, text string) Msg {
	if m.fns.Send == nil {
		return nil
	}
	return m.fns.Send(channelID, text)
}

func (m messageAdapter) Forward(ctx context.Context, teamID string, sourceChannelID ids.ChannelID, ts ids.MessageTS, destinationChannelID ids.ChannelID) (ForwardResult, error) {
	if m.fns.Forward == nil {
		return ForwardResult{}, errors.New("message forwarding is unsupported")
	}
	return m.fns.Forward(ctx, teamID, sourceChannelID, ts, destinationChannelID)
}

func (m messageAdapter) Edit(channelID ids.ChannelID, ts ids.MessageTS, newText string) Msg {
	if m.fns.Edit == nil {
		return nil
	}
	return m.fns.Edit(channelID, ts, newText)
}

func (m messageAdapter) Delete(channelID ids.ChannelID, ts ids.MessageTS) Msg {
	if m.fns.Delete == nil {
		return nil
	}
	return m.fns.Delete(channelID, ts)
}

func (m messageAdapter) MarkUnread(channelID ids.ChannelID, threadTS ids.ThreadTS, boundaryTS ids.MessageTS, unreadCount int) Msg {
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

// ChannelServiceFuncs is the closure bundle accepted by
// NewChannelService. Any field may be nil; the resulting service
// no-ops that operation.
type ChannelServiceFuncs struct {
	Fetch            ChannelFetchFunc
	FetchOlder       OlderMessagesFetchFunc
	FetchAround      func(channelID ids.ChannelID, ts ids.MessageTS) Msg
	ReadCache        ChannelCacheReadFunc
	SyncedAt         func(channelID ids.ChannelID) int64
	MarkRead         func(channelID ids.ChannelID, ts ids.MessageTS) Msg
	Lookup           ChannelLookupFunc
	Join             JoinChannelFunc
	RecordVisit      ChannelVisitRecorder
	MembershipFetch  func(channelID ids.ChannelID)
	OpenConversation func(userIDs []string, requestID uint64) Cmd
	SearchRemote     func(query string) []ChannelFinderItem
}

// NewChannelService builds a ChannelService from a
// ChannelServiceFuncs bundle.
func NewChannelService(fns ChannelServiceFuncs) ChannelService {
	return channelAdapter{fns: fns}
}

type channelAdapter struct {
	fns ChannelServiceFuncs
}

func (c channelAdapter) Fetch(channelID ids.ChannelID, channelName string) Msg {
	if c.fns.Fetch == nil {
		return nil
	}
	return c.fns.Fetch(channelID, channelName)
}

func (c channelAdapter) FetchOlder(channelID ids.ChannelID, oldestTS ids.MessageTS) Msg {
	if c.fns.FetchOlder == nil {
		return nil
	}
	return c.fns.FetchOlder(channelID, oldestTS)
}

func (c channelAdapter) FetchAround(channelID ids.ChannelID, ts ids.MessageTS) Msg {
	if c.fns.FetchAround == nil {
		return nil
	}
	return c.fns.FetchAround(channelID, ts)
}

func (c channelAdapter) ReadCache(channelID ids.ChannelID) []MessageItem {
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

func (c channelAdapter) MarkRead(channelID ids.ChannelID, ts ids.MessageTS) Msg {
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

func (c channelAdapter) Join(channelID ids.ChannelID, channelName string) Msg {
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

func (c channelAdapter) SearchRemote(query string) []ChannelFinderItem {
	if c.fns.SearchRemote == nil {
		return nil
	}
	return c.fns.SearchRemote(query)
}

func (c channelAdapter) OpenConversation(userIDs []string, requestID uint64) Cmd {
	if c.fns.OpenConversation == nil {
		return nil
	}
	return c.fns.OpenConversation(userIDs, requestID)
}

// SearchServiceFuncs is the closure bundle accepted by
// NewSearchService. Any field may be nil; that operation no-ops.
type SearchServiceFuncs struct {
	SearchChannel   func(channelID ids.ChannelID, query string) Msg
	SearchWorkspace func(query string) Msg
}

// NewSearchService builds a SearchService from a SearchServiceFuncs
// bundle. Used by cmd/slk/main.go (production wiring) and tests.
func NewSearchService(fns SearchServiceFuncs) SearchService { return searchAdapter{fns: fns} }

type searchAdapter struct{ fns SearchServiceFuncs }

func (s searchAdapter) SearchChannel(channelID ids.ChannelID, query string) Msg {
	if s.fns.SearchChannel == nil {
		return nil
	}
	return s.fns.SearchChannel(channelID, query)
}

func (s searchAdapter) SearchWorkspace(query string) Msg {
	if s.fns.SearchWorkspace == nil {
		return nil
	}
	return s.fns.SearchWorkspace(query)
}

// NewFileService builds a FileService from closures.
func NewFileService(
	upload func(channelID, threadTS, caption string, attachments []PendingAttachment) Cmd,
	download func(ctx context.Context, url, name string) (string, error),
) FileService {
	return fileAdapter{upload: upload, download: download}
}

type fileAdapter struct {
	upload   func(channelID, threadTS, caption string, attachments []PendingAttachment) Cmd
	download func(ctx context.Context, url, name string) (string, error)
}

func (f fileAdapter) Upload(channelID, threadTS, caption string, attachments []PendingAttachment) Cmd {
	if f.upload == nil {
		return nil
	}
	return f.upload(channelID, threadTS, caption, attachments)
}

func (f fileAdapter) Download(ctx context.Context, url, name string) (string, error) {
	if f.download == nil {
		return "", nil
	}
	return f.download(ctx, url, name)
}

// DesktopServiceFuncs is the closure bundle accepted by
// NewDesktopService. Any field may be nil; that operation no-ops, and a
// nil Stat reports every path as missing.
type DesktopServiceFuncs struct {
	Open          func(target string) error
	ReadClipboard func(f ClipboardFormat) []byte
	Stat          func(path string) (fs.FileInfo, error)
	SaveThread    func(parent MessageItem, replies []MessageItem, userNames, channelNames map[string]string, channelName string) (string, error)
	ReportStatus  func(unread, otherUnread int, workspace, title string)
}

// NewDesktopService builds a DesktopService from a DesktopServiceFuncs bundle.
func NewDesktopService(fns DesktopServiceFuncs) DesktopService {
	return desktopAdapter{fns: fns}
}

type desktopAdapter struct{ fns DesktopServiceFuncs }

func (d desktopAdapter) Open(target string) error {
	if d.fns.Open == nil {
		return nil
	}
	return d.fns.Open(target)
}

func (d desktopAdapter) ReadClipboard(f ClipboardFormat) []byte {
	if d.fns.ReadClipboard == nil {
		return nil
	}
	return d.fns.ReadClipboard(f)
}

func (d desktopAdapter) Stat(path string) (fs.FileInfo, error) {
	// Not (nil, nil): callers read info whenever err is nil.
	if d.fns.Stat == nil {
		return nil, fs.ErrNotExist
	}
	return d.fns.Stat(path)
}

func (d desktopAdapter) SaveThread(parent MessageItem, replies []MessageItem, userNames, channelNames map[string]string, channelName string) (string, error) {
	if d.fns.SaveThread == nil {
		return "", nil
	}
	return d.fns.SaveThread(parent, replies, userNames, channelNames, channelName)
}

func (d desktopAdapter) ReportStatus(unread, otherUnread int, workspace, title string) {
	if d.fns.ReportStatus == nil {
		return
	}
	d.fns.ReportStatus(unread, otherUnread, workspace, title)
}

// NewEditorService builds an EditorService from closures. A nil
// writeDraft or takeDraft fails with errors.ErrUnsupported.
func NewEditorService(
	writeDraft func(text string) (string, error),
	edit func(argv []string, path string, done func(err error) Msg) Cmd,
	takeDraft func(path string) (string, error),
) EditorService {
	return editorAdapter{writeDraft: writeDraft, edit: edit, takeDraft: takeDraft}
}

type editorAdapter struct {
	writeDraft func(text string) (string, error)
	edit       func(argv []string, path string, done func(err error) Msg) Cmd
	takeDraft  func(path string) (string, error)
}

func (e editorAdapter) WriteDraft(text string) (string, error) {
	if e.writeDraft == nil {
		return "", errors.ErrUnsupported
	}
	return e.writeDraft(text)
}

func (e editorAdapter) Edit(argv []string, path string, done func(err error) Msg) Cmd {
	if e.edit == nil {
		return nil
	}
	return e.edit(argv, path, done)
}

func (e editorAdapter) TakeDraft(path string) (string, error) {
	if e.takeDraft == nil {
		return "", errors.ErrUnsupported
	}
	return e.takeDraft(path)
}

// NewPresenceService builds a PresenceService from closures.
func NewPresenceService(
	setStatus func(action PresenceAction, snoozeMinutes int),
	sendTyping func(channelID string),
) PresenceService {
	return presenceAdapter{setStatus: setStatus, sendTyping: sendTyping}
}

type presenceAdapter struct {
	setStatus  func(action PresenceAction, snoozeMinutes int)
	sendTyping func(channelID string)
}

func (p presenceAdapter) SetStatus(action PresenceAction, snoozeMinutes int) {
	if p.setStatus != nil {
		p.setStatus(action, snoozeMinutes)
	}
}

func (p presenceAdapter) SendTyping(channelID string) {
	if p.sendTyping != nil {
		p.sendTyping(channelID)
	}
}

// NewSettingsService builds a SettingsService from closures.
func NewSettingsService(
	saveTheme func(name string, scope ThemeScope),
	saveSidebarWidth func(width int),
) SettingsService {
	return settingsAdapter{saveTheme: saveTheme, saveSidebarWidth: saveSidebarWidth}
}

type settingsAdapter struct {
	saveTheme        func(name string, scope ThemeScope)
	saveSidebarWidth func(width int)
}

func (s settingsAdapter) SaveTheme(name string, scope ThemeScope) {
	if s.saveTheme != nil {
		s.saveTheme(name, scope)
	}
}

func (s settingsAdapter) SaveSidebarWidth(width int) {
	if s.saveSidebarWidth != nil {
		s.saveSidebarWidth(width)
	}
}

// NewUnreadService builds an UnreadService from closures.
func NewUnreadService(
	channelReadStates func() map[string]ReadState,
	unreadWorkspaces func() []string,
) UnreadService {
	return unreadAdapter{channelReadStates: channelReadStates, unreadWorkspaces: unreadWorkspaces}
}

type unreadAdapter struct {
	channelReadStates func() map[string]ReadState
	unreadWorkspaces  func() []string
}

func (u unreadAdapter) ChannelReadStates() map[string]ReadState {
	if u.channelReadStates == nil {
		return nil
	}
	return u.channelReadStates()
}

func (u unreadAdapter) UnreadWorkspaces() []string {
	if u.unreadWorkspaces == nil {
		return nil
	}
	return u.unreadWorkspaces()
}

// NewWorkspaceService builds a WorkspaceService from a closure.
func NewWorkspaceService(switchTo func(teamID string) Msg) WorkspaceService {
	return workspaceAdapter{switchTo: switchTo}
}

type workspaceAdapter struct{ switchTo func(teamID string) Msg }

func (w workspaceAdapter) Switch(teamID string) Msg {
	if w.switchTo == nil {
		return nil
	}
	return w.switchTo(teamID)
}

// NewAvatarService builds an AvatarService from a closure.
func NewAvatarService(avatar func(userID string) string) AvatarService {
	return avatarAdapter{avatar: avatar}
}

type avatarAdapter struct{ avatar func(userID string) string }

func (a avatarAdapter) Avatar(userID string) string {
	if a.avatar == nil {
		return ""
	}
	return a.avatar(userID)
}

// Closure types accepted by the service constructors.

// ChannelFetchFunc is called when the user selects a channel.
type ChannelFetchFunc func(channelID ids.ChannelID, channelName string) Msg

// ChannelCacheReadFunc is called synchronously when the user selects a
// channel; it returns cached messages from local storage. Returning a
// non-empty slice causes the messagepane to render immediately without
// the loading spinner. Returning nil falls through to the network
// fetcher.
type ChannelCacheReadFunc func(channelID ids.ChannelID) []MessageItem

// OlderMessagesFetchFunc is called when the user scrolls to the top of a channel.
type OlderMessagesFetchFunc func(channelID ids.ChannelID, oldestTS ids.MessageTS) Msg

// MessageSendFunc is called when the user sends a message. Returns a Msg with the result.
type MessageSendFunc func(channelID ids.ChannelID, text string) Msg

// MessageForwardFunc forwards a message within the captured workspace,
// without producing normal send/compose events.
type MessageForwardFunc func(ctx context.Context, teamID string, sourceChannelID ids.ChannelID, ts ids.MessageTS, destinationChannelID ids.ChannelID) (ForwardResult, error)

// MessageEditFunc performs the chat.update API call. Returns a Msg
// (typically MessageEditedMsg) describing the result.
type MessageEditFunc func(channelID ids.ChannelID, ts ids.MessageTS, newText string) Msg

// MessageDeleteFunc performs the chat.delete API call. Returns a Msg
// (typically MessageDeletedMsg) describing the result.
type MessageDeleteFunc func(channelID ids.ChannelID, ts ids.MessageTS) Msg

// MarkUnreadFunc performs the conversations.mark or
// subscriptions.thread.mark HTTP call (with the rolled-back ts /
// read=0 form), updates SQLite + in-memory caches if the call
// succeeded, and returns a Msg (typically MessageMarkedUnreadMsg)
// describing the result. ThreadTS == "" means channel-level.
type MarkUnreadFunc func(channelID ids.ChannelID, threadTS ids.ThreadTS, boundaryTS ids.MessageTS, unreadCount int) Msg

// ThreadFetchFunc is called when the user opens a thread.
type ThreadFetchFunc func(channelID ids.ChannelID, threadTS ids.ThreadTS) Msg

// ThreadCacheReadFunc is called synchronously when a thread is opened;
// returns cached replies (or nil) so the thread panel can populate
// without waiting for the network. Returning a non-empty slice causes
// the thread panel to render immediately; the subsequent network
// response overwrites with authoritative data.
type ThreadCacheReadFunc func(channelID ids.ChannelID, threadTS ids.ThreadTS) []MessageItem

// ThreadMarkFunc is called to mark a thread as read on Slack's servers
// (subscriptions.thread.mark) and, on success, to advance the local
// thread_subscriptions cursor. Returns a Cmd yielding
// ThreadMarkedLocalMsg, or nil when no workspace is active.
type ThreadMarkFunc func(channelID ids.ChannelID, threadTS ids.ThreadTS, ts ids.MessageTS) Cmd

// ThreadReplySendFunc is called when the user sends a thread reply.
// broadcast is Slack's "Also send to #channel" (reply_broadcast=true).
type ThreadReplySendFunc func(channelID ids.ChannelID, threadTS ids.ThreadTS, text string, broadcast bool) Msg

// ThreadsListFetchFunc loads the involved-threads list for a workspace.
// Returns the resulting Msg (typically ThreadsListLoadedMsg).
type ThreadsListFetchFunc func(teamID ids.TeamID) Msg

// ActivityFetchFunc loads the first page of the Activity feed for a
// workspace. Returns the resulting Msg (typically
// ActivityListLoadedMsg).
type ActivityFetchFunc func(teamID ids.TeamID, limit int, unreadOnly bool) Msg

// ActivityHydrateFunc fetches message bodies for a page's refs.
// Returns the resulting Msg (typically ActivityBodiesLoadedMsg).
type ActivityHydrateFunc func(teamID ids.TeamID, refs map[string][]string) Msg

type ReactionAddFunc func(channelID ids.ChannelID, messageTS ids.MessageTS, emoji string) error
type ReactionRemoveFunc func(channelID ids.ChannelID, messageTS ids.MessageTS, emoji string) error

// PermalinkFetchFunc is called to fetch the Slack permalink for a message.
// For thread replies, pass the reply's ts; Slack returns a thread-aware URL.
type PermalinkFetchFunc func(ctx context.Context, channelID ids.ChannelID, ts ids.MessageTS) (string, error)
type FrecentLoadFunc func(limit int) []EmojiEntry
type FrecentRecordFunc func(emoji string)

// JoinChannelFunc is called to join a public channel by ID. Returns a Msg
// describing the result (typically ChannelJoinedMsg or ChannelJoinFailedMsg).
type JoinChannelFunc func(channelID ids.ChannelID, channelName string) Msg

// ChannelVisitRecorder is invoked from case ChannelSelectedMsg to let
// main.go persist the visit (SQLite write + in-memory map update on
// the WorkspaceContext). Always called regardless of FromHistory.
type ChannelVisitRecorder func(channelID ids.ChannelID)

// ChannelLookupFunc returns metadata for a channel that the App has
// in its navigation history. Used by navigateBack / navigateForward
// to skip stale entries (channels the user has left, archived, or
// kicked from). Returns ok=false when the channel is no longer
// available in the active workspace.
type ChannelLookupFunc func(channelID ids.ChannelID) (name, channelType string, ok bool)
