package slackclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gammons/slk/internal/debuglog"
)

// BlockActionRequest identifies one press of a Block Kit button in a
// view. The fields are the button as the view rendered it; they are
// echoed back to the app.
type BlockActionRequest struct {
	// ViewID is the view the button belongs to (the app home's id).
	ViewID string
	// ServiceID is the posting app's bot ID.
	ServiceID string
	ActionID  string
	BlockID   string
	Label     string
	Value     string
	Style     string
	// Confirm is the button's confirmation step, when it has one.
	Confirm *BlockActionConfirm
}

// BlockActionConfirm is a button's confirmation dialog as the view
// renders it.
type BlockActionConfirm struct {
	Title       string
	Text        string
	OKText      string
	DismissText string
}

// BlockAction presses a Block Kit button in a view, as Slack's web
// client does (blocks.actions, undocumented). Slack dispatches the
// action to the app and re-publishes the view over the WebSocket, so
// only the error is returned.
//
// Like chat.attachmentAction, this is a single request with no retry: a
// press can have side effects on the app's side, so replaying it after a
// transport failure could act twice.
func (c *Client) BlockAction(ctx context.Context, req BlockActionRequest) error {
	actions, err := marshalBlockActions(req)
	if err != nil {
		return err
	}
	container, err := marshalBlockActionContainer(req.ViewID)
	if err != nil {
		return err
	}
	form := url.Values{
		"service_id":   {req.ServiceID},
		"client_token": {newClientToken()},
		"actions":      {actions},
		"container":    {container},
	}
	debuglog.General("blocks.actions: action=%q view=%q service_id=%q actions=%s",
		req.ActionID, req.ViewID, req.ServiceID, actions)
	raw, err := c.postForm(ctx, "blocks.actions", form)
	if err == nil {
		err = parseOKResponse("blocks.actions", raw)
	}
	if err != nil {
		debuglog.General("blocks.actions: error=%v", err)
		return err
	}
	debuglog.General("blocks.actions: ok")
	return nil
}

// blockActionWire is one action as the web client sends it. The field
// order follows the capture the endpoint was verified against.
type blockActionWire struct {
	ActionID string              `json:"action_id"`
	BlockID  string              `json:"block_id"`
	Text     blockActionText     `json:"text"`
	Value    string              `json:"value,omitempty"`
	Type     string              `json:"type"`
	ActionTS string              `json:"action_ts"`
	Style    string              `json:"style,omitempty"`
	Confirm  *blockActionConfirm `json:"confirm,omitempty"`
}

type blockActionText struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Emoji bool   `json:"emoji,omitempty"`
}

// blockActionConfirmText is a confirmation's body: it is mrkdwn, which
// carries a verbatim flag where plain_text carries emoji.
type blockActionConfirmText struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Verbatim bool   `json:"verbatim"`
}

type blockActionConfirm struct {
	Title   blockActionText        `json:"title"`
	Text    blockActionConfirmText `json:"text"`
	Confirm blockActionText        `json:"confirm"`
	Deny    blockActionText        `json:"deny"`
}

// blockActionContainer names the view the pressed button lives in.
type blockActionContainer struct {
	Type   string `json:"type"`
	ViewID string `json:"view_id"`
}

// marshalBlockActions encodes req as the actions form field.
func marshalBlockActions(req BlockActionRequest) (string, error) {
	a := blockActionWire{
		ActionID: req.ActionID,
		BlockID:  req.BlockID,
		Text:     blockActionText{Type: "plain_text", Text: req.Label, Emoji: true},
		Value:    req.Value,
		Type:     "button",
		ActionTS: strconv.FormatFloat(float64(time.Now().UnixNano())/1e9, 'f', 6, 64),
		Style:    req.Style,
	}
	if c := req.Confirm; c != nil {
		a.Confirm = &blockActionConfirm{
			Title:   blockActionText{Type: "plain_text", Text: c.Title, Emoji: true},
			Text:    blockActionConfirmText{Type: "mrkdwn", Text: c.Text},
			Confirm: blockActionText{Type: "plain_text", Text: c.OKText, Emoji: true},
			Deny:    blockActionText{Type: "plain_text", Text: c.DismissText, Emoji: true},
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode([]blockActionWire{a}); err != nil {
		return "", fmt.Errorf("encoding blocks.actions payload: %w", err)
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

// marshalBlockActionContainer encodes the container form field.
func marshalBlockActionContainer(viewID string) (string, error) {
	raw, err := json.Marshal(blockActionContainer{Type: "view", ViewID: viewID})
	if err != nil {
		return "", fmt.Errorf("encoding blocks.actions container: %w", err)
	}
	return string(raw), nil
}
