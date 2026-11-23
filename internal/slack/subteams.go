package slackclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/slack-go/slack"
)

// ErrUserGroupNotFound is returned by GetUserGroupInfo when Slack has no
// member-workspace usergroup with that ID for this session. Org-level
// groups answer this too: those come from usergroups.list instead.
var ErrUserGroupNotFound = errors.New("usergroup not found")

// UserGroupInfo is the subset of a subteams.info entry slk renders.
type UserGroupInfo struct {
	ID     string `json:"id"`
	TeamID string `json:"team_id"`
	Name   string `json:"name"`
	Handle string `json:"handle"`
}

// GetUserGroupInfo resolves one usergroup by ID via subteams.info, the
// undocumented endpoint the Slack web client uses to render a bare
// <!subteam^S…> mention. On an Enterprise Grid org-level session,
// usergroups.list returns only the org's own groups, whatever team_id it
// is given, so groups owned by a member workspace are reachable only
// here, one ID per call. An HTTP 429 comes back as a
// *slack.RateLimitedError carrying Slack's Retry-After, so callers can
// wait it out (WaitOutRateLimit).
func (c *Client) GetUserGroupInfo(ctx context.Context, id string) (UserGroupInfo, error) {
	raw, err := c.postForm(ctx, "subteams.info", url.Values{"usergroup": {id}})
	if err != nil {
		var rl *rateLimitError
		if errors.As(err, &rl) {
			return UserGroupInfo{}, &slack.RateLimitedError{RetryAfter: rl.retryAfter}
		}
		return UserGroupInfo{}, err
	}
	var resp struct {
		OK       bool            `json:"ok"`
		Error    string          `json:"error"`
		Subteams []UserGroupInfo `json:"subteams"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return UserGroupInfo{}, fmt.Errorf("decoding subteams.info: %w", err)
	}
	if !resp.OK {
		if resp.Error == "subteam_not_found" {
			return UserGroupInfo{}, ErrUserGroupNotFound
		}
		return UserGroupInfo{}, fmt.Errorf("subteams.info: %s", resp.Error)
	}
	for _, g := range resp.Subteams {
		if g.ID == id {
			return g, nil
		}
	}
	return UserGroupInfo{}, ErrUserGroupNotFound
}
