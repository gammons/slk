// internal/ui/reducer_new_message.go
//
// Reducer for the new-message picker lifecycle:
//
//	EnterNewMessageMsg     - user pressed Ctrl+N: seed the picker
//	                         with current workspace users, open it,
//	                         enter ModeNewMessage.
//	NewMessageOpenedMsg    - conversations.open succeeded: validate
//	                         RequestID against the in-flight counter
//	                         and the cancelled flag, then close the
//	                         modal, switch to the opened channel,
//	                         and enter ModeInsert (so the cursor
//	                         lands in compose ready to type).
//	NewMessageFailedMsg    - conversations.open failed: log and
//	                         keep the modal open so the user can
//	                         retry or cancel.
//
// Cache hydration for AlreadyOpen=false is implemented in Task 12;
// this task emits ChannelSelectedMsg directly without inserting a
// channel record. Task 12 adds that insert.
//
// Named reduceNewMessagePicker to avoid colliding with
// reduceNewMessage in reducer_send.go (which handles the WS
// NewMessageMsg event family — unrelated to the new-DM picker).
package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/ui/sidebar"
)

var reduceNewMessagePicker reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case EnterNewMessageMsg:
		a.seedNewMessagePicker()
		a.newMessagePicker.Open()
		a.SetMode(ModeNewMessage)
		return nil, true

	case NewMessageOpenedMsg:
		if !newMessageResultIsCurrent(a, m.RequestID) {
			debuglog.General("new-message: dropping stale/cancelled NewMessageOpenedMsg req=%d inflight=%d cancelled=%v", m.RequestID, a.newMessageInFlightID, a.newMessageCancelled)
			return nil, true
		}
		a.newMessagePicker.Close()
		a.newMessageInFlightID = 0
		a.newMessageCancelled = false
		a.SetMode(ModeInsert)

		channelType := "dm"
		if len(m.UserIDs) > 1 {
			channelType = "group_dm"
		}
		if !m.AlreadyOpen {
			a.hydrateNewConversation(m.ChannelID, channelType, m.UserIDs)
		}

		channelID := m.ChannelID
		emitSelected := func() tea.Msg {
			return ChannelSelectedMsg{ID: channelID, Type: channelType}
		}
		return tea.Batch(emitSelected, a.compose.Focus()), true

	case NewMessageFailedMsg:
		if !newMessageResultIsCurrent(a, m.RequestID) {
			return nil, true
		}
		debuglog.General("new-message: OpenConversation failed: %v", m.Err)
		// Stay in ModeNewMessage; modal stays visible; clear the
		// in-flight so a follow-up submit gets a fresh ID. Surface
		// the error via a toast (the existing app-wide notification
		// channel) so the user knows the submit didn't go through.
		a.newMessageInFlightID = 0
		errText := m.Err.Error()
		return func() tea.Msg { return ToastMsg{Text: "Open DM failed: " + errText} }, true
	}
	return nil, false
}

// hydrateNewConversation inserts a minimal sidebar.ChannelItem for a
// freshly-opened DM or MPIM so the channel-switch path has a record
// to render against. The full channel metadata is filled in by the
// existing RTM event handlers (mpim_open / im_created) and the
// membership.Manager.
func (a *App) hydrateNewConversation(channelID, channelType string, userIDs []string) {
	item := sidebar.ChannelItem{
		ID:   channelID,
		Type: channelType,
		Name: a.deriveConversationName(channelType, userIDs),
	}
	if channelType == "dm" && len(userIDs) == 1 {
		item.DMUserID = userIDs[0]
	}
	a.sidebar.UpsertItem(item)
}

// deriveConversationName builds a display name for a freshly-opened
// DM/MPIM. For DMs we use the peer's display name. For MPIMs we
// join up to 3 names with ", " and append "+N" for the rest.
func (a *App) deriveConversationName(channelType string, userIDs []string) string {
	const previewLimit = 3
	names := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		if name, ok := a.userNames[id]; ok && name != "" {
			names = append(names, name)
		} else {
			names = append(names, id)
		}
	}
	if channelType == "dm" {
		return names[0]
	}
	if len(names) <= previewLimit {
		return strings.Join(names, ", ")
	}
	preview := strings.Join(names[:previewLimit], ", ")
	return fmt.Sprintf("%s +%d", preview, len(names)-previewLimit)
}

// newMessageResultIsCurrent reports whether a NewMessage* message
// with requestID is the response to the current in-flight submit
// AND wasn't cancelled by an Esc. Late or cancelled results are
// dropped silently.
func newMessageResultIsCurrent(a *App, requestID uint64) bool {
	if requestID == 0 {
		return false
	}
	if requestID != a.newMessageInFlightID {
		return false
	}
	if a.newMessageCancelled {
		return false
	}
	return true
}
