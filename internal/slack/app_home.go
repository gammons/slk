package slackclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/slack-go/slack"
)

// AppHome is an app's Home tab as conversations.info returns it for the
// app's DM. Slack serves the view alongside the app_home metadata when
// the app publishes one (home_tab_enabled); a DM with a plain bot has
// none.
type AppHome struct {
	AppID string
	// ViewID identifies this rendering of the view (home_view_id).
	ViewID string
	// Title is the view's title, when it has one.
	Title string
	// BotID is the app's bot user ID (the view's bot_id), which
	// blocks.actions takes as service_id.
	BotID string
	// Blocks is the view's Block Kit content, in the same shape a
	// message carries, so the TUI renders it with the message renderer.
	Blocks slack.Blocks
}

// appHomeInfo is the subset of conversations.info this call decodes.
// The fields are nested exactly as Slack sends them: app_home holds the
// metadata, home_view the view itself (present but null when the app
// has no home tab).
type appHomeInfo struct {
	OK       bool             `json:"ok"`
	Error    string           `json:"error"`
	AppHome  *appHomeMetadata `json:"app_home"`
	HomeView *appHomeView     `json:"home_view"`
}

type appHomeMetadata struct {
	AppID          string `json:"app_id"`
	HomeTabEnabled bool   `json:"home_tab_enabled"`
	HomeViewID     string `json:"home_view_id"`
}

type appHomeView struct {
	ID    string `json:"id"`
	AppID string `json:"app_id"`
	BotID string `json:"bot_id"`
	Title struct {
		Text string `json:"text"`
	} `json:"title"`
	Blocks slack.Blocks `json:"blocks"`
}

// appHomeForm is conversations.info's form for an app home fetch. The
// web client sends the same two fields; return_app_home is what makes
// Slack include app_home and home_view at all.
func appHomeForm(channelID string) url.Values {
	return url.Values{
		"channel":         {channelID},
		"return_app_home": {"true"},
	}
}

// GetAppHome fetches an app's Home tab for its DM channelID. Returns nil
// (no error) when the conversation is not an app DM or the app has no
// home view, which is the common case and not a failure.
func (c *Client) GetAppHome(ctx context.Context, channelID string) (*AppHome, error) {
	raw, err := c.getAppHomeRaw(ctx, channelID)
	if err != nil {
		return nil, err
	}
	return parseAppHome(raw)
}

// getAppHomeRaw is conversations.info for channelID with
// return_app_home=true, unparsed.
func (c *Client) getAppHomeRaw(ctx context.Context, channelID string) ([]byte, error) {
	return c.postForm(ctx, "conversations.info", appHomeForm(channelID))
}

// parseAppHome decodes a conversations.info response into an AppHome.
// Split out so tests can exercise it against a capture without a
// network round trip.
func parseAppHome(raw []byte) (*AppHome, error) {
	var info appHomeInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, fmt.Errorf("parsing conversations.info: %w (body=%s)", err, truncateForLog(raw))
	}
	if !info.OK {
		return nil, fmt.Errorf("conversations.info: %s (body=%s)", info.Error, truncateForLog(raw))
	}
	if info.AppHome == nil || !info.AppHome.HomeTabEnabled || info.HomeView == nil {
		return nil, nil
	}
	return &AppHome{
		AppID:  info.AppHome.AppID,
		ViewID: info.AppHome.HomeViewID,
		Title:  info.HomeView.Title.Text,
		BotID:  info.HomeView.BotID,
		Blocks: info.HomeView.Blocks,
	}, nil
}
