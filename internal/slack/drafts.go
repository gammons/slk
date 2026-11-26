package slackclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/slack-go/slack"

	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/slack/mrkdwn"
	"github.com/gammons/slk/internal/slackhttp"
)

// Draft is one draft as drafts.list, drafts.create and drafts.update
// return it (undocumented web-client API). Captures:
// docs/superpowers/specs/2026-11-25-slack-drafts-captures/.
type Draft struct {
	ID            string       `json:"id"`
	DateCreated   int64        `json:"date_created"`
	LastUpdatedTS string       `json:"last_updated_ts"`
	Blocks        slack.Blocks `json:"blocks"`
	FileIDs       []string     `json:"file_ids"`
	// IsFromComposer is true for a draft saved with Slack's "Save as
	// draft" and for one started in "New message"; false for the text
	// sitting in a conversation's composer (an attached draft).
	IsFromComposer bool               `json:"is_from_composer"`
	IsDeleted      bool               `json:"is_deleted"`
	IsSent         bool               `json:"is_sent"`
	DateScheduled  int64              `json:"date_scheduled"`
	Destinations   []DraftDestination `json:"destinations"`
}

// DraftDestination is where a draft will be sent. A DM reads back with
// UserIDs too; slk writes ChannelID only, as the web client does.
type DraftDestination struct {
	ChannelID string   `json:"channel_id"`
	ThreadTS  string   `json:"thread_ts,omitempty"`
	UserIDs   []string `json:"user_ids,omitempty"`
	Broadcast bool     `json:"broadcast,omitempty"`
}

// Attached reports a conversation's composer draft: one destination, not
// saved with "Save as draft". Slack keeps one per destination.
func (d Draft) Attached() bool { return !d.IsFromComposer && len(d.Destinations) == 1 }

// Writable reports an attached draft slk may restore and overwrite: not
// scheduled, no files.
func (d Draft) Writable() bool {
	return d.Attached() && d.DateScheduled == 0 && len(d.FileIDs) == 0
}

// DraftWrite is the content of a create or update: the destination, the
// composer text (markdown, mentions in wire form) and when it was last
// edited.
type DraftWrite struct {
	ChannelID string
	ThreadTS  string
	Text      string
	Edited    time.Time
}

// DraftRejectedError is a drafts.* {"ok":false,"error":Code}, for
// example attached_draft_exists or draft_has_conflict.
type DraftRejectedError struct{ Method, Code string }

func (e *DraftRejectedError) Error() string { return e.Method + ": " + e.Code }

// DraftRejectionCode returns the code of a *DraftRejectedError in err's
// chain, "" for any other error and for nil.
func DraftRejectionCode(err error) string {
	var r *DraftRejectedError
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

// draftReason is the _x_reason the web client's composer autosave sends
// (drafts_create_autosave.json, drafts_update_autosave.json).
const draftReason = "MessageInput:updateDraft"

const (
	draftListPageLimit = 25
	// draftListMaxPages bounds the walk so a server that never says
	// has_more=false cannot loop: 1000 drafts.
	draftListMaxPages = 40
)

// ListDrafts returns the user's active drafts, newest first, following
// drafts.list's next_ts paging (the oldest last_updated_ts so far).
func (c *Client) ListDrafts(ctx context.Context) ([]Draft, error) {
	ctx = slackhttp.WithReason(ctx, "client-v2-boot-team")
	var out []Draft
	seen := map[string]bool{}
	nextTS := ""
	for range draftListMaxPages {
		form := url.Values{"is_active": {"true"}, "limit": {strconv.Itoa(draftListPageLimit)}}
		if nextTS != "" {
			form.Set("next_ts", nextTS)
		}
		raw, err := c.postForm(ctx, "drafts.list", form)
		if err != nil {
			return nil, err
		}
		var resp struct {
			OK      bool    `json:"ok"`
			Error   string  `json:"error"`
			Drafts  []Draft `json:"drafts"`
			HasMore bool    `json:"has_more"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("parsing drafts.list: %w (body=%s)", err, truncateForLog(raw))
		}
		if !resp.OK {
			return nil, &DraftRejectedError{Method: "drafts.list", Code: resp.Error}
		}
		for _, d := range resp.Drafts {
			if !seen[d.ID] {
				seen[d.ID] = true
				out = append(out, d)
			}
		}
		if !resp.HasMore || len(resp.Drafts) == 0 {
			return out, nil
		}
		last := resp.Drafts[len(resp.Drafts)-1].LastUpdatedTS
		if last == nextTS {
			debuglog.General("drafts.list: has_more without progress at next_ts=%s", nextTS)
			return out, nil
		}
		nextTS = last
	}
	debuglog.General("drafts.list: stopped after %d pages", draftListMaxPages)
	return out, nil
}

// CreateDraft makes the attached draft of w's destination.
func (c *Client) CreateDraft(ctx context.Context, w DraftWrite) (Draft, error) {
	form, err := createDraftForm(w, newClientMsgID())
	if err != nil {
		return Draft{}, err
	}
	return c.draftCall(ctx, "drafts.create", form)
}

// UpdateDraft replaces draftID's text with w's.
func (c *Client) UpdateDraft(ctx context.Context, draftID string, w DraftWrite) (Draft, error) {
	form, err := updateDraftForm(draftID, w)
	if err != nil {
		return Draft{}, err
	}
	return c.draftCall(ctx, "drafts.update", form)
}

// DeleteDraft deletes draftID. Not captured; the shape is the one
// open-source clients use against live workspaces (spec, Evidence).
func (c *Client) DeleteDraft(ctx context.Context, draftID string) error {
	form := url.Values{
		"draft_id":               {draftID},
		"client_last_updated_ts": {strconv.FormatInt(time.Now().UnixMilli(), 10)},
	}
	_, err := c.draftCall(ctx, "drafts.delete", form)
	return err
}

func (c *Client) draftCall(ctx context.Context, method string, form url.Values) (Draft, error) {
	ctx = slackhttp.WithReason(ctx, draftReason)
	debuglog.General("%s: draft_id=%s", method, form.Get("draft_id"))
	raw, err := c.postForm(ctx, method, form)
	if err != nil {
		return Draft{}, err
	}
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Draft Draft  `json:"draft"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Draft{}, fmt.Errorf("parsing %s: %w (body=%s)", method, err, truncateForLog(raw))
	}
	if !resp.OK {
		debuglog.General("%s: rejected error=%s", method, resp.Error)
		return Draft{}, &DraftRejectedError{Method: method, Code: resp.Error}
	}
	return resp.Draft, nil
}

// draftForm is the fields create and update share.
func draftForm(w DraftWrite) (url.Values, error) {
	block := mrkdwn.Convert(w.Text)
	if block == nil {
		return nil, fmt.Errorf("draft text converts to no block")
	}
	blocks, err := marshalUnescaped([]slack.Block{block})
	if err != nil {
		return nil, fmt.Errorf("encoding draft blocks: %w", err)
	}
	dest, err := draftDestinations(w.ChannelID, w.ThreadTS)
	if err != nil {
		return nil, err
	}
	return url.Values{
		"blocks":           {blocks},
		"attachments":      {"[]"},
		"destinations":     {dest},
		"file_ids":         {"[]"},
		"is_from_composer": {"false"},
	}, nil
}

func createDraftForm(w DraftWrite, clientMsgID string) (url.Values, error) {
	form, err := draftForm(w)
	if err != nil {
		return nil, err
	}
	form.Set("client_msg_id", clientMsgID)
	return form, nil
}

func updateDraftForm(draftID string, w DraftWrite) (url.Values, error) {
	form, err := draftForm(w)
	if err != nil {
		return nil, err
	}
	form.Set("draft_id", draftID)
	form.Set("client_last_updated_ts", strconv.FormatInt(w.Edited.UnixMilli(), 10))
	return form, nil
}

// draftDestinations is the one-destination list, with thread_ts and
// broadcast:false for a thread, in drafts.list's key order.
func draftDestinations(channelID, threadTS string) (string, error) {
	if threadTS == "" {
		return marshalUnescaped([]struct {
			ChannelID string `json:"channel_id"`
		}{{channelID}})
	}
	return marshalUnescaped([]struct {
		ChannelID string `json:"channel_id"`
		ThreadTS  string `json:"thread_ts"`
		Broadcast bool   `json:"broadcast"`
	}{{channelID, threadTS, false}})
}

// parseMillis reads a unix-millisecond client_last_updated_ts.
func parseMillis(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) }

// DraftRef names the draft a message is sent from, so Slack marks it
// sent with the post (chat.postMessage draft_id and
// current_draft_last_updated_ts; documented, not captured).
type DraftRef struct {
	ID            string
	LastUpdatedTS string // the draft's last_updated_ts as Slack returned it
}

func (r *DraftRef) fields() url.Values {
	if r == nil {
		return nil
	}
	return url.Values{"draft_id": {r.ID}, "current_draft_last_updated_ts": {r.LastUpdatedTS}}
}

// SendDraft posts text as SendMessage (threadTS "") or SendReply does,
// marking ref sent with it. Slack's refusal is slack.SlackErrorResponse,
// e.g. draft_already_sent: nothing was posted.
func (c *Client) SendDraft(ctx context.Context, channelID, threadTS, text string, broadcast bool, ref DraftRef) (string, string, error) {
	ts, mr, err := c.postMessage(ctx, channelID, threadTS, text, broadcast, &ref)
	if err != nil {
		return "", "", fmt.Errorf("sending message: %w", err)
	}
	return ts, mr, nil
}

// plainPostForm is chat.postMessage's fields for a message without
// chips: chipPostForm without unfurl.
func plainPostForm(channelID, threadTS string, broadcast bool, mr string, block *slack.RichTextBlock, clientMsgID string) (url.Values, error) {
	form := url.Values{
		"channel":       {channelID},
		"text":          {mr},
		"client_msg_id": {clientMsgID},
	}
	if block != nil {
		blocks, err := marshalUnescaped([]slack.Block{block})
		if err != nil {
			return nil, fmt.Errorf("encoding blocks: %w", err)
		}
		form.Set("blocks", blocks)
	}
	if threadTS != "" {
		form.Set("thread_ts", threadTS)
		if broadcast {
			form.Set("reply_broadcast", "true")
		}
	}
	return form, nil
}
