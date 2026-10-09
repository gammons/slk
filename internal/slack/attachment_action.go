package slackclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/gammons/slk/internal/core/blocks"
)

// AttachmentActionRequest identifies one press of a legacy attachment
// button. Action is the button as received; it is echoed back verbatim.
type AttachmentActionRequest struct {
	ChannelID    string
	MessageTS    string
	AttachmentID int
	CallbackID   string
	Ephemeral    bool
	Action       blocks.LegacyAction
}

// AttachmentAction presses a legacy attachment button, as Slack's web
// client does (chat.attachmentAction, undocumented). Slack answers the
// press over the WebSocket -- for an ephemeral, a message_deleted -- so
// only the error is returned.
func (c *Client) AttachmentAction(ctx context.Context, req AttachmentActionRequest) error {
	payload, err := marshalAttachmentAction(req)
	if err != nil {
		return err
	}
	raw, err := c.postForm(ctx, "chat.attachmentAction", url.Values{"payload": {payload}})
	if err != nil {
		return err
	}
	return parseOKResponse("chat.attachmentAction", raw)
}

// attachmentActionPayload is the `payload` form field, key for key in
// the order the web client sends it. See the capture in
// docs/superpowers/specs/2026-10-09-interactive-ephemeral-messages-design.md.
// slk mimics the web client's request shape deliberately: an
// unfamiliar shape is the kind of signature Enterprise Grid anomaly
// detection flags (see postForm).
type attachmentActionPayload struct {
	Actions          []attachmentActionWire `json:"actions"`
	AttachmentID     string                 `json:"attachment_id"`
	CallbackID       string                 `json:"callback_id"`
	ChannelID        string                 `json:"channel_id"`
	IsEphemeral      bool                   `json:"is_ephemeral"`
	MessageTS        string                 `json:"message_ts"`
	PromptAppInstall bool                   `json:"prompt_app_install"`
}

// attachmentActionWire is one action as Slack sent it. style is kept
// even when empty, as captured.
type attachmentActionWire struct {
	ID      string                 `json:"id"`
	Name    string                 `json:"name"`
	Text    string                 `json:"text"`
	Type    string                 `json:"type"`
	Value   string                 `json:"value"`
	Style   string                 `json:"style"`
	URL     string                 `json:"url,omitempty"`
	Confirm *attachmentConfirmWire `json:"confirm,omitempty"`
}

type attachmentConfirmWire struct {
	Text        string `json:"text"`
	Title       string `json:"title"`
	OKText      string `json:"ok_text"`
	DismissText string `json:"dismiss_text"`
}

// marshalAttachmentAction encodes req as the payload field. HTML is not
// escaped: the web client sends confirm text verbatim.
func marshalAttachmentAction(req AttachmentActionRequest) (string, error) {
	a := req.Action
	wire := attachmentActionWire{
		ID: a.ID, Name: a.Name, Text: a.Text, Type: a.Type,
		Value: a.Value, Style: a.Style, URL: a.URL,
	}
	if a.Confirm != nil {
		wire.Confirm = &attachmentConfirmWire{
			Text: a.Confirm.Text, Title: a.Confirm.Title,
			OKText: a.Confirm.OKText, DismissText: a.Confirm.DismissText,
		}
	}
	p := attachmentActionPayload{
		Actions:      []attachmentActionWire{wire},
		AttachmentID: strconv.Itoa(req.AttachmentID),
		CallbackID:   req.CallbackID,
		ChannelID:    req.ChannelID,
		IsEphemeral:  req.Ephemeral,
		MessageTS:    req.MessageTS,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(p); err != nil {
		return "", fmt.Errorf("encoding chat.attachmentAction payload: %w", err)
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}
