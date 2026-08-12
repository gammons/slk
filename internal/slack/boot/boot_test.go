package boot

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	// internal/slack is package slackclient. Importing it here is a
	// test-only dependency and does NOT cycle: nothing outside this
	// directory imports boot yet, and internal/slack's own imports are
	// debuglog, slack/mrkdwn and slackhttp. Phase 2b reverses the
	// direction (slackclient will import boot); at that point this
	// import must move to an external boot_test package or the two-line
	// parse must be inlined. boot.go itself deliberately imports
	// nothing from this repo.
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slackhttp"
)

// recordedCall is the one call UserBoot is expected to make.
type recordedCall struct {
	calls  int
	ctx    context.Context
	method string
	form   url.Values
}

// stubPost returns a PostFunc that records what it was handed and
// answers with body (or err). No network, no httptest server: the
// whole point of PostFunc is that this parser is testable without
// either.
func stubPost(rec *recordedCall, body string, err error) PostFunc {
	return func(ctx context.Context, method string, form url.Values) ([]byte, error) {
		rec.calls++
		rec.ctx = ctx
		rec.method = method
		rec.form = form
		if err != nil {
			return nil, err
		}
		return []byte(body), nil
	}
}

// fullBootBody is a client.userBoot response shaped like the captured
// one: the same 33 top-level keys, the same nesting, the same types.
// Values are synthetic but deliberately *distinct* per field so a
// swapped struct tag cannot survive, and booleans are deliberately
// mixed true/false so an assertion cannot pass against a field that
// was never decoded at all.
//
// It also carries keys this package does not model (workspaces,
// account_types, prefs_version, slack_route, previous_names,
// properties, …) exactly as the capture does — decoding must ignore
// them, because Slack adds fields to this response without notice.
//
// Two shapes here are honest guesses and are marked as such:
// subteams.self and starred were BOTH empty arrays in both captures,
// so their element shape is unverified. This package models them as
// []json.RawMessage precisely so it claims nothing about them, and
// the tests below only assert that whatever bytes arrive survive.
//
// One value is deliberately un-lifelike: slack_route is a 9-character
// string in the capture and is very plausibly the same team id as
// default_workspace, but here it is a different one. That is a
// deliberate choice about the *fixture*, not a claim about the
// protocol — with both set to "T04T4TH8W" a DefaultWorkspace field
// tagged `json:"slack_route"` decodes to the right answer for the
// wrong reason and no assertion can tell. Same-typed neighbours get
// distinct values so a tag swap always dies.
const fullBootBody = `{
  "ok": true,
  "app_commands_cache_ts": "1783337533.019174",
  "cache_ts_version": "v0.0.0.0",
  "cache_version": "v45-anteat",
  "emoji_cache_ts": "1783337534.020175",
  "translations_cache_ts": "1783337535.021176",
  "is_content_reporting_enabled": false,
  "dnd": {
    "dnd_enabled": true,
    "next_dnd_start_ts": 1783400001,
    "next_dnd_end_ts": 1783430002,
    "snooze_enabled": false
  },
  "is_europe": false,
  "account_types": {"is_admin": [], "is_owner": [], "is_primary_owner": []},
  "can_access_client_v2": false,
  "channels_priority": {"C04T4TH9N": 0.5, "C94H848UB": 12.75, "C2QPK1V44": 0.0009765625},
  "channels": [
    {
      "id": "C04T4TH9N",
      "name": "general",
      "name_normalized": "general-norm",
      "is_channel": true,
      "is_group": false,
      "is_im": false,
      "is_mpim": false,
      "is_private": false,
      "created": 1670000001,
      "updated": 1783337533019,
      "is_archived": false,
      "is_general": true,
      "unlinked": 0,
      "is_shared": true,
      "is_frozen": false,
      "is_org_shared": true,
      "is_pending_ext_shared": false,
      "pending_shared": [],
      "context_team_id": "T04T4TH8W",
      "parent_conversation": null,
      "creator": "U04T4THAA",
      "is_ext_shared": false,
      "shared_team_ids": ["T04T4TH8W", "T7777777Z"],
      "pending_connected_team_ids": [],
      "topic": {"value": "company announcements", "creator": "U04T4THAA", "last_set": 1670000101},
      "purpose": {"value": "all hands", "creator": "U04T4THBB", "last_set": 1670000102},
      "properties": {"tabs": [], "canvas": {"file_id": "F0AAA"}},
      "previous_names": ["genral"]
    },
    {
      "id": "C94H848UB",
      "name": "secret-plans",
      "name_normalized": "secret-plans-norm",
      "is_channel": false,
      "is_group": true,
      "is_im": false,
      "is_mpim": false,
      "is_private": true,
      "created": 1670000002,
      "updated": 1783337533020,
      "is_archived": true,
      "is_general": false,
      "unlinked": 0,
      "is_shared": true,
      "is_frozen": false,
      "is_org_shared": true,
      "is_pending_ext_shared": false,
      "pending_shared": [],
      "context_team_id": "T7777777Z",
      "parent_conversation": null,
      "creator": "U04T4THCC",
      "is_ext_shared": false,
      "shared_team_ids": ["T04T4TH8W", "T7777777Z"],
      "pending_connected_team_ids": [],
      "topic": {"value": "hush", "creator": "U04T4THCC", "last_set": 1670000103},
      "purpose": {"value": "plotting", "creator": "U04T4THCC", "last_set": 1670000104},
      "properties": {},
      "previous_names": []
    },
    {
      "id": "C2QPK1V44",
      "name": "mpdm-a--b--c-1",
      "name_normalized": "mpdm-a--b--c-1",
      "is_channel": false,
      "is_group": false,
      "is_im": false,
      "is_mpim": true,
      "is_private": true,
      "created": 1670000003,
      "updated": 1783337533021,
      "is_archived": true,
      "is_general": false,
      "unlinked": 0,
      "is_shared": false,
      "is_frozen": false,
      "is_org_shared": false,
      "is_pending_ext_shared": false,
      "pending_shared": [],
      "context_team_id": "T04T4TH8W",
      "parent_conversation": null,
      "creator": "U04T4THDD",
      "is_ext_shared": false,
      "shared_team_ids": ["T04T4TH8W"],
      "pending_connected_team_ids": [],
      "topic": {"value": "", "creator": "", "last_set": 0},
      "purpose": {"value": "Group messaging", "creator": "U04T4THDD", "last_set": 1670000105},
      "properties": {},
      "previous_names": []
    },
    {
      "id": "C55CONN33",
      "name": "partner-sync",
      "name_normalized": "partner-sync-norm",
      "is_channel": true,
      "is_group": false,
      "is_im": false,
      "is_mpim": false,
      "is_private": true,
      "created": 1670000004,
      "updated": 1783337533022,
      "is_archived": false,
      "is_general": false,
      "unlinked": 0,
      "is_shared": true,
      "is_frozen": false,
      "is_org_shared": false,
      "is_pending_ext_shared": false,
      "pending_shared": [],
      "context_team_id": "T04T4TH8W",
      "parent_conversation": null,
      "creator": "U04T4THEE",
      "is_ext_shared": true,
      "shared_team_ids": ["T04T4TH8W", "E11EXTERN"],
      "pending_connected_team_ids": [],
      "topic": {"value": "partner integration", "creator": "U04T4THEE", "last_set": 1670000106},
      "purpose": {"value": "cross-org work", "creator": "U04T4THEE", "last_set": 1670000107},
      "properties": {},
      "previous_names": []
    }
  ],
  "default_workspace": "T04T4TH8W",
  "has_more_mpdms": true,
  "ims": [
    {
      "id": "D04T4THD1",
      "created": 1660000001,
      "is_org_shared": true,
      "is_im": true,
      "is_archived": false,
      "context_team_id": "T04T4TH8W",
      "updated": 1783337544001,
      "is_frozen": false,
      "user": "U04T4THEE",
      "is_open": true,
      "properties": {"is_dormant": false}
    },
    {
      "id": "D04T4THD2",
      "created": 1660000002,
      "is_org_shared": false,
      "is_im": true,
      "is_archived": true,
      "context_team_id": "T7777777Z",
      "updated": 1783337544002,
      "is_frozen": false,
      "user": "U04T4THFF",
      "is_open": false,
      "properties": {"is_dormant": true}
    },
    {
      "id": "D04T4THD3",
      "created": 1660000003,
      "is_org_shared": false,
      "is_im": true,
      "is_archived": false,
      "context_team_id": "T04T4TH8W",
      "updated": 1783337544003,
      "is_frozen": false,
      "user": "U04T4THGG",
      "is_open": true,
      "properties": {"is_dormant": false}
    }
  ],
  "is_open": ["D04T4THD1", "C04T4TH9N", "C94H848UB"],
  "non_threadable_channels": [],
  "prefs": {
    "muted_channels": "CLEGACY01,CLEGACY02",
    "all_notifications_prefs": "{\"channels\":{\"C94H848UB\":{\"muted\":true,\"suppress_at_channel\":false},\"C04T4TH9N\":{\"muted\":false},\"C2QPK1V44\":{\"muted\":true}}}",
    "arrow_history": false,
    "emoji_mode": "default",
    "mute_sounds": true,
    "user_colors": "",
    "keyboard": null,
    "email_alerts_sleep_until": 0
  },
  "prefs_version": "0f5d2b7c9a",
  "read_only_channels": ["C2QPK1V44", "C94H848UB"],
  "self": {
    "id": "U04T4THAA",
    "name": "grant",
    "is_bot": false,
    "updated": 1783337555001,
    "is_app_user": false,
    "team_id": "T04T4TH8W",
    "deleted": false,
    "color": "9f69e7",
    "is_email_confirmed": true,
    "real_name": "Grant Ammons",
    "tz": "America/Chicago",
    "tz_label": "Central Daylight Time",
    "tz_offset": -18000,
    "is_admin": true,
    "is_owner": false,
    "is_primary_owner": false,
    "is_restricted": false,
    "is_ultra_restricted": false,
    "has_2fa": true,
    "who_can_share_contact_card": "EVERYONE",
    "first_login": 1650000001,
    "manual_presence": "active",
    "profile": {
      "real_name": "Grant Ammons",
      "display_name": "grantammons",
      "avatar_hash": "abc123def456",
      "real_name_normalized": "Grant Ammons-norm",
      "display_name_normalized": "grantammons-norm",
      "image_original": "https://avatars.example/orig.png",
      "is_custom_image": true,
      "first_name": "Grant",
      "last_name": "Ammons",
      "team": "T04T4TH8W",
      "email": "grant@example.com",
      "title": "Engineer",
      "phone": "",
      "skype": "",
      "status_text": "in a meeting",
      "status_text_canonical": "",
      "status_emoji": ":spiral_calendar_pad:",
      "status_emoji_display_info": [],
      "status_expiration": 1783339999,
      "huddle_state": "default_unset"
    }
  },
  "slack_route": "T09ROUTE9",
  "starred": [],
  "team": {
    "id": "T04T4TH8W",
    "name": "Truelist",
    "url": "https://truelist-hq.slack.com/",
    "domain": "truelist-hq",
    "email_domain": "",
    "avatar_base_url": "https://ca.slack-edge.com/",
    "is_verified": true,
    "plan": "std",
    "icon": {"image_default": false, "image_68": "https://ca.slack-edge.com/T04-68.png"},
    "date_created": 1640000001,
    "prefs": {"who_can_post_general": "ra", "allow_calls": true},
    "image_proxy_url": "https://slack-imgs.com/",
    "onboarding_channel_id": "",
    "messages_count": 987654
  },
  "thread_only_channels": ["C04T4TH9N"],
  "workspaces": [
    {"id": "T04T4TH8W", "name": "Truelist", "domain": "truelist-hq"}
  ],
  "is_slack_first_crm": false,
  "is_eligible_invited_user_glow_up": false,
  "mobile_app_requires_upgrade": false,
  "subteams": {"self": []},
  "accept_tos_url": null,
  "links": {"domains_ts": 1783337000}
}`

// mustBoot runs UserBoot against a canned body and fails the test if
// it errors.
func mustBoot(t *testing.T, body string) (*Result, *recordedCall) {
	t.Helper()
	var rec recordedCall
	res, err := UserBoot(context.Background(), stubPost(&rec, body, nil))
	if err != nil {
		t.Fatalf("UserBoot: unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("UserBoot returned a nil Result with a nil error")
	}
	return res, &rec
}

// ------------------------------------------------------------- request

// TestUserBoot_SendsExactlyTheObservedBusinessParams pins the form
// body byte-for-byte against the capture's business params — and, just
// as importantly, pins what is NOT there.
//
// token, _x_reason, _x_sonic and _x_app_name all appear in the captured
// request but none of them is this package's job: token is injected by
// the caller's postForm, and the three _x_ fields are added by
// slackhttp.BrowserTransport. If this package added any of them the
// wire body would carry a duplicate, which is a fingerprint in exactly
// the way this whole effort exists to avoid. An exact key set is the
// only assertion that catches that.
func TestUserBoot_SendsExactlyTheObservedBusinessParams(t *testing.T) {
	_, rec := mustBoot(t, fullBootBody)

	if rec.calls != 1 {
		t.Errorf("post called %d times; want exactly 1 (the whole point is one call replacing five)", rec.calls)
	}
	if rec.method != "client.userBoot" {
		t.Errorf("method = %q; want %q", rec.method, "client.userBoot")
	}

	var got []string
	for k := range rec.form {
		got = append(got, k)
	}
	slices.Sort(got)
	want := []string{"omit_extras", "return_all_relevant_mpdms", "version_all_channels"}
	if !slices.Equal(got, want) {
		t.Errorf("form keys = %v; want exactly %v", got, want)
	}

	// Byte-exact values. These are form fields, so "false" and "true"
	// are strings — sending the JSON booleans would be a different
	// body.
	for _, tc := range []struct{ key, want string }{
		{"version_all_channels", "false"},
		{"return_all_relevant_mpdms", "true"},
		{"omit_extras", "feature_usage_data,plan_info,salesforce_features"},
	} {
		if got := rec.form.Get(tc.key); got != tc.want {
			t.Errorf("form[%s] = %q; want %q", tc.key, got, tc.want)
		}
	}
}

// TestUserBoot_DoesNotOverrideACallerSuppliedReason pins that this
// package leaves _x_reason alone.
//
// slackhttp's defaultReasons table already maps client.userBoot to
// "initial-data" (internal/slackhttp/reason.go), so hardcoding a
// WithReason here would duplicate that constant in a second place and,
// worse, would silently clobber a caller that deliberately set a
// different one. The context must arrive at post untouched.
func TestUserBoot_DoesNotOverrideACallerSuppliedReason(t *testing.T) {
	var rec recordedCall
	ctx := slackhttp.WithReason(context.Background(), "caller-chose-this")
	if _, err := UserBoot(ctx, stubPost(&rec, fullBootBody, nil)); err != nil {
		t.Fatalf("UserBoot: %v", err)
	}
	if got := slackhttp.ReasonFrom(rec.ctx); got != "caller-chose-this" {
		t.Errorf("_x_reason on the outgoing context = %q; want %q", got, "caller-chose-this")
	}
}

// ------------------------------------------------------------ channels

func TestUserBoot_DecodesChannels(t *testing.T) {
	res, _ := mustBoot(t, fullBootBody)

	if len(res.Channels) != 4 {
		t.Fatalf("len(Channels) = %d; want 4", len(res.Channels))
	}

	// Every boolean below has a column vector across these four rows
	// that is unique and non-zero:
	//
	//	is_channel 1001   is_group 0100   is_mpim 0010
	//	is_private 0111   is_archived 0110   is_general 1000
	//	is_shared 1101   is_org_shared 1100   is_ext_shared 0001
	//
	// That is the property that makes these assertions worth anything.
	// With duplicate columns (the first draft had five bools all
	// reading 0100) a swapped tag decodes to the right answer for the
	// wrong reason and nothing fails; with an all-false column an
	// assertion cannot tell "decoded false" from "never decoded".
	// The two unmodelled channel booleans, is_frozen and
	// is_pending_ext_shared, are false on all four rows on purpose, so
	// a modelled field mis-tagged onto one of them goes all-false and
	// dies.
	want := []Channel{
		{
			ID: "C04T4TH9N", Name: "general", NameNormalized: "general-norm",
			Version: 1783337533019, Created: 1670000001,
			IsChannel: true, IsGroup: false, IsMPIM: false,
			IsPrivate: false, IsArchived: false, IsGeneral: true,
			IsShared: true, IsOrgShared: true, IsExtShared: false,
			ContextTeamID: "T04T4TH8W", Creator: "U04T4THAA",
			SharedTeamIDs: []string{"T04T4TH8W", "T7777777Z"},
			Topic:         TextBlock{Value: "company announcements", Creator: "U04T4THAA", LastSet: 1670000101},
			Purpose:       TextBlock{Value: "all hands", Creator: "U04T4THBB", LastSet: 1670000102},
		},
		{
			ID: "C94H848UB", Name: "secret-plans", NameNormalized: "secret-plans-norm",
			Version: 1783337533020, Created: 1670000002,
			IsChannel: false, IsGroup: true, IsMPIM: false,
			IsPrivate: true, IsArchived: true, IsGeneral: false,
			IsShared: true, IsOrgShared: true, IsExtShared: false,
			ContextTeamID: "T7777777Z", Creator: "U04T4THCC",
			SharedTeamIDs: []string{"T04T4TH8W", "T7777777Z"},
			Topic:         TextBlock{Value: "hush", Creator: "U04T4THCC", LastSet: 1670000103},
			Purpose:       TextBlock{Value: "plotting", Creator: "U04T4THCC", LastSet: 1670000104},
		},
		{
			ID: "C2QPK1V44", Name: "mpdm-a--b--c-1", NameNormalized: "mpdm-a--b--c-1",
			Version: 1783337533021, Created: 1670000003,
			IsChannel: false, IsGroup: false, IsMPIM: true,
			IsPrivate: true, IsArchived: true, IsGeneral: false,
			IsShared: false, IsOrgShared: false, IsExtShared: false,
			ContextTeamID: "T04T4TH8W", Creator: "U04T4THDD",
			SharedTeamIDs: []string{"T04T4TH8W"},
			Topic:         TextBlock{},
			Purpose:       TextBlock{Value: "Group messaging", Creator: "U04T4THDD", LastSet: 1670000105},
		},
		{
			ID: "C55CONN33", Name: "partner-sync", NameNormalized: "partner-sync-norm",
			Version: 1783337533022, Created: 1670000004,
			IsChannel: true, IsGroup: false, IsMPIM: false,
			IsPrivate: true, IsArchived: false, IsGeneral: false,
			IsShared: true, IsOrgShared: false, IsExtShared: true,
			ContextTeamID: "T04T4TH8W", Creator: "U04T4THEE",
			SharedTeamIDs: []string{"T04T4TH8W", "E11EXTERN"},
			Topic:         TextBlock{Value: "partner integration", Creator: "U04T4THEE", LastSet: 1670000106},
			Purpose:       TextBlock{Value: "cross-org work", Creator: "U04T4THEE", LastSet: 1670000107},
		},
	}
	for i, w := range want {
		if got := res.Channels[i]; !reflect.DeepEqual(got, w) {
			t.Errorf("Channels[%d] =\n  %#v\nwant\n  %#v", i, got, w)
		}
	}
}

// TestUserBoot_ChannelVersionIsUpdatedNotCreated pins the one channel
// field that has a downstream consumer with teeth: Version feeds
// cache.SetChannelVersion(id, int64), which decides whether a channel
// gets refetched. Reading `created` instead of `updated` would compile,
// decode, and freeze every channel's version at its creation time
// forever — the cache would then believe it is current and never
// revalidate.
//
// created and updated differ by three orders of magnitude in the real
// data (seconds vs milliseconds) and by construction in the fixture, so
// a tag swap cannot survive this.
func TestUserBoot_ChannelVersionIsUpdatedNotCreated(t *testing.T) {
	res, _ := mustBoot(t, fullBootBody)

	for i, want := range []int64{1783337533019, 1783337533020, 1783337533021, 1783337533022} {
		if got := res.Channels[i].Version; got != want {
			t.Errorf("Channels[%d].Version = %d; want %d (the `updated` ms stamp, not `created`)", i, got, want)
		}
	}
	for i, want := range []int64{1670000001, 1670000002, 1670000003, 1670000004} {
		if got := res.Channels[i].Created; got != want {
			t.Errorf("Channels[%d].Created = %d; want %d", i, got, want)
		}
	}
	// int64, not int32/float: a 13-digit ms stamp must survive intact.
	if res.Channels[0].Version <= 1<<31 {
		t.Errorf("Channels[0].Version = %d; a real ms stamp exceeds 2^31, so this field must be int64",
			res.Channels[0].Version)
	}
}

// ----------------------------------------------------------------- ims

// TestUserBoot_DecodesIMsSeparatelyFromChannels pins that ims and
// channels do not decode into each other. They are different shapes on
// the wire — an im carries `user` and `is_open` and has no `name` —
// and both arrays exist at the top level, so transposing the two tags
// is a one-character mistake that leaves a compiling, decoding client
// with an empty sidebar.
func TestUserBoot_DecodesIMsSeparatelyFromChannels(t *testing.T) {
	res, _ := mustBoot(t, fullBootBody)

	if len(res.IMs) != 3 {
		t.Fatalf("len(IMs) = %d; want 3", len(res.IMs))
	}
	// Three rows, not two, for the same column-uniqueness reason as
	// Channels: with two rows there are only three distinct non-zero
	// boolean columns to go round four boolean fields, so two of them
	// have to collide and become freely swappable. Here is_im reads
	// 111, is_open 101, is_archived 010 and is_org_shared 100, while
	// the unmodelled is_frozen is 000.
	want := []IM{
		{
			ID: "D04T4THD1", UserID: "U04T4THEE", Created: 1660000001, Version: 1783337544001,
			IsIM: true, IsOpen: true, IsArchived: false, IsOrgShared: true,
			ContextTeamID: "T04T4TH8W",
		},
		{
			ID: "D04T4THD2", UserID: "U04T4THFF", Created: 1660000002, Version: 1783337544002,
			IsIM: true, IsOpen: false, IsArchived: true, IsOrgShared: false,
			ContextTeamID: "T7777777Z",
		},
		{
			ID: "D04T4THD3", UserID: "U04T4THGG", Created: 1660000003, Version: 1783337544003,
			IsIM: true, IsOpen: true, IsArchived: false, IsOrgShared: false,
			ContextTeamID: "T04T4TH8W",
		},
	}
	for i, w := range want {
		if res.IMs[i] != w {
			t.Errorf("IMs[%d] =\n  %#v\nwant\n  %#v", i, res.IMs[i], w)
		}
	}

	// The transposition guard: no channel id may appear in IMs and no
	// im id in Channels.
	for _, im := range res.IMs {
		if !strings.HasPrefix(im.ID, "D") {
			t.Errorf("IMs contains %q, which is not a DM id — ims and channels are crossed", im.ID)
		}
	}
	for _, ch := range res.Channels {
		if strings.HasPrefix(ch.ID, "D") {
			t.Errorf("Channels contains %q, which is a DM id — ims and channels are crossed", ch.ID)
		}
	}
}

// ------------------------------------------------------------ id lists

func TestUserBoot_DecodesChannelIDLists(t *testing.T) {
	res, _ := mustBoot(t, fullBootBody)

	if want := []string{"D04T4THD1", "C04T4TH9N", "C94H848UB"}; !slices.Equal(res.IsOpen, want) {
		t.Errorf("IsOpen = %v; want %v", res.IsOpen, want)
	}
	if want := []string{"C2QPK1V44", "C94H848UB"}; !slices.Equal(res.ReadOnlyChannels, want) {
		t.Errorf("ReadOnlyChannels = %v; want %v", res.ReadOnlyChannels, want)
	}
	if want := []string{"C04T4TH9N"}; !slices.Equal(res.ThreadOnlyChannels, want) {
		t.Errorf("ThreadOnlyChannels = %v; want %v", res.ThreadOnlyChannels, want)
	}
	if res.DefaultWorkspace != "T04T4TH8W" {
		t.Errorf("DefaultWorkspace = %q; want %q", res.DefaultWorkspace, "T04T4TH8W")
	}
	if !res.HasMoreMPDMs {
		t.Error("HasMoreMPDMs = false; want true")
	}
}

// TestUserBoot_EmptyArraysDecodeToEmptyNotMissing covers the two
// arrays the capture showed empty: starred (this workspace has no
// stars) and non_threadable_channels.
//
// Asserting "len == 0" alone would be vacuous — it passes just as well
// against a field that was never decoded, or one whose tag is
// misspelled. Non-nil is what separates "the server said []" from "we
// never looked", and callers need that distinction: an absent
// non_threadable_channels means "unknown", an empty one means "none".
func TestUserBoot_EmptyArraysDecodeToEmptyNotMissing(t *testing.T) {
	res, _ := mustBoot(t, fullBootBody)

	if res.Starred == nil {
		t.Error("Starred is nil; the response carried `\"starred\": []` so it must decode to an empty, non-nil slice")
	}
	if len(res.Starred) != 0 {
		t.Errorf("len(Starred) = %d; want 0", len(res.Starred))
	}
	if res.NonThreadableChannels == nil {
		t.Error("NonThreadableChannels is nil; the response carried `[]` so it must decode to an empty, non-nil slice")
	}
	if len(res.NonThreadableChannels) != 0 {
		t.Errorf("len(NonThreadableChannels) = %d; want 0", len(res.NonThreadableChannels))
	}
	if res.Subteams.Self == nil {
		t.Error("Subteams.Self is nil; the response carried `{\"self\": []}` so it must decode to an empty, non-nil slice")
	}
}

// ------------------------------------------------------------ subteams

// TestUserBoot_SubteamsIsAnObjectNotAnArray pins the shape that is
// easiest to get wrong from the key name alone. Every other plural key
// at this level (channels, ims, starred, workspaces) is an array;
// subteams is `{"self": [...]}`. Modelling it as a slice makes the
// whole response fail to decode, so this also pins that such a failure
// is loud rather than silent.
func TestUserBoot_SubteamsIsAnObjectNotAnArray(t *testing.T) {
	const body = `{"ok":true,"subteams":{"self":[{"id":"S0AB1CD2E"},{"id":"S0FF9EE8D"}]}}`
	res, _ := mustBoot(t, body)

	if len(res.Subteams.Self) != 2 {
		t.Fatalf("len(Subteams.Self) = %d; want 2", len(res.Subteams.Self))
	}
	// The element shape is deliberately unclaimed: both captures showed
	// `"self": []`, so there is no evidence for any field inside. The
	// contract this asserts is only that the raw bytes survive intact
	// for a later phase (or a later capture) to interpret.
	for i, want := range []string{`{"id":"S0AB1CD2E"}`, `{"id":"S0FF9EE8D"}`} {
		if got := string(res.Subteams.Self[i]); got != want {
			t.Errorf("Subteams.Self[%d] = %s; want %s", i, got, want)
		}
	}
}

// --------------------------------------------------------------- types

// TestUserBoot_ChannelsPriorityValuesAreFloats pins the type of the
// values in channels_priority. They are floats in the capture, and
// several are far below 1 — modelling them as int does not merely
// truncate, it fails the decode outright, but a map[string]int that
// only ever saw whole numbers in a fixture would have looked fine.
func TestUserBoot_ChannelsPriorityValuesAreFloats(t *testing.T) {
	res, _ := mustBoot(t, fullBootBody)

	want := map[string]float64{
		"C04T4TH9N": 0.5,
		"C94H848UB": 12.75,
		"C2QPK1V44": 0.0009765625,
	}
	if len(res.ChannelsPriority) != len(want) {
		t.Fatalf("len(ChannelsPriority) = %d; want %d", len(res.ChannelsPriority), len(want))
	}
	for id, w := range want {
		if got := res.ChannelsPriority[id]; got != w {
			t.Errorf("ChannelsPriority[%s] = %v; want %v", id, got, w)
		}
	}
}

// TestUserBoot_EmojiCacheTSIsAString pins the type of a field that
// looks numeric and is not. Slack ships emoji_cache_ts as a 17-character
// string ("1783337534.020175"); an int64 field there fails the decode,
// and a float64 one would silently lose the exact bytes we have to send
// back.
func TestUserBoot_EmojiCacheTSIsAString(t *testing.T) {
	res, _ := mustBoot(t, fullBootBody)

	if got, want := res.EmojiCacheTS, "1783337534.020175"; got != want {
		t.Errorf("EmojiCacheTS = %q; want %q", got, want)
	}
}

// TestUserBoot_DecodesDND pins the four dnd fields, which sit next to
// each other with two bools and two int stamps — exactly the shape
// where a transposed tag decodes cleanly and lies. dnd_enabled is true
// and snooze_enabled false in the fixture so neither assertion can pass
// against a field that was never decoded.
func TestUserBoot_DecodesDND(t *testing.T) {
	res, _ := mustBoot(t, fullBootBody)

	want := DND{
		Enabled:       true,
		NextStartTS:   1783400001,
		NextEndTS:     1783430002,
		SnoozeEnabled: false,
	}
	if res.DND != want {
		t.Errorf("DND = %#v; want %#v", res.DND, want)
	}
}

// TestUserBoot_DecodesSelf pins the authenticated user. id and team_id
// are both 9-character T/U-prefixed strings sitting in the same object,
// which is the classic silent transposition.
func TestUserBoot_DecodesSelf(t *testing.T) {
	res, _ := mustBoot(t, fullBootBody)

	self := res.Self
	if self.ID != "U04T4THAA" {
		t.Errorf("Self.ID = %q; want %q", self.ID, "U04T4THAA")
	}
	if self.TeamID != "T04T4TH8W" {
		t.Errorf("Self.TeamID = %q; want %q", self.TeamID, "T04T4TH8W")
	}
	if self.Name != "grant" {
		t.Errorf("Self.Name = %q; want %q", self.Name, "grant")
	}
	if self.RealName != "Grant Ammons" {
		t.Errorf("Self.RealName = %q; want %q", self.RealName, "Grant Ammons")
	}
	if self.TZ != "America/Chicago" {
		t.Errorf("Self.TZ = %q; want %q", self.TZ, "America/Chicago")
	}
	if self.TZOffset != -18000 {
		t.Errorf("Self.TZOffset = %d; want %d", self.TZOffset, -18000)
	}
	if self.Version != 1783337555001 {
		t.Errorf("Self.Version = %d; want %d", self.Version, int64(1783337555001))
	}
	// self carries is_bot, deleted, is_admin, is_owner and four more
	// booleans that are deliberately not modelled — see the comment on
	// the Self type. There is exactly one self object per response, so
	// any two of them sharing a value would be swappable with nothing
	// able to notice.

	p := self.Profile
	if p.DisplayName != "grantammons" {
		t.Errorf("Self.Profile.DisplayName = %q; want %q", p.DisplayName, "grantammons")
	}
	if p.RealName != "Grant Ammons" {
		t.Errorf("Self.Profile.RealName = %q; want %q", p.RealName, "Grant Ammons")
	}
	if p.AvatarHash != "abc123def456" {
		t.Errorf("Self.Profile.AvatarHash = %q; want %q", p.AvatarHash, "abc123def456")
	}
	if p.ImageOriginal != "https://avatars.example/orig.png" {
		t.Errorf("Self.Profile.ImageOriginal = %q; want %q", p.ImageOriginal, "https://avatars.example/orig.png")
	}
	if p.Email != "grant@example.com" {
		t.Errorf("Self.Profile.Email = %q; want %q", p.Email, "grant@example.com")
	}
	if p.StatusText != "in a meeting" {
		t.Errorf("Self.Profile.StatusText = %q; want %q", p.StatusText, "in a meeting")
	}
	if p.StatusEmoji != ":spiral_calendar_pad:" {
		t.Errorf("Self.Profile.StatusEmoji = %q; want %q", p.StatusEmoji, ":spiral_calendar_pad:")
	}
	if p.StatusExpiration != 1783339999 {
		t.Errorf("Self.Profile.StatusExpiration = %d; want %d", p.StatusExpiration, int64(1783339999))
	}
}

// TestUserBoot_DecodesTeam pins the workspace record. Every field here
// is a string and four of them (id, name, domain, url) are adjacent, so
// the assertion values are deliberately all different from each other
// and from anything in Self.
func TestUserBoot_DecodesTeam(t *testing.T) {
	res, _ := mustBoot(t, fullBootBody)

	want := Team{
		ID:            "T04T4TH8W",
		Name:          "Truelist",
		Domain:        "truelist-hq",
		URL:           "https://truelist-hq.slack.com/",
		AvatarBaseURL: "https://ca.slack-edge.com/",
	}
	if res.Team != want {
		t.Errorf("Team = %#v; want %#v", res.Team, want)
	}
}

// ---------------------------------------------------------------- mute

// TestUserBoot_ExposesRawMutePrefs pins the two pref strings this
// package pulls out by name, and proves the important one is usable by
// its existing consumer.
//
// The plan's spec claims prefs carries a flat `muted_channels` list. It
// does not: all 702 keys of the captured response were checked and
// there is no such key. Mute state lives inside all_notifications_prefs,
// whose value is a JSON-encoded *string* (a Slack quirk), and
// slackclient.ParseMutedFromAllNotificationsPrefs already decodes it. The
// legacy field is still surfaced because slk's existing code merges it
// for older workspaces.
//
// The two fixture values name disjoint sets on purpose, so swapping the
// two tags cannot pass.
func TestUserBoot_ExposesRawMutePrefs(t *testing.T) {
	res, _ := mustBoot(t, fullBootBody)

	const wantAll = `{"channels":{"C94H848UB":{"muted":true,"suppress_at_channel":false},"C04T4TH9N":{"muted":false},"C2QPK1V44":{"muted":true}}}`
	if got := res.Prefs.AllNotificationsPrefs; got != wantAll {
		t.Errorf("Prefs.AllNotificationsPrefs =\n  %s\nwant\n  %s", got, wantAll)
	}
	if got, want := res.Prefs.MutedChannels, "CLEGACY01,CLEGACY02"; got != want {
		t.Errorf("Prefs.MutedChannels = %q; want %q", got, want)
	}

	// The string is not merely preserved, it is the shape the existing
	// parser expects. C04T4TH9N carries "muted": false and must not
	// appear.
	muted := slackclient.ParseMutedFromAllNotificationsPrefs(res.Prefs.AllNotificationsPrefs)
	slices.Sort(muted)
	if want := []string{"C2QPK1V44", "C94H848UB"}; !slices.Equal(muted, want) {
		t.Errorf("ParseMutedFromAllNotificationsPrefs(AllNotificationsPrefs) = %v; want %v", muted, want)
	}
}

// TestUserBoot_KeepsAllOtherPrefsRaw pins that the remaining ~700 pref
// keys survive undecoded. Modelling them would be 700 fields of churn;
// dropping them would mean a second users.prefs.get call, which is the
// exact round trip this endpoint exists to remove.
func TestUserBoot_KeepsAllOtherPrefsRaw(t *testing.T) {
	res, _ := mustBoot(t, fullBootBody)

	if len(res.Prefs.Raw) == 0 {
		t.Fatal("Prefs.Raw is empty; the other ~700 pref keys must survive undecoded")
	}
	var all map[string]any
	if err := json.Unmarshal(res.Prefs.Raw, &all); err != nil {
		t.Fatalf("Prefs.Raw is not a JSON object: %v (%s)", err, res.Prefs.Raw)
	}
	if got, want := all["emoji_mode"], "default"; got != want {
		t.Errorf("Prefs.Raw[emoji_mode] = %v; want %v", got, want)
	}
	if got := all["mute_sounds"]; got != true {
		t.Errorf("Prefs.Raw[mute_sounds] = %v; want true", got)
	}
	if got := all["arrow_history"]; got != false {
		t.Errorf("Prefs.Raw[arrow_history] = %v; want false", got)
	}
	// The two named prefs stay in the raw blob too — pulling them out
	// is additive, not a move.
	if _, ok := all["all_notifications_prefs"]; !ok {
		t.Error("Prefs.Raw lost all_notifications_prefs")
	}
}

// TestPrefsUnmarshalJSONCopiesItsInput pins the copy in
// Prefs.UnmarshalJSON.
//
// This is unobservable through UserBoot today — the body it decodes is
// freshly read and never reused — so it is pinned directly against the
// method, the same way edge's TestFetchInfo_DoesNotMergeAnErroredBatch
// is. It is not decoration: encoding/json's Unmarshaler contract says
// outright that "UnmarshalJSON must copy the JSON data if it wishes to
// retain the data after returning", so aliasing is a latent bug that
// activates the first time a caller decodes from a pooled or reused
// buffer, and it would show up as prefs that silently mutate under a
// Result the caller already holds.
func TestPrefsUnmarshalJSONCopiesItsInput(t *testing.T) {
	buf := []byte(`{"all_notifications_prefs":"{}","emoji_mode":"default"}`)

	var p Prefs
	if err := p.UnmarshalJSON(buf); err != nil {
		t.Fatalf("Prefs.UnmarshalJSON: %v", err)
	}
	before := string(p.Raw)
	if !strings.Contains(before, "emoji_mode") {
		t.Fatalf("Prefs.Raw = %q; it never captured the input, so this test would be vacuous", before)
	}

	// Simulate the caller reusing its buffer, which encoding/json
	// explicitly permits.
	for i := range buf {
		buf[i] = 'x'
	}

	if got := string(p.Raw); got != before {
		t.Errorf("Prefs.Raw became %q after its input buffer was overwritten; it aliases the caller's bytes instead of copying them", got)
	}
}

// -------------------------------------------------------------- errors

// TestUserBoot_OKFalseIsAnError pins Slack's application-level failure
// mode. The Web API answers HTTP 200 for these, so `ok` is the only
// signal; without the check a token-revoked response would sail through
// as a workspace with zero channels and zero DMs, and slk would render
// an empty client rather than say anything.
func TestUserBoot_OKFalseIsAnError(t *testing.T) {
	var rec recordedCall
	res, err := UserBoot(context.Background(), stubPost(&rec, `{"ok":false,"error":"invalid_auth"}`, nil))
	if err == nil {
		t.Fatal("UserBoot returned nil error for ok:false")
	}
	if !strings.Contains(err.Error(), "invalid_auth") {
		t.Errorf("error = %q; want it to carry Slack's error string %q", err, "invalid_auth")
	}
	if res != nil {
		t.Errorf("UserBoot returned a non-nil Result alongside an error: %#v", res)
	}
}

// TestUserBoot_OKFalseWithAFullBodyReturnsNoData is the partial-data
// seam, pinned directly.
//
// encoding/json fills the struct before anything inspects `ok`, so at
// the moment the check runs there is a fully populated Result sitting
// in a local. Returning it — or returning a pointer to it "for
// diagnostics" — would hand the caller a plausible-looking workspace
// built from a response the server explicitly rejected. Every earlier
// task in this plan grew a surviving mutant at exactly this seam.
func TestUserBoot_OKFalseWithAFullBodyReturnsNoData(t *testing.T) {
	body := strings.Replace(fullBootBody, `"ok": true`, `"ok": false, "error": "team_added_to_org"`, 1)
	if !strings.Contains(body, "team_added_to_org") {
		t.Fatal("fixture rewrite failed; this test would be vacuous")
	}
	// Sanity: the rewritten body still carries the data that must NOT
	// come back, so a passing test is not just passing on an empty body.
	if !strings.Contains(body, "C04T4TH9N") {
		t.Fatal("rewritten fixture lost its channels; this test would be vacuous")
	}

	var rec recordedCall
	res, err := UserBoot(context.Background(), stubPost(&rec, body, nil))
	if err == nil {
		t.Fatal("UserBoot returned nil error for ok:false")
	}
	if !strings.Contains(err.Error(), "team_added_to_org") {
		t.Errorf("error = %q; want it to carry %q", err, "team_added_to_org")
	}
	if res != nil {
		t.Fatalf("UserBoot returned %d channels alongside an error; a rejected response must yield no data",
			len(res.Channels))
	}
}

// TestUserBoot_OKFalseWithNoErrorFieldStillSaysSomething guards against
// an error message that ends in a dangling colon and carries no
// diagnostic at all.
func TestUserBoot_OKFalseWithNoErrorFieldStillSaysSomething(t *testing.T) {
	var rec recordedCall
	_, err := UserBoot(context.Background(), stubPost(&rec, `{"ok":false}`, nil))
	if err == nil {
		t.Fatal("UserBoot returned nil error for ok:false")
	}
	if !strings.Contains(err.Error(), "ok=false") {
		t.Errorf("error = %q; want it to explain that ok was false", err)
	}
}

// TestUserBoot_MalformedJSONIsAnErrorWithNoData covers the other half
// of the partial-data seam. encoding/json keeps decoding past the first
// type error, so a body with one bad field populates everything else
// AND returns an error. Nothing may escape.
func TestUserBoot_MalformedJSONIsAnErrorWithNoData(t *testing.T) {
	// Valid JSON, wrong type for one modelled field, and `ok` is true
	// so the ok check cannot be what rejects it.
	const body = `{"ok":true,"self":{"id":"U04T4THAA"},"channels":"not-an-array"}`

	var rec recordedCall
	res, err := UserBoot(context.Background(), stubPost(&rec, body, nil))
	if err == nil {
		t.Fatal("UserBoot returned nil error for a type-mismatched body")
	}
	if res != nil {
		t.Fatalf("UserBoot returned a Result alongside a decode error: Self.ID=%q", res.Self.ID)
	}
}

func TestUserBoot_NotJSONIsAnError(t *testing.T) {
	var rec recordedCall
	res, err := UserBoot(context.Background(), stubPost(&rec, `<html>proxy says no</html>`, nil))
	if err == nil {
		t.Fatal("UserBoot returned nil error for a non-JSON body")
	}
	if res != nil {
		t.Error("UserBoot returned a non-nil Result for a non-JSON body")
	}
}

// TestUserBoot_PropagatesPostError pins that a transport failure is
// reported rather than turned into an empty workspace.
func TestUserBoot_PropagatesPostError(t *testing.T) {
	sentinel := errors.New("HTTP 503: edge unavailable")
	var rec recordedCall
	res, err := UserBoot(context.Background(), stubPost(&rec, "", sentinel))
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v; want it to wrap %v", err, sentinel)
	}
	if res != nil {
		t.Error("UserBoot returned a non-nil Result alongside a transport error")
	}
}

// ------------------------------------------------------------ tolerance

// TestUserBoot_IgnoresUnknownFields pins forward compatibility. Slack
// adds top-level and nested keys to this response without notice; a
// decoder that rejected them would break slk on Slack's schedule.
func TestUserBoot_IgnoresUnknownFields(t *testing.T) {
	const body = `{
	  "ok": true,
	  "a_brand_new_top_level_key": {"nested": [1, 2, 3]},
	  "another_one": "surprise",
	  "channels": [
	    {"id": "C04T4TH9N", "name": "general", "updated": 1783337533019,
	     "a_brand_new_channel_key": {"deeply": {"nested": true}}}
	  ],
	  "dnd": {"dnd_enabled": true, "a_brand_new_dnd_key": 7},
	  "self": {"id": "U04T4THAA", "profile": {"a_brand_new_profile_key": []}},
	  "prefs": {"all_notifications_prefs": "{}", "a_brand_new_pref": 1}
	}`
	res, _ := mustBoot(t, body)

	if len(res.Channels) != 1 || res.Channels[0].ID != "C04T4TH9N" {
		t.Errorf("Channels = %#v; want the one general channel", res.Channels)
	}
	if res.Channels[0].Version != 1783337533019 {
		t.Errorf("Channels[0].Version = %d; want 1783337533019", res.Channels[0].Version)
	}
	if !res.DND.Enabled {
		t.Error("DND.Enabled = false; want true")
	}
	if res.Self.ID != "U04T4THAA" {
		t.Errorf("Self.ID = %q; want %q", res.Self.ID, "U04T4THAA")
	}
	if res.Prefs.AllNotificationsPrefs != "{}" {
		t.Errorf("Prefs.AllNotificationsPrefs = %q; want %q", res.Prefs.AllNotificationsPrefs, "{}")
	}
}

// TestPostFuncMatchesClientPostForm is a compile-time assertion, not a
// runtime one. Phase 2b wires this parser onto slack.Client.postForm,
// whose signature is
//
//	func(ctx context.Context, method string, form url.Values) ([]byte, error)
//
// If PostFunc ever drifts from that, 2b becomes a rewrite instead of a
// one-line adapter. A local function with postForm's exact signature
// must remain assignable to PostFunc.
func TestPostFuncMatchesClientPostForm(t *testing.T) {
	postForm := func(ctx context.Context, method string, form url.Values) ([]byte, error) {
		_, _, _ = ctx, method, form
		return nil, nil
	}
	var p PostFunc = postForm
	if p == nil {
		t.Fatal("PostFunc is nil after assignment")
	}
}
