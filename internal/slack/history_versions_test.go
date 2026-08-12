package slackclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/slackhttp"
)

// Fixture values. Every one is distinct, and none is a zero value, so a
// tag pointing at the wrong key decodes nothing rather than
// accidentally decoding the right answer for the wrong reason (the
// Task 7 failure mode: two string fields sharing a fixture value).
const (
	hvChannel = "C0HVTEST01"

	// Cached {ts: version} pair, the shape the scroll capture sent.
	hvCachedTS      = "1783337000.111111"
	hvCachedVersion = "17833370001111"

	// The ts the server confirmed unchanged. DIFFERENT from hvCachedTS
	// so a test cannot pass by echoing the request.
	hvUnchangedTS = "1783337111.222222"

	// latest_updates entries. Keys and values are all distinct from
	// every other constant here.
	hvUpdateTS1  = "1783337222.333333"
	hvUpdateVer1 = "17833372223333331"
	hvUpdateTS2  = "1783337333.444444"
	hvUpdateVer2 = "17833373334444441"

	hvAnchorLatest = "1783337444.555555"
	hvAnchorOldest = "1783337555.666666"
)

// hvExpectedKeys is the parameter set measured on all 14 captured
// conversations.history requests, plus `token` (postForm injects it).
// Spelled out rather than derived from the implementation: a test that
// read the key set off the code under test would pass for any key set,
// including an empty one.
var hvExpectedKeys = []string{
	"token",
	"channel",
	"limit",
	"ignore_replies",
	"include_pin_count",
	"inclusive",
	"no_user_profile",
	"include_stories",
	"include_free_team_extra_messages",
	"include_date_joined",
	"cached_latest_updates",
	"_x_reason",
	"_x_mode",
	"_x_sonic",
	"_x_app_name",
}

// hvServer stands up an httptest server that records the decoded form
// body of the single request it expects, replies with body, and returns
// a *Client wired to reach it through the real BrowserTransport (so the
// _x_* envelope params are on the wire, as they are in production).
//
// Everything is local; nothing in this file touches the network.
func hvServer(t *testing.T, body string) (*Client, *url.Values, *string) {
	t.Helper()
	var form url.Values
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		// PostForm, not Form: Form merges the query string in, and
		// this endpoint's contract is about the body.
		form = r.PostForm
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	c := NewClient("xoxc-hv-test", "d-cookie")
	pointClientAtTestServer(t, c, srv)
	return c, &form, &path
}

// hvOKBody is a minimal well-formed response.
const hvOKBody = `{"ok":true,"messages":[],"unchanged_messages":[],"latest_updates":{},"has_more":false}`

func hvKeys(form url.Values) []string {
	out := make([]string, 0, len(form))
	for k := range form {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// The request must carry exactly the measured parameter set — no more,
// no fewer — when neither anchor is supplied (5 of 14 captured
// requests carried no anchor).
func TestHistoryWithVersions_RequestParamSet(t *testing.T) {
	c, form, path := hvServer(t, hvOKBody)

	if _, err := c.HistoryWithVersions(context.Background(), hvChannel, HistoryOpts{}); err != nil {
		t.Fatalf("HistoryWithVersions: %v", err)
	}

	if *path != "/api/conversations.history" {
		t.Errorf("path = %q, want %q", *path, "/api/conversations.history")
	}

	want := append([]string(nil), hvExpectedKeys...)
	sort.Strings(want)
	got := hvKeys(*form)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("param set mismatch\n got: %v\nwant: %v", got, want)
	}
}

// Each of the seven always-true booleans is asserted BY NAME. The set
// check above cannot tell two "true" params apart, so a dropped one is
// only visible here, and only if every key is named individually.
func TestHistoryWithVersions_BooleanParamsIndividually(t *testing.T) {
	c, form, _ := hvServer(t, hvOKBody)

	if _, err := c.HistoryWithVersions(context.Background(), hvChannel, HistoryOpts{}); err != nil {
		t.Fatalf("HistoryWithVersions: %v", err)
	}

	for _, key := range []string{
		"ignore_replies",
		"include_pin_count",
		"inclusive",
		"no_user_profile",
		"include_stories",
		"include_free_team_extra_messages",
	} {
		t.Run(key, func(t *testing.T) {
			vs, ok := (*form)[key]
			if !ok {
				t.Fatalf("%s missing from request", key)
			}
			if len(vs) != 1 || vs[0] != "true" {
				t.Errorf("%s = %v, want [\"true\"]", key, vs)
			}
		})
	}
}

func TestHistoryWithVersions_Channel(t *testing.T) {
	c, form, _ := hvServer(t, hvOKBody)

	if _, err := c.HistoryWithVersions(context.Background(), hvChannel, HistoryOpts{}); err != nil {
		t.Fatalf("HistoryWithVersions: %v", err)
	}
	if got := form.Get("channel"); got != hvChannel {
		t.Errorf("channel = %q, want %q", got, hvChannel)
	}
}

// An empty channel id would put `channel=` on the wire — a third
// request shape, present in none of the 14 captures. No request should
// be made at all.
func TestHistoryWithVersions_EmptyChannelIDErrorsWithoutRequesting(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(hvOKBody))
	}))
	defer srv.Close()
	c := NewClient("xoxc-hv-test", "d-cookie")
	pointClientAtTestServer(t, c, srv)

	got, err := c.HistoryWithVersions(context.Background(), "", HistoryOpts{})
	if err == nil {
		t.Fatal("want error for empty channel id, got nil")
	}
	if called {
		t.Error("a request was made despite the empty channel id")
	}
	if len(got.Messages) != 0 || got.HasMore || got.LatestUpdates != nil || got.UnchangedTS != nil {
		t.Errorf("want zero HistoryResult alongside the error, got %+v", got)
	}
}

// limit=28 on 14 of 14 captured requests. A caller passing 0 gets that.
func TestHistoryWithVersions_DefaultLimitIs28(t *testing.T) {
	c, form, _ := hvServer(t, hvOKBody)

	if _, err := c.HistoryWithVersions(context.Background(), hvChannel, HistoryOpts{}); err != nil {
		t.Fatalf("HistoryWithVersions: %v", err)
	}
	if got := form.Get("limit"); got != "28" {
		t.Errorf("limit = %q, want %q", got, "28")
	}
}

// A non-positive limit is not a page size, it is caller error, and
// `limit=0` / `limit=-5` are wire shapes none of the 14 captures show
// and the server can only reject. Those get the default. This is the
// same asymmetry edge.UsersList documents: a knowably-wrong shape is
// corrected, an unusually large but well-formed one is not.
func TestHistoryWithVersions_NonPositiveLimitGetsTheDefault(t *testing.T) {
	for _, limit := range []int{0, -1, -500} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			c, form, _ := hvServer(t, hvOKBody)
			_, err := c.HistoryWithVersions(context.Background(), hvChannel, HistoryOpts{Limit: limit})
			if err != nil {
				t.Fatalf("HistoryWithVersions: %v", err)
			}
			if got := form.Get("limit"); got != "28" {
				t.Errorf("limit = %q, want %q", got, "28")
			}
		})
	}
}

// Pins the documented-not-clamped decision: a caller-supplied limit
// reaches the wire verbatim, including the 50/200/500 page sizes that
// are slk's current fingerprint. Clamping here would silently return
// fewer messages than the caller asked for, and would convert one
// oversized request into a burst of correctly-sized ones — the worse
// fingerprint of the two. See HistoryOpts.Limit.
func TestHistoryWithVersions_CallerLimitIsSentVerbatim(t *testing.T) {
	for _, limit := range []int{1, 28, 50, 200, 500} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			c, form, _ := hvServer(t, hvOKBody)
			_, err := c.HistoryWithVersions(context.Background(), hvChannel, HistoryOpts{Limit: limit})
			if err != nil {
				t.Fatalf("HistoryWithVersions: %v", err)
			}
			if got, want := form.Get("limit"), fmt.Sprint(limit); got != want {
				t.Errorf("limit = %q, want %q", got, want)
			}
		})
	}
}

// cached_latest_updates is present on 14 of 14 requests. When the
// client holds nothing it sends the literal two-character string "{}";
// it does NOT omit the key. Omitting it is a shape the real client
// never emits.
func TestHistoryWithVersions_CachedLatestUpdates(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]string
		want string
	}{
		{"nil map", nil, "{}"},
		{"empty map", map[string]string{}, "{}"},
		{"one pair", map[string]string{hvCachedTS: hvCachedVersion},
			`{"` + hvCachedTS + `":"` + hvCachedVersion + `"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, form, _ := hvServer(t, hvOKBody)
			_, err := c.HistoryWithVersions(context.Background(), hvChannel,
				HistoryOpts{CachedVersions: tc.in})
			if err != nil {
				t.Fatalf("HistoryWithVersions: %v", err)
			}
			vs, ok := (*form)["cached_latest_updates"]
			if !ok {
				t.Fatal("cached_latest_updates absent from request; it is present on 14/14 captures")
			}
			if len(vs) != 1 || vs[0] != tc.want {
				t.Errorf("cached_latest_updates = %v, want [%q]", vs, tc.want)
			}
		})
	}
}

// A JSON object, not an array. `["ts"]` would be a different wire shape
// carrying the same information, and the server would not be able to
// answer with unchanged_messages at all.
func TestHistoryWithVersions_CachedLatestUpdatesIsAJSONObject(t *testing.T) {
	c, form, _ := hvServer(t, hvOKBody)
	_, err := c.HistoryWithVersions(context.Background(), hvChannel, HistoryOpts{
		CachedVersions: map[string]string{hvCachedTS: hvCachedVersion},
	})
	if err != nil {
		t.Fatalf("HistoryWithVersions: %v", err)
	}
	got := form.Get("cached_latest_updates")
	if !strings.HasPrefix(got, "{") || !strings.HasSuffix(got, "}") {
		t.Errorf("cached_latest_updates = %q, want a JSON object", got)
	}
	if strings.HasPrefix(got, "[") {
		t.Errorf("cached_latest_updates = %q, want an object not an array", got)
	}
}

// The caller's map must not be readable back through a mutation of the
// request: two pairs must both survive serialisation.
func TestHistoryWithVersions_CachedLatestUpdatesTwoPairs(t *testing.T) {
	c, form, _ := hvServer(t, hvOKBody)
	_, err := c.HistoryWithVersions(context.Background(), hvChannel, HistoryOpts{
		CachedVersions: map[string]string{
			hvCachedTS:  hvCachedVersion,
			hvUpdateTS1: hvUpdateVer1,
		},
	})
	if err != nil {
		t.Fatalf("HistoryWithVersions: %v", err)
	}
	got := form.Get("cached_latest_updates")
	for _, want := range []string{
		`"` + hvCachedTS + `":"` + hvCachedVersion + `"`,
		`"` + hvUpdateTS1 + `":"` + hvUpdateVer1 + `"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("cached_latest_updates = %q, missing %s", got, want)
		}
	}
}

// oldest appears on 5 of 14 requests, latest on 4, and neither on 5.
// Never both. Each must land under its OWN key and must be absent — not
// present-and-empty — when unset.
func TestHistoryWithVersions_Anchors(t *testing.T) {
	t.Run("oldest only", func(t *testing.T) {
		c, form, _ := hvServer(t, hvOKBody)
		_, err := c.HistoryWithVersions(context.Background(), hvChannel,
			HistoryOpts{Oldest: hvAnchorOldest})
		if err != nil {
			t.Fatalf("HistoryWithVersions: %v", err)
		}
		if got := form.Get("oldest"); got != hvAnchorOldest {
			t.Errorf("oldest = %q, want %q", got, hvAnchorOldest)
		}
		if _, ok := (*form)["latest"]; ok {
			t.Errorf("latest present (%q) with only Oldest set", form.Get("latest"))
		}
	})

	t.Run("latest only", func(t *testing.T) {
		c, form, _ := hvServer(t, hvOKBody)
		_, err := c.HistoryWithVersions(context.Background(), hvChannel,
			HistoryOpts{Latest: hvAnchorLatest})
		if err != nil {
			t.Fatalf("HistoryWithVersions: %v", err)
		}
		if got := form.Get("latest"); got != hvAnchorLatest {
			t.Errorf("latest = %q, want %q", got, hvAnchorLatest)
		}
		if _, ok := (*form)["oldest"]; ok {
			t.Errorf("oldest present (%q) with only Latest set", form.Get("oldest"))
		}
	})

	t.Run("neither", func(t *testing.T) {
		c, form, _ := hvServer(t, hvOKBody)
		_, err := c.HistoryWithVersions(context.Background(), hvChannel, HistoryOpts{})
		if err != nil {
			t.Fatalf("HistoryWithVersions: %v", err)
		}
		if _, ok := (*form)["latest"]; ok {
			t.Errorf("latest present (%q) with no anchor set", form.Get("latest"))
		}
		if _, ok := (*form)["oldest"]; ok {
			t.Errorf("oldest present (%q) with no anchor set", form.Get("oldest"))
		}
	})
}

// include_date_joined is the one business param that varies: true on 8
// of 14 requests, false on 6. Both directions are asserted, because a
// hardcoded literal is invisible if only one is.
func TestHistoryWithVersions_IncludeDateJoined(t *testing.T) {
	for _, tc := range []struct {
		in   bool
		want string
	}{
		{true, "true"},
		{false, "false"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			c, form, _ := hvServer(t, hvOKBody)
			_, err := c.HistoryWithVersions(context.Background(), hvChannel,
				HistoryOpts{IncludeDateJoined: tc.in})
			if err != nil {
				t.Fatalf("HistoryWithVersions: %v", err)
			}
			vs, ok := (*form)["include_date_joined"]
			if !ok {
				t.Fatal("include_date_joined absent; it is present on 14/14 captures")
			}
			if len(vs) != 1 || vs[0] != tc.want {
				t.Errorf("include_date_joined = %v, want [%q]", vs, tc.want)
			}
		})
	}
}

// _x_reason: reason.go's defaultReasons already maps
// conversations.history to "message-pane/requestHistory", so this
// passes with or without the WithReason call in the method. It pins
// the value that reaches the wire either way.
func TestHistoryWithVersions_SendsXReason(t *testing.T) {
	c, form, _ := hvServer(t, hvOKBody)
	if _, err := c.HistoryWithVersions(context.Background(), hvChannel, HistoryOpts{}); err != nil {
		t.Fatalf("HistoryWithVersions: %v", err)
	}
	if got := form.Get("_x_reason"); got != "message-pane/requestHistory" {
		t.Errorf("_x_reason = %q, want %q", got, "message-pane/requestHistory")
	}
}

// The captures show a second reason on this endpoint,
// "unread-counts/onLastReadUpdated" (3 of 14). reason.go documents that
// as a WithReason override rather than a table entry, so a caller that
// sets one must reach the wire with it — this method must not clobber
// a reason the caller already chose.
func TestHistoryWithVersions_CallerReasonWins(t *testing.T) {
	c, form, _ := hvServer(t, hvOKBody)
	ctx := slackhttp.WithReason(context.Background(), "unread-counts/onLastReadUpdated")
	if _, err := c.HistoryWithVersions(ctx, hvChannel, HistoryOpts{}); err != nil {
		t.Fatalf("HistoryWithVersions: %v", err)
	}
	if got := form.Get("_x_reason"); got != "unread-counts/onLastReadUpdated" {
		t.Errorf("_x_reason = %q, want the caller's %q", got, "unread-counts/onLastReadUpdated")
	}
}

// hvDecodeBody is a response exercising every modelled field with a
// distinct, non-zero value. has_more is TRUE here on purpose: with a
// false fixture no assertion can tell "decoded false" from "never
// decoded" (the Task 4 failure mode, 9 surviving mutants).
const hvDecodeBody = `{
	"ok": true,
	"messages": [
		{"type":"message","user":"U0HV1","text":"first","ts":"` + hvUpdateTS1 + `"},
		{"type":"message","user":"U0HV2","text":"second","ts":"` + hvUpdateTS2 + `"}
	],
	"unchanged_messages": ["` + hvUnchangedTS + `"],
	"latest_updates": {
		"` + hvUpdateTS1 + `": "` + hvUpdateVer1 + `",
		"` + hvUpdateTS2 + `": "` + hvUpdateVer2 + `"
	},
	"has_more": true
}`

func TestHistoryWithVersions_DecodesResponse(t *testing.T) {
	c, _, _ := hvServer(t, hvDecodeBody)

	got, err := c.HistoryWithVersions(context.Background(), hvChannel, HistoryOpts{})
	if err != nil {
		t.Fatalf("HistoryWithVersions: %v", err)
	}

	if len(got.Messages) != 2 {
		t.Fatalf("len(Messages) = %d, want 2", len(got.Messages))
	}
	if got.Messages[0].Text != "first" || got.Messages[1].Text != "second" {
		t.Errorf("Messages texts = %q/%q, want first/second",
			got.Messages[0].Text, got.Messages[1].Text)
	}
	if got.Messages[0].Timestamp != hvUpdateTS1 {
		t.Errorf("Messages[0].Timestamp = %q, want %q", got.Messages[0].Timestamp, hvUpdateTS1)
	}

	if len(got.UnchangedTS) != 1 || got.UnchangedTS[0] != hvUnchangedTS {
		t.Errorf("UnchangedTS = %v, want [%q]", got.UnchangedTS, hvUnchangedTS)
	}

	if len(got.LatestUpdates) != 2 {
		t.Fatalf("len(LatestUpdates) = %d, want 2", len(got.LatestUpdates))
	}
	if got.LatestUpdates[hvUpdateTS1] != hvUpdateVer1 {
		t.Errorf("LatestUpdates[%q] = %q, want %q",
			hvUpdateTS1, got.LatestUpdates[hvUpdateTS1], hvUpdateVer1)
	}
	if got.LatestUpdates[hvUpdateTS2] != hvUpdateVer2 {
		t.Errorf("LatestUpdates[%q] = %q, want %q",
			hvUpdateTS2, got.LatestUpdates[hvUpdateTS2], hvUpdateVer2)
	}

	if !got.HasMore {
		t.Error("HasMore = false, want true")
	}
}

// The other direction for has_more: a false on the wire must arrive as
// false, so the field cannot be hardcoded or inverted.
func TestHistoryWithVersions_HasMoreFalse(t *testing.T) {
	c, _, _ := hvServer(t, `{"ok":true,"messages":[],"unchanged_messages":["`+hvUnchangedTS+`"],"latest_updates":{"`+hvUpdateTS1+`":"`+hvUpdateVer1+`"},"has_more":false}`)

	got, err := c.HistoryWithVersions(context.Background(), hvChannel, HistoryOpts{})
	if err != nil {
		t.Fatalf("HistoryWithVersions: %v", err)
	}
	if got.HasMore {
		t.Error("HasMore = true, want false")
	}
	// Guard against "the whole decode silently did nothing": the two
	// neighbouring fields must still be populated in this fixture.
	if len(got.UnchangedTS) != 1 || len(got.LatestUpdates) != 1 {
		t.Errorf("neighbouring fields did not decode: UnchangedTS=%v LatestUpdates=%v",
			got.UnchangedTS, got.LatestUpdates)
	}
}

// The interaction actually observed in the scroll capture: the client
// sent one cached {ts: version} pair and the server answered
// unchanged_messages=1, messages=27. Both halves are asserted, because
// "we validated one cached message" and "we still received 27 bodies"
// are the two facts this endpoint exists to deliver.
func TestHistoryWithVersions_ScrollCaptureInteraction(t *testing.T) {
	var msgs []string
	for i := 0; i < 27; i++ {
		msgs = append(msgs, fmt.Sprintf(
			`{"type":"message","user":"U0HV%02d","text":"m%02d","ts":"1783338%03d.000100"}`, i, i, i))
	}
	body := `{"ok":true,"latest_updates":{"` + hvUpdateTS1 + `":"` + hvUpdateVer1 + `"},` +
		`"unchanged_messages":["` + hvUnchangedTS + `"],` +
		`"messages":[` + strings.Join(msgs, ",") + `],"has_more":true}`

	c, form, _ := hvServer(t, body)

	got, err := c.HistoryWithVersions(context.Background(), hvChannel, HistoryOpts{
		CachedVersions: map[string]string{hvCachedTS: hvCachedVersion},
	})
	if err != nil {
		t.Fatalf("HistoryWithVersions: %v", err)
	}

	// Request half: exactly the one cached pair went out.
	wantCached := `{"` + hvCachedTS + `":"` + hvCachedVersion + `"}`
	if gotCached := form.Get("cached_latest_updates"); gotCached != wantCached {
		t.Errorf("cached_latest_updates = %q, want %q", gotCached, wantCached)
	}

	// Response half.
	if len(got.UnchangedTS) != 1 {
		t.Errorf("len(UnchangedTS) = %d, want 1", len(got.UnchangedTS))
	} else if got.UnchangedTS[0] != hvUnchangedTS {
		t.Errorf("UnchangedTS[0] = %q, want %q", got.UnchangedTS[0], hvUnchangedTS)
	}
	if len(got.Messages) != 27 {
		t.Errorf("len(Messages) = %d, want 27", len(got.Messages))
	}
	if len(got.LatestUpdates) != 1 || got.LatestUpdates[hvUpdateTS1] != hvUpdateVer1 {
		t.Errorf("LatestUpdates = %v, want one %q entry", got.LatestUpdates, hvUpdateTS1)
	}
}

// ok:false is an error, and the fully populated body that arrives with
// it must not reach the caller. The fixture deliberately fills every
// field, so returning the decoded struct here would be visible.
func TestHistoryWithVersions_OKFalseIsAnErrorWithNoData(t *testing.T) {
	body := `{"ok":false,"error":"channel_not_found",` +
		`"messages":[{"type":"message","text":"leaked","ts":"` + hvUpdateTS1 + `"}],` +
		`"unchanged_messages":["` + hvUnchangedTS + `"],` +
		`"latest_updates":{"` + hvUpdateTS1 + `":"` + hvUpdateVer1 + `"},` +
		`"has_more":true}`
	c, _, _ := hvServer(t, body)

	got, err := c.HistoryWithVersions(context.Background(), hvChannel, HistoryOpts{})
	if err == nil {
		t.Fatal("want error for ok:false, got nil")
	}
	if !strings.Contains(err.Error(), "channel_not_found") {
		t.Errorf("error = %v, want it to name channel_not_found", err)
	}
	if len(got.Messages) != 0 || len(got.UnchangedTS) != 0 ||
		len(got.LatestUpdates) != 0 || got.HasMore {
		t.Errorf("data returned alongside ok:false error: %+v", got)
	}
}

// ok:false with no error field must still name a failure.
func TestHistoryWithVersions_OKFalseWithoutErrorField(t *testing.T) {
	c, _, _ := hvServer(t, `{"ok":false}`)

	_, err := c.HistoryWithVersions(context.Background(), hvChannel, HistoryOpts{})
	if err == nil {
		t.Fatal("want error for ok:false, got nil")
	}
	if strings.TrimSpace(err.Error()) == "" {
		t.Error("error message is empty")
	}
}

// encoding/json keeps decoding past the first type error, so a
// malformed response leaves a partly populated struct in a local by the
// time the error is checked. None of it may reach the caller.
func TestHistoryWithVersions_DecodeErrorReturnsNoPartialData(t *testing.T) {
	// messages, unchanged_messages and latest_updates are all valid
	// and decode cleanly; only has_more has the wrong type. A
	// pass-the-local-back implementation returns three populated
	// fields here.
	body := `{"ok":true,` +
		`"messages":[{"type":"message","text":"leaked","ts":"` + hvUpdateTS1 + `"}],` +
		`"unchanged_messages":["` + hvUnchangedTS + `"],` +
		`"latest_updates":{"` + hvUpdateTS1 + `":"` + hvUpdateVer1 + `"},` +
		`"has_more":"yes"}`
	c, _, _ := hvServer(t, body)

	got, err := c.HistoryWithVersions(context.Background(), hvChannel, HistoryOpts{})
	if err == nil {
		t.Fatal("want a decode error for has_more:\"yes\", got nil")
	}
	if len(got.Messages) != 0 {
		t.Errorf("Messages leaked past a decode error: %+v", got.Messages)
	}
	if len(got.UnchangedTS) != 0 {
		t.Errorf("UnchangedTS leaked past a decode error: %v", got.UnchangedTS)
	}
	if len(got.LatestUpdates) != 0 {
		t.Errorf("LatestUpdates leaked past a decode error: %v", got.LatestUpdates)
	}
	if got.HasMore {
		t.Error("HasMore leaked past a decode error")
	}
}

// Non-JSON bodies (an HTML error page from a proxy) are an error, not a
// silent empty result.
func TestHistoryWithVersions_NonJSONBodyIsAnError(t *testing.T) {
	c, _, _ := hvServer(t, `<html><body>gateway timeout</body></html>`)

	got, err := c.HistoryWithVersions(context.Background(), hvChannel, HistoryOpts{})
	if err == nil {
		t.Fatal("want error for a non-JSON body, got nil")
	}
	if len(got.Messages) != 0 || got.HasMore {
		t.Errorf("data returned alongside a parse error: %+v", got)
	}
}

// The captures record eight response keys; slk models four of them.
// The four it ignores, plus anything Slack adds later, must not break
// the decode.
func TestHistoryWithVersions_UnknownFieldsAreIgnored(t *testing.T) {
	body := `{"ok":true,` +
		`"messages":[{"type":"message","text":"kept","ts":"` + hvUpdateTS1 + `"}],` +
		`"unchanged_messages":["` + hvUnchangedTS + `"],` +
		`"latest_updates":{"` + hvUpdateTS1 + `":"` + hvUpdateVer1 + `"},` +
		`"has_more":true,` +
		`"pin_count":3,"channel_actions_ts":null,"channel_actions_count":0,` +
		`"date_joined":{"U0HV1":1700000000},` +
		`"oldest":"` + hvAnchorOldest + `","latest":"` + hvAnchorLatest + `",` +
		`"response_metadata":{"next_cursor":"bmV4dA=="},` +
		`"some_field_slack_adds_in_2027":{"nested":[1,2,3]}}`
	c, _, _ := hvServer(t, body)

	got, err := c.HistoryWithVersions(context.Background(), hvChannel, HistoryOpts{})
	if err != nil {
		t.Fatalf("HistoryWithVersions: %v", err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Text != "kept" {
		t.Errorf("Messages = %+v, want one \"kept\"", got.Messages)
	}
	if len(got.UnchangedTS) != 1 || len(got.LatestUpdates) != 1 || !got.HasMore {
		t.Errorf("modelled fields did not survive the unknown keys: %+v", got)
	}
}

// A transport-level failure (non-2xx) is an error with no data.
func TestHistoryWithVersions_HTTPErrorIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("bad gateway"))
	}))
	defer srv.Close()
	c := NewClient("xoxc-hv-test", "d-cookie")
	pointClientAtTestServer(t, c, srv)

	got, err := c.HistoryWithVersions(context.Background(), hvChannel, HistoryOpts{})
	if err == nil {
		t.Fatal("want error for HTTP 502, got nil")
	}
	if len(got.Messages) != 0 || got.HasMore {
		t.Errorf("data returned alongside a transport error: %+v", got)
	}
}

// The caller's CachedVersions map must not be mutated or retained.
func TestHistoryWithVersions_DoesNotMutateCallerMap(t *testing.T) {
	c, _, _ := hvServer(t, hvOKBody)
	in := map[string]string{hvCachedTS: hvCachedVersion}

	if _, err := c.HistoryWithVersions(context.Background(), hvChannel,
		HistoryOpts{CachedVersions: in}); err != nil {
		t.Fatalf("HistoryWithVersions: %v", err)
	}
	if len(in) != 1 || in[hvCachedTS] != hvCachedVersion {
		t.Errorf("caller map was mutated: %v", in)
	}
}
