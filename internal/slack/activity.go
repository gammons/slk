package slackclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/gammons/slk/internal/debuglog"
)

// activityFeedTypes is the full set of activity item types slk requests
// from the internal activity.feed endpoint — the same list the official
// web client sends when bootstrapping its 🔔 Activity view.
const activityFeedTypes = "at_user,at_user_group,at_channel,at_everyone,keyword,thread_v2,message_reaction,bot_dm_bundle,dm,unjoined_channel_mention,channel,saved_reminder,list_user_mentioned"

// ActivityItem is a flattened, UI-friendly representation of a single
// activity.feed entry. The per-type extraction happens in the client so
// the UI never touches the raw (and wildly type-dependent) JSON.
type ActivityItem struct {
	Key       string // stable dedupe/selection id
	Type      string // at_user, at_channel, thread_v2, message_reaction, dm, bot_dm_bundle, ...
	IsUnread  bool
	IsBot     bool
	FeedTS    string // sort key (newest first)
	ChannelID string // resolved target channel (from message OR bundle payload)
	TS        string // resolved target message ts (may be "")
	ThreadTS  string // set for thread_v2 (== thread root)
	AuthorID  string // message author (mentions); "" when unknown
	Reaction  string // emoji short name, for message_reaction only
}

// ActivityFeedResult is one page of activity items plus the cursor for
// the next page (empty when there are no more pages).
type ActivityFeedResult struct {
	Items      []ActivityItem
	NextCursor string
}

// activityFeedResponse mirrors the activity.feed JSON envelope. Parsed
// leniently: every field is optional and unknown fields are ignored.
type activityFeedResponse struct {
	OK               bool                 `json:"ok"`
	Error            string               `json:"error"`
	Items            []json.RawMessage    `json:"items"`
	ResponseMetadata activityRespMetadata `json:"response_metadata"`
}

type activityRespMetadata struct {
	NextCursor string `json:"next_cursor"`
}

// rawActivityItem mirrors a single item. The nested `item` object's
// shape differs by `item.type`; we decode a superset and pick per type.
type rawActivityItem struct {
	IsUnread bool             `json:"is_unread"`
	IsBot    bool             `json:"is_bot"`
	FeedTS   string           `json:"feed_ts"`
	Key      string           `json:"key"`
	Item     rawActivityInner `json:"item"`
}

type rawActivityInner struct {
	Type       string               `json:"type"`
	Message    *rawActivityMessage  `json:"message"`
	Reaction   *rawActivityReaction `json:"reaction"`
	BundleInfo *rawActivityBundle   `json:"bundle_info"`
}

type rawActivityMessage struct {
	TS           string `json:"ts"`
	Channel      string `json:"channel"`
	AuthorUserID string `json:"author_user_id"`
	ThreadTS     string `json:"thread_ts"`
}

type rawActivityReaction struct {
	User string `json:"user"`
	Name string `json:"name"`
}

type rawActivityBundle struct {
	UnreadCount int                `json:"unread_count"`
	Payload     rawActivityPayload `json:"payload"`
}

type rawActivityPayload struct {
	ThreadEntry *rawThreadEntry     `json:"thread_entry"`
	DMEntry     *rawDMEntry         `json:"dm_entry"`
	Message     *rawActivityMessage `json:"message"`
}

type rawThreadEntry struct {
	ChannelID string `json:"channel_id"`
	ThreadTS  string `json:"thread_ts"`
	LatestTS  string `json:"latest_ts"`
}

type rawDMEntry struct {
	LatestMessage *rawActivityMessage `json:"latest_message"`
}

// GetActivityFeed fetches one page of the workspace's 🔔 Activity feed
// via Slack's internal activity.feed endpoint (the same call the
// official web client makes for its Activity view). Returns the
// flattened items plus the next-page cursor. Pass the previous result's
// NextCursor to page forward; unreadOnly toggles the server-side
// read/unread filter.
//
// Parses leniently: every field is optional, unknown fields are ignored,
// and a single malformed item is skipped rather than failing the page.
//
// Returns (zero, err) on network failure or ok=false JSON, mirroring
// callListThreadSubscriptions / ListThreadSubscriptions.
//
// TODO(activity): activity.markRead — marking items read is out of scope
// for the MVP; opening an item marks the underlying conversation read via
// existing paths and the feed reflects server read state on next fetch.
func (c *Client) GetActivityFeed(ctx context.Context, limit int, cursor string, unreadOnly bool) (ActivityFeedResult, error) {
	body, err := c.callActivityFeed(ctx, limit, cursor, unreadOnly)
	if err != nil {
		return ActivityFeedResult{}, err
	}
	return parseActivityFeed(body)
}

func (c *Client) callActivityFeed(ctx context.Context, limit int, cursor string, unreadOnly bool) ([]byte, error) {
	form := url.Values{}
	form.Set("limit", strconv.Itoa(limit))
	form.Set("types", activityFeedTypes)
	form.Set("mode", "chrono_v1")
	form.Set("archive_only", "false")
	form.Set("unread_only", strconv.FormatBool(unreadOnly))
	form.Set("priority_only", "false")
	form.Set("exclude_automations", "false")
	if cursor != "" {
		form.Set("cursor", cursor)
	}
	return c.postForm(ctx, "activity.feed", form)
}

// parseActivityFeed decodes an activity.feed response body into a
// flattened result. Split out from GetActivityFeed so tests can exercise
// the raw→ActivityItem mapping against the committed fixture without a
// network round-trip.
func parseActivityFeed(body []byte) (ActivityFeedResult, error) {
	var resp activityFeedResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return ActivityFeedResult{}, fmt.Errorf("parsing activity.feed: %w (body=%s)", err, truncateForLog(body))
	}
	if !resp.OK {
		return ActivityFeedResult{}, fmt.Errorf("activity.feed: %s (body=%s)", resp.Error, truncateForLog(body))
	}

	items := make([]ActivityItem, 0, len(resp.Items))
	for _, rawJSON := range resp.Items {
		// Decode each item independently so one malformed entry can't
		// abort the whole page. Slack's internal endpoints occasionally
		// return an unexpected JSON type for a field (e.g. false where an
		// object is expected); such an item is skipped, not fatal.
		var raw rawActivityItem
		if err := json.Unmarshal(rawJSON, &raw); err != nil {
			debuglog.General("activity.feed: skipping unparseable item: %v", err)
			continue
		}
		items = append(items, flattenActivityItem(raw))
	}
	return ActivityFeedResult{
		Items:      items,
		NextCursor: resp.ResponseMetadata.NextCursor,
	}, nil
}

// flattenActivityItem maps one raw item to the UI-friendly ActivityItem
// via a switch on item.type. Extraction is best-effort: an unrecognized
// or partial item still yields Key/Type/IsUnread/FeedTS so it never fails
// the page.
func flattenActivityItem(raw rawActivityItem) ActivityItem {
	out := ActivityItem{
		Key:      raw.Key,
		Type:     raw.Item.Type,
		IsUnread: raw.IsUnread,
		IsBot:    raw.IsBot,
		FeedTS:   raw.FeedTS,
	}

	switch raw.Item.Type {
	case "at_user", "at_user_group", "at_channel", "at_everyone",
		"keyword", "list_user_mentioned", "unjoined_channel_mention", "channel":
		// A mention — reference lives in item.message.
		if m := raw.Item.Message; m != nil {
			out.ChannelID = m.Channel
			out.TS = m.TS
			out.ThreadTS = m.ThreadTS
			out.AuthorID = m.AuthorUserID
		}
	case "message_reaction":
		if m := raw.Item.Message; m != nil {
			out.ChannelID = m.Channel
			out.TS = m.TS
			out.ThreadTS = m.ThreadTS
		}
		if r := raw.Item.Reaction; r != nil {
			out.Reaction = r.Name
			out.AuthorID = r.User
		}
	case "thread_v2":
		if te := payloadThreadEntry(raw.Item.BundleInfo); te != nil {
			out.ChannelID = te.ChannelID
			out.ThreadTS = te.ThreadTS
			out.TS = te.LatestTS
		}
	case "dm":
		if de := payloadDMEntry(raw.Item.BundleInfo); de != nil && de.LatestMessage != nil {
			out.ChannelID = de.LatestMessage.Channel
			out.TS = de.LatestMessage.TS
		}
	case "bot_dm_bundle":
		if m := payloadMessage(raw.Item.BundleInfo); m != nil {
			out.ChannelID = m.Channel
			out.TS = m.TS
		}
	default:
		// Unknown type: best-effort channel/ts if a message is present,
		// but never error the whole page on one weird item.
		if m := raw.Item.Message; m != nil {
			out.ChannelID = m.Channel
			out.TS = m.TS
			out.ThreadTS = m.ThreadTS
			out.AuthorID = m.AuthorUserID
		}
		if raw.Item.Type == "" {
			debuglog.Backfill("GetActivityFeed: item %q has empty type", raw.Key)
		}
	}
	return out
}

func payloadThreadEntry(b *rawActivityBundle) *rawThreadEntry {
	if b == nil {
		return nil
	}
	return b.Payload.ThreadEntry
}

func payloadDMEntry(b *rawActivityBundle) *rawDMEntry {
	if b == nil {
		return nil
	}
	return b.Payload.DMEntry
}

func payloadMessage(b *rawActivityBundle) *rawActivityMessage {
	if b == nil {
		return nil
	}
	return b.Payload.Message
}
