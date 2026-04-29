package slackclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

func TestNewClient(t *testing.T) {
	client := NewClient("xoxc-test", "test-cookie-value")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.TeamID() != "" {
		t.Error("expected empty team ID before connecting")
	}
}

// mockSlackAPI implements SlackAPI for testing.
// Function fields allow tests to override default behavior.
type mockSlackAPI struct {
	getConversationRepliesFn func(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error)
	getEmojiFn               func() (map[string]string, error)
	getPermalinkContextFn    func(ctx context.Context, params *slack.PermalinkParameters) (string, error)
	setUserPresenceContextFn func(ctx context.Context, presence string) error
	getUserPresenceContextFn func(ctx context.Context, user string) (*slack.UserPresence, error)
	setSnoozeContextFn       func(ctx context.Context, minutes int) (*slack.DNDStatus, error)
	endSnoozeContextFn       func(ctx context.Context) (*slack.DNDStatus, error)
	endDNDContextFn          func(ctx context.Context) error
	getDNDInfoContextFn      func(ctx context.Context, user *string, options ...slack.ParamOption) (*slack.DNDStatus, error)
	uploadFileContextFn      func(ctx context.Context, params slack.UploadFileParameters) (*slack.FileSummary, error)
}

func (m *mockSlackAPI) GetConversations(params *slack.GetConversationsParameters) ([]slack.Channel, string, error) {
	return nil, "", nil
}

func (m *mockSlackAPI) GetConversationsForUser(params *slack.GetConversationsForUserParameters) ([]slack.Channel, string, error) {
	return nil, "", nil
}

func (m *mockSlackAPI) GetConversationHistory(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
	return nil, nil
}

func (m *mockSlackAPI) GetConversationReplies(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
	if m.getConversationRepliesFn != nil {
		return m.getConversationRepliesFn(params)
	}
	return []slack.Message{
		{Msg: slack.Msg{Timestamp: "1700000001.000000", Text: "parent msg", User: "U1"}},
		{Msg: slack.Msg{Timestamp: "1700000002.000000", Text: "reply 1", User: "U2"}},
	}, false, "", nil
}

func (m *mockSlackAPI) GetUserInfo(user string) (*slack.User, error) {
	return nil, fmt.Errorf("user not found")
}

func (m *mockSlackAPI) GetUsersContext(ctx context.Context, options ...slack.GetUsersOption) ([]slack.User, error) {
	return nil, nil
}

func (m *mockSlackAPI) GetEmoji() (map[string]string, error) {
	if m.getEmojiFn != nil {
		return m.getEmojiFn()
	}
	return nil, nil
}

func (m *mockSlackAPI) PostMessage(channelID string, options ...slack.MsgOption) (string, string, error) {
	return "", "", nil
}

func (m *mockSlackAPI) UpdateMessage(channelID, timestamp string, options ...slack.MsgOption) (string, string, string, error) {
	return "", "", "", nil
}

func (m *mockSlackAPI) DeleteMessage(channelID, timestamp string) (string, string, error) {
	return "", "", nil
}

func (m *mockSlackAPI) AddReaction(name string, item slack.ItemRef) error {
	return nil
}

func (m *mockSlackAPI) RemoveReaction(name string, item slack.ItemRef) error {
	return nil
}

func (m *mockSlackAPI) AuthTest() (*slack.AuthTestResponse, error) {
	return nil, nil
}

func (m *mockSlackAPI) JoinConversation(channelID string) (*slack.Channel, string, []string, error) {
	return &slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: channelID}}}, "", nil, nil
}

func (m *mockSlackAPI) GetPermalinkContext(ctx context.Context, params *slack.PermalinkParameters) (string, error) {
	if m.getPermalinkContextFn != nil {
		return m.getPermalinkContextFn(ctx, params)
	}
	return "", nil
}

func (m *mockSlackAPI) SetUserPresenceContext(ctx context.Context, presence string) error {
	if m.setUserPresenceContextFn != nil {
		return m.setUserPresenceContextFn(ctx, presence)
	}
	return nil
}

func (m *mockSlackAPI) GetUserPresenceContext(ctx context.Context, user string) (*slack.UserPresence, error) {
	if m.getUserPresenceContextFn != nil {
		return m.getUserPresenceContextFn(ctx, user)
	}
	return &slack.UserPresence{}, nil
}

func (m *mockSlackAPI) SetSnoozeContext(ctx context.Context, minutes int) (*slack.DNDStatus, error) {
	if m.setSnoozeContextFn != nil {
		return m.setSnoozeContextFn(ctx, minutes)
	}
	return &slack.DNDStatus{}, nil
}

func (m *mockSlackAPI) EndSnoozeContext(ctx context.Context) (*slack.DNDStatus, error) {
	if m.endSnoozeContextFn != nil {
		return m.endSnoozeContextFn(ctx)
	}
	return &slack.DNDStatus{}, nil
}

func (m *mockSlackAPI) EndDNDContext(ctx context.Context) error {
	if m.endDNDContextFn != nil {
		return m.endDNDContextFn(ctx)
	}
	return nil
}

func (m *mockSlackAPI) GetDNDInfoContext(ctx context.Context, user *string, options ...slack.ParamOption) (*slack.DNDStatus, error) {
	if m.getDNDInfoContextFn != nil {
		return m.getDNDInfoContextFn(ctx, user, options...)
	}
	return &slack.DNDStatus{}, nil
}

func (m *mockSlackAPI) UploadFileContext(ctx context.Context, params slack.UploadFileParameters) (*slack.FileSummary, error) {
	if m.uploadFileContextFn != nil {
		return m.uploadFileContextFn(ctx, params)
	}
	return &slack.FileSummary{}, nil
}

func TestUploadFile_Success(t *testing.T) {
	var got slack.UploadFileParameters
	mock := &mockSlackAPI{
		uploadFileContextFn: func(ctx context.Context, params slack.UploadFileParameters) (*slack.FileSummary, error) {
			got = params
			return &slack.FileSummary{ID: "F123", Title: "screenshot.png"}, nil
		},
	}
	c := &Client{api: mock}

	r := strings.NewReader("fake-png-bytes")
	f, err := c.UploadFile(context.Background(), "C1", "", "screenshot.png", r, 14, "look at this")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.ID != "F123" {
		t.Errorf("expected FileSummary ID F123, got %q", f.ID)
	}
	if got.Channel != "C1" {
		t.Errorf("expected Channel=C1, got %q", got.Channel)
	}
	if got.Filename != "screenshot.png" {
		t.Errorf("expected Filename=screenshot.png, got %q", got.Filename)
	}
	if got.FileSize != 14 {
		t.Errorf("expected FileSize=14, got %d", got.FileSize)
	}
	if got.InitialComment != "look at this" {
		t.Errorf("expected InitialComment, got %q", got.InitialComment)
	}
	if got.ThreadTimestamp != "" {
		t.Errorf("expected empty ThreadTimestamp, got %q", got.ThreadTimestamp)
	}
}

func TestUploadFile_Thread(t *testing.T) {
	var got slack.UploadFileParameters
	mock := &mockSlackAPI{
		uploadFileContextFn: func(ctx context.Context, params slack.UploadFileParameters) (*slack.FileSummary, error) {
			got = params
			return &slack.FileSummary{ID: "F124"}, nil
		},
	}
	c := &Client{api: mock}

	_, err := c.UploadFile(context.Background(), "C1", "1700000000.000100", "x.png",
		strings.NewReader("x"), 1, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ThreadTimestamp != "1700000000.000100" {
		t.Errorf("expected ThreadTimestamp set, got %q", got.ThreadTimestamp)
	}
	if got.InitialComment != "" {
		t.Errorf("expected empty InitialComment, got %q", got.InitialComment)
	}
}

func TestUploadFile_ErrorWraps(t *testing.T) {
	mock := &mockSlackAPI{
		uploadFileContextFn: func(ctx context.Context, params slack.UploadFileParameters) (*slack.FileSummary, error) {
			return nil, errors.New("not_authorized")
		},
	}
	c := &Client{api: mock}

	_, err := c.UploadFile(context.Background(), "C1", "", "x.png",
		strings.NewReader("x"), 1, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "x.png") {
		t.Errorf("expected error to mention filename, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "not_authorized") {
		t.Errorf("expected error to wrap underlying, got %q", err.Error())
	}
}

func TestClient_SetUserPresence(t *testing.T) {
	var calls int
	var gotPresence string
	mock := &mockSlackAPI{
		setUserPresenceContextFn: func(ctx context.Context, presence string) error {
			calls++
			gotPresence = presence
			return nil
		},
	}
	c := &Client{api: mock}
	if err := c.SetUserPresence(context.Background(), "away"); err != nil {
		t.Fatalf("SetUserPresence: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 SetUserPresence call, got %d", calls)
	}
	if gotPresence != "away" {
		t.Errorf("expected presence 'away', got %q", gotPresence)
	}
}

func TestClient_SetUserPresence_Error(t *testing.T) {
	wantErr := errors.New("api boom")
	mock := &mockSlackAPI{
		setUserPresenceContextFn: func(ctx context.Context, presence string) error {
			return wantErr
		},
	}
	c := &Client{api: mock}
	err := c.SetUserPresence(context.Background(), "away")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped wantErr, got %v", err)
	}
	if !strings.Contains(err.Error(), "setting presence") {
		t.Errorf("expected 'setting presence' prefix, got %q", err.Error())
	}
}

func TestClient_GetUserPresence(t *testing.T) {
	var gotUser string
	mock := &mockSlackAPI{
		getUserPresenceContextFn: func(ctx context.Context, user string) (*slack.UserPresence, error) {
			gotUser = user
			return &slack.UserPresence{Presence: "active"}, nil
		},
	}
	c := &Client{api: mock}
	got, err := c.GetUserPresence(context.Background(), "U1")
	if err != nil {
		t.Fatalf("GetUserPresence: %v", err)
	}
	if got.Presence != "active" {
		t.Errorf("expected 'active', got %q", got.Presence)
	}
	if gotUser != "U1" {
		t.Errorf("expected user 'U1', got %q", gotUser)
	}
}

func TestClient_GetUserPresence_Error(t *testing.T) {
	wantErr := errors.New("api boom")
	mock := &mockSlackAPI{
		getUserPresenceContextFn: func(ctx context.Context, user string) (*slack.UserPresence, error) {
			return nil, wantErr
		},
	}
	c := &Client{api: mock}
	_, err := c.GetUserPresence(context.Background(), "U1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped wantErr, got %v", err)
	}
	if !strings.Contains(err.Error(), "getting presence") {
		t.Errorf("expected 'getting presence' prefix, got %q", err.Error())
	}
}

func TestClient_SetSnooze(t *testing.T) {
	var gotMinutes int
	wantStatus := &slack.DNDStatus{
		SnoozeInfo: slack.SnoozeInfo{SnoozeEnabled: true, SnoozeEndTime: 1700000000},
	}
	mock := &mockSlackAPI{
		setSnoozeContextFn: func(ctx context.Context, minutes int) (*slack.DNDStatus, error) {
			gotMinutes = minutes
			return wantStatus, nil
		},
	}
	c := &Client{api: mock}
	got, err := c.SetSnooze(context.Background(), 60)
	if err != nil {
		t.Fatalf("SetSnooze: %v", err)
	}
	if !got.SnoozeEnabled {
		t.Error("expected snooze enabled")
	}
	if gotMinutes != 60 {
		t.Errorf("expected 60 minutes, got %d", gotMinutes)
	}
}

func TestClient_SetSnooze_Error(t *testing.T) {
	wantErr := errors.New("api boom")
	mock := &mockSlackAPI{
		setSnoozeContextFn: func(ctx context.Context, minutes int) (*slack.DNDStatus, error) {
			return nil, wantErr
		},
	}
	c := &Client{api: mock}
	_, err := c.SetSnooze(context.Background(), 60)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped wantErr, got %v", err)
	}
	if !strings.Contains(err.Error(), "setting snooze") {
		t.Errorf("expected 'setting snooze' prefix, got %q", err.Error())
	}
}

func TestClient_EndSnooze(t *testing.T) {
	calls := 0
	mock := &mockSlackAPI{
		endSnoozeContextFn: func(ctx context.Context) (*slack.DNDStatus, error) {
			calls++
			return &slack.DNDStatus{}, nil
		},
	}
	c := &Client{api: mock}
	if _, err := c.EndSnooze(context.Background()); err != nil {
		t.Fatalf("EndSnooze: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 EndSnooze call, got %d", calls)
	}
}

func TestClient_EndSnooze_Error(t *testing.T) {
	wantErr := errors.New("api boom")
	mock := &mockSlackAPI{
		endSnoozeContextFn: func(ctx context.Context) (*slack.DNDStatus, error) {
			return nil, wantErr
		},
	}
	c := &Client{api: mock}
	_, err := c.EndSnooze(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped wantErr, got %v", err)
	}
	if !strings.Contains(err.Error(), "ending snooze") {
		t.Errorf("expected 'ending snooze' prefix, got %q", err.Error())
	}
}

func TestClient_EndDND(t *testing.T) {
	calls := 0
	mock := &mockSlackAPI{
		endDNDContextFn: func(ctx context.Context) error {
			calls++
			return nil
		},
	}
	c := &Client{api: mock}
	if err := c.EndDND(context.Background()); err != nil {
		t.Fatalf("EndDND: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 EndDND call, got %d", calls)
	}
}

func TestClient_EndDND_Error(t *testing.T) {
	wantErr := errors.New("api boom")
	mock := &mockSlackAPI{
		endDNDContextFn: func(ctx context.Context) error {
			return wantErr
		},
	}
	c := &Client{api: mock}
	err := c.EndDND(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped wantErr, got %v", err)
	}
	if !strings.Contains(err.Error(), "ending DND") {
		t.Errorf("expected 'ending DND' prefix, got %q", err.Error())
	}
}

func TestClient_GetDNDInfo(t *testing.T) {
	var gotUser string
	wantStatus := &slack.DNDStatus{
		Enabled:    true,
		SnoozeInfo: slack.SnoozeInfo{SnoozeEnabled: true, SnoozeEndTime: 1700000000},
	}
	mock := &mockSlackAPI{
		getDNDInfoContextFn: func(ctx context.Context, user *string, options ...slack.ParamOption) (*slack.DNDStatus, error) {
			if user != nil {
				gotUser = *user
			}
			return wantStatus, nil
		},
	}
	c := &Client{api: mock}
	got, err := c.GetDNDInfo(context.Background(), "U1")
	if err != nil {
		t.Fatalf("GetDNDInfo: %v", err)
	}
	if !got.SnoozeEnabled || got.SnoozeEndTime != 1700000000 {
		t.Errorf("unexpected DND status: %+v", got)
	}
	if gotUser != "U1" {
		t.Errorf("expected user 'U1', got %q", gotUser)
	}
}

func TestClient_GetDNDInfo_Error(t *testing.T) {
	wantErr := errors.New("api boom")
	mock := &mockSlackAPI{
		getDNDInfoContextFn: func(ctx context.Context, user *string, options ...slack.ParamOption) (*slack.DNDStatus, error) {
			return nil, wantErr
		},
	}
	c := &Client{api: mock}
	_, err := c.GetDNDInfo(context.Background(), "U1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped wantErr, got %v", err)
	}
	if !strings.Contains(err.Error(), "getting DND info") {
		t.Errorf("expected 'getting DND info' prefix, got %q", err.Error())
	}
}

func TestSendTypingReturnsErrorWhenNotConnected(t *testing.T) {
	c := &Client{}
	err := c.SendTyping("C123")
	if err == nil {
		t.Error("expected error when wsConn is nil")
	}
}

func TestGetReplies(t *testing.T) {
	mock := &mockSlackAPI{}
	client := &Client{api: mock}

	msgs, err := client.GetReplies(context.Background(), "C123", "1700000001.000000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Text != "parent msg" {
		t.Errorf("expected parent msg, got %s", msgs[0].Text)
	}
	if msgs[1].Text != "reply 1" {
		t.Errorf("expected reply 1, got %s", msgs[1].Text)
	}
}

func TestGetReplies_Pagination(t *testing.T) {
	callCount := 0
	mock := &mockSlackAPI{
		getConversationRepliesFn: func(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
			callCount++
			switch callCount {
			case 1:
				if params.Cursor != "" {
					t.Errorf("expected empty cursor on first call, got %q", params.Cursor)
				}
				return []slack.Message{
					{Msg: slack.Msg{Timestamp: "1700000001.000000", Text: "parent msg", User: "U1"}},
					{Msg: slack.Msg{Timestamp: "1700000002.000000", Text: "reply 1", User: "U2"}},
				}, true, "cursor_page2", nil
			case 2:
				if params.Cursor != "cursor_page2" {
					t.Errorf("expected cursor_page2 on second call, got %q", params.Cursor)
				}
				return []slack.Message{
					{Msg: slack.Msg{Timestamp: "1700000003.000000", Text: "reply 2", User: "U3"}},
				}, false, "", nil
			default:
				t.Fatalf("unexpected call #%d to GetConversationReplies", callCount)
				return nil, false, "", nil
			}
		},
	}
	client := &Client{api: mock}

	msgs, err := client.GetReplies(context.Background(), "C123", "1700000001.000000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 API calls, got %d", callCount)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages across 2 pages, got %d", len(msgs))
	}
	expectedTexts := []string{"parent msg", "reply 1", "reply 2"}
	for i, want := range expectedTexts {
		if msgs[i].Text != want {
			t.Errorf("msgs[%d].Text = %q, want %q", i, msgs[i].Text, want)
		}
	}
}

func TestGetReplies_Error(t *testing.T) {
	apiErr := errors.New("slack API unavailable")
	mock := &mockSlackAPI{
		getConversationRepliesFn: func(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
			return nil, false, "", apiErr
		},
	}
	client := &Client{api: mock}

	_, err := client.GetReplies(context.Background(), "C123", "1700000001.000000")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, apiErr) {
		t.Errorf("expected wrapped apiErr, got: %v", err)
	}
	expectedMsg := "getting thread replies: slack API unavailable"
	if err.Error() != expectedMsg {
		t.Errorf("error message = %q, want %q", err.Error(), expectedMsg)
	}
}

func TestListCustomEmoji(t *testing.T) {
	mock := &mockSlackAPI{
		getEmojiFn: func() (map[string]string, error) {
			return map[string]string{
				"partyparrot":  "https://emoji.slack-edge.com/T1/partyparrot/abc.gif",
				"thumbsup_alt": "alias:thumbsup",
			}, nil
		},
	}
	client := &Client{api: mock}

	got, err := client.ListCustomEmoji(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 emojis, got %d", len(got))
	}
	if got["partyparrot"] != "https://emoji.slack-edge.com/T1/partyparrot/abc.gif" {
		t.Errorf("partyparrot URL wrong: %q", got["partyparrot"])
	}
	if got["thumbsup_alt"] != "alias:thumbsup" {
		t.Errorf("thumbsup_alt alias wrong: %q", got["thumbsup_alt"])
	}
}

func TestListCustomEmoji_Error(t *testing.T) {
	apiErr := errors.New("slack API unavailable")
	mock := &mockSlackAPI{
		getEmojiFn: func() (map[string]string, error) {
			return nil, apiErr
		},
	}
	client := &Client{api: mock}

	_, err := client.ListCustomEmoji(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, apiErr) {
		t.Errorf("expected wrapped apiErr, got: %v", err)
	}
}

func TestGetPermalink(t *testing.T) {
	wantURL := "https://example.slack.com/archives/C123/p1700000001000200"
	var gotChannel, gotTS string
	mock := &mockSlackAPI{
		getPermalinkContextFn: func(ctx context.Context, params *slack.PermalinkParameters) (string, error) {
			gotChannel = params.Channel
			gotTS = params.Ts
			return wantURL, nil
		},
	}
	client := &Client{api: mock}

	url, err := client.GetPermalink(context.Background(), "C123", "1700000001.000200")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != wantURL {
		t.Errorf("url = %q, want %q", url, wantURL)
	}
	if gotChannel != "C123" {
		t.Errorf("channel = %q, want %q", gotChannel, "C123")
	}
	if gotTS != "1700000001.000200" {
		t.Errorf("ts = %q, want %q", gotTS, "1700000001.000200")
	}
}

func TestGetPermalinkPropagatesError(t *testing.T) {
	wantErr := errors.New("boom")
	mock := &mockSlackAPI{
		getPermalinkContextFn: func(ctx context.Context, params *slack.PermalinkParameters) (string, error) {
			return "", wantErr
		},
	}
	client := &Client{api: mock}

	_, err := client.GetPermalink(context.Background(), "C123", "1.0")
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want wraps %v", err, wantErr)
	}
}

func TestSubscribePresenceReturnsErrorWhenNotConnected(t *testing.T) {
	c := &Client{}
	err := c.SubscribePresence([]string{"U1", "U2"})
	if err == nil {
		t.Error("expected error when websocket not connected")
	}
}
