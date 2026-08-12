package edge

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// wantFalse asserts a payload key is present *and* carries false.
//
// The two-step check is the whole point and must not be collapsed into
// `body[key] != false`. as_admin is false in 17 of 17 observations
// across the two endpoints that send it, so a value-only assertion
// cannot tell "we sent false" from "we never sent the key" — a missing
// key decodes to a nil any, and while nil != false happens to hold
// today, that is an accident of interface comparison rather than
// something the assertion says. Earlier in this phase a fixture where
// every boolean was false left 9 mutants alive for exactly this
// reason; this helper names the failure instead.
func wantFalse(t *testing.T, body map[string]any, key string) {
	t.Helper()
	got, ok := body[key]
	if !ok {
		t.Errorf("%s is absent from the payload; want it present carrying false — "+
			"the official client sends this key explicitly, so omitting it is a divergence", key)
		return
	}
	if got != false {
		t.Errorf("%s = %#v; want false", key, got)
	}
}

// isZeroCounts reports whether c is the zero Counts.
//
// A plain `c == Counts{}` will not compile: by_team makes the struct
// contain a map. reflect.DeepEqual would, but treats a nil ByTeam and
// an empty one as different, and either is a fine "nothing decoded"
// — so the fields are checked directly.
func isZeroCounts(c Counts) bool {
	return c.Everyone == 0 && c.People == 0 && c.Members == 0 && c.Guests == 0 &&
		c.Bots == 0 && c.Apps == 0 && c.Invited == 0 && len(c.ByTeam) == 0
}

// ------------------------------------------------------------ users/list

// usersListResults is a two-user results array shaped like the
// captured one, used by the tests that only care about the request.
const usersListResults = `{"ok":true,"results":[
	{"id":"U04T4TH8Y","team_id":"T04T4TH8W","name":"tester","updated":1612802061,
	 "profile":{"display_name":"Test","real_name":"Test Person"}}
]}`

// TestUsersList_SendsObservedPayload pins the request against the four
// captured users/list samples, which agree on every key.
func TestUsersList_SendsObservedPayload(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) { return 200, usersListResults })

	if _, _, err := rec.client().UsersList(context.Background(), "C04T4TH9Q", 30); err != nil {
		t.Fatalf("UsersList: %v", err)
	}

	reqs := rec.requests()
	if len(reqs) != 1 {
		t.Fatalf("made %d requests; want exactly 1 — users/list is a single call, never batched", len(reqs))
	}
	if reqs[0].path != "/cache/T04T4TH8W/users/list" {
		t.Errorf("path = %q; want /cache/T04T4TH8W/users/list", reqs[0].path)
	}

	assertExactKeys(t, reqs, "token", "channels", "present_first", "filter", "count")

	body := reqs[0].generic(t)
	wantString(t, body, "token", "xoxc-test")
	// An array with one id, not a bare string. The wire shape is an
	// array in 4 of 4 observations even though every one of them
	// carried exactly one channel; sending the id unwrapped would be
	// a shape the server has never been seen accepting from the
	// official client.
	wantStrings(t, body, "channels", "C04T4TH9Q")
	wantTrue(t, body, "present_first")
	// The literal string, byte for byte, deliberately not the
	// usersListFilter constant: an assertion against the constant is
	// satisfied by whatever the constant happens to hold and so could
	// never notice it drifting off the captured value.
	wantString(t, body, "filter", "everyone AND NOT bots AND NOT apps")
	wantNumber(t, body, "count", 30)
}

// TestUsersList_CountIsTheCallersValue walks both counts the captures
// show. Three of the four observations sent 30 and one sent 20, so a
// payload that hardcoded 30 would match the majority of the evidence
// and still be wrong.
func TestUsersList_CountIsTheCallersValue(t *testing.T) {
	// 7 is not an observed value. It is here because 20 and 30 are
	// both plausible hardcodes for this endpoint, so a third,
	// arbitrary count is what proves the value is genuinely the
	// caller's rather than one of the two the captures show.
	for _, want := range []int{20, 30, 7} {
		t.Run(strconv.Itoa(want), func(t *testing.T) {
			rec := newRecorder(t, func(int) (int, string) { return 200, usersListResults })
			if _, _, err := rec.client().UsersList(context.Background(), "C1", want); err != nil {
				t.Fatalf("UsersList: %v", err)
			}
			reqs := rec.requests()
			if len(reqs) != 1 {
				t.Fatalf("made %d requests; want 1", len(reqs))
			}
			wantNumber(t, reqs[0].generic(t), "count", float64(want))
		})
	}
}

// TestUsersList_ChannelIsTheCallersChannel catches a payload that
// wired the channel id to something other than the argument — the
// team id, say, which is the other id in scope here.
func TestUsersList_ChannelIsTheCallersChannel(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) { return 200, usersListResults })
	if _, _, err := rec.client().UsersList(context.Background(), "C99DIFFERENT", 30); err != nil {
		t.Fatalf("UsersList: %v", err)
	}
	reqs := rec.requests()
	if len(reqs) != 1 {
		t.Fatalf("made %d requests; want 1", len(reqs))
	}
	wantStrings(t, reqs[0].generic(t), "channels", "C99DIFFERENT")
}

// TestUsersList_DecodesResults covers the response half.
//
// The fixture gives `deleted` the value true and `is_bot` false, one
// per user rather than all-false, because a fixture where every
// boolean is false cannot tell "decoded false" from "never decoded".
// (Both flags are synthetic here: the captured filter excludes bots.
// User is shared with users/info and users/search, so the tags have
// to hold on this response too.)
//
// The two real_name values differ on purpose. A users/list result
// carries real_name at the top level *and* inside profile; User reads
// the profile one, and identical values would let a mutant read the
// wrong one and survive.
func TestUsersList_DecodesResults(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"results":[
			{"id":"U04T4TH8Y","team_id":"T04T4TH8W","name":"tester","deleted":true,
			 "is_bot":false,"updated":1612802061,"real_name":"Top Level Name",
			 "profile":{"display_name":"Test","real_name":"Profile Name","avatar_hash":"g1a2b3"}},
			{"id":"U0B6SR2FLG1","team_id":"T99OTHER","name":"other","deleted":false,
			 "is_bot":true,"updated":1612802062,
			 "profile":{"display_name":"Other","real_name":"Other Person"}}
		]}`
	})

	got, _, err := rec.client().UsersList(context.Background(), "C1", 30)
	if err != nil {
		t.Fatalf("UsersList: %v", err)
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
	if u.Profile.RealName != "Profile Name" {
		t.Errorf("Profile.RealName = %q; want %q — a users/list result carries "+
			"real_name at the top level too, and this must read the profile one",
			u.Profile.RealName, "Profile Name")
	}

	// present_first:true means the server ranks the results; order is
	// the answer, not an implementation detail.
	if got[1].ID != "U0B6SR2FLG1" {
		t.Errorf("second result ID = %q; want U0B6SR2FLG1 — results must keep server order", got[1].ID)
	}
	if got[1].Deleted {
		t.Error("second result Deleted = true; want false")
	}
	if !got[1].IsBot {
		t.Error("second result IsBot = false; want true (is_bot is true in the fixture)")
	}
	if got[1].TeamID != "T99OTHER" {
		t.Errorf("second result TeamID = %q; want T99OTHER", got[1].TeamID)
	}
}

// TestUsersList_TruncatedTracksNextMarker pins both observed response
// shapes. next_marker came back in 3 of 4 captures — present exactly
// when the page came back full — and its presence is the only signal
// that the channel has more members than the caller asked for.
func TestUsersList_TruncatedTracksNextMarker(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) {
			return 200, `{"ok":true,"results":[{"id":"U1"}],
				"next_marker":"eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.abcdef"}`
		})
		got, truncated, err := rec.client().UsersList(context.Background(), "C1", 30)
		if err != nil {
			t.Fatalf("UsersList: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d users; want 1", len(got))
		}
		if !truncated {
			t.Error("truncated = false; want true — next_marker is present, so the " +
				"channel has more members than this page carries and a caller that " +
				"renders it as complete is lying to the user")
		}
	})
	t.Run("absent", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) {
			return 200, `{"ok":true,"results":[{"id":"U1"}]}`
		})
		_, truncated, err := rec.client().UsersList(context.Background(), "C1", 30)
		if err != nil {
			t.Fatalf("UsersList: %v", err)
		}
		if truncated {
			t.Error("truncated = true; want false when next_marker is absent")
		}
	})
	t.Run("empty string", func(t *testing.T) {
		// Not observed, but the difference between "absent" and
		// "present and empty" is one an implementation could get
		// wrong either way, and an empty cursor is not a cursor.
		rec := newRecorder(t, func(int) (int, string) {
			return 200, `{"ok":true,"results":[{"id":"U1"}],"next_marker":""}`
		})
		_, truncated, err := rec.client().UsersList(context.Background(), "C1", 30)
		if err != nil {
			t.Fatalf("UsersList: %v", err)
		}
		if truncated {
			t.Error("truncated = true; want false for an empty next_marker")
		}
	})
}

// TestUsersList_TruncationDoesNotLeakBetweenCalls catches a marker
// held on the Client and reused: a full first page would then make
// every later page look truncated forever.
func TestUsersList_TruncationDoesNotLeakBetweenCalls(t *testing.T) {
	rec := newRecorder(t, func(n int) (int, string) {
		if n == 1 {
			return 200, `{"ok":true,"results":[{"id":"U1"}],"next_marker":"abc"}`
		}
		return 200, `{"ok":true,"results":[{"id":"U2"}]}`
	})
	c := rec.client()

	if _, truncated, err := c.UsersList(context.Background(), "C1", 30); err != nil || !truncated {
		t.Fatalf("first call: truncated = %v, err = %v; want true, nil", truncated, err)
	}
	got, truncated, err := c.UsersList(context.Background(), "C2", 30)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if truncated {
		t.Error("second call truncated = true; want false — the marker from the " +
			"first call must not survive into the second")
	}
	if len(got) != 1 || got[0].ID != "U2" {
		t.Errorf("second call results = %+v; want just U2", got)
	}
}

func TestUsersList_RejectsAnEmptyChannel(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) { return 200, usersListResults })

	got, truncated, err := rec.client().UsersList(context.Background(), "", 30)
	if err == nil {
		t.Fatal("UsersList with no channel returned nil error; want one — this call " +
			"is channel-scoped by construction and a channel-less variant is the " +
			"workspace-wide enumeration the package exists to avoid")
	}
	if got != nil || truncated {
		t.Errorf("got = %+v, %v; want nil, false alongside the error", got, truncated)
	}
	if n := len(rec.requests()); n != 0 {
		t.Errorf("made %d requests with no channel; want 0", n)
	}
}

func TestUsersList_RejectsANonPositiveCount(t *testing.T) {
	for _, count := range []int{0, -1} {
		rec := newRecorder(t, func(int) (int, string) { return 200, usersListResults })
		got, truncated, err := rec.client().UsersList(context.Background(), "C1", count)
		if err == nil {
			t.Errorf("UsersList(count=%d) returned nil error; want one", count)
		}
		if got != nil || truncated {
			t.Errorf("count=%d: got = %+v, %v; want nil, false alongside the error", count, got, truncated)
		}
		if n := len(rec.requests()); n != 0 {
			t.Errorf("count=%d: made %d requests; want 0", count, n)
		}
	}
}

func TestUsersList_PropagatesAPIError(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":false,"error":"channel_not_found"}`
	})

	got, truncated, err := rec.client().UsersList(context.Background(), "C1", 30)
	if err == nil {
		t.Fatal("UsersList returned nil error on ok:false")
	}
	if !strings.Contains(err.Error(), "channel_not_found") {
		t.Errorf("error = %v; want it to mention channel_not_found", err)
	}
	if got != nil || truncated {
		t.Errorf("got = %+v, %v; want nil, false alongside an error", got, truncated)
	}
}

func TestUsersList_IgnoresUnknownResponseFields(t *testing.T) {
	// A result carrying the full captured field set plus keys Slack
	// has not shipped yet. Slack adds fields without notice; a decode
	// that rejected them would break in production, not in CI.
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"a_future_top_level_key":{"a":1},"next_marker":"abc","results":[{
			"id":"U04T4TH8Y","team_id":"T04T4TH8W","name":"tester","deleted":false,
			"color":"9f69e7","real_name":"Top Level","tz":"America/New_York",
			"tz_label":"Eastern Standard Time","tz_offset":-18000,
			"profile":{"title":"","phone":"","skype":"","real_name":"Profile Name",
			 "real_name_normalized":"Profile Name","display_name":"Test",
			 "display_name_normalized":"Test","fields":null,"status_text":"",
			 "status_emoji":"","status_emoji_display_info":[],"status_expiration":0,
			 "status_clear_on_focus_end":false,"avatar_hash":"g1a2b3","first_name":"Test",
			 "last_name":"Person","status_text_canonical":"","team":"T04T4TH8W"},
			"is_admin":false,"is_owner":false,"is_primary_owner":false,
			"is_restricted":false,"is_ultra_restricted":false,"is_bot":false,
			"is_app_user":false,"updated":1612802061,"is_email_confirmed":true,
			"who_can_share_contact_card":"EVERYONE","a_field_slack_ships_next_week":42
		}]}`
	})

	got, truncated, err := rec.client().UsersList(context.Background(), "C1", 30)
	if err != nil {
		t.Fatalf("UsersList on a full real-shaped response: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d users; want 1", len(got))
	}
	if got[0].ID != "U04T4TH8Y" || got[0].Version != 1612802061 || got[0].Profile.RealName != "Profile Name" {
		t.Errorf("modelled fields did not survive the unmodelled ones: %+v", got[0])
	}
	if !truncated {
		t.Error("truncated = false; want true")
	}
}

// --------------------------------------------------- channels/membership

// TestChannelsMembership_SendsObservedPayload pins the request against
// the ten captured samples, which agree on every key.
func TestChannelsMembership_SendsObservedPayload(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"channel":"C04T4TH9Q","members":["U1"],"non_members":["U2","U3"]}`
	})

	if _, _, err := rec.client().ChannelsMembership(context.Background(), "C04T4TH9Q",
		[]string{"U1", "U2", "U3"}); err != nil {
		t.Fatalf("ChannelsMembership: %v", err)
	}

	reqs := rec.requests()
	if len(reqs) != 1 {
		t.Fatalf("made %d requests; want exactly 1 — the captures put 66 ids in one "+
			"request, so this endpoint is not batched", len(reqs))
	}
	if reqs[0].path != "/cache/T04T4TH8W/channels/membership" {
		t.Errorf("path = %q; want /cache/T04T4TH8W/channels/membership", reqs[0].path)
	}

	assertExactKeys(t, reqs, "token", "channel", "users", "as_admin")

	body := reqs[0].generic(t)
	wantString(t, body, "token", "xoxc-test")
	// A bare string here, unlike users/list's array — the two
	// endpoints genuinely disagree on the shape and the captures are
	// unanimous on each.
	wantString(t, body, "channel", "C04T4TH9Q")
	wantStrings(t, body, "users", "U1", "U2", "U3")
	wantFalse(t, body, "as_admin")
}

// TestChannelsMembership_SendsEveryUserIDInOneRequest pins the batch
// size question: the largest observed request carried 66 ids, so this
// must not be split the way channels/info and users/info are.
func TestChannelsMembership_SendsEveryUserIDInOneRequest(t *testing.T) {
	want := make([]string, 66)
	for i := range want {
		want[i] = "U" + string(rune('A'+i%26)) + strings.Repeat("0", 1+i/26)
	}
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"channel":"C1","non_members":[]}`
	})

	if _, _, err := rec.client().ChannelsMembership(context.Background(), "C1", want); err != nil {
		t.Fatalf("ChannelsMembership: %v", err)
	}
	reqs := rec.requests()
	if len(reqs) != 1 {
		t.Fatalf("made %d requests for 66 ids; want 1 — the captures show 66 in a "+
			"single request, and splitting them invents a shape the server never sees", len(reqs))
	}
	wantStrings(t, reqs[0].generic(t), "users", want...)
}

// TestChannelsMembership_PartitionsTheUsersSent covers the response
// half and the invariant that held in all 10 observations:
// len(members) + len(non_members) == len(users sent).
//
// The two arrays are disjoint and differently sized on purpose, so a
// swap of the two return values fails here rather than passing.
func TestChannelsMembership_PartitionsTheUsersSent(t *testing.T) {
	sent := []string{"U1", "U2", "U3", "U4", "U5"}
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"channel":"C1",
			"members":["U1","U3"],"non_members":["U2","U4","U5"]}`
	})

	members, nonMembers, err := rec.client().ChannelsMembership(context.Background(), "C1", sent)
	if err != nil {
		t.Fatalf("ChannelsMembership: %v", err)
	}
	if want := []string{"U1", "U3"}; !slices.Equal(members, want) {
		t.Errorf("members = %v; want %v", members, want)
	}
	if want := []string{"U2", "U4", "U5"}; !slices.Equal(nonMembers, want) {
		t.Errorf("non_members = %v; want %v", nonMembers, want)
	}
	if got := len(members) + len(nonMembers); got != len(sent) {
		t.Errorf("members+non_members covers %d ids; want %d — every id sent came "+
			"back in exactly one of the two arrays in all 10 observations, so a "+
			"short count means one of them was dropped", got, len(sent))
	}
	// The partition is a partition: no id in both, none invented.
	seen := map[string]bool{}
	for _, id := range slices.Concat(members, nonMembers) {
		if seen[id] {
			t.Errorf("%s appears in both members and non_members", id)
		}
		seen[id] = true
		if !slices.Contains(sent, id) {
			t.Errorf("%s came back but was never sent", id)
		}
	}
}

// TestChannelsMembership_AbsentArraysDecodeEmpty pins the two shapes
// the captures show alongside the both-present one: members is absent
// when nobody in the batch belongs (1 of 10), and non_members is
// absent when everybody does (5 of 10). Absence means empty, never an
// error.
func TestChannelsMembership_AbsentArraysDecodeEmpty(t *testing.T) {
	t.Run("members absent", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) {
			return 200, `{"ok":true,"channel":"C1","non_members":["U1"]}`
		})
		members, nonMembers, err := rec.client().ChannelsMembership(
			context.Background(), "C1", []string{"U1"})
		if err != nil {
			t.Fatalf("ChannelsMembership with no members key: %v", err)
		}
		if len(members) != 0 {
			t.Errorf("members = %v; want empty when the key is absent", members)
		}
		if want := []string{"U1"}; !slices.Equal(nonMembers, want) {
			t.Errorf("non_members = %v; want %v", nonMembers, want)
		}
	})
	t.Run("non_members absent", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) {
			return 200, `{"ok":true,"channel":"C1","members":["U1","U2","U3"]}`
		})
		members, nonMembers, err := rec.client().ChannelsMembership(
			context.Background(), "C1", []string{"U1", "U2", "U3"})
		if err != nil {
			t.Fatalf("ChannelsMembership with no non_members key: %v", err)
		}
		if want := []string{"U1", "U2", "U3"}; !slices.Equal(members, want) {
			t.Errorf("members = %v; want %v", members, want)
		}
		if len(nonMembers) != 0 {
			t.Errorf("non_members = %v; want empty when the key is absent", nonMembers)
		}
	})
}

// TestChannelsMembership_NoUsersMakesNoRequest: an empty batch can
// only come back empty, and a caller that filtered its unknown ids
// down to none is the normal way to get here. The captures never show
// an empty users array — the smallest is 1.
func TestChannelsMembership_NoUsersMakesNoRequest(t *testing.T) {
	for _, tc := range []struct {
		name string
		ids  []string
	}{
		{"nil", nil},
		{"empty", []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := newRecorder(t, func(int) (int, string) {
				return 200, `{"ok":true,"channel":"C1","members":["U1"]}`
			})
			members, nonMembers, err := rec.client().ChannelsMembership(
				context.Background(), "C1", tc.ids)
			if err != nil {
				t.Fatalf("ChannelsMembership with no ids: %v", err)
			}
			if len(members) != 0 || len(nonMembers) != 0 {
				t.Errorf("got %v, %v; want empty for an empty batch", members, nonMembers)
			}
			if n := len(rec.requests()); n != 0 {
				t.Errorf("made %d requests for an empty batch; want 0", n)
			}
		})
	}
}

func TestChannelsMembership_RejectsAnEmptyChannel(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"channel":"C1","members":["U1"]}`
	})
	members, nonMembers, err := rec.client().ChannelsMembership(
		context.Background(), "", []string{"U1"})
	if err == nil {
		t.Fatal("ChannelsMembership with no channel returned nil error; want one")
	}
	if members != nil || nonMembers != nil {
		t.Errorf("got %v, %v; want nil, nil alongside the error", members, nonMembers)
	}
	if n := len(rec.requests()); n != 0 {
		t.Errorf("made %d requests with no channel; want 0", n)
	}
}

func TestChannelsMembership_PropagatesAPIError(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":false,"error":"channel_not_found"}`
	})

	members, nonMembers, err := rec.client().ChannelsMembership(
		context.Background(), "C1", []string{"U1"})
	if err == nil {
		t.Fatal("ChannelsMembership returned nil error on ok:false")
	}
	if !strings.Contains(err.Error(), "channel_not_found") {
		t.Errorf("error = %v; want it to mention channel_not_found", err)
	}
	if members != nil || nonMembers != nil {
		t.Errorf("got %v, %v; want nil, nil alongside an error", members, nonMembers)
	}
}

func TestChannelsMembership_IgnoresUnknownResponseFields(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"channel":"C1","members":["U1"],"non_members":["U2"],
			"a_field_slack_ships_next_week":{"nested":[1,2,3]}}`
	})

	members, nonMembers, err := rec.client().ChannelsMembership(
		context.Background(), "C1", []string{"U1", "U2"})
	if err != nil {
		t.Fatalf("ChannelsMembership on a response with unknown fields: %v", err)
	}
	if want := []string{"U1"}; !slices.Equal(members, want) {
		t.Errorf("members = %v; want %v", members, want)
	}
	if want := []string{"U2"}; !slices.Equal(nonMembers, want) {
		t.Errorf("non_members = %v; want %v", nonMembers, want)
	}
}

// TestMembership_EchoedChannelIsIgnored pins a deliberate omission
// rather than a capability.
//
// channels/membership echoes `channel` in 10 of 10 responses and
// users/counts in 7 of 7, and neither implementation reads it. That
// is on purpose: the value is one the caller supplied a moment
// earlier, so it carries no information, and a mismatch check could
// only ever produce a false positive — Go's http.Client pairs each
// response with its own request, so there is no path by which another
// channel's answer arrives here, while a future id normalisation on
// Slack's side would fail a call that worked.
//
// The assertion is that a disagreeing echo changes nothing. If
// somebody later decides validation is worth it, this test fires and
// makes them read the paragraph above first.
func TestMembership_EchoedChannelIsIgnored(t *testing.T) {
	t.Run("channels/membership", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) {
			return 200, `{"ok":true,"channel":"C99SOMETHINGELSE","members":["U1"]}`
		})
		members, _, err := rec.client().ChannelsMembership(
			context.Background(), "C1", []string{"U1"})
		if err != nil {
			t.Fatalf("ChannelsMembership: %v", err)
		}
		if want := []string{"U1"}; !slices.Equal(members, want) {
			t.Errorf("members = %v; want %v", members, want)
		}
	})
	t.Run("users/counts", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) {
			return 200, `{"ok":true,"channel":"C99SOMETHINGELSE","counts":{"everyone":91}}`
		})
		got, err := rec.client().UsersCounts(context.Background(), "C1")
		if err != nil {
			t.Fatalf("UsersCounts: %v", err)
		}
		if got.Everyone != 91 {
			t.Errorf("Everyone = %d; want 91", got.Everyone)
		}
	})
}

// -------------------------------------------------------- users/counts

// TestUsersCounts_SendsObservedPayload pins the request against the
// seven captured samples, which agree on all three keys.
func TestUsersCounts_SendsObservedPayload(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"channel":"C04T4TH9Q","counts":{"everyone":1}}`
	})

	if _, err := rec.client().UsersCounts(context.Background(), "C04T4TH9Q"); err != nil {
		t.Fatalf("UsersCounts: %v", err)
	}

	reqs := rec.requests()
	if len(reqs) != 1 {
		t.Fatalf("made %d requests; want exactly 1", len(reqs))
	}
	if reqs[0].path != "/cache/T04T4TH8W/users/counts" {
		t.Errorf("path = %q; want /cache/T04T4TH8W/users/counts", reqs[0].path)
	}

	assertExactKeys(t, reqs, "token", "channel", "as_admin")

	body := reqs[0].generic(t)
	wantString(t, body, "token", "xoxc-test")
	wantString(t, body, "channel", "C04T4TH9Q")
	wantFalse(t, body, "as_admin")
}

// TestUsersCounts_DecodesEveryField gives every count a distinct value.
//
// That is the point of the fixture, not decoration: with all the
// counts equal, a field reading a neighbour's JSON key — Members
// tagged `everyone`, say — decodes the right number by accident and
// the mutant survives. Distinct values make each tag observable
// exactly once.
func TestUsersCounts_DecodesEveryField(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"channel":"C1","counts":{
			"everyone":91,"people":82,"members":73,"guests":64,"bots":55,
			"apps":46,"invited":37,"by_team":{"T04T4TH8W":28,"T99OTHER":19}}}`
	})

	got, err := rec.client().UsersCounts(context.Background(), "C1")
	if err != nil {
		t.Fatalf("UsersCounts: %v", err)
	}

	for _, tc := range []struct {
		name string
		got  int
		want int
	}{
		{"Everyone", got.Everyone, 91},
		{"People", got.People, 82},
		{"Members", got.Members, 73},
		{"Guests", got.Guests, 64},
		{"Bots", got.Bots, 55},
		{"Apps", got.Apps, 46},
		{"Invited", got.Invited, 37},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d; want %d", tc.name, tc.got, tc.want)
		}
	}

	want := map[string]int{"T04T4TH8W": 28, "T99OTHER": 19}
	if !maps.Equal(got.ByTeam, want) {
		t.Errorf("ByTeam = %v; want %v — this is the per-team split, and on Grid it "+
			"is the only field that says which workspaces a channel spans", got.ByTeam, want)
	}
}

// TestUsersCounts_AbsentInvitedIsZero: invited appears in 2 of 7
// observations, both from boot captures. Absence is the common case
// and must decode to zero rather than failing.
func TestUsersCounts_AbsentInvitedIsZero(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"channel":"C1","counts":{
			"everyone":91,"people":82,"members":73,"guests":64,"bots":55,
			"apps":46,"by_team":{"T04T4TH8W":28}}}`
	})

	got, err := rec.client().UsersCounts(context.Background(), "C1")
	if err != nil {
		t.Fatalf("UsersCounts without invited: %v", err)
	}
	if got.Invited != 0 {
		t.Errorf("Invited = %d; want 0 when the key is absent", got.Invited)
	}
	if got.Everyone != 91 || got.Members != 73 {
		t.Errorf("the other counts did not survive the absent one: %+v", got)
	}
}

// TestUsersCounts_ReadsTheNestedCountsObject catches an implementation
// that decoded the counts from the top level of the response. The
// captures nest them under `counts` in 7 of 7 — the plan that this
// task came from assumed the endpoint returned a bare int, so getting
// the nesting wrong is the live failure mode here, not a hypothetical.
func TestUsersCounts_ReadsTheNestedCountsObject(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"channel":"C1","everyone":999,"members":888,
			"counts":{"everyone":91,"members":73,"by_team":{"T04T4TH8W":28}}}`
	})

	got, err := rec.client().UsersCounts(context.Background(), "C1")
	if err != nil {
		t.Fatalf("UsersCounts: %v", err)
	}
	if got.Everyone != 91 {
		t.Errorf("Everyone = %d; want 91 — the counts live under the `counts` key, "+
			"not at the top level", got.Everyone)
	}
	if got.Members != 73 {
		t.Errorf("Members = %d; want 73 — read from counts.members", got.Members)
	}
}

func TestUsersCounts_RejectsAnEmptyChannel(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"channel":"C1","counts":{"everyone":1}}`
	})
	got, err := rec.client().UsersCounts(context.Background(), "")
	if err == nil {
		t.Fatal("UsersCounts with no channel returned nil error; want one")
	}
	if !isZeroCounts(got) {
		t.Errorf("got = %+v; want the zero Counts alongside the error", got)
	}
	if n := len(rec.requests()); n != 0 {
		t.Errorf("made %d requests with no channel; want 0", n)
	}
}

func TestUsersCounts_PropagatesAPIError(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":false,"error":"channel_not_found"}`
	})

	got, err := rec.client().UsersCounts(context.Background(), "C1")
	if err == nil {
		t.Fatal("UsersCounts returned nil error on ok:false")
	}
	if !strings.Contains(err.Error(), "channel_not_found") {
		t.Errorf("error = %v; want it to mention channel_not_found", err)
	}
	if !isZeroCounts(got) {
		t.Errorf("got = %+v; want the zero Counts alongside an error", got)
	}
}

// ------------------------------------------------------ all three calls

// TestMembers_DiscardsPartialResultsOnADecodeError pins zero-value-on-
// error for every method in this file, which `return resp.X, err`
// quietly breaks.
//
// The ok:false tests above cannot show this and must not be mistaken
// for it: call returns before it ever unmarshals into the result
// struct, so the struct is still zero and a leaking implementation is
// byte-identical to a correct one. The failure that discriminates is a
// modelled field that decodes cleanly followed by a *later* field with
// the wrong type — encoding/json records the first UnmarshalTypeError
// and keeps decoding, so the earlier field is already populated when
// the error comes back. That is what makes the leak reachable rather
// than theoretical.
//
// Handing that data to a caller alongside an error is worse than it
// sounds here. A member list rendered next to a logged error is a
// roster the user has no reason to doubt; a `truncated` read off a
// half-decoded response tells them "30+ members" about a channel
// nobody counted; and Counts is a bare number with nowhere to put a
// caveat. cache.go states this invariant for fetchInfo and search.go
// pins it for both search endpoints — this closes the file that had
// no coverage at all.
func TestMembers_DiscardsPartialResultsOnADecodeError(t *testing.T) {
	t.Run("UsersList", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) {
			// next_marker comes first and decodes, so a leaking
			// `truncated` would be true; the first result decodes, so
			// a leaking `users` would be non-empty; the second result
			// has a string where `updated` must be a number. All
			// three return values are live at the moment of failure.
			return 200, `{"ok":true,"next_marker":"abc",` +
				`"results":[{"id":"U-LEAKED","updated":9},{"id":"U2","updated":"not-a-number"}]}`
		})
		got, truncated, err := rec.client().UsersList(context.Background(), "C1", 30)
		if err == nil {
			t.Fatal("UsersList returned nil error on an undecodable result")
		}
		if got != nil {
			t.Errorf("results = %+v; want nil — those rows came from a response that "+
				"failed to decode, and a roster rendered beside a logged error looks "+
				"exactly like a complete one", got)
		}
		if truncated {
			t.Error("truncated = true; want false alongside an error — next_marker " +
				"decoded, but it describes a page this call never successfully read")
		}
	})
	t.Run("ChannelsMembership/non_members fails after members decodes", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) {
			return 200, `{"ok":true,"channel":"C1","members":["U1","U3"],` +
				`"non_members":"not-an-array"}`
		})
		members, nonMembers, err := rec.client().ChannelsMembership(
			context.Background(), "C1", []string{"U1", "U2", "U3"})
		if err == nil {
			t.Fatal("ChannelsMembership returned nil error on an undecodable non_members")
		}
		if members != nil {
			t.Errorf("members = %v; want nil alongside an error — this array decoded, "+
				"but the partition it is half of never arrived, so treating it as the "+
				"answer silently reclassifies everybody missing from it as a non-member",
				members)
		}
		if nonMembers != nil {
			t.Errorf("non_members = %v; want nil alongside an error", nonMembers)
		}
	})
	t.Run("ChannelsMembership/members fails after non_members decodes", func(t *testing.T) {
		// The mirror image. Whichever key fails is the key left at
		// its zero value, so one ordering alone cannot see both
		// halves of the partition leaking.
		rec := newRecorder(t, func(int) (int, string) {
			return 200, `{"ok":true,"channel":"C1","non_members":["U2"],` +
				`"members":"not-an-array"}`
		})
		members, nonMembers, err := rec.client().ChannelsMembership(
			context.Background(), "C1", []string{"U1", "U2"})
		if err == nil {
			t.Fatal("ChannelsMembership returned nil error on an undecodable members")
		}
		if members != nil {
			t.Errorf("members = %v; want nil alongside an error", members)
		}
		if nonMembers != nil {
			t.Errorf("non_members = %v; want nil alongside an error", nonMembers)
		}
	})
	t.Run("UsersCounts", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) {
			// everyone decodes; people is a string where an int must
			// be. resp.Counts is genuinely populated when the error
			// is returned.
			return 200, `{"ok":true,"channel":"C1","counts":{"everyone":91,` +
				`"people":"not-a-number"}}`
		})
		got, err := rec.client().UsersCounts(context.Background(), "C1")
		if err == nil {
			t.Fatal("UsersCounts returned nil error on an undecodable count")
		}
		if !isZeroCounts(got) {
			t.Errorf("got = %+v; want the zero Counts alongside an error — a count is "+
				"a bare number with nowhere to carry a caveat, so a partial one is "+
				"indistinguishable from a real one at every call site", got)
		}
	})
}

// TestMembers_UseTheCallersContext pins that ctx reaches the request
// rather than being swapped for a background one.
//
// These are the channel-switch calls: all three fire when the user
// opens a channel, and a superseded switch is precisely when
// cancellation matters. A user clicking through four channels with an
// implementation that ignored ctx would leave twelve abandoned
// requests running to completion against one credential — a burst
// shape produced by nobody's hand, which is the fingerprint this
// package exists to avoid. Cancellation is the only thing that stops
// them, and it can only arrive through the caller's context.
//
// The handler answers successfully on purpose. A client that honours
// the cancelled context never reaches it; one that drops it gets a
// clean success and fails the assertion below immediately, rather
// than hanging.
func TestMembers_UseTheCallersContext(t *testing.T) {
	cancelled := func() context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}

	t.Run("UsersList", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) { return 200, usersListResults })
		got, truncated, err := rec.client().UsersList(cancelled(), "C1", 30)
		if err == nil {
			t.Fatal("UsersList on a cancelled context returned nil error")
		}
		if got != nil || truncated {
			t.Errorf("got = %+v, %v; want nil, false on a cancelled context", got, truncated)
		}
		if n := len(rec.requests()); n != 0 {
			t.Errorf("made %d requests on a cancelled context; want 0 — the caller's "+
				"cancellation must reach the request", n)
		}
	})
	t.Run("ChannelsMembership", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) {
			return 200, `{"ok":true,"channel":"C1","members":["U1"]}`
		})
		members, nonMembers, err := rec.client().ChannelsMembership(
			cancelled(), "C1", []string{"U1"})
		if err == nil {
			t.Fatal("ChannelsMembership on a cancelled context returned nil error")
		}
		if members != nil || nonMembers != nil {
			t.Errorf("got %v, %v; want nil, nil on a cancelled context", members, nonMembers)
		}
		if n := len(rec.requests()); n != 0 {
			t.Errorf("made %d requests on a cancelled context; want 0", n)
		}
	})
	t.Run("UsersCounts", func(t *testing.T) {
		rec := newRecorder(t, func(int) (int, string) {
			return 200, `{"ok":true,"channel":"C1","counts":{"everyone":91}}`
		})
		got, err := rec.client().UsersCounts(cancelled(), "C1")
		if err == nil {
			t.Fatal("UsersCounts on a cancelled context returned nil error")
		}
		if !isZeroCounts(got) {
			t.Errorf("got = %+v; want the zero Counts on a cancelled context", got)
		}
		if n := len(rec.requests()); n != 0 {
			t.Errorf("made %d requests on a cancelled context; want 0", n)
		}
	})
}

// ------------------------------------------------ users/counts, continued

func TestUsersCounts_IgnoresUnknownResponseFields(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"channel":"C1","a_future_top_level_key":[1,2],
			"counts":{"everyone":91,"people":82,"members":73,"guests":64,"bots":55,
			"apps":46,"by_team":{"T04T4TH8W":28},"a_count_slack_ships_next_week":7}}`
	})

	got, err := rec.client().UsersCounts(context.Background(), "C1")
	if err != nil {
		t.Fatalf("UsersCounts on a response with unknown fields: %v", err)
	}
	if got.Everyone != 91 || got.Apps != 46 {
		t.Errorf("modelled counts did not survive the unmodelled ones: %+v", got)
	}
	if want := map[string]int{"T04T4TH8W": 28}; !maps.Equal(got.ByTeam, want) {
		t.Errorf("ByTeam = %v; want %v", got.ByTeam, want)
	}
}
