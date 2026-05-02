package slackclient

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/slack-go/slack"
)

type dndChangeRecord struct {
	enabled bool
	endUnix int64
}

type mockEventHandler struct {
	messages            []string
	subtypes            []string
	deletedMessages     []string
	reactions           []string
	presenceChanges     []string
	typingEvents        []string
	selfPresenceChanges []string
	dndChanges          []dndChangeRecord
	lastBlocks          slack.Blocks
	lastAttachments     []slack.Attachment

	channelMarks []channelMarkRecord
	threadMarks  []threadMarkRecord

	lastConversationOpenedID string
	lastConversationOpenedCh slack.Channel

	sectionUpserted               *ChannelSectionUpserted
	sectionDeletedID              string
	sectionChannelsAddedSection   string
	sectionChannelsAdded          []string
	sectionChannelsRemovedSection string
	sectionChannelsRemoved        []string
}

type channelMarkRecord struct {
	channelID   string
	ts          string
	unreadCount int
}

type threadMarkRecord struct {
	channelID, threadTS, ts string
	read                    bool
}

func (m *mockEventHandler) OnMessage(channelID, userID, ts, text, threadTS, subtype string, edited bool, files []slack.File, blocks slack.Blocks, attachments []slack.Attachment) {
	m.messages = append(m.messages, text)
	m.subtypes = append(m.subtypes, subtype)
	m.lastBlocks = blocks
	m.lastAttachments = attachments
}

func (m *mockEventHandler) OnMessageDeleted(channelID, ts string) {
	m.deletedMessages = append(m.deletedMessages, ts)
}

func (m *mockEventHandler) OnReactionAdded(channelID, ts, userID, emoji string) {
	m.reactions = append(m.reactions, emoji)
}

func (m *mockEventHandler) OnReactionRemoved(channelID, ts, userID, emoji string) {}
func (m *mockEventHandler) OnPresenceChange(userID, presence string) {
	m.presenceChanges = append(m.presenceChanges, userID+":"+presence)
}
func (m *mockEventHandler) OnUserTyping(channelID, userID string) {
	m.typingEvents = append(m.typingEvents, channelID+":"+userID)
}
func (m *mockEventHandler) OnConnect()    {}
func (m *mockEventHandler) OnDisconnect() {}
func (m *mockEventHandler) OnSelfPresenceChange(presence string) {
	m.selfPresenceChanges = append(m.selfPresenceChanges, presence)
}
func (m *mockEventHandler) OnDNDChange(enabled bool, endUnix int64) {
	m.dndChanges = append(m.dndChanges, dndChangeRecord{enabled, endUnix})
}

func (m *mockEventHandler) OnChannelMarked(channelID, ts string, unreadCount int) {
	m.channelMarks = append(m.channelMarks, channelMarkRecord{channelID, ts, unreadCount})
}

func (m *mockEventHandler) OnThreadMarked(channelID, threadTS, ts string, read bool) {
	m.threadMarks = append(m.threadMarks, threadMarkRecord{channelID, threadTS, ts, read})
}

func (m *mockEventHandler) OnConversationOpened(ch slack.Channel) {
	m.lastConversationOpenedID = ch.ID
	m.lastConversationOpenedCh = ch
}

func (m *mockEventHandler) OnChannelSectionUpserted(ev ChannelSectionUpserted) {
	m.sectionUpserted = &ev
}
func (m *mockEventHandler) OnChannelSectionDeleted(sectionID string) {
	m.sectionDeletedID = sectionID
}
func (m *mockEventHandler) OnChannelSectionChannelsUpserted(sectionID string, channelIDs []string) {
	m.sectionChannelsAddedSection = sectionID
	m.sectionChannelsAdded = channelIDs
}
func (m *mockEventHandler) OnChannelSectionChannelsRemoved(sectionID string, channelIDs []string) {
	m.sectionChannelsRemovedSection = sectionID
	m.sectionChannelsRemoved = channelIDs
}

func TestEventHandlerInterface(t *testing.T) {
	handler := &mockEventHandler{}
	var _ EventHandler = handler

	handler.OnMessage("C1", "U1", "123.456", "hello", "", "", false, nil, slack.Blocks{}, nil)
	if len(handler.messages) != 1 || handler.messages[0] != "hello" {
		t.Error("expected message to be recorded")
	}
}

func TestDispatchWebSocketMessageEvent(t *testing.T) {
	handler := &mockEventHandler{}

	data := []byte(`{"type":"message","channel":"C1","user":"U1","text":"hello world","ts":"123.456"}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(handler.messages))
	}
	if handler.messages[0] != "hello world" {
		t.Errorf("expected 'hello world', got %q", handler.messages[0])
	}
}

func TestDispatchWebSocketBotMessageEvent(t *testing.T) {
	handler := &mockEventHandler{}

	data := []byte(`{"type":"message","subtype":"bot_message","channel":"C1","text":"bot says hi","ts":"123.456","bot_id":"B123"}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(handler.messages))
	}
	if handler.messages[0] != "bot says hi" {
		t.Errorf("expected 'bot says hi', got %q", handler.messages[0])
	}
}

func TestDispatchWebSocketReactionAddedEvent(t *testing.T) {
	handler := &mockEventHandler{}

	data := []byte(`{"type":"reaction_added","user":"U1","reaction":"thumbsup","item":{"channel":"C1","ts":"123.456"}}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.reactions) != 1 {
		t.Fatalf("expected 1 reaction, got %d", len(handler.reactions))
	}
	if handler.reactions[0] != "thumbsup" {
		t.Errorf("expected 'thumbsup', got %q", handler.reactions[0])
	}
}

func TestDispatchWebSocketPresenceChangeEvent(t *testing.T) {
	handler := &mockEventHandler{}

	data := []byte(`{"type":"presence_change","user":"U1","presence":"active"}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.presenceChanges) != 1 {
		t.Fatalf("expected 1 presence change, got %d", len(handler.presenceChanges))
	}
	if handler.presenceChanges[0] != "U1:active" {
		t.Errorf("expected 'U1:active', got %q", handler.presenceChanges[0])
	}
}

func TestDispatchWebSocketMessageDeletedEvent(t *testing.T) {
	handler := &mockEventHandler{}

	data := []byte(`{"type":"message","subtype":"message_deleted","channel":"C1","deleted_ts":"123.456"}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.deletedMessages) != 1 {
		t.Fatalf("expected 1 deleted message, got %d", len(handler.deletedMessages))
	}
	if handler.deletedMessages[0] != "123.456" {
		t.Errorf("expected '123.456', got %q", handler.deletedMessages[0])
	}
}

func TestDispatchWebSocketMessageChangedEvent(t *testing.T) {
	handler := &mockEventHandler{}

	data := []byte(`{"type":"message","subtype":"message_changed","channel":"C1","message":{"user":"U1","text":"edited text","ts":"123.456"},"previous_message":{"text":"original"}}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(handler.messages))
	}
	if handler.messages[0] != "edited text" {
		t.Errorf("expected 'edited text', got %q", handler.messages[0])
	}
}

// TestDispatchWebSocketThreadBroadcastEvent asserts that a
// thread_broadcast subtype is forwarded as a regular OnMessage call
// with the subtype preserved so the UI can render the
// "replied to a thread" label.
func TestDispatchWebSocketThreadBroadcastEvent(t *testing.T) {
	handler := &mockEventHandler{}

	data := []byte(`{"type":"message","subtype":"thread_broadcast","channel":"C1","user":"U1","text":"broadcast","ts":"200.0","thread_ts":"100.0"}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(handler.messages))
	}
	if handler.messages[0] != "broadcast" {
		t.Errorf("expected 'broadcast', got %q", handler.messages[0])
	}
	if handler.subtypes[0] != "thread_broadcast" {
		t.Errorf("expected subtype 'thread_broadcast', got %q", handler.subtypes[0])
	}
}

func TestDispatchWebSocketUserTypingEvent(t *testing.T) {
	handler := &mockEventHandler{}

	data := []byte(`{"type":"user_typing","channel":"C1","user":"U1"}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.typingEvents) != 1 {
		t.Fatalf("expected 1 typing event, got %d", len(handler.typingEvents))
	}
	if handler.typingEvents[0] != "C1:U1" {
		t.Errorf("expected 'C1:U1', got %q", handler.typingEvents[0])
	}
}

func TestDispatchWebSocketManualPresenceChangeEvent(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"manual_presence_change","presence":"away"}`)
	dispatchWebSocketEvent(data, handler)
	if len(handler.selfPresenceChanges) != 1 {
		t.Fatalf("expected 1 self presence change, got %d", len(handler.selfPresenceChanges))
	}
	if handler.selfPresenceChanges[0] != "away" {
		t.Errorf("expected 'away', got %q", handler.selfPresenceChanges[0])
	}
}

func TestDispatchWebSocketDNDUpdatedEvent_ActiveSnooze(t *testing.T) {
	// Snooze is currently active (end is 1h in the future). User is in DND.
	end := time.Now().Add(time.Hour).Unix()
	handler := &mockEventHandler{}
	data := []byte(fmt.Sprintf(
		`{"type":"dnd_updated","dnd_status":{"dnd_enabled":true,"snooze_enabled":true,"snooze_endtime":%d,"next_dnd_start_ts":0,"next_dnd_end_ts":0}}`,
		end))
	dispatchWebSocketEvent(data, handler)
	if len(handler.dndChanges) != 1 {
		t.Fatalf("expected 1 dnd change, got %d", len(handler.dndChanges))
	}
	got := handler.dndChanges[0]
	if !got.enabled {
		t.Error("expected enabled=true (snooze active)")
	}
	if got.endUnix != end {
		t.Errorf("expected endUnix=%d (snooze_endtime), got %d", end, got.endUnix)
	}
}

func TestDispatchWebSocketDNDUpdatedUserEvent_NoDND(t *testing.T) {
	// Neither snooze nor schedule active.
	handler := &mockEventHandler{}
	data := []byte(`{"type":"dnd_updated_user","dnd_status":{"dnd_enabled":false,"snooze_enabled":false,"next_dnd_start_ts":0,"next_dnd_end_ts":0}}`)
	dispatchWebSocketEvent(data, handler)
	if len(handler.dndChanges) != 1 {
		t.Fatalf("expected 1 dnd change, got %d", len(handler.dndChanges))
	}
	got := handler.dndChanges[0]
	if got.enabled {
		t.Error("expected enabled=false")
	}
	if got.endUnix != 0 {
		t.Errorf("expected endUnix=0, got %d", got.endUnix)
	}
}

func TestDispatchWebSocketDNDUpdatedEvent_InScheduledWindow(t *testing.T) {
	// User is currently inside the scheduled DND window.
	now := time.Now().Unix()
	start := now - 600           // 10 min ago
	end := now + 3600             // 1h from now
	handler := &mockEventHandler{}
	data := []byte(fmt.Sprintf(
		`{"type":"dnd_updated","dnd_status":{"dnd_enabled":true,"snooze_enabled":false,"snooze_endtime":0,"next_dnd_start_ts":%d,"next_dnd_end_ts":%d}}`,
		start, end))
	dispatchWebSocketEvent(data, handler)
	got := handler.dndChanges[0]
	if !got.enabled {
		t.Error("expected enabled=true (inside scheduled window)")
	}
	if got.endUnix != end {
		t.Errorf("expected endUnix=%d (next_dnd_end_ts), got %d", end, got.endUnix)
	}
}

func TestDispatchWebSocketDNDUpdatedEvent_BetweenSchedules(t *testing.T) {
	// User has a DND schedule configured, but the current time is BEFORE
	// the next scheduled window starts. dnd_enabled=true is just "schedule
	// exists" — must NOT be interpreted as "currently in DND".
	now := time.Now().Unix()
	start := now + 3600  // 1h from now
	end := now + 7200    // 2h from now
	handler := &mockEventHandler{}
	data := []byte(fmt.Sprintf(
		`{"type":"dnd_updated","dnd_status":{"dnd_enabled":true,"snooze_enabled":false,"snooze_endtime":0,"next_dnd_start_ts":%d,"next_dnd_end_ts":%d}}`,
		start, end))
	dispatchWebSocketEvent(data, handler)
	got := handler.dndChanges[0]
	if got.enabled {
		t.Errorf("expected enabled=false (between scheduled windows), got enabled=true endUnix=%d", got.endUnix)
	}
	if got.endUnix != 0 {
		t.Errorf("expected endUnix=0 when not in DND, got %d", got.endUnix)
	}
}

func TestDispatchMessageForwardsBlocks(t *testing.T) {
	handler := &mockEventHandler{}
	payload := []byte(`{
		"type": "message",
		"channel": "C1",
		"user": "U1",
		"ts": "1700000000.000000",
		"text": "hi",
		"blocks": [
			{ "type": "section", "text": {"type": "mrkdwn", "text": "*hello*"} }
		],
		"attachments": [
			{ "color": "good", "title": "T" }
		]
	}`)
	dispatchWebSocketEvent(payload, handler)

	if len(handler.lastBlocks.BlockSet) != 1 {
		t.Errorf("expected 1 block forwarded; got %d", len(handler.lastBlocks.BlockSet))
	}
	if len(handler.lastAttachments) != 1 {
		t.Errorf("expected 1 attachment forwarded; got %d", len(handler.lastAttachments))
	}
	if handler.lastAttachments[0].Color != "good" {
		t.Errorf("attachment Color = %q, want \"good\"", handler.lastAttachments[0].Color)
	}
}

func TestDispatchWebSocketDNDUpdatedEvent_ExpiredSnooze(t *testing.T) {
	// snooze_enabled=true but snooze_endtime is in the past — Slack hasn't
	// updated yet. Must NOT be reported as in DND.
	end := time.Now().Add(-time.Hour).Unix()
	handler := &mockEventHandler{}
	data := []byte(fmt.Sprintf(
		`{"type":"dnd_updated","dnd_status":{"dnd_enabled":true,"snooze_enabled":true,"snooze_endtime":%d,"next_dnd_start_ts":0,"next_dnd_end_ts":0}}`,
		end))
	dispatchWebSocketEvent(data, handler)
	got := handler.dndChanges[0]
	if got.enabled {
		t.Errorf("expected enabled=false (expired snooze), got enabled=true endUnix=%d", got.endUnix)
	}
}

func TestDispatch_ChannelMarked_CallsHandler(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"channel_marked","channel":"C123","ts":"1700000000.000100","unread_count_display":3}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.channelMarks) != 1 {
		t.Fatalf("expected 1 channelMark, got %d", len(handler.channelMarks))
	}
	got := handler.channelMarks[0]
	if got.channelID != "C123" || got.ts != "1700000000.000100" || got.unreadCount != 3 {
		t.Errorf("unexpected: %+v", got)
	}
}

func TestDispatch_IMMarked_CallsHandler(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"im_marked","channel":"D1","ts":"1.0","unread_count_display":1}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.channelMarks) != 1 {
		t.Fatalf("expected 1 channelMark, got %d", len(handler.channelMarks))
	}
	if handler.channelMarks[0].channelID != "D1" {
		t.Errorf("channel: %q", handler.channelMarks[0].channelID)
	}
}

func TestDispatch_GroupMarked_CallsHandler(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"group_marked","channel":"G1","ts":"1.0","unread_count_display":0}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.channelMarks) != 1 {
		t.Fatalf("expected 1 channelMark, got %d", len(handler.channelMarks))
	}
	if handler.channelMarks[0].unreadCount != 0 {
		t.Errorf("unreadCount: %d", handler.channelMarks[0].unreadCount)
	}
}

func TestDispatch_MPIMMarked_CallsHandler(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"mpim_marked","channel":"G2","ts":"1.0","unread_count_display":2}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.channelMarks) != 1 {
		t.Fatalf("expected 1 channelMark, got %d", len(handler.channelMarks))
	}
}

func TestDispatch_ChannelMarked_MalformedJSON_NoCall(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"channel_marked","channel":`) // truncated
	dispatchWebSocketEvent(data, handler)

	if len(handler.channelMarks) != 0 {
		t.Errorf("expected 0 calls on malformed JSON, got %d", len(handler.channelMarks))
	}
}

func TestDispatch_ThreadMarked_Unread_CallsHandler(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"thread_marked","subscription":{"channel":"C1","thread_ts":"1700000000.000100","last_read":"1700000000.000200","active":true}}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.threadMarks) != 1 {
		t.Fatalf("expected 1 threadMark, got %d", len(handler.threadMarks))
	}
	got := handler.threadMarks[0]
	if got.channelID != "C1" || got.threadTS != "1700000000.000100" || got.ts != "1700000000.000200" {
		t.Errorf("unexpected: %+v", got)
	}
	if got.read {
		t.Error("expected read=false (active=true means unread)")
	}
}

func TestDispatch_ThreadMarked_Read_CallsHandler(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"thread_marked","subscription":{"channel":"C1","thread_ts":"P1","last_read":"R5","active":false}}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.threadMarks) != 1 {
		t.Fatalf("expected 1 threadMark, got %d", len(handler.threadMarks))
	}
	if !handler.threadMarks[0].read {
		t.Error("expected read=true (active=false means read)")
	}
}

func TestDispatch_ThreadMarked_MalformedJSON_NoCall(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"thread_marked","subscription":{`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.threadMarks) != 0 {
		t.Errorf("expected 0 calls on malformed JSON, got %d", len(handler.threadMarks))
	}
}

func TestDispatch_MPIMOpen(t *testing.T) {
	h := &mockEventHandler{}
	payload := []byte(`{"type":"mpim_open","channel":{"id":"G1","is_mpim":true,"name":"mpdm-alice--bob-1"}}`)
	dispatchWebSocketEvent(payload, h)
	if h.lastConversationOpenedID != "G1" {
		t.Errorf("OnConversationOpened not called or wrong ID; got %q", h.lastConversationOpenedID)
	}
}

func TestDispatch_IMCreated(t *testing.T) {
	h := &mockEventHandler{}
	payload := []byte(`{"type":"im_created","channel":{"id":"D1","is_im":true,"user":"U1"}}`)
	dispatchWebSocketEvent(payload, h)
	if h.lastConversationOpenedID != "D1" {
		t.Errorf("OnConversationOpened not called or wrong ID; got %q", h.lastConversationOpenedID)
	}
}

func TestDispatch_IMOpen(t *testing.T) {
	h := &mockEventHandler{}
	payload := []byte(`{"type":"im_open","channel":{"id":"D2","is_im":true,"user":"U2"}}`)
	dispatchWebSocketEvent(payload, h)
	if h.lastConversationOpenedID != "D2" {
		t.Errorf("OnConversationOpened not called or wrong ID; got %q", h.lastConversationOpenedID)
	}
}

func TestDispatch_GroupJoined(t *testing.T) {
	h := &mockEventHandler{}
	payload := []byte(`{"type":"group_joined","channel":{"id":"G2","is_group":true,"name":"private-room"}}`)
	dispatchWebSocketEvent(payload, h)
	if h.lastConversationOpenedID != "G2" {
		t.Errorf("OnConversationOpened not called or wrong ID; got %q", h.lastConversationOpenedID)
	}
}

func TestDispatch_ChannelJoined(t *testing.T) {
	h := &mockEventHandler{}
	payload := []byte(`{"type":"channel_joined","channel":{"id":"C1","is_channel":true,"name":"general"}}`)
	dispatchWebSocketEvent(payload, h)
	if h.lastConversationOpenedID != "C1" {
		t.Errorf("OnConversationOpened not called or wrong ID; got %q", h.lastConversationOpenedID)
	}
}

func TestDispatch_ChannelSectionUpserted(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "ws_section_upserted.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	h := &mockEventHandler{}
	dispatchWebSocketEvent(data, h)
	if h.sectionUpserted == nil {
		t.Fatalf("handler not called")
	}
	if h.sectionUpserted.ID != "L0B12LBBCTD" || h.sectionUpserted.Type != "standard" {
		t.Errorf("got %+v", h.sectionUpserted)
	}
}

func TestDispatch_ChannelSectionDeleted(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "ws_section_deleted.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	h := &mockEventHandler{}
	dispatchWebSocketEvent(data, h)
	if h.sectionDeletedID != "L0B12L90PLK" {
		t.Errorf("sectionDeletedID = %q", h.sectionDeletedID)
	}
}

func TestDispatch_ChannelsUpserted(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "ws_channels_upserted.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	h := &mockEventHandler{}
	dispatchWebSocketEvent(data, h)
	if h.sectionChannelsAddedSection != "L0B1709V0LE" {
		t.Errorf("section = %q", h.sectionChannelsAddedSection)
	}
	if len(h.sectionChannelsAdded) != 1 || h.sectionChannelsAdded[0] != "D09R4P6G6QL" {
		t.Errorf("channels = %v", h.sectionChannelsAdded)
	}
}

func TestDispatch_ChannelsRemoved(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "ws_channels_removed.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	h := &mockEventHandler{}
	dispatchWebSocketEvent(data, h)
	if h.sectionChannelsRemovedSection != "L0B1709V0LE" {
		t.Errorf("section = %q", h.sectionChannelsRemovedSection)
	}
	if len(h.sectionChannelsRemoved) != 1 || h.sectionChannelsRemoved[0] != "C0AR3C3HMJT" {
		t.Errorf("channels = %v", h.sectionChannelsRemoved)
	}
}
