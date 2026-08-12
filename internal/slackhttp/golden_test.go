package slackhttp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
)

// requestShape mirrors testdata/official-request-shape.json, the
// redacted digest of the 2026-07-30 captures of the official Slack web
// client.
//
// That file — not this one — is the source of truth. The tests below
// deliberately hardcode no param names, no header names and no
// ordering: they read every expectation out of the fixture and drive
// real requests through BrowserTransport to compare. A param list
// duplicated here would defeat the point, because a refactor could
// then change the transport and the test together and stay green.
type requestShape struct {
	Source string `json:"source"`
	Note   string `json:"note"`

	HTTPHeaders struct {
		Present []string `json:"present"`
		Absent  []string `json:"absent"`
	} `json:"http_headers"`

	WebSocketUpgradeHeaders struct {
		Present []string `json:"present"`
		Absent  []string `json:"absent"`
	} `json:"websocket_upgrade_headers"`

	ImageHeaders struct {
		Present []string `json:"present"`
		Absent  []string `json:"absent"`
	} `json:"image_headers"`

	WorkspaceAPI struct {
		QueryParamOrder []string `json:"query_param_order"`
		PreBoot         struct {
			AbsentParams []string `json:"absent_params"`
			XIDPrefix    string   `json:"x_id_prefix"`
		} `json:"pre_boot"`
		BodyTrailingFieldOrder   []string `json:"body_trailing_field_order"`
		BodyXModeAbsentMethods   []string `json:"body_x_mode_absent_methods"`
		BodyXReasonAbsentMethods []string `json:"body_x_reason_absent_methods"`
		XReasonPlacement         string   `json:"x_reason_placement"`
		XReasonPresentOutside    bool     `json:"x_reason_present_outside_absent_methods"`
		// XReasonObservedValues is every distinct _x_reason the
		// official client was seen sending — the membership set
		// TestGenericReasonIsAValueTheOfficialClientSends checks the
		// fallback against.
		XReasonObservedValues []string `json:"x_reason_observed_values"`
	} `json:"workspace_api"`

	EdgeAPI struct {
		QueryParamOrder []string `json:"query_param_order"`
		AbsentParams    []string `json:"absent_params"`
		Body            struct {
			ContentType    string   `json:"content_type"`
			EnvelopeFields []string `json:"envelope_fields"`
		} `json:"body"`
	} `json:"edgeapi"`
}

func loadRequestShape(t *testing.T) requestShape {
	t.Helper()
	raw, err := os.ReadFile("testdata/official-request-shape.json")
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	var s requestShape
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("parse golden fixture: %v", err)
	}
	// A fixture that silently failed to populate would make every
	// assertion below vacuously true, which is worse than a failure.
	if len(s.WorkspaceAPI.QueryParamOrder) == 0 ||
		len(s.EdgeAPI.QueryParamOrder) == 0 ||
		len(s.HTTPHeaders.Present) == 0 ||
		len(s.WebSocketUpgradeHeaders.Present) == 0 ||
		len(s.ImageHeaders.Present) == 0 ||
		len(s.WorkspaceAPI.BodyTrailingFieldOrder) == 0 ||
		len(s.WorkspaceAPI.BodyXModeAbsentMethods) == 0 ||
		len(s.WorkspaceAPI.BodyXReasonAbsentMethods) == 0 ||
		len(s.WorkspaceAPI.XReasonObservedValues) == 0 {
		t.Fatalf("golden fixture parsed but is missing sections: %+v", s)
	}
	return s
}

// goldenReq drives one request through BrowserTransport and returns it
// exactly as the inner transport saw it, preserving query and body
// ORDER — which url.Values cannot. Pass an empty contentType/body for a
// bodyless request.
//
// Built on newEnvelopeClient/captureRT from transport_test.go rather
// than a second harness; doEnvelopeReq and doBodyReq are not reusable
// here because they normalize away the ordering these tests exist to
// check.
func goldenReq(t *testing.T, env *Envelope, host, path, contentType, body, reason string) *http.Request {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	client, recorder := newEnvelopeClient(t, env)

	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest("POST", srv.URL+path, rdr)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Host = host
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if reason != "" {
		req = req.WithContext(WithReason(req.Context(), reason))
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	return recorder.last
}

// queryKeyOrder returns the keys of a raw query string in emission
// order. url.Values is a map and cannot express this.
func queryKeyOrder(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, "&")
	keys := make([]string, 0, len(parts))
	for _, kv := range parts {
		keys = append(keys, strings.SplitN(kv, "=", 2)[0])
	}
	return keys
}

// assertKeyOrder compares an observed key sequence to the fixture's,
// reporting the first divergence with both full sequences.
func assertKeyOrder(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d keys %v; fixture requires %d %v", what, len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: key[%d] = %q; fixture requires %q\n got:  %v\n want: %v",
				what, i, got[i], want[i], got, want)
		}
	}
}

func TestGolden_WorkspaceAPIPostBootShape(t *testing.T) {
	shape := loadRequestShape(t)

	env := NewEnvelope()
	env.SetTeamID("T04T4TH8W")
	req := goldenReq(t, env, "rands-leadership.slack.com", "/api/conversations.history",
		"application/x-www-form-urlencoded", "token=xoxc-redacted&channel=C123",
		"message-pane/requestHistory")

	// Query params, in the captured emission order.
	assertKeyOrder(t, "workspace API query",
		queryKeyOrder(req.URL.RawQuery), shape.WorkspaceAPI.QueryParamOrder)

	// The order must not merely differ from alphabetical by accident:
	// url.Values.Encode() sorts, and a refactor back to it would give
	// every slk request a perfectly alphabetized query string.
	sorted := append([]string(nil), shape.WorkspaceAPI.QueryParamOrder...)
	sort.Strings(sorted)
	alphabetical := true
	for i := range sorted {
		if sorted[i] != shape.WorkspaceAPI.QueryParamOrder[i] {
			alphabetical = false
			break
		}
	}
	if alphabetical {
		t.Error("fixture's workspace order is alphabetical; the captures show 0 of 163 " +
			"requests sorted, so the fixture itself must be wrong")
	}

	// Every header the official client sends must be present.
	for _, h := range shape.HTTPHeaders.Present {
		if req.Header.Get(h) == "" {
			t.Errorf("header %s absent; fixture requires it on every API request", h)
		}
	}
	// And nothing the fixture marks absent — Referer, on 0 of 279.
	for _, h := range shape.HTTPHeaders.Absent {
		if v := req.Header.Get(h); v != "" {
			t.Errorf("header %s = %q; fixture requires it absent (real client sends none)", h, v)
		}
	}

	// Body: business fields first, then the captured trailing envelope.
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	gotBody := queryKeyOrder(string(raw))
	if len(gotBody) < len(shape.WorkspaceAPI.BodyTrailingFieldOrder) {
		t.Fatalf("body keys = %v; fixture requires a %d-field envelope at the end",
			gotBody, len(shape.WorkspaceAPI.BodyTrailingFieldOrder))
	}
	tail := gotBody[len(gotBody)-len(shape.WorkspaceAPI.BodyTrailingFieldOrder):]
	assertKeyOrder(t, "workspace API body tail", tail, shape.WorkspaceAPI.BodyTrailingFieldOrder)
	// Business params keep their place ahead of the envelope.
	lead := gotBody[:len(gotBody)-len(tail)]
	assertKeyOrder(t, "workspace API body business params", lead, []string{"token", "channel"})
}

func TestGolden_WorkspaceAPIPreBootShape(t *testing.T) {
	shape := loadRequestShape(t)

	// The pre-boot order is DERIVED from the canonical order by
	// removing the post-boot-only params in place. Restating it as a
	// second literal list would let the two drift apart.
	absent := make(map[string]bool, len(shape.WorkspaceAPI.PreBoot.AbsentParams))
	for _, k := range shape.WorkspaceAPI.PreBoot.AbsentParams {
		absent[k] = true
	}
	var want []string
	for _, k := range shape.WorkspaceAPI.QueryParamOrder {
		if !absent[k] {
			want = append(want, k)
		}
	}
	if len(want) == len(shape.WorkspaceAPI.QueryParamOrder) {
		t.Fatal("fixture's pre_boot.absent_params matches nothing in query_param_order")
	}

	req := goldenReq(t, NewEnvelope(), "rands-leadership.slack.com",
		"/api/experiments.getByUser", "", "", "")

	assertKeyOrder(t, "pre-boot workspace API query", queryKeyOrder(req.URL.RawQuery), want)

	q := req.URL.Query()
	for _, k := range shape.WorkspaceAPI.PreBoot.AbsentParams {
		if v := q.Get(k); v != "" {
			t.Errorf("pre-boot param %s = %q; fixture requires it absent until the team id is known", k, v)
		}
	}
	if pfx := shape.WorkspaceAPI.PreBoot.XIDPrefix; !strings.HasPrefix(q.Get("_x_id"), pfx) {
		t.Errorf("_x_id = %q; fixture requires the %q prefix pre-boot", q.Get("_x_id"), pfx)
	}
}

func TestGolden_EdgeAPIShape(t *testing.T) {
	shape := loadRequestShape(t)

	env := NewEnvelope()
	env.SetTeamID("T04T4TH8W")
	req := goldenReq(t, env, "edgeapi.slack.com", "/cache/T04T4TH8W/users/info",
		shape.EdgeAPI.Body.ContentType, `{"updated_ids":{"U123":0}}`, "boot")

	assertKeyOrder(t, "edgeapi query",
		queryKeyOrder(req.URL.RawQuery), shape.EdgeAPI.QueryParamOrder)

	q := req.URL.Query()
	for _, k := range shape.EdgeAPI.AbsentParams {
		if v := q.Get(k); v != "" {
			t.Errorf("edgeapi param %s = %q; fixture requires it absent "+
				"(workspace-API-only, 0 of 116 edgeapi requests carry it)", k, v)
		}
	}

	// edgeapi bodies are JSON sent as text/plain and carry no _x_*
	// fields at all — 116/116. The body envelope is workspace-only, so
	// the transport must pass the JSON through byte-for-byte.
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if got, want := string(raw), `{"updated_ids":{"U123":0}}`; got != want {
		t.Errorf("edgeapi body = %q; want %q unmodified", got, want)
	}
	if len(shape.EdgeAPI.Body.EnvelopeFields) != 0 {
		t.Fatalf("fixture claims edgeapi bodies carry envelope fields %v; the captures show none",
			shape.EdgeAPI.Body.EnvelopeFields)
	}
	for _, f := range shape.WorkspaceAPI.BodyTrailingFieldOrder {
		if strings.Contains(string(raw), f) {
			t.Errorf("edgeapi body contains workspace body field %q; fixture requires none", f)
		}
	}
}

func TestGolden_WebSocketUpgradeHeaders(t *testing.T) {
	shape := loadRequestShape(t)
	h := WebSocketHeaders()

	for _, k := range shape.WebSocketUpgradeHeaders.Present {
		if h.Get(k) == "" {
			t.Errorf("WebSocketHeaders()[%s] absent; fixture requires it on the upgrade", k)
		}
	}
	for _, k := range shape.WebSocketUpgradeHeaders.Absent {
		if v := h.Get(k); v != "" {
			t.Errorf("WebSocketHeaders()[%s] = %q; fixture requires it absent "+
				"(real Chrome omits it on a WS upgrade, so sending it is an slk signature)", k, v)
		}
	}
	// Exact count: an EXTRA header not named in either fixture list is
	// still a signature, and neither loop above would catch it.
	if len(h) != len(shape.WebSocketUpgradeHeaders.Present) {
		keys := make([]string, 0, len(h))
		for k := range h {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		t.Errorf("WebSocketHeaders() has %d headers %v; fixture requires exactly %d %v",
			len(h), keys, len(shape.WebSocketUpgradeHeaders.Present),
			shape.WebSocketUpgradeHeaders.Present)
	}

	// The WS set must stay strictly smaller than the XHR set. Merging
	// the two is the specific regression this guards.
	if len(h) >= len(shape.HTTPHeaders.Present) {
		t.Errorf("WS header set (%d) is not smaller than the XHR set (%d); "+
			"the captures show Chrome sending strictly fewer on an upgrade",
			len(h), len(shape.HTTPHeaders.Present))
	}
}

func TestGolden_ImageHeaders(t *testing.T) {
	shape := loadRequestShape(t)
	got := imageHeaderPairs()

	for _, k := range shape.ImageHeaders.Present {
		if got[k] == "" {
			t.Errorf("imageHeaderPairs()[%s] absent; fixture requires it on every image fetch", k)
		}
	}
	for _, k := range shape.ImageHeaders.Absent {
		if v, ok := got[k]; ok {
			t.Errorf("imageHeaderPairs()[%s] = %q; fixture requires it absent "+
				"(real Chrome omits it on a no-cors image load, so sending it is an slk signature)", k, v)
		}
	}
	// Exact count: an EXTRA header named in neither list is still a
	// signature, and neither loop above would catch it.
	if len(got) != len(shape.ImageHeaders.Present) {
		keys := make([]string, 0, len(got))
		for k := range got {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		t.Errorf("imageHeaderPairs() has %d headers %v; fixture requires exactly %d %v",
			len(got), keys, len(shape.ImageHeaders.Present), shape.ImageHeaders.Present)
	}

	// And the set must actually reach the wire on the image path.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	recorder := &captureRT{wrapped: http.DefaultTransport}
	client := &http.Client{Transport: &BrowserTransport{Inner: recorder, Dest: DestImage}}
	req, err := http.NewRequest("GET", srv.URL+"/files-tmb/T04-F0A-abc/image_480.png", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Host = "files.slack.com"
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	for _, k := range shape.ImageHeaders.Present {
		if recorder.last.Header.Get(k) == "" {
			t.Errorf("image request header %s absent on the wire; fixture requires it", k)
		}
	}
	for _, k := range shape.ImageHeaders.Absent {
		if v := recorder.last.Header.Get(k); v != "" {
			t.Errorf("image request header %s = %q on the wire; fixture requires it absent", k, v)
		}
	}
}

func TestGolden_XReasonIsBodyOnly(t *testing.T) {
	shape := loadRequestShape(t)
	if shape.WorkspaceAPI.XReasonPlacement != "body" {
		t.Fatalf("fixture x_reason_placement = %q; captures show 153 in bodies, 0 in query strings",
			shape.WorkspaceAPI.XReasonPlacement)
	}

	env := NewEnvelope()
	env.SetTeamID("T04T4TH8W")
	req := goldenReq(t, env, "rands-leadership.slack.com", "/api/conversations.history",
		"application/x-www-form-urlencoded", "token=xoxc-redacted",
		"message-pane/requestHistory")

	if v := req.URL.Query().Get("_x_reason"); v != "" {
		t.Errorf("_x_reason = %q in the query string; fixture requires it body-only", v)
	}
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(raw), "_x_reason=message-pane") {
		t.Errorf("body = %q; want _x_reason carried in the body", raw)
	}
}

func TestGolden_XReasonPresentOnNonExcludedWorkspaceBodies(t *testing.T) {
	shape := loadRequestShape(t)
	if !shape.WorkspaceAPI.XReasonPresentOutside {
		t.Fatal("fixture x_reason_present_outside_absent_methods is false; slk must never " +
			"emit a workspace-API body with _x_mode and no _x_reason")
	}
	// conversations.history is deliberately not in
	// body_x_reason_absent_methods; if it ever were, this test would be
	// asserting the opposite of what it says.
	for _, m := range shape.WorkspaceAPI.BodyXReasonAbsentMethods {
		if m == "conversations.history" {
			t.Fatal("fixture lists conversations.history as omitting _x_reason; " +
				"this test drives that endpoint precisely because it does not")
		}
	}

	env := NewEnvelope()
	env.SetTeamID("T04T4TH8W")
	// No WithReason on the context — the state nearly every slk call
	// site is in.
	req := goldenReq(t, env, "rands-leadership.slack.com", "/api/conversations.history",
		"application/x-www-form-urlencoded", "token=xoxc-redacted", "")

	raw, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	vals, err := url.ParseQuery(string(raw))
	if err != nil {
		t.Fatalf("ParseQuery(%q): %v", raw, err)
	}
	if vals.Get("_x_reason") == "" {
		t.Errorf("body = %q has no _x_reason; fixture requires it on every workspace-API body", raw)
	}
	// The fields are still in the captured order, reason included.
	tail := queryKeyOrder(string(raw))
	tail = tail[len(tail)-len(shape.WorkspaceAPI.BodyTrailingFieldOrder):]
	assertKeyOrder(t, "workspace API body tail (defaulted reason)",
		tail, shape.WorkspaceAPI.BodyTrailingFieldOrder)
}

func TestGolden_BodyXModeAbsentOnBootPhaseEndpoints(t *testing.T) {
	shape := loadRequestShape(t)

	// The no-_x_mode tail is DERIVED from the canonical tail by
	// removing _x_mode in place, the same way pre_boot's query order is
	// derived. Restating it as a second literal list would let the two
	// drift apart, and the point of this test is that the OTHER fields
	// keep their relative order when _x_mode drops out.
	//
	// Five of these seven are also in body_x_reason_absent_methods and
	// lose _x_reason as well, so the removal set is per-endpoint —
	// still derived, never restated.
	reasonAbsent := make(map[string]struct{}, len(shape.WorkspaceAPI.BodyXReasonAbsentMethods))
	for _, m := range shape.WorkspaceAPI.BodyXReasonAbsentMethods {
		reasonAbsent[m] = struct{}{}
	}
	derive := func(method string) []string {
		var out []string
		_, alsoNoReason := reasonAbsent[method]
		for _, k := range shape.WorkspaceAPI.BodyTrailingFieldOrder {
			if k == "_x_mode" || (alsoNoReason && k == "_x_reason") {
				continue
			}
			out = append(out, k)
		}
		return out
	}
	if len(derive("client.userBoot")) == len(shape.WorkspaceAPI.BodyTrailingFieldOrder) {
		t.Fatal("fixture's body_trailing_field_order contains no _x_mode; " +
			"this test would then assert nothing")
	}

	for _, method := range shape.WorkspaceAPI.BodyXModeAbsentMethods {
		want := derive(method)
		t.Run(method, func(t *testing.T) {
			env := NewEnvelope()
			env.SetTeamID("T04T4TH8W")
			req := goldenReq(t, env, "rands-leadership.slack.com", "/api/"+method,
				"application/x-www-form-urlencoded", "token=xoxc-redacted", "")

			raw, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			keys := queryKeyOrder(string(raw))
			for _, k := range keys {
				if k == "_x_mode" {
					t.Fatalf("body = %q carries _x_mode; the captures show %s sending "+
						"none (it is one of the %d boot-phase endpoints in the fixture's "+
						"body_x_mode_absent_methods)", raw, method, len(shape.WorkspaceAPI.BodyXModeAbsentMethods))
				}
			}
			if len(keys) < len(want) {
				t.Fatalf("body keys = %v; fixture requires the %d-field tail %v", keys, len(want), want)
			}
			assertKeyOrder(t, method+" body tail (no _x_mode)", keys[len(keys)-len(want):], want)
		})
	}
}

func TestGolden_BodyXReasonAbsentOnNeitherFlagEndpoints(t *testing.T) {
	shape := loadRequestShape(t)

	// Same derivation discipline as the _x_mode test: the two-field
	// tail comes out of body_trailing_field_order by removing both
	// leading fields in place, never restated as a literal.
	var want []string
	for _, k := range shape.WorkspaceAPI.BodyTrailingFieldOrder {
		if k != "_x_reason" && k != "_x_mode" {
			want = append(want, k)
		}
	}
	if len(want) != len(shape.WorkspaceAPI.BodyTrailingFieldOrder)-2 {
		t.Fatalf("fixture's body_trailing_field_order %v does not contain both "+
			"_x_reason and _x_mode; this test would then assert the wrong tail",
			shape.WorkspaceAPI.BodyTrailingFieldOrder)
	}

	for _, method := range shape.WorkspaceAPI.BodyXReasonAbsentMethods {
		t.Run(method, func(t *testing.T) {
			env := NewEnvelope()
			env.SetTeamID("T04T4TH8W")
			req := goldenReq(t, env, "rands-leadership.slack.com", "/api/"+method,
				"application/x-www-form-urlencoded", "token=xoxc-redacted", "")

			raw, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			keys := queryKeyOrder(string(raw))
			for _, k := range keys {
				if k == "_x_reason" {
					t.Fatalf("body = %q carries _x_reason; the captures show %s sending "+
						"none (it is one of the %d endpoints in the fixture's "+
						"body_x_reason_absent_methods)", raw, method,
						len(shape.WorkspaceAPI.BodyXReasonAbsentMethods))
				}
			}
			if len(keys) < len(want) {
				t.Fatalf("body keys = %v; fixture requires the %d-field tail %v", keys, len(want), want)
			}
			assertKeyOrder(t, method+" body tail (neither flag)", keys[len(keys)-len(want):], want)
		})
	}
}

func TestGolden_XReasonAbsentMethodsAreASubsetOfXModeAbsentMethods(t *testing.T) {
	// The fixture's own statement of the hierarchy, checked against
	// itself. body_x_reason_invariant records that the captures hold
	// zero requests carrying _x_mode without _x_reason; if these two
	// lists ever stopped nesting, the fixture would be describing a
	// wire shape the official client never produces.
	shape := loadRequestShape(t)
	modeAbsent := make(map[string]struct{}, len(shape.WorkspaceAPI.BodyXModeAbsentMethods))
	for _, m := range shape.WorkspaceAPI.BodyXModeAbsentMethods {
		modeAbsent[m] = struct{}{}
	}
	for _, m := range shape.WorkspaceAPI.BodyXReasonAbsentMethods {
		if _, ok := modeAbsent[m]; !ok {
			t.Errorf("fixture: %q is in body_x_reason_absent_methods but not in "+
				"body_x_mode_absent_methods; that is the (_x_reason=false, _x_mode=true) "+
				"combination the captures show 0 times in 163", m)
		}
	}
	if len(shape.WorkspaceAPI.BodyXReasonAbsentMethods) >= len(shape.WorkspaceAPI.BodyXModeAbsentMethods) {
		t.Errorf("fixture: body_x_reason_absent_methods (%d) is not a STRICT subset of "+
			"body_x_mode_absent_methods (%d); the captures show a 2-endpoint middle tier",
			len(shape.WorkspaceAPI.BodyXReasonAbsentMethods),
			len(shape.WorkspaceAPI.BodyXModeAbsentMethods))
	}
}
