package slackclient

// Link chips: a link to something an app publishes as a Slack Work Object
// (a Linear issue, for one) goes out as an attachment_mention element, a
// chip showing the object's title, the way Slack's web client sends it.
//
// The web client builds the chip in its composer: it calls
// chat.unfurlLink for the pasted URL, whose reply only says the app's
// preview is pending; a couple of seconds later the WebSocket delivers
// unfurl_preview_updated for the same client_msg_id, carrying the
// work_object_entity the chip is made from; chat.postMessage then sends
// the chip in the rich_text block, plus unfurl=[{"url":…}]. slk does the
// same at send time (SendMessage, SendReply), waiting at most
// linkChipWait. Any link that gets no Work Object in that time goes out
// as the plain link it always was. Captures: testdata/chat_unfurl_link.json,
// ws_unfurl_preview_updated.json, chat_post_message_chip.json.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/slack/mrkdwn"
	"github.com/gammons/slk/internal/slackhttp"
	"github.com/slack-go/slack"
)

// linkChipWait is how long a send waits for the apps to publish previews
// of its links. The web client's capture shows about 2.5s from paste to
// chip. A var so tests can shorten it. A send with links can take this
// long before chat.postMessage starts; internal/ui's BeginSend keeps
// suppressing the echo of a send until it answers (selfSendPendingLimit).
var linkChipWait = 3 * time.Second

// maxLinkChips caps the chat.unfurlLink calls one send makes. Links past
// it go out plain.
const maxLinkChips = 5

// linkChip is one attachment_mention element. Field order is the wire
// order the web client sends.
type linkChip struct {
	URL         string `json:"url"`
	Title       string `json:"text"`
	EntityID    string `json:"entity_id"`
	AppID       string `json:"app_id"`
	IconURL     string `json:"icon_url,omitempty"`
	IconName    string `json:"icon_name,omitempty"`
	ProductName string `json:"product_name,omitempty"`
}

// element is the chip as a rich_text section element. slack-go has no
// type for it; an unknown element marshals back to its raw JSON.
func (ch linkChip) element() (*slack.RichTextSectionUnknownElement, error) {
	raw, err := marshalUnescaped(struct {
		Type string `json:"type"`
		linkChip
	}{"attachment_mention", ch})
	if err != nil {
		return nil, err
	}
	return &slack.RichTextSectionUnknownElement{Type: "attachment_mention", Raw: raw}, nil
}

// marshalUnescaped is json.Marshal without HTML escaping, so <, > and &
// in titles and URLs go out as themselves, as the web client sends them.
func marshalUnescaped(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

// chipEligible reports a link the web client would make a chip of: a bare
// http(s) URL as typed or pasted, unstyled. A link with its own label, or
// inside bold or code, stays what the user wrote.
func chipEligible(l *slack.RichTextSectionLinkElement) bool {
	return l.Style == nil &&
		(l.Text == "" || l.Text == l.URL) &&
		(strings.HasPrefix(l.URL, "https://") || strings.HasPrefix(l.URL, "http://"))
}

// eachSectionElements calls fn with every inline element list in block
// that can hold a link: sections (also inside lists) and quotes.
// Preformatted blocks are code and are skipped.
func eachSectionElements(block *slack.RichTextBlock, fn func([]slack.RichTextSectionElement)) {
	if block == nil {
		return
	}
	var walk func([]slack.RichTextElement)
	walk = func(els []slack.RichTextElement) {
		for _, el := range els {
			switch e := el.(type) {
			case *slack.RichTextSection:
				fn(e.Elements)
			case *slack.RichTextQuote:
				fn(e.Elements)
			case *slack.RichTextList:
				walk(e.Elements)
			}
		}
	}
	walk(block.Elements)
}

// chipCandidates returns the distinct URLs in block that could become
// chips, in order, at most maxLinkChips.
func chipCandidates(block *slack.RichTextBlock) []string {
	var urls []string
	seen := map[string]bool{}
	eachSectionElements(block, func(els []slack.RichTextSectionElement) {
		for _, el := range els {
			l, ok := el.(*slack.RichTextSectionLinkElement)
			if !ok || !chipEligible(l) || seen[l.URL] || len(urls) >= maxLinkChips {
				continue
			}
			seen[l.URL] = true
			urls = append(urls, l.URL)
		}
	})
	return urls
}

// applyChips replaces, in place, every eligible link to a URL in chips
// with its chip.
func applyChips(block *slack.RichTextBlock, chips map[string]linkChip) {
	eachSectionElements(block, func(els []slack.RichTextSectionElement) {
		for i, el := range els {
			l, ok := el.(*slack.RichTextSectionLinkElement)
			if !ok || !chipEligible(l) {
				continue
			}
			chip, ok := chips[l.URL]
			if !ok {
				continue
			}
			e, err := chip.element()
			if err != nil {
				continue
			}
			els[i] = e
		}
	})
}

// chipMrkdwn rewrites each chip's link in the mrkdwn fallback as
// <url|title>, which is what notifications and slk's own optimistic row
// (it drops the WebSocket echo of its own send) show. The link is <url>,
// or <url|url> when the user labelled it with itself (chipEligible takes
// both). One written literally inside a code span is rewritten too; only
// the fallback text is affected, the blocks are exact.
func chipMrkdwn(mr string, chips map[string]linkChip) string {
	esc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	for u, chip := range chips {
		titled := "<" + u + "|" + esc.Replace(chip.Title) + ">"
		mr = strings.ReplaceAll(mr, "<"+u+">", titled)
		mr = strings.ReplaceAll(mr, "<"+u+"|"+u+">", titled)
	}
	return mr
}

// newClientMsgID returns a random (version 4) UUID, the shape of the web
// client's client_msg_id.
func newClientMsgID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// unfurlAttachment is the part of one chat.unfurlLink /
// unfurl_preview_updated attachment a chip needs.
type unfurlAttachment struct {
	IsAppUnfurl      bool `json:"is_app_unfurl"`
	IsPreviewPending bool `json:"is_preview_pending"`
	WorkObject       *struct {
		EntityID     string `json:"entity_id"`
		AppID        string `json:"app_id"`
		ProductName  string `json:"product_name"`
		SlugIconName string `json:"slug_icon_name"`
		AppIcons     struct {
			Image36 struct {
				ImageURL string `json:"image_url"`
			} `json:"image_36"`
		} `json:"app_icons"`
		Layouts struct {
			Expanded struct {
				Title struct {
					Text string `json:"text"`
				} `json:"title"`
			} `json:"expanded"`
		} `json:"layouts"`
	} `json:"work_object_entity"`
}

// attachmentFor picks u's attachment: the one keyed by u, else the only
// one (one URL is looked up per client_msg_id, so a lone entry under a
// differently written key is still the answer).
func attachmentFor(atts map[string]unfurlAttachment, u string) (unfurlAttachment, bool) {
	if a, ok := atts[u]; ok {
		return a, true
	}
	if len(atts) == 1 {
		for _, a := range atts {
			return a, true
		}
	}
	return unfurlAttachment{}, false
}

// parseUnfurlPreview reads an unfurl_preview_updated frame for u. It is a
// chip only if the app published a Work Object with an id and a title.
func parseUnfurlPreview(frame []byte, u string) (linkChip, bool) {
	var evt struct {
		Attachments map[string]unfurlAttachment `json:"attachments"`
	}
	if err := json.Unmarshal(frame, &evt); err != nil {
		return linkChip{}, false
	}
	a, ok := attachmentFor(evt.Attachments, u)
	if !ok || a.WorkObject == nil {
		return linkChip{}, false
	}
	wo := a.WorkObject
	chip := linkChip{
		URL:         u,
		Title:       wo.Layouts.Expanded.Title.Text,
		EntityID:    wo.EntityID,
		AppID:       wo.AppID,
		IconURL:     wo.AppIcons.Image36.ImageURL,
		IconName:    wo.SlugIconName,
		ProductName: wo.ProductName,
	}
	if chip.Title == "" || chip.EntityID == "" || chip.AppID == "" {
		return linkChip{}, false
	}
	return chip, true
}

// unfurlLinkForm is chat.unfurlLink's business fields (postForm adds the
// token, slackhttp the _x_* envelope).
func unfurlLinkForm(channelID, u, clientMsgID string) url.Values {
	return url.Values{
		"url":           {u},
		"channel":       {channelID},
		"client_msg_id": {clientMsgID},
	}
}

// parseUnfurlLink reports whether chat.unfurlLink's reply says an app's
// preview of u is on its way over the WebSocket. Anything else (a refusal,
// no app unfurl) means no chip, so there is nothing to wait for.
func parseUnfurlLink(raw []byte, u string) bool {
	var resp struct {
		OK          bool                        `json:"ok"`
		Attachments map[string]unfurlAttachment `json:"attachments"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || !resp.OK {
		return false
	}
	a, ok := attachmentFor(resp.Attachments, u)
	return ok && a.IsAppUnfurl && a.IsPreviewPending
}

// unfurlWaiters routes unfurl_preview_updated frames from the WebSocket
// event owner to the lookups waiting for them, by client_msg_id. The zero
// value is ready to use.
type unfurlWaiters struct {
	mu sync.Mutex
	m  map[string]chan []byte
}

// wait registers a lookup for id; the frame arrives on the channel.
// Register before calling chat.unfurlLink: the frame can beat its reply.
func (w *unfurlWaiters) wait(id string) <-chan []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.m == nil {
		w.m = map[string]chan []byte{}
	}
	ch := make(chan []byte, 1)
	w.m[id] = ch
	return ch
}

func (w *unfurlWaiters) cancel(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.m, id)
}

// deliver takes frame if it is an unfurl_preview_updated, handing it to
// the lookup waiting for its client_msg_id, if any. It never blocks: it
// runs on the event owner. Reports whether the frame was taken.
func (w *unfurlWaiters) deliver(frame []byte) bool {
	if !bytes.Contains(frame, []byte(`"unfurl_preview_updated"`)) {
		return false
	}
	var evt struct {
		Type        string `json:"type"`
		ClientMsgID string `json:"client_msg_id"`
	}
	if err := json.Unmarshal(frame, &evt); err != nil || evt.Type != "unfurl_preview_updated" {
		return false
	}
	w.mu.Lock()
	ch := w.m[evt.ClientMsgID]
	w.mu.Unlock()
	if ch != nil {
		select {
		case ch <- frame:
		default:
		}
	}
	return true
}

// lookupLinkChips asks Slack for a preview of each URL, concurrently, and
// returns the chips that arrive within linkChipWait, keyed by URL. Nil
// when there is nothing to look up or no WebSocket to hear the answer on.
func (c *Client) lookupLinkChips(ctx context.Context, channelID string, urls []string) map[string]linkChip {
	if len(urls) == 0 || !c.wsLive.Load() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, linkChipWait)
	defer cancel()
	type result struct {
		chip linkChip
		ok   bool
	}
	results := make(chan result, len(urls))
	for _, u := range urls {
		go func(u string) {
			chip, ok := c.lookupLinkChip(ctx, channelID, u)
			results <- result{chip, ok}
		}(u)
	}
	chips := map[string]linkChip{}
	for range urls {
		if r := <-results; r.ok {
			chips[r.chip.URL] = r.chip
		}
	}
	return chips
}

func (c *Client) lookupLinkChip(ctx context.Context, channelID, u string) (linkChip, bool) {
	id := newClientMsgID()
	frames := c.unfurls.wait(id)
	defer c.unfurls.cancel(id)
	raw, err := c.postForm(slackhttp.WithReason(ctx, "fetch-unfurl-preview"), "chat.unfurlLink", unfurlLinkForm(channelID, u, id))
	if err != nil {
		debuglog.General("chat.unfurlLink: error=%v", err)
		return linkChip{}, false
	}
	if !parseUnfurlLink(raw, u) {
		return linkChip{}, false
	}
	select {
	case frame := <-frames:
		chip, ok := parseUnfurlPreview(frame, u)
		debuglog.General("chat.unfurlLink: preview arrived, chip=%v", ok)
		return chip, ok
	case <-ctx.Done():
		debuglog.General("chat.unfurlLink: no preview within %s", linkChipWait)
		return linkChip{}, false
	}
}

// chipPostForm is chat.postMessage's business fields for a message with
// chips; chipURLs is the unfurl list, in message order.
func chipPostForm(channelID, threadTS string, broadcast bool, mr string, block *slack.RichTextBlock, chipURLs []string, clientMsgID string) (url.Values, error) {
	blocks, err := marshalUnescaped([]slack.Block{block})
	if err != nil {
		return nil, fmt.Errorf("encoding blocks: %w", err)
	}
	type unfurl struct {
		URL string `json:"url"`
	}
	list := make([]unfurl, 0, len(chipURLs))
	for _, u := range chipURLs {
		list = append(list, unfurl{u})
	}
	unfurls, err := marshalUnescaped(list)
	if err != nil {
		return nil, fmt.Errorf("encoding unfurl: %w", err)
	}
	form := url.Values{
		"channel":       {channelID},
		"text":          {mr},
		"blocks":        {blocks},
		"unfurl":        {unfurls},
		"client_msg_id": {clientMsgID},
	}
	if threadTS != "" {
		form.Set("thread_ts", threadTS)
		if broadcast {
			form.Set("reply_broadcast", "true")
		}
	}
	return form, nil
}

// postChips sends a message carrying chips. Slack's refusal comes back as
// slack.SlackErrorResponse, as slack-go's PostMessage returns it, and a
// 429 as *slack.RateLimitedError.
func (c *Client) postChips(ctx context.Context, form url.Values) (string, error) {
	raw, err := c.postForm(slackhttp.WithReason(ctx, "webapp_message_send"), "chat.postMessage", form)
	if err != nil {
		var rl *rateLimitError
		if errors.As(err, &rl) {
			return "", &slack.RateLimitedError{RetryAfter: rl.retryAfter}
		}
		return "", err
	}
	var resp struct {
		OK    bool   `json:"ok"`
		TS    string `json:"ts"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("parsing chat.postMessage: %w (body=%s)", err, truncateForLog(raw))
	}
	if !resp.OK {
		return "", slack.SlackErrorResponse{Err: resp.Error}
	}
	return resp.TS, nil
}

// sendWithChips sends text with every link that has a Work Object as a
// chip; block is text's conversion, read for the links and left alone.
// sent is false when no link got a chip, and the caller sends block the
// ordinary way; also when Slack refused the chips as invalid_blocks, in
// which case nothing was posted.
func (c *Client) sendWithChips(ctx context.Context, channelID, threadTS, text string, broadcast bool, block *slack.RichTextBlock) (ts, mr string, sent bool, err error) {
	urls := chipCandidates(block)
	chips := c.lookupLinkChips(ctx, channelID, urls)
	if len(chips) == 0 {
		return "", "", false, nil
	}
	mr, block = mrkdwn.Convert(text)
	applyChips(block, chips)
	mr = chipMrkdwn(mr, chips)
	var chipURLs []string
	for _, u := range urls {
		if _, ok := chips[u]; ok {
			chipURLs = append(chipURLs, u)
		}
	}
	form, err := chipPostForm(channelID, threadTS, broadcast, mr, block, chipURLs, newClientMsgID())
	if err != nil {
		return "", "", true, err
	}
	ts, err = c.postChips(ctx, form)
	var refusal slack.SlackErrorResponse
	if errors.As(err, &refusal) && refusal.Err == "invalid_blocks" {
		debuglog.General("chat.postMessage: chips refused (invalid_blocks), sending plain links")
		return "", "", false, nil
	}
	return ts, mr, true, err
}
