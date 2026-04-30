package slackclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/slack-go/slack"
)

// SlackAPI defines the subset of the Slack API we use.
// This interface enables mocking in tests.
type SlackAPI interface {
	GetConversations(params *slack.GetConversationsParameters) ([]slack.Channel, string, error)
	GetConversationsForUser(params *slack.GetConversationsForUserParameters) ([]slack.Channel, string, error)
	GetConversationHistory(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error)
	GetConversationReplies(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error)
	GetUsersContext(ctx context.Context, options ...slack.GetUsersOption) ([]slack.User, error)
	GetUserInfo(user string) (*slack.User, error)
	GetEmoji() (map[string]string, error)
	PostMessage(channelID string, options ...slack.MsgOption) (string, string, error)
	UpdateMessage(channelID, timestamp string, options ...slack.MsgOption) (string, string, string, error)
	DeleteMessage(channelID, timestamp string) (string, string, error)
	AddReaction(name string, item slack.ItemRef) error
	RemoveReaction(name string, item slack.ItemRef) error
	GetPermalinkContext(ctx context.Context, params *slack.PermalinkParameters) (string, error)
	AuthTest() (*slack.AuthTestResponse, error)
	JoinConversation(channelID string) (*slack.Channel, string, []string, error)
	SetUserPresenceContext(ctx context.Context, presence string) error
	GetUserPresenceContext(ctx context.Context, user string) (*slack.UserPresence, error)
	SetSnoozeContext(ctx context.Context, minutes int) (*slack.DNDStatus, error)
	EndSnoozeContext(ctx context.Context) (*slack.DNDStatus, error)
	EndDNDContext(ctx context.Context) error
	GetDNDInfoContext(ctx context.Context, user *string, options ...slack.ParamOption) (*slack.DNDStatus, error)
	UploadFileContext(ctx context.Context, params slack.UploadFileParameters) (*slack.FileSummary, error)
}

// Client wraps the slack-go library, providing RTM connectivity
// and a simplified Web API surface for the service layer.
// Uses browser cookie auth (xoxc token + d cookie).
type Client struct {
	api    SlackAPI
	wsConn *websocket.Conn
	wsMu   sync.Mutex
	wsDone chan struct{}
	teamID string
	userID string
	token  string
	cookie string
}

// NewClient creates a new Slack client using browser cookie auth.
// xoxcToken is the xoxc-... token from the browser.
// dCookie is the value of the 'd' cookie from slack.com.
func NewClient(xoxcToken, dCookie string) *Client {
	httpClient := newCookieHTTPClient(dCookie)

	api := slack.New(
		xoxcToken,
		slack.OptionHTTPClient(httpClient),
	)

	return &Client{
		api:    api,
		token:  xoxcToken,
		cookie: dCookie,
	}
}

// newCookieJar creates a cookie jar with the Slack 'd' cookie set.
func newCookieJar(dCookie string) http.CookieJar {
	jar, _ := cookiejar.New(nil)

	slackURL, _ := url.Parse("https://slack.com")
	jar.SetCookies(slackURL, []*http.Cookie{
		{
			Name:   "d",
			Value:  dCookie,
			Domain: ".slack.com",
			Path:   "/",
			Secure: true,
		},
	})

	return jar
}

// newCookieHTTPClient creates an http.Client with the Slack 'd' cookie set.
func newCookieHTTPClient(dCookie string) *http.Client {
	return &http.Client{Jar: newCookieJar(dCookie)}
}

// NewCookieHTTPClient returns an http.Client that authenticates against
// files.slack.com (and other *.slack.com endpoints) using the given 'd'
// cookie value. Used by the inline-image fetcher to download file thumbnails,
// which require browser-session auth.
//
// The returned client has no timeout; callers that need one should wrap it.
func NewCookieHTTPClient(dCookie string) *http.Client {
	return newCookieHTTPClient(dCookie)
}

// fileAuthTransport is an http.RoundTripper that adds an
// `Authorization: Bearer <xoxc-token>` header to requests for
// files.slack.com URLs, selecting the correct token by parsing the
// team ID from the URL path. Slack file URLs embed the team ID as the
// first segment after `/files-tmb/` or `/files-pri/`, e.g.
// `https://files.slack.com/files-tmb/T04T4TH8W-F0123ABCD-.../foo_360.png`.
type fileAuthTransport struct {
	tokensByTeam map[string]string // teamID -> xoxc token
	base         http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (t *fileAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == "files.slack.com" {
		teamID := parseTeamIDFromFilesURL(req.URL.Path)
		if teamID != "" {
			if tok, ok := t.tokensByTeam[teamID]; ok && tok != "" {
				// Clone the request to avoid mutating the caller's copy.
				clone := req.Clone(req.Context())
				clone.Header.Set("Authorization", "Bearer "+tok)
				req = clone
			} else {
				log.Printf("[image-debug] file auth: no token for team %q on URL %s", teamID, req.URL.String())
			}
		} else {
			log.Printf("[image-debug] file auth: could not parse team ID from URL %s", req.URL.String())
		}
	}
	return t.base.RoundTrip(req)
}

// parseTeamIDFromFilesURL returns the team ID embedded in a Slack file URL
// path, or "" if the path doesn't match a recognized pattern.
//
// Recognized patterns:
//
//	/files-tmb/<TEAM>-<FILE>-<HASH>/...
//	/files-pri/<TEAM>-<FILE>/...
//	/files/<TEAM>/<FILE>/...
func parseTeamIDFromFilesURL(path string) string {
	// Strip leading slash, split on slashes, look at the segment after the
	// /files*/ prefix.
	for _, prefix := range []string{"/files-tmb/", "/files-pri/", "/files/"} {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		rest := path[len(prefix):]
		// Take the first path segment.
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			rest = rest[:i]
		}
		// Within that segment, the team ID is the first dash-separated piece
		// (for files-tmb/files-pri it's TEAM-FILE-HASH; for /files/ it IS the segment).
		if prefix == "/files/" {
			return rest
		}
		if i := strings.IndexByte(rest, '-'); i >= 0 {
			return rest[:i]
		}
		return rest
	}
	return ""
}

// NewFileAuthHTTPClient returns an http.Client that authenticates Slack
// file thumbnail / private-download URLs by setting an
// Authorization: Bearer header keyed on the team ID embedded in the URL.
// Other URLs pass through unchanged.
//
// tokensByTeam maps team ID (e.g. "T04T4TH8W") to that workspace's
// xoxc token. The 'd' cookie is also attached as a fallback for
// endpoints that accept it.
//
// The returned client has no timeout; callers that need one should set
// it on the returned client.
func NewFileAuthHTTPClient(tokensByTeam map[string]string, anyDCookie string) *http.Client {
	base := http.DefaultTransport
	rt := &fileAuthTransport{tokensByTeam: tokensByTeam, base: base}
	jar := newCookieJar(anyDCookie)
	return &http.Client{Transport: rt, Jar: jar}
}

// TeamID returns the authenticated workspace's team ID.
// Empty before Connect is called.
func (c *Client) TeamID() string {
	return c.teamID
}

// UserID returns the authenticated user's ID.
// Empty before Connect is called.
func (c *Client) UserID() string {
	return c.userID
}

// WsDone returns a channel that is closed when the WebSocket read loop exits.
func (c *Client) WsDone() <-chan struct{} {
	return c.wsDone
}

// Connect authenticates with Slack and populates the team/user IDs.
func (c *Client) Connect(ctx context.Context) error {
	resp, err := c.api.AuthTest()
	if err != nil {
		return fmt.Errorf("auth test failed: %w", err)
	}
	c.teamID = resp.TeamID
	c.userID = resp.UserID
	return nil
}

// StartWebSocket connects to Slack's internal WebSocket using the xoxc token
// and d cookie, matching the protocol used by the browser client.
// Events are dispatched to the provided handler in a goroutine.
// Call this after Connect.
func (c *Client) StartWebSocket(handler EventHandler) error {
	wsURL := fmt.Sprintf(
		"wss://wss-primary.slack.com/?token=%s&sync_desync=1&slack_client=desktop&start_args=%%3Fagent%%3Dclient%%26connect_only%%3Dtrue%%26ms_latest%%3Dtrue&no_query_on_subscribe=1&flannel=3&lazy_channels=1&gateway_server=%s-1&batch_presence_aware=1",
		url.QueryEscape(c.token),
		c.teamID,
	)

	jar := newCookieJar(c.cookie)
	dialer := &websocket.Dialer{Jar: jar}

	headers := http.Header{}
	headers.Add("Origin", "https://app.slack.com")

	conn, _, err := dialer.Dial(wsURL, headers)
	if err != nil {
		return fmt.Errorf("websocket connect failed: %w", err)
	}
	c.wsConn = conn
	c.wsDone = make(chan struct{})

	// Detect dead connections: set a read deadline that resets on every
	// incoming message or pong. Slack sends pings ~every 30s, so a 60s
	// deadline gives plenty of margin. Without this, ReadMessage blocks
	// forever on a silently-dropped TCP connection (e.g., wifi disconnect).
	const wsTimeout = 60 * time.Second
	conn.SetReadDeadline(time.Now().Add(wsTimeout))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsTimeout))
		return nil
	})
	conn.SetPingHandler(func(msg string) error {
		conn.SetReadDeadline(time.Now().Add(wsTimeout))
		c.wsMu.Lock()
		defer c.wsMu.Unlock()
		return conn.WriteControl(websocket.PongMessage, []byte(msg), time.Now().Add(10*time.Second))
	})

	go func() {
		defer close(c.wsDone)
		defer handler.OnDisconnect()
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					return
				}
				// Read error (timeout, connection closed, etc.) — exit loop
				return
			}
			// Reset deadline on every successful read
			conn.SetReadDeadline(time.Now().Add(wsTimeout))
			dispatchWebSocketEvent(message, handler)
		}
	}()

	return nil
}

// StopWebSocket disconnects the WebSocket connection.
func (c *Client) StopWebSocket() error {
	if c.wsConn != nil {
		return c.wsConn.Close()
	}
	return nil
}

// SendTyping sends a typing indicator to the given channel via WebSocket.
func (c *Client) SendTyping(channelID string) error {
	c.wsMu.Lock()
	defer c.wsMu.Unlock()
	if c.wsConn == nil {
		return fmt.Errorf("websocket not connected")
	}
	msg := map[string]string{
		"type":    "typing",
		"channel": channelID,
	}
	return c.wsConn.WriteJSON(msg)
}

// SubscribePresence asks Slack to deliver presence_change events for the
// given user IDs. Sent over the existing WebSocket connection. Slack only
// emits presence_change for users you've explicitly subscribed to (the
// authenticated user is typically auto-subscribed at connect, but the
// explicit subscription is reliable across servers).
func (c *Client) SubscribePresence(userIDs []string) error {
	c.wsMu.Lock()
	defer c.wsMu.Unlock()
	if c.wsConn == nil {
		return fmt.Errorf("websocket not connected")
	}
	msg := map[string]interface{}{
		"type": "presence_sub",
		"ids":  userIDs,
	}
	return c.wsConn.WriteJSON(msg)
}

// GetChannels retrieves conversations the user is a member of (channels, DMs,
// group DMs), paginating automatically. Uses users.conversations which returns
// only joined channels — much faster than conversations.list for large workspaces.
func (c *Client) GetChannels(ctx context.Context) ([]slack.Channel, error) {
	var allChannels []slack.Channel
	cursor := ""

	for {
		params := &slack.GetConversationsForUserParameters{
			Types:           []string{"public_channel", "private_channel", "mpim", "im"},
			Limit:           200,
			Cursor:          cursor,
			ExcludeArchived: true,
		}

		channels, nextCursor, err := c.api.GetConversationsForUser(params)
		if err != nil {
			// Handle rate limits gracefully
			if rlErr, ok := err.(*slack.RateLimitedError); ok {
				wait := rlErr.RetryAfter
				if wait == 0 {
					wait = 30 * time.Second
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(wait):
				}
				continue // retry same page
			}
			return nil, fmt.Errorf("getting user conversations: %w", err)
		}

		allChannels = append(allChannels, channels...)

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return allChannels, nil
}

// GetAllPublicChannels retrieves all public channels in the workspace via
// conversations.list, including ones the user is NOT a member of. This is used
// to populate the channel finder so users can join / switch to public channels
// they haven't joined yet.
//
// Note: this is significantly slower than GetChannels for large workspaces
// (potentially thousands of channels). Callers should run it in the background
// after the joined-channel list is loaded.
func (c *Client) GetAllPublicChannels(ctx context.Context) ([]slack.Channel, error) {
	var allChannels []slack.Channel
	cursor := ""

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		params := &slack.GetConversationsParameters{
			Types:           []string{"public_channel"},
			Limit:           1000,
			Cursor:          cursor,
			ExcludeArchived: true,
		}

		channels, nextCursor, err := c.api.GetConversations(params)
		if err != nil {
			if rlErr, ok := err.(*slack.RateLimitedError); ok {
				wait := rlErr.RetryAfter
				if wait == 0 {
					wait = 30 * time.Second
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(wait):
				}
				continue
			}
			return nil, fmt.Errorf("listing public channels: %w", err)
		}

		allChannels = append(allChannels, channels...)

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return allChannels, nil
}

// JoinChannel joins a public channel via conversations.join. Returns nil on
// success. Idempotent: joining a channel you're already in is a no-op on
// Slack's side and returns no error here.
func (c *Client) JoinChannel(ctx context.Context, channelID string) error {
	_, _, _, err := c.api.JoinConversation(channelID)
	if err != nil {
		return fmt.Errorf("joining channel %s: %w", channelID, err)
	}
	return nil
}

// GetUserProfile fetches a single user's profile by ID.
func (c *Client) GetUserProfile(userID string) (*slack.User, error) {
	user, err := c.api.GetUserInfo(userID)
	if err != nil {
		return nil, fmt.Errorf("getting user info: %w", err)
	}
	return user, nil
}

// GetHistory retrieves message history for a channel.
// If oldest is set, returns messages newer than that timestamp.
func (c *Client) GetHistory(ctx context.Context, channelID string, limit int, oldest string) ([]slack.Message, error) {
	params := &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Limit:     limit,
	}
	if oldest != "" {
		params.Oldest = oldest
	}

	resp, err := c.api.GetConversationHistory(params)
	if err != nil {
		return nil, fmt.Errorf("getting history: %w", err)
	}

	return resp.Messages, nil
}

// GetOlderHistory retrieves messages older than the given timestamp.
func (c *Client) GetOlderHistory(ctx context.Context, channelID string, limit int, latest string) ([]slack.Message, error) {
	params := &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Limit:     limit,
		Latest:    latest,
	}

	resp, err := c.api.GetConversationHistory(params)
	if err != nil {
		return nil, fmt.Errorf("getting older history: %w", err)
	}

	return resp.Messages, nil
}

// GetUsers retrieves all users in the workspace.
func (c *Client) GetUsers(ctx context.Context) ([]slack.User, error) {
	users, err := c.api.GetUsersContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting users: %w", err)
	}
	return users, nil
}

// ListCustomEmoji fetches the workspace's custom emoji list via Slack's
// emoji.list API. Returns a map of emoji name -> URL or "alias:targetname".
// The map is empty if the workspace has no custom emojis.
func (c *Client) ListCustomEmoji(ctx context.Context) (map[string]string, error) {
	emojis, err := c.api.GetEmoji()
	if err != nil {
		return nil, fmt.Errorf("listing custom emoji: %w", err)
	}
	if emojis == nil {
		emojis = map[string]string{}
	}
	return emojis, nil
}

// SendMessage posts a new message to the specified channel.
// Returns the timestamp of the sent message.
func (c *Client) SendMessage(ctx context.Context, channelID, text string) (string, error) {
	_, ts, err := c.api.PostMessage(channelID, slack.MsgOptionText(text, false))
	if err != nil {
		return "", fmt.Errorf("sending message: %w", err)
	}
	return ts, nil
}

// UploadFile uploads a single file to a channel (and optional thread)
// using Slack's V2 external-upload flow. The slack-go library's
// UploadFileContext (named for the underlying file.upload.v2 API)
// handles the three internal steps:
// getUploadURLExternal -> PUT -> completeUploadExternal.
//
// caption, when non-empty, is attached as the file's initial_comment.
// For multi-file batches the caller should set caption on the LAST
// file only (Slack groups files completed in one share into one
// message; sequential single-file uploads can't be grouped).
//
// size is int64 (matching os.FileInfo.Size()) and is narrowed to int
// for slack-go. Callers must enforce a reasonable upper bound; this
// wrapper does not.
func (c *Client) UploadFile(
	ctx context.Context,
	channelID, threadTS, filename string,
	r io.Reader,
	size int64,
	caption string,
) (*slack.FileSummary, error) {
	params := slack.UploadFileParameters{
		Filename: filename,
		Reader:   r,
		FileSize: int(size),
		Channel:  channelID,
	}
	if threadTS != "" {
		params.ThreadTimestamp = threadTS
	}
	if caption != "" {
		params.InitialComment = caption
	}
	f, err := c.api.UploadFileContext(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("uploading file %q: %w", filename, err)
	}
	return f, nil
}

// UnreadInfo holds the unread state for a single channel.
type UnreadInfo struct {
	ChannelID string
	Count     int
	HasUnread bool
	LastRead  string // Slack message timestamp
}

// GetUnreadCounts fetches unread counts for all channels using Slack's
// internal client.counts API (available with xoxc browser tokens).
func (c *Client) GetUnreadCounts() ([]UnreadInfo, error) {
	reqURL := "https://slack.com/api/client.counts"
	req, err := http.NewRequest("POST", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	httpClient := newCookieHTTPClient(c.cookie)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching unread counts: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var result struct {
		OK       bool `json:"ok"`
		Channels []struct {
			ID                 string `json:"id"`
			HasUnreads         bool   `json:"has_unreads"`
			MentionCount       int    `json:"mention_count"`
			UnreadCountDisplay int    `json:"unread_count_display,omitempty"`
			LastRead           string `json:"last_read"`
		} `json:"channels"`
		Mpims []struct {
			ID           string `json:"id"`
			HasUnreads   bool   `json:"has_unreads"`
			MentionCount int    `json:"mention_count"`
			LastRead     string `json:"last_read"`
		} `json:"mpims"`
		Ims []struct {
			ID         string `json:"id"`
			HasUnreads bool   `json:"has_unreads"`
			LastRead   string `json:"last_read"`
		} `json:"ims"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	if !result.OK {
		return nil, fmt.Errorf("client.counts API returned ok=false")
	}

	var unreads []UnreadInfo
	for _, ch := range result.Channels {
		info := UnreadInfo{
			ChannelID: ch.ID,
			LastRead:  ch.LastRead,
			HasUnread: ch.HasUnreads,
		}
		if ch.HasUnreads {
			info.Count = ch.MentionCount
			if info.Count == 0 {
				info.Count = 1 // has unreads but no mention count
			}
		}
		unreads = append(unreads, info)
	}
	for _, ch := range result.Mpims {
		info := UnreadInfo{
			ChannelID: ch.ID,
			LastRead:  ch.LastRead,
			HasUnread: ch.HasUnreads,
		}
		if ch.HasUnreads {
			info.Count = max(ch.MentionCount, 1)
		}
		unreads = append(unreads, info)
	}
	for _, ch := range result.Ims {
		info := UnreadInfo{
			ChannelID: ch.ID,
			LastRead:  ch.LastRead,
			HasUnread: ch.HasUnreads,
		}
		if ch.HasUnreads {
			info.Count = 1
		}
		unreads = append(unreads, info)
	}

	return unreads, nil
}

// SendReply posts a threaded reply to the specified message.
func (c *Client) SendReply(ctx context.Context, channelID, threadTS, text string) (string, error) {
	_, ts, err := c.api.PostMessage(channelID,
		slack.MsgOptionText(text, false),
		slack.MsgOptionTS(threadTS),
	)
	if err != nil {
		return "", fmt.Errorf("sending reply: %w", err)
	}
	return ts, nil
}

// GetReplies retrieves all replies in a thread.
// The first message in the returned slice is the parent message.
func (c *Client) GetReplies(ctx context.Context, channelID, threadTS string) ([]slack.Message, error) {
	var allMessages []slack.Message
	cursor := ""

	for {
		msgs, hasMore, nextCursor, err := c.api.GetConversationReplies(&slack.GetConversationRepliesParameters{
			ChannelID: channelID,
			Timestamp: threadTS,
			Cursor:    cursor,
		})
		if err != nil {
			return nil, fmt.Errorf("getting thread replies: %w", err)
		}
		allMessages = append(allMessages, msgs...)
		if !hasMore || nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return allMessages, nil
}

// EditMessage updates an existing message's text.
func (c *Client) EditMessage(ctx context.Context, channelID, ts, text string) error {
	_, _, _, err := c.api.UpdateMessage(channelID, ts, slack.MsgOptionText(text, false))
	if err != nil {
		return fmt.Errorf("editing message: %w", err)
	}
	return nil
}

// RemoveMessage deletes a message from the channel.
func (c *Client) RemoveMessage(ctx context.Context, channelID, ts string) error {
	_, _, err := c.api.DeleteMessage(channelID, ts)
	if err != nil {
		return fmt.Errorf("deleting message: %w", err)
	}
	return nil
}

// AddReaction adds an emoji reaction to a message.
func (c *Client) AddReaction(ctx context.Context, channelID, ts, emoji string) error {
	return c.api.AddReaction(emoji, slack.ItemRef{Channel: channelID, Timestamp: ts})
}

// RemoveReaction removes an emoji reaction from a message.
func (c *Client) RemoveReaction(ctx context.Context, channelID, ts, emoji string) error {
	return c.api.RemoveReaction(emoji, slack.ItemRef{Channel: channelID, Timestamp: ts})
}

// SetUserPresence sets the authenticated user's presence. Accepts "auto"
// (let Slack determine activity) or "away" (force away). Note the write
// vocabulary differs from the read side — GetUserPresence and the
// presence_change WebSocket event return "active" or "away".
func (c *Client) SetUserPresence(ctx context.Context, presence string) error {
	if err := c.api.SetUserPresenceContext(ctx, presence); err != nil {
		return fmt.Errorf("setting presence: %w", err)
	}
	return nil
}

// GetUserPresence fetches a user's current presence ("active" or "away").
// Pass the authenticated user's ID to read your own state.
func (c *Client) GetUserPresence(ctx context.Context, userID string) (*slack.UserPresence, error) {
	p, err := c.api.GetUserPresenceContext(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("getting presence: %w", err)
	}
	return p, nil
}

// SetSnooze enables Do-Not-Disturb for `minutes` minutes.
func (c *Client) SetSnooze(ctx context.Context, minutes int) (*slack.DNDStatus, error) {
	st, err := c.api.SetSnoozeContext(ctx, minutes)
	if err != nil {
		return nil, fmt.Errorf("setting snooze: %w", err)
	}
	return st, nil
}

// EndSnooze ends the current snooze window. Does NOT end admin-scheduled DND.
func (c *Client) EndSnooze(ctx context.Context) (*slack.DNDStatus, error) {
	st, err := c.api.EndSnoozeContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("ending snooze: %w", err)
	}
	return st, nil
}

// EndDND ends the user's current scheduled DND session.
func (c *Client) EndDND(ctx context.Context) error {
	if err := c.api.EndDNDContext(ctx); err != nil {
		return fmt.Errorf("ending DND: %w", err)
	}
	return nil
}

// GetDNDInfo fetches DND/snooze status for a user.
func (c *Client) GetDNDInfo(ctx context.Context, userID string) (*slack.DNDStatus, error) {
	u := userID
	st, err := c.api.GetDNDInfoContext(ctx, &u)
	if err != nil {
		return nil, fmt.Errorf("getting DND info: %w", err)
	}
	return st, nil
}

// GetPermalink returns the Slack permalink for a message. For a thread reply,
// pass the reply's ts; Slack returns a thread-aware URL with thread_ts and cid
// query parameters.
func (c *Client) GetPermalink(ctx context.Context, channelID, ts string) (string, error) {
	url, err := c.api.GetPermalinkContext(ctx, &slack.PermalinkParameters{
		Channel: channelID,
		Ts:      ts,
	})
	if err != nil {
		return "", fmt.Errorf("getting permalink: %w", err)
	}
	return url, nil
}

// MarkChannel marks a channel as read up to the given timestamp.
func (c *Client) MarkChannel(ctx context.Context, channelID, ts string) error {
	data := url.Values{
		"channel": {channelID},
		"ts":      {ts},
	}

	req, err := http.NewRequest("POST", "https://slack.com/api/conversations.mark",
		strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("creating mark request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+c.token)

	httpClient := newCookieHTTPClient(c.cookie)
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("marking channel: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

// ChannelSection represents a user's sidebar section from the undocumented Slack API.
type ChannelSection struct {
	ID         string   `json:"channel_section_id"`
	Name       string   `json:"name"`
	ChannelIDs []string `json:"channel_ids_page"`
	Type       string   `json:"type"`
}

// GetChannelSections calls the undocumented users.channelSections.list API
// to retrieve the user's sidebar sections. This may break if Slack changes the API.
func (c *Client) GetChannelSections(ctx context.Context) ([]ChannelSection, error) {
	endpoint := "https://slack.com/api/users.channelSections.list"

	form := url.Values{}
	form.Set("token", c.token)

	resp, err := http.PostForm(endpoint, form)
	if err != nil {
		return nil, fmt.Errorf("calling channelSections API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var result struct {
		OK              bool             `json:"ok"`
		Error           string           `json:"error"`
		ChannelSections []ChannelSection `json:"channel_sections"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	if !result.OK {
		return nil, fmt.Errorf("API error: %s (response: %s)", result.Error, string(body))
	}

	return result.ChannelSections, nil
}
