package boot

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/slackhttp"
)

// fullViewBody is a conversations.view response shaped like the
// captured one: the same 8 top-level keys, the same nesting, the same
// types. Values are synthetic but deliberately *distinct* per field so
// a swapped struct tag cannot survive, and booleans are arranged so
// that every modelled one has a unique non-zero column vector down the
// rows of its array.
//
// It also carries keys this package does not model (unlinked,
// pending_shared, parent_conversation, properties, previous_names,
// is_admin, tz_label, huddle_state, …) exactly as the capture does —
// decoding must ignore them, because Slack adds fields to this
// response without notice.
//
// Every unmodelled *string* neighbour is set to a "WRONG-…" value on
// purpose. A modelled field mis-tagged onto one of them then decodes a
// visibly wrong answer instead of a plausible one, and the assertion
// dies. Unmodelled *booleans* are all-false for the same reason: an
// all-false column matches no modelled field's column, so a mis-tag
// onto one is always caught.
//
// The channels[] entries carry the unread state measured on the real
// ones, and carry it UNEVENLY on purpose: last_read, latest,
// unread_count and unread_count_display appear on rows 1 and 4 only,
// because across the 54 observed entries they appear on 14. A fixture
// that put them on every row would pin a contract the captures do not
// support, which is the exact failure this file exists to avoid.
//
// `latest` on a channels[] entry is an OBJECT — a whole message — and
// not the ts string the top-level `channel` object carries under the
// same key. That is measured (14/14 object on entries, 2/2 string on
// the `channel` object) and it is the reason ViewChannelEntry is a
// separate type from ViewChannel rather than a reuse of it: a
// `Latest string` field would fail the entire response decode.
//
// One shape here is an honest guess and is marked as such: `bots` was
// `[]` in BOTH captures, so its element shape is unverified. This
// package models it as []json.RawMessage precisely so it claims
// nothing, and the tests below only assert that whatever bytes arrive
// survive.
const fullViewBody = `{
  "ok": true,
  "history": {
    "messages": [
      {"type": "message", "user": "U0VIEWAA1", "ts": "1783337500.000100", "client_msg_id": "cmid-0001", "text": "first message", "team": "T04T4TH8W", "blocks": [], "reactions": []},
      {"type": "message", "user": "U0VIEWBB2", "ts": "1783337500.000200", "client_msg_id": "cmid-0002", "text": "second message", "team": "T04T4TH8W", "blocks": [], "reactions": []},
      {"type": "message", "user": "U0VIEWCC3", "ts": "1783337500.000300", "client_msg_id": "cmid-0003", "text": "third message", "team": "T04T4TH8W", "blocks": [], "reactions": []}
    ],
    "has_more": true,
    "mutation_timestamps": {
      "latest": "17833371111111111",
      "updated": "17833372222222222",
      "history_invalid": "17833373333333333"
    },
    "channel_actions_ts": null,
    "channel_actions_count": 7,
    "next_ts": 1783337540
  },
  "users": [
    {
      "id": "U0VIEWAA1", "team_id": "T04T4TH8W", "name": "aardvark",
      "real_name": "Top Level Aardvark", "updated": 1783337561001,
      "deleted": false, "is_bot": false, "is_app_user": false,
      "color": "WRONG-color-1", "tz": "WRONG-tz-1", "tz_label": "WRONG-tzlabel-1", "tz_offset": -18000,
      "is_admin": false, "is_owner": false, "is_primary_owner": false,
      "is_restricted": false, "is_ultra_restricted": false, "is_email_confirmed": false,
      "who_can_share_contact_card": "WRONG-share-1",
      "profile": {
        "real_name": "Profile Aardvark", "display_name": "aard-display",
        "avatar_hash": "hash-aaa111", "image_original": "https://avatars.example/aaa.png",
        "status_text": "aardvark status", "status_emoji": ":ant:", "status_expiration": 1783339001,
        "real_name_normalized": "WRONG-rnn-1", "display_name_normalized": "WRONG-dnn-1",
        "is_custom_image": false, "first_name": "WRONG-first-1", "last_name": "WRONG-last-1",
        "team": "WRONG-team-1", "title": "WRONG-title-1", "phone": "WRONG-phone-1",
        "skype": "WRONG-skype-1", "status_text_canonical": "WRONG-stc-1",
        "status_emoji_display_info": [], "who_can_share_contact_card": "WRONG-share-p1",
        "huddle_state": "WRONG-huddle-1", "huddle_state_expiration_ts": 0
      }
    },
    {
      "id": "U0VIEWBB2", "team_id": "T7777777Z", "name": "badger",
      "real_name": "Top Level Badger", "updated": 1783337561002,
      "deleted": true, "is_bot": false, "is_app_user": false,
      "color": "WRONG-color-2", "tz": "WRONG-tz-2", "tz_label": "WRONG-tzlabel-2", "tz_offset": 0,
      "is_admin": false, "is_owner": false, "is_primary_owner": false,
      "is_restricted": false, "is_ultra_restricted": false, "is_email_confirmed": false,
      "who_can_share_contact_card": "WRONG-share-2",
      "profile": {
        "real_name": "Profile Badger", "display_name": "badge-display",
        "avatar_hash": "hash-bbb222", "image_original": "https://avatars.example/bbb.png",
        "status_text": "badger status", "status_emoji": ":badger:", "status_expiration": 1783339002,
        "real_name_normalized": "WRONG-rnn-2", "display_name_normalized": "WRONG-dnn-2",
        "is_custom_image": false, "first_name": "WRONG-first-2", "last_name": "WRONG-last-2",
        "team": "WRONG-team-2", "title": "WRONG-title-2", "phone": "WRONG-phone-2",
        "skype": "WRONG-skype-2", "status_text_canonical": "WRONG-stc-2",
        "status_emoji_display_info": [], "who_can_share_contact_card": "WRONG-share-p2",
        "huddle_state": "WRONG-huddle-2", "huddle_state_expiration_ts": 0
      }
    },
    {
      "id": "U0VIEWCC3", "team_id": "T04T4TH8W", "name": "coyote",
      "real_name": "Top Level Coyote", "updated": 1783337561003,
      "deleted": false, "is_bot": true, "is_app_user": false,
      "color": "WRONG-color-3", "tz": "WRONG-tz-3", "tz_label": "WRONG-tzlabel-3", "tz_offset": 0,
      "is_admin": false, "is_owner": false, "is_primary_owner": false,
      "is_restricted": false, "is_ultra_restricted": false, "is_email_confirmed": false,
      "who_can_share_contact_card": "WRONG-share-3",
      "profile": {
        "real_name": "Profile Coyote", "display_name": "coy-display",
        "avatar_hash": "hash-ccc333", "image_original": "https://avatars.example/ccc.png",
        "status_text": "coyote status", "status_emoji": ":wolf:", "status_expiration": 1783339003,
        "real_name_normalized": "WRONG-rnn-3", "display_name_normalized": "WRONG-dnn-3",
        "is_custom_image": false, "first_name": "WRONG-first-3", "last_name": "WRONG-last-3",
        "team": "WRONG-team-3", "title": "WRONG-title-3", "phone": "WRONG-phone-3",
        "skype": "WRONG-skype-3", "status_text_canonical": "WRONG-stc-3",
        "status_emoji_display_info": [], "who_can_share_contact_card": "WRONG-share-p3",
        "huddle_state": "WRONG-huddle-3", "huddle_state_expiration_ts": 0
      }
    },
    {
      "id": "U0VIEWDD4", "team_id": "T04T4TH8W", "name": "dingo",
      "real_name": "Top Level Dingo", "updated": 1783337561004,
      "deleted": false, "is_bot": false, "is_app_user": true,
      "color": "WRONG-color-4", "tz": "WRONG-tz-4", "tz_label": "WRONG-tzlabel-4", "tz_offset": 0,
      "is_admin": false, "is_owner": false, "is_primary_owner": false,
      "is_restricted": false, "is_ultra_restricted": false, "is_email_confirmed": false,
      "who_can_share_contact_card": "WRONG-share-4",
      "profile": {
        "real_name": "Profile Dingo", "display_name": "ding-display",
        "avatar_hash": "hash-ddd444", "image_original": "https://avatars.example/ddd.png",
        "status_text": "dingo status", "status_emoji": ":dog:", "status_expiration": 1783339004,
        "real_name_normalized": "WRONG-rnn-4", "display_name_normalized": "WRONG-dnn-4",
        "is_custom_image": false, "first_name": "WRONG-first-4", "last_name": "WRONG-last-4",
        "team": "WRONG-team-4", "title": "WRONG-title-4", "phone": "WRONG-phone-4",
        "skype": "WRONG-skype-4", "status_text_canonical": "WRONG-stc-4",
        "status_emoji_display_info": [], "who_can_share_contact_card": "WRONG-share-p4",
        "huddle_state": "WRONG-huddle-4", "huddle_state_expiration_ts": 0
      }
    },
    {
      "id": "U0VIEWEE5", "team_id": "T7777777Z", "name": "emu",
      "real_name": "Top Level Emu", "updated": 1783337561005,
      "deleted": true, "is_bot": true, "is_app_user": true,
      "color": "WRONG-color-5", "tz": "WRONG-tz-5", "tz_label": "WRONG-tzlabel-5", "tz_offset": 0,
      "is_admin": false, "is_owner": false, "is_primary_owner": false,
      "is_restricted": false, "is_ultra_restricted": false, "is_email_confirmed": false,
      "who_can_share_contact_card": "WRONG-share-5",
      "profile": {
        "real_name": "Profile Emu", "display_name": "emu-display",
        "avatar_hash": "hash-eee555", "image_original": "https://avatars.example/eee.png",
        "status_text": "emu status", "status_emoji": ":bird:", "status_expiration": 1783339005,
        "real_name_normalized": "WRONG-rnn-5", "display_name_normalized": "WRONG-dnn-5",
        "is_custom_image": false, "first_name": "WRONG-first-5", "last_name": "WRONG-last-5",
        "team": "WRONG-team-5", "title": "WRONG-title-5", "phone": "WRONG-phone-5",
        "skype": "WRONG-skype-5", "status_text_canonical": "WRONG-stc-5",
        "status_emoji_display_info": [], "who_can_share_contact_card": "WRONG-share-p5",
        "huddle_state": "WRONG-huddle-5", "huddle_state_expiration_ts": 0
      }
    }
  ],
  "bots": [],
  "channels": [
    {
      "id": "C0VIEW0001", "name": "general", "name_normalized": "general-norm",
      "is_channel": true, "is_group": false, "is_im": false, "is_mpim": false,
      "is_private": false, "created": 1670000001, "updated": 1783337533019,
      "is_archived": false, "is_general": true, "unlinked": 0,
      "is_shared": true, "is_org_shared": true, "is_pending_ext_shared": false,
      "is_ext_shared": false, "is_member": true,
      "pending_shared": [], "parent_conversation": null,
      "context_team_id": "T04T4TH8W", "creator": "U0VIEWAA1",
      "shared_team_ids": ["T04T4TH8W", "T7777777Z"], "pending_connected_team_ids": [],
      "topic": {"value": "company announcements", "creator": "U0VIEWAA1", "last_set": 1670000101},
      "purpose": {"value": "all hands", "creator": "U0VIEWBB2", "last_set": 1670000102},
      "properties": {"tabs": [], "canvas": {"file_id": "F0AAA"}},
      "previous_names": ["genral"],
      "last_read": "1783337511.000111",
      "latest": {"type": "message", "user": "U0VIEWAA1", "ts": "1783337512.000112", "text": "latest in general"},
      "unread_count": 41,
      "unread_count_display": 17
    },
    {
      "id": "C0VIEW0002", "name": "secret-plans", "name_normalized": "secret-plans-norm",
      "is_channel": false, "is_group": true, "is_im": false, "is_mpim": false,
      "is_private": true, "created": 1670000002, "updated": 1783337533020,
      "is_archived": true, "is_general": false, "unlinked": 0,
      "is_shared": true, "is_org_shared": true, "is_pending_ext_shared": false,
      "is_ext_shared": false, "is_member": false,
      "pending_shared": [], "parent_conversation": null,
      "context_team_id": "T7777777Z", "creator": "U0VIEWCC3",
      "shared_team_ids": ["T04T4TH8W", "T7777777Z"], "pending_connected_team_ids": [],
      "topic": {"value": "hush", "creator": "U0VIEWCC3", "last_set": 1670000103},
      "purpose": {"value": "plotting", "creator": "U0VIEWCC3", "last_set": 1670000104},
      "properties": {}, "previous_names": []
    },
    {
      "id": "C0VIEW0003", "name": "mpdm-a--b--c-1", "name_normalized": "mpdm-a--b--c-1",
      "is_channel": false, "is_group": false, "is_im": false, "is_mpim": true,
      "is_private": true, "created": 1670000003, "updated": 1783337533021,
      "is_archived": true, "is_general": false, "unlinked": 0,
      "is_shared": false, "is_org_shared": false, "is_pending_ext_shared": false,
      "is_ext_shared": false, "is_member": true,
      "pending_shared": [], "parent_conversation": null,
      "context_team_id": "T04T4TH8W", "creator": "U0VIEWDD4",
      "shared_team_ids": ["T04T4TH8W"], "pending_connected_team_ids": [],
      "topic": {"value": "", "creator": "", "last_set": 0},
      "purpose": {"value": "Group messaging", "creator": "U0VIEWDD4", "last_set": 1670000105},
      "properties": {}, "previous_names": []
    },
    {
      "id": "C0VIEW0004", "name": "partner-sync", "name_normalized": "partner-sync-norm",
      "is_channel": true, "is_group": false, "is_im": false, "is_mpim": false,
      "is_private": true, "created": 1670000004, "updated": 1783337533022,
      "is_archived": false, "is_general": false, "unlinked": 0,
      "is_shared": true, "is_org_shared": false, "is_pending_ext_shared": false,
      "is_ext_shared": true, "is_member": false,
      "pending_shared": [], "parent_conversation": null,
      "context_team_id": "T04T4TH8W", "creator": "U0VIEWEE5",
      "shared_team_ids": ["T04T4TH8W", "E11EXTERN"], "pending_connected_team_ids": [],
      "topic": {"value": "partner integration", "creator": "U0VIEWEE5", "last_set": 1670000106},
      "purpose": {"value": "cross-org work", "creator": "U0VIEWEE5", "last_set": 1670000107},
      "properties": {}, "previous_names": [],
      "last_read": "1783337544.000444",
      "latest": {"type": "message", "user": "U0VIEWEE5", "ts": "1783337545.000445", "text": "latest in partner-sync"},
      "unread_count": 6,
      "unread_count_display": 3
    }
  ],
  "emojis": {
    "rls-heart": "https://emoji.slack-edge.com/T04T4TH8W/rls-heart/1111aaaa.png",
    "oregon-trail": "https://emoji.slack-edge.com/T04T4TH8W/oregon-trail/2222bbbb.gif",
    "woohoo": "https://emoji.slack-edge.com/T04T4TH8W/woohoo/3333cccc.png"
  },
  "channel": {
    "id": "C0OPENED99", "name": "the-opened-one", "name_normalized": "the-opened-one-norm",
    "is_channel": true, "is_group": false, "is_im": false, "is_mpim": false,
    "is_private": false, "created": 1660000009, "updated": 1783337599009,
    "is_archived": false, "is_general": false, "unlinked": 0,
    "is_shared": false, "is_org_shared": true, "is_pending_ext_shared": false,
    "is_ext_shared": false, "is_member": true,
    "pending_shared": [], "parent_conversation": null,
    "context_team_id": "T04T4TH8W", "creator": "U0VIEWAA1",
    "shared_team_ids": ["T04T4TH8W"], "pending_connected_team_ids": [],
    "topic": {"value": "opened topic", "creator": "U0VIEWAA1", "last_set": 1670000201},
    "purpose": {"value": "opened purpose", "creator": "U0VIEWBB2", "last_set": 1670000202},
    "last_read": "17833374444444444",
    "latest": "17833375555555555",
    "unread_count": 12,
    "unread_count_display": 5,
    "is_thread_only": false,
    "is_read_only": false,
    "properties": {"tabs": [], "tabz": []},
    "previous_names": ["the-opened-one-was-called-this"]
  },
  "response_metadata": {"next_cursor": "bmV4dC1jdXJzb3ItMzItY2hhcnMtbG9uZzEy"}
}`

// viewBusinessParams is the exact set of form keys ConversationsView
// sends when channelID is empty — that is, the captured request minus
// the two things this package must never send itself.
//
// token is injected by the caller's postForm; _x_sonic and _x_app_name
// are added by slackhttp.BrowserTransport. conversations.view sends
// neither _x_reason nor _x_mode at all, so there is nothing else.
var viewBusinessParams = []string{
	"canonical_avatars",
	"count",
	"ignore_replies",
	"include_free_team_extra_messages",
	"include_full_users",
	"include_mutation_timestamps",
	"include_stories",
	"include_use_case",
	"no_members",
	"no_self",
	"no_user_profile",
}

// viewBooleanParams is every business param whose value is the STRING
// "true". Asserting them individually, not by count, is the point:
// eleven params that all carry the same value are freely swappable, so
// only a per-key assertion plus an exact key set can tell them apart.
var viewBooleanParams = []string{
	"canonical_avatars",
	"ignore_replies",
	"include_free_team_extra_messages",
	"include_full_users",
	"include_mutation_timestamps",
	"include_stories",
	"include_use_case",
	"no_members",
	"no_self",
	"no_user_profile",
}

// mustView runs ConversationsView against a canned body and fails the
// test if it errors.
func mustView(t *testing.T, body, channelID string) (*ViewResult, *recordedCall) {
	t.Helper()
	var rec recordedCall
	res, err := ConversationsView(context.Background(), stubPost(&rec, body, nil), channelID)
	if err != nil {
		t.Fatalf("ConversationsView: unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("ConversationsView returned a nil ViewResult with a nil error")
	}
	return res, &rec
}

// formKeys returns the sorted key set of a recorded form.
func formKeys(rec *recordedCall) []string {
	var got []string
	for k := range rec.form {
		got = append(got, k)
	}
	slices.Sort(got)
	return got
}

// ------------------------------------------------------------- request

// TestConversationsView_SendsExactlyTheObservedBusinessParams pins the
// form body against the captured request's business params — and, just
// as importantly, pins what is NOT there.
//
// token, _x_sonic and _x_app_name all appear in the captured request
// but none is this package's job: token is injected by the caller's
// postForm, and the _x_ fields are added by
// slackhttp.BrowserTransport. If this package added any of them the
// wire body would carry a duplicate, which is a fingerprint in exactly
// the way this whole effort exists to avoid. An exact key set is the
// only assertion that catches that.
//
// _x_reason and _x_mode appear in NEITHER capture of this endpoint,
// and slackhttp now suppresses both for it (commits 4df0f14 and
// 7a3293d). Sending one from here would defeat that suppression
// outright: applyEnvelopeBody only ever appends, so a form param the
// caller supplies is the one route by which a suppressed _x_reason can
// come back.
func TestConversationsView_SendsExactlyTheObservedBusinessParams(t *testing.T) {
	_, rec := mustView(t, fullViewBody, "")

	if rec.calls != 1 {
		t.Errorf("post called %d times; want exactly 1 (the whole point is one call replacing ~30)", rec.calls)
	}
	if rec.method != "conversations.view" {
		t.Errorf("method = %q; want %q", rec.method, "conversations.view")
	}

	if got := formKeys(rec); !slices.Equal(got, viewBusinessParams) {
		t.Errorf("form keys = %v;\nwant exactly     %v", got, viewBusinessParams)
	}

	// count is the string "28" — this is a form body, so the JSON
	// number 28 would be a different value, and 28 itself is measured,
	// not chosen.
	if got := rec.form.Get("count"); got != "28" {
		t.Errorf("form[count] = %q; want %q (measured, byte-identical in both captures)", got, "28")
	}

	// Every boolean param individually. A count-based assertion cannot
	// distinguish these from each other, because they all carry the
	// same value.
	for _, key := range viewBooleanParams {
		if got := rec.form.Get(key); got != "true" {
			t.Errorf("form[%s] = %q; want %q", key, got, "true")
		}
	}
}

// TestConversationsView_AddsNoXParams is the same guarantee stated
// directly, so a future edit that adds an _x_ param fails with a
// message that says why rather than as a key-set diff.
func TestConversationsView_AddsNoXParams(t *testing.T) {
	for _, channelID := range []string{"", "C0OPENED99"} {
		_, rec := mustView(t, fullViewBody, channelID)
		for k := range rec.form {
			if strings.HasPrefix(k, "_x_") {
				t.Errorf("form carries %q (channelID=%q); the _x_ envelope belongs to slackhttp.BrowserTransport, and conversations.view sends neither _x_reason nor _x_mode at all",
					k, channelID)
			}
		}
	}
}

// TestConversationsView_OmitsChannelWhenEmpty pins the captured
// request shape exactly: both HAR captures of this endpoint carried NO
// channel param.
//
// "Absent" and "present but empty" are different bodies. A
// `channel=` on the wire was observed in neither capture and is the
// one request shape guaranteed not to match the real client, so this
// asserts the key is missing from the SET, not that its value is "".
func TestConversationsView_OmitsChannelWhenEmpty(t *testing.T) {
	_, rec := mustView(t, fullViewBody, "")

	keys := formKeys(rec)
	if slices.Contains(keys, "channel") {
		t.Errorf("form keys = %v; `channel` must be ABSENT for an empty channelID, not present-and-empty (the captured request carried none)", keys)
	}
	if _, ok := rec.form["channel"]; ok {
		t.Errorf("form has a `channel` key with value %q; it must not be set at all", rec.form.Get("channel"))
	}
}

// TestConversationsView_SendsChannelWhenNonEmpty pins the other half.
//
// The channel param is UNVERIFIED — see ConversationsView's doc
// comment. This test pins what slk sends, not that Slack honours it.
func TestConversationsView_SendsChannelWhenNonEmpty(t *testing.T) {
	_, rec := mustView(t, fullViewBody, "C0OPENED99")

	want := append(slices.Clone(viewBusinessParams), "channel")
	slices.Sort(want)
	if got := formKeys(rec); !slices.Equal(got, want) {
		t.Errorf("form keys = %v;\nwant exactly     %v", got, want)
	}
	if got := rec.form.Get("channel"); got != "C0OPENED99" {
		t.Errorf("form[channel] = %q; want %q", got, "C0OPENED99")
	}

	// The rest of the body must be unchanged by adding a channel.
	if got := rec.form.Get("count"); got != "28" {
		t.Errorf("form[count] = %q; want %q", got, "28")
	}
	for _, key := range viewBooleanParams {
		if got := rec.form.Get(key); got != "true" {
			t.Errorf("form[%s] = %q; want %q", key, got, "true")
		}
	}
}

// TestConversationsView_DoesNotOverrideACallerSuppliedReason pins that
// this package leaves the context alone.
//
// conversations.view is in slackhttp's xReasonExcludedMethods, so the
// transport drops _x_reason for it regardless of what the context
// says — deliberately, since the official client sends none. This
// package must not try to fight that, and in particular must not write
// _x_reason into the form body, which is the one place the transport
// cannot suppress it.
func TestConversationsView_DoesNotOverrideACallerSuppliedReason(t *testing.T) {
	var rec recordedCall
	ctx := slackhttp.WithReason(context.Background(), "caller-chose-this")
	if _, err := ConversationsView(ctx, stubPost(&rec, fullViewBody, nil), "C0OPENED99"); err != nil {
		t.Fatalf("ConversationsView: %v", err)
	}
	if got := slackhttp.ReasonFrom(rec.ctx); got != "caller-chose-this" {
		t.Errorf("_x_reason on the outgoing context = %q; want %q (the context must arrive at post untouched)", got, "caller-chose-this")
	}
}

// ------------------------------------------------------------- history

// TestConversationsView_DecodesHistory pins the whole history object.
// channel_actions_count and next_ts are adjacent ints with distinct
// values so a transposed tag dies; has_more is true so the assertion
// cannot pass against a field that was never decoded.
func TestConversationsView_DecodesHistory(t *testing.T) {
	res, _ := mustView(t, fullViewBody, "C0OPENED99")
	h := res.History

	if !h.HasMore {
		t.Error("History.HasMore = false; want true")
	}
	if h.ChannelActionsCount != 7 {
		t.Errorf("History.ChannelActionsCount = %d; want 7", h.ChannelActionsCount)
	}
	if h.NextTS != 1783337540 {
		t.Errorf("History.NextTS = %d; want 1783337540 (an INT on the wire, unlike every other ts here)", h.NextTS)
	}
	if h.ChannelActionsCount == int(h.NextTS) {
		t.Error("ChannelActionsCount and NextTS decoded to the same value; the fixture must keep them distinct or a tag swap survives")
	}
}

// TestConversationsView_DecodesMessagesRaw pins the substance of the
// call.
//
// Messages stay []json.RawMessage for Phase 2a — see the field's doc
// comment — so the contract asserted here is only that the right
// number of messages arrive, in order, with their bytes intact for
// Phase 2b to hand to slack-go.
func TestConversationsView_DecodesMessagesRaw(t *testing.T) {
	res, _ := mustView(t, fullViewBody, "C0OPENED99")

	if len(res.History.Messages) != 3 {
		t.Fatalf("len(History.Messages) = %d; want 3", len(res.History.Messages))
	}
	for i, wantTS := range []string{"1783337500.000100", "1783337500.000200", "1783337500.000300"} {
		var m struct {
			TS   string `json:"ts"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(res.History.Messages[i], &m); err != nil {
			t.Fatalf("History.Messages[%d] is not a JSON object: %v (%s)", i, err, res.History.Messages[i])
		}
		if m.TS != wantTS {
			t.Errorf("History.Messages[%d].ts = %q; want %q (order must be preserved)", i, m.TS, wantTS)
		}
	}
	// The bytes must survive whole, not merely parse: Phase 2b decodes
	// these into slack.Message, which reads keys this test does not.
	if got := string(res.History.Messages[0]); !strings.Contains(got, `"client_msg_id": "cmid-0001"`) {
		t.Errorf("History.Messages[0] lost fields in transit: %s", got)
	}
}

// TestConversationsView_MutationTimestampsAreStrings pins the type of
// three fields that look exactly like integers and are not.
//
// They are 17-character strings ("17833371111111111"). An int64 field
// here does not truncate — it fails the entire response decode, so the
// channel open is lost. They are also cache tokens to be echoed back
// verbatim, so string is the correct semantic type as well as the
// tolerant one.
//
// The three values are distinct so a transposed tag among them dies.
func TestConversationsView_MutationTimestampsAreStrings(t *testing.T) {
	res, _ := mustView(t, fullViewBody, "C0OPENED99")

	want := MutationTimestamps{
		Latest:         "17833371111111111",
		Updated:        "17833372222222222",
		HistoryInvalid: "17833373333333333",
	}
	if got := res.History.MutationTimestamps; got != want {
		t.Errorf("History.MutationTimestamps = %#v; want %#v", got, want)
	}
	// Guard against the fixture drifting into all-equal values, which
	// would make the assertion above unable to see a tag swap.
	mt := res.History.MutationTimestamps
	if mt.Latest == mt.Updated || mt.Updated == mt.HistoryInvalid || mt.Latest == mt.HistoryInvalid {
		t.Error("the three mutation timestamps are not pairwise distinct in the fixture; a swapped tag would survive")
	}
}

// TestConversationsView_ChannelActionsTSSurvivesNull covers the one
// field that was `null` in BOTH captures.
//
// Its non-null type is therefore unknown, so it is json.RawMessage: a
// typed field would be a guess, and a guess that is wrong fails the
// whole decode. This asserts only that a null decodes without error
// and is *distinguishable* from the key being absent. It deliberately
// does NOT assert a zero value as though the null were evidence of
// one.
func TestConversationsView_ChannelActionsTSSurvivesNull(t *testing.T) {
	res, _ := mustView(t, fullViewBody, "C0OPENED99")

	if got := string(res.History.ChannelActionsTS); got != "null" {
		t.Errorf("History.ChannelActionsTS = %q; want the literal %q — the server said null, and that is a different fact from the key being absent",
			got, "null")
	}

	// Absent key: nil, not "null". The distinction is the reason
	// json.RawMessage is the right type here.
	absent, _ := mustView(t, `{"ok":true,"history":{"has_more":false}}`, "C0OPENED99")
	if absent.History.ChannelActionsTS != nil {
		t.Errorf("History.ChannelActionsTS = %q for a body that never mentioned the key; want nil",
			absent.History.ChannelActionsTS)
	}

	// And a future capture with a real value must survive untouched.
	valued, _ := mustView(t, `{"ok":true,"history":{"channel_actions_ts":"1783337533.019174"}}`, "C0OPENED99")
	if got := string(valued.History.ChannelActionsTS); got != `"1783337533.019174"` {
		t.Errorf("History.ChannelActionsTS = %s; want the raw bytes preserved", got)
	}
}

// TestConversationsView_HasMoreDecodesBothWays stops has_more being
// satisfied by a constant. The full fixture has it true; a false one
// must come back false.
func TestConversationsView_HasMoreDecodesBothWays(t *testing.T) {
	res, _ := mustView(t, fullViewBody, "C0OPENED99")
	if !res.History.HasMore {
		t.Error("History.HasMore = false for a body with has_more:true")
	}

	off, _ := mustView(t, `{"ok":true,"history":{"has_more":false,"channel_actions_count":7}}`, "C0OPENED99")
	if off.History.HasMore {
		t.Error("History.HasMore = true for a body with has_more:false; the value is inverted or hardcoded")
	}
	// Sanity: the small body really did decode, so the assertion above
	// is not passing against a struct nothing ever touched.
	if off.History.ChannelActionsCount != 7 {
		t.Fatalf("the has_more:false body did not decode at all (ChannelActionsCount = %d); the assertion above would be vacuous",
			off.History.ChannelActionsCount)
	}
}

// --------------------------------------------------------------- users

// TestConversationsView_DecodesUsers pins the array that replaces the
// per-author users.info fan-out.
//
// Every modelled boolean has a unique non-zero column vector across
// these five rows:
//
//	deleted     01001
//	is_bot      00101
//	is_app_user 00011
//
// The six unmodelled user booleans (is_admin, is_owner,
// is_primary_owner, is_restricted, is_ultra_restricted,
// is_email_confirmed) are false on all five rows on purpose, so a
// modelled field mis-tagged onto one of them goes all-false and dies.
//
// real_name and profile.real_name are DIFFERENT keys and are given
// different values, because a tag pointing at the wrong one compiles
// and decodes.
func TestConversationsView_DecodesUsers(t *testing.T) {
	res, _ := mustView(t, fullViewBody, "C0OPENED99")

	if len(res.Users) != 5 {
		t.Fatalf("len(Users) = %d; want 5", len(res.Users))
	}

	want := []User{
		{
			ID: "U0VIEWAA1", TeamID: "T04T4TH8W", Name: "aardvark",
			RealName: "Top Level Aardvark", Version: 1783337561001,
			Deleted: false, IsBot: false, IsAppUser: false,
			Profile: UserProfile{
				RealName: "Profile Aardvark", DisplayName: "aard-display",
				AvatarHash: "hash-aaa111", ImageOriginal: "https://avatars.example/aaa.png",
				StatusText: "aardvark status", StatusEmoji: ":ant:", StatusExpiration: 1783339001,
			},
		},
		{
			ID: "U0VIEWBB2", TeamID: "T7777777Z", Name: "badger",
			RealName: "Top Level Badger", Version: 1783337561002,
			Deleted: true, IsBot: false, IsAppUser: false,
			Profile: UserProfile{
				RealName: "Profile Badger", DisplayName: "badge-display",
				AvatarHash: "hash-bbb222", ImageOriginal: "https://avatars.example/bbb.png",
				StatusText: "badger status", StatusEmoji: ":badger:", StatusExpiration: 1783339002,
			},
		},
		{
			ID: "U0VIEWCC3", TeamID: "T04T4TH8W", Name: "coyote",
			RealName: "Top Level Coyote", Version: 1783337561003,
			Deleted: false, IsBot: true, IsAppUser: false,
			Profile: UserProfile{
				RealName: "Profile Coyote", DisplayName: "coy-display",
				AvatarHash: "hash-ccc333", ImageOriginal: "https://avatars.example/ccc.png",
				StatusText: "coyote status", StatusEmoji: ":wolf:", StatusExpiration: 1783339003,
			},
		},
		{
			ID: "U0VIEWDD4", TeamID: "T04T4TH8W", Name: "dingo",
			RealName: "Top Level Dingo", Version: 1783337561004,
			Deleted: false, IsBot: false, IsAppUser: true,
			Profile: UserProfile{
				RealName: "Profile Dingo", DisplayName: "ding-display",
				AvatarHash: "hash-ddd444", ImageOriginal: "https://avatars.example/ddd.png",
				StatusText: "dingo status", StatusEmoji: ":dog:", StatusExpiration: 1783339004,
			},
		},
		{
			ID: "U0VIEWEE5", TeamID: "T7777777Z", Name: "emu",
			RealName: "Top Level Emu", Version: 1783337561005,
			Deleted: true, IsBot: true, IsAppUser: true,
			Profile: UserProfile{
				RealName: "Profile Emu", DisplayName: "emu-display",
				AvatarHash: "hash-eee555", ImageOriginal: "https://avatars.example/eee.png",
				StatusText: "emu status", StatusEmoji: ":bird:", StatusExpiration: 1783339005,
			},
		},
	}
	for i, w := range want {
		if got := res.Users[i]; got != w {
			t.Errorf("Users[%d] =\n  %#v\nwant\n  %#v", i, got, w)
		}
	}
}

// ------------------------------------------------------------ channels

// TestConversationsView_DecodesChannels pins the part of the
// referenced-channel array that comes from the embedded boot.Channel —
// the fields shared with userBoot's channels[]. The five view-only
// fields are pinned separately by
// TestConversationsView_ChannelsCarryUnreadState.
//
// Every boolean has a unique non-zero column vector across these four
// rows, which is the property that makes these assertions worth
// anything:
//
//	is_channel 1001   is_group 0100   is_mpim 0010
//	is_private 0111   is_archived 0110   is_general 1000
//	is_shared 1101   is_org_shared 1100   is_ext_shared 0001
//	is_member  1010
//
// is_member is in that list rather than the decoy list below because
// it is now modelled: it appears on 54 of 54 observed entries, and
// giving it column 1010 — unused by any other boolean here — is what
// stops it being swappable with one of them.
//
// The unmodelled channel booleans are decoys with a column no modelled
// field has: is_im and is_pending_ext_shared are all-false. A modelled
// field mis-tagged onto either dies.
func TestConversationsView_DecodesChannels(t *testing.T) {
	res, _ := mustView(t, fullViewBody, "C0OPENED99")

	if len(res.Channels) != 4 {
		t.Fatalf("len(Channels) = %d; want 4", len(res.Channels))
	}

	want := []Channel{
		{
			ID: "C0VIEW0001", Name: "general", NameNormalized: "general-norm",
			Version: 1783337533019, Created: 1670000001,
			IsChannel: true, IsGroup: false, IsMPIM: false,
			IsPrivate: false, IsArchived: false, IsGeneral: true,
			IsShared: true, IsOrgShared: true, IsExtShared: false,
			ContextTeamID: "T04T4TH8W", Creator: "U0VIEWAA1",
			SharedTeamIDs: []string{"T04T4TH8W", "T7777777Z"},
			Topic:         TextBlock{Value: "company announcements", Creator: "U0VIEWAA1", LastSet: 1670000101},
			Purpose:       TextBlock{Value: "all hands", Creator: "U0VIEWBB2", LastSet: 1670000102},
		},
		{
			ID: "C0VIEW0002", Name: "secret-plans", NameNormalized: "secret-plans-norm",
			Version: 1783337533020, Created: 1670000002,
			IsChannel: false, IsGroup: true, IsMPIM: false,
			IsPrivate: true, IsArchived: true, IsGeneral: false,
			IsShared: true, IsOrgShared: true, IsExtShared: false,
			ContextTeamID: "T7777777Z", Creator: "U0VIEWCC3",
			SharedTeamIDs: []string{"T04T4TH8W", "T7777777Z"},
			Topic:         TextBlock{Value: "hush", Creator: "U0VIEWCC3", LastSet: 1670000103},
			Purpose:       TextBlock{Value: "plotting", Creator: "U0VIEWCC3", LastSet: 1670000104},
		},
		{
			ID: "C0VIEW0003", Name: "mpdm-a--b--c-1", NameNormalized: "mpdm-a--b--c-1",
			Version: 1783337533021, Created: 1670000003,
			IsChannel: false, IsGroup: false, IsMPIM: true,
			IsPrivate: true, IsArchived: true, IsGeneral: false,
			IsShared: false, IsOrgShared: false, IsExtShared: false,
			ContextTeamID: "T04T4TH8W", Creator: "U0VIEWDD4",
			SharedTeamIDs: []string{"T04T4TH8W"},
			Topic:         TextBlock{},
			Purpose:       TextBlock{Value: "Group messaging", Creator: "U0VIEWDD4", LastSet: 1670000105},
		},
		{
			ID: "C0VIEW0004", Name: "partner-sync", NameNormalized: "partner-sync-norm",
			Version: 1783337533022, Created: 1670000004,
			IsChannel: true, IsGroup: false, IsMPIM: false,
			IsPrivate: true, IsArchived: false, IsGeneral: false,
			IsShared: true, IsOrgShared: false, IsExtShared: true,
			ContextTeamID: "T04T4TH8W", Creator: "U0VIEWEE5",
			SharedTeamIDs: []string{"T04T4TH8W", "E11EXTERN"},
			Topic:         TextBlock{Value: "partner integration", Creator: "U0VIEWEE5", LastSet: 1670000106},
			Purpose:       TextBlock{Value: "cross-org work", Creator: "U0VIEWEE5", LastSet: 1670000107},
		},
	}
	for i, w := range want {
		if got := res.Channels[i].Channel; !reflect.DeepEqual(got, w) {
			t.Errorf("Channels[%d].Channel =\n  %#v\nwant\n  %#v", i, got, w)
		}
	}
}

// TestConversationsView_ChannelsCarryUnreadState pins the five keys a
// conversations.view channels[] entry carries and a userBoot one does
// not.
//
// This is the correction of a measured claim, not a new feature. An
// earlier version of this package decoded channels[] into boot.Channel
// and dropped all five, on the stated grounds that the four
// non-boolean ones "would decode to zero on every row forever". That
// was read off a single committed sample. Measured across all 54
// entries in the two captures:
//
//	is_member             54/54
//	last_read             14/54
//	latest                14/54
//	unread_count          14/54
//	unread_count_display  14/54
//
// So the four do appear, on 26% of entries, and dropping them meant
// Phase 2b would issue a separate unread-counts call for numbers it
// had already been handed in the same response.
//
// The two count fields carry DISTINCT values on both rows that have
// them (41/17 and 6/3). Same-typed adjacent fields sharing a value are
// freely swappable and no assertion can tell them apart; this project
// has already lost mutants to exactly that.
func TestConversationsView_ChannelsCarryUnreadState(t *testing.T) {
	res, _ := mustView(t, fullViewBody, "C0OPENED99")

	if len(res.Channels) != 4 {
		t.Fatalf("len(Channels) = %d; want 4", len(res.Channels))
	}

	// is_member is on every observed entry, so every row asserts it.
	// The column is 1010 — see TestConversationsView_DecodesChannels.
	for i, want := range []bool{true, false, true, false} {
		if got := res.Channels[i].IsMember; got != want {
			t.Errorf("Channels[%d].IsMember = %v; want %v (column 1010 across the four rows)", i, got, want)
		}
	}

	// The unread quartet, on the two rows that carry it.
	unread := map[int]struct {
		lastRead string
		count    int
		display  int
	}{
		0: {lastRead: "1783337511.000111", count: 41, display: 17},
		3: {lastRead: "1783337544.000444", count: 6, display: 3},
	}
	for i, w := range unread {
		c := res.Channels[i]
		if c.LastRead != w.lastRead {
			t.Errorf("Channels[%d].LastRead = %q; want %q (a ts STRING, not an int)", i, c.LastRead, w.lastRead)
		}
		if c.UnreadCount != w.count {
			t.Errorf("Channels[%d].UnreadCount = %d; want %d", i, c.UnreadCount, w.count)
		}
		if c.UnreadCountDisplay != w.display {
			t.Errorf("Channels[%d].UnreadCountDisplay = %d; want %d", i, c.UnreadCountDisplay, w.display)
		}
		if c.UnreadCount == c.UnreadCountDisplay {
			t.Errorf("Channels[%d] has unread_count == unread_count_display (%d); the fixture must keep them distinct or a swapped tag survives", i, c.UnreadCount)
		}
	}

	// A row that never mentioned the keys must stay zero — and must be
	// distinguishable from one that carried a real zero. Latest is the
	// field that can say so: nil means the key was absent.
	for _, i := range []int{1, 2} {
		c := res.Channels[i]
		if c.LastRead != "" || c.UnreadCount != 0 || c.UnreadCountDisplay != 0 || c.Latest != nil {
			t.Errorf("Channels[%d] carries unread state it was never sent: LastRead=%q UnreadCount=%d UnreadCountDisplay=%d Latest=%s",
				i, c.LastRead, c.UnreadCount, c.UnreadCountDisplay, c.Latest)
		}
	}
}

// TestConversationsView_ChannelsLatestIsAMessageNotATS pins the one
// key whose TYPE differs between the two places conversations.view
// uses it.
//
// On the top-level `channel` object, `latest` is a ts string (2/2
// captures). On a channels[] entry it is a whole message OBJECT (14/14
// entries that carry it). Those are not the same contract, and the
// difference is load-bearing: `Latest string` on the entry type fails
// the entire response decode, losing the channel open. That is why
// ViewChannelEntry is a sibling of ViewChannel rather than the same
// type used twice.
//
// json.RawMessage, matching History.Messages, for the reason stated
// there: nothing consumes these yet, a second message struct would be
// a shape to keep in agreement forever, and raw bytes claim nothing.
func TestConversationsView_ChannelsLatestIsAMessageNotATS(t *testing.T) {
	res, _ := mustView(t, fullViewBody, "C0OPENED99")

	// The entry's latest is an object and its bytes survive whole.
	var m struct {
		Type string `json:"type"`
		TS   string `json:"ts"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(res.Channels[0].Latest, &m); err != nil {
		t.Fatalf("Channels[0].Latest is not a JSON object: %v (%s)", err, res.Channels[0].Latest)
	}
	if m.TS != "1783337512.000112" || m.Text != "latest in general" || m.Type != "message" {
		t.Errorf("Channels[0].Latest = %s; want the message with ts 1783337512.000112 intact", res.Channels[0].Latest)
	}
	// Not the channel's own ts, and not the other row's message.
	if strings.Contains(string(res.Channels[0].Latest), "partner-sync") {
		t.Errorf("Channels[0].Latest = %s; rows are crossed", res.Channels[0].Latest)
	}
	if got := string(res.Channels[3].Latest); !strings.Contains(got, "1783337545.000445") {
		t.Errorf("Channels[3].Latest = %s; want the partner-sync message", got)
	}

	// The top-level `channel` object's latest is a plain string, and
	// keeping it typed is what makes the divergence visible.
	if res.Channel.Latest != "17833375555555555" {
		t.Errorf("Channel.Latest = %q; want the ts string — the `channel` object and a channels[] entry disagree about this key's type",
			res.Channel.Latest)
	}

	// A ts string where the entry expects a message must be a LOUD
	// failure, not a silent empty. This is the shape a `Latest string`
	// field would have imposed on every one of the 14 observed
	// entries.
	const stringLatest = `{"ok":true,"channels":[{"id":"C0VIEW0001","latest":"1783337512.000112"}]}`
	strRes, _ := mustView(t, stringLatest, "C0OPENED99")
	if got := string(strRes.Channels[0].Latest); got != `"1783337512.000112"` {
		t.Errorf("Channels[0].Latest = %s; raw bytes must survive whichever shape the server sends", got)
	}
}

// TestConversationsView_UsersAndChannelsDoNotCross pins that the two
// arrays do not decode into each other. They sit next to each other at
// the top level and both are arrays of objects with `id`, `name`,
// `updated` and `team_id`/`context_team_id`, so transposing the two
// tags compiles, decodes partially, and leaves a client with no
// authors and no channels.
func TestConversationsView_UsersAndChannelsDoNotCross(t *testing.T) {
	res, _ := mustView(t, fullViewBody, "C0OPENED99")

	for _, u := range res.Users {
		if !strings.HasPrefix(u.ID, "U") {
			t.Errorf("Users contains %q, which is not a user id — users and channels are crossed", u.ID)
		}
	}
	for _, c := range res.Channels {
		if !strings.HasPrefix(c.ID, "C") {
			t.Errorf("Channels contains %q, which is not a channel id — users and channels are crossed", c.ID)
		}
	}
	if len(res.Users) == len(res.Channels) {
		t.Error("the fixture gives users and channels the same length; a swapped tag would be harder to see")
	}
}

// --------------------------------------------------------------- emoji

// TestConversationsView_EmojisIsAMapNotAnArray pins the shape that is
// easiest to get wrong from the key name alone.
//
// `emojis` is plural, and every other plural key at this level
// (users, bots, channels) is an array — this one is an OBJECT keyed by
// emoji name. Modelling it as a slice fails the whole response decode,
// so this also pins that such a failure would be loud rather than
// silent.
func TestConversationsView_EmojisIsAMapNotAnArray(t *testing.T) {
	res, _ := mustView(t, fullViewBody, "C0OPENED99")

	want := map[string]string{
		"rls-heart":    "https://emoji.slack-edge.com/T04T4TH8W/rls-heart/1111aaaa.png",
		"oregon-trail": "https://emoji.slack-edge.com/T04T4TH8W/oregon-trail/2222bbbb.gif",
		"woohoo":       "https://emoji.slack-edge.com/T04T4TH8W/woohoo/3333cccc.png",
	}
	if !reflect.DeepEqual(res.Emojis, want) {
		t.Errorf("Emojis = %#v; want %#v", res.Emojis, want)
	}
	// The names are the keys, not a positional list: a []string model
	// would lose the association entirely, and a map with the URLs as
	// keys would invert it.
	if got, ok := res.Emojis["woohoo"]; !ok || !strings.Contains(got, "/woohoo/") {
		t.Errorf("Emojis[woohoo] = %q, present=%v; the map must be keyed by NAME with the URL as the value", got, ok)
	}
}

// ---------------------------------------------------------- opened channel

// TestConversationsView_ExposesTheOpenedChannel pins the field the
// plan's spec omitted and the whole fallback strategy depends on.
//
// The `channel` request param is unverified. If Slack ignores it, the
// response is a well-formed ok:true body describing the LAST-VIEWED
// conversation, and Channel.ID is the only thing in the response that
// says so. See ConversationsView's doc comment.
func TestConversationsView_ExposesTheOpenedChannel(t *testing.T) {
	res, _ := mustView(t, fullViewBody, "C0OPENED99")

	if res.Channel.ID != "C0OPENED99" {
		t.Errorf("Channel.ID = %q; want %q — this is how a caller verifies the unverified `channel` param was honoured",
			res.Channel.ID, "C0OPENED99")
	}
	if res.Channel.Name != "the-opened-one" {
		t.Errorf("Channel.Name = %q; want %q", res.Channel.Name, "the-opened-one")
	}
	// The opened channel is NOT one of the referenced channels, so a
	// `channel` tag that accidentally read channels[0] would be caught.
	for _, c := range res.Channels {
		if c.ID == res.Channel.ID {
			t.Errorf("Channel.ID %q also appears in Channels; the fixture must keep them disjoint or a mis-tag survives", c.ID)
		}
	}

	// The embedded Channel's fields decode through the promotion.
	if res.Channel.Version != 1783337599009 {
		t.Errorf("Channel.Version = %d; want 1783337599009 (the `updated` ms stamp, not `created`)", res.Channel.Version)
	}
	if res.Channel.Created != 1660000009 {
		t.Errorf("Channel.Created = %d; want 1660000009", res.Channel.Created)
	}
	if want := (TextBlock{Value: "opened topic", Creator: "U0VIEWAA1", LastSet: 1670000201}); res.Channel.Topic != want {
		t.Errorf("Channel.Topic = %#v; want %#v", res.Channel.Topic, want)
	}

	// The four fields that exist ONLY on this object. last_read and
	// latest are 17-character STRINGS; unread_count and
	// unread_count_display are adjacent ints with distinct values so a
	// transposed tag dies.
	if got, want := res.Channel.LastRead, "17833374444444444"; got != want {
		t.Errorf("Channel.LastRead = %q; want %q", got, want)
	}
	if got, want := res.Channel.Latest, "17833375555555555"; got != want {
		t.Errorf("Channel.Latest = %q; want %q", got, want)
	}
	if res.Channel.UnreadCount != 12 {
		t.Errorf("Channel.UnreadCount = %d; want 12", res.Channel.UnreadCount)
	}
	if res.Channel.UnreadCountDisplay != 5 {
		t.Errorf("Channel.UnreadCountDisplay = %d; want 5", res.Channel.UnreadCountDisplay)
	}
}

// TestConversationsView_ChannelIDDetectsAnIgnoredChannelParam is the
// fallback seam, pinned as a caller would use it.
//
// This is the SILENT failure mode of the unverified `channel` param:
// Slack ignores it, answers ok:true with a complete, well-formed body,
// and the parser has nothing to complain about. Nothing here can raise
// an error for that. The caller compares Channel.ID against what it
// asked for, and falls back to conversations.history (Task 9) when
// they differ — so Channel.ID must survive decoding even when it is
// the "wrong" answer.
func TestConversationsView_ChannelIDDetectsAnIgnoredChannelParam(t *testing.T) {
	const asked = "C0WANTED11"

	res, rec := mustView(t, fullViewBody, asked)

	// What slk asked for went out on the wire...
	if got := rec.form.Get("channel"); got != asked {
		t.Fatalf("form[channel] = %q; want %q", got, asked)
	}
	// ...and the server answered about a different conversation. A
	// caller MUST be able to see that, and this is the only signal.
	if res.Channel.ID == asked {
		t.Fatal("the fixture returns the channel that was asked for; this test cannot demonstrate the detection it exists for")
	}
	if res.Channel.ID != "C0OPENED99" {
		t.Errorf("Channel.ID = %q; want %q — without this field a caller cannot tell it got the last-viewed conversation instead of the one it requested",
			res.Channel.ID, "C0OPENED99")
	}
}

// ---------------------------------------------------- response metadata

func TestConversationsView_DecodesResponseMetadata(t *testing.T) {
	res, _ := mustView(t, fullViewBody, "C0OPENED99")

	const want = "bmV4dC1jdXJzb3ItMzItY2hhcnMtbG9uZzEy"
	if got := res.ResponseMetadata.NextCursor; got != want {
		t.Errorf("ResponseMetadata.NextCursor = %q; want %q", got, want)
	}
}

// ----------------------------------------------------------- int width

// TestConversationsView_UnverifiedWireIntegersAreInt64 pins the WIDTH
// of the wire integers this file declares. It is a compile-time
// assertion, not a runtime one.
//
// It exists because narrowing any of these to `int` is currently
// invisible: slk ships only amd64 and arm64 (.goreleaser.yaml), where
// `int` is already 64 bits, so no value-based assertion in this file
// can tell the two apart. The hazard is latent, not active — but it
// activates silently the day a 32-bit target is added, and the failure
// mode is a millisecond stamp wrapping to garbage rather than
// anything loud.
//
// The obvious alternative — asserting a fixture magnitude above 2^31,
// as boot_test does for Channel.Version — is NOT available here.
// boot_test can do it because the userBoot capture shows `updated` as
// a 13-digit millisecond value, so >2^31 is measured. The
// conversations.view capture records next_ts and users[].updated only
// as "int", with no magnitude, so asserting one would be inventing a
// contract. int64 is simply the correct default for a wire integer of
// unverified size, and these three lines stop it being narrowed by
// accident without claiming anything the captures do not show.
func TestConversationsView_UnverifiedWireIntegersAreInt64(t *testing.T) {
	res, _ := mustView(t, fullViewBody, "C0OPENED99")

	// Go has no implicit numeric conversion, so each of these fails to
	// compile if the field is retyped to `int`.
	var _ int64 = res.History.NextTS
	var _ int64 = res.Users[0].Version
	var _ int64 = res.Users[0].Profile.StatusExpiration
}

// ---------------------------------------------------------------- bots

// TestConversationsView_EmptyBotsDecodesToEmptyNotMissing covers the
// one array the captures showed empty in 2/2.
//
// Asserting "len == 0" alone would be vacuous — it passes just as well
// against a field that was never decoded, or one whose tag is
// misspelled. Non-nil is what separates "the server said []" from "we
// never looked".
func TestConversationsView_EmptyBotsDecodesToEmptyNotMissing(t *testing.T) {
	res, _ := mustView(t, fullViewBody, "C0OPENED99")

	if res.Bots == nil {
		t.Error("Bots is nil; the response carried `\"bots\": []` so it must decode to an empty, non-nil slice")
	}
	if len(res.Bots) != 0 {
		t.Errorf("len(Bots) = %d; want 0", len(res.Bots))
	}
}

// TestConversationsView_PopulatedBotsSurviveAsRawBytes pins that the
// unverified element shape is preserved rather than interpreted.
//
// bots was `[]` in BOTH captures, so there is no evidence for what an
// entry looks like. []json.RawMessage claims nothing and loses
// nothing; a struct here would be an invented contract. This asserts
// only that whatever bytes arrive survive intact for a later capture
// to interpret.
func TestConversationsView_PopulatedBotsSurviveAsRawBytes(t *testing.T) {
	const body = `{"ok":true,"bots":[{"id":"B0AB1CD2E","name":"jenkins"},{"id":"B0FF9EE8D"}]}`
	res, _ := mustView(t, body, "C0OPENED99")

	if len(res.Bots) != 2 {
		t.Fatalf("len(Bots) = %d; want 2", len(res.Bots))
	}
	for i, want := range []string{`{"id":"B0AB1CD2E","name":"jenkins"}`, `{"id":"B0FF9EE8D"}`} {
		if got := string(res.Bots[i]); got != want {
			t.Errorf("Bots[%d] = %s; want %s", i, got, want)
		}
	}
}

// -------------------------------------------------------------- errors

// TestConversationsView_OKFalseIsAnError pins Slack's
// application-level failure mode. The Web API answers HTTP 200 for
// these, so `ok` is the only signal; without the check a rejected
// `channel` param would sail through as a channel with zero messages
// and zero users, and slk would render an empty channel rather than
// fall back.
func TestConversationsView_OKFalseIsAnError(t *testing.T) {
	var rec recordedCall
	res, err := ConversationsView(context.Background(), stubPost(&rec, `{"ok":false,"error":"channel_not_found"}`, nil), "C0OPENED99")
	if err == nil {
		t.Fatal("ConversationsView returned nil error for ok:false")
	}
	if !strings.Contains(err.Error(), "channel_not_found") {
		t.Errorf("error = %q; want it to carry Slack's error string %q", err, "channel_not_found")
	}
	if res != nil {
		t.Errorf("ConversationsView returned a non-nil ViewResult alongside an error: %#v", res)
	}
}

// TestConversationsView_OKFalseWithAFullBodyReturnsNoData is the
// partial-data seam, pinned directly.
//
// encoding/json fills the struct before anything inspects `ok`, so at
// the moment the check runs there is a fully populated ViewResult
// sitting in a local. Returning it — or returning a pointer to it "for
// diagnostics" — would hand the caller a plausible-looking channel
// built from a response the server explicitly rejected. Every earlier
// task in this plan grew a surviving mutant at exactly this seam.
func TestConversationsView_OKFalseWithAFullBodyReturnsNoData(t *testing.T) {
	body := strings.Replace(fullViewBody, `"ok": true`, `"ok": false, "error": "not_in_channel"`, 1)
	if !strings.Contains(body, "not_in_channel") {
		t.Fatal("fixture rewrite failed; this test would be vacuous")
	}
	// Sanity: the rewritten body still carries the data that must NOT
	// come back, so a passing test is not just passing on an empty body.
	if !strings.Contains(body, "C0OPENED99") || !strings.Contains(body, "cmid-0001") {
		t.Fatal("rewritten fixture lost its channel or its messages; this test would be vacuous")
	}

	var rec recordedCall
	res, err := ConversationsView(context.Background(), stubPost(&rec, body, nil), "C0OPENED99")
	if err == nil {
		t.Fatal("ConversationsView returned nil error for ok:false")
	}
	if !strings.Contains(err.Error(), "not_in_channel") {
		t.Errorf("error = %q; want it to carry %q", err, "not_in_channel")
	}
	if res != nil {
		t.Fatalf("ConversationsView returned %d messages and channel %q alongside an error; a rejected response must yield no data",
			len(res.History.Messages), res.Channel.ID)
	}
}

// TestConversationsView_OKFalseWithNoErrorFieldStillSaysSomething
// guards against an error message that ends in a dangling colon and
// carries no diagnostic at all.
func TestConversationsView_OKFalseWithNoErrorFieldStillSaysSomething(t *testing.T) {
	var rec recordedCall
	_, err := ConversationsView(context.Background(), stubPost(&rec, `{"ok":false}`, nil), "C0OPENED99")
	if err == nil {
		t.Fatal("ConversationsView returned nil error for ok:false")
	}
	if !strings.Contains(err.Error(), "ok=false") {
		t.Errorf("error = %q; want it to explain that ok was false", err)
	}
}

// TestConversationsView_MalformedJSONIsAnErrorWithNoData covers the
// other half of the partial-data seam. encoding/json keeps decoding
// past the first type error, so a body with one bad field populates
// everything else AND returns an error. Nothing may escape.
func TestConversationsView_MalformedJSONIsAnErrorWithNoData(t *testing.T) {
	// Valid JSON, wrong type for one modelled field, and `ok` is true
	// so the ok check cannot be what rejects it. The bad field is
	// LAST, so everything before it has already been decoded when the
	// error fires.
	const body = `{"ok":true,"channel":{"id":"C0OPENED99"},"emojis":{"woohoo":"u"},"users":"not-an-array"}`

	var rec recordedCall
	res, err := ConversationsView(context.Background(), stubPost(&rec, body, nil), "C0OPENED99")
	if err == nil {
		t.Fatal("ConversationsView returned nil error for a type-mismatched body")
	}
	if res != nil {
		t.Fatalf("ConversationsView returned a ViewResult alongside a decode error: Channel.ID=%q, Emojis=%v",
			res.Channel.ID, res.Emojis)
	}
}

// TestConversationsView_MutationTimestampTypeErrorReturnsNoData is the
// same seam at the field most likely to trip it in production: the
// three 17-character strings that look like integers. If a future edit
// retypes one, the decode fails mid-response with the history's
// messages already populated.
func TestConversationsView_MutationTimestampTypeErrorReturnsNoData(t *testing.T) {
	const body = `{"ok":true,"channel":{"id":"C0OPENED99"},"history":{"messages":[{"ts":"1.1"}],"mutation_timestamps":{"latest":[]}}}`

	var rec recordedCall
	res, err := ConversationsView(context.Background(), stubPost(&rec, body, nil), "C0OPENED99")
	if err == nil {
		t.Fatal("ConversationsView returned nil error for a type-mismatched mutation timestamp")
	}
	if res != nil {
		t.Fatalf("ConversationsView returned a ViewResult alongside a decode error: %d messages, Channel.ID=%q",
			len(res.History.Messages), res.Channel.ID)
	}
}

func TestConversationsView_NotJSONIsAnError(t *testing.T) {
	var rec recordedCall
	res, err := ConversationsView(context.Background(), stubPost(&rec, `<html>proxy says no</html>`, nil), "C0OPENED99")
	if err == nil {
		t.Fatal("ConversationsView returned nil error for a non-JSON body")
	}
	if res != nil {
		t.Error("ConversationsView returned a non-nil ViewResult for a non-JSON body")
	}
}

// TestConversationsView_PropagatesPostError pins that a transport
// failure is reported rather than turned into an empty channel.
func TestConversationsView_PropagatesPostError(t *testing.T) {
	sentinel := errors.New("HTTP 503: edge unavailable")
	var rec recordedCall
	res, err := ConversationsView(context.Background(), stubPost(&rec, "", sentinel), "C0OPENED99")
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v; want it to wrap %v", err, sentinel)
	}
	if res != nil {
		t.Error("ConversationsView returned a non-nil ViewResult alongside a transport error")
	}
}

// ------------------------------------------------------------ tolerance

// TestConversationsView_IgnoresUnknownFields pins forward
// compatibility. Slack adds top-level and nested keys to this response
// without notice; a decoder that rejected them would break slk on
// Slack's schedule.
func TestConversationsView_IgnoresUnknownFields(t *testing.T) {
	const body = `{
	  "ok": true,
	  "a_brand_new_top_level_key": {"nested": [1, 2, 3]},
	  "another_one": "surprise",
	  "history": {
	    "messages": [{"ts": "1783337500.000100"}],
	    "has_more": true,
	    "a_brand_new_history_key": {"deeply": {"nested": true}},
	    "mutation_timestamps": {"latest": "17833371111111111", "a_brand_new_mt_key": 9}
	  },
	  "users": [
	    {"id": "U0VIEWAA1", "updated": 1783337561001,
	     "a_brand_new_user_key": [1],
	     "profile": {"display_name": "aard-display", "a_brand_new_profile_key": []}}
	  ],
	  "channels": [
	    {"id": "C0VIEW0001", "updated": 1783337533019, "a_brand_new_channel_key": null}
	  ],
	  "emojis": {"woohoo": "https://emoji.slack-edge.com/T04T4TH8W/woohoo/3333cccc.png"},
	  "channel": {"id": "C0OPENED99", "unread_count": 12, "a_brand_new_channel_key": {"x": 1}},
	  "response_metadata": {"next_cursor": "abc", "a_brand_new_meta_key": true}
	}`
	res, _ := mustView(t, body, "C0OPENED99")

	if len(res.History.Messages) != 1 || !res.History.HasMore {
		t.Errorf("History = %#v; want the one message and has_more true", res.History)
	}
	if res.History.MutationTimestamps.Latest != "17833371111111111" {
		t.Errorf("History.MutationTimestamps.Latest = %q; want %q", res.History.MutationTimestamps.Latest, "17833371111111111")
	}
	if len(res.Users) != 1 || res.Users[0].ID != "U0VIEWAA1" || res.Users[0].Profile.DisplayName != "aard-display" {
		t.Errorf("Users = %#v; want the one aardvark", res.Users)
	}
	if len(res.Channels) != 1 || res.Channels[0].ID != "C0VIEW0001" {
		t.Errorf("Channels = %#v; want the one channel", res.Channels)
	}
	if res.Channel.ID != "C0OPENED99" || res.Channel.UnreadCount != 12 {
		t.Errorf("Channel = %#v; want C0OPENED99 with 12 unread", res.Channel)
	}
	if res.ResponseMetadata.NextCursor != "abc" {
		t.Errorf("ResponseMetadata.NextCursor = %q; want %q", res.ResponseMetadata.NextCursor, "abc")
	}
	if len(res.Emojis) != 1 {
		t.Errorf("Emojis = %#v; want the one entry", res.Emojis)
	}
}
