// Package slackhttp owns the two distinct header sets a recent desktop
// Chrome sends to Slack, and the http.RoundTripper that applies one of them.
//
//   - BrowserTransport decorates outbound HTTP requests to *.slack.com with
//     the fetch/XHR set (browserHeaderPairs).
//   - WebSocketHeaders returns the strictly smaller set Chrome sends on a
//     WebSocket upgrade, for the gorilla/websocket dialer, which cannot go
//     through an http.RoundTripper.
//
// The two sets are deliberately different — real Chrome omits Accept,
// Sec-Fetch-*, sec-ch-ua*, and Priority on a WS handshake — so they must not
// be merged. The goal is to make xoxc-token traffic indistinguishable from
// official browser-client traffic at the header level, so Enterprise Grid
// anomaly detectors don't flag slk as a non-browser client and sign the user
// out.
//
// See: docs/superpowers/plans/2026-05-20-browser-like-headers.md and GitHub
// issue #5 for context.
package slackhttp

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
)

// BrowserTransport wraps an inner http.RoundTripper and adds browser-like
// headers to requests bound for *.slack.com hosts. It never overwrites
// headers the caller has already set, so caller-controlled values like
// Authorization, Cookie, or a custom User-Agent for diagnostics survive.
type BrowserTransport struct {
	// Inner is the underlying transport that actually performs the round
	// trip. If nil, http.DefaultTransport is used.
	Inner http.RoundTripper

	// Env supplies the Slack client telemetry envelope (_x_id, _x_csid,
	// slack_route, ...). If nil, no envelope params are added — asset
	// fetches to CDN hosts carry no envelope. Even when non-nil, the
	// envelope is scoped to edgeapi and to /api/ paths, so a stray Env
	// on a files.slack.com client cannot decorate download URLs.
	Env *Envelope

	// Dest selects which header set to apply. The zero value is
	// DestXHR, so a BrowserTransport constructed without naming it
	// keeps the fetch/XHR behaviour every existing call site relies
	// on.
	Dest Dest

	// Counter, if non-nil, tallies every API request that passes
	// through. This transport is the single chokepoint beneath both
	// slack-go and the hand-rolled postForm path, so it is the one
	// place where a whole-process call count can be taken — which is
	// what Phase 2b's success criteria are stated in.
	//
	// The zero value is nil — a BrowserTransport built as a literal
	// counts nothing, which every pre-existing construction site
	// relies on. The constructors below attach DefaultCounter, which
	// is how the tally reaches production; set this explicitly to
	// tally into an isolated Counter instead.
	Counter *Counter
}

// Dest is the browser fetch destination a transport is imitating.
// Chrome sends materially different headers per destination, so a
// single "browser-like" set is wrong for at least one of them: an
// avatar sent with Sec-Fetch-Dest: empty and no Referer is no more
// browser-like than a bare Go client, just differently wrong.
//
// This mirrors the split already made for the WebSocket handshake
// (WebSocketHeaders), for the same reason.
type Dest int

const (
	// DestXHR is the fetch/XHR destination: /api/ calls and edgeapi.
	// It is the zero value deliberately — every pre-existing
	// construction site omits Dest and must keep XHR behaviour.
	DestXHR Dest = iota

	// DestImage is an <img> load: avatars, file thumbnails, emoji.
	DestImage
)

// String makes test failures name the destination rather than print an
// integer.
func (d Dest) String() string {
	switch d {
	case DestXHR:
		return "DestXHR"
	case DestImage:
		return "DestImage"
	default:
		return fmt.Sprintf("Dest(%d)", int(d))
	}
}

// headerPairs returns the header set for d.
func (d Dest) headerPairs() map[string]string {
	if d == DestImage {
		return imageHeaderPairs()
	}
	return browserHeaderPairs()
}

// RoundTrip implements http.RoundTripper.
func (t *BrowserTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Before any decoration, and before anything that can return
	// early with an error: the tally counts requests ISSUED, which is
	// the number the Phase 2b criteria are written in. Record ignores
	// non-API URLs itself, so asset traffic does not inflate it.
	if t.Counter != nil && req.URL != nil {
		t.Counter.Record(req.URL.String())
	}
	if (req.URL != nil && isSlackHost(req.URL.Host)) || isSlackHost(req.Host) {
		// Clone the request so we don't mutate the caller's copy — net/http's
		// RoundTripper contract forbids in-place modification.
		req = req.Clone(req.Context())
		// http.Header.Clone() returns nil when its receiver is nil, so a
		// caller who constructed *http.Request as a literal without setting
		// Header would otherwise hit a "nil map" panic on the first Set.
		if req.Header == nil {
			req.Header = http.Header{}
		}
		for k, v := range t.Dest.headerPairs() {
			setIfMissing(req.Header, k, v)
		}
		// applyEnvelopeQuery and applyEnvelopeBody both dereference
		// req.URL. http.Client.Do rejects a nil URL before the
		// transport sees it, but a caller invoking RoundTrip directly
		// can reach here with one, and a panic is a poor answer to a
		// malformed request.
		//
		// DestImage is excluded outright rather than relying on the
		// path scoping inside applyEnvelope*: an image load carries no
		// _x_* params in any real client, whatever its URL looks like,
		// and "the asset client passes Env: nil" is a convention
		// nothing enforces.
		if t.Env != nil && req.URL != nil && t.Dest != DestImage {
			applyEnvelopeQuery(req, t.Env)
			if err := applyEnvelopeBody(req); err != nil {
				return nil, err
			}
		}
	}
	inner := t.Inner
	if inner == nil {
		inner = http.DefaultTransport
	}
	return inner.RoundTrip(req)
}

// NewBrowserHTTPClient returns an *http.Client wired up with BrowserTransport
// and an optional cookie jar. Use this anywhere an http.Client is needed for
// Slack XHR traffic.
//
// NOT for avatars, thumbnails or emoji: those are image loads, and
// Chrome's image headers differ from its XHR headers in six places.
// Use NewImageHTTPClient.
func NewBrowserHTTPClient(jar http.CookieJar) *http.Client {
	return &http.Client{
		Transport: &BrowserTransport{Inner: http.DefaultTransport, Counter: DefaultCounter},
		Jar:       jar,
	}
}

// NewImageHTTPClient returns an *http.Client for asset fetches —
// avatars, file thumbnails, emoji — carrying Chrome's image header set
// and no telemetry envelope.
//
// Asset traffic dominates by volume: a single boot of the official
// client made 337 CDN requests against 53 workspace-API calls, so
// getting this set wrong is a bigger divergence than getting the API
// set wrong.
func NewImageHTTPClient(jar http.CookieJar) *http.Client {
	return &http.Client{
		Transport: &BrowserTransport{Inner: http.DefaultTransport, Dest: DestImage, Counter: DefaultCounter},
		Jar:       jar,
	}
}

// WebSocketHeaders returns the headers Chrome sends on a WebSocket
// upgrade to Slack. This is deliberately a SMALLER set than the HTTP
// set in browserHeaderPairs: Chrome omits Accept, all Sec-Fetch-*
// headers, all sec-ch-ua* client hints, and Priority on a WS handshake.
//
// Verified against the status-101 upgrade requests in the 2026-07-30
// captures. See docs/superpowers/specs/2026-07-30-enterprise-grid-bootstrap-design.md.
//
// gorilla/websocket's Dialer owns Connection, Upgrade, and the
// Sec-Websocket-* set — it rejects a caller-supplied duplicate of any of
// them — so those are absent here by design. Host is NOT in that list:
// gorilla explicitly honors a caller-supplied Host, so omitting it here
// is a choice, not a constraint.
func WebSocketHeaders() http.Header {
	h := http.Header{}
	h.Set("User-Agent", UserAgent())
	h.Set("Accept-Language", "en-US,en;q=0.9")
	h.Set("Cache-Control", "no-cache")
	h.Set("Pragma", "no-cache")
	h.Set("Origin", "https://app.slack.com")
	return h
}

// browserHeaderPairs is the single source of truth for the headers a
// Chrome tab sends on a same-site XHR to Slack. RoundTrip is its only
// consumer.
//
// The WebSocket upgrade deliberately does NOT consume this — see
// WebSocketHeaders. Chrome's WS handshake omits Accept, Sec-Fetch-*,
// sec-ch-ua*, and Priority, so sharing this set with the WS path would
// make the socket separable rather than consistent.
//
// Deliberately contains NO Referer: the official web client sends none
// on /api/ calls, and slk sending one made it separable. Verified
// across all 8 of the 2026-07-30 HAR captures: 279 requests to
// *.slack.com/api/* and edgeapi.slack.com, zero with a Referer.
//
// Caveat for anyone re-deriving that number: Chrome DevTools records an
// EMPTY `referer:` key on requests it aborted (status 0, e.g. during the
// deliberate network-outage capture). Those are not Referers. The only
// two non-empty Referers anywhere in the captures are a webfont pointing
// at its CSS and an image on slack-imgs.com — static subresources, not
// API calls. See
// docs/superpowers/specs/2026-07-30-enterprise-grid-bootstrap-design.md.
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

// imageHeaderPairs is the set Chrome sends on an <img> load from
// Slack — avatars, file thumbnails, emoji. Measured across 40
// files.slack.com 200-responses in the 2026-07-30 captures.
//
// Six values differ from browserHeaderPairs, and each difference is
// load-bearing:
//
//	Accept          the image list, not */*
//	Sec-Fetch-Dest  image, not empty
//	Sec-Fetch-Mode  no-cors, not cors
//	Priority        i, not u=1, i
//	Referer         PRESENT — https://app.slack.com/ — where the XHR
//	                set deliberately has none
//	Origin          ABSENT — a no-cors fetch carries no Origin, where
//	                the XHR set sends one
//
// The Referer asymmetry is the easy one to get backwards. The
// official client sends no Referer on /api/ calls, which is why
// browserHeaderPairs omits it; Chrome DOES send one on image
// requests. Applying the XHR set here therefore stripped a header
// real browsers send, making asset fetches LESS browser-like than
// before that removal.
//
// Chrome also sends `dnt: 1` on these requests in the captures. It is
// deliberately not reproduced: DNT reflects a user's browser
// preference, not the client's identity, and most Chrome installs
// leave it off. Sending it would narrow slk to the subset of users who
// enabled it, which is a signature rather than camouflage.
//
// Accept-Encoding is likewise absent, matching browserHeaderPairs:
// net/http manages it and transparently decompresses the response.
// Overriding it with Chrome's "gzip, deflate, br, zstd" would leave
// bodies in an encoding this process cannot decode.
func imageHeaderPairs() map[string]string {
	return map[string]string{
		"User-Agent":         UserAgent(),
		"Accept":             "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8",
		"Accept-Language":    "en-US,en;q=0.9",
		"Referer":            "https://app.slack.com/",
		"Sec-Fetch-Site":     "same-site",
		"Sec-Fetch-Mode":     "no-cors",
		"Sec-Fetch-Dest":     "image",
		"Sec-Ch-Ua":          ClientHintUA(),
		"Sec-Ch-Ua-Mobile":   "?0",
		"Sec-Ch-Ua-Platform": ClientHintPlatform(),
		"Cache-Control":      "no-cache",
		"Pragma":             "no-cache",
		"Priority":           "i",
	}
}

// chromeMajor is the Chrome major version slk impersonates. Both the
// User-Agent string and the sec-ch-ua client hints interpolate it, so
// their *version numbers* cannot drift apart — a Chrome UA paired with
// absent or mismatched client hints is a combination real Chrome never
// emits, and is trivially detectable.
//
// Only the version number is derived from this constant. The rest of
// the sec-ch-ua value — the GREASE brand token and the ordering of the
// three brand entries — is hardcoded in ClientHintUA, and Chrome
// permutes both between major versions. Chrome 147 sent
// `"Google Chrome";v="147", "Not.A/Brand";v="8", "Chromium";v="147"`;
// Chrome 150 sends `"Not;A=Brand";v="8", "Chromium";v="150",
// "Google Chrome";v="150"` — a different token and a different order.
//
// So do NOT bump this constant on its own. Doing so yields a correct
// UA paired with a sec-ch-ua no real Chrome emits, which is a stable,
// slk-specific fingerprint: worse than sending nothing. A bump
// requires a fresh capture of the real client, with ClientHintUA
// updated to match. See the "Verified impersonation values" section of
// docs/superpowers/specs/2026-07-30-enterprise-grid-bootstrap-design.md.
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
		// Linux and anything else (freebsd, openbsd, ...) → Linux UA.
		return fmt.Sprintf(tmpl, "X11; Linux x86_64", chromeMajor)
	}
}

// ClientHintUA returns the sec-ch-ua header value paired with
// UserAgent(). The brand list — the GREASE token, the three entries and
// their order — reproduces what Chrome 150 was observed sending in
// captures of the Slack web client taken 2026-07-30; only the version
// number comes from chromeMajor. Because Chrome varies the GREASE token
// and the ordering per major version, this string is correct for the
// captured version only: see the chromeMajor doc comment before
// changing either.
func ClientHintUA() string {
	return fmt.Sprintf(`"Not;A=Brand";v="8", "Chromium";v="%s", "Google Chrome";v="%s"`,
		chromeMajor, chromeMajor)
}

// ClientHintPlatform returns the sec-ch-ua-platform value for the host OS.
func ClientHintPlatform() string {
	return clientHintPlatformForGOOS(runtime.GOOS)
}

// clientHintPlatformForGOOS is split out so every branch is testable on
// any host, matching the userAgentForGOOS pattern. The quotes are part
// of the header value: sec-ch-ua-platform is a structured-header
// string, so Chrome sends `"Linux"`, not bare Linux.
func clientHintPlatformForGOOS(goos string) string {
	switch goos {
	case "darwin":
		return `"macOS"`
	case "windows":
		return `"Windows"`
	default:
		// Linux and anything else (freebsd, openbsd, ...) → Linux.
		return `"Linux"`
	}
}

// isEdgeAPIHost reports whether host is Slack's edge cache API, which
// takes a different (much smaller) envelope than the workspace API.
func isEdgeAPIHost(host string) bool {
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	return host == "edgeapi.slack.com"
}

// envelopeHost returns the host req is logically addressed to.
//
// req.Host takes precedence over req.URL.Host because that is what the
// server sees: net/http sends req.Host as the Host header whenever it
// is non-empty, regardless of the address actually dialed. RoundTrip's
// Slack-host gate accepts a match on either field, so classifying the
// envelope on URL.Host alone would send the workspace param set to a
// request whose Host header says edgeapi.
func envelopeHost(req *http.Request) string {
	if req.Host != "" {
		return req.Host
	}
	if req.URL != nil {
		return req.URL.Host
	}
	return ""
}

// isWorkspaceAPIPath reports whether path is a workspace API call, the
// only place the workspace envelope belongs.
//
// Without this scope check, any Slack host that isn't edgeapi gets the
// workspace set — so an Envelope accidentally attached to the client
// used for files.slack.com downloads would decorate every asset URL
// with _x_id and slack_route. No real client does that, and "asset
// clients pass Env: nil" is a convention nothing enforced.
func isWorkspaceAPIPath(path string) bool {
	return strings.HasPrefix(path, "/api/")
}

// envelopeParam is one query param in the order the official client
// emits it.
type envelopeParam struct{ key, value string }

// applyEnvelopeQuery appends Slack's client telemetry params to req's
// URL in the ORDER the official client emits them, never overwriting a
// param the caller already set.
//
// Order matters. Across the 2026-07-30 captures, 0 of 163 workspace-API
// requests carried alphabetically-sorted params; the client emits one
// canonical sequence with optional members omitted in place, and fp /
// _x_num_retries always last. url.Values.Encode() sorts keys, so using
// it here would give the envelope a perfectly alphabetized order — a
// stable distributional signature, which is exactly what this package
// exists to remove.
//
// Scope: this orders the params it APPENDS, and nothing else. Params
// the caller already put on the URL keep the caller's order, and on
// slack-go's GET path (misc.go getResource) that order is
// url.Values.Encode()'s — i.e. sorted. In practice almost every slk
// API call is a POST whose business params ride in the body, so the
// query string is envelope-only; chat.getPermalink is the exception.
//
// Host classes take different sets — measured, not assumed (see
// testdata/capture-evidence.json):
//
//	workspace API (*.slack.com/api/*), 163 requests:
//	  _x_id, _x_csid, slack_route, _x_version_ts, _x_foreground,
//	  _x_frontend_build_type, _x_desktop_ia, _x_gantry,
//	  _x_b3_traceid, _x_b3_spanid, _x_b3_sampled, fp, _x_num_retries
//	  (_x_csid/slack_route/_x_b3_* are post-boot only)
//
//	edgeapi.slack.com, 116 requests:
//	  _x_app_name, _x_b3_traceid, _x_b3_spanid, _x_b3_sampled,
//	  fp, _x_num_retries
//	  never: _x_id, _x_version_ts, slack_route, _x_csid, or any
//	         _x_frontend_build_type/_x_desktop_ia/_x_gantry
//
// Sending the workspace set to edgeapi would be an slk-specific
// signature, as would sending it to a non-API path.
func applyEnvelopeQuery(req *http.Request, env *Envelope) {
	existing := req.URL.Query()
	var out []envelopeParam

	add := func(key, value string) {
		if value == "" || existing.Get(key) != "" {
			return
		}
		out = append(out, envelopeParam{key, value})
	}

	postBoot := env.TeamID() != ""

	switch {
	case isEdgeAPIHost(envelopeHost(req)):
		add("_x_app_name", "client")
	case isWorkspaceAPIPath(req.URL.Path):
		add("_x_id", env.RequestID())
		if postBoot {
			add("_x_csid", env.SessionID())
			add("slack_route", env.TeamID())
		}
		add("_x_version_ts", env.VersionTS())
		// The real client varies _x_foreground with browser tab focus
		// (145/163 carry true). A TUI has no equivalent notion, and
		// omitting a param present on 88% of traffic is the larger
		// divergence, so always send true.
		add("_x_foreground", "true")
		add("_x_frontend_build_type", "current")
		add("_x_desktop_ia", "4")
		add("_x_gantry", "true")
	default:
		// A Slack host that is neither edgeapi nor an API path —
		// files.slack.com and the CDNs. Real clients send no envelope
		// there, not even fp, so send nothing at all.
		return
	}

	// B3 trace ids appear on only 14-18% of real requests, but they are
	// per-request random values rather than constants, so over-sending
	// is much less identifying than emitting a wrong fixed value.
	if postBoot {
		trace, span := env.TraceIDs()
		add("_x_b3_traceid", trace)
		add("_x_b3_spanid", span)
		add("_x_b3_sampled", "1")
	}

	// Always last, on both host classes.
	add("fp", "6e")
	add("_x_num_retries", "0")

	req.URL.RawQuery = appendQuery(req.URL.RawQuery, out)
}

// applyEnvelopeBody appends Slack's client telemetry fields to a
// form-encoded POST body, in the order the official client emits them.
//
// Order matters for the same reason it does in the query string: the
// captured trailing sequence is _x_reason, _x_mode, _x_sonic,
// _x_app_name (149/163 requests), with business params first.
// url.Values.Encode() sorts alphabetically, which would put
// _x_app_name first and token last — an order no real client produces.
//
// The tail is not fixed-width: the other 14 of 163 bodies carry no
// _x_mode, and on those the remaining three keep this relative order
// (_x_reason, _x_sonic, _x_app_name). mode.go owns which endpoints
// those are.
//
// Only application/x-www-form-urlencoded bodies are touched. Multipart
// bodies (file uploads) and the JSON bodies edgeapi takes as
// text/plain pass through untouched; rewriting either would corrupt
// them.
//
// Two known residual divergences, both deferred, both recorded in the
// residual-divergence table in
// docs/superpowers/plans/2026-07-30-grid-parity-phase1-outcomes.md:
//
//   - All 163 captured bodies are multipart/form-data while slk sends
//     urlencoded.
//   - Only this four-field tail is ordered. The business params AHEAD
//     of it are alphabetical, because every body slk sends is built
//     with url.Values.Encode(): slack.Client.postForm for the
//     hand-rolled endpoints, slack-go's own misc.go postForm for the
//     rest. So slk emits e.g.
//     `channel=…&include_all_metadata=0&inclusive=0&limit=50&token=…`
//     followed by this tail. Reordering only the bodies this repo
//     builds would leave slack-go's sorted and give slk two
//     distinguishable body shapes rather than one; the multipart
//     conversion rebuilds every body here, at the chokepoint, and is
//     where that gets fixed for all of them at once. Pinned by
//     TestPostForm_BodyFieldOrderIsAlphabeticalThenEnvelope.
func applyEnvelopeBody(req *http.Request) error {
	if req.Body == nil {
		return nil
	}
	if !isWorkspaceAPIPath(req.URL.Path) || isEdgeAPIHost(envelopeHost(req)) {
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

	existing, err := url.ParseQuery(string(raw))
	if err != nil {
		// Not parseable as a form; pass it through rather than
		// corrupting it.
		setBody(req, string(raw))
		return nil
	}

	var out []envelopeParam
	add := func(key, value string) {
		if value == "" || existing.Get(key) != "" {
			return
		}
		out = append(out, envelopeParam{key, value})
	}
	// _x_reason is caller intent, but almost no caller supplies it, and
	// a body carrying _x_mode with no _x_reason is a shape the real
	// client produces on 0 of 163 requests. Falling back to the
	// endpoint's observed reason keeps slk inside the 153/163 majority
	// instead of pinning it to a shape the client never emits. An
	// explicit WithReason beats the fallback.
	//
	// _x_reason is NOT universal either: 10 of the 163 captured bodies
	// omit it, split cleanly across five boot-phase endpoints that also
	// omit _x_mode. On those, no reason is emitted at all — not the
	// endpoint default and not an explicit WithReason. reason.go owns
	// that set and explains why the exclusion outranks caller intent.
	method := methodFromPath(req.URL.Path)
	if sendsXReason(method) {
		reason := ReasonFrom(req.Context())
		if reason == "" {
			reason = defaultReason(method)
		}
		add("_x_reason", reason)
	}
	// _x_mode is NOT universal: 14 of the 163 captured form bodies
	// omit it, split cleanly across seven boot-phase endpoints. See
	// mode.go, which owns that set and the caveat about what the
	// captures cannot distinguish.
	if sendsXMode(method) {
		add("_x_mode", "online")
	}
	add("_x_sonic", "true")
	add("_x_app_name", "client")

	setBody(req, appendQuery(string(raw), out))
	return nil
}

// setBody replaces req's body with s, keeping ContentLength and
// GetBody consistent so net/http can replay it on redirect or HTTP/2
// retry.
func setBody(req *http.Request, s string) {
	req.Body = io.NopCloser(strings.NewReader(s))
	req.ContentLength = int64(len(s))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(s)), nil
	}
}

// appendQuery appends params to an existing raw query string without
// reordering or re-encoding what is already there.
func appendQuery(raw string, params []envelopeParam) string {
	if len(params) == 0 {
		return raw
	}
	var b strings.Builder
	b.WriteString(raw)
	for _, p := range params {
		if b.Len() > 0 {
			b.WriteByte('&')
		}
		b.WriteString(url.QueryEscape(p.key))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(p.value))
	}
	return b.String()
}

func isSlackHost(host string) bool {
	if host == "" {
		return false
	}
	// Strip any :port suffix.
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	return host == "slack.com" || strings.HasSuffix(host, ".slack.com")
}

func setIfMissing(h http.Header, key, value string) {
	if h.Get(key) == "" {
		h.Set(key, value)
	}
}
