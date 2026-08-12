package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// ViewMethod is the API method name, used as the map key slackhttp
// looks up for the _x_reason and _x_mode exclusions. conversations.view
// is in BOTH exclusion sets: the official client sends neither flag on
// it (internal/slackhttp/reason.go, internal/slackhttp/mode.go).
const ViewMethod = "conversations.view"

// viewCount is the `count` param, byte for byte what the official
// client sends in both captures.
//
// It is a string because this is a form body, and it is 28 because
// that is what was measured — not a round number someone liked. slk's
// own history fetches use different limits; matching the real client
// here is the entire point, so this constant must not be "tuned".
const viewCount = "28"

// UserProfile is the subset of a conversations.view users[] entry's
// `profile` that slk renders.
//
// Deliberately NOT boot.SelfProfile, even though the two overlap
// heavily. SelfProfile carries Email, and the 21 profile keys observed
// on a view users[] entry do not include `email` — reusing SelfProfile
// would hand every user an Email that is "" for structural reasons,
// which reads as evidence ("this user has no email") when it is only
// an artifact of the wrong type. That is the same mistake the comment
// on boot.Channel's missing IsIM documents.
type UserProfile struct {
	RealName    string `json:"real_name"`
	DisplayName string `json:"display_name"`
	AvatarHash  string `json:"avatar_hash"`
	// ImageOriginal is the only absolute avatar URL in this object.
	ImageOriginal    string `json:"image_original"`
	StatusText       string `json:"status_text"`
	StatusEmoji      string `json:"status_emoji"`
	StatusExpiration int64  `json:"status_expiration"`
}

// User is one entry in conversations.view's `users` array — the
// authors the returned history references, already resolved.
//
// This array is why the endpoint is worth having: it replaces the
// per-author users.info fan-out slk fires after every history fetch,
// which on a busy channel is up to `count` extra requests for one
// channel open.
//
// A deliberate subset of the 20 observed keys. The modelled set is
// exactly edge.User's (id, name, team_id, updated, deleted, is_bot,
// profile.{display_name,real_name}) plus is_app_user, real_name and
// the avatar/status profile fields — edge.User is the existing shape
// slk's user cache is fed from, and cache.User.IsBot is documented as
// the union of is_bot and is_app_user, so both are needed to fill it.
//
// The six remaining booleans (is_admin, is_owner, is_primary_owner,
// is_restricted, is_ultra_restricted, is_email_confirmed) and the
// three remaining scalars (color, tz, tz_label, tz_offset,
// who_can_share_contact_card) have no consumer in slk. Adding one is a
// two-line change; inventing a consumer for it is not.
type User struct {
	ID     string `json:"id"`
	TeamID string `json:"team_id"`
	Name   string `json:"name"`
	// RealName is the top-level `real_name`, which is a DIFFERENT key
	// from profile.real_name and can hold a different value. Both are
	// modelled, and the fixture gives them different values, because a
	// tag pointing at the wrong one still compiles and decodes.
	RealName string `json:"real_name"`
	// Version is the `updated` stamp, same opaque-monotonic-version
	// semantics as boot.Channel.Version.
	Version int64 `json:"updated"`

	Deleted bool `json:"deleted"`
	// IsBot and IsAppUser are both needed: cache.User.IsBot is their
	// union (classic bots set is_bot, Slack apps set is_app_user), and
	// it decides whether a DM lands in the "Apps" sidebar section.
	IsBot     bool `json:"is_bot"`
	IsAppUser bool `json:"is_app_user"`

	Profile UserProfile `json:"profile"`
}

// MutationTimestamps is history.mutation_timestamps, returned because
// the request sets include_mutation_timestamps=true.
//
// ALL THREE ARE STRINGS. They are 17-character values that look
// exactly like integers ("1783337533019174"), and an int64 field here
// does not truncate or round — it fails the whole response decode, so
// getting this wrong loses the channel open entirely. Same trap as
// boot.Result.EmojiCacheTS.
//
// The values are cache tokens to be echoed back verbatim, so string is
// also the correct semantic type, not merely the tolerant one.
type MutationTimestamps struct {
	Latest         string `json:"latest"`
	Updated        string `json:"updated"`
	HistoryInvalid string `json:"history_invalid"`
}

// History is conversations.view's `history` object — the same payload
// a conversations.history call returns, delivered as part of the
// channel-open response instead of as a separate request.
type History struct {
	// Messages is the substance of the whole call: `count` messages of
	// channel history, and the reason any of the other sections are
	// here at all (users, bots, channels and emojis are all "things
	// these messages reference").
	//
	// []json.RawMessage, not a message struct, and that is a
	// Phase-2a-scoped decision with three reasons:
	//
	//  1. Nothing consumes them yet. This package is parser-only and
	//     unwired; Phase 2b is what feeds them to the renderer.
	//  2. slk models messages as slack-go's slack.Message
	//     (internal/slack/client.go). json.Unmarshal from a
	//     RawMessage into a slack.Message is lossless and one line, so
	//     deferring costs the wiring phase nothing — whereas defining
	//     a *second* message type here would create two shapes that
	//     have to be kept in agreement forever.
	//  3. The message shape VARIES. Across the 56 messages in the two
	//     captures there are 17 distinct keys, and only 8 are on all
	//     56 (user, type, ts, client_msg_id, text, team, blocks,
	//     reactions). The other 9 — thread_ts, reply_count,
	//     reply_users, reply_users_count, latest_reply, subscribed,
	//     is_locked, edited, attachments — appear on 4 to 16 of them.
	//     Declaring a struct from that is inventing a contract, which
	//     is the failure this phase exists to correct.
	//
	//     An earlier version of this comment said "only 8 message keys
	//     were captured", which was true of the committed extract
	//     (messages[0] of one sample) and false of the captures. The
	//     conclusion is unchanged and if anything better supported: 17
	//     keys of which 9 are optional is a stronger argument for raw
	//     bytes than 8 fixed ones. Only the evidence was wrong — a
	//     per-field claim about an array element needs a denominator.
	//
	// Honest cost, stated so it is a choice and not an accident:
	// raw bytes mean a message whose shape slk cannot handle fails
	// later, at render, instead of here at decode. That is a
	// deliberate trade — it also means one unexpected message cannot
	// fail the entire channel open, which for a 28-message batch is
	// the safer default.
	Messages []json.RawMessage `json:"messages"`

	HasMore            bool               `json:"has_more"`
	MutationTimestamps MutationTimestamps `json:"mutation_timestamps"`

	// ChannelActionsTS was `null` in 2/2 captures, so its non-null
	// type is entirely unknown — the name suggests a Slack ts, which
	// would make it a string, but "suggests" is not evidence. Raw
	// bytes claim nothing, tolerate the null, and preserve whatever a
	// future capture turns out to hold.
	//
	// Note the distinction json.RawMessage preserves and a typed field
	// would destroy: `null` on the wire decodes to the four bytes
	// "null", an absent key decodes to nil. "The server said null" and
	// "the server did not mention it" are different facts.
	ChannelActionsTS json.RawMessage `json:"channel_actions_ts"`

	ChannelActionsCount int `json:"channel_actions_count"`

	// NextTS is an INT in both captures, not the string ts Slack uses
	// almost everywhere else (compare MutationTimestamps above, which
	// are strings that look like ints — this response contains both
	// traps, in adjacent fields). A string field here fails the
	// decode.
	NextTS int64 `json:"next_ts"`
}

// ViewChannelEntry is one entry in conversations.view's `channels[]`
// array — the conversations the returned messages reference.
//
// It embeds Channel for the 27 keys this array shares with userBoot's
// channels[], and adds the five that only conversations.view sends.
// Measured across all 54 entries in the two captures:
//
//	is_member             54/54
//	last_read             14/54
//	latest                14/54
//	unread_count          14/54
//	unread_count_display  14/54
//
// The five are HERE and not on Channel deliberately. userBoot's
// channels[] entries carry none of them — 0 of 110 observed elements —
// so putting them on the shared type would give every userBoot channel
// five fields that decode zero forever and read as meaningful, which
// is the exact mistake edge.Channel's missing IsMember documents at
// length. The traffic runs the other way too: userBoot's entries carry
// is_frozen (110/110) and members (4/110), which view's never do, and
// neither is modelled anywhere for the same reason in reverse.
//
// Why the four unread fields are worth modelling at all: without them
// ViewResult.Channels discards the unread state of 26% of the entries
// it is handed, and a caller that wants those counts has to make a
// second call for numbers that arrived in this response.
type ViewChannelEntry struct {
	Channel

	// IsMember is on every observed entry, and its 14-true-of-54 split
	// is real data rather than a structural constant — unlike
	// edge.Channel, where membership does not travel on the result at
	// all.
	IsMember bool `json:"is_member"`

	// LastRead is a ts string, as on ViewChannel.
	LastRead string `json:"last_read"`

	// Latest is a whole message OBJECT here, not the ts string the
	// top-level `channel` object carries under this same key: 14/14
	// entries that have it send an object, 2/2 `channel` objects send
	// a string. That divergence is why this type exists instead of
	// ViewChannel being reused for the array — a `Latest string` field
	// does not merely read wrong, it fails the entire response decode
	// and loses the channel open.
	//
	// json.RawMessage for the same three reasons as History.Messages:
	// nothing consumes it yet, a second message struct would be a
	// shape to keep in agreement with slack.Message forever, and raw
	// bytes claim nothing about a value shape the captures show in two
	// different forms. It also preserves the absent/null/present
	// distinction, which matters here because 40 of 54 entries omit
	// the key entirely.
	Latest json.RawMessage `json:"latest"`

	// UnreadCount and UnreadCountDisplay are ints. They were EQUAL on
	// all 14 observed entries that carry them, so the captures do not
	// separate the two — the fixture gives them distinct values on
	// purpose, because same-typed adjacent fields sharing a value are
	// freely swappable and no assertion can tell them apart.
	UnreadCount        int `json:"unread_count"`
	UnreadCountDisplay int `json:"unread_count_display"`
}

// ViewChannel is conversations.view's top-level `channel` object: the
// conversation that was actually opened.
//
// It embeds Channel because this object is a strict SUPERSET of a
// channels[] entry: measured across both captures, the entry's 32
// distinct keys are all present among this object's 34. The set
// difference in the other direction is empty, and the 2 keys this
// object uniquely adds are is_read_only and is_thread_only. That is
// reuse of one Slack conversation shape, not a coincidence worth
// duplicating.
//
// The superset holds for key NAMES only, and one type differs:
// `latest` is a ts string here (2/2) and a message object on a
// channels[] entry (14/14). See ViewChannelEntry.Latest — that single
// disagreement is why the array gets its own type rather than this
// one.
//
// Embedding also buys the boolean tags real mutation coverage for
// free: those tags are pinned by the channels[] assertions, which have
// four rows and so can give every boolean a unique non-zero column
// vector. A standalone copy of them here could not be pinned at all,
// because there is exactly one `channel` object per response, and two
// booleans sharing a value in a single object are freely swappable.
//
// is_read_only and is_thread_only are left out for that same pinning
// reason — both were true-and-false respectively in 2/2 captures, in a
// single object per response, so a swapped tag between them would
// survive any assertion this package can write. See the same argument
// on boot.Self. is_member is left off for the same reason and is
// modelled on ViewChannelEntry instead, where 54 rows can pin it.
type ViewChannel struct {
	Channel

	// LastRead and Latest are string ts values, not ints. Note that
	// Latest is a STRING on this object and a message object on a
	// channels[] entry; ViewChannelEntry.Latest is the other half.
	LastRead string `json:"last_read"`
	Latest   string `json:"latest"`

	UnreadCount        int `json:"unread_count"`
	UnreadCountDisplay int `json:"unread_count_display"`
}

// ViewResponseMetadata is conversations.view's `response_metadata`.
//
// The plan's spec listed six top-level keys and missed this one and
// `channel`. It is modelled because next_cursor is how a caller pages
// further back through history without starting a second endpoint's
// pagination scheme.
type ViewResponseMetadata struct {
	NextCursor string `json:"next_cursor"`
}

// ViewResult is everything one conversations.view call learned.
//
// The point of the endpoint is that these sections arrive together:
// slk's current channel open is one conversations.history, then a
// users.info per distinct author, then emoji.list. That is a burst of
// up to ~30 requests per channel opened, and burst request volume is
// exactly what Grid's anomaly detection scores.
type ViewResult struct {
	// History replaces the conversations.history call.
	History History `json:"history"`

	// Users replaces the per-author users.info fan-out. 5 entries in
	// both captures.
	Users []User `json:"users"`

	// Bots is []json.RawMessage because it was `[]` in BOTH captures,
	// so there is no evidence whatsoever for what an entry looks like.
	// A struct here would be an invented contract; raw bytes claim
	// nothing and lose nothing. Same reasoning as boot.Subteams.Self
	// and boot.Result.Starred. Give it a real type when a capture with
	// a non-empty bots list exists.
	//
	// It plausibly mirrors Users for bot authors — plausibly is not
	// evidence.
	Bots []json.RawMessage `json:"bots"`

	// Channels are the conversations the returned messages reference
	// (channel mentions and the like), pre-resolved. 27 entries in
	// both captures.
	//
	// ViewChannelEntry, not Channel: a view channels[] entry carries
	// five keys a userBoot one does not, four of them unread state
	// that would otherwise be thrown away and refetched.
	Channels []ViewChannelEntry `json:"channels"`

	// Emojis replaces the emoji.list call. It is an OBJECT keyed by
	// emoji name with a URL value — NOT an array, despite being the
	// plural of a thing. Modelling it as a slice fails the whole
	// decode. Same shape trap as boot.Subteams, in the other
	// direction.
	Emojis map[string]string `json:"emojis"`

	// Channel is the conversation that was opened.
	//
	// Channel.ID is the single most load-bearing field this parser
	// exposes, and the plan's spec omitted it entirely. See
	// ConversationsView: the `channel` request param is verified on
	// two non-Grid workspaces as of 2026-08-01 and remains UNVERIFIED
	// on Enterprise Grid, and the captured requests sent none and got
	// back whatever conversation the user last looked at. Channel.ID
	// is how a caller finds out which conversation it actually got,
	// and therefore whether the probe worked or it must fall back.
	Channel ViewChannel `json:"channel"`

	ResponseMetadata ViewResponseMetadata `json:"response_metadata"`
}

// viewResponse is ViewResult plus the two envelope fields every Slack
// Web API answer carries. They are kept off ViewResult because a
// caller never sees a ViewResult unless ok was true, so exposing them
// would only invite a second, redundant check. Same split as
// boot.response.
type viewResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	ViewResult
}

// ConversationsView calls conversations.view through post and returns
// the parsed result.
//
// This is the official client's channel-open call. One request returns
// the history, the users and bots that authored it, the channels it
// mentions and the custom emoji it uses — replacing slk's
// conversations.history plus a users.info per distinct author plus
// emoji.list.
//
// # The channel param: verified off Grid, unverified on it
//
// Read this before relying on channelID.
//
// Both HAR captures of this endpoint carried NO `channel` param at
// all, and the response was the conversation the user had last
// viewed. slk needs a specific conversation, so this function sends
// `channel` when channelID is non-empty and omits it entirely when
// channelID is empty, reproducing the captured request byte for byte
// in the latter case.
//
// VERIFIED 2026-08-01, and what that does and does not settle:
//
//   - Known: two live NON-GRID workspaces both honoured the param.
//     Each boot opened a specific channel and got that channel back —
//     ViewResult.Channel.ID equalled the requested id, and neither run
//     logged the caller's "ignored the channel param" fallback line.
//     Until then this was an unmeasured assumption, however obvious
//     the parameter name.
//   - NOT known: Enterprise Grid. conversations.view has never been
//     captured there under any parameters, and Grid is the environment
//     this package exists for. Two observations off Grid say nothing
//     about the method existing on it, let alone about this param.
//
// So the caller's probe-and-compare stays mandatory, unchanged. Two
// samples on the easy environment are not a contract, and the failure
// they would miss is the silent one below.
//
// Callers MUST be able to detect the assumption failing, and there
// are two distinct failure modes:
//
//   - Loud: Slack rejects the param and answers ok:false. This returns
//     an error and no data.
//   - SILENT, and the dangerous one: Slack IGNORES the unknown param
//     and answers with the last-viewed conversation, ok:true, a
//     perfectly well-formed body full of the wrong channel's messages.
//     No error can be raised for this from inside the parser.
//
// The second is why ViewResult.Channel.ID is exposed. A caller
// verifies the probe by comparing it to the channelID it asked for; if
// they differ, the param is not honoured on this workspace and the
// result must be discarded. The verified fallback is conversations.history
// with limit=28 and cached_latest_updates (Task 9). Phase 2b probes
// once, checks Channel.ID, and falls back for the session on either
// failure mode.
//
// # Envelope
//
// ctx is passed through untouched, and this function sets no _x_
// param. In particular it does NOT call slackhttp.WithReason:
// conversations.view is in slackhttp's xReasonExcludedMethods AND its
// xModeExcludedMethods, because the official client sends neither flag
// on it. _x_sonic and _x_app_name are added by
// slackhttp.BrowserTransport. Adding any of them here would put a
// duplicate on the wire — and a caller-supplied form param is the ONE
// route by which a suppressed _x_reason could come back, since
// applyEnvelopeBody only ever appends and never removes what the
// caller sent.
//
// # Errors
//
// Any error returns a nil ViewResult. That is load-bearing, not
// tidiness: encoding/json populates a struct as it goes and keeps
// decoding past the first type error, and `ok` is only inspected after
// the whole body has been decoded — so at both failure points there is
// a fully populated ViewResult sitting in a local. Handing it back
// would give the caller a plausible-looking channel built from a
// response the server rejected or the decoder could not read.
func ConversationsView(ctx context.Context, post PostFunc, channelID string) (*ViewResult, error) {
	// Strings, not booleans: this is a form body. Every value below
	// was read off both captures and is byte-identical between them.
	form := url.Values{
		"canonical_avatars":                {"true"},
		"no_user_profile":                  {"true"},
		"ignore_replies":                   {"true"},
		"no_self":                          {"true"},
		"include_full_users":               {"true"},
		"include_use_case":                 {"true"},
		"include_stories":                  {"true"},
		"no_members":                       {"true"},
		"include_mutation_timestamps":      {"true"},
		"count":                            {viewCount},
		"include_free_team_extra_messages": {"true"},
	}
	// Set, not an unconditional assignment: an empty channelID must
	// leave the key absent, not present-and-empty. `channel=` on the
	// wire is a third request shape, observed in neither capture, and
	// is the one thing here guaranteed not to match the real client.
	if channelID != "" {
		form.Set("channel", channelID)
	}

	raw, err := post(ctx, ViewMethod, form)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ViewMethod, err)
	}

	var resp viewResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%s: decoding response: %w (body: %s)", ViewMethod, err, truncate(raw))
	}
	if !resp.OK {
		apiErr := resp.Error
		if apiErr == "" {
			// Without this the message names no failure at all.
			apiErr = "ok=false with no error field"
		}
		return nil, fmt.Errorf("%s: API error: %s", ViewMethod, apiErr)
	}

	out := resp.ViewResult
	return &out, nil
}
