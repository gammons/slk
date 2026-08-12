package edge

import "context"

// searchCount is the result count both search endpoints ask for.
//
// Every captured search request sent 30. Do not read this as an
// edgeapi-wide constant: a sibling endpoint, users/list, was observed
// with both 20 and 30. It is what *search* was seen sending, and the
// evidence for it is two requests.
const searchCount = 30

// ChannelsSearch asks the server which channels match query, and
// returns the results alongside the ids the user is a member of.
//
// This is the endpoint that replaces walking conversations.list. The
// official web client never fetches a full channel list in any of the
// 8 captures — it searches server-side as the user types and lets the
// server rank. An enumeration is what gets a Grid account signed out
// for "data scraping"; a search is not, because it is what every
// other client on the workspace is also doing.
//
// topChannels is the caller's frecency list, sent as a ranking hint;
// the captures show 22 ids. It is optional and omitted entirely when
// empty — a null or empty array under that key is still a key on the
// wire, and no capture has one.
//
// It is sent whole and in the caller's order. Order is the entire
// content of a frecency hint, and the list is not capped: both
// captured requests carried exactly 22, so 22 is the official
// client's own list size rather than a limit it was seen negotiating.
// A caller handing this more than 22 is therefore in territory no
// capture covers — but truncating here would be equally uncovered and
// silent about it, so that call belongs to Phase 2b with a capture
// behind it, not to a slice expression.
//
// The returned []string is the top-level member_channels array, the
// same one channels/info returns and the same thing check_membership
// buys: membership without enumeration. It is a snapshot over the
// results, not a delta, and it is absent from 1 of the 2 observed
// responses — absence means empty, never an error.
//
// # Callers must debounce
//
// This is a contract, not a nicety. The capture shows two requests
// for a four-second typing session — roughly one per pause in typing,
// not one per keystroke. A finder that fired per keystroke would emit
// a request burst no human hand produces and would be a *worse*
// fingerprint than the enumeration it replaces. Phase 2b owns the
// timer (~300 ms); this comment owns the requirement.
//
// An empty query returns empty and makes no request: there is nothing
// to rank, and firing one every time the input is cleared is a shape
// the official client never produces. Whether "empty" arrives as a
// nil slice or a zero-length one is deliberately unspecified here and
// on UsersSearch — no caller can act on the difference, and pinning it
// would promote a Go representation detail into a contract no capture
// backs. Use len().
//
// Evidence: 2 observed requests, both from one capture. That is a
// much thinner base than channels/info's 18 — the payload below is
// what a single quick-switcher session did, and a second capture
// could yet show a param that varies with state the way
// current_channel does on users/search.
func (c *Client) ChannelsSearch(ctx context.Context, query string, topChannels []string) ([]Channel, []string, error) {
	if query == "" {
		return nil, nil, nil
	}
	// Deliberately not routed through fetchInfo. That helper exists
	// to split a large updated_ids map across requests; a search
	// sends one query string and asks for 30 results, so batching it
	// would mean inventing a request shape the server never sees.
	payload := map[string]any{
		"query": query,
		"count": searchCount,
		// The number 1, not true. That is what the captures carry,
		// and JSON tells the two apart.
		"fuzz":                    1,
		"include_record_channels": true,
		"check_membership":        true,
		"default_workspace":       c.teamID,
	}
	if len(topChannels) > 0 {
		payload["top_channels"] = topChannels
	}

	var resp struct {
		Results        []Channel `json:"results"`
		MemberChannels []string  `json:"member_channels"`
	}
	if err := c.call(ctx, c.teamID, "channels/search", payload, &resp); err != nil {
		return nil, nil, err
	}
	return resp.Results, resp.MemberChannels, nil
}

// UsersSearch asks the server which users match query.
//
// The users half of ChannelsSearch, with the same rationale, the same
// debounce requirement, and the same no-request-for-an-empty-query
// rule. Results come back server-ranked; preserve that order.
//
// currentChannel is the channel the user is currently viewing, which
// the server uses as a ranking signal — a member of the channel
// you are looking at is a likelier match than a stranger. It is
// caller state rather than client state, hence the parameter, and it
// is omitted when empty (the finder's first keystroke after launch
// may well have no current channel).
//
// topUsers is the frecency hint, 50 ids in both captures. Sent whole
// and in the caller's order, uncapped, for the reasons on
// ChannelsSearch — 50 is the size of the official client's list, not
// an observed ceiling.
//
// This payload is the one place this package knowingly departs from
// the plan it was built to: the plan omitted both current_channel and
// default_workspace. Both appear in 2 of 2 observed requests, so the
// captures win. Sending a subset of what the official client always
// sends is exactly the separable difference this package exists to
// remove.
//
// A users/search profile carries image_original and is_custom_image
// (42 of 60 observed results each), and both are modelled — on
// edge.User.Profile, shared with users/info. An earlier version of
// this comment added "which a users/info profile does not", and that
// was wrong: users/info carries image_original on 255 of 291 results,
// the same key at substantially the same rate. The two endpoints
// AGREE; see the measured table on edge.User in cache.go, and the
// samples[:3] fixture truncation that produced the mistake.
//
// That agreement is what lets one field on a shared type serve both
// without half-populating it, and it is pinned from both sides — see
// TestUsersSearch_DecodesProfileAvatar and
// TestUsersInfo_DecodesProfileAvatar.
//
// Evidence: 2 observed requests, both from one capture — see the same
// caveat on ChannelsSearch.
func (c *Client) UsersSearch(ctx context.Context, query, currentChannel string, topUsers []string) ([]User, error) {
	if query == "" {
		return nil, nil
	}
	// Single request, not batched — see ChannelsSearch.
	payload := map[string]any{
		"query":                      query,
		"count":                      searchCount,
		"fuzz":                       1,
		"enable_workspace_ranking":   true,
		"search_email":               true,
		"include_profile_only_users": true,
		"default_workspace":          c.teamID,
	}
	if len(topUsers) > 0 {
		payload["top_users"] = topUsers
	}
	// Gated independently of top_users: the two are unrelated pieces
	// of caller state and either can be present without the other.
	if currentChannel != "" {
		payload["current_channel"] = currentChannel
	}

	var resp struct {
		Results []User `json:"results"`
	}
	if err := c.call(ctx, c.teamID, "users/search", payload, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}
