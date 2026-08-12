package slackclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gammons/slk/internal/slackhttp"
	"github.com/gorilla/websocket"
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

// newTestSlackAPI returns a real *slack.Client wired to a captured
// httptest.Server, plus a closure to inspect the most recent request's
// form values. Use this when tests need to verify the actual wire-form
// payload (especially blocks, which slack-go only serialises inside
// the request pipeline, NOT inside UnsafeApplyMsgOptions).
//
// The server returns a successful chat.postMessage response by default.
// Pass a non-empty resp to override the response body.
func newTestSlackAPI(t *testing.T, resp string) (*slack.Client, func() url.Values, func()) {
	t.Helper()
	if resp == "" {
		resp = `{"ok":true,"ts":"1700000000.000100","channel":"C1"}`
	}
	var lastForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
			http.Error(w, err.Error(), 500)
			return
		}
		lastForm = r.Form
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, resp)
	}))
	api := slack.New("xoxc-test", slack.OptionAPIURL(srv.URL+"/"))
	return api, func() url.Values { return lastForm }, srv.Close
}

func TestSendMessage_BuildsRichTextBlock(t *testing.T) {
	api, getForm, closeFn := newTestSlackAPI(t, "")
	defer closeFn()
	c := &Client{api: api}

	ts, sentMrkdwn, err := c.SendMessage(context.Background(), "C1", "**hello** world")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if ts != "1700000000.000100" {
		t.Errorf("ts = %q, want canned timestamp", ts)
	}
	if sentMrkdwn != "*hello* world" {
		t.Errorf("sentMrkdwn = %q, want %q", sentMrkdwn, "*hello* world")
	}
	form := getForm()
	if got := form.Get("channel"); got != "C1" {
		t.Errorf("channel = %q, want C1", got)
	}
	if got := form.Get("text"); got != "*hello* world" {
		t.Errorf("wire text = %q, want %q", got, "*hello* world")
	}
	blocksJSON := form.Get("blocks")
	if blocksJSON == "" {
		t.Fatal("blocks form value is empty; expected serialised rich_text block")
	}
	if !strings.Contains(blocksJSON, `"type":"rich_text"`) {
		t.Errorf("blocks JSON does not contain rich_text type: %s", blocksJSON)
	}
	// Loose check: the bold word should appear with bold style
	if !strings.Contains(blocksJSON, `"bold":true`) {
		t.Errorf("blocks JSON does not contain bold style: %s", blocksJSON)
	}
}

func TestSendMessage_PlainTextSendsBothTextAndBlocks(t *testing.T) {
	// Even plain text gets a rich_text block (uniform wire shape).
	api, getForm, closeFn := newTestSlackAPI(t, "")
	defer closeFn()
	c := &Client{api: api}

	_, mr, err := c.SendMessage(context.Background(), "C1", "hello")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if mr != "hello" {
		t.Errorf("mrkdwn = %q", mr)
	}
	form := getForm()
	if form.Get("text") != "hello" {
		t.Errorf("wire text = %q", form.Get("text"))
	}
	if form.Get("blocks") == "" {
		t.Error("blocks form value empty; want a rich_text block even for plain text")
	}
}

func TestSendMessage_EmptyTextSendsNoBlocks(t *testing.T) {
	// Empty input should produce empty mrkdwn and no blocks.
	api, getForm, closeFn := newTestSlackAPI(t, "")
	defer closeFn()
	c := &Client{api: api}

	_, mr, _ := c.SendMessage(context.Background(), "C1", "")
	if mr != "" {
		t.Errorf("mrkdwn = %q, want empty", mr)
	}
	form := getForm()
	if form.Get("text") != "" {
		t.Errorf("text = %q, want empty", form.Get("text"))
	}
	if form.Get("blocks") != "" {
		t.Errorf("blocks = %q, want empty", form.Get("blocks"))
	}
}

// mockSlackAPI implements SlackAPI for testing.
// Function fields allow tests to override default behavior.
type mockSlackAPI struct {
	authTestFn                      func() (*slack.AuthTestResponse, error)
	getConversationHistoryFn        func(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error)
	getConversationRepliesFn        func(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error)
	getEmojiFn                      func() (map[string]string, error)
	getPermalinkContextFn           func(ctx context.Context, params *slack.PermalinkParameters) (string, error)
	setUserPresenceContextFn        func(ctx context.Context, presence string) error
	getUserPresenceContextFn        func(ctx context.Context, user string) (*slack.UserPresence, error)
	setSnoozeContextFn              func(ctx context.Context, minutes int) (*slack.DNDStatus, error)
	endSnoozeContextFn              func(ctx context.Context) (*slack.DNDStatus, error)
	endDNDContextFn                 func(ctx context.Context) error
	getDNDInfoContextFn             func(ctx context.Context, user *string, options ...slack.ParamOption) (*slack.DNDStatus, error)
	uploadFileContextFn             func(ctx context.Context, params slack.UploadFileParameters) (*slack.FileSummary, error)
	getUsersInConversationContextFn func(ctx context.Context, params *slack.GetUsersInConversationParameters) ([]string, string, error)
	openConversationContextFn       func(ctx context.Context, params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error)
	searchMessagesFn                func(ctx context.Context, query string, params slack.SearchParameters) (*slack.SearchMessages, error)
	getUserGroupsContextFn          func(ctx context.Context, options ...slack.GetUserGroupsOption) ([]slack.UserGroup, error)
}

func (m *mockSlackAPI) GetUserGroupsContext(ctx context.Context, options ...slack.GetUserGroupsOption) ([]slack.UserGroup, error) {
	if m.getUserGroupsContextFn != nil {
		return m.getUserGroupsContextFn(ctx, options...)
	}
	return nil, nil
}

func (m *mockSlackAPI) SearchMessagesContext(ctx context.Context, query string, params slack.SearchParameters) (*slack.SearchMessages, error) {
	if m.searchMessagesFn != nil {
		return m.searchMessagesFn(ctx, query, params)
	}
	return &slack.SearchMessages{}, nil
}

func (m *mockSlackAPI) GetConversationsForUser(params *slack.GetConversationsForUserParameters) ([]slack.Channel, string, error) {
	return nil, "", nil
}

func (m *mockSlackAPI) GetConversationHistory(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
	if m.getConversationHistoryFn != nil {
		return m.getConversationHistoryFn(params)
	}
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

func (m *mockSlackAPI) GetBotInfoContext(ctx context.Context, parameters slack.GetBotInfoParameters) (*slack.Bot, error) {
	return nil, fmt.Errorf("bot not found")
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

func (m *mockSlackAPI) GetUsersInConversationContext(ctx context.Context, params *slack.GetUsersInConversationParameters) ([]string, string, error) {
	if m.getUsersInConversationContextFn != nil {
		return m.getUsersInConversationContextFn(ctx, params)
	}
	return nil, "", nil
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
		_, _ = w.Write([]byte(`{
			"ok": true,
			"channel_sections": [
				{
					"channel_section_id": "L1",
					"name": "Engineering",
					"type": "standard",
					"emoji": "rocket",
					"next_channel_section_id": "L2",
					"last_updated": 1700000000,
					"channel_ids_page": {"channel_ids": ["C1","C2"], "count": 2, "cursor": ""},
					"is_redacted": false
				}
			],
			"count": 1,
			"cursor": ""
		}`))
	}))
	defer srv.Close()

	c := &Client{
		token:      "xoxc-test",
		cookie:     "d-cookie",
		apiBaseURL: srv.URL + "/api/",
	}
	sections, err := c.GetChannelSections(context.Background())
	if err != nil {
		t.Fatalf("GetChannelSections: %v", err)
	}
	if gotPath != "/api/users.channelSections.list" {
		t.Errorf("path = %q, want %q", gotPath, "/api/users.channelSections.list")
	}
	if len(sections) != 1 {
		t.Fatalf("sections len = %d, want 1", len(sections))
	}
	s := sections[0]
	if s.ID != "L1" || s.Name != "Engineering" || s.Type != "standard" {
		t.Errorf("section = %+v", s)
	}
	if len(s.ChannelIDs) != 2 || s.ChannelIDs[0] != "C1" || s.ChannelIDs[1] != "C2" {
		t.Errorf("ChannelIDs = %v", s.ChannelIDs)
	}
}

// TestGetStarredChannels_ParsesItems verifies GetStarredChannels extracts
// channel-typed starred items from stars.list. Slack's channelSections.list
// returns the stars section with an empty channel_ids array; stars.list is
// the authoritative source for which channels the user has starred.
func TestGetStarredChannels_ParsesItems(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ok": true,
			"items": [
				{"type": "channel", "channel": "C1", "date_create": 1700000001},
				{"type": "channel", "channel": "C2", "date_create": 1700000002},
				{"type": "message", "channel": "C9", "message": {"ts": "1.1"}, "date_create": 1700000003},
				{"type": "im", "channel": "D1", "date_create": 1700000004}
			],
			"paging": {"count": 4, "total": 4}
		}`))
	}))
	defer srv.Close()

	c := &Client{
		token:      "xoxc-test",
		cookie:     "d-cookie",
		apiBaseURL: srv.URL + "/api/",
	}
	got, err := c.GetStarredChannels(context.Background())
	if err != nil {
		t.Fatalf("GetStarredChannels: %v", err)
	}
	if gotPath != "/api/stars.list" {
		t.Errorf("path = %q, want %q", gotPath, "/api/stars.list")
	}
	// Only type=="channel" items count; message/im stars are not sidebar channels.
	want := []string{"C1", "C2"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("[%d] = %q, want %q", i, got[i], w)
		}
	}
}

// TestGetStarredChannels_APIError verifies the client surfaces Slack's
// ok=false responses as errors rather than returning an empty list silently.
func TestGetStarredChannels_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": false, "error": "not_authed"}`))
	}))
	defer srv.Close()

	c := &Client{
		token:      "xoxc-test",
		cookie:     "d-cookie",
		apiBaseURL: srv.URL + "/api/",
	}
	if _, err := c.GetStarredChannels(context.Background()); err == nil {
		t.Errorf("expected error on ok=false response")
	}
}

// TestHandRolledEndpoints_FormBodyTokenNoBearer verifies that every
// undocumented endpoint slk calls directly sends the xoxc token in the
// form body (the browser-client convention) rather than as Authorization:
// Bearer (an OAuth pattern). Mixing Bearer with the browser-like headers
// BrowserTransport injects is a contradictory request signature that
// triggers Enterprise Grid anomaly detection (issue #5).
//
// One test, multiple endpoints — each subtest is one hand-rolled endpoint.
func TestHandRolledEndpoints_FormBodyTokenNoBearer(t *testing.T) {
	type capture struct {
		auth    string
		bodyTok string
		path    string
	}

	cases := []struct {
		name string
		// Body the test server returns. Must be valid for the parser
		// inside the endpoint we're calling, otherwise the call errors
		// before our assertions run.
		respBody string
		// Drive the endpoint through Client.
		call func(t *testing.T, c *Client)
	}{
		{
			name:     "GetUnreadCounts",
			respBody: `{"ok":true,"channels":[],"mpims":[],"ims":[],"threads":{"has_unreads":false}}`,
			call: func(t *testing.T, c *Client) {
				if _, _, err := c.GetUnreadCounts(); err != nil {
					t.Fatalf("GetUnreadCounts: %v", err)
				}
			},
		},
		{
			name:     "MarkChannel",
			respBody: `{"ok":true}`,
			call: func(t *testing.T, c *Client) {
				if err := c.MarkChannel(context.Background(), "C1", "1700000000.000100"); err != nil {
					t.Fatalf("MarkChannel: %v", err)
				}
			},
		},
		{
			name:     "MarkThread",
			respBody: `{"ok":true}`,
			call: func(t *testing.T, c *Client) {
				if err := c.MarkThread(context.Background(), "C1", "1700000000.000100", "1700000001.000200"); err != nil {
					t.Fatalf("MarkThread: %v", err)
				}
			},
		},
		{
			name:     "GetMutedChannels",
			respBody: `{"ok":true,"prefs":{"muted_channels":""}}`,
			call: func(t *testing.T, c *Client) {
				if _, err := c.GetMutedChannels(context.Background()); err != nil {
					t.Fatalf("GetMutedChannels: %v", err)
				}
			},
		},
		{
			name:     "GetChannelSections",
			respBody: `{"ok":true,"channel_sections":[],"count":0,"cursor":""}`,
			call: func(t *testing.T, c *Client) {
				if _, err := c.GetChannelSections(context.Background()); err != nil {
					t.Fatalf("GetChannelSections: %v", err)
				}
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var got capture
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got.auth = r.Header.Get("Authorization")
				got.path = r.URL.Path
				if err := r.ParseForm(); err != nil {
					t.Errorf("ParseForm: %v", err)
				}
				got.bodyTok = r.PostForm.Get("token")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.respBody))
			}))
			defer srv.Close()

			c := &Client{
				token:      "xoxc-test",
				cookie:     "d-cookie",
				apiBaseURL: srv.URL + "/api/",
				httpClient: srv.Client(),
			}
			tc.call(t, c)

			if got.auth != "" {
				t.Errorf("Authorization header present: %q; want empty (browser clients don't send Bearer to app.slack.com)", got.auth)
			}
			if got.bodyTok != "xoxc-test" {
				t.Errorf("form body token = %q; want xoxc-test (token must be in form body, not Authorization header)", got.bodyTok)
			}
		})
	}
}

func TestGetChannelSections_FollowsTopLevelCursor(t *testing.T) {
	var calls int
	var capturedCursors []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = r.ParseForm()
		capturedCursors = append(capturedCursors, r.PostForm.Get("cursor"))
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			// First page: returns cursor "P2".
			_, _ = w.Write([]byte(`{
				"ok": true,
				"channel_sections": [
					{
						"channel_section_id": "L1",
						"name": "First",
						"type": "standard",
						"emoji": "",
						"next_channel_section_id": "L2",
						"last_updated": 1700000000,
						"channel_ids_page": {"channel_ids": [], "count": 0},
						"is_redacted": false
					}
				],
				"cursor": "P2"
			}`))
		case 2:
			// Second page: cursor empty terminates loop.
			_, _ = w.Write([]byte(`{
				"ok": true,
				"channel_sections": [
					{
						"channel_section_id": "L2",
						"name": "Second",
						"type": "standard",
						"emoji": "",
						"next_channel_section_id": null,
						"last_updated": 1700000001,
						"channel_ids_page": {"channel_ids": [], "count": 0},
						"is_redacted": false
					}
				],
				"cursor": ""
			}`))
		default:
			t.Errorf("unexpected call %d", calls)
		}
	}))
	defer srv.Close()

	c := &Client{
		token:      "xoxc-test",
		cookie:     "d-cookie",
		apiBaseURL: srv.URL + "/api/",
	}
	sections, err := c.GetChannelSections(context.Background())
	if err != nil {
		t.Fatalf("GetChannelSections: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
	if len(sections) != 2 || sections[0].ID != "L1" || sections[1].ID != "L2" {
		t.Errorf("sections = %+v, want L1 then L2", sections)
	}
	if len(capturedCursors) != 2 {
		t.Fatalf("capturedCursors len = %d", len(capturedCursors))
	}
	if capturedCursors[0] != "" {
		t.Errorf("first call cursor = %q, want empty", capturedCursors[0])
	}
	if capturedCursors[1] != "P2" {
		t.Errorf("second call cursor = %q, want P2", capturedCursors[1])
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

// NewClient must likewise give the Client a wsBaseURL, since StartWebSocket
// builds its URL from that field alone. The value is pinned literally rather
// than compared against defaultWSBaseURL: the host is the one the official
// web client dials, and changing it silently would move slk's event socket
// off the browser's path.
func TestNewClient_HasDefaultWSBaseURL(t *testing.T) {
	c := NewClient("xoxc-test", "d-cookie")
	if c.wsBaseURL != "wss://wss-primary.slack.com" {
		t.Errorf("wsBaseURL = %q, want %q", c.wsBaseURL, "wss://wss-primary.slack.com")
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
	// The hand-rolled endpoints used to send Authorization: Bearer; that's
	// an OAuth/server-side pattern browsers never use against app.slack.com.
	// Combined with the browser-shaped headers BrowserTransport injects it
	// looks contradictory to Slack's anomaly detector and was logging
	// Enterprise Grid users out (issue #5). Token now goes in the form body.
	if gotAuth != "" {
		t.Errorf("auth: got %q, want empty (token belongs in form body)", gotAuth)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Errorf("content-type: got %q", gotContentType)
	}
	form, _ := url.ParseQuery(gotBody)
	if form.Get("token") != "xoxc-test" {
		t.Errorf("token: got %q, want xoxc-test", form.Get("token"))
	}
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

func TestGetMutedChannels_FromAllNotificationsPrefs(t *testing.T) {
	// Real-world: Slack ships mute state inside the JSON-string
	// `all_notifications_prefs` pref under channels[id].muted.
	respBody := `{"ok":true,"prefs":{"all_notifications_prefs":"{\"channels\":{\"C1\":{\"muted\":true},\"C2\":{\"muted\":false},\"C3\":{\"muted\":true}},\"global\":{}}"}}`
	var gotPath, gotAuth, gotTokenInBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		gotTokenInBody = r.PostForm.Get("token")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respBody))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	got, err := c.GetMutedChannels(context.Background())
	if err != nil {
		t.Fatalf("GetMutedChannels: %v", err)
	}
	if !strings.HasSuffix(gotPath, "/users.prefs.get") {
		t.Errorf("path: got %q, want suffix /users.prefs.get", gotPath)
	}
	// Hand-rolled endpoints used to send Authorization: Bearer — an OAuth
	// pattern browsers never use against app.slack.com. Now token rides in
	// the form body (issue #5).
	if gotAuth != "" {
		t.Errorf("auth: got %q, want empty (token belongs in form body)", gotAuth)
	}
	if gotTokenInBody != "xoxc-test" {
		t.Errorf("form body token: got %q, want xoxc-test", gotTokenInBody)
	}
	gotSet := map[string]bool{}
	for _, id := range got {
		gotSet[id] = true
	}
	if !gotSet["C1"] || !gotSet["C3"] {
		t.Errorf("expected C1 and C3 muted, got %v", got)
	}
	if gotSet["C2"] {
		t.Errorf("C2 should not be muted (muted=false), got %v", got)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2 (got %v)", len(got), got)
	}
}

func TestGetMutedChannels_LegacyMutedChannelsField(t *testing.T) {
	// Back-compat: if Slack ever ships the flat muted_channels pref
	// again, we still pick it up.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"prefs":{"muted_channels":"C1,C2"}}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	got, err := c.GetMutedChannels(context.Background())
	if err != nil {
		t.Fatalf("GetMutedChannels: %v", err)
	}
	gotSet := map[string]bool{}
	for _, id := range got {
		gotSet[id] = true
	}
	if !gotSet["C1"] || !gotSet["C2"] || len(got) != 2 {
		t.Errorf("expected {C1,C2}, got %v", got)
	}
}

func TestGetMutedChannels_BothPrefsMerged(t *testing.T) {
	// Defensive: if a workspace returns both, we take the union.
	respBody := `{"ok":true,"prefs":{"muted_channels":"C1","all_notifications_prefs":"{\"channels\":{\"C2\":{\"muted\":true}}}"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respBody))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	got, err := c.GetMutedChannels(context.Background())
	if err != nil {
		t.Fatalf("GetMutedChannels: %v", err)
	}
	gotSet := map[string]bool{}
	for _, id := range got {
		gotSet[id] = true
	}
	if !gotSet["C1"] || !gotSet["C2"] || len(got) != 2 {
		t.Errorf("expected union {C1,C2}, got %v", got)
	}
}

func TestGetMutedChannels_EmptyChannelsObject(t *testing.T) {
	// Workspaces with no muted channels return channels:{} — should
	// produce an empty (non-nil) slice.
	respBody := `{"ok":true,"prefs":{"all_notifications_prefs":"{\"channels\":{},\"global\":{}}"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respBody))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	got, err := c.GetMutedChannels(context.Background())
	if err != nil {
		t.Fatalf("GetMutedChannels: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0 (got %v)", len(got), got)
	}
}

func TestParseMutedFromAllNotificationsPrefs_BadJSON(t *testing.T) {
	// Garbage in => empty out, no panic. Mute is best-effort.
	if got := ParseMutedFromAllNotificationsPrefs("not json"); len(got) != 0 {
		t.Errorf("expected empty slice for invalid JSON, got %v", got)
	}
	if got := ParseMutedFromAllNotificationsPrefs(""); len(got) != 0 {
		t.Errorf("expected empty slice for empty string, got %v", got)
	}
}

func TestGetMutedChannels_ApiError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	_, err := c.GetMutedChannels(context.Background())
	if err == nil {
		t.Fatal("expected error for ok=false response")
	}
	if !strings.Contains(err.Error(), "invalid_auth") {
		t.Errorf("error = %v, want it to mention invalid_auth", err)
	}
}

func TestSendReply_BuildsRichTextBlock(t *testing.T) {
	api, getForm, closeFn := newTestSlackAPI(t, `{"ok":true,"ts":"1700000000.000200","channel":"C1"}`)
	defer closeFn()
	c := &Client{api: api}

	ts, sentMrkdwn, err := c.SendReply(context.Background(), "C1", "1700000000.000100", "see [docs](https://x.com)")
	if err != nil {
		t.Fatalf("SendReply: %v", err)
	}
	if ts != "1700000000.000200" {
		t.Errorf("ts = %q", ts)
	}
	if sentMrkdwn != "see <https://x.com|docs>" {
		t.Errorf("sentMrkdwn = %q", sentMrkdwn)
	}
	form := getForm()
	if form.Get("text") != "see <https://x.com|docs>" {
		t.Errorf("wire text = %q", form.Get("text"))
	}
	if form.Get("blocks") == "" {
		t.Error("blocks form value empty; expected rich_text block")
	}
	if form.Get("thread_ts") != "1700000000.000100" {
		t.Errorf("thread_ts = %q, want parent ts", form.Get("thread_ts"))
	}
}

func TestEditMessage_BuildsRichTextBlock(t *testing.T) {
	api, getForm, closeFn := newTestSlackAPI(t, `{"ok":true,"channel":"C1","ts":"1700000000.000100","text":"*new* text"}`)
	defer closeFn()
	c := &Client{api: api}

	sentMrkdwn, err := c.EditMessage(context.Background(), "C1", "1700000000.000100", "**new** text")
	if err != nil {
		t.Fatalf("EditMessage: %v", err)
	}
	if sentMrkdwn != "*new* text" {
		t.Errorf("sentMrkdwn = %q", sentMrkdwn)
	}
	form := getForm()
	if form.Get("text") != "*new* text" {
		t.Errorf("wire text = %q", form.Get("text"))
	}
	if form.Get("ts") != "1700000000.000100" {
		t.Errorf("ts = %q", form.Get("ts"))
	}
	if form.Get("channel") != "C1" {
		t.Errorf("channel = %q", form.Get("channel"))
	}
	if form.Get("blocks") == "" {
		t.Error("blocks form value empty; expected rich_text block")
	}
}

func TestGetHistorySince_PaginatesUntilExhausted(t *testing.T) {
	page1 := []slack.Message{
		{Msg: slack.Msg{Timestamp: "100.000000", User: "U1", Text: "a"}},
		{Msg: slack.Msg{Timestamp: "200.000000", User: "U1", Text: "b"}},
	}
	page2 := []slack.Message{
		{Msg: slack.Msg{Timestamp: "300.000000", User: "U1", Text: "c"}},
	}
	var calls []slack.GetConversationHistoryParameters
	resp1 := &slack.GetConversationHistoryResponse{Messages: page1, HasMore: true}
	resp1.ResponseMetaData.NextCursor = "cur1"
	resp2 := &slack.GetConversationHistoryResponse{Messages: page2, HasMore: false}
	responses := []*slack.GetConversationHistoryResponse{resp1, resp2}
	mock := &mockSlackAPI{
		getConversationHistoryFn: func(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
			calls = append(calls, *params)
			if len(responses) == 0 {
				t.Fatalf("unexpected extra call to GetConversationHistory: %+v", params)
			}
			resp := responses[0]
			responses = responses[1:]
			return resp, nil
		},
	}
	c := &Client{api: mock}

	res, err := c.GetHistorySince(context.Background(), "C1", "50.000000", 500)
	if err != nil {
		t.Fatalf("GetHistorySince: %v", err)
	}
	if len(res.Messages) != 3 {
		t.Errorf("got %d messages, want 3", len(res.Messages))
	}
	if res.Capped {
		t.Errorf("Capped = true; expected false (exhausted all pages)")
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 history calls, got %d", len(calls))
	}
	if calls[0].Oldest != "50.000000" || calls[0].Cursor != "" {
		t.Errorf("call[0] = %+v, want Oldest=50.000000, Cursor=''", calls[0])
	}
	if calls[1].Cursor != "cur1" {
		t.Errorf("call[1].Cursor = %q, want %q", calls[1].Cursor, "cur1")
	}
}

func TestGetHistorySince_RespectsHardCap(t *testing.T) {
	// 3 pages of 2 messages each = 6 available; cap=4 should stop early.
	mkPage := func(start int, hasMore bool, cursor string) *slack.GetConversationHistoryResponse {
		r := &slack.GetConversationHistoryResponse{
			Messages: []slack.Message{
				{Msg: slack.Msg{Timestamp: fmt.Sprintf("%d.000000", start), User: "U1"}},
				{Msg: slack.Msg{Timestamp: fmt.Sprintf("%d.000000", start+1), User: "U1"}},
			},
			HasMore: hasMore,
		}
		r.ResponseMetaData.NextCursor = cursor
		return r
	}
	responses := []*slack.GetConversationHistoryResponse{
		mkPage(100, true, "c1"),
		mkPage(200, true, "c2"),
		mkPage(300, false, ""),
	}
	var calls int
	mock := &mockSlackAPI{
		getConversationHistoryFn: func(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
			if calls >= len(responses) {
				t.Fatalf("unexpected call #%d (cap should have stopped earlier)", calls+1)
			}
			resp := responses[calls]
			calls++
			return resp, nil
		},
	}
	c := &Client{api: mock}

	res, err := c.GetHistorySince(context.Background(), "C1", "0", 4)
	if err != nil {
		t.Fatalf("GetHistorySince: %v", err)
	}
	if len(res.Messages) != 4 {
		t.Errorf("got %d, want 4 (cap)", len(res.Messages))
	}
	if !res.Capped {
		t.Errorf("Capped = false; expected true (hit maxTotal)")
	}
	if calls != 2 {
		t.Errorf("expected 2 calls before cap stop, got %d", calls)
	}
}

func TestGetHistorySince_EmptyOldestFetchesLatestPageOnly(t *testing.T) {
	var calls []slack.GetConversationHistoryParameters
	mock := &mockSlackAPI{
		getConversationHistoryFn: func(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
			calls = append(calls, *params)
			r := &slack.GetConversationHistoryResponse{
				Messages: []slack.Message{
					{Msg: slack.Msg{Timestamp: "100.000000", User: "U1"}},
				},
				HasMore: true, // server says more pages exist...
			}
			r.ResponseMetaData.NextCursor = "shouldnotbeused"
			return r, nil
		},
	}
	c := &Client{api: mock}

	// When oldest is empty (no prior sync), fetch latest page only — do NOT paginate.
	res, err := c.GetHistorySince(context.Background(), "C1", "", 500)
	if err != nil {
		t.Fatalf("GetHistorySince: %v", err)
	}
	if len(res.Messages) != 1 {
		t.Errorf("got %d msgs, want 1", len(res.Messages))
	}
	// HasMore was true in the response, so Capped must propagate that.
	if !res.Capped {
		t.Errorf("Capped = false; expected true (response had HasMore)")
	}
	if len(calls) != 1 {
		t.Errorf("expected 1 call (no pagination when oldest is empty), got %d", len(calls))
	}
	if calls[0].Oldest != "" {
		t.Errorf("call.Oldest = %q, want empty", calls[0].Oldest)
	}
}

func TestGetHistorySince_NoMessages(t *testing.T) {
	mock := &mockSlackAPI{
		getConversationHistoryFn: func(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
			return &slack.GetConversationHistoryResponse{Messages: nil, HasMore: false}, nil
		},
	}
	c := &Client{api: mock}

	res, err := c.GetHistorySince(context.Background(), "C1", "100.000000", 500)
	if err != nil {
		t.Fatalf("GetHistorySince: %v", err)
	}
	if len(res.Messages) != 0 {
		t.Errorf("expected empty, got %+v", res.Messages)
	}
	if res.Capped {
		t.Errorf("Capped = true; expected false (HasMore was false)")
	}
}

func TestListThreadSubscriptions_PaginatesUntilExhausted(t *testing.T) {
	var calls int
	var capturedCurrentTS []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = r.ParseForm()
		capturedCurrentTS = append(capturedCurrentTS, r.PostForm.Get("current_ts"))
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{
				"ok": true,
				"threads": [
					{"root_msg": {"channel": "C1", "ts": "1700000000.000100", "thread_ts": "1700000000.000100", "last_read": "1700000000.000200", "subscribed": true, "user": "U2", "text": "p1"}},
					{"root_msg": {"channel": "C2", "ts": "1700000001.000100", "thread_ts": "1700000001.000100", "last_read": "1700000001.000200", "subscribed": true, "user": "U3", "text": "p2"}}
				],
				"has_more": true,
				"max_ts": "1700000001.000100"
			}`))
		case 2:
			_, _ = w.Write([]byte(`{
				"ok": true,
				"threads": [
					{"root_msg": {"channel": "C3", "ts": "1700000002.000100", "thread_ts": "1700000002.000100", "last_read": "1700000002.000200", "subscribed": true, "user": "U4", "text": "p3"}}
				],
				"has_more": false,
				"max_ts": "1700000002.000100"
			}`))
		default:
			t.Fatalf("unexpected call %d", calls)
		}
	}))
	defer srv.Close()

	c := &Client{token: "xoxc-test", cookie: "d-cookie", apiBaseURL: srv.URL + "/api/"}
	got, err := c.ListThreadSubscriptions(context.Background())
	if err != nil {
		t.Fatalf("ListThreadSubscriptions: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
	if len(got) != 3 {
		t.Errorf("len(got) = %d, want 3", len(got))
	}
	if got[0].Subscription.ChannelID != "C1" || got[2].Subscription.ChannelID != "C3" {
		t.Errorf("got = %+v", got)
	}
	if got[0].RootMessage.Text != "p1" {
		t.Errorf("expected root_msg.text to populate RootMessage.Text, got %+v", got[0].RootMessage)
	}
	if capturedCurrentTS[0] != "" || capturedCurrentTS[1] != "1700000001.000100" {
		t.Errorf("current_ts = %v, want [\"\", \"1700000001.000100\"]", capturedCurrentTS)
	}
}

func TestListThreadSubscriptions_EmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": true, "threads": [], "has_more": false, "max_ts": ""}`))
	}))
	defer srv.Close()

	c := &Client{token: "xoxc-test", cookie: "d-cookie", apiBaseURL: srv.URL + "/api/"}
	got, err := c.ListThreadSubscriptions(context.Background())
	if err != nil {
		t.Fatalf("ListThreadSubscriptions: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len(got) = %d, want 0", len(got))
	}
}

func TestListThreadSubscriptions_RespectsHardCap(t *testing.T) {
	// Server returns 100 subs per page with has_more=true forever.
	// The client should stop after the hard cap (1000) and never make
	// an 11th call.
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		var b []byte
		b = append(b, []byte(`{"ok": true, "threads": [`)...)
		for i := 0; i < 100; i++ {
			if i > 0 {
				b = append(b, ',')
			}
			b = append(b, []byte(`{"root_msg": {"channel": "C", "ts": "1.0", "thread_ts": "1.0", "last_read": "1.0", "subscribed": true, "user": "U"}}`)...)
		}
		b = append(b, []byte(`], "has_more": true, "max_ts": "1.0"}`)...)
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	c := &Client{token: "xoxc-test", cookie: "d-cookie", apiBaseURL: srv.URL + "/api/"}
	got, err := c.ListThreadSubscriptions(context.Background())
	if err != nil {
		t.Fatalf("ListThreadSubscriptions: %v", err)
	}
	if len(got) != 1000 {
		t.Errorf("len(got) = %d, want 1000 (hard cap)", len(got))
	}
	if calls != 10 {
		t.Errorf("calls = %d, want 10 (1000 / 100 per page)", calls)
	}
}

func TestListThreadSubscriptions_FiltersUnsubscribedItems(t *testing.T) {
	// Defensively drop any items the server marks as subscribed=false.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ok": true,
			"threads": [
				{"root_msg": {"channel": "C1", "ts": "1.0", "thread_ts": "1.0", "last_read": "1.0", "subscribed": true}},
				{"root_msg": {"channel": "C2", "ts": "2.0", "thread_ts": "2.0", "last_read": "2.0", "subscribed": false}}
			],
			"has_more": false,
			"max_ts": ""
		}`))
	}))
	defer srv.Close()

	c := &Client{token: "xoxc-test", cookie: "d-cookie", apiBaseURL: srv.URL + "/api/"}
	got, err := c.ListThreadSubscriptions(context.Background())
	if err != nil {
		t.Fatalf("ListThreadSubscriptions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1 (unsubscribed filtered out)", len(got))
	}
	if got[0].Subscription.ChannelID != "C1" {
		t.Errorf("wrong item survived filter: %+v", got[0])
	}
}

func TestListThreadSubscriptions_ReturnsErrorOnNotOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": false, "error": "invalid_auth"}`))
	}))
	defer srv.Close()

	c := &Client{token: "xoxc-test", cookie: "d-cookie", apiBaseURL: srv.URL + "/api/"}
	_, err := c.ListThreadSubscriptions(context.Background())
	if err == nil {
		t.Fatalf("expected error on ok=false, got nil")
	}
	if !strings.Contains(err.Error(), "invalid_auth") {
		t.Errorf("error = %q, want contains \"invalid_auth\"", err.Error())
	}
}

func TestNewClient_UsesBrowserTransport(t *testing.T) {
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true,"team_id":"T1","user_id":"U1","url":"https://example.slack.com/"}`)
	}))
	defer srv.Close()

	// Build a Client via NewClient, then point its slack-go api at our
	// httptest server. Use slack.OptionAPIURL on a fresh slack-go client
	// that shares the SAME http.Client we built — so the BrowserTransport
	// is exercised.
	c := NewClient("xoxc-test", "test-cookie")
	c.api = slack.New(
		c.token,
		slack.OptionHTTPClient(c.httpClient),
		slack.OptionAPIURL(srv.URL+"/"),
	)

	// BrowserTransport only injects headers on *.slack.com hosts. httptest
	// binds to 127.0.0.1 so the headers won't appear on the wire here —
	// header-injection coverage lives in internal/slackhttp/transport_test.go.
	// This test only verifies the wiring contract.
	if _, ok := c.httpClient.Transport.(*slackhttp.BrowserTransport); !ok {
		t.Fatalf("c.httpClient.Transport = %T; want *slackhttp.BrowserTransport", c.httpClient.Transport)
	}

	// Sanity check: a real call goes through and reaches the server.
	if _, err := c.api.AuthTest(); err != nil {
		t.Fatalf("AuthTest: %v", err)
	}
	if gotHeaders == nil {
		t.Fatal("server never received a request")
	}
}

// TestStartWebSocket_SendsChromeUpgradeHeaders checks what actually
// reaches the wire on an upgrade, as opposed to
// TestWSUpgradeHeaders_OmitsXHROnlyHeaders, which only inspects the map.
// The name deliberately says "ChromeUpgrade" rather than "Browser": the
// WS handshake carries a different, smaller set than the browser headers
// BrowserTransport puts on ordinary HTTP requests.
//
// This drives StartWebSocket end-to-end rather than dialing with
// wsUpgradeHeaders() directly, so that severing the two — passing nil
// headers to dialer.Dial, say — fails here instead of silently turning
// wsUpgradeHeaders into dead code that the rest of the suite still
// happily exercises.
func TestStartWebSocket_SendsChromeUpgradeHeaders(t *testing.T) {
	// Spin up an httptest server that completes the WS upgrade and
	// reports the upgrade request's headers back over a channel (rather
	// than a shared var, which the race detector would flag).
	gotCh := make(chan http.Header, 1)
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			select {
			case gotCh <- r.Header.Clone():
			default:
			}
			return true
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade failed: %v", err)
			return
		}
		conn.Close()
	}))
	defer srv.Close()

	// wsBaseURL is the seam: in production it is defaultWSBaseURL, here
	// it points at the test server. Everything else about the URL —
	// every query parameter — is StartWebSocket's own, and is part of
	// the protocol fingerprint we are impersonating.
	c := &Client{
		token:     "xoxc-test",
		cookie:    "d-cookie",
		teamID:    "T12345",
		wsBaseURL: "ws://" + srv.Listener.Addr().String(),
	}
	if err := c.StartWebSocket(&mockEventHandler{}); err != nil {
		t.Fatalf("StartWebSocket: %v", err)
	}
	// The server hangs up immediately; wait for the read loop to notice
	// so the goroutine doesn't outlive the test.
	defer func() { <-c.WsDone() }()

	var gotHeaders http.Header
	select {
	case gotHeaders = <-gotCh:
	case <-time.After(5 * time.Second):
		t.Fatal("server never received an upgrade request")
	}

	// The five headers real Chrome sends on a WS upgrade.
	wantPresent := map[string]string{
		"User-Agent":      slackhttp.UserAgent(),
		"Accept-Language": "en-US,en;q=0.9",
		"Cache-Control":   "no-cache",
		"Pragma":          "no-cache",
		"Origin":          "https://app.slack.com",
	}
	for k, want := range wantPresent {
		if got := gotHeaders.Get(k); got != want {
			t.Errorf("upgrade header %s = %q; want %q", k, got, want)
		}
	}

	// Real Chrome sends none of these on a WS upgrade — verified against
	// the status-101 upgrades in the 2026-07-30 captures. An earlier
	// version of slk sent Sec-Fetch-Dest: websocket on the mistaken
	// belief that browsers do; that made slk separable.
	for _, k := range []string{
		"Accept", "Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-Dest",
		"Sec-Ch-Ua", "Sec-Ch-Ua-Mobile", "Sec-Ch-Ua-Platform", "Priority", "Referer",
	} {
		if got := gotHeaders.Get(k); got != "" {
			t.Errorf("upgrade header %s = %q; want absent (real Chrome omits it)", k, got)
		}
	}
}

// TestStartWebSocket_StartArgsMatchTheCapture pins the start_args
// query param against the official client's, from the 2026-08-02
// coldboot capture:
//
//	start_args = ?agent=client&org_wide_aware=true&agent_version=1785403654
//	             &eac_cache_ts=true&cache_ts=0&name_tagging=true
//	             &only_self_subteams=true&connect_only=true&ms_latest=true
//
// slk sent only agent/connect_only/ms_latest, and at some point
// user_typing frames stopped arriving — the visible symptom was typing
// indicators silently disappearing while everything else on the socket
// kept working. Which of the missing keys gates typing delivery is not
// documented anywhere; the fix is the project's standing rule — match
// the capture, don't invent — applied in full.
func TestStartWebSocket_StartArgsMatchTheCapture(t *testing.T) {
	gotCh := make(chan url.Values, 1)
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			select {
			case gotCh <- r.URL.Query():
			default:
			}
			return true
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade failed: %v", err)
			return
		}
		conn.Close()
	}))
	defer srv.Close()

	env := slackhttp.NewEnvelope()
	env.SetVersionTS("1785403654")
	c := &Client{
		token:     "xoxc-test",
		cookie:    "d-cookie",
		teamID:    "T12345",
		wsBaseURL: "ws://" + srv.Listener.Addr().String(),
		envelope:  env,
	}
	if err := c.StartWebSocket(&mockEventHandler{}); err != nil {
		t.Fatalf("StartWebSocket: %v", err)
	}
	defer func() { <-c.WsDone() }()

	var q url.Values
	select {
	case q = <-gotCh:
	case <-time.After(5 * time.Second):
		t.Fatal("server never received an upgrade request")
	}

	raw := q.Get("start_args")
	if raw == "" {
		t.Fatalf("no start_args in dialed URL: %v", q)
	}
	args, err := url.ParseQuery(strings.TrimPrefix(raw, "?"))
	if err != nil {
		t.Fatalf("start_args is not a query string: %q (%v)", raw, err)
	}
	want := map[string]string{
		"agent":              "client",
		"org_wide_aware":     "true",
		"agent_version":      "1785403654", // the envelope's version_ts
		"eac_cache_ts":       "true",
		"cache_ts":           "0",
		"name_tagging":       "true",
		"only_self_subteams": "true",
		"connect_only":       "true",
		"ms_latest":          "true",
	}
	for k, v := range want {
		if got := args.Get(k); got != v {
			t.Errorf("start_args[%s] = %q; want %q (from the capture)", k, got, v)
		}
	}
}

func TestGetUsersInConversationPaginates(t *testing.T) {
	type page struct {
		users []string
		next  string
	}
	pages := map[string]page{
		"":        {users: []string{"U1", "U2"}, next: "cursor2"},
		"cursor2": {users: []string{"U3"}, next: ""},
	}
	var seenChannel string
	var seenLimits []int
	mock := &mockSlackAPI{
		getUsersInConversationContextFn: func(ctx context.Context, params *slack.GetUsersInConversationParameters) ([]string, string, error) {
			seenChannel = params.ChannelID
			seenLimits = append(seenLimits, params.Limit)
			p, ok := pages[params.Cursor]
			if !ok {
				t.Fatalf("unexpected cursor %q", params.Cursor)
			}
			return p.users, p.next, nil
		},
	}
	c := &Client{api: mock}

	got, err := c.GetUsersInConversation(context.Background(), "C1")
	if err != nil {
		t.Fatalf("GetUsersInConversation: %v", err)
	}
	want := []string{"U1", "U2", "U3"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if seenChannel != "C1" {
		t.Errorf("ChannelID = %q, want %q", seenChannel, "C1")
	}
	if len(seenLimits) != 2 {
		t.Fatalf("expected 2 API calls, got %d", len(seenLimits))
	}
	for i, lim := range seenLimits {
		if lim != 1000 {
			t.Errorf("call[%d].Limit = %d, want 1000", i, lim)
		}
	}
}

func TestGetUsersInConversationPropagatesError(t *testing.T) {
	wantErr := errors.New("channel_not_found")
	mock := &mockSlackAPI{
		getUsersInConversationContextFn: func(ctx context.Context, params *slack.GetUsersInConversationParameters) ([]string, string, error) {
			return nil, "", wantErr
		},
	}
	c := &Client{api: mock}

	_, err := c.GetUsersInConversation(context.Background(), "C1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped wantErr, got %v", err)
	}
	if !strings.Contains(err.Error(), "listing conversation members") {
		t.Errorf("expected 'listing conversation members' prefix, got %q", err.Error())
	}
}

func TestGetUsersInConversationRetriesOnRateLimit(t *testing.T) {
	var calls int
	var seenCursors []string
	mock := &mockSlackAPI{
		getUsersInConversationContextFn: func(ctx context.Context, params *slack.GetUsersInConversationParameters) ([]string, string, error) {
			calls++
			seenCursors = append(seenCursors, params.Cursor)
			if calls == 1 {
				return nil, "", &slack.RateLimitedError{RetryAfter: 10 * time.Millisecond}
			}
			return []string{"U1", "U2"}, "", nil
		},
	}
	c := &Client{api: mock}

	got, err := c.GetUsersInConversation(context.Background(), "C1")
	if err != nil {
		t.Fatalf("GetUsersInConversation: %v", err)
	}
	want := []string{"U1", "U2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if calls != 2 {
		t.Fatalf("expected 2 API calls (1 rate-limited + 1 retry), got %d", calls)
	}
	// On rate-limit, cursor must NOT advance — the retry hits the same page.
	if seenCursors[0] != "" || seenCursors[1] != "" {
		t.Errorf("expected both calls with empty cursor (same page retry), got %v", seenCursors)
	}
}

func (m *mockSlackAPI) OpenConversationContext(ctx context.Context, params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error) {
	if m.openConversationContextFn != nil {
		return m.openConversationContextFn(ctx, params)
	}
	return nil, false, false, nil
}

func TestOpenConversation_SingleUserReturnsIMChannelID(t *testing.T) {
	mock := &mockSlackAPI{
		openConversationContextFn: func(ctx context.Context, params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error) {
			if len(params.Users) != 1 || params.Users[0] != "U123" {
				t.Errorf("expected Users=[U123], got %v", params.Users)
			}
			if !params.ReturnIM {
				t.Error("expected ReturnIM=true")
			}
			return &slack.Channel{
				GroupConversation: slack.GroupConversation{
					Conversation: slack.Conversation{ID: "D456"},
				},
			}, false, false, nil
		},
	}
	c := &Client{api: mock}

	channelID, alreadyOpen, err := c.OpenConversation(context.Background(), []string{"U123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if channelID != "D456" {
		t.Errorf("expected channelID=D456, got %q", channelID)
	}
	if alreadyOpen {
		t.Error("expected alreadyOpen=false")
	}
}

func TestOpenConversation_MultipleUsersReturnsMPIMChannelID(t *testing.T) {
	mock := &mockSlackAPI{
		openConversationContextFn: func(ctx context.Context, params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error) {
			if len(params.Users) != 3 {
				t.Errorf("expected 3 users, got %d", len(params.Users))
			}
			return &slack.Channel{
				GroupConversation: slack.GroupConversation{
					Conversation: slack.Conversation{ID: "G789"},
				},
			}, false, false, nil
		},
	}
	c := &Client{api: mock}

	channelID, _, err := c.OpenConversation(context.Background(), []string{"U1", "U2", "U3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if channelID != "G789" {
		t.Errorf("expected channelID=G789, got %q", channelID)
	}
}

func TestOpenConversation_EmptyUserIDsReturnsErrorWithoutAPICall(t *testing.T) {
	called := false
	mock := &mockSlackAPI{
		openConversationContextFn: func(ctx context.Context, params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error) {
			called = true
			return nil, false, false, nil
		},
	}
	c := &Client{api: mock}

	_, _, err := c.OpenConversation(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for empty userIDs")
	}
	if called {
		t.Error("API should not have been called")
	}
}

func TestOpenConversation_TooManyUserIDsReturnsErrorWithoutAPICall(t *testing.T) {
	called := false
	mock := &mockSlackAPI{
		openConversationContextFn: func(ctx context.Context, params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error) {
			called = true
			return nil, false, false, nil
		},
	}
	c := &Client{api: mock}

	nine := []string{"U1", "U2", "U3", "U4", "U5", "U6", "U7", "U8", "U9"}
	_, _, err := c.OpenConversation(context.Background(), nine)
	if err == nil {
		t.Fatal("expected error for 9 userIDs")
	}
	if called {
		t.Error("API should not have been called")
	}
}

func TestOpenConversation_APIErrorIsWrapped(t *testing.T) {
	mock := &mockSlackAPI{
		openConversationContextFn: func(ctx context.Context, params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error) {
			return nil, false, false, fmt.Errorf("rate_limited")
		},
	}
	c := &Client{api: mock}

	_, _, err := c.OpenConversation(context.Background(), []string{"U1"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "opening conversation") {
		t.Errorf("expected error to be wrapped with opening conversation prefix, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "rate_limited") {
		t.Errorf("expected error to contain underlying message, got %q", err.Error())
	}
}

func TestOpenConversation_AlreadyOpenFlagPropagates(t *testing.T) {
	mock := &mockSlackAPI{
		openConversationContextFn: func(ctx context.Context, params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error) {
			return &slack.Channel{
				GroupConversation: slack.GroupConversation{
					Conversation: slack.Conversation{ID: "D1"},
				},
			}, false, true, nil
		},
	}
	c := &Client{api: mock}

	_, alreadyOpen, err := c.OpenConversation(context.Background(), []string{"U1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !alreadyOpen {
		t.Error("expected alreadyOpen=true")
	}
}

func TestSearchMessages_PassesQueryVerbatim(t *testing.T) {
	var gotQuery string
	var gotParams slack.SearchParameters
	mock := &mockSlackAPI{
		searchMessagesFn: func(ctx context.Context, query string, params slack.SearchParameters) (*slack.SearchMessages, error) {
			gotQuery = query
			gotParams = params
			return &slack.SearchMessages{Total: 2}, nil
		},
	}
	c := &Client{api: mock}

	res, err := c.SearchMessages(context.Background(), "from:@grant in:#general deploy", 50)
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "from:@grant in:#general deploy" {
		t.Errorf("query mangled: %q", gotQuery)
	}
	if gotParams.Count != 50 {
		t.Errorf("count = %d, want 50", gotParams.Count)
	}
	if res.Total != 2 {
		t.Errorf("total = %d", res.Total)
	}
}

func TestSearchMessages_WrapsError(t *testing.T) {
	sentinel := errors.New("ratelimited")
	mock := &mockSlackAPI{
		searchMessagesFn: func(ctx context.Context, query string, params slack.SearchParameters) (*slack.SearchMessages, error) {
			return nil, sentinel
		},
	}
	c := &Client{api: mock}
	_, err := c.SearchMessages(context.Background(), "x", 50)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error chain broken: %v does not wrap sentinel", err)
	}
}

func TestSearchMessages_NonPositiveCountKeepsDefault(t *testing.T) {
	var gotParams slack.SearchParameters
	mock := &mockSlackAPI{
		searchMessagesFn: func(ctx context.Context, query string, params slack.SearchParameters) (*slack.SearchMessages, error) {
			gotParams = params
			return &slack.SearchMessages{}, nil
		},
	}
	c := &Client{api: mock}

	if _, err := c.SearchMessages(context.Background(), "x", 0); err != nil {
		t.Fatal(err)
	}
	want := slack.NewSearchParameters().Count
	if gotParams.Count != want {
		t.Errorf("count = %d, want slack-go default %d", gotParams.Count, want)
	}
}

func TestGetHistoryAround_FetchesBothDirections(t *testing.T) {
	var calls []*slack.GetConversationHistoryParameters
	mock := &mockSlackAPI{
		getConversationHistoryFn: func(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
			calls = append(calls, params)
			if params.Latest != "" {
				// older-or-equal page, newest first
				return &slack.GetConversationHistoryResponse{Messages: []slack.Message{
					{Msg: slack.Msg{Timestamp: "1700000005.000000"}},
					{Msg: slack.Msg{Timestamp: "1700000004.000000"}},
				}}, nil
			}
			// newer page, newest first
			return &slack.GetConversationHistoryResponse{Messages: []slack.Message{
				{Msg: slack.Msg{Timestamp: "1700000007.000000"}},
				{Msg: slack.Msg{Timestamp: "1700000006.000000"}},
			}}, nil
		},
	}
	c := &Client{api: mock}

	got, err := c.GetHistoryAround(context.Background(), "C1", "1700000005.000000", 25)
	if err != nil {
		t.Fatal(err)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 history calls, got %d", len(calls))
	}
	older, newer := calls[0], calls[1]
	if older.Latest != "1700000005.000000" || !older.Inclusive || older.Limit != 25 || older.ChannelID != "C1" {
		t.Errorf("older call params: %+v", older)
	}
	if newer.Oldest != "1700000005.000000" || newer.Inclusive || newer.Limit != 25 {
		t.Errorf("newer call params: %+v", newer)
	}

	// Combined newest-first: newer page then older page.
	want := []string{"1700000007.000000", "1700000006.000000", "1700000005.000000", "1700000004.000000"}
	if len(got) != len(want) {
		t.Fatalf("got %d messages, want %d", len(got), len(want))
	}
	for i, ts := range want {
		if got[i].Timestamp != ts {
			t.Errorf("got[%d].Timestamp = %s, want %s", i, got[i].Timestamp, ts)
		}
	}
}

func TestGetHistoryAround_DropsIncompleteNewerPage(t *testing.T) {
	mock := &mockSlackAPI{
		getConversationHistoryFn: func(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
			if params.Latest != "" {
				return &slack.GetConversationHistoryResponse{Messages: []slack.Message{
					{Msg: slack.Msg{Timestamp: "1700000005.000000"}},
					{Msg: slack.Msg{Timestamp: "1700000004.000000"}},
				}}, nil
			}
			// Oldest-anchored call: more than limit messages exist after
			// ts, so this page holds the channel's most recent messages,
			// NOT those adjacent to ts — including it would leave a gap.
			return &slack.GetConversationHistoryResponse{
				HasMore: true,
				Messages: []slack.Message{
					{Msg: slack.Msg{Timestamp: "1700000099.000000"}},
					{Msg: slack.Msg{Timestamp: "1700000098.000000"}},
				},
			}, nil
		},
	}
	c := &Client{api: mock}

	got, err := c.GetHistoryAround(context.Background(), "C1", "1700000005.000000", 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1700000005.000000", "1700000004.000000"}
	if len(got) != len(want) {
		t.Fatalf("got %d messages, want %d (newer page should be dropped)", len(got), len(want))
	}
	for i, ts := range want {
		if got[i].Timestamp != ts {
			t.Errorf("got[%d].Timestamp = %s, want %s", i, got[i].Timestamp, ts)
		}
	}
}

func TestGetHistoryAround_WrapsOlderError(t *testing.T) {
	sentinel := errors.New("channel_not_found")
	mock := &mockSlackAPI{
		getConversationHistoryFn: func(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
			return nil, sentinel
		},
	}
	c := &Client{api: mock}

	_, err := c.GetHistoryAround(context.Background(), "C1", "1700000005.000000", 25)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error chain broken: %v does not wrap sentinel", err)
	}
}

func TestGetHistoryAround_WrapsNewerError(t *testing.T) {
	sentinel := errors.New("ratelimited")
	mock := &mockSlackAPI{
		getConversationHistoryFn: func(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
			if params.Latest != "" {
				return &slack.GetConversationHistoryResponse{}, nil
			}
			return nil, sentinel
		},
	}
	c := &Client{api: mock}

	_, err := c.GetHistoryAround(context.Background(), "C1", "1700000005.000000", 25)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error chain broken: %v does not wrap sentinel", err)
	}
}

func TestGetHistoryAround_HonorsCancelledContext(t *testing.T) {
	var calls int
	mock := &mockSlackAPI{
		getConversationHistoryFn: func(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
			calls++
			return &slack.GetConversationHistoryResponse{}, nil
		},
	}
	c := &Client{api: mock}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.GetHistoryAround(ctx, "C1", "1700000005.000000", 25)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error chain broken: %v does not wrap context.Canceled", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call before ctx check aborts, got %d", calls)
	}
}

func TestWSUpgradeHeaders_OmitsXHROnlyHeaders(t *testing.T) {
	h := wsUpgradeHeaders()
	// Real Chrome omits all of these on a WebSocket upgrade. slk
	// previously sent Sec-Fetch-Dest: websocket on the mistaken belief
	// that browsers do; the 2026-07-30 captures show they send no
	// Sec-Fetch-* header at all.
	for _, k := range []string{
		"Accept", "Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-Dest",
		"Sec-Ch-Ua", "Sec-Ch-Ua-Mobile", "Sec-Ch-Ua-Platform", "Priority", "Referer",
	} {
		if got := h.Get(k); got != "" {
			t.Errorf("wsUpgradeHeaders()[%s] = %q; want absent", k, got)
		}
	}
	if h.Get("User-Agent") == "" {
		t.Error("wsUpgradeHeaders() missing User-Agent")
	}
	if h.Get("Origin") != "https://app.slack.com" {
		t.Errorf("wsUpgradeHeaders() Origin = %q; want https://app.slack.com", h.Get("Origin"))
	}
}

// --- Envelope wiring (Task 7) ---------------------------------------

// pointClientAtTestServer makes the client's requests to slack.com land
// on srv without changing the URL the client builds.
//
// BrowserTransport only decorates *.slack.com hosts, so a test that
// pointed apiBaseURL straight at the 127.0.0.1 httptest address would
// silently skip every envelope param and every browser header — and
// would then "pass" only if we weakened it to assert struct fields
// instead of wire bytes. Redirecting inside the dialer instead keeps
// req.URL.Host genuinely "slack.com" (exactly as in production) while
// the bytes go to the test server, so the production decoration path
// runs for real.
//
// It deliberately reaches through c.httpClient.Transport rather than
// replacing it: swapping only BrowserTransport.Inner leaves the Env
// NewClient installed in place, so the test fails if NewClient stops
// wiring it.
func pointClientAtTestServer(t *testing.T, c *Client, srv *httptest.Server) {
	t.Helper()
	bt, ok := c.httpClient.Transport.(*slackhttp.BrowserTransport)
	if !ok {
		t.Fatalf("client transport is %T; want *slackhttp.BrowserTransport", c.httpClient.Transport)
	}
	addr := srv.Listener.Addr().String()
	bt.Inner = &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	c.apiBaseURL = "http://slack.com/api/"
}

func TestClient_EnvelopeExistsAndIsPreBoot(t *testing.T) {
	c := NewClient("xoxc-test", "d-cookie")
	if c.Envelope() == nil {
		t.Fatal("Envelope() is nil; want a non-nil envelope")
	}
	if got := c.Envelope().TeamID(); got != "" {
		t.Errorf("TeamID() = %q before Connect; want empty (pre-boot phase)", got)
	}
}

func TestClient_ConnectSetsEnvelopeTeamID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"url":"https://acme.slack.com/","team":"Acme","user":"grant","team_id":"T04T4TH8W","user_id":"U123"}`))
	}))
	defer srv.Close()

	c := NewClient("xoxc-test", "d-cookie")
	c.apiBaseURL = srv.URL + "/api/"
	c.api = slack.New("xoxc-test",
		slack.OptionHTTPClient(c.httpClient),
		slack.OptionAPIURL(c.apiBaseURL))

	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got := c.Envelope().TeamID(); got != "T04T4TH8W" {
		t.Errorf("Envelope().TeamID() = %q after Connect; want T04T4TH8W", got)
	}
}

func TestClient_RequestsCarryEnvelopeParams(t *testing.T) {
	// End-to-end: a real call through the client's http.Client must
	// arrive with the envelope on it. This is the test that proves the
	// wiring, not just that the field exists.
	var gotQuery url.Values
	var gotUA, gotSecCHUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		gotUA = r.Header.Get("User-Agent")
		gotSecCHUA = r.Header.Get("Sec-Ch-Ua")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := NewClient("xoxc-test", "d-cookie")
	pointClientAtTestServer(t, c, srv)
	c.Envelope().SetTeamID("T04T4TH8W")

	if _, err := c.postForm(context.Background(), "conversations.mark", url.Values{"channel": {"C123"}}); err != nil {
		t.Fatalf("postForm: %v", err)
	}

	for _, k := range []string{"_x_id", "_x_version_ts", "slack_route", "_x_csid", "fp", "_x_num_retries"} {
		if gotQuery.Get(k) == "" {
			t.Errorf("request missing envelope param %s (query: %v)", k, gotQuery)
		}
	}
	if gotQuery.Get("slack_route") != "T04T4TH8W" {
		t.Errorf("slack_route = %q; want T04T4TH8W", gotQuery.Get("slack_route"))
	}
	if gotUA == "" || gotSecCHUA == "" {
		t.Errorf("browser headers missing: UA=%q sec-ch-ua=%q", gotUA, gotSecCHUA)
	}
}

// --- client.shouldReload (Task 8) -----------------------------------

func TestShouldReload_ReturnsBuildVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		if got := r.FormValue("build_version_ts"); got == "" {
			t.Error("build_version_ts not sent in body")
		}
		if got := r.FormValue("team_ids"); got != "T04T4TH8W" {
			t.Errorf("team_ids = %q; want T04T4TH8W", got)
		}
		if got := r.FormValue("_x_reason"); got != "boot" {
			t.Errorf("_x_reason = %q; want boot", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"should_reload":false,"recommended_build_version":1785403654,"build_manifest_last_modified":1785408685}`))
	}))
	defer srv.Close()

	c := NewClient("xoxc-test", "d-cookie")
	pointClientAtTestServer(t, c, srv)
	c.teamID = "T04T4TH8W"
	c.Envelope().SetTeamID("T04T4TH8W")

	ts, err := c.ShouldReload(context.Background())
	if err != nil {
		t.Fatalf("ShouldReload: %v", err)
	}
	if ts != "1785403654" {
		t.Errorf("ShouldReload = %q; want 1785403654", ts)
	}
}

func TestShouldReload_DoesNotMutateEnvelope(t *testing.T) {
	// ShouldReload returns a value; the caller decides whether to store
	// it. A failed lookup must not be able to clobber a good value.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"recommended_build_version":1785403654}`))
	}))
	defer srv.Close()

	c := NewClient("xoxc-test", "d-cookie")
	pointClientAtTestServer(t, c, srv)
	before := c.Envelope().VersionTS()
	if _, err := c.ShouldReload(context.Background()); err != nil {
		t.Fatalf("ShouldReload: %v", err)
	}
	if after := c.Envelope().VersionTS(); after != before {
		t.Errorf("ShouldReload mutated the envelope: %q -> %q", before, after)
	}
}

func TestShouldReload_ErrorCases(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"http error", 500, ``},
		{"api error", 200, `{"ok":false,"error":"invalid_auth"}`},
		// ok:false *with* a version present. Without an explicit ok
		// check this parses fine and looks like a success, so this is
		// the case that actually pins the check — the plain "api error"
		// body above is caught by the missing-version check instead.
		{"api error with version", 200, `{"ok":false,"error":"invalid_auth","recommended_build_version":1785403654}`},
		{"missing version", 200, `{"ok":true}`},
		{"malformed json", 200, `not json`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			c := NewClient("xoxc-test", "d-cookie")
			pointClientAtTestServer(t, c, srv)
			before := c.Envelope().VersionTS()
			if _, err := c.ShouldReload(context.Background()); err == nil {
				t.Error("ShouldReload returned nil error")
			}
			if after := c.Envelope().VersionTS(); after != before {
				t.Errorf("VersionTS changed on failure: %q -> %q", before, after)
			}
		})
	}
}

// postForm must treat a non-2xx as a failure rather than handing the
// caller whatever bytes came back. Slack's Web API answers 200 even for
// {"ok":false}, so a 5xx here is a proxy/edge failure — and it can
// still carry a body that parses cleanly, which is how it used to slip
// through as an apparent success.
func TestPostForm_NonOKStatusIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := NewClient("xoxc-test", "d-cookie")
	pointClientAtTestServer(t, c, srv)

	body, err := c.postForm(context.Background(), "users.prefs.get", nil)
	if err == nil {
		t.Fatalf("postForm returned nil error on HTTP 502 (body: %s)", body)
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("error = %v; want it to mention the 502 status", err)
	}
}

// TestHandRolledEndpoints_RouteThroughSharedClient is the regression
// guard for three endpoints that used to build their own http.Client
// via newCookieHTTPClient instead of reusing c.httpClient:
// client.counts (GetUnreadCounts), users.channelSections.list
// (callChannelSectionsList) and stars.list (GetStarredChannels).
//
// Two things were wrong with that, and this test fails on both:
//
//   - Envelope loss was undetectable. Passing nil where c.envelope
//     belongs stripped every _x_* param from three of slk's
//     highest-traffic calls (client.counts runs on every boot and every
//     reconnect) and no test in the suite noticed.
//   - They were unreachable from the harness. pointClientAtTestServer
//     redirects c.httpClient's transport; a method that ignores
//     c.httpClient dials slack.com for real, so these endpoints made
//     live outbound connections during `go test`.
//
// Driving them through pointClientAtTestServer covers both: a method
// that bypasses c.httpClient never reaches this handler, so its
// response never parses and the subtest fails before the param
// assertions run.
func TestHandRolledEndpoints_RouteThroughSharedClient(t *testing.T) {
	cases := []struct {
		name     string
		respBody string
		call     func(t *testing.T, c *Client)
	}{
		{
			name:     "GetUnreadCounts",
			respBody: `{"ok":true,"channels":[],"mpims":[],"ims":[],"threads":{"has_unreads":false}}`,
			call: func(t *testing.T, c *Client) {
				if _, _, err := c.GetUnreadCounts(); err != nil {
					t.Fatalf("GetUnreadCounts: %v", err)
				}
			},
		},
		{
			name:     "GetChannelSections",
			respBody: `{"ok":true,"channel_sections":[],"count":0,"cursor":""}`,
			call: func(t *testing.T, c *Client) {
				if _, err := c.GetChannelSections(context.Background()); err != nil {
					t.Fatalf("GetChannelSections: %v", err)
				}
			},
		},
		{
			name:     "GetStarredChannels",
			respBody: `{"ok":true,"items":[],"paging":{"count":0,"total":0}}`,
			call: func(t *testing.T, c *Client) {
				if _, err := c.GetStarredChannels(context.Background()); err != nil {
					t.Fatalf("GetStarredChannels: %v", err)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotQuery url.Values
			var gotHost, gotUA string
			var served bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				served = true
				gotQuery = r.URL.Query()
				gotHost = r.Host
				gotUA = r.Header.Get("User-Agent")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.respBody))
			}))
			defer srv.Close()

			c := NewClient("xoxc-test", "d-cookie")
			pointClientAtTestServer(t, c, srv)
			c.Envelope().SetTeamID("T04T4TH8W")

			tc.call(t, c)

			if !served {
				t.Fatal("request never reached the test server; the endpoint bypassed c.httpClient " +
					"and dialed the network for real")
			}
			// req.URL.Host stays "slack.com" (only the dialer is
			// redirected), so BrowserTransport's Slack-host gate runs
			// exactly as it does in production.
			if gotHost != "slack.com" {
				t.Errorf("Host = %q; want slack.com — the request must look identical to production", gotHost)
			}
			for _, k := range []string{"_x_id", "_x_csid", "slack_route", "_x_version_ts", "fp", "_x_num_retries"} {
				if gotQuery.Get(k) == "" {
					t.Errorf("request missing envelope param %s (query: %v)", k, gotQuery)
				}
			}
			if gotQuery.Get("slack_route") != "T04T4TH8W" {
				t.Errorf("slack_route = %q; want T04T4TH8W", gotQuery.Get("slack_route"))
			}
			if gotUA == "" {
				t.Error("request carries no browser User-Agent")
			}
		})
	}
}

// TestAPIHTTPClient_FallbackCarriesEnvelope pins the one remaining
// newCookieHTTPClient call site inside Client. It is reached only by
// Clients constructed directly in tests (NewClient always sets
// httpClient), so no wire-level test exercises it — and without this,
// dropping the envelope there would again be a silent mutation.
func TestAPIHTTPClient_FallbackCarriesEnvelope(t *testing.T) {
	env := slackhttp.NewEnvelope()
	c := &Client{cookie: "d-cookie", envelope: env}

	got := c.apiHTTPClient()
	bt, ok := got.Transport.(*slackhttp.BrowserTransport)
	if !ok {
		t.Fatalf("fallback transport is %T; want *slackhttp.BrowserTransport", got.Transport)
	}
	if bt.Env != env {
		t.Errorf("fallback transport Env = %p; want the client's envelope %p", bt.Env, env)
	}
	if got.Jar == nil {
		t.Error("fallback client has no cookie jar; the d cookie would not be sent")
	}

	// And when httpClient is set, that exact client is returned —
	// never a fresh one, which is what made the three endpoints above
	// unreachable from the test harness.
	shared := &http.Client{}
	c.httpClient = shared
	if c.apiHTTPClient() != shared {
		t.Error("apiHTTPClient() built a new client while c.httpClient was set")
	}
}

// TestPostForm_BodyFieldOrderIsAlphabeticalThenEnvelope pins what
// postForm actually puts on the wire. It is a characterization test:
// the alphabetical lead it asserts is a known, deliberate residual
// divergence, not the shape the official client emits.
//
// postForm builds its body with url.Values.Encode(), which SORTS keys.
// So slk emits `channel=…&include_all_metadata=0&inclusive=0&limit=50&
// token=…` — perfectly alphabetized — and only the four-field _x_*
// envelope the transport appends is in the client's captured order.
//
// It is not fixed here because the fix would be partial and would make
// things worse rather than better. slack-go builds its own bodies with
// the same url.Values.Encode() (misc.go postForm) and is out of reach
// from this package, so ordering postForm's handful of hand-rolled
// endpoints would give slk TWO distinguishable body shapes — hand-
// ordered on ~6 endpoints, sorted on ~50 — where the official client
// has one. The real fix is the deferred multipart conversion, which
// rebuilds every body at the transport chokepoint and can impose one
// order on all of them; see the residual-divergence table in
// docs/superpowers/plans/2026-07-30-grid-parity-phase1-outcomes.md.
//
// The golden fixture test cannot cover this: it hands the transport a
// hand-written literal body, so it only proves appendQuery does not
// reorder its input. This test drives the real production body
// builder.
func TestPostForm_BodyFieldOrderIsAlphabeticalThenEnvelope(t *testing.T) {
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		raw = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := NewClient("xoxc-test", "d-cookie")
	pointClientAtTestServer(t, c, srv)
	c.Envelope().SetTeamID("T04T4TH8W")

	// Deliberately supplied out of alphabetical order, so a body that
	// came back sorted can only have been sorted by postForm.
	if _, err := c.postForm(context.Background(), "conversations.history", url.Values{
		"limit":                {"50"},
		"channel":              {"C123"},
		"inclusive":            {"0"},
		"include_all_metadata": {"0"},
	}); err != nil {
		t.Fatalf("postForm: %v", err)
	}

	const want = "channel=C123&include_all_metadata=0&inclusive=0&limit=50&token=xoxc-test" +
		"&_x_reason=message-pane%2FrequestHistory&_x_mode=online&_x_sonic=true&_x_app_name=client"
	if raw != want {
		t.Errorf("postForm body =\n  %s\nwant\n  %s\n\n"+
			"If the lead is no longer alphabetical, postForm stopped using url.Values.Encode(): "+
			"that is an improvement only if slack-go's bodies were fixed too, otherwise slk now "+
			"emits two different body shapes. Update the residual-divergence table either way.", raw, want)
	}
}
