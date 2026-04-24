package slackclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

// SlackAPI defines the subset of the Slack API we use.
// This interface enables mocking in tests.
type SlackAPI interface {
	GetConversations(params *slack.GetConversationsParameters) ([]slack.Channel, string, error)
	GetConversationHistory(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error)
	GetConversationReplies(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error)
	GetUsersContext(ctx context.Context) ([]slack.User, error)
	PostMessage(channelID string, options ...slack.MsgOption) (string, string, error)
	UpdateMessage(channelID, timestamp string, options ...slack.MsgOption) (string, string, string, error)
	DeleteMessage(channelID, timestamp string) (string, string, error)
	AddReaction(name string, item slack.ItemRef) error
	RemoveReaction(name string, item slack.ItemRef) error
	AuthTest() (*slack.AuthTestResponse, error)
}

// Client wraps the slack-go library, providing Socket Mode connectivity
// and a simplified Web API surface for the service layer.
type Client struct {
	api       *slack.Client
	socket    *socketmode.Client
	teamID    string
	userID    string
	userToken string
	appToken  string
}

// NewClient creates a new Slack client with the given user and app-level tokens.
func NewClient(userToken, appToken string) *Client {
	api := slack.New(
		userToken,
		slack.OptionAppLevelToken(appToken),
	)

	socket := socketmode.New(
		api,
		socketmode.OptionLog(log.New(log.Writer(), "socketmode: ", log.Lshortfile|log.LstdFlags)),
	)

	return &Client{
		api:       api,
		socket:    socket,
		userToken: userToken,
		appToken:  appToken,
	}
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

// RunSocketMode starts the Socket Mode event loop. Events are dispatched
// to the provided handler. Blocks until the context is cancelled.
func (c *Client) RunSocketMode(ctx context.Context, handler EventHandler) error {
	go func() {
		for evt := range c.socket.Events {
			dispatcher := NewEventDispatcher(c.socket, handler)
			dispatcher.HandleEvent(evt)
		}
	}()
	return c.socket.RunContext(ctx)
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
	form.Set("token", c.userToken)

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
