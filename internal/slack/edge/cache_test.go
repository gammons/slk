package edge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

// capturedRequest is one request the recorder saw, kept as raw bytes so
// each test can decode it in whatever shape it wants to assert on.
type capturedRequest struct {
	path string
	raw  []byte
}

func (r capturedRequest) generic(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.raw, &m); err != nil {
		t.Fatalf("request body is not JSON: %v (%q)", err, r.raw)
	}
	return m
}

func (r capturedRequest) updatedIDs(t *testing.T) map[string]int64 {
	t.Helper()
	var m struct {
		UpdatedIDs map[string]int64 `json:"updated_ids"`
	}
	if err := json.Unmarshal(r.raw, &m); err != nil {
		t.Fatalf("request body is not JSON: %v (%q)", err, r.raw)
	}
	return m.UpdatedIDs
}

func (r capturedRequest) keys(t *testing.T) []string {
	t.Helper()
	var ks []string
	for k := range r.generic(t) {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

// assertExactKeys pins the top-level key set of every captured
// request, not just the first.
//
// The package's whole claim is that slk puts exactly the keys on the
// wire that the official client puts there, and an extra key is as
// much a fingerprint as a missing one. Checking only reqs[0] verifies
// that for the first request of a call and nothing else, which leaves
// "flags leak in from batch 2 onwards" completely uncovered — so this
// runs over all of them.
func assertExactKeys(t *testing.T, reqs []capturedRequest, want ...string) {
	t.Helper()
	if len(reqs) == 0 {
		t.Fatal("no requests captured; an exact-key assertion over zero requests proves nothing")
	}
	slices.Sort(want)
	for i, r := range reqs {
		if got := r.keys(t); !slices.Equal(got, want) {
			t.Errorf("request %d keys = %v; want exactly %v", i+1, got, want)
		}
	}
}

// recorder is an httptest server that records every request body and
// answers from a per-request-number reply function.
type recorder struct {
	mu   sync.Mutex
	reqs []capturedRequest
	srv  *httptest.Server
}

// newRecorder starts a server whose reply is chosen by the 1-based
// index of the request. Returning a status other than 200 lets a test
// fail a specific batch.
func newRecorder(t *testing.T, reply func(n int) (int, string)) *recorder {
	t.Helper()
	rec := &recorder{}
	rec.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading request body: %v", err)
		}
		rec.mu.Lock()
		rec.reqs = append(rec.reqs, capturedRequest{path: r.URL.Path, raw: raw})
		n := len(rec.reqs)
		rec.mu.Unlock()

		status, body := reply(n)
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(rec.srv.Close)
	return rec
}

func (rec *recorder) client() *Client {
	c := New("xoxc-test", "T04T4TH8W", rec.srv.Client())
	c.baseURL = rec.srv.URL
	return c
}

func (rec *recorder) requests() []capturedRequest {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	out := make([]capturedRequest, len(rec.reqs))
	copy(out, rec.reqs)
	return out
}

// alwaysEmpty is the unchanged-batch reply: the literal 24-byte body
// edgeapi returns when nothing in the batch has changed.
func alwaysEmpty(int) (int, string) { return 200, `{"results":[],"ok":true}` }

func ids(prefix string, n int) map[string]int64 {
	m := make(map[string]int64, n)
	for i := range n {
		m[fmt.Sprintf("%s%05d", prefix, i)] = int64(i)
	}
	return m
}

// sortedIDs is a sorted copy, for comparing id sets whose order is
// nondeterministic.
//
// Batch composition comes from ranging a Go map, so nothing may assert
// which ids landed in which batch or in what order they appear inside
// one. What is assertable is that a batch's id set and the set handed
// alongside its response are the same set — hence sorting both sides
// rather than pinning an order neither end controls.
func sortedIDs(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}

// requestIDs is the sorted id set the nth (1-based) captured request
// carried in updated_ids.
func requestIDs(t *testing.T, reqs []capturedRequest, n int) []string {
	t.Helper()
	if n < 1 || n > len(reqs) {
		t.Fatalf("asked for request %d but only %d were captured", n, len(reqs))
	}
	var out []string
	for id := range reqs[n-1].updatedIDs(t) {
		out = append(out, id)
	}
	slices.Sort(out)
	if len(out) == 0 {
		t.Fatalf("request %d carried no ids; an assertion against an empty batch proves nothing", n)
	}
	return out
}

// ---------------------------------------------------------------- channels

func TestChannelsInfo_SendsUpdatedIDsAndDecodesResults(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"results":[
			{"id":"C2QPK1V44","name":"general","updated":1783337533019,
			 "is_channel":true,"is_group":false,"is_im":false,"is_mpim":false,
			 "is_private":false,"is_archived":false,
			 "context_team_id":"T04T4TH8W",
			 "topic":{"creator":"U1","last_set":123,"value":"stand-ups here"}}
		],"member_channels":["C2QPK1V44"]}`
	})
	c := rec.client()

	got, err := c.ChannelsInfo(context.Background(), "T04T4TH8W", map[string]int64{
		"C2QPK1V44":   1783337533019,
		"C092E63RUUC": 0,
	})
	if err != nil {
		t.Fatalf("ChannelsInfo: %v", err)
	}

	reqs := rec.requests()
	if len(reqs) != 1 {
		t.Fatalf("made %d requests; want 1", len(reqs))
	}
	if reqs[0].path != "/cache/T04T4TH8W/channels/info" {
		t.Errorf("path = %q; want /cache/T04T4TH8W/channels/info", reqs[0].path)
	}

	// The captured request carries exactly three keys. An extra key is
	// as much a divergence from the official client as a missing one —
	// this is the whole point of the package.
	assertExactKeys(t, reqs, "token", "check_membership", "updated_ids")
	body := reqs[0].generic(t)
	if body["check_membership"] != true {
		t.Errorf("check_membership = %v; want true", body["check_membership"])
	}
	if _, ok := body["check_interaction"]; ok {
		t.Errorf("channels/info sent check_interaction; that flag belongs to users/info: %v", reqs[0].keys(t))
	}
	if body["token"] != "xoxc-test" {
		t.Errorf("token = %v; want xoxc-test", body["token"])
	}

	sent := reqs[0].updatedIDs(t)
	if len(sent) != 2 || sent["C2QPK1V44"] != 1783337533019 || sent["C092E63RUUC"] != 0 {
		t.Errorf("updated_ids = %v; want the {id: version} map verbatim, version 0 included", sent)
	}

	if len(got.Channels) != 1 {
		t.Fatalf("got %d channels; want 1", len(got.Channels))
	}
	ch := got.Channels[0]
	if ch.ID != "C2QPK1V44" {
		t.Errorf("ID = %q; want C2QPK1V44", ch.ID)
	}
	if ch.Name != "general" {
		t.Errorf("Name = %q; want general", ch.Name)
	}
	// The version stamp arrives as "updated", not "version". Getting
	// this wrong makes every channel look permanently stale and
	// reintroduces full enumeration.
	if ch.Version != 1783337533019 {
		t.Errorf("Version = %d; want 1783337533019 (from the `updated` field)", ch.Version)
	}
	// Asserted one field at a time, never as an `||` chain: a chain
	// cannot say which flag broke, and — because this fixture is a
	// plain public channel and so has five of the six booleans false —
	// it cannot tell "decoded false" from "never decoded". The flags
	// with a true value live in TestChannel_DecodesEachBooleanFlagIndependently.
	if !ch.IsChannel {
		t.Error("IsChannel = false; want true")
	}
	if ch.IsGroup {
		t.Error("IsGroup = true; want false (is_group is false in the fixture)")
	}
	if ch.IsIM {
		t.Error("IsIM = true; want false (is_im is false in the fixture)")
	}
	if ch.IsMPIM {
		t.Error("IsMPIM = true; want false (is_mpim is false in the fixture)")
	}
	if ch.IsPrivate {
		t.Error("IsPrivate = true; want false (is_private is false in the fixture)")
	}
	if ch.IsArchived {
		t.Error("IsArchived = true; want false (is_archived is false in the fixture)")
	}
	if ch.ContextTeam != "T04T4TH8W" {
		t.Errorf("ContextTeam = %q; want T04T4TH8W", ch.ContextTeam)
	}
	if ch.Topic.Value != "stand-ups here" {
		t.Errorf("Topic.Value = %q; want %q", ch.Topic.Value, "stand-ups here")
	}

	if len(got.MemberChannels) != 1 || got.MemberChannels[0] != "C2QPK1V44" {
		t.Errorf("MemberChannels = %v; want [C2QPK1V44] — this array is what "+
			"check_membership:true actually returns", got.MemberChannels)
	}
	if len(got.FailedIDs) != 0 {
		t.Errorf("FailedIDs = %v; want empty when the response omits failed_ids", got.FailedIDs)
	}
}

func TestChannelsInfo_ScopesThePathToTheGivenTeam(t *testing.T) {
	// On Enterprise Grid a user's conversations are owned by many
	// teams within the org, and the edge cache keys them under the
	// owning team. The team in the request path is therefore a
	// per-call decision, not a client property: scoping every request
	// to the auth.test team is what resolved zero of raff's 217
	// conversations (gammons/slk#5).
	rec := newRecorder(t, alwaysEmpty)
	c := rec.client() // constructed with team T04T4TH8W

	if _, err := c.ChannelsInfo(context.Background(), "T_OTHER_TEAM", map[string]int64{"C1": 0}); err != nil {
		t.Fatalf("ChannelsInfo: %v", err)
	}
	reqs := rec.requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d; want 1", len(reqs))
	}
	if want := "/cache/T_OTHER_TEAM/channels/info"; reqs[0].path != want {
		t.Errorf("path = %q; want %q — the call's team, not the client's construction team", reqs[0].path, want)
	}
}

// channelBooleanFlags is every boolean modelled on Channel, paired
// with the wire key it must decode from.
var channelBooleanFlags = []struct {
	name string
	key  string
	get  func(Channel) bool
}{
	{"IsChannel", "is_channel", func(c Channel) bool { return c.IsChannel }},
	{"IsGroup", "is_group", func(c Channel) bool { return c.IsGroup }},
	{"IsIM", "is_im", func(c Channel) bool { return c.IsIM }},
	{"IsMPIM", "is_mpim", func(c Channel) bool { return c.IsMPIM }},
	{"IsPrivate", "is_private", func(c Channel) bool { return c.IsPrivate }},
	{"IsArchived", "is_archived", func(c Channel) bool { return c.IsArchived }},
}

// TestChannel_DecodesEachBooleanFlagIndependently gives every boolean
// on Channel a fixture where it is true.
//
// This exists because the rest of the suite could not distinguish a
// decoded false from a field that was never decoded at all. Every
// other fixture is a plain public channel, so five of these six
// booleans are false everywhere, and a false-only fixture is satisfied
// just as well by `json:"-"` as by the right tag. It is also satisfied
// by any *permutation* of the tags, since all the permuted values are
// identical.
//
// That is not a theoretical gap. is_archived is what a caller filters
// on, and is_im/is_mpim/is_group is how a caller tells a DM from a
// channel — a swapped tag pair there files DMs into the channel list.
//
// The fixtures are one-hot and constructed rather than captured: the
// property being bought is discrimination, not realism. Exactly one
// flag true per fixture means every field is true somewhere (so a
// dropped tag reads false where the fixture says true) and every pair
// of fields disagrees somewhere (so any swapped pair is caught). Each
// field is then asserted on its own, never in an `||` chain, so a
// failure names the flag that broke.
func TestChannel_DecodesEachBooleanFlagIndependently(t *testing.T) {
	for _, on := range channelBooleanFlags {
		t.Run(on.name, func(t *testing.T) {
			fields := make([]string, 0, len(channelBooleanFlags))
			for _, f := range channelBooleanFlags {
				fields = append(fields, fmt.Sprintf("%q:%t", f.key, f.key == on.key))
			}
			rec := newRecorder(t, func(int) (int, string) {
				return 200, fmt.Sprintf(
					`{"ok":true,"results":[{"id":"C2QPK1V44","name":"x","updated":1,%s}]}`,
					strings.Join(fields, ","))
			})

			got, err := rec.client().ChannelsInfo(context.Background(), "T04T4TH8W", map[string]int64{"C2QPK1V44": 1})
			if err != nil {
				t.Fatalf("ChannelsInfo: %v", err)
			}
			if len(got.Channels) != 1 {
				t.Fatalf("got %d channels; want 1", len(got.Channels))
			}
			ch := got.Channels[0]

			for _, f := range channelBooleanFlags {
				want := f.key == on.key
				if got := f.get(ch); got != want {
					t.Errorf("with only %q true on the wire: Channel.%s = %t; want %t "+
						"(field is tagged json:%q)", on.key, f.name, got, want, f.key)
				}
			}
		})
	}
}

// userBooleanFlags is every boolean modelled on User, paired with the
// wire key it must decode from.
var userBooleanFlags = []struct {
	name string
	key  string
	get  func(User) bool
}{
	{"Deleted", "deleted", func(u User) bool { return u.Deleted }},
	{"IsBot", "is_bot", func(u User) bool { return u.IsBot }},
}

// TestUser_DecodesEachBooleanFlagIndependently is the users/info half
// of TestChannel_DecodesEachBooleanFlagIndependently, and the stakes
// are if anything higher: deleted is precisely what a caller filters
// on before rendering a member list, and swapping it with is_bot hides
// every deactivated account behind "it's a bot" and vice versa. Both
// are false in every other fixture, so nothing else here could tell
// the two tags apart or notice either going missing.
func TestUser_DecodesEachBooleanFlagIndependently(t *testing.T) {
	for _, on := range userBooleanFlags {
		t.Run(on.name, func(t *testing.T) {
			fields := make([]string, 0, len(userBooleanFlags))
			for _, f := range userBooleanFlags {
				fields = append(fields, fmt.Sprintf("%q:%t", f.key, f.key == on.key))
			}
			rec := newRecorder(t, func(int) (int, string) {
				return 200, fmt.Sprintf(
					`{"ok":true,"results":[{"id":"U04T4TH8Y","name":"grant","updated":1,%s}]}`,
					strings.Join(fields, ","))
			})

			got, err := rec.client().UsersInfo(context.Background(), map[string]int64{"U04T4TH8Y": 1})
			if err != nil {
				t.Fatalf("UsersInfo: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("got %d users; want 1", len(got))
			}

			for _, f := range userBooleanFlags {
				want := f.key == on.key
				if gotFlag := f.get(got[0]); gotFlag != want {
					t.Errorf("with only %q true on the wire: User.%s = %t; want %t "+
						"(field is tagged json:%q)", on.key, f.name, gotFlag, want, f.key)
				}
			}
		})
	}
}

// TestChannel_HasNoIsMemberField pins the finding that motivated
// removing it. Across 8 HAR captures — 18 channels/info requests, 7
// responses with results, 36 result objects — `is_member` appears 0
// times, while the other 43 fields appear 36/36. A struct field for it
// would decode false forever and read as "not a member" for every
// channel the user is in.
//
// If the server ever does start sending it, a field is still the wrong
// answer: membership is carried by member_channels, and two sources of
// truth for it would eventually disagree.
func TestChannel_HasNoIsMemberField(t *testing.T) {
	typ := reflect.TypeFor[Channel]()
	if _, ok := typ.FieldByName("IsMember"); ok {
		t.Error("Channel has an IsMember field; the captures show is_member on 0 of 36 " +
			"observed result objects — membership arrives in the top-level member_channels")
	}
	for i := range typ.NumField() {
		f := typ.Field(i)
		if f.Tag.Get("json") == "is_member" {
			t.Errorf("field %s is tagged json:%q; that key does not exist on a "+
				"channels/info result", f.Name, "is_member")
		}
	}
}

// TestChannelsInfo_MembershipArrivesWithNoResults is the common
// real-world shape, not an edge case: all 5 observed responses
// carrying member_channels had `"results":[]`. Membership comes back
// whether or not any channel record changed, which is exactly what
// lets us stop enumerating.
//
// (This comment previously said "5 of the 6", contradicting cache.go's
// "all 5" inside the same commit. Re-derived from the 8 raw captures:
// 18 channels/info responses, 5 carrying member_channels, all 5 of
// those with an empty results array. cache.go was right.)
func TestChannelsInfo_MembershipArrivesWithNoResults(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"results":[],"ok":true,"member_channels":["C2QPK1V44","CL0AET1L0"]}`
	})
	c := rec.client()

	got, err := c.ChannelsInfo(context.Background(), "T04T4TH8W", map[string]int64{
		"C2QPK1V44":   1,
		"CL0AET1L0":   2,
		"C092E63RUUC": 3,
	})
	if err != nil {
		t.Fatalf("ChannelsInfo: %v", err)
	}
	if len(got.Channels) != 0 {
		t.Errorf("Channels = %+v; want empty", got.Channels)
	}
	if len(got.MemberChannels) != 2 {
		t.Fatalf("MemberChannels = %v; want 2 ids — membership is returned even when "+
			"results is empty, and dropping it forces enumeration", got.MemberChannels)
	}
	seen := map[string]bool{}
	for _, id := range got.MemberChannels {
		seen[id] = true
	}
	if !seen["C2QPK1V44"] || !seen["CL0AET1L0"] {
		t.Errorf("MemberChannels = %v; want C2QPK1V44 and CL0AET1L0", got.MemberChannels)
	}
	// An id sent but absent from member_channels is a non-membership,
	// not missing data.
	if seen["C092E63RUUC"] {
		t.Errorf("MemberChannels = %v; C092E63RUUC was not in the response", got.MemberChannels)
	}
}

// TestChannelsInfo_SurfacesFailedIDs covers the correctness hazard: an
// id the server could not resolve is absent from results, exactly like
// an unchanged one. Without failed_ids the caller marks it fresh and
// keeps a stale record forever, because its version never advances.
func TestChannelsInfo_SurfacesFailedIDs(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"results":[],"ok":true,
			"member_channels":["C2QPK1V44"],
			"failed_ids":["C092E63RUUCX","C0B0QD6BH1N"]}`
	})
	c := rec.client()

	got, err := c.ChannelsInfo(context.Background(), "T04T4TH8W", map[string]int64{
		"C2QPK1V44":    1,
		"C092E63RUUCX": 2,
		"C0B0QD6BH1N":  3,
	})
	if err != nil {
		t.Fatalf("ChannelsInfo: %v", err)
	}
	// failed_ids is not an error: the rest of the batch succeeded.
	if len(got.FailedIDs) != 2 {
		t.Fatalf("FailedIDs = %v; want 2 — a failed id is indistinguishable from an "+
			"unchanged one unless it is surfaced", got.FailedIDs)
	}
	seen := map[string]bool{}
	for _, id := range got.FailedIDs {
		seen[id] = true
	}
	if !seen["C092E63RUUCX"] || !seen["C0B0QD6BH1N"] {
		t.Errorf("FailedIDs = %v; want C092E63RUUCX and C0B0QD6BH1N", got.FailedIDs)
	}
	if len(got.MemberChannels) != 1 || got.MemberChannels[0] != "C2QPK1V44" {
		t.Errorf("MemberChannels = %v; want [C2QPK1V44] alongside the failures",
			got.MemberChannels)
	}
}

// TestChannelsInfo_AbsentMembershipAndFailuresDecodeEmpty pins the
// dominant observed shape: member_channels appears in 5 of 18
// responses and failed_ids in 4 of 18, so absence is the norm and must
// mean empty, never an error.
func TestChannelsInfo_AbsentMembershipAndFailuresDecodeEmpty(t *testing.T) {
	rec := newRecorder(t, alwaysEmpty) // {"results":[],"ok":true}
	c := rec.client()

	got, err := c.ChannelsInfo(context.Background(), "T04T4TH8W", map[string]int64{"C2QPK1V44": 1})
	if err != nil {
		t.Fatalf("ChannelsInfo on a response with neither key: %v", err)
	}
	if len(got.MemberChannels) != 0 {
		t.Errorf("MemberChannels = %v; want empty when member_channels is absent",
			got.MemberChannels)
	}
	if len(got.FailedIDs) != 0 {
		t.Errorf("FailedIDs = %v; want empty when failed_ids is absent", got.FailedIDs)
	}
}

// TestChannelsInfo_AccumulatesMembershipAcrossBatches is the batching
// bug most likely to slip through: member_channels is per-batch, so
// assigning instead of appending silently keeps only the last batch
// and reports every channel in the earlier batches as a non-membership.
func TestChannelsInfo_AccumulatesMembershipAcrossBatches(t *testing.T) {
	// Three batches, each naming a distinct member channel and a
	// distinct failure.
	rec := newRecorder(t, func(n int) (int, string) {
		return 200, fmt.Sprintf(
			`{"ok":true,"results":[],"member_channels":["M%d"],"failed_ids":["F%d"]}`, n, n)
	})
	c := rec.client()

	got, err := c.ChannelsInfo(context.Background(), "T04T4TH8W", ids("C", channelsInfoBatchSize*2+10))
	if err != nil {
		t.Fatalf("ChannelsInfo: %v", err)
	}
	if n := len(rec.requests()); n != 3 {
		t.Fatalf("made %d requests; want 3", n)
	}

	member := map[string]bool{}
	for _, id := range got.MemberChannels {
		member[id] = true
	}
	for _, want := range []string{"M1", "M2", "M3"} {
		if !member[want] {
			t.Errorf("member channel %s missing from %v; membership from earlier batches "+
				"was overwritten instead of accumulated", want, got.MemberChannels)
		}
	}
	if len(got.MemberChannels) != 3 {
		t.Errorf("MemberChannels = %v; want exactly 3 ids, one per batch", got.MemberChannels)
	}

	failed := map[string]bool{}
	for _, id := range got.FailedIDs {
		failed[id] = true
	}
	for _, want := range []string{"F1", "F2", "F3"} {
		if !failed[want] {
			t.Errorf("failed id %s missing from %v; failures from earlier batches "+
				"were overwritten instead of accumulated", want, got.FailedIDs)
		}
	}
	if len(got.FailedIDs) != 3 {
		t.Errorf("FailedIDs = %v; want exactly 3 ids, one per batch", got.FailedIDs)
	}
}

// TestChannelsInfo_MembershipQueriedCoversOnlyBatchesThatReported is
// the bug MembershipQueried exists to make impossible.
//
// member_channels is absent from 13 of the 18 observed channels/info
// responses, every one of which asked for it — so a call whose batches
// disagree is the expected case, not a corner. Accumulating across
// them leaves a MemberChannels that is non-empty, and therefore looks
// authoritative, while holding no answer at all for the silent batch's
// ids. A caller applying that against the full queried set clears
// membership for every one of them.
//
// So the result has to say which ids it actually covers. Batch 2 here
// stays silent and its ids must be absent from MembershipQueried even
// though batches 1 and 3 reported.
func TestChannelsInfo_MembershipQueriedCoversOnlyBatchesThatReported(t *testing.T) {
	rec := newRecorder(t, func(n int) (int, string) {
		if n == 2 {
			return 200, `{"ok":true,"results":[]}`
		}
		return 200, fmt.Sprintf(`{"ok":true,"results":[],"member_channels":["M%d"]}`, n)
	})
	c := rec.client()

	got, err := c.ChannelsInfo(context.Background(), "T04T4TH8W", ids("C", channelsInfoBatchSize*2+10))
	if err != nil {
		t.Fatalf("ChannelsInfo: %v", err)
	}
	reqs := rec.requests()
	if len(reqs) != 3 {
		t.Fatalf("made %d requests; want 3", len(reqs))
	}

	// The reporting batches, and only those.
	want := append(requestIDs(t, reqs, 1), requestIDs(t, reqs, 3)...)
	slices.Sort(want)
	if gotQueried := sortedIDs(got.MembershipQueried); !slices.Equal(gotQueried, want) {
		t.Errorf("MembershipQueried covers %d ids; want the %d ids sent in batches 1 and 3\n"+
			" got: %v\nwant: %v", len(gotQueried), len(want), gotQueried, want)
	}
	// Spelled out separately from the set equality above, because
	// this is the specific failure that clears a user out of channels
	// they are still in.
	for _, id := range requestIDs(t, reqs, 2) {
		if slices.Contains(got.MembershipQueried, id) {
			t.Fatalf("MembershipQueried contains %s, which was sent in batch 2 — the batch "+
				"whose response carried no member_channels. Membership is unknown for that "+
				"id, and claiming otherwise clears it.", id)
		}
	}
	// And the membership that *was* reported still accumulates.
	if want := []string{"M1", "M3"}; !slices.Equal(got.MemberChannels, want) {
		t.Errorf("MemberChannels = %v; want %v", got.MemberChannels, want)
	}
}

// TestChannelsInfo_SurfacesFailedIDsWithoutMemberChannels keeps the
// two top-level keys independent.
//
// They are independent on the wire — failed_ids appears in 4 of 18
// observed responses and member_channels in 5, and nothing ties the
// two — but the membership-presence check is an early return in the
// merge closure, which puts every other accumulator at risk of being
// stranded behind it. This is not hypothetical: writing that closure
// with the FailedIDs append below the guard passes the entire rest of
// this file, because every other fixture that carries failed_ids also
// carries member_channels.
//
// The consequence is the exact hazard FailedIDs exists to prevent. An
// unreported failure is indistinguishable from an unchanged channel,
// so its stale record is marked fresh and never revalidated again.
func TestChannelsInfo_SurfacesFailedIDsWithoutMemberChannels(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"results":[],"failed_ids":["C092E63RUUCX","C0B0QD6BH1N"]}`
	})
	c := rec.client()

	got, err := c.ChannelsInfo(context.Background(), "T04T4TH8W", map[string]int64{
		"C092E63RUUCX": 44,
		"C0B0QD6BH1N":  55,
	})
	if err != nil {
		t.Fatalf("ChannelsInfo: %v", err)
	}
	if want := []string{"C092E63RUUCX", "C0B0QD6BH1N"}; !slices.Equal(got.FailedIDs, want) {
		t.Errorf("FailedIDs = %v; want %v. This response carried no member_channels, which "+
			"says nothing about whether it carried failures — a failed id dropped here is "+
			"marked fresh and its stale record kept forever", got.FailedIDs, want)
	}
	// The membership half is still unreported, which is what makes
	// this a distinct case rather than a copy of the test above.
	if len(got.MembershipQueried) != 0 {
		t.Errorf("MembershipQueried = %v; want empty", got.MembershipQueried)
	}
}

// TestChannelsInfo_AbsentMemberChannelsCoversNoIDs pins the "no
// information" half of the presence contract. The dominant shape —
// 13 of 18 — is a response with no member_channels key at all, and a
// call that only saw those has learned nothing about membership for
// any id it sent.
func TestChannelsInfo_AbsentMemberChannelsCoversNoIDs(t *testing.T) {
	rec := newRecorder(t, alwaysEmpty) // {"results":[],"ok":true}
	c := rec.client()

	got, err := c.ChannelsInfo(context.Background(), "T04T4TH8W", map[string]int64{
		"C2QPK1V44":   11,
		"CL0AET1L0":   22,
		"C092E63RUUC": 33,
	})
	if err != nil {
		t.Fatalf("ChannelsInfo: %v", err)
	}
	if len(got.MembershipQueried) != 0 {
		t.Errorf("MembershipQueried = %v; want empty. member_channels was absent, so the "+
			"response answered nothing about membership and covering these ids would "+
			"turn silence into 'not a member'", got.MembershipQueried)
	}
}

// TestChannelsInfo_ExplicitlyEmptyMemberChannelsCoversTheWholeBatch is
// the other half, and the reason this needs more than a len() check on
// the wire.
//
// encoding/json decodes an absent key and a literal [] to the same nil
// slice, so a plain []string cannot tell "the server said nothing"
// from "the server looked and named nobody". Those are opposite
// answers: the first must preserve every id's membership, the second
// must clear it. The batch was still covered.
func TestChannelsInfo_ExplicitlyEmptyMemberChannelsCoversTheWholeBatch(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"results":[],"member_channels":[]}`
	})
	c := rec.client()

	queried := map[string]int64{
		"C2QPK1V44":   11,
		"CL0AET1L0":   22,
		"C092E63RUUC": 33,
	}
	got, err := c.ChannelsInfo(context.Background(), "T04T4TH8W", queried)
	if err != nil {
		t.Fatalf("ChannelsInfo: %v", err)
	}
	if len(got.MemberChannels) != 0 {
		t.Errorf("MemberChannels = %v; want empty — the response named nobody",
			got.MemberChannels)
	}
	var want []string
	for id := range queried {
		want = append(want, id)
	}
	slices.Sort(want)
	if gotQueried := sortedIDs(got.MembershipQueried); !slices.Equal(gotQueried, want) {
		t.Errorf("MembershipQueried = %v; want %v. member_channels was present and empty, "+
			"which is a real answer — every queried id is a confirmed non-member — and "+
			"reading it as 'absent' throws that answer away", gotQueried, want)
	}
}

// TestChannelsInfo_MembershipQueriedHoldsIDsSentNotIDsReturned pins
// which of the two id sets in play this field carries. It records
// coverage — what the request asked about — not the answer. Filling it
// from member_channels would make it a duplicate of that field and
// leave the non-members and the failures uncovered, which is precisely
// the information a caller needs in order to clear anything.
func TestChannelsInfo_MembershipQueriedHoldsIDsSentNotIDsReturned(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"results":[],
			"member_channels":["CL0AET1L0"],
			"failed_ids":["C092E63RUUC"]}`
	})
	c := rec.client()

	got, err := c.ChannelsInfo(context.Background(), "T04T4TH8W", map[string]int64{
		"C2QPK1V44":   11, // reported on, named nowhere: a non-member
		"CL0AET1L0":   22, // a member
		"C092E63RUUC": 33, // a failure
	})
	if err != nil {
		t.Fatalf("ChannelsInfo: %v", err)
	}
	want := []string{"C092E63RUUC", "CL0AET1L0", "C2QPK1V44"}
	slices.Sort(want)
	if gotQueried := sortedIDs(got.MembershipQueried); !slices.Equal(gotQueried, want) {
		t.Errorf("MembershipQueried = %v; want all %v — the ids the batch SENT, including "+
			"the non-member and the failed lookup, not the ids member_channels returned",
			gotQueried, want)
	}
}

// TestChannelsInfo_AllAccumulatorsPreserveRequestOrder pins ordering
// for every accumulator on the channels path, not just one of them.
//
// Ordering was previously pinned for MemberChannels alone, which made
// it ambiguous whether request order was a contract or an accident:
// the same reversal applied to Channels or FailedIDs went unnoticed.
// Resolved in favour of "it is a contract, for all of them". The
// merge closure appends, appending is order-preserving, and a
// prepend/reverse/keep-only-first bug passes every set-equality
// assertion in this file — so it is worth a line each.
//
// Note what is *not* claimed: which ids ride in which batch. fetchInfo
// ranges a Go map, so batch composition is deliberately
// nondeterministic and nothing here depends on it. The contract is
// only that batch N's contribution precedes batch N+1's, which is
// well defined however the ids were partitioned — the recorder keys
// its reply off the request number, not off the ids.
func TestChannelsInfo_AllAccumulatorsPreserveRequestOrder(t *testing.T) {
	rec := newRecorder(t, func(n int) (int, string) {
		return 200, fmt.Sprintf(
			`{"ok":true,"results":[{"id":"C%d","updated":%d}],"member_channels":["M%d"],"failed_ids":["F%d"]}`,
			n, n, n, n)
	})
	c := rec.client()

	got, err := c.ChannelsInfo(context.Background(), "T04T4TH8W", ids("C", channelsInfoBatchSize*2+10))
	if err != nil {
		t.Fatalf("ChannelsInfo: %v", err)
	}

	var gotChannels []string
	for _, ch := range got.Channels {
		gotChannels = append(gotChannels, ch.ID)
	}
	if want := []string{"C1", "C2", "C3"}; !slices.Equal(gotChannels, want) {
		t.Errorf("Channels = %v; want %v in request order", gotChannels, want)
	}
	if want := []string{"M1", "M2", "M3"}; !slices.Equal(got.MemberChannels, want) {
		t.Errorf("MemberChannels = %v; want %v in request order", got.MemberChannels, want)
	}
	if want := []string{"F1", "F2", "F3"}; !slices.Equal(got.FailedIDs, want) {
		t.Errorf("FailedIDs = %v; want %v in request order", got.FailedIDs, want)
	}
}

// TestUsersInfo_ResultsPreserveRequestOrder is the users/info half of
// TestChannelsInfo_AllAccumulatorsPreserveRequestOrder. Same contract,
// same reason: order was pinned on the channels path only, and an
// asymmetry like that reads as an oversight rather than a decision.
func TestUsersInfo_ResultsPreserveRequestOrder(t *testing.T) {
	rec := newRecorder(t, func(n int) (int, string) {
		return 200, fmt.Sprintf(`{"ok":true,"results":[{"id":"U%d","updated":%d}]}`, n, n)
	})
	c := rec.client()

	got, err := c.UsersInfo(context.Background(), ids("U", usersInfoBatchSize*2+10))
	if err != nil {
		t.Fatalf("UsersInfo: %v", err)
	}
	var gotIDs []string
	for _, u := range got {
		gotIDs = append(gotIDs, u.ID)
	}
	if want := []string{"U1", "U2", "U3"}; !slices.Equal(gotIDs, want) {
		t.Errorf("users = %v; want %v in request order", gotIDs, want)
	}
}

func TestChannelsInfo_EmptyResultsMeansNothingChanged(t *testing.T) {
	rec := newRecorder(t, alwaysEmpty)
	c := rec.client()

	got, err := c.ChannelsInfo(context.Background(), "T04T4TH8W", map[string]int64{"CL0AET1L0": 1783337533019})
	if err != nil {
		t.Fatalf("ChannelsInfo: %v", err)
	}
	if len(got.Channels) != 0 {
		t.Errorf("got %d channels; want 0 — an empty results array means nothing changed, not an error", len(got.Channels))
	}
	// The request still has to happen: this is the revalidation.
	if n := len(rec.requests()); n != 1 {
		t.Errorf("made %d requests; want 1", n)
	}
}

func TestChannelsInfo_NoIDsMakesNoRequest(t *testing.T) {
	rec := newRecorder(t, alwaysEmpty)
	c := rec.client()

	for _, in := range []map[string]int64{nil, {}} {
		got, err := c.ChannelsInfo(context.Background(), "T04T4TH8W", in)
		if err != nil {
			t.Fatalf("ChannelsInfo(%v): %v", in, err)
		}
		if len(got.Channels) != 0 || len(got.MemberChannels) != 0 || len(got.FailedIDs) != 0 {
			t.Errorf("ChannelsInfo(%v) returned %+v; want a zero result", in, got)
		}
	}
	if n := len(rec.requests()); n != 0 {
		t.Errorf("made %d requests for an empty id set; want 0 — an empty updated_ids "+
			"map is a pointless round trip and, worse, looks like a probe", n)
	}
}

func TestChannelsInfo_SplitsLargeIDSets(t *testing.T) {
	rec := newRecorder(t, alwaysEmpty)
	c := rec.client()

	const total = channelsInfoBatchSize*2 + 10
	want := ids("C", total)
	if _, err := c.ChannelsInfo(context.Background(), "T04T4TH8W", want); err != nil {
		t.Fatalf("ChannelsInfo: %v", err)
	}

	reqs := rec.requests()
	if len(reqs) != 3 {
		t.Fatalf("made %d requests for %d ids; want 3 (%d+%d+10)",
			len(reqs), total, channelsInfoBatchSize, channelsInfoBatchSize)
	}

	// Every batch, not just the first. The single-request tests pin
	// the key set for request 1 of a call and say nothing about 2..N,
	// which leaves the package's central claim — we send exactly the
	// keys the official client sends — unverified for every request
	// after the first in a multi-batch revalidation.
	assertExactKeys(t, reqs, "token", "check_membership", "updated_ids")

	seen := map[string]int64{}
	for i, r := range reqs {
		batch := r.updatedIDs(t)
		if len(batch) > channelsInfoBatchSize {
			t.Errorf("request %d carried %d ids; want at most %d", i, len(batch), channelsInfoBatchSize)
		}
		if len(batch) == 0 {
			t.Errorf("request %d carried no ids; an empty batch should never be sent", i)
		}
		for id, v := range batch {
			if _, dup := seen[id]; dup {
				t.Errorf("id %s sent in more than one batch; batches must not overlap", id)
			}
			seen[id] = v
		}
	}
	// Catches both a dropped final partial batch and a batch map
	// reused across flushes.
	if len(seen) != total {
		t.Errorf("sent %d distinct ids across all batches; want all %d", len(seen), total)
	}
	for id, v := range want {
		if got, ok := seen[id]; !ok || got != v {
			t.Errorf("id %s: sent %d (present=%v); want %d", id, got, ok, v)
		}
	}
}

func TestChannelsInfo_ExactMultipleOfBatchSizeSendsNoEmptyBatch(t *testing.T) {
	rec := newRecorder(t, alwaysEmpty)
	c := rec.client()

	if _, err := c.ChannelsInfo(context.Background(), "T04T4TH8W", ids("C", channelsInfoBatchSize)); err != nil {
		t.Fatalf("ChannelsInfo: %v", err)
	}
	if n := len(rec.requests()); n != 1 {
		t.Errorf("made %d requests for exactly %d ids; want 1 — a trailing empty batch "+
			"is a wasted round trip", n, channelsInfoBatchSize)
	}
}

func TestChannelsInfo_ReturnsResultsFromEveryBatch(t *testing.T) {
	// Each batch answers with one distinct channel. Anything that
	// returns only the first batch's results, or overwrites instead of
	// appending, loses rows here.
	rec := newRecorder(t, func(n int) (int, string) {
		return 200, fmt.Sprintf(`{"ok":true,"results":[{"id":"C%d","name":"batch%d","updated":%d}]}`, n, n, n)
	})
	c := rec.client()

	got, err := c.ChannelsInfo(context.Background(), "T04T4TH8W", ids("C", channelsInfoBatchSize*2+10))
	if err != nil {
		t.Fatalf("ChannelsInfo: %v", err)
	}
	if len(got.Channels) != 3 {
		t.Fatalf("got %d channels from 3 batches; want 3 — results from later batches were dropped: %+v", len(got.Channels), got.Channels)
	}
	seen := map[string]bool{}
	for _, ch := range got.Channels {
		seen[ch.ID] = true
	}
	for _, want := range []string{"C1", "C2", "C3"} {
		if !seen[want] {
			t.Errorf("channel %s missing from the merged result: %+v", want, got.Channels)
		}
	}
}

func TestChannelsInfo_IgnoresUnknownResponseFields(t *testing.T) {
	// Every field observed on a real channels/info result, plus a
	// top-level extra. Slack adds fields without notice; a decode that
	// rejected them would break the client in production, not in CI.
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"some_future_top_level_key":{"a":1},"results":[{
			"id":"C2QPK1V44","enterprise_id":"","context_team_id":"T04T4TH8W",
			"internal_team_ids":[],"pending_connected_team_ids":[],"pending_shared":[],
			"shared_team_ids":["T04T4TH8W"],"connected_limited_team_ids":[],
			"connected_team_ids":[],"conversation_host_id":"","creator":"U04T4TH8Y",
			"name":"general","name_normalized":"general","previous_names":[],
			"created":1668181000,"unlinked":0,"updated":1783337533019,
			"is_archived":false,"is_channel":true,"is_frozen":false,"is_general":true,
			"is_group":false,"is_im":false,"is_moved":0,"is_mpim":false,
			"is_org_default":false,"is_org_mandatory":false,"is_record_channel":false,
			"is_file":false,"is_shared":false,"is_ext_shared":false,"is_org_shared":false,
			"is_pending_ext_shared":false,"is_private":false,"is_global_shared":false,
			"parent_conversation":"",
			"purpose":{"creator":"U1","last_set":1,"value":"p"},
			"topic":{"creator":"U1","last_set":1,"value":"t"},
			"properties":{"canvas":{"file_id":"F1"},"tabs":[{"id":"x"}]},
			"frozen_reason":"","is_ext_ws_shared":false,"use_case":"",
			"channel_agent_status":"","a_field_slack_ships_next_week":42
		}]}`
	})
	c := rec.client()

	got, err := c.ChannelsInfo(context.Background(), "T04T4TH8W", map[string]int64{"C2QPK1V44": 1})
	if err != nil {
		t.Fatalf("ChannelsInfo on a full real-shaped response: %v", err)
	}
	if len(got.Channels) != 1 {
		t.Fatalf("got %d channels; want 1", len(got.Channels))
	}
	if got.Channels[0].ID != "C2QPK1V44" || got.Channels[0].Version != 1783337533019 ||
		got.Channels[0].Topic.Value != "t" {
		t.Errorf("modelled fields did not survive the unmodelled ones: %+v", got.Channels[0])
	}
}

func TestChannelsInfo_PropagatesAPIError(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":false,"error":"invalid_auth"}`
	})
	c := rec.client()

	got, err := c.ChannelsInfo(context.Background(), "T04T4TH8W", map[string]int64{"C1": 1})
	if err == nil {
		t.Fatal("ChannelsInfo returned nil error on ok:false")
	}
	if !strings.Contains(err.Error(), "invalid_auth") {
		t.Errorf("error = %v; want it to mention invalid_auth", err)
	}
	if got.Channels != nil || got.MemberChannels != nil || got.FailedIDs != nil ||
		got.MembershipQueried != nil {
		t.Errorf("got = %+v; want a zero result alongside an error", got)
	}
}

func TestChannelsInfo_MidBatchErrorAbortsAndDiscardsPartialResults(t *testing.T) {
	// Chosen behaviour: a failed batch fails the whole call and no
	// partial results are returned. The alternative — returning what
	// succeeded with a nil error — is indistinguishable from "only
	// these changed", so the caller would mark the unfetched ids
	// current and never revalidate them again.
	rec := newRecorder(t, func(n int) (int, string) {
		if n == 2 {
			return 200, `{"ok":false,"error":"ratelimited"}`
		}
		return 200, fmt.Sprintf(
			`{"ok":true,"results":[{"id":"C%d","updated":%d}],"member_channels":["M%d"],"failed_ids":["F%d"]}`,
			n, n, n, n)
	})
	c := rec.client()

	got, err := c.ChannelsInfo(context.Background(), "T04T4TH8W", ids("C", channelsInfoBatchSize*2+10))
	if err == nil {
		t.Fatal("ChannelsInfo returned nil error when the second batch failed")
	}
	if !strings.Contains(err.Error(), "ratelimited") {
		t.Errorf("error = %v; want it to mention ratelimited", err)
	}
	// Membership and failures accumulated by the first batch must be
	// discarded too: a partial membership snapshot read as a complete
	// one turns every unqueried channel into a non-membership.
	if got.Channels != nil || got.MemberChannels != nil || got.FailedIDs != nil ||
		got.MembershipQueried != nil {
		t.Errorf("got = %+v; want a zero result — partial results would look like "+
			"'only these changed' and strand the unfetched ids", got)
	}
	if n := len(rec.requests()); n != 2 {
		t.Errorf("made %d requests; want 2 — the third batch should not be attempted "+
			"after the second failed", n)
	}
}

// ------------------------------------------------------------------- users

func TestUsersInfo_SendsExpectedFlags(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"results":[
			{"id":"U04T4TH8Y","team_id":"T04T4TH8W","name":"grant","deleted":false,
			 "is_bot":false,"updated":1612802061,
			 "profile":{"display_name":"Grant","real_name":"Grant Ammons","avatar_hash":"g1a2b3"}}
		],"can_interact":{"U04T4TH8Y":true}}`
	})
	c := rec.client()

	got, err := c.UsersInfo(context.Background(), map[string]int64{
		"U04T4TH8Y":   1612802061,
		"U0B0QD6BH1N": 0,
	})
	if err != nil {
		t.Fatalf("UsersInfo: %v", err)
	}

	reqs := rec.requests()
	if len(reqs) != 1 {
		t.Fatalf("made %d requests; want 1", len(reqs))
	}
	if reqs[0].path != "/cache/T04T4TH8W/users/info" {
		t.Errorf("path = %q; want /cache/T04T4TH8W/users/info", reqs[0].path)
	}

	assertExactKeys(t, reqs,
		"token", "check_interaction", "include_profile_only_users", "updated_ids")
	body := reqs[0].generic(t)
	if body["check_interaction"] != true {
		t.Errorf("check_interaction = %v; want true", body["check_interaction"])
	}
	if body["include_profile_only_users"] != true {
		t.Errorf("include_profile_only_users = %v; want true", body["include_profile_only_users"])
	}
	if _, ok := body["check_membership"]; ok {
		t.Errorf("users/info sent check_membership; that flag belongs to channels/info: %v", reqs[0].keys(t))
	}

	sent := reqs[0].updatedIDs(t)
	if len(sent) != 2 || sent["U04T4TH8Y"] != 1612802061 || sent["U0B0QD6BH1N"] != 0 {
		t.Errorf("updated_ids = %v; want the {id: version} map verbatim", sent)
	}

	if len(got) != 1 {
		t.Fatalf("got %d users; want 1", len(got))
	}
	u := got[0]
	if u.ID != "U04T4TH8Y" {
		t.Errorf("ID = %q; want U04T4TH8Y", u.ID)
	}
	if u.Name != "grant" {
		t.Errorf("Name = %q; want grant", u.Name)
	}
	if u.TeamID != "T04T4TH8W" {
		t.Errorf("TeamID = %q; want T04T4TH8W", u.TeamID)
	}
	// One field at a time, for the reason spelled out in
	// TestUser_DecodesEachBooleanFlagIndependently: this fixture is a
	// live human, so both booleans are false and neither a dropped tag
	// nor a swapped pair could change the outcome here. The true-valued
	// cases are over there.
	if u.Deleted {
		t.Error("Deleted = true; want false (deleted is false in the fixture)")
	}
	if u.IsBot {
		t.Error("IsBot = true; want false (is_bot is false in the fixture)")
	}
	// users/info stamps `updated` in whole seconds, channels/info in
	// milliseconds. Both are just opaque version stamps to us, but
	// they come from the same field name.
	if u.Version != 1612802061 {
		t.Errorf("Version = %d; want 1612802061 (from the `updated` field)", u.Version)
	}
	if u.Profile.DisplayName != "Grant" {
		t.Errorf("Profile.DisplayName = %q; want Grant", u.Profile.DisplayName)
	}
	if u.Profile.RealName != "Grant Ammons" {
		t.Errorf("Profile.RealName = %q; want Grant Ammons", u.Profile.RealName)
	}
}

// TestUsersInfo_DecodesProfileAvatar pins the correction to a claim
// Phase 2a made and got wrong.
//
// That version of edge.User asserted a users/info profile carries no
// image URL at all, and modelled none. Measured across all 291
// users/info result objects in the 8 captures:
//
//	profile.avatar_hash      288/291
//	profile.image_original   255/291   (non-empty in all 255)
//	profile.is_custom_image  255/291
//	profile.image_32           0/291
//	profile.image_72           0/291
//	profile.image_192          0/291
//
// Dropping the sized image_NN variants was right — a field for one
// would decode empty forever and hand callers a blank avatar. "No
// image anywhere" was not: image_original is an absolute URL on 88% of
// results.
//
// The wrong claim came from the committed fixture rather than the
// captures. internal/slack/testdata/phase2-api-contracts.json keeps
// samples[:3]; two of the three users/info samples were `results: []`,
// and the one that remained held a user with no custom image. One user
// was generalised into a contract.
//
// It matters because cache.User has an avatar_url column: without this
// field a revalidation pass has nothing to write there.
//
// Every asserted field carries a distinct, non-zero value on purpose —
// including is_custom_image, which is the only bool in Profile and
// would otherwise be indistinguishable from the two on User.
func TestUsersInfo_DecodesProfileAvatar(t *testing.T) {
	const wantURL = "https://avatars.slack-edge.com/2021-02-08/1783337533019_g1a2b3_original.png"
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"results":[
			{"id":"U04T4TH8Y","team_id":"T04T4TH8W","name":"grant","updated":1612802061,
			 "deleted":true,"is_bot":false,
			 "profile":{"display_name":"Grant","real_name":"Grant Ammons",
			  "avatar_hash":"g1a2b3","is_custom_image":true,
			  "image_original":"` + wantURL + `"}}
		],"can_interact":{"U04T4TH8Y":true}}`
	})

	got, err := rec.client().UsersInfo(context.Background(), map[string]int64{"U04T4TH8Y": 1612802061})
	if err != nil {
		t.Fatalf("UsersInfo: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d users; want 1", len(got))
	}
	u := got[0]

	if u.Profile.ImageOriginal != wantURL {
		t.Errorf("Profile.ImageOriginal = %q; want %q. This is the avatar URL a users/info "+
			"profile actually carries — 255 of 291 observed results — and cache.User's "+
			"avatar_url column has nothing to write without it",
			u.Profile.ImageOriginal, wantURL)
	}
	if !u.Profile.IsCustomImage {
		t.Error("Profile.IsCustomImage = false; want true (is_custom_image is true in the fixture)")
	}
	// The two User-level bools are asserted alongside it so a tag
	// swapped between them and IsCustomImage cannot go unnoticed.
	// deleted is true and is_bot is false here, so a swap in either
	// direction changes an answer.
	if !u.Deleted {
		t.Error("Deleted = true expected; got false (deleted is true in the fixture)")
	}
	if u.IsBot {
		t.Error("IsBot = true; want false (is_bot is false in the fixture)")
	}
	// The neighbouring profile strings, so a tag pointed at the wrong
	// key inside Profile shows up here rather than silently.
	if u.Profile.DisplayName != "Grant" {
		t.Errorf("Profile.DisplayName = %q; want Grant", u.Profile.DisplayName)
	}
	if u.Profile.RealName != "Grant Ammons" {
		t.Errorf("Profile.RealName = %q; want Grant Ammons", u.Profile.RealName)
	}
}

// TestUsersInfo_ProfileWithoutAnImageDecodesEmpty is the other 36 of
// 291 — users who have never set a custom avatar. Neither key is
// present on those profiles, and their absence must decode to the zero
// value rather than failing the batch.
//
// The empty string therefore means "this user has no custom avatar",
// never "this endpoint cannot tell you". That distinction is what
// makes it safe for internal/cache's UpdateUserFromEdge to treat an
// empty AvatarURL as "preserve".
func TestUsersInfo_ProfileWithoutAnImageDecodesEmpty(t *testing.T) {
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"results":[
			{"id":"U0B6SR2FLG1","team_id":"T04T4TH8W","name":"nova","updated":1612802062,
			 "profile":{"display_name":"Nova","real_name":"Nova Prime","avatar_hash":"h9z8y7"}}
		]}`
	})

	got, err := rec.client().UsersInfo(context.Background(), map[string]int64{"U0B6SR2FLG1": 1})
	if err != nil {
		t.Fatalf("UsersInfo on a profile with no image keys: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d users; want 1 — a missing image_original must not drop the result", len(got))
	}
	if got[0].Profile.ImageOriginal != "" {
		t.Errorf("Profile.ImageOriginal = %q; want empty", got[0].Profile.ImageOriginal)
	}
	if got[0].Profile.IsCustomImage {
		t.Error("Profile.IsCustomImage = true; want false when the key is absent")
	}
	// The rest of the profile still has to survive.
	if got[0].Profile.RealName != "Nova Prime" {
		t.Errorf("Profile.RealName = %q; want Nova Prime", got[0].Profile.RealName)
	}
}

func TestUsersInfo_NoIDsMakesNoRequest(t *testing.T) {
	rec := newRecorder(t, alwaysEmpty)
	c := rec.client()

	for _, in := range []map[string]int64{nil, {}} {
		got, err := c.UsersInfo(context.Background(), in)
		if err != nil {
			t.Fatalf("UsersInfo(%v): %v", in, err)
		}
		if len(got) != 0 {
			t.Errorf("UsersInfo(%v) returned %d users; want 0", in, len(got))
		}
	}
	if n := len(rec.requests()); n != 0 {
		t.Errorf("made %d requests for an empty id set; want 0", n)
	}
}

func TestUsersInfo_EmptyResultsMeansNothingChanged(t *testing.T) {
	rec := newRecorder(t, alwaysEmpty)
	c := rec.client()

	got, err := c.UsersInfo(context.Background(), map[string]int64{"U04T4TH8Y": 1612802061})
	if err != nil {
		t.Fatalf("UsersInfo: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d users; want 0", len(got))
	}
	if n := len(rec.requests()); n != 1 {
		t.Errorf("made %d requests; want 1", n)
	}
}

func TestUsersInfo_SplitsLargeIDSets(t *testing.T) {
	rec := newRecorder(t, alwaysEmpty)
	c := rec.client()

	const total = usersInfoBatchSize*2 + 10
	want := ids("U", total)
	if _, err := c.UsersInfo(context.Background(), want); err != nil {
		t.Fatalf("UsersInfo: %v", err)
	}

	reqs := rec.requests()
	if len(reqs) != 3 {
		t.Fatalf("made %d requests for %d ids; want 3", len(reqs), total)
	}
	// Every batch, not just the first — see the same assertion in
	// TestChannelsInfo_SplitsLargeIDSets.
	assertExactKeys(t, reqs,
		"token", "check_interaction", "include_profile_only_users", "updated_ids")

	seen := map[string]int64{}
	for i, r := range reqs {
		batch := r.updatedIDs(t)
		if len(batch) > usersInfoBatchSize {
			t.Errorf("request %d carried %d ids; want at most %d", i, len(batch), usersInfoBatchSize)
		}
		if len(batch) == 0 {
			t.Errorf("request %d carried no ids; an empty batch should never be sent", i)
		}
		for id, v := range batch {
			if _, dup := seen[id]; dup {
				t.Errorf("id %s sent in more than one batch", id)
			}
			seen[id] = v
		}
	}
	if len(seen) != total {
		t.Errorf("sent %d distinct ids across all batches; want all %d", len(seen), total)
	}
	for id, v := range want {
		if got, ok := seen[id]; !ok || got != v {
			t.Errorf("id %s: sent %d (present=%v); want %d", id, got, ok, v)
		}
	}
}

func TestUsersInfo_ReturnsResultsFromEveryBatch(t *testing.T) {
	rec := newRecorder(t, func(n int) (int, string) {
		return 200, fmt.Sprintf(`{"ok":true,"results":[{"id":"U%d","name":"batch%d","updated":%d}]}`, n, n, n)
	})
	c := rec.client()

	got, err := c.UsersInfo(context.Background(), ids("U", usersInfoBatchSize*2+10))
	if err != nil {
		t.Fatalf("UsersInfo: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d users from 3 batches; want 3: %+v", len(got), got)
	}
}

func TestUsersInfo_MidBatchErrorAbortsAndDiscardsPartialResults(t *testing.T) {
	rec := newRecorder(t, func(n int) (int, string) {
		if n == 2 {
			return 500, `{"ok":true,"results":[]}`
		}
		return 200, fmt.Sprintf(`{"ok":true,"results":[{"id":"U%d","updated":%d}]}`, n, n)
	})
	c := rec.client()

	got, err := c.UsersInfo(context.Background(), ids("U", usersInfoBatchSize*2+10))
	if err == nil {
		t.Fatal("UsersInfo returned nil error when the second batch got HTTP 500")
	}
	if got != nil {
		t.Errorf("got = %+v; want nil results alongside an error", got)
	}
	if n := len(rec.requests()); n != 2 {
		t.Errorf("made %d requests; want 2 — later batches should not be attempted", n)
	}
}

func TestUsersInfo_IgnoresUnknownResponseFieldsIncludingCanInteract(t *testing.T) {
	// can_interact is a real top-level key in every users/info
	// response (it is what check_interaction:true buys) and nothing in
	// this package models it. Neither it nor any unmodelled per-user
	// field may break the decode.
	rec := newRecorder(t, func(int) (int, string) {
		return 200, `{"ok":true,"results":[{
			"id":"U04T4TH8Y","team_id":"T04T4TH8W","name":"grant","deleted":false,
			"color":"9f69e7","real_name":"Grant Ammons","tz":"America/New_York",
			"tz_label":"Eastern Standard Time","tz_offset":-18000,
			"profile":{"title":"","phone":"","skype":"","real_name":"Grant Ammons",
			  "real_name_normalized":"Grant Ammons","display_name":"Grant",
			  "display_name_normalized":"Grant","fields":null,"status_text":"",
			  "status_emoji":"","status_emoji_display_info":[],"status_expiration":0,
			  "status_clear_on_focus_end":false,"avatar_hash":"g1a2b3","start_date":"",
			  "huddle_state":"default_unset","first_name":"Grant","last_name":"Ammons",
			  "status_text_canonical":"","team":"T04T4TH8W"},
			"is_admin":true,"is_owner":true,"is_primary_owner":true,"is_restricted":false,
			"is_ultra_restricted":false,"is_bot":false,"is_app_user":false,
			"updated":1612802061,"is_email_confirmed":true,
			"who_can_share_contact_card":"EVERYONE","a_field_slack_ships_next_week":42
		}],"can_interact":{"U04T4TH8Y":true,"U0B0QD6BH1N":false},"ok_extra":1}`
	})
	c := rec.client()

	got, err := c.UsersInfo(context.Background(), map[string]int64{"U04T4TH8Y": 1})
	if err != nil {
		t.Fatalf("UsersInfo on a full real-shaped response: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d users; want 1", len(got))
	}
	if got[0].ID != "U04T4TH8Y" || got[0].Version != 1612802061 || got[0].Profile.DisplayName != "Grant" {
		t.Errorf("modelled fields did not survive the unmodelled ones: %+v", got[0])
	}
}

// ---------------------------------------------------------------- batching

// TestFetchInfo_DoesNotMergeAnErroredBatch pins the half of
// fetchInfo's merge contract that the exported methods cannot see.
//
// ChannelsInfo and UsersInfo both return the zero value on any error,
// so from outside the package a merge on a failed batch is
// indistinguishable from no merge at all — every test that goes
// through them passes either way. That equivalence is a property of
// today's discard-on-error choice, not of fetchInfo, and the moment
// anyone decides partial results are useful it becomes a live bug
// with no coverage. So this drives fetchInfo directly.
//
// The failing batch here is not an ok:false, which would leave the
// response struct untouched and prove nothing. It is a well-formed
// results array followed by a type error, which is exactly what
// call's final json.Unmarshal turns into "err != nil, and out is
// partially populated": encoding/json records the first
// UnmarshalTypeError and keeps going, so Results is already filled in
// by the time the error comes back. Merging that batch would splice
// C-LEAKED — a row from a response we could not fully decode — into
// the accumulator.
func TestFetchInfo_DoesNotMergeAnErroredBatch(t *testing.T) {
	rec := newRecorder(t, func(n int) (int, string) {
		if n == 2 {
			// Decodes results, then fails on member_channels.
			return 200, `{"ok":true,"results":[{"id":"C-LEAKED","updated":9}],` +
				`"member_channels":"not-an-array"}`
		}
		return 200, fmt.Sprintf(`{"ok":true,"results":[{"id":"C%d","updated":%d}]}`, n, n)
	})
	c := rec.client()

	var merged []channelsInfoResponse
	err := fetchInfo(context.Background(), c, "T04T4TH8W", "channels/info",
		map[string]any{"check_membership": true},
		ids("C", channelsInfoBatchSize*2+10), channelsInfoBatchSize,
		func(batch channelsInfoResponse, _ []string) { merged = append(merged, batch) })
	if err == nil {
		t.Fatal("fetchInfo returned nil error when the second batch failed to decode")
	}

	if len(merged) != 1 {
		t.Fatalf("merge ran %d times; want exactly 1 — only the first batch succeeded, "+
			"and a batch that errored must never reach merge", len(merged))
	}
	for i, batch := range merged {
		for _, ch := range batch.Results {
			if ch.ID == "C-LEAKED" {
				t.Errorf("merge %d received %+v; that row came from a response that failed "+
					"to decode and must not be accumulated", i, ch)
			}
		}
	}
}

// TestFetchInfo_HandsMergeTheIDsThatBatchSent pins the plumbing that
// makes ChannelsInfoResult.MembershipQueried possible.
//
// A per-batch response can only answer about the ids that batch sent,
// so merge has to be told which those were. Handing it the whole
// updatedIDs map instead, or the previous batch's ids, produces a
// result that looks batch-scoped and is not — and every assertion that
// goes through ChannelsInfo would still pass in the single-batch case.
// So this drives fetchInfo directly and checks all three batches.
//
// Each slice is retained exactly as handed over — deliberately not
// copied — and every one of them is only checked after the last batch
// has run. That is the second half of the contract: fetchInfo promises
// a freshly allocated slice per batch that it keeps no reference to,
// so a merge implementation may hold on to it.
//
// Copying inside merge would hide a violation of that promise. It very
// nearly did: an earlier version of this test called sortedIDs() on
// the way in, and a fetchInfo that filled one reused buffer per call
// instead of allocating per batch passed the whole suite. Both merge
// implementations in this package happen to copy on receipt, so
// nothing observable broke — until some future one does not.
func TestFetchInfo_HandsMergeTheIDsThatBatchSent(t *testing.T) {
	rec := newRecorder(t, alwaysEmpty)
	c := rec.client()

	var seen [][]string
	err := fetchInfo(context.Background(), c, "T04T4TH8W", "channels/info",
		map[string]any{"check_membership": true},
		ids("C", channelsInfoBatchSize*2+10), channelsInfoBatchSize,
		func(_ channelsInfoResponse, queried []string) {
			seen = append(seen, queried)
		})
	if err != nil {
		t.Fatalf("fetchInfo: %v", err)
	}

	reqs := rec.requests()
	if len(reqs) != 3 {
		t.Fatalf("made %d requests; want 3", len(reqs))
	}
	if len(seen) != 3 {
		t.Fatalf("merge ran %d times; want 3", len(seen))
	}
	for i := range reqs {
		want := requestIDs(t, reqs, i+1)
		// sortedIDs sorts a clone. Sorting seen[i] in place would
		// scramble the very aliasing this test is here to detect.
		if got := sortedIDs(seen[i]); !slices.Equal(got, want) {
			t.Errorf("merge %d was handed %d ids; want the %d ids request %d actually sent\n"+
				" got: %v\nwant: %v", i+1, len(got), len(want), i+1, got, want)
		}
	}
}

// TestBatchSizes_StayWithinObservedShapes pins the constants against
// the captures. Exceeding a batch size the official client has never
// sent is exactly the kind of divergence that gets flagged.
func TestBatchSizes_StayWithinObservedShapes(t *testing.T) {
	if channelsInfoBatchSize > 63 {
		t.Errorf("channelsInfoBatchSize = %d; the largest channels/info batch observed "+
			"across 18 captured requests is 63", channelsInfoBatchSize)
	}
	if usersInfoBatchSize > 80 {
		t.Errorf("usersInfoBatchSize = %d; the largest users/info batch observed "+
			"across 30 captured requests is 80", usersInfoBatchSize)
	}
	if channelsInfoBatchSize < 1 || usersInfoBatchSize < 1 {
		t.Fatal("batch sizes must be positive; a zero size never flushes")
	}
}
