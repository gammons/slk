// Package boot parses Slack's boot-phase endpoints — the calls the
// official web client makes once, at connect, to populate everything a
// session needs.
//
// client.userBoot is the reason this package exists. One request
// returns the channel list, the DM list, the open/starred/read-only
// sidebar state, every user pref, the subteam membership, the DND
// window and the self/team records — replacing five separate calls slk
// used to make (users.conversations, users.prefs.get, stars.list,
// usergroups.list, dnd.info). On a Grid workspace that is the single
// biggest reduction available in slk's ~400-call boot, and call volume
// is precisely what Grid's anomaly detection scores.
//
// Request decoration is NOT this package's job. The _x_ envelope
// (_x_reason, _x_sonic, _x_app_name, and the _x_mode this endpoint
// deliberately omits) is added by slackhttp.BrowserTransport, and the
// token is injected by the caller's post function. This package sends
// only the endpoint's own business params. Adding any of the others
// here would put a duplicate on the wire, which is exactly the kind of
// fingerprint the whole effort exists to remove.
//
// Contracts verified against internal/slack/testdata/phase2-api-contracts.json
// and the raw 2026-07-30 HAR captures.
package boot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// Method is the API method name, used as the map key slackhttp looks
// up for _x_reason and for the _x_mode exclusion.
const Method = "client.userBoot"

// omitExtras is the value the official client sends, byte for byte,
// in both captures. These are response sections a TUI has no use for;
// omitting them is what the real client does, so sending a different
// list — including the obvious "omit more" — would itself be a
// divergence.
const omitExtras = "feature_usage_data,plan_info,salesforce_features"

// PostFunc performs one form POST to a Slack Web API method and
// returns the raw response body.
//
// This is a func rather than an interface so the parser is testable
// with no HTTP server at all, and its signature is deliberately
// identical to slack.Client.postForm — the unexported method that
// injects the xoxc token and carries the browser transport. Keeping
// them identical is what makes wiring this up a one-line conversion
// rather than an adapter. Do not add parameters here without changing
// postForm to match.
type PostFunc func(ctx context.Context, method string, form url.Values) ([]byte, error)

// TextBlock is a channel topic or purpose.
type TextBlock struct {
	Value   string `json:"value"`
	Creator string `json:"creator"`
	LastSet int64  `json:"last_set"`
}

// Channel is one entry in userBoot's `channels` array — the
// conversations the user is a member of, minus DMs, which arrive
// separately in `ims`.
//
// A deliberate subset. The union across all 110 observed elements is
// 29 keys (pending_shared, properties{}, unlinked, parent_conversation,
// …); decoding ignores the rest and must keep doing so, because Slack
// adds fields to this response without notice.
//
// 29, not the 28 an earlier version of this comment claimed — that
// number was read off a single element. Two of the 29 are not on every
// entry: previous_names appears on 106 of 110 and members on 4 of 110.
// A per-field claim about an array element needs a denominator, which
// a count taken from one element cannot have.
//
// The 5 keys a conversations.view channels[] entry adds over these are
// modelled on boot.ViewChannelEntry rather than here — see that type,
// and note the traffic runs both ways: is_frozen and members are
// userBoot-only, so neither belongs on a shared type either.
type Channel struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	NameNormalized string `json:"name_normalized"`
	// Version is the `updated` stamp — a 13-digit millisecond value in
	// this response — and it is the field with real teeth here: it
	// feeds cache.SetChannelVersion(id, int64), which decides whether a
	// channel ever gets refetched. `created` sits right next to it and
	// is a 10-digit second value; reading that one instead would
	// compile, decode, and pin every channel at its creation time
	// forever, so the cache would believe itself current and never
	// revalidate. To us this is an opaque monotonic version, never a
	// timestamp.
	Version int64 `json:"updated"`
	Created int64 `json:"created"`

	// There is deliberately no IsIM here, even though every captured
	// channel entry carries an `is_im` key. userBoot splits DMs into
	// the separate `ims` array, so a channels[] entry with is_im=true
	// was never observed — a field for it would decode false forever
	// and read as meaningful, which is exactly the mistake
	// edge.Channel's missing IsMember documents. Nothing in slk wants
	// it either. Add it if a capture ever shows a true one.
	IsChannel  bool `json:"is_channel"`
	IsGroup    bool `json:"is_group"`
	IsMPIM     bool `json:"is_mpim"`
	IsPrivate  bool `json:"is_private"`
	IsArchived bool `json:"is_archived"`
	IsGeneral  bool `json:"is_general"`

	IsShared    bool `json:"is_shared"`
	IsOrgShared bool `json:"is_org_shared"`
	IsExtShared bool `json:"is_ext_shared"`

	ContextTeamID string   `json:"context_team_id"`
	Creator       string   `json:"creator"`
	SharedTeamIDs []string `json:"shared_team_ids"`

	Topic   TextBlock `json:"topic"`
	Purpose TextBlock `json:"purpose"`
}

// IM is one entry in userBoot's `ims` array.
//
// Note what is different from Channel and why this is a separate type
// rather than a reuse: an im has no `name` and no `topic`, and it
// carries `user` and `is_open`, which a channel does not. The two
// arrays sit next to each other at the top level, so decoding one into
// the other is a one-character mistake that still compiles.
type IM struct {
	ID     string `json:"id"`
	UserID string `json:"user"`
	// Version is the `updated` stamp, same semantics as Channel.Version.
	Version     int64 `json:"updated"`
	Created     int64 `json:"created"`
	IsIM        bool  `json:"is_im"`
	IsOpen      bool  `json:"is_open"`
	IsArchived  bool  `json:"is_archived"`
	IsOrgShared bool  `json:"is_org_shared"`

	ContextTeamID string `json:"context_team_id"`
}

// DND is the authenticated user's do-not-disturb window, replacing a
// dnd.info call.
type DND struct {
	Enabled       bool  `json:"dnd_enabled"`
	NextStartTS   int64 `json:"next_dnd_start_ts"`
	NextEndTS     int64 `json:"next_dnd_end_ts"`
	SnoozeEnabled bool  `json:"snooze_enabled"`
}

// SelfProfile is the subset of self.profile slk renders.
type SelfProfile struct {
	RealName    string `json:"real_name"`
	DisplayName string `json:"display_name"`
	AvatarHash  string `json:"avatar_hash"`
	// ImageOriginal is the only absolute avatar URL in this object;
	// the sized image_NN variants that appear on other endpoints are
	// absent here.
	ImageOriginal    string `json:"image_original"`
	Email            string `json:"email"`
	StatusText       string `json:"status_text"`
	StatusEmoji      string `json:"status_emoji"`
	StatusExpiration int64  `json:"status_expiration"`
}

// Self is the authenticated user's own record.
type Self struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	TeamID   string `json:"team_id"`
	RealName string `json:"real_name"`
	TZ       string `json:"tz"`
	TZOffset int    `json:"tz_offset"`
	// Version is the `updated` stamp, same semantics as Channel.Version.
	Version int64 `json:"updated"`

	// The captured self object also carries is_bot, deleted, is_admin,
	// is_owner, is_primary_owner, is_restricted, is_ultra_restricted
	// and has_2fa. None is modelled: none has a consumer in slk, and —
	// more to the point — this object occurs exactly once in a
	// response, so two boolean fields that happen to share a value are
	// freely swappable and no test can tell them apart. Fields that
	// cannot be pinned do not belong in a package whose whole job is
	// being right about the wire.
	Profile SelfProfile `json:"profile"`
}

// Team is the workspace record. The captured object also carries a
// large `prefs` sub-object, an `icon` set and counters; none of it has
// a consumer in slk, so none of it is modelled.
type Team struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Domain        string `json:"domain"`
	URL           string `json:"url"`
	AvatarBaseURL string `json:"avatar_base_url"`
}

// Subteams is userBoot's `subteams` value.
//
// It is an OBJECT, `{"self": [...]}`, not an array — unlike every
// other plural key at this level (channels, ims, starred, workspaces).
// Modelling it as a slice makes the entire response fail to decode.
//
// The element type is json.RawMessage on purpose. Both captures showed
// `"self": []` on this workspace, so there is no evidence whatsoever
// for what a populated entry looks like. A struct here would be an
// invented contract; raw bytes claim nothing and lose nothing. Give it
// a real type when a capture with a non-empty subteam list exists.
type Subteams struct {
	Self []json.RawMessage `json:"self"`
}

// Prefs is the user's preference blob — 702 keys in the captured
// response.
//
// Only the two mute-related strings are pulled out by name; everything
// else stays in Raw. Modelling 702 fields would be churn with no
// consumer, and dropping them would force the users.prefs.get round
// trip this endpoint exists to remove.
type Prefs struct {
	// MutedChannels is the legacy flat comma-separated list.
	//
	// It was NOT present in the captured response — all 702 keys were
	// checked — and the plan's spec is wrong to say it was. It is
	// surfaced anyway because slk's existing GetMutedChannels still
	// merges it for workspaces that do ship it (see
	// internal/slack/client.go).
	MutedChannels string
	// AllNotificationsPrefs is where mute state actually lives. Its
	// value is a JSON-encoded *string* (a Slack quirk: a string whose
	// contents are JSON), decoding to
	// {"channels":{"<id>":{"muted":bool,…},…}}.
	//
	// It is deliberately left as the raw string rather than parsed
	// here. slack.ParseMutedFromAllNotificationsPrefs already decodes
	// exactly this, and calling it would mean this package importing
	// internal/slack — the wrong direction, since internal/slack is
	// what will import this package. Callers parse.
	AllNotificationsPrefs string
	// Raw is the whole prefs object, undecoded, including the two
	// fields above. Pulling those out is additive, not a move.
	Raw json.RawMessage
}

// prefsWire is the tagged view of the two named prefs. It is separate
// from Prefs so Prefs.UnmarshalJSON can keep the raw bytes as well,
// which a single struct cannot do: encoding/json refuses to map two
// fields of one struct to the same JSON key.
type prefsWire struct {
	MutedChannels         string `json:"muted_channels"`
	AllNotificationsPrefs string `json:"all_notifications_prefs"`
}

// UnmarshalJSON decodes the two named prefs and keeps the rest verbatim.
func (p *Prefs) UnmarshalJSON(b []byte) error {
	var w prefsWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	p.MutedChannels = w.MutedChannels
	p.AllNotificationsPrefs = w.AllNotificationsPrefs
	// Cloned, not aliased. This is encoding/json's stated contract for
	// an Unmarshaler — "UnmarshalJSON must copy the JSON data if it
	// wishes to retain the data after returning" — and aliasing here
	// would hand the caller a Result whose prefs silently change the
	// next time the decoding buffer is reused. Pinned directly by
	// TestPrefsUnmarshalJSONCopiesItsInput, because UserBoot's own
	// path cannot observe the difference today.
	p.Raw = bytes.Clone(b)
	return nil
}

// Result is everything one client.userBoot call learned that slk
// consumes.
//
// The captured response has 33 top-level keys. The ones absent here
// (app_commands_cache_ts, cache_version, translations_cache_ts,
// is_europe, account_types, can_access_client_v2, slack_route,
// workspaces, prefs_version, links, accept_tos_url, the various
// feature flags…) have no consumer in slk. Adding one is a two-line
// change; inventing a consumer for it is not.
type Result struct {
	// Channels are the conversations the user belongs to, DMs
	// excluded. Replaces the users.conversations walk.
	Channels []Channel `json:"channels"`
	// IMs are the user's DMs. The other half of users.conversations.
	IMs []IM `json:"ims"`
	// IsOpen holds the conversation ids currently shown in the
	// sidebar — channels and DMs mixed.
	IsOpen []string `json:"is_open"`
	// Starred replaces the stars.list call.
	//
	// []json.RawMessage for the same reason as Subteams.Self: the
	// captured value was `[]` on this workspace, so the element shape
	// is unverified. stars.list items are {type, channel, …}, but
	// there is no evidence userBoot uses that same shape and guessing
	// would be inventing a contract. Give it a real type when a
	// capture with a non-empty starred list exists.
	Starred []json.RawMessage `json:"starred"`
	// Subteams replaces the usergroups.list call.
	Subteams Subteams `json:"subteams"`
	// DND replaces the dnd.info call.
	DND DND `json:"dnd"`
	// Prefs replaces the users.prefs.get call.
	Prefs Prefs `json:"prefs"`

	Self Self `json:"self"`
	Team Team `json:"team"`

	// ChannelsPriority is Slack's per-channel affinity score, keyed by
	// channel id. The values are floats and are frequently far below
	// 1; an int map does not truncate them, it fails the decode.
	ChannelsPriority map[string]float64 `json:"channels_priority"`

	// EmojiCacheTS looks numeric and is not: Slack ships it as a
	// 17-character string. It is a cache token to be echoed back
	// verbatim, so it stays a string — a numeric type would fail the
	// decode outright, and a float would lose the exact bytes.
	EmojiCacheTS string `json:"emoji_cache_ts"`

	ReadOnlyChannels      []string `json:"read_only_channels"`
	NonThreadableChannels []string `json:"non_threadable_channels"`
	ThreadOnlyChannels    []string `json:"thread_only_channels"`

	DefaultWorkspace string `json:"default_workspace"`
	HasMoreMPDMs     bool   `json:"has_more_mpdms"`
}

// response is Result plus the two envelope fields every Slack Web API
// answer carries. They are kept off Result because a caller never sees
// a Result unless ok was true, so exposing them would only invite a
// second, redundant check.
type response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	Result
}

// UserBoot calls client.userBoot through post and returns the parsed
// result.
//
// ctx is passed through untouched. In particular this does NOT set
// _x_reason: slackhttp's defaultReasons table already maps
// client.userBoot to "initial-data" (internal/slackhttp/reason.go), so
// setting it here would duplicate that constant in a second place
// where the two could silently disagree, and — worse — would override
// a caller that deliberately chose a different reason, since an
// explicit WithReason always beats the table.
//
// Any error returns a nil Result. That is load-bearing, not tidiness:
// encoding/json populates a struct as it goes and keeps decoding past
// the first type error, and `ok` is only inspected after the whole
// body has been decoded — so at both failure points there is a fully
// populated Result sitting in a local. Handing it back would give the
// caller a plausible-looking workspace built from a response the
// server rejected or the decoder could not read.
func UserBoot(ctx context.Context, post PostFunc) (*Result, error) {
	form := url.Values{
		// Strings, not booleans: this is a form body. The official
		// client sends exactly these three business params and nothing
		// else of its own.
		"version_all_channels":      {"false"},
		"return_all_relevant_mpdms": {"true"},
		"omit_extras":               {omitExtras},
	}

	raw, err := post(ctx, Method, form)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", Method, err)
	}

	var resp response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%s: decoding response: %w (body: %s)", Method, err, truncate(raw))
	}
	if !resp.OK {
		apiErr := resp.Error
		if apiErr == "" {
			// Without this the message names no failure at all.
			apiErr = "ok=false with no error field"
		}
		return nil, fmt.Errorf("%s: API error: %s", Method, apiErr)
	}

	out := resp.Result
	return &out, nil
}

// truncate bounds a body quoted into an error message. A userBoot
// response is ~139 KB; a log line is not.
func truncate(b []byte) string {
	const max = 512
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "...(truncated)"
}
