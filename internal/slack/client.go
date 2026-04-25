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

	"github.com/gorilla/websocket"
	"github.com/slack-go/slack"
)

// SlackAPI defines the subset of the Slack API we use.
// This interface enables mocking in tests.
type SlackAPI interface {
	GetConversations(params *slack.GetConversationsParameters) ([]slack.Channel, string, error)
	GetConversationHistory(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error)
	GetConversationReplies(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error)
	GetUsersContext(ctx context.Context, options ...slack.GetUsersOption) ([]slack.User, error)
	PostMessage(channelID string, options ...slack.MsgOption) (string, string, error)
	UpdateMessage(channelID, timestamp string, options ...slack.MsgOption) (string, string, string, error)
	DeleteMessage(channelID, timestamp string) (string, string, error)
	AddReaction(name string, item slack.ItemRef) error
	RemoveReaction(name string, item slack.ItemRef) error
	AuthTest() (*slack.AuthTestResponse, error)
}

// Client wraps the slack-go library, providing RTM connectivity
// and a simplified Web API surface for the service layer.
// Uses browser cookie auth (xoxc token + d cookie).
type Client struct {
	api    SlackAPI
	wsConn *websocket.Conn
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

	go func() {
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					return
				}
				log.Printf("WebSocket read error: %v", err)
				return
			}
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

// GetChannels retrieves all conversations (channels, DMs, group DMs) the
// authenticated user has access to, paginating automatically.
func (c *Client) GetChannels(ctx context.Context) ([]slack.Channel, error) {
	var allChannels []slack.Channel
	cursor := ""

	for {
		params := &slack.GetConversationsParameters{
			Types:           []string{"public_channel", "private_channel", "mpim", "im"},
			Limit:           200,
			Cursor:          cursor,
			ExcludeArchived: true,
		}

		channels, nextCursor, err := c.api.GetConversations(params)
		if err != nil {
			return nil, fmt.Errorf("getting conversations: %w", err)
		}

		allChannels = append(allChannels, channels...)

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return allChannels, nil
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

// SendMessage posts a new message to the specified channel.
// Returns the timestamp of the sent message.
func (c *Client) SendMessage(ctx context.Context, channelID, text string) (string, error) {
	_, ts, err := c.api.PostMessage(channelID, slack.MsgOptionText(text, false))
	if err != nil {
		return "", fmt.Errorf("sending message: %w", err)
	}
	return ts, nil
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
