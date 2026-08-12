package edge

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// teamOf builds a client against the recorder with a team id other
// than the recorder's default.
//
// This exists for one assertion: default_workspace must be c.teamID,
// not a literal. Every other test in this package uses T04T4TH8W —
// the team id from the captures — so a payload that hardcoded
// "T04T4TH8W" would satisfy all of them and ship a request that says
// somebody else's workspace on every install but mine.
func teamOf(rec *recorder, teamID string) *Client {
	c := New("xoxc-test", teamID, rec.srv.Client())
	c.baseURL = rec.srv.URL
	return c
}

// wantNumber asserts a payload value arrived as the JSON number want.
//
// A float comparison rather than ==: encoding/json decodes every
// number into float64, so `body["fuzz"] != 1` compares against an int
// and fails for the right value. It also has to reject `true`, which
// is the plausible mutation for a field the captures show as the
// number 1 — the type assertion is what buys that.
func wantNumber(t *testing.T, body map[string]any, key string, want float64) {
	t.Helper()
	got, ok := body[key].(float64)
	if !ok {
		t.Errorf("%s = %#v (%T); want the JSON number %v", key, body[key], body[key], want)
		return
	}
	if got != want {
		t.Errorf("%s = %v; want %v", key, got, want)
	}
}

func wantString(t *testing.T, body map[string]any, key, want string) {
	t.Helper()
	if got := body[key]; got != want {
		t.Errorf("%s = %#v; want %q", key, got, want)
	}
}

func wantTrue(t *testing.T, body map[string]any, key string) {
	t.Helper()
	if got := body[key]; got != true {
		t.Errorf("%s = %#v; want true", key, got)
	}
}

// wantStrings asserts a payload value is exactly the given JSON array
// of strings, in order.
func wantStrings(t *testing.T, body map[string]any, key string, want ...string) {
	t.Helper()
	raw, ok := body[key].([]any)
	if !ok {
		t.Fatalf("%s = %#v (%T); want an array of strings", key, body[key], body[key])
	}
	var got []string
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("%s contains %#v (%T); want strings only", key, v, v)
		}
		got = append(got, s)
	}
	if !slices.Equal(got, want) {
		t.Errorf("%s = %v; want %v in that order", key, got, want)
	}
}

// ------------------------------------------------------------- channels

// TestChannelsSearch_SendsObservedPayload pins the request against the
// two captured channels/search samples, which agree on every key.
func TestChannelsSearch_SendsObservedPayload(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"results":[],"member_channels":[]}`
	})
	c := rec.client()

	// Deliberately not in sorted order. The capture's top_channels
	// begins C04T4TH9N, C04T4TH9Q, C94H848UB — ascending by accident —
	// and a fixture in that order cannot tell "sent verbatim" from
	// "sorted on the way out", because both produce the same bytes.
	// Order is the entire content of a frecency hint: a sorted
	// most-recently-used list is not a ranking, it is noise. So these
	// are the capture's three ids rotated, which is neither sorted nor
	// reverse-sorted and so fails under either mutation.
	if _, _, err := c.ChannelsSearch(context.Background(), "test",
		[]string{"C94H848UB", "C04T4TH9N", "C04T4TH9Q"}); err != nil {
		t.Fatalf("ChannelsSearch: %v", err)
	}

	reqs := rec.requests()
	if len(reqs) != 1 {
		t.Fatalf("made %d requests; want exactly 1 — search is a single call, never batched", len(reqs))
	}
	if reqs[0].path != "/cache/T04T4TH8W/channels/search" {
		t.Errorf("path = %q; want /cache/T04T4TH8W/channels/search", reqs[0].path)
	}

	// The whole point of the package: exactly these keys, no more.
	// An extra key fingerprints us as loudly as a missing one.
	assertExactKeys(t, reqs,
		"token", "query", "count", "fuzz", "include_record_channels",
		"check_membership", "default_workspace", "top_channels")

	body := reqs[0].generic(t)
	wantString(t, body, "token", "xoxc-test")
	wantString(t, body, "query", "test")
	// The literal 30, deliberately not `float64(searchCount)`: an
	// assertion against the constant is satisfied by any value the
	// constant happens to hold, so it could not notice the constant
	// drifting off the captured one.
	wantNumber(t, body, "count", 30)
	wantNumber(t, body, "fuzz", 1)
	wantTrue(t, body, "include_record_channels")
	wantTrue(t, body, "check_membership")
	wantString(t, body, "default_workspace", "T04T4TH8W")
	wantStrings(t, body, "top_channels", "C94H848UB", "C04T4TH9N", "C04T4TH9Q")
}

// TestChannelsSearch_DecodesResultsAndMemberChannels covers the
// response half. The fixture gives is_channel, is_private and
// is_archived the value true, because a fixture where every boolean
// is false cannot tell "decoded false" from "never decoded" — that
// exact gap produced 9 surviving mutants earlier in this phase.
func TestChannelsSearch_DecodesResultsAndMemberChannels(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"results":[
			{"id":"C2QPK1V44","name":"test-channel","updated":1783337533019,
			 "is_channel":true,"is_group":false,"is_im":false,"is_mpim":false,
			 "is_private":true,"is_archived":true,
			 "context_team_id":"T04T4TH8W",
			 "topic":{"creator":"U1","last_set":123,"value":"stand-ups here"}},
			{"id":"CL0AET1L0","name":"testing","updated":1783337533020,
			 "is_channel":true,"is_private":false,"is_archived":false,
			 "context_team_id":"T04T4TH8W","topic":{"value":""}}
		],"member_channels":["CL0AET1L0","C2QPK1V44"]}`
	})
	c := rec.client()

	got, members, err := c.ChannelsSearch(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("ChannelsSearch: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d channels; want 2", len(got))
	}
	ch := got[0]
	if ch.ID != "C2QPK1V44" {
		t.Errorf("ID = %q; want C2QPK1V44", ch.ID)
	}
	if ch.Name != "test-channel" {
		t.Errorf("Name = %q; want test-channel", ch.Name)
	}
	if ch.Version != 1783337533019 {
		t.Errorf("Version = %d; want 1783337533019 (from the `updated` field)", ch.Version)
	}
	// One field per assertion, never an `||` chain: a chain cannot
	// name the flag that broke.
	if !ch.IsChannel {
		t.Error("IsChannel = false; want true")
	}
	if !ch.IsPrivate {
		t.Error("IsPrivate = false; want true (is_private is true in the fixture)")
	}
	if !ch.IsArchived {
		t.Error("IsArchived = false; want true (is_archived is true in the fixture)")
	}
	if ch.IsGroup {
		t.Error("IsGroup = true; want false")
	}
	if ch.IsIM {
		t.Error("IsIM = true; want false")
	}
	if ch.IsMPIM {
		t.Error("IsMPIM = true; want false")
	}
	if ch.ContextTeam != "T04T4TH8W" {
		t.Errorf("ContextTeam = %q; want T04T4TH8W", ch.ContextTeam)
	}
	if ch.Topic.Value != "stand-ups here" {
		t.Errorf("Topic.Value = %q; want %q", ch.Topic.Value, "stand-ups here")
	}
	// Results are ranked by the server; order is the answer, not an
	// implementation detail.
	if got[1].ID != "CL0AET1L0" {
		t.Errorf("second result ID = %q; want CL0AET1L0 — results must keep server order", got[1].ID)
	}
	if got[1].IsPrivate {
		t.Error("second result IsPrivate = true; want false")
	}
	if got[1].IsArchived {
		t.Error("second result IsArchived = true; want false")
	}

	// member_channels is the array that lets the finder mark "you are
	// in this one" without a conversations.list walk. Dropping it
	// puts the enumeration back.
	if want := []string{"CL0AET1L0", "C2QPK1V44"}; !slices.Equal(members, want) {
		t.Errorf("member_channels = %v; want %v — this array is what check_membership:true "+
			"buys, and losing it forces enumeration", members, want)
	}
}

// TestChannelsSearch_AbsentMemberChannelsDecodesEmpty pins the other
// observed shape: of the two captured channels/search responses only
// one carries member_channels. Absence means empty, never an error.
func TestChannelsSearch_AbsentMemberChannelsDecodesEmpty(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"results":[{"id":"C1","name":"x","updated":1}]}`
	})

	got, members, err := rec.client().ChannelsSearch(context.Background(), "x", nil)
	if err != nil {
		t.Fatalf("ChannelsSearch on a response without member_channels: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d channels; want 1", len(got))
	}
	if len(members) != 0 {
		t.Errorf("member_channels = %v; want empty when the key is absent", members)
	}
}

// TestChannelsSearch_TopChannelsPresenceTracksTheList walks the list
// sizes that decide whether the key ships at all.
//
// The single-id case is not padding. Without it, `len(topChannels) >
// 1` passes every other test here — and a one-entry frecency list is
// exactly what a fresh install has on its first search, so that
// mutant would drop the ranking hint precisely when it is the only
// hint there is.
func TestChannelsSearch_TopChannelsPresenceTracksTheList(t *testing.T) {
	base := []string{
		"token", "query", "count", "fuzz", "include_record_channels",
		"check_membership", "default_workspace",
	}
	for _, tc := range []struct {
		name  string
		top   []string
		extra []string
	}{
		{"nil", nil, nil},
		{"empty", []string{}, nil},
		{"single", []string{"C04T4TH9N"}, []string{"top_channels"}},
		{"many", []string{"C04T4TH9N", "C04T4TH9Q"}, []string{"top_channels"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := newRecorder(t, func(int) (int, string) {
				return 200, `{"ok":true,"results":[]}`
			})
			if _, _, err := rec.client().ChannelsSearch(context.Background(), "test", tc.top); err != nil {
				t.Fatalf("ChannelsSearch: %v", err)
			}
			// Asserted as an exact key set, not as `top_channels ==
			// nil`: a key present with a null or [] value is still a
			// key on the wire, and the captures never show one.
			assertExactKeys(t, rec.requests(), append(slices.Clone(base), tc.extra...)...)
		})
	}
}

func TestChannelsSearch_EmptyQueryMakesNoRequest(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"results":[{"id":"C1"}],"member_channels":["C1"]}`
	})

	got, members, err := rec.client().ChannelsSearch(context.Background(), "",
		[]string{"C04T4TH9N"})
	if err != nil {
		t.Fatalf("ChannelsSearch(\"\"): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d channels; want 0 for an empty query", len(got))
	}
	if len(members) != 0 {
		t.Errorf("member_channels = %v; want empty for an empty query", members)
	}
	if n := len(rec.requests()); n != 0 {
		t.Errorf("made %d requests for an empty query; want 0 — an empty-query search "+
			"can only return noise, and firing one on every cleared input is a shape "+
			"the official client never produces", n)
	}
}

func TestChannelsSearch_PropagatesAPIError(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":false,"error":"invalid_auth"}`
	})

	got, members, err := rec.client().ChannelsSearch(context.Background(), "test", nil)
	if err == nil {
		t.Fatal("ChannelsSearch returned nil error on ok:false")
	}
	if !strings.Contains(err.Error(), "invalid_auth") {
		t.Errorf("error = %v; want it to mention invalid_auth", err)
	}
	if got != nil || members != nil {
		t.Errorf("got = %+v, %v; want nil results alongside an error", got, members)
	}
}

func TestChannelsSearch_IgnoresUnknownResponseFields(t *testing.T) {
	// A real 43-field result plus keys Slack has not shipped yet.
	// Slack adds fields to this response without notice; a decode
	// that rejected them would break in production, not in CI.
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"some_future_top_level_key":{"a":1},"results":[{
			"id":"C2QPK1V44","enterprise_id":"","context_team_id":"T04T4TH8W",
			"internal_team_ids":[],"pending_connected_team_ids":[],"pending_shared":[],
			"shared_team_ids":["T04T4TH8W"],"connected_limited_team_ids":[],
			"connected_team_ids":[],"conversation_host_id":"","creator":"U04T4TH8Y",
			"name":"test","name_normalized":"test","previous_names":[],
			"created":1668181000,"unlinked":0,"updated":1783337533019,
			"is_archived":false,"is_channel":true,"is_frozen":false,"is_general":true,
			"is_group":false,"is_im":false,"is_moved":0,"is_mpim":false,
			"is_org_default":false,"is_org_mandatory":false,"is_record_channel":false,
			"is_file":false,"is_shared":false,"is_ext_shared":false,"is_org_shared":false,
			"is_pending_ext_shared":false,"is_private":false,"is_global_shared":false,
			"parent_conversation":"",
			"purpose":{"creator":"U1","last_set":1,"value":"p"},
			"topic":{"creator":"U1","last_set":1,"value":"t"},
			"properties":null,
			"frozen_reason":"","is_ext_ws_shared":false,"use_case":"",
			"channel_agent_status":"","a_field_slack_ships_next_week":42
		}],"member_channels":["C2QPK1V44"]}`
	})

	got, members, err := rec.client().ChannelsSearch(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("ChannelsSearch on a full real-shaped response: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d channels; want 1", len(got))
	}
	if got[0].ID != "C2QPK1V44" || got[0].Version != 1783337533019 || got[0].Topic.Value != "t" {
		t.Errorf("modelled fields did not survive the unmodelled ones: %+v", got[0])
	}
	if len(members) != 1 || members[0] != "C2QPK1V44" {
		t.Errorf("member_channels = %v; want [C2QPK1V44]", members)
	}
}

// ---------------------------------------------------------------- users

// TestUsersSearch_SendsObservedPayload pins the request against the
// two captured users/search samples.
//
// Note default_workspace and current_channel: the plan this task came
// from omitted both, and both are present in 2 of 2 observations.
func TestUsersSearch_SendsObservedPayload(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"results":[]}`
	})
	c := rec.client()

	if _, err := c.UsersSearch(context.Background(), "test", "C014Y7K5U8K",
		[]string{"UG2U3CFCN", "U0B6SR2FLG1", "UUC6ZQ2NQ"}); err != nil {
		t.Fatalf("UsersSearch: %v", err)
	}

	reqs := rec.requests()
	if len(reqs) != 1 {
		t.Fatalf("made %d requests; want exactly 1", len(reqs))
	}
	if reqs[0].path != "/cache/T04T4TH8W/users/search" {
		t.Errorf("path = %q; want /cache/T04T4TH8W/users/search", reqs[0].path)
	}

	assertExactKeys(t, reqs,
		"token", "query", "count", "fuzz", "enable_workspace_ranking",
		"search_email", "include_profile_only_users", "default_workspace",
		"top_users", "current_channel")

	body := reqs[0].generic(t)
	wantString(t, body, "token", "xoxc-test")
	wantString(t, body, "query", "test")
	wantNumber(t, body, "count", 30)
	wantNumber(t, body, "fuzz", 1)
	wantTrue(t, body, "enable_workspace_ranking")
	wantTrue(t, body, "search_email")
	wantTrue(t, body, "include_profile_only_users")
	wantString(t, body, "default_workspace", "T04T4TH8W")
	wantString(t, body, "current_channel", "C014Y7K5U8K")
	wantStrings(t, body, "top_users", "UG2U3CFCN", "U0B6SR2FLG1", "UUC6ZQ2NQ")
}

// TestSearch_DefaultWorkspaceIsTheClientsTeam catches the payload that
// hardcodes the capture's team id. Every other test here runs against
// T04T4TH8W, so a literal would pass all of them and then send a
// stranger's workspace id from every other install.
func TestSearch_DefaultWorkspaceIsTheClientsTeam(t *testing.T) {
	t.Run("channels", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) { return 200, `{"ok":true,"results":[]}` })
		if _, _, err := teamOf(rec, "T99OTHER").ChannelsSearch(context.Background(), "x", nil); err != nil {
			t.Fatalf("ChannelsSearch: %v", err)
		}
		reqs := rec.requests()
		if len(reqs) != 1 {
			t.Fatalf("made %d requests; want 1", len(reqs))
		}
		wantString(t, reqs[0].generic(t), "default_workspace", "T99OTHER")
		if reqs[0].path != "/cache/T99OTHER/channels/search" {
			t.Errorf("path = %q; want /cache/T99OTHER/channels/search", reqs[0].path)
		}
	})
	t.Run("users", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) { return 200, `{"ok":true,"results":[]}` })
		if _, err := teamOf(rec, "T99OTHER").UsersSearch(context.Background(), "x", "", nil); err != nil {
			t.Fatalf("UsersSearch: %v", err)
		}
		reqs := rec.requests()
		if len(reqs) != 1 {
			t.Fatalf("made %d requests; want 1", len(reqs))
		}
		wantString(t, reqs[0].generic(t), "default_workspace", "T99OTHER")
		if reqs[0].path != "/cache/T99OTHER/users/search" {
			t.Errorf("path = %q; want /cache/T99OTHER/users/search", reqs[0].path)
		}
	})
}

// TestUsersSearch_OmitsEmptyCallerState walks all four combinations of
// the two optional params.
//
// The combinations matter: with only "both set" and "neither set"
// covered, an implementation that gated current_channel on
// len(topUsers) — or sent the pair or nothing — would pass. The finder
// will call this with a frecency list and no current channel on the
// very first keystroke after launch, so that is the live case.
func TestUsersSearch_OmitsEmptyCallerState(t *testing.T) {
	base := []string{
		"token", "query", "count", "fuzz", "enable_workspace_ranking",
		"search_email", "include_profile_only_users", "default_workspace",
	}
	for _, tc := range []struct {
		name           string
		currentChannel string
		topUsers       []string
		extra          []string
	}{
		{"neither", "", nil, nil},
		{"empty slice and empty channel", "", []string{}, nil},
		{"top_users only", "", []string{"UG2U3CFCN"}, []string{"top_users"}},
		{"current_channel only", "C014Y7K5U8K", nil, []string{"current_channel"}},
		{"both", "C014Y7K5U8K", []string{"UG2U3CFCN"}, []string{"top_users", "current_channel"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := newRecorder(t, func(int) (int, string) {
				return 200, `{"ok":true,"results":[]}`
			})
			if _, err := rec.client().UsersSearch(context.Background(), "test",
				tc.currentChannel, tc.topUsers); err != nil {
				t.Fatalf("UsersSearch: %v", err)
			}
			assertExactKeys(t, rec.requests(), append(slices.Clone(base), tc.extra...)...)
		})
	}
}

// TestUsersSearch_DecodesResults gives deleted and is_bot a true
// fixture apiece. Both are false in every captured user this package
// has, and a false-only fixture is satisfied just as well by a
// missing tag or a swapped pair as by the right one.
func TestUsersSearch_DecodesResults(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"results":[
			{"id":"U04T4TH8Y","team_id":"T04T4TH8W","name":"tester","deleted":true,
			 "is_bot":false,"updated":1612802061,
			 "profile":{"display_name":"Test","real_name":"Test Person","avatar_hash":"g1a2b3"}},
			{"id":"U0B6SR2FLG1","team_id":"T04T4TH8W","name":"testbot","deleted":false,
			 "is_bot":true,"updated":1612802062,
			 "profile":{"display_name":"Test Bot","real_name":"Test Bot"}}
		]}`
	})

	got, err := rec.client().UsersSearch(context.Background(), "test", "", nil)
	if err != nil {
		t.Fatalf("UsersSearch: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d users; want 2", len(got))
	}

	u := got[0]
	if u.ID != "U04T4TH8Y" {
		t.Errorf("ID = %q; want U04T4TH8Y", u.ID)
	}
	if u.Name != "tester" {
		t.Errorf("Name = %q; want tester", u.Name)
	}
	if u.TeamID != "T04T4TH8W" {
		t.Errorf("TeamID = %q; want T04T4TH8W", u.TeamID)
	}
	if u.Version != 1612802061 {
		t.Errorf("Version = %d; want 1612802061 (from the `updated` field)", u.Version)
	}
	if !u.Deleted {
		t.Error("Deleted = false; want true (deleted is true in the fixture)")
	}
	if u.IsBot {
		t.Error("IsBot = true; want false")
	}
	if u.Profile.DisplayName != "Test" {
		t.Errorf("Profile.DisplayName = %q; want Test", u.Profile.DisplayName)
	}
	if u.Profile.RealName != "Test Person" {
		t.Errorf("Profile.RealName = %q; want Test Person", u.Profile.RealName)
	}

	// Server ranking order is the answer; preserve it.
	b := got[1]
	if b.ID != "U0B6SR2FLG1" {
		t.Errorf("second result ID = %q; want U0B6SR2FLG1 — results must keep server order", b.ID)
	}
	if !b.IsBot {
		t.Error("second result IsBot = false; want true (is_bot is true in the fixture)")
	}
	if b.Deleted {
		t.Error("second result Deleted = true; want false")
	}
}

// TestUsersSearch_DecodesProfileAvatar is the users/search half of
// TestUsersInfo_DecodesProfileAvatar, and exists to pin that the two
// endpoints agree rather than leaving it asserted only in a comment.
//
// image_original and is_custom_image appear on 42 of the 60 observed
// users/search results and on 255 of the 291 users/info results — the
// same keys at substantially the same rate. An earlier version of the
// UsersSearch doc comment claimed users/search carried an image URL
// "which a users/info profile does not", which was the same
// single-sample generalisation from the opposite direction.
//
// They share edge.User, so a field that only worked on one of them
// would be a contradiction. This makes that checkable.
func TestUsersSearch_DecodesProfileAvatar(t *testing.T) {
	const wantURL = "https://avatars.slack-edge.com/2022-11-11/T04T4TH8W_h9z8y7_original.jpg"
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"results":[
			{"id":"U0B6SR2FLG1","team_id":"T04T4TH8W","name":"nova","updated":1612802062,
			 "deleted":false,"is_bot":true,
			 "profile":{"display_name":"Nova","real_name":"Nova Prime",
			  "avatar_hash":"h9z8y7","is_custom_image":true,
			  "image_original":"` + wantURL + `"}}
		]}`
	})

	got, err := rec.client().UsersSearch(context.Background(), "nova", "", nil)
	if err != nil {
		t.Fatalf("UsersSearch: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d users; want 1", len(got))
	}
	if got[0].Profile.ImageOriginal != wantURL {
		t.Errorf("Profile.ImageOriginal = %q; want %q — users/search carries the same key "+
			"as users/info, on 42 of 60 observed results",
			got[0].Profile.ImageOriginal, wantURL)
	}
	if !got[0].Profile.IsCustomImage {
		t.Error("Profile.IsCustomImage = false; want true (is_custom_image is true in the fixture)")
	}
	// is_bot is the true-valued User bool in this fixture, where
	// TestUsersInfo_DecodesProfileAvatar uses deleted. Asserting it
	// alongside IsCustomImage covers the other half of a tag swap.
	if !got[0].IsBot {
		t.Error("IsBot = false; want true (is_bot is true in the fixture)")
	}
	if got[0].Deleted {
		t.Error("Deleted = true; want false")
	}
}

func TestUsersSearch_EmptyQueryMakesNoRequest(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"results":[{"id":"U1"}]}`
	})

	got, err := rec.client().UsersSearch(context.Background(), "", "C014Y7K5U8K",
		[]string{"UG2U3CFCN"})
	if err != nil {
		t.Fatalf("UsersSearch(\"\"): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d users; want 0 for an empty query", len(got))
	}
	if n := len(rec.requests()); n != 0 {
		t.Errorf("made %d requests for an empty query; want 0", n)
	}
}

func TestUsersSearch_PropagatesAPIError(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":false,"error":"ratelimited"}`
	})

	got, err := rec.client().UsersSearch(context.Background(), "test", "", nil)
	if err == nil {
		t.Fatal("UsersSearch returned nil error on ok:false")
	}
	if !strings.Contains(err.Error(), "ratelimited") {
		t.Errorf("error = %v; want it to mention ratelimited", err)
	}
	if got != nil {
		t.Errorf("got = %+v; want nil results alongside an error", got)
	}
}

func TestUsersSearch_IgnoresUnknownResponseFields(t *testing.T) {
	// The full 20-field observed result, including the profile
	// image_original that this package deliberately does not model.
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"results":[{
			"id":"U04T4TH8Y","team_id":"T04T4TH8W","name":"tester","deleted":false,
			"color":"9f69e7","real_name":"Test Person","tz":"America/New_York",
			"tz_label":"Eastern Standard Time","tz_offset":-18000,
			"profile":{"title":"","phone":"","skype":"","real_name":"Test Person",
			  "real_name_normalized":"Test Person","display_name":"Test",
			  "display_name_normalized":"Test","fields":null,"status_text":"",
			  "status_emoji":"","status_emoji_display_info":[],"status_expiration":0,
			  "status_clear_on_focus_end":false,"avatar_hash":"g1a2b3",
			  "image_original":"https://example.invalid/a.png","is_custom_image":true,
			  "first_name":"Test","last_name":"Person",
			  "status_text_canonical":"","team":"T04T4TH8W"},
			"is_admin":true,"is_owner":true,"is_primary_owner":true,"is_restricted":false,
			"is_ultra_restricted":false,"is_bot":false,"is_app_user":false,
			"updated":1612802061,"is_email_confirmed":true,
			"who_can_share_contact_card":"EVERYONE","a_field_slack_ships_next_week":42
		}],"some_future_top_level_key":1}`
	})

	got, err := rec.client().UsersSearch(context.Background(), "test", "", nil)
	if err != nil {
		t.Fatalf("UsersSearch on a full real-shaped response: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d users; want 1", len(got))
	}
	if got[0].ID != "U04T4TH8Y" || got[0].Version != 1612802061 ||
		got[0].Profile.DisplayName != "Test" {
		t.Errorf("modelled fields did not survive the unmodelled ones: %+v", got[0])
	}
}

// ------------------------------------------------------- both endpoints

// TestSearch_DiscardsPartialResultsOnADecodeError pins zero-value-on-
// error, which `return resp.Results, err` quietly breaks.
//
// An ok:false response cannot show this: call returns before it ever
// unmarshals into the result struct, so the struct is still zero and
// a leaking implementation looks identical to a correct one. The
// failure that discriminates is a well-formed results array followed
// by a type error — encoding/json records the first
// UnmarshalTypeError and keeps going, so Results is already populated
// when the error comes back.
//
// Handing those rows to a caller alongside an error is worse than it
// sounds for a finder: the natural call site renders results and logs
// the error, so a half-decoded response becomes a list of plausible
// wrong answers rather than a visible failure. ChannelsInfo and
// UsersInfo already discard on error; this keeps the package
// consistent.
// Both orderings are covered per endpoint, and that is not
// redundancy. Whichever key fails is the key that stays at its zero
// value, so the fixture where member_channels is the broken one
// cannot see a `return nil, resp.MemberChannels, err` — the leak it
// would expose is nil either way. Only a fixture where
// member_channels decodes *before* the failure discriminates.
func TestSearch_DiscardsPartialResultsOnADecodeError(t *testing.T) {
	t.Run("channels/member_channels fails after results decodes", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) {
			// results decodes; member_channels then fails.
			return 200, `{"ok":true,"results":[{"id":"C-LEAKED","updated":9}],` +
				`"member_channels":"not-an-array"}`
		})
		got, members, err := rec.client().ChannelsSearch(context.Background(), "test", nil)
		if err == nil {
			t.Fatal("ChannelsSearch returned nil error on an undecodable member_channels")
		}
		if got != nil {
			t.Errorf("results = %+v; want nil — those rows came from a response that "+
				"failed to decode", got)
		}
		if members != nil {
			t.Errorf("member_channels = %v; want nil alongside an error", members)
		}
	})
	t.Run("channels/results fails after member_channels decodes", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) {
			// member_channels is first on the wire and well-formed,
			// so it is fully populated by the time the second result
			// hits a string where `updated` must be a number. Both
			// return values are live at the moment the error is
			// produced, which is the only arrangement that can catch
			// either one being handed to the caller.
			return 200, `{"ok":true,"member_channels":["C-LEAKED","C2"],` +
				`"results":[{"id":"C-LEAKED","updated":9},{"id":"C2","updated":"not-a-number"}]}`
		})
		got, members, err := rec.client().ChannelsSearch(context.Background(), "test", nil)
		if err == nil {
			t.Fatal("ChannelsSearch returned nil error on an undecodable result")
		}
		if got != nil {
			t.Errorf("results = %+v; want nil alongside an error", got)
		}
		if members != nil {
			t.Errorf("member_channels = %v; want nil — this array decoded cleanly, but "+
				"it came from a response that failed, and a finder that marks channels "+
				"\"you are in this one\" from a half-read response is confidently wrong", members)
		}
	})
	t.Run("users", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) {
			// The first user decodes; the second has a string where
			// `updated` must be a number.
			return 200, `{"ok":true,"results":[{"id":"U-LEAKED","updated":9},` +
				`{"id":"U2","updated":"not-a-number"}]}`
		})
		got, err := rec.client().UsersSearch(context.Background(), "test", "", nil)
		if err == nil {
			t.Fatal("UsersSearch returned nil error on an undecodable result")
		}
		if got != nil {
			t.Errorf("results = %+v; want nil — those rows came from a response that "+
				"failed to decode", got)
		}
	})
}

// TestSearch_SendsTheWholeTopList catches an implementation that caps
// the frecency hint at the length the captures happen to show.
//
// Every captured channels/search sent exactly 22 top_channels and
// every captured users/search exactly 50 top_users, so a silent
// `[:22]` or `[:50]` is invisible to every other test in this file —
// none of them sends a longer list. This one sends more than both.
//
// Be honest about what this does and does not establish. It pins that
// slk does not truncate; it does *not* establish that sending more
// than 22/50 is a shape the server has been seen accepting, because
// no capture shows one. The lengths are the official client's own
// frecency-list sizes rather than a limit it was observed negotiating,
// so if Phase 2b hands this a list of 500 it is in unverified
// territory — and capping here would be equally unverified, just
// silently. Whichever way that is decided, it should be decided out
// loud with a capture behind it, not by a slice expression.
func TestSearch_SendsTheWholeTopList(t *testing.T) {
	// Distinct, non-ascending ids: a truncation and a sort both have
	// to fail here, and 60 clears both observed lengths at once.
	top := make([]string, 60)
	for i := range top {
		top[i] = "X" + strconv.Itoa(len(top)-i)
	}

	t.Run("channels", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) { return 200, `{"ok":true,"results":[]}` })
		if _, _, err := rec.client().ChannelsSearch(context.Background(), "test", top); err != nil {
			t.Fatalf("ChannelsSearch: %v", err)
		}
		reqs := rec.requests()
		if len(reqs) != 1 {
			t.Fatalf("made %d requests; want 1", len(reqs))
		}
		wantStrings(t, reqs[0].generic(t), "top_channels", top...)
	})
	t.Run("users", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) { return 200, `{"ok":true,"results":[]}` })
		if _, err := rec.client().UsersSearch(context.Background(), "test", "", top); err != nil {
			t.Fatalf("UsersSearch: %v", err)
		}
		reqs := rec.requests()
		if len(reqs) != 1 {
			t.Fatalf("made %d requests; want 1", len(reqs))
		}
		wantStrings(t, reqs[0].generic(t), "top_users", top...)
	})
}

// TestSearch_UsesTheCallersContext pins that ctx reaches the request
// rather than being swapped for a background one.
//
// This is the debounce contract's other half. A finder that debounces
// still has an in-flight request every time the user types past the
// timer, and the only way to stop the superseded one is to cancel its
// context. An implementation that ignored ctx would leave every
// abandoned search running to completion — turning one request per
// typing pause into one per pause that never gets cancelled, which is
// the request burst the debounce exists to prevent.
func TestSearch_UsesTheCallersContext(t *testing.T) {
	t.Run("channels", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) {
			return 200, `{"ok":true,"results":[{"id":"C1","updated":1}]}`
		})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		got, _, err := rec.client().ChannelsSearch(ctx, "test", nil)
		if err == nil {
			t.Fatal("ChannelsSearch on a cancelled context returned nil error")
		}
		if got != nil {
			t.Errorf("results = %+v; want nil on a cancelled context", got)
		}
		if n := len(rec.requests()); n != 0 {
			t.Errorf("made %d requests on a cancelled context; want 0 — the caller's "+
				"cancellation must reach the request", n)
		}
	})
	t.Run("users", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) {
			return 200, `{"ok":true,"results":[{"id":"U1","updated":1}]}`
		})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		got, err := rec.client().UsersSearch(ctx, "test", "", nil)
		if err == nil {
			t.Fatal("UsersSearch on a cancelled context returned nil error")
		}
		if got != nil {
			t.Errorf("results = %+v; want nil on a cancelled context", got)
		}
		if n := len(rec.requests()); n != 0 {
			t.Errorf("made %d requests on a cancelled context; want 0", n)
		}
	})
}

// TestSearch_SendsTheQueryVerbatim pins that we do not normalise what
// the user typed.
//
// Be honest about what this rests on: both captured queries are plain
// lowercase words with no whitespace, so the captures cannot say
// whether the official client trims or lowercases. This asserts the
// null hypothesis instead — we send what we were given — because
// matching is the server's job (that is what fuzz:1 is for) and any
// client-side normalisation is a transformation we would be inventing
// without evidence. Trimming in particular is not neutral: it changes
// which results come back and silently disagrees with a client that
// does not trim.
//
// If a future capture shows the official client normalising, this
// test is the thing to change, and it should change with the capture
// cited.
func TestSearch_SendsTheQueryVerbatim(t *testing.T) {
	const query = "  Test Channel  "
	t.Run("channels", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) { return 200, `{"ok":true,"results":[]}` })
		if _, _, err := rec.client().ChannelsSearch(context.Background(), query, nil); err != nil {
			t.Fatalf("ChannelsSearch: %v", err)
		}
		reqs := rec.requests()
		if len(reqs) != 1 {
			t.Fatalf("made %d requests; want 1", len(reqs))
		}
		wantString(t, reqs[0].generic(t), "query", query)
	})
	t.Run("users", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) { return 200, `{"ok":true,"results":[]}` })
		if _, err := rec.client().UsersSearch(context.Background(), query, "", nil); err != nil {
			t.Fatalf("UsersSearch: %v", err)
		}
		reqs := rec.requests()
		if len(reqs) != 1 {
			t.Fatalf("made %d requests; want 1", len(reqs))
		}
		wantString(t, reqs[0].generic(t), "query", query)
	})
}

// TestSearchCount_MatchesObservedRequests pins the constant itself.
// Both search samples asked for 30. A sibling endpoint, users/list,
// was observed with both 20 and 30, so 30 is not a universal edgeapi
// constant — it is what *search* was observed sending, twice.
func TestSearchCount_MatchesObservedRequests(t *testing.T) {
	if searchCount != 30 {
		t.Errorf("searchCount = %d; both captured search requests sent 30", searchCount)
	}
}
