# Grid Parity Phase 1: Request Envelope Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every slk request to Slack indistinguishable from official web-client traffic at the header and query-param level, so slk cannot be separated from client traffic by a single log predicate.

**Architecture:** All changes land in `internal/slackhttp`, which already sits beneath both API call paths (slack-go at `internal/slack/client.go:101` and hand-rolled `postForm` at `client.go:1320`). A new `Envelope` type holds per-session state (client id, session id, team id, build version) and `BrowserTransport` consumes it to decorate outbound requests. One integration point in `internal/slack` sets the team id after `Connect`, and one new client method sources the build version from `client.shouldReload`.

**Tech Stack:** Go, `net/http.RoundTripper`, `sync/atomic`, standard `testing`.

**Spec:** `docs/superpowers/specs/2026-07-30-enterprise-grid-bootstrap-design.md` (Layer 1)

**Ships independently.** This phase does not depend on Phase 2 or 3 and is safe to release alone. It is also the cheapest read on whether Grid detection is behavioral or TLS-level.

---

## Observed Contract

From the HAR captures, every official request carries these query params:

| Param | Value | When |
|---|---|---|
| `_x_id` | `noversion-<unix>.<micros>` before boot, `<8-hex>-<unix>.<micros>` after | always |
| `_x_version_ts` | build timestamp, e.g. `1785403654` | always |
| `_x_frontend_build_type` | `current` | always |
| `_x_desktop_ia` | `4` | always |
| `_x_gantry` | `true` | always |
| `fp` | `6e` | always |
| `_x_num_retries` | `0` | always |
| `slack_route` | team id | **only after boot** |
| `_x_csid` | 11-char session id | **only after boot** |
| `_x_b3_traceid` | 32 hex | post-boot calls |
| `_x_b3_spanid` | 16 hex | post-boot calls |
| `_x_b3_sampled` | `1` | post-boot calls |

And these POST body fields: `_x_sonic=true`, `_x_app_name=client`, `_x_mode=online`, `_x_reason=<ui-trigger>`.

The pre-boot / post-boot split is real and verified: `experiments.getByUser` at t+3.0s in `initial-load.har` has `_x_id=noversion-…` with no `slack_route` and no `_x_csid`; `sfdc.integration.listOrgs` at t+4.6s has `_x_id=741e4b14-…&slack_route=T04T4TH8W`. Replicating that transition is more faithful than always sending the post-boot form.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/slackhttp/envelope.go` (create) | `Envelope` type: session identity + build version, concurrency-safe |
| `internal/slackhttp/envelope_test.go` (create) | Envelope unit tests |
| `internal/slackhttp/transport.go` (modify) | Header parity + envelope injection into URL and body |
| `internal/slackhttp/transport_test.go` (modify) | Header + param assertions |
| `internal/slackhttp/reason.go` (create) | `context.Context` carrier for `_x_reason` |
| `internal/slackhttp/reason_test.go` (create) | Reason context tests |
| `internal/slack/client.go` (modify) | Wire `Envelope` into the client; set team id after `Connect`; add `ShouldReload` |
| `internal/config/config.go` (modify) | `Workspace.VersionTS` persistence field |
| `cmd/slk/save_version_ts.go` (create) | Textual TOML rewrite for `version_ts`, mirroring `save_width.go` |
| `cmd/slk/save_version_ts_test.go` (create) | Setter tests |

---

## Task 1: Chrome version constants coupled to client hints

**Files:**
- Modify: `internal/slackhttp/transport.go:86-101`
- Test: `internal/slackhttp/transport_test.go`

A Chrome UA with no `sec-ch-ua` header is a combination real Chrome never emits, and slk's UA is pinned to Chrome/120 (30 major versions stale as of the captures). Both must move together, so they derive from one constant.

- [ ] **Step 1: Write the failing test**

Append to `internal/slackhttp/transport_test.go`:

```go
func TestUserAgentAndClientHintsShareMajorVersion(t *testing.T) {
	ua := UserAgent()
	// Extract "150" from ".../Chrome/150.0.0.0 Safari/..."
	m := regexp.MustCompile(`Chrome/(\d+)\.`).FindStringSubmatch(ua)
	if m == nil {
		t.Fatalf("UserAgent() = %q; no Chrome/<major> found", ua)
	}
	uaMajor := m[1]

	hint := ClientHintUA()
	if !strings.Contains(hint, `"Chromium";v="`+uaMajor+`"`) {
		t.Errorf("ClientHintUA() = %q; want it to contain Chromium v=%q", hint, uaMajor)
	}
	if !strings.Contains(hint, `"Google Chrome";v="`+uaMajor+`"`) {
		t.Errorf("ClientHintUA() = %q; want it to contain Google Chrome v=%q", hint, uaMajor)
	}
}

func TestChromeMajorIsCurrent(t *testing.T) {
	// Guards against the Chrome/120-in-2026 staleness that made slk
	// separable. Bump chromeMajor (and this floor) at release time.
	const floor = 150
	n, err := strconv.Atoi(chromeMajor)
	if err != nil {
		t.Fatalf("chromeMajor = %q; not an integer", chromeMajor)
	}
	if n < floor {
		t.Errorf("chromeMajor = %d; want >= %d (stale UA is itself an anomaly signal)", n, floor)
	}
}
```

Add `"regexp"` and `"strconv"` to that file's imports.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/slackhttp/ -run 'TestUserAgentAndClientHints|TestChromeMajor' -v`
Expected: FAIL — `undefined: ClientHintUA`, `undefined: chromeMajor`.

- [ ] **Step 3: Implement**

In `internal/slackhttp/transport.go`, replace the `UserAgent` / `userAgentForGOOS` block (lines 86-101) with:

```go
// chromeMajor is the Chrome major version slk impersonates. The
// User-Agent string and the sec-ch-ua client hints are both derived
// from it so they can never disagree — a Chrome UA paired with absent
// or mismatched client hints is a combination real Chrome never emits,
// and is trivially detectable. Bump this at release time.
const chromeMajor = "150"

// UserAgent returns a Chrome User-Agent appropriate for the host OS.
func UserAgent() string {
	return userAgentForGOOS(runtime.GOOS)
}

func userAgentForGOOS(goos string) string {
	const tmpl = "Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s.0.0.0 Safari/537.36"
	switch goos {
	case "darwin":
		return fmt.Sprintf(tmpl, "Macintosh; Intel Mac OS X 10_15_7", chromeMajor)
	case "windows":
		return fmt.Sprintf(tmpl, "Windows NT 10.0; Win64; x64", chromeMajor)
	default:
		return fmt.Sprintf(tmpl, "X11; Linux x86_64", chromeMajor)
	}
}

// ClientHintUA returns the sec-ch-ua header value matching UserAgent().
// Format mirrors Chrome 150 exactly, including the GREASE brand.
func ClientHintUA() string {
	return fmt.Sprintf(`"Not;A=Brand";v="8", "Chromium";v="%s", "Google Chrome";v="%s"`,
		chromeMajor, chromeMajor)
}

// ClientHintPlatform returns the sec-ch-ua-platform value for the host OS.
func ClientHintPlatform() string {
	switch runtime.GOOS {
	case "darwin":
		return `"macOS"`
	case "windows":
		return `"Windows"`
	default:
		return `"Linux"`
	}
}
```

Add `"fmt"` to the imports.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/slackhttp/ -run 'TestUserAgentAndClientHints|TestChromeMajor' -v`
Expected: PASS (both).

- [ ] **Step 5: Commit**

```bash
git add internal/slackhttp/transport.go internal/slackhttp/transport_test.go
git commit -m "fix(slackhttp): bump to Chrome 150 and couple UA to client hints"
```

---

## Task 2: Header parity — add client hints, drop Referer

**Files:**
- Modify: `internal/slackhttp/transport.go:29-54` (RoundTrip), `:66-81` (BrowserHeaders)
- Test: `internal/slackhttp/transport_test.go:29-65` (existing test asserts Referer — must change)

The captures show the official client sends **no** `Referer` on API calls, while slk sends one. It also sends `cache-control`, `pragma`, and `priority` which slk omits.

- [ ] **Step 1: Write the failing test**

Append to `internal/slackhttp/transport_test.go`:

```go
func TestBrowserTransport_HeaderParity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()
	client, recorder := newCaptureClient(t, srv)

	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Host = "slack.com"
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	got := recorder.last

	// Present, matching the official client.
	want := map[string]string{
		"Sec-Ch-Ua":          ClientHintUA(),
		"Sec-Ch-Ua-Mobile":   "?0",
		"Sec-Ch-Ua-Platform": ClientHintPlatform(),
		"Cache-Control":      "no-cache",
		"Pragma":             "no-cache",
		"Priority":           "u=1, i",
	}
	for k, v := range want {
		if got.Header.Get(k) != v {
			t.Errorf("header %s = %q; want %q", k, got.Header.Get(k), v)
		}
	}

	// Absent: the official client sends no Referer on API calls.
	if r := got.Header.Get("Referer"); r != "" {
		t.Errorf("Referer = %q; want absent (official client sends none)", r)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/slackhttp/ -run TestBrowserTransport_HeaderParity -v`
Expected: FAIL — `Sec-Ch-Ua = ""`, and `Referer` non-empty.

- [ ] **Step 3: Implement**

In `internal/slackhttp/transport.go`, inside `RoundTrip`, replace the `setIfMissing` block (lines 41-49) with:

```go
		for k, v := range browserHeaderPairs() {
			setIfMissing(req.Header, k, v)
		}
```

Replace the body of `BrowserHeaders()` (lines 66-81) with:

```go
func BrowserHeaders() http.Header {
	h := http.Header{}
	for k, v := range browserHeaderPairs() {
		h.Set(k, v)
	}
	return h
}

// browserHeaderPairs is the single source of truth for the headers a
// Chrome tab sends on a same-site XHR to Slack. Deliberately contains
// NO Referer: the official web client sends none on /api/ calls, and
// sending one made slk separable. Verified across all seven 2026-07-30
// HAR captures.
func browserHeaderPairs() map[string]string {
	return map[string]string{
		"User-Agent":         UserAgent(),
		"Accept":             "*/*",
		"Accept-Language":    "en-US,en;q=0.9",
		"Origin":             "https://app.slack.com",
		"Sec-Fetch-Site":     "same-site",
		"Sec-Fetch-Mode":     "cors",
		"Sec-Fetch-Dest":     "empty",
		"Sec-Ch-Ua":          ClientHintUA(),
		"Sec-Ch-Ua-Mobile":   "?0",
		"Sec-Ch-Ua-Platform": ClientHintPlatform(),
		"Cache-Control":      "no-cache",
		"Pragma":             "no-cache",
		"Priority":           "u=1, i",
	}
}
```

- [ ] **Step 4: Fix the existing test that asserts Referer**

In `internal/slackhttp/transport_test.go`, in `TestBrowserTransport_AddsHeadersToSlackHosts`, delete this block:

```go
	if got.Header.Get("Referer") != "https://app.slack.com/" {
		t.Errorf("Referer = %q; want https://app.slack.com/", got.Header.Get("Referer"))
	}
```

- [ ] **Step 5: Run the full package to verify green**

Run: `go test ./internal/slackhttp/ -v`
Expected: PASS, all tests.

- [ ] **Step 6: Commit**

```bash
git add internal/slackhttp/transport.go internal/slackhttp/transport_test.go
git commit -m "fix(slackhttp): add sec-ch-ua client hints, drop Referer

The official web client sends no Referer on /api/ calls and always
sends the three sec-ch-ua client hints. slk did the inverse, which
separated it from client traffic on headers alone."
```

---

## Task 3: Envelope type

**Files:**
- Create: `internal/slackhttp/envelope.go`
- Test: `internal/slackhttp/envelope_test.go`

Holds per-session identity. Written once at startup and after `Connect`, read from every request goroutine, so reads must be lock-free and safe.

- [ ] **Step 1: Write the failing test**

Create `internal/slackhttp/envelope_test.go`:

```go
package slackhttp

import (
	"regexp"
	"sync"
	"testing"
)

func TestEnvelope_PreBootIdentity(t *testing.T) {
	e := NewEnvelope()
	// Before SetTeamID, _x_id uses the "noversion-" prefix and there is
	// no session id — matching the official client's pre-boot requests.
	id := e.RequestID()
	if !regexp.MustCompile(`^noversion-\d+\.\d{3}$`).MatchString(id) {
		t.Errorf("RequestID() = %q; want noversion-<unix>.<millis>", id)
	}
	if got := e.SessionID(); got != "" {
		t.Errorf("SessionID() = %q; want empty pre-boot", got)
	}
	if got := e.TeamID(); got != "" {
		t.Errorf("TeamID() = %q; want empty pre-boot", got)
	}
}

func TestEnvelope_PostBootIdentity(t *testing.T) {
	e := NewEnvelope()
	e.SetTeamID("T04T4TH8W")

	if got := e.TeamID(); got != "T04T4TH8W" {
		t.Errorf("TeamID() = %q; want T04T4TH8W", got)
	}
	id := e.RequestID()
	if !regexp.MustCompile(`^[0-9a-f]{8}-\d+\.\d{3}$`).MatchString(id) {
		t.Errorf("RequestID() = %q; want <8-hex>-<unix>.<millis> post-boot", id)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`).MatchString(e.SessionID()) {
		t.Errorf("SessionID() = %q; want an 11-char session id post-boot", e.SessionID())
	}
	// Client and session ids are stable across calls within a process.
	if e.SessionID() != e.SessionID() {
		t.Error("SessionID() is not stable across calls")
	}
	a, b := e.RequestID(), e.RequestID()
	if a[:9] != b[:9] {
		t.Errorf("RequestID() client-id prefix changed: %q vs %q", a, b)
	}
}

func TestEnvelope_VersionTSDefaultAndOverride(t *testing.T) {
	e := NewEnvelope()
	if e.VersionTS() != DefaultVersionTS {
		t.Errorf("VersionTS() = %q; want default %q", e.VersionTS(), DefaultVersionTS)
	}
	e.SetVersionTS("1785403654")
	if e.VersionTS() != "1785403654" {
		t.Errorf("VersionTS() = %q; want 1785403654", e.VersionTS())
	}
	// Empty values must not clobber a good one.
	e.SetVersionTS("")
	if e.VersionTS() != "1785403654" {
		t.Errorf("SetVersionTS(\"\") clobbered the value: %q", e.VersionTS())
	}
}

func TestEnvelope_ConcurrentAccess(t *testing.T) {
	e := NewEnvelope()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.SetTeamID("T04T4TH8W")
			e.SetVersionTS("1785403654")
			_ = e.RequestID()
			_ = e.TeamID()
			_ = e.SessionID()
			_ = e.VersionTS()
			_, _ = e.TraceIDs()
		}()
	}
	wg.Wait()
}

func TestEnvelope_TraceIDs(t *testing.T) {
	e := NewEnvelope()
	trace, span := e.TraceIDs()
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(trace) {
		t.Errorf("trace id = %q; want 32 hex chars", trace)
	}
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(span) {
		t.Errorf("span id = %q; want 16 hex chars", span)
	}
	t2, s2 := e.TraceIDs()
	if trace == t2 || span == s2 {
		t.Error("TraceIDs() must return fresh ids per call")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/slackhttp/ -run TestEnvelope -v`
Expected: FAIL — `undefined: NewEnvelope`.

- [ ] **Step 3: Implement**

Create `internal/slackhttp/envelope.go`:

```go
package slackhttp

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"time"
)

// DefaultVersionTS is the fallback Slack build timestamp used before
// client.shouldReload reports the real one. Observed 2026-07-30. It is
// only a seed: Envelope.SetVersionTS replaces it on the first
// successful boot, and the value is persisted per workspace so later
// runs start current. Refresh at release time.
const DefaultVersionTS = "1785403654"

// Envelope carries the per-session values Slack's web client puts on
// every API request. One Envelope per process is correct; it is safe
// for concurrent use.
//
// Identity has two phases, mirroring the official client:
//   - Pre-boot (no team id yet): _x_id uses the "noversion-" prefix,
//     and neither _x_csid nor slack_route is sent.
//   - Post-boot (team id known): _x_id uses an 8-hex client id, and
//     _x_csid and slack_route are sent.
//
// Verified in initial-load.har: experiments.getByUser at t+3.0s has
// _x_id=noversion-… and no slack_route; sfdc.integration.listOrgs at
// t+4.6s has _x_id=741e4b14-… and slack_route=T04T4TH8W.
type Envelope struct {
	clientID  string // 8 hex chars, stable for the process
	sessionID string // 11 chars, stable for the process
	teamID    atomic.Value
	versionTS atomic.Value
}

// NewEnvelope returns an Envelope in the pre-boot phase.
func NewEnvelope() *Envelope {
	e := &Envelope{
		clientID:  randHex(4),  // 4 bytes -> 8 hex chars
		sessionID: randToken(8), // 8 bytes -> 11 base64url chars
	}
	e.teamID.Store("")
	e.versionTS.Store(DefaultVersionTS)
	return e
}

// SetTeamID records the workspace id and moves the envelope into its
// post-boot phase. Ignores empty input.
func (e *Envelope) SetTeamID(id string) {
	if id == "" {
		return
	}
	e.teamID.Store(id)
}

// TeamID returns the workspace id, or "" pre-boot.
func (e *Envelope) TeamID() string {
	s, _ := e.teamID.Load().(string)
	return s
}

// SetVersionTS records the Slack build timestamp reported by
// client.shouldReload. Ignores empty input so a failed lookup cannot
// clobber a good value.
func (e *Envelope) SetVersionTS(ts string) {
	if ts == "" {
		return
	}
	e.versionTS.Store(ts)
}

// VersionTS returns the current build timestamp.
func (e *Envelope) VersionTS() string {
	s, _ := e.versionTS.Load().(string)
	return s
}

// SessionID returns the _x_csid value, or "" pre-boot.
func (e *Envelope) SessionID() string {
	if e.TeamID() == "" {
		return ""
	}
	return e.sessionID
}

// RequestID returns a fresh _x_id for one request.
func (e *Envelope) RequestID() string {
	prefix := "noversion"
	if e.TeamID() != "" {
		prefix = e.clientID
	}
	now := time.Now()
	return fmt.Sprintf("%s-%d.%03d", prefix, now.Unix(), now.Nanosecond()/1e6)
}

// TraceIDs returns a fresh (traceID, spanID) pair for one request.
func (e *Envelope) TraceIDs() (string, string) {
	return randHex(16), randHex(8)
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is not recoverable and not worth
		// propagating through every request; fall back to a
		// time-derived value so requests still carry a plausible id.
		for i := range b {
			b[i] = byte(time.Now().UnixNano() >> (i % 8 * 8))
		}
	}
	return hex.EncodeToString(b)
}

func randToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		for i := range b {
			b[i] = byte(time.Now().UnixNano() >> (i % 8 * 8))
		}
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/slackhttp/ -run TestEnvelope -v -race`
Expected: PASS, all five tests, no race warnings.

- [ ] **Step 5: Commit**

```bash
git add internal/slackhttp/envelope.go internal/slackhttp/envelope_test.go
git commit -m "feat(slackhttp): add Envelope for Slack client telemetry identity"
```

---

## Task 4: `_x_reason` context carrier

**Files:**
- Create: `internal/slackhttp/reason.go`
- Test: `internal/slackhttp/reason_test.go`

`_x_reason` encodes which UI action triggered a call (`message-pane/requestHistory`, `initial-data`, `boot`). It is caller intent, so it rides the context rather than a transport field.

- [ ] **Step 1: Write the failing test**

Create `internal/slackhttp/reason_test.go`:

```go
package slackhttp

import (
	"context"
	"testing"
)

func TestReasonRoundTrip(t *testing.T) {
	ctx := WithReason(context.Background(), "message-pane/requestHistory")
	if got := ReasonFrom(ctx); got != "message-pane/requestHistory" {
		t.Errorf("ReasonFrom = %q; want message-pane/requestHistory", got)
	}
}

func TestReasonDefaultsWhenAbsent(t *testing.T) {
	if got := ReasonFrom(context.Background()); got != "" {
		t.Errorf("ReasonFrom(empty ctx) = %q; want \"\"", got)
	}
}

func TestReasonIgnoresEmpty(t *testing.T) {
	ctx := WithReason(context.Background(), "")
	if got := ReasonFrom(ctx); got != "" {
		t.Errorf("ReasonFrom = %q; want \"\"", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/slackhttp/ -run TestReason -v`
Expected: FAIL — `undefined: WithReason`.

- [ ] **Step 3: Implement**

Create `internal/slackhttp/reason.go`:

```go
package slackhttp

import "context"

// reasonKey is the unexported context key for the _x_reason value.
type reasonKey struct{}

// WithReason returns a context carrying the _x_reason value for the
// request(s) made with it. Slack's web client tags every API call with
// the UI action that triggered it, e.g. "message-pane/requestHistory",
// "unread-counts/onLastReadUpdated", "initial-data", "boot".
func WithReason(ctx context.Context, reason string) context.Context {
	if reason == "" {
		return ctx
	}
	return context.WithValue(ctx, reasonKey{}, reason)
}

// ReasonFrom returns the _x_reason carried by ctx, or "" if none.
func ReasonFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(reasonKey{}).(string)
	return s
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/slackhttp/ -run TestReason -v`
Expected: PASS (all three).

- [ ] **Step 5: Commit**

```bash
git add internal/slackhttp/reason.go internal/slackhttp/reason_test.go
git commit -m "feat(slackhttp): add _x_reason context carrier"
```

---

## Task 5: Inject envelope params into request URLs

**Files:**
- Modify: `internal/slackhttp/transport.go` (`BrowserTransport` struct + `RoundTrip`)
- Test: `internal/slackhttp/transport_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/slackhttp/transport_test.go`:

```go
func newEnvelopeClient(t *testing.T, env *Envelope) (*http.Client, *captureRT) {
	t.Helper()
	recorder := &captureRT{wrapped: http.DefaultTransport}
	bt := &BrowserTransport{Inner: recorder, Env: env}
	return &http.Client{Transport: bt}, recorder
}

func TestEnvelopeParams_PreBoot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	env := NewEnvelope()
	client, recorder := newEnvelopeClient(t, env)

	req, _ := http.NewRequest("POST", srv.URL+"/api/auth.test", nil)
	req.Host = "slack.com"
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	q := recorder.last.URL.Query()
	for k, want := range map[string]string{
		"_x_frontend_build_type": "current",
		"_x_desktop_ia":          "4",
		"_x_gantry":              "true",
		"fp":                     "6e",
		"_x_num_retries":         "0",
		"_x_version_ts":          DefaultVersionTS,
	} {
		if q.Get(k) != want {
			t.Errorf("query %s = %q; want %q", k, q.Get(k), want)
		}
	}
	if !strings.HasPrefix(q.Get("_x_id"), "noversion-") {
		t.Errorf("_x_id = %q; want noversion- prefix pre-boot", q.Get("_x_id"))
	}
	// Pre-boot: these must be absent.
	for _, k := range []string{"slack_route", "_x_csid", "_x_b3_traceid"} {
		if q.Get(k) != "" {
			t.Errorf("query %s = %q; want absent pre-boot", k, q.Get(k))
		}
	}
}

func TestEnvelopeParams_PostBoot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	env := NewEnvelope()
	env.SetTeamID("T04T4TH8W")
	client, recorder := newEnvelopeClient(t, env)

	req, _ := http.NewRequest("POST", srv.URL+"/api/conversations.history", nil)
	req.Host = "slack.com"
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	q := recorder.last.URL.Query()
	if q.Get("slack_route") != "T04T4TH8W" {
		t.Errorf("slack_route = %q; want T04T4TH8W", q.Get("slack_route"))
	}
	if q.Get("_x_csid") == "" {
		t.Error("_x_csid is empty; want a session id post-boot")
	}
	if len(q.Get("_x_b3_traceid")) != 32 {
		t.Errorf("_x_b3_traceid = %q; want 32 hex chars", q.Get("_x_b3_traceid"))
	}
	if len(q.Get("_x_b3_spanid")) != 16 {
		t.Errorf("_x_b3_spanid = %q; want 16 hex chars", q.Get("_x_b3_spanid"))
	}
	if q.Get("_x_b3_sampled") != "1" {
		t.Errorf("_x_b3_sampled = %q; want 1", q.Get("_x_b3_sampled"))
	}
	if strings.HasPrefix(q.Get("_x_id"), "noversion-") {
		t.Errorf("_x_id = %q; want 8-hex prefix post-boot", q.Get("_x_id"))
	}
}

func TestEnvelopeParams_PreservesExistingQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	client, recorder := newEnvelopeClient(t, NewEnvelope())

	req, _ := http.NewRequest("POST", srv.URL+"/api/conversations.history?channel=C123&limit=28", nil)
	req.Host = "slack.com"
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	q := recorder.last.URL.Query()
	if q.Get("channel") != "C123" || q.Get("limit") != "28" {
		t.Errorf("caller query params lost: channel=%q limit=%q", q.Get("channel"), q.Get("limit"))
	}
	if q.Get("fp") != "6e" {
		t.Error("envelope params not added alongside caller params")
	}
}

func TestEnvelopeParams_NotAddedToNonSlackHosts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	client, recorder := newEnvelopeClient(t, NewEnvelope())

	req, _ := http.NewRequest("GET", srv.URL+"/whatever", nil)
	req.Host = "example.com"
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	if recorder.last.URL.Query().Get("fp") != "" {
		t.Error("envelope params leaked to a non-Slack host")
	}
	if recorder.last.Header.Get("Sec-Ch-Ua") != "" {
		t.Error("browser headers leaked to a non-Slack host")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/slackhttp/ -run TestEnvelopeParams -v`
Expected: FAIL — `unknown field Env in struct literal`.

- [ ] **Step 3: Implement**

In `internal/slackhttp/transport.go`, add the `Env` field to the struct:

```go
type BrowserTransport struct {
	// Inner is the underlying transport that actually performs the round
	// trip. If nil, http.DefaultTransport is used.
	Inner http.RoundTripper

	// Env supplies the Slack client telemetry envelope (_x_id, _x_csid,
	// slack_route, ...). If nil, no envelope params are added — useful
	// for asset fetches to CDN hosts, which carry no envelope.
	Env *Envelope
}
```

Then, inside `RoundTrip`, after the header loop and still within the `isSlackHost` branch, add:

```go
		if t.Env != nil {
			applyEnvelopeQuery(req, t.Env)
		}
```

Add this function to the file:

```go
// applyEnvelopeQuery adds Slack's client telemetry params to req's URL,
// never overwriting a param the caller already set. Params that only
// appear post-boot (slack_route, _x_csid, B3 trace ids) are omitted
// until the envelope has a team id, matching the official client.
func applyEnvelopeQuery(req *http.Request, env *Envelope) {
	q := req.URL.Query()
	setQueryIfMissing(q, "_x_id", env.RequestID())
	setQueryIfMissing(q, "_x_version_ts", env.VersionTS())
	setQueryIfMissing(q, "_x_frontend_build_type", "current")
	setQueryIfMissing(q, "_x_desktop_ia", "4")
	setQueryIfMissing(q, "_x_gantry", "true")
	setQueryIfMissing(q, "fp", "6e")
	setQueryIfMissing(q, "_x_num_retries", "0")

	if teamID := env.TeamID(); teamID != "" {
		setQueryIfMissing(q, "slack_route", teamID)
		setQueryIfMissing(q, "_x_csid", env.SessionID())
		trace, span := env.TraceIDs()
		setQueryIfMissing(q, "_x_b3_traceid", trace)
		setQueryIfMissing(q, "_x_b3_spanid", span)
		setQueryIfMissing(q, "_x_b3_sampled", "1")
	}
	req.URL.RawQuery = q.Encode()
}

func setQueryIfMissing(q url.Values, key, value string) {
	if value == "" {
		return
	}
	if q.Get(key) == "" {
		q.Set(key, value)
	}
}
```

Add `"net/url"` to the imports.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/slackhttp/ -v -race`
Expected: PASS, all tests.

- [ ] **Step 5: Commit**

```bash
git add internal/slackhttp/transport.go internal/slackhttp/transport_test.go
git commit -m "feat(slackhttp): inject Slack client envelope params into API URLs"
```

---

## Task 6: Inject envelope fields into POST bodies

**Files:**
- Modify: `internal/slackhttp/transport.go`
- Test: `internal/slackhttp/transport_test.go`

Official POST bodies carry `_x_sonic=true`, `_x_app_name=client`, `_x_mode=online`, and `_x_reason`. Only `application/x-www-form-urlencoded` bodies are rewritten — multipart uploads (`files.upload`) must pass through untouched.

- [ ] **Step 1: Write the failing test**

Append to `internal/slackhttp/transport_test.go`:

```go
func TestEnvelopeBody_AddsFieldsToFormEncoded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	client, recorder := newEnvelopeClient(t, NewEnvelope())

	body := "token=xoxc-abc&channel=C123"
	req, _ := http.NewRequest("POST", srv.URL+"/api/conversations.history", strings.NewReader(body))
	req.Host = "slack.com"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := WithReason(req.Context(), "message-pane/requestHistory")
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	sent, err := io.ReadAll(recorder.last.Body)
	if err != nil {
		t.Fatalf("read captured body: %v", err)
	}
	vals, err := url.ParseQuery(string(sent))
	if err != nil {
		t.Fatalf("ParseQuery(%q): %v", sent, err)
	}
	for k, want := range map[string]string{
		"token":       "xoxc-abc",
		"channel":     "C123",
		"_x_sonic":    "true",
		"_x_app_name": "client",
		"_x_mode":     "online",
		"_x_reason":   "message-pane/requestHistory",
	} {
		if vals.Get(k) != want {
			t.Errorf("body field %s = %q; want %q", k, vals.Get(k), want)
		}
	}
	if recorder.last.ContentLength != int64(len(sent)) {
		t.Errorf("ContentLength = %d; want %d", recorder.last.ContentLength, len(sent))
	}
}

func TestEnvelopeBody_LeavesMultipartAlone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	client, recorder := newEnvelopeClient(t, NewEnvelope())

	const raw = "--BOUNDARY\r\nContent-Disposition: form-data; name=\"file\"\r\n\r\nDATA\r\n--BOUNDARY--\r\n"
	req, _ := http.NewRequest("POST", srv.URL+"/api/files.upload", strings.NewReader(raw))
	req.Host = "slack.com"
	req.Header.Set("Content-Type", "multipart/form-data; boundary=BOUNDARY")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	sent, _ := io.ReadAll(recorder.last.Body)
	if string(sent) != raw {
		t.Errorf("multipart body was rewritten:\ngot  %q\nwant %q", sent, raw)
	}
}

func TestEnvelopeBody_NoBodyIsSafe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	client, _ := newEnvelopeClient(t, NewEnvelope())

	req, _ := http.NewRequest("GET", srv.URL+"/api/auth.test", nil)
	req.Host = "slack.com"
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do with nil body: %v", err)
	}
	resp.Body.Close()
}
```

Add `"io"` to that file's imports.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/slackhttp/ -run TestEnvelopeBody -v`
Expected: FAIL — `_x_sonic` empty.

- [ ] **Step 3: Implement**

In `internal/slackhttp/transport.go`, in `RoundTrip`, immediately after the `applyEnvelopeQuery` call, add:

```go
			if err := applyEnvelopeBody(req); err != nil {
				return nil, err
			}
```

And add:

```go
// applyEnvelopeBody appends Slack's client telemetry fields to a
// form-encoded POST body. Bodies with any other content type — notably
// the multipart bodies used for file uploads — pass through untouched.
//
// Note: re-encoding sorts fields alphabetically, so slk's body field
// ORDER differs from the official client's. Field presence is the
// signal that matters; order is not worth the risk of a hand-rolled
// encoder.
func applyEnvelopeBody(req *http.Request) error {
	if req.Body == nil {
		return nil
	}
	ct := req.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
		return nil
	}

	raw, err := io.ReadAll(req.Body)
	req.Body.Close()
	if err != nil {
		return fmt.Errorf("slackhttp: reading request body: %w", err)
	}
	vals, err := url.ParseQuery(string(raw))
	if err != nil {
		// Not parseable as a form; send it through unchanged rather
		// than corrupting it.
		req.Body = io.NopCloser(bytes.NewReader(raw))
		req.ContentLength = int64(len(raw))
		return nil
	}

	setFormIfMissing(vals, "_x_sonic", "true")
	setFormIfMissing(vals, "_x_app_name", "client")
	setFormIfMissing(vals, "_x_mode", "online")
	setFormIfMissing(vals, "_x_reason", ReasonFrom(req.Context()))

	encoded := vals.Encode()
	req.Body = io.NopCloser(strings.NewReader(encoded))
	req.ContentLength = int64(len(encoded))
	// GetBody lets net/http replay the body on redirect or HTTP/2 retry.
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(encoded)), nil
	}
	return nil
}

func setFormIfMissing(v url.Values, key, value string) {
	if value == "" {
		return
	}
	if v.Get(key) == "" {
		v.Set(key, value)
	}
}
```

Add `"bytes"` and `"io"` to the imports.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/slackhttp/ -v -race`
Expected: PASS, all tests.

- [ ] **Step 5: Commit**

```bash
git add internal/slackhttp/transport.go internal/slackhttp/transport_test.go
git commit -m "feat(slackhttp): add _x_sonic/_x_app_name/_x_mode/_x_reason to form bodies"
```

---

## Task 7: Wire the Envelope into the Slack client

**Files:**
- Modify: `internal/slack/client.go:98-113` (`NewClient`), `:137-139` (`newCookieHTTPClient`), `:172-191` (`Connect`)
- Test: `internal/slack/client_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/slack/client_test.go`:

```go
func TestClient_EnvelopeTeamIDSetAfterConnect(t *testing.T) {
	c := NewClient("xoxc-test", "d-cookie")
	if c.Envelope() == nil {
		t.Fatal("Envelope() is nil; want a non-nil envelope")
	}
	if got := c.Envelope().TeamID(); got != "" {
		t.Errorf("TeamID() = %q before Connect; want empty", got)
	}
	// Connect sets teamID; simulate what Connect does at client.go:177.
	c.teamID = "T04T4TH8W"
	c.Envelope().SetTeamID(c.teamID)
	if got := c.Envelope().TeamID(); got != "T04T4TH8W" {
		t.Errorf("TeamID() = %q after Connect; want T04T4TH8W", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/slack/ -run TestClient_EnvelopeTeamID -v`
Expected: FAIL — `c.Envelope undefined`.

- [ ] **Step 3: Implement**

In `internal/slack/client.go`, add an `envelope` field to the `Client` struct (next to `httpClient`):

```go
	// envelope supplies Slack client telemetry params on every API
	// request. Shared with the http.Client's BrowserTransport.
	envelope *slackhttp.Envelope
```

Change `newCookieHTTPClient` to accept and use an envelope:

```go
func newCookieHTTPClient(dCookie string, env *slackhttp.Envelope) *http.Client {
	return &http.Client{
		Transport: &slackhttp.BrowserTransport{
			Inner: http.DefaultTransport,
			Env:   env,
		},
		Jar: newCookieJar(dCookie),
	}
}
```

Update `NewClient`:

```go
func NewClient(xoxcToken, dCookie string) *Client {
	env := slackhttp.NewEnvelope()
	httpClient := newCookieHTTPClient(dCookie, env)

	api := slack.New(
		xoxcToken,
		slack.OptionHTTPClient(httpClient),
	)

	return &Client{
		api:        api,
		token:      xoxcToken,
		cookie:     dCookie,
		apiBaseURL: defaultAPIBaseURL,
		httpClient: httpClient,
		envelope:   env,
	}
}

// Envelope returns the client's telemetry envelope.
func (c *Client) Envelope() *slackhttp.Envelope { return c.envelope }
```

In `Connect`, immediately after `c.teamID = resp.TeamID` (line 177), add:

```go
	c.envelope.SetTeamID(c.teamID)
```

Then fix the other `newCookieHTTPClient` caller in `postForm` (line 1332-ish):

```go
	httpClient := c.httpClient
	if httpClient == nil {
		httpClient = newCookieHTTPClient(c.cookie, c.envelope)
	}
```

- [ ] **Step 4: Run to verify it passes and nothing regressed**

Run: `go build ./... && go test ./internal/slack/ ./internal/slackhttp/ -race`
Expected: PASS. If any other `newCookieHTTPClient(` call sites fail to compile, pass `c.envelope` (or `nil` in tests).

- [ ] **Step 5: Commit**

```bash
git add internal/slack/client.go internal/slack/client_test.go
git commit -m "feat(slack): wire telemetry Envelope into the API client"
```

---

## Task 8: Source `_x_version_ts` from `client.shouldReload`

**Files:**
- Modify: `internal/slack/client.go` (add `ShouldReload`)
- Test: `internal/slack/client_test.go`

Sourcing the build timestamp from an API call rather than a page scrape avoids reintroducing the workspace-page navigation removed in `da6a7e1` — the request that #111 showed corporate proxies 403. `client.shouldReload` appears in both boot captures, so calling it is also more faithful.

- [ ] **Step 1: Write the failing test**

Append to `internal/slack/client_test.go`:

```go
func TestShouldReload_ReturnsBuildVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		if got := r.FormValue("build_version_ts"); got == "" {
			t.Error("build_version_ts not sent in body")
		}
		if got := r.FormValue("team_ids"); got != "T04T4TH8W" {
			t.Errorf("team_ids = %q; want T04T4TH8W", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"should_reload":false,"recommended_build_version":1785403654,"build_manifest_last_modified":1785408685}`))
	}))
	defer srv.Close()

	c := NewClient("xoxc-test", "d-cookie")
	c.apiBaseURL = srv.URL + "/api/"
	c.teamID = "T04T4TH8W"
	c.envelope.SetTeamID("T04T4TH8W")

	ts, err := c.ShouldReload(context.Background())
	if err != nil {
		t.Fatalf("ShouldReload: %v", err)
	}
	if ts != "1785403654" {
		t.Errorf("ShouldReload = %q; want 1785403654", ts)
	}
}

func TestShouldReload_ErrorLeavesVersionUnchanged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	c := NewClient("xoxc-test", "d-cookie")
	c.apiBaseURL = srv.URL + "/api/"
	before := c.Envelope().VersionTS()

	if _, err := c.ShouldReload(context.Background()); err == nil {
		t.Error("ShouldReload returned nil error on HTTP 500")
	}
	if after := c.Envelope().VersionTS(); after != before {
		t.Errorf("VersionTS changed on failure: %q -> %q", before, after)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/slack/ -run TestShouldReload -v`
Expected: FAIL — `c.ShouldReload undefined`.

- [ ] **Step 3: Implement**

Add to `internal/slack/client.go`:

```go
// ShouldReload calls client.shouldReload and returns Slack's current
// build timestamp as a string, suitable for Envelope.SetVersionTS.
//
// This is how the official web client learns its build version, and
// how slk keeps _x_version_ts current without fetching the workspace
// page — a navigation request that corporate proxies reject (#111) and
// that was deliberately removed in da6a7e1.
//
// The caller decides whether to store the result; ShouldReload does not
// mutate the envelope, so a failed call cannot clobber a good value.
func (c *Client) ShouldReload(ctx context.Context) (string, error) {
	ctx = slackhttp.WithReason(ctx, "boot")
	form := url.Values{
		"team_ids":         {c.teamID},
		"build_version_ts": {c.envelope.VersionTS()},
	}
	body, err := c.postForm(ctx, "client.shouldReload", form)
	if err != nil {
		return "", fmt.Errorf("client.shouldReload: %w", err)
	}
	var resp struct {
		OK                      bool   `json:"ok"`
		Error                   string `json:"error"`
		RecommendedBuildVersion int64  `json:"recommended_build_version"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("client.shouldReload: decoding %s: %w", truncateForLog(body), err)
	}
	if !resp.OK {
		return "", fmt.Errorf("client.shouldReload: %s", resp.Error)
	}
	if resp.RecommendedBuildVersion == 0 {
		return "", fmt.Errorf("client.shouldReload: no recommended_build_version in %s", truncateForLog(body))
	}
	return strconv.FormatInt(resp.RecommendedBuildVersion, 10), nil
}
```

Add `"strconv"` to the imports if absent.

`postForm` returns the body without checking HTTP status, so add a status check there. In `postForm` (around line 1345), replace `return io.ReadAll(resp.Body)` with:

```go
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s response: %w", method, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d: %s", method, resp.StatusCode, truncateForLog(body))
	}
	return body, nil
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/slack/ -run TestShouldReload -v && go test ./internal/slack/ -race`
Expected: PASS. If pre-existing tests relied on `postForm` ignoring non-200 status, update them to serve 200.

- [ ] **Step 5: Commit**

```bash
git add internal/slack/client.go internal/slack/client_test.go
git commit -m "feat(slack): source _x_version_ts from client.shouldReload

Avoids reintroducing the workspace-page fetch removed in da6a7e1,
which #111 showed corporate proxies reject with 403. shouldReload
appears in both official boot captures."
```

---

## Task 9: Persist `version_ts` per workspace

**Files:**
- Modify: `internal/config/config.go:155-168` (`Workspace` struct)
- Test: `internal/config/config_test.go`

Persisting means the second and later runs start with a current build stamp instead of the compiled-in fallback.

- [ ] **Step 1: Write the failing test**

Append to `internal/config/config_test.go`:

```go
func TestWorkspaceVersionTSRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	contents := `
[workspaces.acme]
team_id = "T04T4TH8W"
version_ts = "1785403654"
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ws, ok := cfg.Workspaces["acme"]
	if !ok {
		t.Fatal("workspace acme not loaded")
	}
	if ws.VersionTS != "1785403654" {
		t.Errorf("VersionTS = %q; want 1785403654", ws.VersionTS)
	}
}

func TestWorkspaceVersionTSOptional(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	contents := `
[workspaces.acme]
team_id = "T04T4TH8W"
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Workspaces["acme"].VersionTS; got != "" {
		t.Errorf("VersionTS = %q; want empty when unset", got)
	}
}
```

`config.Load` takes a path (`internal/config/config.go:218`).

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/config/ -run TestWorkspaceVersionTS -v`
Expected: FAIL — `ws.VersionTS undefined`.

- [ ] **Step 3: Implement**

In `internal/config/config.go`, add to the `Workspace` struct:

```go
	// VersionTS caches the Slack build timestamp last reported by
	// client.shouldReload, sent as _x_version_ts on every API request.
	// Empty means "use the compiled-in fallback and refresh on boot".
	VersionTS string `toml:"version_ts"`
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS, all tests.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): persist per-workspace version_ts"
```

---

## Task 10: Golden fixture test against the captures

**Files:**
- Create: `internal/slackhttp/testdata/official-request-shape.json`
- Create: `internal/slackhttp/golden_test.go`

Turns the HAR analysis into a permanent regression harness, so a future refactor cannot silently drop a param.

- [ ] **Step 1: Create the fixture**

Create `internal/slackhttp/testdata/official-request-shape.json`. These are the exact param and header names observed on `conversations.history` in `channel-switch.har` (post-boot) and `experiments.getByUser` in `initial-load.har` (pre-boot), with values redacted where per-session.

```json
{
  "source": "HAR captures of Slack web client, rands-leadership.slack.com, 2026-07-30",
  "post_boot_query_params": [
    "_x_id",
    "_x_csid",
    "slack_route",
    "_x_version_ts",
    "_x_frontend_build_type",
    "_x_desktop_ia",
    "_x_gantry",
    "_x_b3_traceid",
    "_x_b3_spanid",
    "_x_b3_sampled",
    "fp",
    "_x_num_retries"
  ],
  "pre_boot_query_params": [
    "_x_id",
    "_x_version_ts",
    "_x_frontend_build_type",
    "_x_desktop_ia",
    "_x_gantry",
    "fp",
    "_x_num_retries"
  ],
  "post_boot_absent_query_params": [],
  "pre_boot_absent_query_params": [
    "slack_route",
    "_x_csid",
    "_x_b3_traceid",
    "_x_b3_spanid",
    "_x_b3_sampled"
  ],
  "body_fields": [
    "_x_sonic",
    "_x_app_name",
    "_x_mode",
    "_x_reason"
  ],
  "headers_present": [
    "User-Agent",
    "Accept",
    "Accept-Language",
    "Origin",
    "Sec-Fetch-Site",
    "Sec-Fetch-Mode",
    "Sec-Fetch-Dest",
    "Sec-Ch-Ua",
    "Sec-Ch-Ua-Mobile",
    "Sec-Ch-Ua-Platform",
    "Cache-Control",
    "Pragma",
    "Priority"
  ],
  "headers_absent": [
    "Referer"
  ]
}
```

- [ ] **Step 2: Write the failing test**

Create `internal/slackhttp/golden_test.go`:

```go
package slackhttp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

type requestShape struct {
	Source                    string   `json:"source"`
	PostBootQueryParams       []string `json:"post_boot_query_params"`
	PreBootQueryParams        []string `json:"pre_boot_query_params"`
	PreBootAbsentQueryParams  []string `json:"pre_boot_absent_query_params"`
	BodyFields                []string `json:"body_fields"`
	HeadersPresent            []string `json:"headers_present"`
	HeadersAbsent             []string `json:"headers_absent"`
}

func loadShape(t *testing.T) requestShape {
	t.Helper()
	raw, err := os.ReadFile("testdata/official-request-shape.json")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var s requestShape
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}
	return s
}

// doCapture issues one request through BrowserTransport and returns the
// decorated request as the inner transport saw it.
func doCapture(t *testing.T, env *Envelope, reason string) *http.Request {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	client, recorder := newEnvelopeClient(t, env)

	req, err := http.NewRequest("POST", srv.URL+"/api/conversations.history",
		strings.NewReader("token=xoxc-test&channel=C123"))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Host = "slack.com"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(WithReason(req.Context(), reason))

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	return recorder.last
}

func TestGolden_PostBootRequestMatchesOfficialShape(t *testing.T) {
	shape := loadShape(t)
	env := NewEnvelope()
	env.SetTeamID("T04T4TH8W")
	got := doCapture(t, env, "message-pane/requestHistory")

	q := got.URL.Query()
	for _, p := range shape.PostBootQueryParams {
		if q.Get(p) == "" {
			t.Errorf("post-boot query param %q missing (fixture: %s)", p, shape.Source)
		}
	}
	for _, h := range shape.HeadersPresent {
		if got.Header.Get(h) == "" {
			t.Errorf("header %q missing", h)
		}
	}
	for _, h := range shape.HeadersAbsent {
		if got.Header.Get(h) != "" {
			t.Errorf("header %q = %q; official client sends none", h, got.Header.Get(h))
		}
	}

	body, err := io.ReadAll(got.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	vals, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	for _, f := range shape.BodyFields {
		if vals.Get(f) == "" {
			t.Errorf("body field %q missing", f)
		}
	}
}

func TestGolden_PreBootRequestMatchesOfficialShape(t *testing.T) {
	shape := loadShape(t)
	got := doCapture(t, NewEnvelope(), "boot")

	q := got.URL.Query()
	for _, p := range shape.PreBootQueryParams {
		if q.Get(p) == "" {
			t.Errorf("pre-boot query param %q missing", p)
		}
	}
	for _, p := range shape.PreBootAbsentQueryParams {
		if q.Get(p) != "" {
			t.Errorf("pre-boot query param %q = %q; want absent before boot", p, q.Get(p))
		}
	}
}
```

- [ ] **Step 3: Run to verify it passes**

Run: `go test ./internal/slackhttp/ -run TestGolden -v`
Expected: PASS (both). Tasks 1-6 already implement the behavior; this test locks it in.

- [ ] **Step 4: Run the whole suite**

Run: `go vet ./... && go test ./... -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/slackhttp/testdata/official-request-shape.json internal/slackhttp/golden_test.go
git commit -m "test(slackhttp): lock request shape against official-client captures"
```

---

## Task 11: Refresh and persist `version_ts` on connect

**Files:**
- Modify: `cmd/slk/main.go` (in `connectWorkspace`, after `Connect` succeeds)
- Test: manual verification (this is wiring; the units are covered by Tasks 8 and 9)

- [ ] **Step 1: Seed the envelope from config before Connect**

In `cmd/slk/main.go`, in `connectWorkspace`, immediately after the `slack.NewClient(...)` call that creates the client and before `Connect`, add:

```go
	// Seed the build timestamp from the last run so the very first
	// request of this session already carries a current _x_version_ts.
	if ws, ok := cfg.Workspaces[wsKey]; ok && ws.VersionTS != "" {
		client.Envelope().SetVersionTS(ws.VersionTS)
	}
```

Adjust `cfg`, `wsKey`, and `client` to the identifiers already in scope at that point.

- [ ] **Step 2: Refresh it after Connect**

Immediately after the `Connect` call succeeds (which is where `Envelope().SetTeamID` now fires inside the client), add:

```go
	// Refresh the build timestamp in the background. Failure is
	// non-fatal: the seeded or compiled-in value stays in use.
	go func() {
		ts, err := client.ShouldReload(context.Background())
		if err != nil {
			debuglog.General("shouldReload: %v", err)
			return
		}
		client.Envelope().SetVersionTS(ts)
		if err := saveWorkspaceVersionTS(configPath, wsKey, client.TeamID(), teamName, ts); err != nil {
			debuglog.General("saving version_ts: %v", err)
		}
	}()
```

Use the same `configPath` / `tomlKey` / team-name identifiers that the nearby `saveWorkspaceWidth` and `saveWorkspaceTheme` call sites in `cmd/slk/` already use.

- [ ] **Step 3: Add the config setter**

slk has **no** `config.Save()`. Config is persisted by *textual line rewriting* in `cmd/slk/`, deliberately, so user comments and ordering survive. Follow `saveWorkspaceWidth` (`cmd/slk/save_width.go:14`) exactly — it is the closest analogue (a scalar field on a workspace block).

Create `cmd/slk/save_version_ts.go`:

```go
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// saveWorkspaceVersionTS rewrites or appends a version_ts entry in
// [workspaces.<tomlKey>]. Mirrors saveWorkspaceWidth.
func saveWorkspaceVersionTS(configPath, tomlKey, teamID, teamName, versionTS string) error {
	data, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			return err
		}
		data = nil
	} else if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")

	header := fmt.Sprintf("[workspaces.%s]", tomlKey)

	sectionStart := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == header {
			sectionStart = i
			break
		}
	}

	if sectionStart >= 0 {
		end := len(lines)
		for j := sectionStart + 1; j < len(lines); j++ {
			t := strings.TrimSpace(lines[j])
			if t == "" || strings.HasPrefix(t, "[") {
				end = j
				break
			}
		}
		updated := false
		for j := sectionStart + 1; j < end; j++ {
			t := strings.TrimSpace(lines[j])
			if strings.HasPrefix(t, "version_ts") && strings.Contains(t, "=") {
				lines[j] = "version_ts = " + tomlString(versionTS)
				updated = true
				break
			}
		}
		if !updated {
			newLines := make([]string, 0, len(lines)+1)
			newLines = append(newLines, lines[:sectionStart+1]...)
			newLines = append(newLines, "version_ts = "+tomlString(versionTS))
			newLines = append(newLines, lines[sectionStart+1:]...)
			lines = newLines
		}
		return os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644)
	}

	// No existing section — append a legacy-keyed block.
	if len(lines) > 0 && lines[len(lines)-1] != "" {
		lines = append(lines, "")
	}
	safeName := sanitizeComment(teamName)
	if safeName == "" {
		safeName = teamID
	}
	lines = append(lines,
		"# "+safeName,
		fmt.Sprintf("[workspaces.%s]", teamID),
		"version_ts = "+tomlString(versionTS))
	return os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644)
}
```

`tomlString` and `sanitizeComment` already exist in `cmd/slk/save_theme.go`.

- [ ] **Step 3b: Test the setter**

Create `cmd/slk/save_version_ts_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveWorkspaceVersionTS_UpdatesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	initial := "# My Team\n[workspaces.acme]\nteam_id = \"T04T4TH8W\"\nversion_ts = \"1111111111\"\n"
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := saveWorkspaceVersionTS(path, "acme", "T04T4TH8W", "My Team", "1785403654"); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, _ := os.ReadFile(path)
	if !strings.Contains(string(out), `version_ts = "1785403654"`) {
		t.Errorf("version_ts not updated:\n%s", out)
	}
	if strings.Contains(string(out), "1111111111") {
		t.Errorf("old version_ts still present:\n%s", out)
	}
	if !strings.Contains(string(out), `team_id = "T04T4TH8W"`) {
		t.Errorf("team_id was clobbered:\n%s", out)
	}
	if !strings.Contains(string(out), "# My Team") {
		t.Errorf("comment was lost:\n%s", out)
	}
}

func TestSaveWorkspaceVersionTS_AddsToExistingSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[workspaces.acme]\nteam_id = \"T04T4TH8W\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := saveWorkspaceVersionTS(path, "acme", "T04T4TH8W", "My Team", "1785403654"); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, _ := os.ReadFile(path)
	if !strings.Contains(string(out), `version_ts = "1785403654"`) {
		t.Errorf("version_ts not added:\n%s", out)
	}
}

func TestSaveWorkspaceVersionTS_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.toml")
	if err := saveWorkspaceVersionTS(path, "acme", "T04T4TH8W", "My Team", "1785403654"); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(out), `version_ts = "1785403654"`) {
		t.Errorf("version_ts missing from new file:\n%s", out)
	}
}
```

Run: `go test ./cmd/slk/ -run TestSaveWorkspaceVersionTS -v`
Expected: PASS (all three).

- [ ] **Step 4: Verify end to end**

Run: `go build ./... && SLK_DEBUG=1 ./slk`

Then, after quitting: `grep -c '_x_version_ts' slk-debug.log` — if slk does not log outbound URLs, instead confirm `version_ts` now appears under your workspace in `config.toml`.

Expected: `config.toml` gains a `version_ts` line for the connected workspace; no errors in the debug log.

- [ ] **Step 5: Commit**

```bash
git add cmd/slk/main.go internal/config/
git commit -m "feat: refresh and persist _x_version_ts on workspace connect"
```

---

## Task 12: Full verification

- [ ] **Step 1: Build, vet, test**

```bash
go build ./... && go vet ./... && go test ./... -race
```

Expected: all pass.

- [ ] **Step 2: Confirm no Referer remains anywhere**

```bash
grep -rn 'Referer' internal/ cmd/ --include='*.go'
```

Expected: no matches in non-test code. Any match in `internal/slackhttp` is a regression.

- [ ] **Step 3: Confirm the Chrome version is coupled**

```bash
grep -rn 'Chrome/1' internal/slackhttp/*.go
```

Expected: only the `tmpl` format string in `userAgentForGOOS`; no other hardcoded Chrome version.

- [ ] **Step 4: Commit any fixes, then tag the phase**

```bash
git commit -am "chore: phase 1 verification fixes" || true
```

---

## Self-Review Notes

**Spec coverage.** Layer 1 of the spec lists: header parity (Task 2), envelope query params (Task 5), body params incl. `_x_reason` (Tasks 4, 6), `_x_version_ts` sourcing (Tasks 8, 9, 11), UA/client-hint coupling (Task 1), and golden fixtures (Task 10). All covered.

**Two deviations from the spec, both deliberate:**

1. **`_x_version_ts` comes from `client.shouldReload`, not a page scrape.** The spec says scrape the workspace page. Commit `da6a7e1` removed exactly that fetch, and #111 showed corporate proxies 403 it. `shouldReload` appears in both boot captures, so this is both safer and more faithful. **The spec should be updated to match.**

2. **`_x_b3_*` are query params, not headers.** The spec's Layer 1 table calls them "B3 trace headers". The captures show them in the query string (`channel-switch.har`, `conversations.history`). **The spec should be corrected.**

**Deferred, as the spec states:** multipart body encoding. slk still sends `x-www-form-urlencoded` where the official client sends `multipart/form-data`. Task 6 documents this and the alphabetical field-order difference as known residuals.

**Known risk in Task 8:** adding a status check to `postForm` changes behavior for every hand-rolled endpoint. Any existing test that serves a non-200 and expects a parsed body will now fail — Step 4 calls this out. This is a correctness improvement worth making, but it is the one place in this plan that touches shared code.
