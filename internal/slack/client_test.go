package slackclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	authTestFn               func() (*slack.AuthTestResponse, error)
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
	if m.authTestFn != nil {
		return m.authTestFn()
	}
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

// deriveAPIBaseURL turns the team URL returned by auth.test (e.g.
// "https://hackclub.enterprise.slack.com/") into the API base URL we send
// requests to ("https://hackclub.enterprise.slack.com/api/"). Empty or
// malformed input falls back to the canonical "https://slack.com/api/".
func TestDeriveAPIBaseURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "enterprise grid URL",
			in:   "https://hackclub.enterprise.slack.com/",
			want: "https://hackclub.enterprise.slack.com/api/",
		},
		{
			name: "standard workspace URL",
			in:   "https://myteam.slack.com/",
			want: "https://myteam.slack.com/api/",
		},
		{
			name: "URL without trailing slash",
			in:   "https://myteam.slack.com",
			want: "https://myteam.slack.com/api/",
		},
		{
			name: "empty string falls back",
			in:   "",
			want: "https://slack.com/api/",
		},
		{
			name: "non-slack host falls back",
			in:   "https://evil.example.com/",
			want: "https://slack.com/api/",
		},
		{
			name: "garbage input falls back",
			in:   "not a url",
			want: "https://slack.com/api/",
		},
		{
			name: "missing scheme falls back",
			in:   "myteam.slack.com",
			want: "https://slack.com/api/",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveAPIBaseURL(tc.in)
			if got != tc.want {
				t.Errorf("deriveAPIBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestConnect_DiscoversEnterpriseAPIBaseURL(t *testing.T) {
	mock := &mockSlackAPI{
		authTestFn: func() (*slack.AuthTestResponse, error) {
			return &slack.AuthTestResponse{
				URL:    "https://hackclub.enterprise.slack.com/",
				TeamID: "T1",
				UserID: "U1",
			}, nil
		},
	}
	c := &Client{api: mock}
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	want := "https://hackclub.enterprise.slack.com/api/"
	if c.apiBaseURL != want {
		t.Errorf("apiBaseURL = %q, want %q", c.apiBaseURL, want)
	}
	if c.teamID != "T1" {
		t.Errorf("teamID = %q, want %q", c.teamID, "T1")
	}
}

func TestConnect_DiscoversStandardWorkspaceAPIBaseURL(t *testing.T) {
	mock := &mockSlackAPI{
		authTestFn: func() (*slack.AuthTestResponse, error) {
			return &slack.AuthTestResponse{
				URL:    "https://myteam.slack.com/",
				TeamID: "T1",
				UserID: "U1",
			}, nil
		},
	}
	c := &Client{api: mock}
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	want := "https://myteam.slack.com/api/"
	if c.apiBaseURL != want {
		t.Errorf("apiBaseURL = %q, want %q", c.apiBaseURL, want)
	}
}

func TestConnect_FallsBackWhenAuthTestURLIsEmpty(t *testing.T) {
	mock := &mockSlackAPI{
		authTestFn: func() (*slack.AuthTestResponse, error) {
			return &slack.AuthTestResponse{
				URL:    "",
				TeamID: "T1",
				UserID: "U1",
			}, nil
		},
	}
	c := &Client{api: mock}
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	want := "https://slack.com/api/"
	if c.apiBaseURL != want {
		t.Errorf("apiBaseURL = %q, want %q", c.apiBaseURL, want)
	}
}

// MarkChannel must POST to the workspace-specific API host so it works on
// enterprise grid workspaces, not just to slack.com.
func TestMarkChannel_UsesAPIBaseURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := &Client{
		token:      "xoxc-test",
		cookie:     "d-cookie",
		apiBaseURL: srv.URL + "/api/",
		httpClient: srv.Client(),
	}
	if err := c.MarkChannel(context.Background(), "C1", "1700000000.000100"); err != nil {
		t.Fatalf("MarkChannel: %v", err)
	}
	if gotPath != "/api/conversations.mark" {
		t.Errorf("path = %q, want %q", gotPath, "/api/conversations.mark")
	}
}

func TestMarkThread_UsesAPIBaseURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := &Client{
		token:      "xoxc-test",
		cookie:     "d-cookie",
		apiBaseURL: srv.URL + "/api/",
		httpClient: srv.Client(),
	}
	if err := c.MarkThread(context.Background(), "C1", "1700000000.000100", "1700000001.000200"); err != nil {
		t.Fatalf("MarkThread: %v", err)
	}
	if gotPath != "/api/subscriptions.thread.mark" {
		t.Errorf("path = %q, want %q", gotPath, "/api/subscriptions.thread.mark")
	}
}

func TestGetUnreadCounts_UsesAPIBaseURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		// Minimal valid response so the parser stays happy.
		_, _ = w.Write([]byte(`{"ok":true,"channels":[],"mpims":[],"ims":[],"threads":{"has_unreads":false}}`))
	}))
	defer srv.Close()

	c := &Client{
		token:      "xoxc-test",
		cookie:     "d-cookie",
		apiBaseURL: srv.URL + "/api/",
	}
	if _, _, err := c.GetUnreadCounts(); err != nil {
		t.Fatalf("GetUnreadCounts: %v", err)
	}
	if gotPath != "/api/client.counts" {
		t.Errorf("path = %q, want %q", gotPath, "/api/client.counts")
	}
}

func TestGetChannelSections_UsesAPIBaseURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"channel_sections":[]}`))
	}))
	defer srv.Close()

	c := &Client{
		token:      "xoxc-test",
		cookie:     "d-cookie",
		apiBaseURL: srv.URL + "/api/",
	}
	if _, err := c.GetChannelSections(context.Background()); err != nil {
		t.Fatalf("GetChannelSections: %v", err)
	}
	if gotPath != "/api/users.channelSections.list" {
		t.Errorf("path = %q, want %q", gotPath, "/api/users.channelSections.list")
	}
}

// NewClient must give the resulting Client a non-empty apiBaseURL so that
// methods called before Connect() (or in tests that bypass Connect) still
// produce well-formed URLs. The default is the canonical slack.com host.
func TestNewClient_HasDefaultAPIBaseURL(t *testing.T) {
	c := NewClient("xoxc-test", "d-cookie")
	if c.apiBaseURL != "https://slack.com/api/" {
		t.Errorf("apiBaseURL = %q, want %q", c.apiBaseURL, "https://slack.com/api/")
	}
}

// newTestClient returns a *Client wired to point at the given test server.
// Internal helpers like markChannel use c.httpClient (set here) and
// c.apiBaseURL (which defaults to https://slack.com/api/ in production).
// We deliberately reuse the existing apiBaseURL field rather than introducing
// a parallel markBaseURL so that mark endpoints continue to honor enterprise
// grid host discovery (see TestMarkChannel_UsesAPIBaseURL).
func newTestClient(server *httptest.Server) *Client {
	return &Client{
		token:      "xoxc-test",
		cookie:     "test-cookie",
		httpClient: server.Client(),
		apiBaseURL: server.URL + "/api/",
	}
}

func TestMarkChannel_PostsCorrectForm(t *testing.T) {
	var gotPath, gotAuth, gotContentType, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if err := c.MarkChannel(context.Background(), "C123", "1700000000.000100"); err != nil {
		t.Fatalf("MarkChannel: %v", err)
	}

	if !strings.HasSuffix(gotPath, "/conversations.mark") {
		t.Errorf("path: got %q, want suffix /conversations.mark", gotPath)
	}
	if gotAuth != "Bearer xoxc-test" {
		t.Errorf("auth: got %q", gotAuth)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Errorf("content-type: got %q", gotContentType)
	}
	form, _ := url.ParseQuery(gotBody)
	if form.Get("channel") != "C123" {
		t.Errorf("channel: got %q", form.Get("channel"))
	}
	if form.Get("ts") != "1700000000.000100" {
		t.Errorf("ts: got %q", form.Get("ts"))
	}
}

func TestMarkThread_PostsReadOne(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if err := c.MarkThread(context.Background(), "C1", "P1", "R5"); err != nil {
		t.Fatalf("MarkThread: %v", err)
	}

	if !strings.HasSuffix(gotPath, "/subscriptions.thread.mark") {
		t.Errorf("path: got %q", gotPath)
	}
	form, _ := url.ParseQuery(gotBody)
	if form.Get("channel") != "C1" || form.Get("thread_ts") != "P1" || form.Get("ts") != "R5" {
		t.Errorf("form: %v", form)
	}
	if form.Get("read") != "1" {
		t.Errorf("expected read=1, got %q", form.Get("read"))
	}
}

func TestMarkThread_EmptyArgs_NoOp(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if err := c.MarkThread(context.Background(), "", "P1", "R5"); err != nil {
		t.Errorf("expected nil err on empty channelID, got %v", err)
	}
	if err := c.MarkThread(context.Background(), "C1", "", "R5"); err != nil {
		t.Errorf("expected nil err on empty threadTS, got %v", err)
	}
	if called {
		t.Error("expected no HTTP call when args are empty")
	}
}

func TestMarkChannelUnread_PostsCorrectForm(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if err := c.MarkChannelUnread(context.Background(), "C123", "1700000000.000050"); err != nil {
		t.Fatalf("MarkChannelUnread: %v", err)
	}

	if !strings.HasSuffix(gotPath, "/conversations.mark") {
		t.Errorf("path: got %q", gotPath)
	}
	form, _ := url.ParseQuery(gotBody)
	if form.Get("channel") != "C123" {
		t.Errorf("channel: got %q", form.Get("channel"))
	}
	if form.Get("ts") != "1700000000.000050" {
		t.Errorf("ts: got %q", form.Get("ts"))
	}
}

func TestMarkChannelUnread_EmptyTSSendsZero(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if err := c.MarkChannelUnread(context.Background(), "C1", ""); err != nil {
		t.Fatalf("MarkChannelUnread: %v", err)
	}

	form, _ := url.ParseQuery(gotBody)
	if form.Get("ts") != "0" {
		t.Errorf("expected ts=0 for empty input, got %q", form.Get("ts"))
	}
}

func TestMarkThreadUnread_PostsReadZero(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if err := c.MarkThreadUnread(context.Background(), "C1", "P1", "R5"); err != nil {
		t.Fatalf("MarkThreadUnread: %v", err)
	}

	if !strings.HasSuffix(gotPath, "/subscriptions.thread.mark") {
		t.Errorf("path: got %q", gotPath)
	}
	form, _ := url.ParseQuery(gotBody)
	if form.Get("channel") != "C1" || form.Get("thread_ts") != "P1" || form.Get("ts") != "R5" {
		t.Errorf("form: %v", form)
	}
	if form.Get("read") != "0" {
		t.Errorf("expected read=0, got %q", form.Get("read"))
	}
}

func TestMarkThreadUnread_EmptyArgs_NoOp(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if err := c.MarkThreadUnread(context.Background(), "", "P1", "R5"); err != nil {
		t.Errorf("expected nil err, got %v", err)
	}
	if err := c.MarkThreadUnread(context.Background(), "C1", "", "R5"); err != nil {
		t.Errorf("expected nil err, got %v", err)
	}
	if called {
		t.Error("expected no HTTP call when args empty")
	}
}
