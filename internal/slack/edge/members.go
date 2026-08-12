package edge

import (
	"context"
	"fmt"
)

// usersListFilter is the audience selector every captured users/list
// request carries, byte for byte in 4 of 4 observations.
//
// It reads like a query language and is not one this package invents
// or composes: it is a literal copied off the wire. Building it from
// parts — or exposing it as a parameter so a caller could ask for
// "everyone" — would put a string on the wire that the official
// client has never been seen sending, which is the whole class of
// divergence this package exists to remove. If a future capture shows
// a second value, add a second constant with the capture that backs
// it rather than making this configurable.
const usersListFilter = "everyone AND NOT bots AND NOT apps"

// UsersList returns the members of one channel, server-ranked.
//
// This is the channel-scoped replacement for a users.list walk. The
// distinction is the entire point: slk's ~50-page users.list sweep is
// what gets a Grid account signed out for "data scraping", and in 8
// captures of the official web client there is no workspace-wide
// member fetch at all — every member lookup it makes is scoped to a
// channel, which is what this is.
//
// count is the caller's page size. It is a parameter rather than a
// constant because the captures disagree: three requests asked for 30
// and one asked for 20, so there is no single value to pin. Keep it
// near that range. Nothing here enforces an upper bound — the
// captures record no server limit, and inventing one would be a
// guess — but a caller asking for thousands has rebuilt the
// enumeration in one request instead of fifty, and the fingerprint
// does not care which shape it arrived in.
//
// The asymmetry with the `count <= 0` check below is deliberate, not
// an oversight. A non-positive count has a knowably wrong shape —
// `count:0` is a request no capture shows and the server can only
// reject or answer uselessly — whereas the upper hazard is a
// judgement about volume with no observed threshold behind it.
// Erroring on the first is enforcing the captures; erroring on the
// second would be enforcing a number this package made up.
//
// # truncated, and the cursor that is deliberately not returned
//
// The response carried a next_marker pagination cursor in 3 of 4
// observations. All three that had one asked for 30 and got 30 back;
// the one without asked for 20 and got 4. "Present exactly when the
// page came back full" is the obvious reading of that, but note the
// denominator before relying on it: only one observation
// discriminates, because only one short page was ever seen. If the
// server ever returns a marker on a non-full page — or omits one on a
// full page — `truncated` is silently wrong, and unlike a cursor it
// gives the caller nothing to inspect and no way to notice. Treat the
// correlation as inferred from a single sample rather than promised.
//
// This returns whether that marker was present and not the marker
// itself.
//
// That is a deliberate narrowing, for two reasons.
//
// The first is evidential: no captured request carries a cursor key
// at all. The request key set is token/channels/present_first/filter/
// count in 4 of 4 — the official client received next_marker three
// times and never sent one back. So this package does not know what
// the parameter is called, and handing callers an opaque string they
// could only use by guessing a request key would be inventing a
// contract, which is the failure mode this phase exists to correct.
//
// The second is the point of the phase: following that cursor in a
// loop is enumeration. A caller looping until next_marker is empty
// reproduces the exact users.list fingerprint — a burst of sequential
// full-page fetches against one credential — with a different
// endpoint on the front of it, and the sign-out that motivated this
// work would follow just the same. Phase 2b must not do that. Not
// returning the cursor makes it something a future author has to add
// on purpose, with a capture to justify it, rather than something
// that is already sitting there in the return values.
//
// What the boolean buys is the thing a UI actually needs: whether the
// list it is about to render is the whole channel. A caller can say
// "30+ members" honestly, and reach for UsersCounts when it wants the
// real total, which is a single request rather than a walk.
//
// channelID and count are validated rather than defaulted. An empty
// channel id would put `channels:[""]` on the wire, and a
// non-positive count `count:0` — shapes no capture shows and the
// server can only reject or answer uselessly. Note this differs from
// ChannelsSearch, which returns empty for an empty query without
// erroring: an empty search box is a state a user is in constantly,
// whereas there is no moment in a channel-scoped lookup where the
// caller legitimately has no channel.
func (c *Client) UsersList(ctx context.Context, channelID string, count int) (users []User, truncated bool, err error) {
	if channelID == "" {
		return nil, false, fmt.Errorf("edge users/list: channelID is required")
	}
	if count <= 0 {
		return nil, false, fmt.Errorf("edge users/list: count must be positive, got %d", count)
	}

	// Single request, not routed through fetchInfo: that helper
	// exists to split a large updated_ids map across batches, and
	// there is nothing here to split.
	payload := map[string]any{
		// An array, always. Every observation carried exactly one id,
		// which is why this takes a single channel rather than a
		// slice — the array is the wire format, one channel is the
		// observed usage, and a multi-channel request is a shape no
		// capture backs.
		"channels":      []string{channelID},
		"present_first": true,
		"filter":        usersListFilter,
		"count":         count,
	}

	var resp struct {
		Results []User `json:"results"`
		// Read only to decide `truncated`; see the doc comment above
		// for why the value itself does not leave this function.
		NextMarker string `json:"next_marker"`
	}
	if err := c.call(ctx, c.teamID, "users/list", payload, &resp); err != nil {
		return nil, false, err
	}
	return resp.Results, resp.NextMarker != "", nil
}

// ChannelsMembership asks which of userIDs belong to one channel.
//
// The server partitions the ids it was given: every id sent came back
// in exactly one of members or non_members in all 10 observations.
// That is what makes this the channel-scoped answer to "is this
// person in here" — the caller names the people it cares about
// instead of pulling the channel's roster.
//
// Both arrays are optional on the wire and each is absent when it
// would be empty: members is missing when nobody in the batch belongs
// (1 of 10), non_members when everybody does (5 of 10). Absence means
// empty and is never an error.
//
// This is emphatically not batched. The largest observed request
// carried 66 ids in one go, so splitting it the way channels/info and
// users/info are split would emit a run of requests where the
// official client emits one.
//
// An empty userIDs makes no request: `users:[]` is a shape no capture
// shows (the smallest observed is 1) and could only come back empty
// anyway. Unlike UsersList's empty channel this is not a caller bug —
// filtering a set of ids down to none is the ordinary way to arrive
// here — so it returns empty rather than erroring.
//
// The response also echoes the channel id back. It is deliberately
// not modelled: the value is one the caller supplied a moment
// earlier, so it carries no information, and asserting on it would
// add a failure mode no observation exercises.
//
// The partition invariant is likewise observed rather than enforced.
// Nothing here checks that the two arrays cover userIDs, because a
// capture that violated it would then fail a call that the official
// client would have accepted. A caller that needs certainty about a
// specific id should check for it explicitly rather than inferring
// non-membership from absence.
func (c *Client) ChannelsMembership(ctx context.Context, channelID string, userIDs []string) (members, nonMembers []string, err error) {
	if channelID == "" {
		return nil, nil, fmt.Errorf("edge channels/membership: channelID is required")
	}
	if len(userIDs) == 0 {
		return nil, nil, nil
	}

	var resp struct {
		Members    []string `json:"members"`
		NonMembers []string `json:"non_members"`
	}
	if err := c.call(ctx, c.teamID, "channels/membership", map[string]any{
		"channel": channelID,
		"users":   userIDs,
		// Sent explicitly as false in 10 of 10 observations, not
		// omitted. An omitted key and a key carrying false are
		// different bytes on the wire, and this package's claim is
		// about the bytes.
		"as_admin": false,
	}, &resp); err != nil {
		return nil, nil, err
	}
	return resp.Members, resp.NonMembers, nil
}

// Counts is the counts object from a users/counts response.
//
// The plan this task came from expected an int. The captures say
// otherwise: 7 of 7 responses nest an object under `counts` carrying
// everyone, people, members, guests, bots, apps and by_team, with
// invited in 2 of 7 (both from boot captures). All values are ints.
//
// The fields are not modelled as derivable from one another even
// where they look like they should be — nothing in the captures says
// everyone == people + bots + apps, and asserting a relationship the
// server never promised is how a subtly wrong number gets shipped.
// Take each as what the server said.
type Counts struct {
	Everyone int `json:"everyone"`
	People   int `json:"people"`
	Members  int `json:"members"`
	Guests   int `json:"guests"`
	Bots     int `json:"bots"`
	Apps     int `json:"apps"`
	// Invited appears in only 2 of 7 observations. Expect zero.
	Invited int `json:"invited"`
	// ByTeam is the per-team split, keyed by team id. On Grid this is
	// the only field that says which workspaces a channel spans.
	ByTeam map[string]int `json:"by_team"`
}

// UsersCounts returns the membership totals for one channel.
//
// This is the cheap companion to UsersList: it answers "how many
// people are in here" in one request, without fetching anybody. A
// caller that wanted a total and reached for pagination to get it
// would be enumerating for a number that is already available here.
//
// Like ChannelsMembership, the response echoes the channel id back
// and this deliberately ignores it — the caller supplied it.
func (c *Client) UsersCounts(ctx context.Context, channelID string) (Counts, error) {
	if channelID == "" {
		return Counts{}, fmt.Errorf("edge users/counts: channelID is required")
	}

	var resp struct {
		Counts Counts `json:"counts"`
	}
	if err := c.call(ctx, c.teamID, "users/counts", map[string]any{
		"channel": channelID,
		// false explicitly, 7 of 7 — see ChannelsMembership.
		"as_admin": false,
	}, &resp); err != nil {
		return Counts{}, err
	}
	return resp.Counts, nil
}
