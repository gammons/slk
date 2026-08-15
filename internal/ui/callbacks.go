// internal/ui/callbacks.go
//
// Callback function types used to inject collaborators into App from
// cmd/slk/main.go. Each Set* method on App takes one of these types
// and stores it; the App invokes them in response to user actions.
//
// Phase 1 of the SOLID refactor of internal/ui/app.go: this file
// collects every callback type that previously lived in app.go. The
// callbacks themselves are still flat function pointers — Phase 3
// will group cohesive subsets into service interfaces (ChannelService,
// MessageService, ThreadService, ReactionService, WorkspaceService).
//
// No semantic change in this commit: same package, same declarations.
package ui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"golang.design/x/clipboard"

	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/compose"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/reactionpicker"
)

// SwitchWorkspaceFunc is called to switch the active workspace.
type SwitchWorkspaceFunc func(teamID string) tea.Msg

// ChannelFetchFunc is called when the user selects a channel.
type ChannelFetchFunc func(channelID ids.ChannelID, channelName string) tea.Msg

// ChannelCacheReadFunc is called synchronously when the user selects a
// channel; it returns cached messages from local storage. Returning a
// non-empty slice causes the messagepane to render immediately without
// the loading spinner. Returning nil falls through to the network
// fetcher.
type ChannelCacheReadFunc func(channelID ids.ChannelID) []messages.MessageItem

// OlderMessagesFetchFunc is called when the user scrolls to the top of a channel.
type OlderMessagesFetchFunc func(channelID ids.ChannelID, oldestTS ids.MessageTS) tea.Msg

// MessageSendFunc is called when the user sends a message. Returns a tea.Msg with the result.
type MessageSendFunc func(channelID ids.ChannelID, text string) tea.Msg

// UploadFunc performs an upload of one or more files to a channel
// (with optional thread). It returns a tea.Cmd whose terminal
// message is UploadResultMsg; intermediate UploadProgressMsg events
// are dispatched out-of-band via program.Send.
type UploadFunc func(channelID, threadTS, caption string, attachments []compose.PendingAttachment) tea.Cmd

// MessageEditFunc performs the chat.update API call. Returns a tea.Msg
// (typically MessageEditedMsg) describing the result.
type MessageEditFunc func(channelID ids.ChannelID, ts ids.MessageTS, newText string) tea.Msg

// MessageDeleteFunc performs the chat.delete API call. Returns a tea.Msg
// (typically MessageDeletedMsg) describing the result.
type MessageDeleteFunc func(channelID ids.ChannelID, ts ids.MessageTS) tea.Msg

// MarkUnreadFunc performs the conversations.mark or
// subscriptions.thread.mark HTTP call (with the rolled-back ts /
// read=0 form), updates SQLite + in-memory caches if the call
// succeeded, and returns a tea.Msg (typically MessageMarkedUnreadMsg)
// describing the result. ThreadTS == "" means channel-level.
type MarkUnreadFunc func(channelID ids.ChannelID, threadTS ids.ThreadTS, boundaryTS ids.MessageTS, unreadCount int) tea.Msg

// ThreadFetchFunc is called when the user opens a thread.
type ThreadFetchFunc func(channelID ids.ChannelID, threadTS ids.ThreadTS) tea.Msg

// ThreadCacheReadFunc is called synchronously when a thread is opened;
// returns cached replies (or nil) so the thread panel can populate
// without waiting for the network. Returning a non-empty slice causes
// the thread panel to render immediately; the subsequent network
// response overwrites with authoritative data.
type ThreadCacheReadFunc func(channelID ids.ChannelID, threadTS ids.ThreadTS) []messages.MessageItem

// ThreadMarkFunc is called to mark a thread as read on Slack's servers
// (subscriptions.thread.mark). channelID is the parent channel, threadTS
// is the parent message ts, and ts is the latest reply ts the user has now
// seen. Implementations should be best-effort and non-blocking.
type ThreadMarkFunc func(channelID ids.ChannelID, threadTS ids.ThreadTS, ts ids.MessageTS)

// ThreadReplySendFunc is called when the user sends a thread reply.
type ThreadReplySendFunc func(channelID ids.ChannelID, threadTS ids.ThreadTS, text string) tea.Msg

// ThreadsListFetchFunc loads the involved-threads list for a workspace.
// Returns the resulting tea.Msg (typically ThreadsListLoadedMsg).
type ThreadsListFetchFunc func(teamID ids.TeamID) tea.Msg

// ActivityFetchFunc loads one page of the Activity feed for a
// workspace. Returns the resulting tea.Msg (typically
// ActivityListLoadedMsg).
type ActivityFetchFunc func(teamID ids.TeamID, limit int, cursor string, unreadOnly bool) tea.Msg

type ReactionAddFunc func(channelID ids.ChannelID, messageTS ids.MessageTS, emoji string) error
type ReactionRemoveFunc func(channelID ids.ChannelID, messageTS ids.MessageTS, emoji string) error

// PermalinkFetchFunc is called to fetch the Slack permalink for a message.
// For thread replies, pass the reply's ts; Slack returns a thread-aware URL.
type PermalinkFetchFunc func(ctx context.Context, channelID ids.ChannelID, ts ids.MessageTS) (string, error)
type FrecentLoadFunc func(limit int) []reactionpicker.EmojiEntry
type FrecentRecordFunc func(emoji string)

// TypingSendFunc is called to broadcast a typing indicator.
type TypingSendFunc func(channelID string)

// JoinChannelFunc is called to join a public channel by ID. Returns a tea.Msg
// describing the result (typically ChannelJoinedMsg or ChannelJoinFailedMsg).
type JoinChannelFunc func(channelID ids.ChannelID, channelName string) tea.Msg

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

// clipboardReader abstracts clipboard.Read so tests can inject fake
// clipboard contents. Production code uses the real clipboard.Read.
type clipboardReader func(format clipboard.Format) []byte

// defaultClipboardReader is the real clipboard read function. It's
// overridable per-App via SetClipboardReader for tests.
var defaultClipboardReader clipboardReader = clipboard.Read

// clipboardWriter creates a Bubble Tea command that writes text through the
// terminal. Production uses tea.SetClipboard, which emits OSC 52 without
// invoking the native clipboard library or requiring CGO.
type clipboardWriter func(text string) tea.Cmd

// defaultClipboardWriter is overridable per-App for tests.
var defaultClipboardWriter clipboardWriter = tea.SetClipboard

// StatusReportFunc mirrors slk's unread state onto an external surface. It is
// called by notifyReadStateChanged on every read-state change with the
// active-workspace unread count, the other-workspace unread count, the active
// workspace name, and the window-title string. See notifications.status_command.
type StatusReportFunc func(unread, otherUnread int, workspace, title string)
