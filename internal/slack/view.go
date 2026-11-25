package slackclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/slack-go/slack"
)

// View is a modal an app opened for the current user (Slack calls
// these "views"): the Block Kit content, the labels of its close and
// submit buttons, and the IDs a button press or a refresh needs.
type View struct {
	ID string
	// TeamID is the workspace the view belongs to, as Slack records it.
	TeamID string
	// AppID is the app that published the view.
	AppID string
	// BotID is the app's bot user ID; blocks.actions takes it as
	// service_id.
	BotID      string
	Title      string
	CloseLabel string
	// SubmitLabel is the submit button's label, empty when the view has
	// no submit (a read-only dialog).
	SubmitLabel string
	CallbackID  string
	// PreviousViewID is the view this one was pushed over, empty for
	// the root view of a modal stack.
	PreviousViewID string
	// RootViewID is the bottom view of the modal stack.
	RootViewID string
	Blocks     slack.Blocks
}

// viewText is Slack's plain_text object.
type viewText struct {
	Text string `json:"text"`
}

// viewWire is the `view` object Slack sends on views.get and inside
// view_opened / view_updated frames. Only the fields slk draws or sends
// back are decoded.
type viewWire struct {
	ID             string       `json:"id"`
	TeamID         string       `json:"team_id"`
	AppID          string       `json:"app_id"`
	BotID          string       `json:"bot_id"`
	Title          viewText     `json:"title"`
	Submit         *viewText    `json:"submit"`
	Close          *viewText    `json:"close"`
	CallbackID     string       `json:"callback_id"`
	PreviousViewID *string      `json:"previous_view_id"`
	RootViewID     string       `json:"root_view_id"`
	Blocks         slack.Blocks `json:"blocks"`
}

// decodeView decodes a view object without requiring an ID: a frame
// may trim it, and a title-only view can still be shown.
func decodeView(raw []byte) (*View, error) {
	var w viewWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, fmt.Errorf("parsing view: %w (body=%s)", err, truncateForLog(raw))
	}
	v := &View{
		ID:         w.ID,
		TeamID:     w.TeamID,
		AppID:      w.AppID,
		BotID:      w.BotID,
		Title:      w.Title.Text,
		CallbackID: w.CallbackID,
		RootViewID: w.RootViewID,
		Blocks:     w.Blocks,
	}
	if w.Submit != nil {
		v.SubmitLabel = w.Submit.Text
	}
	if w.Close != nil {
		v.CloseLabel = w.Close.Text
	}
	if w.PreviousViewID != nil {
		v.PreviousViewID = *w.PreviousViewID
	}
	return v, nil
}

// parseView decodes a view object that must carry an ID, which a press
// and a refresh both need. It is split out from GetView so the tests
// can exercise it without a round trip.
func parseView(raw []byte) (*View, error) {
	v, err := decodeView(raw)
	if err != nil {
		return nil, err
	}
	if v.ID == "" {
		return nil, fmt.Errorf("parsing view: no view id (body=%s)", truncateForLog(raw))
	}
	return v, nil
}

// viewOpenedEvent is the view_opened frame: the view is inline, and
// some of its fields are repeated at the top level. The nested object
// is authoritative; the top-level ones are the fallback for a frame
// whose view is missing or that Slack trimmed.
type viewOpenedEvent struct {
	ViewID string          `json:"view_id"`
	AppID  string          `json:"app_id"`
	Title  viewText        `json:"title"`
	Submit *viewText       `json:"submit"`
	Close  *viewText       `json:"close"`
	View   json.RawMessage `json:"view"`
}

// parseViewOpened decodes a view_opened frame. A frame with no view
// object still yields a View carrying the title and ID, so a short
// notice or a fetch can still name it.
func parseViewOpened(raw []byte) (*View, error) {
	var evt viewOpenedEvent
	if err := json.Unmarshal(raw, &evt); err != nil {
		return nil, fmt.Errorf("parsing view_opened: %w (body=%s)", err, truncateForLog(raw))
	}
	v, err := decodeView(evt.View)
	if err != nil {
		v = nil
	}
	if v == nil {
		v = &View{}
	}
	if v.ID == "" && evt.ViewID == "" && v.Title == "" && evt.Title.Text == "" {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("view_opened: no view or title (body=%s)", truncateForLog(raw))
	}
	if v.ID == "" {
		v.ID = evt.ViewID
	}
	if v.AppID == "" {
		v.AppID = evt.AppID
	}
	if v.Title == "" {
		v.Title = evt.Title.Text
	}
	if v.SubmitLabel == "" && evt.Submit != nil {
		v.SubmitLabel = evt.Submit.Text
	}
	if v.CloseLabel == "" && evt.Close != nil {
		v.CloseLabel = evt.Close.Text
	}
	return v, nil
}

// viewUpdatedEvent is the view_updated frame. view is null when Slack
// asks the client to fetch the current content itself.
type viewUpdatedEvent struct {
	ViewID string          `json:"view_id"`
	View   json.RawMessage `json:"view"`
}

// GetView fetches a modal's current content (views.get). The view may
// have changed since the frame that announced it, and a view_updated
// with no inline view leaves fetching as the only way to draw it.
func (c *Client) GetView(ctx context.Context, viewID string) (*View, error) {
	raw, err := c.postForm(ctx, "views.get", url.Values{"view_id": {viewID}})
	if err != nil {
		return nil, err
	}
	var resp struct {
		OK    bool            `json:"ok"`
		Error string          `json:"error"`
		View  json.RawMessage `json:"view"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parsing views.get: %w (body=%s)", err, truncateForLog(raw))
	}
	if !resp.OK {
		return nil, fmt.Errorf("views.get: %s (body=%s)", resp.Error, truncateForLog(raw))
	}
	if len(resp.View) == 0 || string(resp.View) == "null" {
		return nil, fmt.Errorf("views.get: no view (body=%s)", truncateForLog(raw))
	}
	return parseView(resp.View)
}
