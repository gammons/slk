package slackclient

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/debuglog"
)

// ListRecentStatuses returns the user's recent statuses, newest first,
// from users.customStatus.list: the list Slack's desktop and mobile
// clients show under "Recent". The method is undocumented; any error
// means the caller shows no recent statuses.
func (c *Client) ListRecentStatuses(ctx context.Context) ([]core.RecentStatus, error) {
	raw, err := c.postForm(ctx, "users.customStatus.list", nil)
	if err != nil {
		debuglog.General("users.customStatus.list: error=%v", err)
		return nil, err
	}
	out, err := parseRecentStatuses(raw)
	if err != nil {
		debuglog.General("users.customStatus.list: error=%v", err)
		return nil, err
	}
	return out, nil
}

// parseRecentStatuses reads a users.customStatus.list response. Only
// statuses[] is read; scheduled statuses and custom sections are not
// shown. Entries with neither text nor emoji are dropped.
func parseRecentStatuses(raw []byte) ([]core.RecentStatus, error) {
	var resp struct {
		OK       bool   `json:"ok"`
		Error    string `json:"error"`
		Statuses []struct {
			Text        string `json:"text"`
			Emoji       string `json:"emoji"`
			DateCreated int64  `json:"date_created"`
			DateExpire  int64  `json:"date_expire"`
		} `json:"statuses"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parsing users.customStatus.list: %w (body=%s)", err, truncateForLog(raw))
	}
	if !resp.OK {
		if resp.Error == "" {
			resp.Error = "not ok"
		}
		return nil, fmt.Errorf("users.customStatus.list: %s", resp.Error)
	}
	out := make([]core.RecentStatus, 0, len(resp.Statuses))
	for _, s := range resp.Statuses {
		if s.Text == "" && s.Emoji == "" {
			continue
		}
		out = append(out, core.RecentStatus{Text: s.Text, Emoji: s.Emoji, Created: s.DateCreated, Expire: s.DateExpire})
	}
	return out, nil
}
