package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"github.com/slack-go/slack"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/gammons/slk/internal/bootstrap"
	"github.com/gammons/slk/internal/cache"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slack/boot"
	"github.com/gammons/slk/internal/slack/edge"
	"github.com/gammons/slk/internal/slackhttp"
)

// newTestClient returns a real *slackclient.Client whose every request
// lands on srv.
//
// srv must be a TLS server. The client's base URLs are https (the
// workspace API defaults to https://slack.com/api/, edge.Client is
// hardcoded to https://edgeapi.slack.com), and neither is settable from
// outside internal/slack — so the redirection happens at the transport,
// by replacing the BrowserTransport's Inner with one that dials srv
// whatever host it was asked for.
//
// Replacing Inner and not the whole Transport is the point: the
// BrowserTransport itself, with its Chrome header set and its telemetry
// envelope, is exactly what these tests are checking survives the
// wiring.
func newTestClient(t *testing.T, srv *httptest.Server) *slackclient.Client {
	t.Helper()
	c := slackclient.NewClient("xoxc-test", "d-cookie")
	bt, ok := c.HTTPClient().Transport.(*slackhttp.BrowserTransport)
	if !ok {
		t.Fatalf("client transport is %T; want *slackhttp.BrowserTransport", c.HTTPClient().Transport)
	}
	addr := srv.Listener.Addr().String()
	bt.Inner = &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
		//nolint:gosec // dialing a local httptest server with a self-signed cert
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	return c
}

// recordedRequest is one request the fake Slack captured.
type recordedRequest struct {
	path   string
	form   url.Values
	query  url.Values
	header http.Header
}

// fakeSlack is a TLS httptest server that answers a fixed body per path
// and records what it was asked.
type fakeSlack struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []recordedRequest
}

func newFakeSlack(t *testing.T, bodies map[string]string) *fakeSlack {
	t.Helper()
	f := &fakeSlack{}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		rec := recordedRequest{
			path:   r.URL.Path,
			form:   r.PostForm,
			query:  r.URL.Query(),
			header: r.Header.Clone(),
		}
		f.mu.Lock()
		f.reqs = append(f.reqs, rec)
		f.mu.Unlock()

		body, ok := bodies[r.URL.Path]
		if !ok {
			t.Errorf("fake Slack got an unexpected request to %s", r.URL.Path)
			http.Error(w, `{"ok":false,"error":"unexpected_path"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeSlack) requestTo(t *testing.T, path string) recordedRequest {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.reqs {
		if r.path == path {
			return r
		}
	}
	var paths []string
	for _, r := range f.reqs {
		paths = append(paths, r.path)
	}
	t.Fatalf("no request to %s; got %v", path, paths)
	return recordedRequest{}
}

// ---------------------------------------------------------------------
// bootAdapter
// ---------------------------------------------------------------------

const userBootBody = `{
  "ok": true,
  "self": {"id":"U1","name":"grant","team_id":"T1"},
  "team": {"id":"T1","name":"Acme","domain":"acme"},
  "channels": [
    {"id":"C1","name":"general","is_channel":true,"updated":1783337533019,"topic":{"value":"talk"}}
  ],
  "ims": [{"id":"D1","user":"U2","is_im":true,"is_open":true,"updated":1783337533020}],
  "is_open": ["C1","D1"],
  "prefs": {"all_notifications_prefs":"{\"channels\":{\"C1\":{\"muted\":true}}}"},
  "emoji_cache_ts": "17833375330191740"
}`

func TestBootAdapter_CallsUserBootAndReturnsParsedResult(t *testing.T) {
	srv := newFakeSlack(t, map[string]string{"/api/client.userBoot": userBootBody})
	c := newTestClient(t, srv.Server)

	res, err := bootAdapter{c}.UserBoot(context.Background())
	if err != nil {
		t.Fatalf("UserBoot: %v", err)
	}
	if res.Self.ID != "U1" || res.Team.Name != "Acme" {
		t.Errorf("self=%+v team=%+v; want U1 / Acme", res.Self, res.Team)
	}
	if len(res.Channels) != 1 || res.Channels[0].Name != "general" {
		t.Errorf("channels = %+v; want one named general", res.Channels)
	}
	if len(res.IMs) != 1 || res.IMs[0].UserID != "U2" {
		t.Errorf("ims = %+v; want one for U2", res.IMs)
	}

	// The token has to reach the form body. postForm injects it, and
	// the adapter is a pass-through precisely so that stays true.
	req := srv.requestTo(t, "/api/client.userBoot")
	if got := req.form.Get("token"); got != "xoxc-test" {
		t.Errorf("token = %q; want the client's xoxc token", got)
	}
	if got := req.form.Get("omit_extras"); got == "" {
		t.Error("omit_extras absent from the body; the adapter is not going through boot.UserBoot")
	}
}

// ---------------------------------------------------------------------
// viewAdapter
// ---------------------------------------------------------------------

const viewBody = `{
  "ok": true,
  "channel": {"id":"C1","name":"general"},
  "history": {"messages":[{"type":"message","ts":"1.1","text":"hi"}],"has_more":false},
  "users": [{"id":"U2","name":"pat","profile":{"display_name":"Pat"}}],
  "emojis": {"party":"https://emoji/party.png"}
}`

func TestViewAdapter_SendsTheChannelAndParsesTheResult(t *testing.T) {
	srv := newFakeSlack(t, map[string]string{"/api/conversations.view": viewBody})
	c := newTestClient(t, srv.Server)

	res, err := viewAdapter{c}.ConversationsView(context.Background(), "C1")
	if err != nil {
		t.Fatalf("ConversationsView: %v", err)
	}
	if res.Channel.ID != "C1" {
		t.Errorf("channel = %q; want C1", res.Channel.ID)
	}
	if len(res.Users) != 1 || res.Users[0].ID != "U2" {
		t.Errorf("users = %+v; want one for U2", res.Users)
	}
	if got := srv.requestTo(t, "/api/conversations.view").form.Get("channel"); got != "C1" {
		t.Errorf("channel param = %q; want C1", got)
	}
}

// ---------------------------------------------------------------------
// countsAdapter
// ---------------------------------------------------------------------

const countsBody = `{
  "ok": true,
  "channels": [{"id":"C1","has_unreads":true,"mention_count":2,"unread_count_display":5,"last_read":"1.0"}],
  "ims": [{"id":"D1","has_unreads":false,"last_read":"2.0"}],
  "threads": {"has_unreads":true,"unread_count":3,"mention_count":1}
}`

func TestCountsAdapter_CarriesUnreadsAndTheThreadRollup(t *testing.T) {
	srv := newFakeSlack(t, map[string]string{"/api/client.counts": countsBody})
	c := newTestClient(t, srv.Server)

	counts, err := countsAdapter{c}.Counts(context.Background())
	if err != nil {
		t.Fatalf("Counts: %v", err)
	}
	if len(counts.Unreads) != 2 {
		t.Fatalf("unreads = %+v; want 2 (one channel, one im)", counts.Unreads)
	}
	byID := map[string]bool{}
	for _, u := range counts.Unreads {
		byID[u.ChannelID] = u.HasUnread
	}
	if !byID["C1"] || byID["D1"] {
		t.Errorf("unread flags = %+v; want C1 unread and D1 read", byID)
	}
	// The threads rollup is the authoritative answer to "does the user
	// have unread thread activity"; the local cache has no per-thread
	// read state and its heuristic produces false positives without it.
	want := struct {
		has          bool
		unread, ment int
	}{true, 3, 1}
	got := struct {
		has          bool
		unread, ment int
	}{counts.Threads.HasUnreads, counts.Threads.UnreadCount, counts.Threads.MentionCount}
	if got != want {
		t.Errorf("threads = %+v; want %+v", got, want)
	}
}

// ---------------------------------------------------------------------
// historyAdapter
// ---------------------------------------------------------------------

const historyBody = `{
  "ok": true,
  "messages": [{"type":"message","ts":"3.0","text":"new"}],
  "unchanged_messages": ["1.0","2.0"],
  "latest_updates": {"3.0":"17833375330191740"},
  "has_more": true
}`

func TestHistoryAdapter_SendsCachedVersionsAndReturnsTheIncrementalVerdict(t *testing.T) {
	srv := newFakeSlack(t, map[string]string{"/api/conversations.history": historyBody})
	c := newTestClient(t, srv.Server)

	cached := map[string]string{"1.0": "v1", "2.0": "v2"}
	hist, err := historyAdapter{c}.HistoryWithVersions(context.Background(), "C1", cached)
	if err != nil {
		t.Fatalf("HistoryWithVersions: %v", err)
	}

	// The request must carry the caller's map verbatim. Without it the
	// server has nothing to compare against and re-sends scrollback slk
	// already holds.
	req := srv.requestTo(t, "/api/conversations.history")
	var sent map[string]string
	if err := json.Unmarshal([]byte(req.form.Get("cached_latest_updates")), &sent); err != nil {
		t.Fatalf("cached_latest_updates = %q: %v", req.form.Get("cached_latest_updates"), err)
	}
	if !reflect.DeepEqual(sent, cached) {
		t.Errorf("cached_latest_updates = %+v; want %+v", sent, cached)
	}
	if got := req.form.Get("limit"); got != "28" {
		t.Errorf("limit = %q; want 28, the only page size the official client was observed sending", got)
	}

	// UnchangedTS is the server's verdict on what the caller still
	// holds correctly. Dropping it turns every incremental sync into a
	// full refetch on the next open, silently — the bodies for those
	// timestamps are NOT in the response, so a caller that loses the
	// list has no way to notice.
	if !reflect.DeepEqual(hist.UnchangedTS, []string{"1.0", "2.0"}) {
		t.Errorf("UnchangedTS = %+v; want [1.0 2.0]", hist.UnchangedTS)
	}
	if !reflect.DeepEqual(hist.LatestUpdates, map[string]string{"3.0": "17833375330191740"}) {
		t.Errorf("LatestUpdates = %+v; want the returned message's version", hist.LatestUpdates)
	}
	if !hist.HasMore {
		t.Error("HasMore = false; want true")
	}
	if len(hist.Messages) != 1 {
		t.Fatalf("Messages = %d; want 1", len(hist.Messages))
	}
	// Messages arrive as raw JSON so a view result and a history result
	// can share Result.Messages. Decode far enough to prove the
	// conversion did not lose the body.
	var m struct {
		TS   string `json:"ts"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(hist.Messages[0], &m); err != nil {
		t.Fatalf("decoding converted message: %v", err)
	}
	if m.TS != "3.0" || m.Text != "new" {
		t.Errorf("converted message = %+v; want ts 3.0 / text new", m)
	}
}

// ---------------------------------------------------------------------
// boundedMessageVersions
// ---------------------------------------------------------------------

// fakeWindowSource records the window it was asked for.
type fakeWindowSource struct {
	rows []cache.Message

	gotLimit    int
	gotBeforeTS string
	gotOldestTS string
	gotLatestTS string
	called      bool

	getErr error
}

func (f *fakeWindowSource) GetMessages(_ string, limit int, beforeTS string) ([]cache.Message, error) {
	f.gotLimit = limit
	f.gotBeforeTS = beforeTS
	if f.getErr != nil {
		return nil, f.getErr
	}
	if limit >= len(f.rows) {
		return f.rows, nil
	}
	return f.rows[len(f.rows)-limit:], nil
}

func (f *fakeWindowSource) MessageVersions(_, oldestTS, latestTS string) (map[string]string, error) {
	f.called = true
	f.gotOldestTS = oldestTS
	f.gotLatestTS = latestTS
	return map[string]string{oldestTS: "v"}, nil
}

func TestBoundedMessageVersions_AsksForABoundedWindowNotTheWholeChannel(t *testing.T) {
	// 100 cached messages: far more than any one request can be
	// answered about.
	rows := make([]cache.Message, 0, 100)
	for i := 0; i < 100; i++ {
		rows = append(rows, cache.Message{TS: fixedWidthTS(i)})
	}
	src := &fakeWindowSource{rows: rows}

	if _, err := boundedMessageVersions(src, "C1"); err != nil {
		t.Fatalf("boundedMessageVersions: %v", err)
	}
	if !src.called {
		t.Fatal("MessageVersions was never called")
	}

	// The failure this guards against: an unbounded window. "" and "9"
	// are the two literals that produce one — "" is below every ts and
	// "9" is above every ts in Slack's second-precision string form —
	// and either puts the whole channel's version map into a request
	// body.
	if src.gotOldestTS == "" || src.gotOldestTS == "9" {
		t.Errorf("oldestTS = %q; want a real cached timestamp, not an unbounded lower edge", src.gotOldestTS)
	}
	if src.gotLatestTS == "" || src.gotLatestTS == "9" {
		t.Errorf("latestTS = %q; want a real cached timestamp, not an unbounded upper edge", src.gotLatestTS)
	}

	// And it must be bounded by count, not merely non-empty: the window
	// is the newest messageVersionWindow rows, so its edges are the
	// first and last of exactly those.
	if src.gotLimit != messageVersionWindow {
		t.Errorf("GetMessages limit = %d; want %d (the page size the request itself asks for)",
			src.gotLimit, messageVersionWindow)
	}
	if src.gotBeforeTS != "" {
		t.Errorf("GetMessages beforeTS = %q; want the newest window, not a paginated one", src.gotBeforeTS)
	}
	wantOldest := rows[len(rows)-messageVersionWindow].TS
	wantLatest := rows[len(rows)-1].TS
	if src.gotOldestTS != wantOldest || src.gotLatestTS != wantLatest {
		t.Errorf("window = [%s, %s]; want [%s, %s] — the newest %d cached messages",
			src.gotOldestTS, src.gotLatestTS, wantOldest, wantLatest, messageVersionWindow)
	}
}

// fixedWidthTS builds a sortable Slack-shaped timestamp.
func fixedWidthTS(i int) string {
	return "17000000" + string(rune('0'+i/100%10)) + string(rune('0'+i/10%10)) + string(rune('0'+i%10)) + ".000000"
}

func TestBoundedMessageVersions_VouchesForNothingWhenTheCacheIsEmpty(t *testing.T) {
	src := &fakeWindowSource{}
	got, err := boundedMessageVersions(src, "C1")
	if err != nil {
		t.Fatalf("boundedMessageVersions: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("versions = %+v; want none", got)
	}
	// No range query at all: vouching for versions slk does not hold
	// makes the server withhold messages slk never received.
	if src.called {
		t.Errorf("MessageVersions called with [%q, %q]; want no call for an empty channel",
			src.gotOldestTS, src.gotLatestTS)
	}
}

func TestBoundedMessageVersions_SurfacesAReadFailure(t *testing.T) {
	src := &fakeWindowSource{getErr: errors.New("disk gone")}
	if _, err := boundedMessageVersions(src, "C1"); err == nil {
		t.Fatal("want an error when the cache read fails")
	}
}

// TestStoreAdapter_UsesTheOneArgumentMessageVersions pins the shadowing
// in storeAdapter. cache.DB.MessageVersions takes three arguments and
// bootstrap.Store's takes one; if the override were removed the
// embedded method would be the only one, and this would not compile.
func TestStoreAdapter_UsesTheOneArgumentMessageVersions(t *testing.T) {
	db, err := cache.New(":memory:")
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}
	defer db.Close()

	a := storeAdapter{db}
	got, err := a.MessageVersions("C1")
	if err != nil {
		t.Fatalf("MessageVersions: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("versions = %+v; want none for an empty cache", got)
	}
}

// ---------------------------------------------------------------------
// Deps
// ---------------------------------------------------------------------

// TestBootstrapDeps_PopulatesEveryDependency is the guard for the one
// wiring mistake nothing else catches.
//
// bootstrap.Run returns an error for a nil Boot, Counts, View, History
// or Store. It does NOT for a nil Revalidate: revalidate() logs one
// debug line and returns, on the documented grounds that a stale cache
// still renders. So an omitted Revalidate here produces a workspace
// that boots, looks right, and has the entire conditional-revalidation
// phase switched off.
//
// Reflection rather than a field-by-field list so a dependency added to
// Deps later is covered the day it is added.
func TestBootstrapDeps_PopulatesEveryDependency(t *testing.T) {
	db, err := cache.New(":memory:")
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}
	defer db.Close()

	deps := newBootstrapDeps(slackclient.NewClient("xoxc-test", "d-cookie"), db, "xoxc-test", "C1", edge.NewHealth())

	v := reflect.ValueOf(deps)
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		name := v.Type().Field(i).Name
		switch f.Kind() {
		case reflect.Interface, reflect.Func:
			if f.IsNil() {
				t.Errorf("Deps.%s is nil; bootstrap.Run cannot use a dependency that was never wired", name)
			}
		}
	}
	if deps.Health == nil {
		t.Errorf("Deps.Health is nil; bootstrap cannot mark wholesale edge failures degraded without it")
	}
	if deps.OpenChannelID != "C1" {
		t.Errorf("OpenChannelID = %q; want the channel it was given", deps.OpenChannelID)
	}
}

// TestBootstrapDeps_RevalidatorUsesTheBrowserShapedHTTPClient is the
// highest-consequence wiring check in this file.
//
// edge.New takes an *http.Client and accepts any of them. Handing it a
// plain &http.Client{} compiles, runs, revalidates correctly, and sends
// every edgeapi request with Go's default User-Agent, none of Chrome's
// header set and no envelope — which is the exact divergence this whole
// project exists to remove, and it is invisible from inside slk. The
// only place it IS visible is on the wire, so this test reads the wire.
func TestBootstrapDeps_RevalidatorUsesTheBrowserShapedHTTPClient(t *testing.T) {
	srv := newFakeSlack(t, map[string]string{
		"/api/auth.test":          `{"ok":true,"url":"https://acme.slack.com/","team":"Acme","user":"grant","team_id":"T1","user_id":"U1"}`,
		"/cache/T1/channels/info": `{"ok":true,"results":[]}`,
	})
	c := newTestClient(t, srv.Server)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	db, err := cache.New(":memory:")
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}
	defer db.Close()

	deps := newBootstrapDeps(c, db, "xoxc-test", "", edge.NewHealth())

	// Structural check FIRST, and it is deliberately not redundant
	// with the wire assertions below. edge.Client's base URL is
	// hardcoded to https://edgeapi.slack.com and is not settable, so
	// these tests only reach a local server because the client's own
	// transport is what redirects the dial. Give edge.New any OTHER
	// http.Client and the request does not arrive here wearing the
	// wrong headers — it leaves the machine for the real edgeapi. So
	// the wire assertions alone would turn a wiring bug into a live
	// network call from the test suite, and would report it as an
	// auth failure rather than as the header divergence it is.
	//
	// The field is unexported; reflect.Value.Pointer is readable
	// anyway, which Interface() would not be.
	edgeHTTP := reflect.ValueOf(deps.Revalidate).Elem().FieldByName("http")
	if !edgeHTTP.IsValid() {
		t.Fatal("edge.Client has no `http` field; this test needs updating alongside that rename")
	}
	if edgeHTTP.Pointer() != reflect.ValueOf(c.HTTPClient()).Pointer() {
		t.Fatalf("edge.New was given a different *http.Client than the slack client's own. " +
			"Only that one carries slackhttp.BrowserTransport, its Chrome header set, the " +
			"telemetry envelope and DefaultCounter; any other sends bare Go requests to edgeapi.")
	}

	if _, err := deps.Revalidate.ChannelsInfo(context.Background(), "T1", map[string]int64{"C1": 0}); err != nil {
		t.Fatalf("ChannelsInfo: %v", err)
	}

	req := srv.requestTo(t, "/cache/T1/channels/info")
	if ua := req.header.Get("User-Agent"); !strings.Contains(ua, "Chrome/") {
		t.Errorf("User-Agent = %q; want Chrome's — edge.New was given a plain http.Client, not the BrowserTransport one", ua)
	}
	if got := req.header.Get("Sec-Ch-Ua-Platform"); got == "" {
		t.Error("no Sec-Ch-Ua-Platform; the browser header set is not being applied to edgeapi requests")
	}
	// The edgeapi envelope. BrowserTransport adds these only when it
	// has an Envelope, which only the client's own HTTP client carries.
	if got := req.query.Get("_x_app_name"); got != "client" {
		t.Errorf("_x_app_name = %q; want client — the edgeapi envelope is missing", got)
	}
	if got := req.query.Get("fp"); got != "6e" {
		t.Errorf("fp = %q; want 6e", got)
	}
	// And NOT the workspace set: edgeapi never carries _x_id or
	// slack_route in any capture.
	if got := req.query.Get("_x_id"); got != "" {
		t.Errorf("_x_id = %q; edgeapi requests must not carry the workspace envelope", got)
	}
}

// ---------------------------------------------------------------------
// first-sight hydration
// ---------------------------------------------------------------------

func TestHydrateFirstSight_InsertsRowsTheCacheHasNeverSeen(t *testing.T) {
	db, err := cache.New(":memory:")
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}
	defer db.Close()

	res := &bootstrap.Result{
		Channels: []boot.Channel{
			{ID: "C1", Name: "general", IsChannel: true, Topic: boot.TextBlock{Value: "talk"}},
			{ID: "C2", Name: "secret", IsPrivate: true},
			{ID: "C3", Name: "mpdm", IsPrivate: true, IsMPIM: true},
		},
		IMs:   []boot.IM{{ID: "D1", UserID: "U2", IsIM: true}},
		Users: []boot.User{{ID: "U2", Name: "pat", Profile: boot.UserProfile{DisplayName: "Pat"}}},
	}
	hydrateFirstSight(db, "T1", res)

	for _, tc := range []struct{ id, name, chType string }{
		{"C1", "general", "channel"},
		{"C2", "secret", "private"},
		// is_private is true on an MPDM too, so testing it first would
		// file every group DM under "private".
		{"C3", "mpdm", "group_dm"},
		{"D1", "", "dm"},
	} {
		got, err := db.GetChannel(tc.id)
		if err != nil {
			t.Errorf("GetChannel(%s): %v", tc.id, err)
			continue
		}
		if got.Name != tc.name || got.Type != tc.chType {
			t.Errorf("%s = {name:%q type:%q}; want {name:%q type:%q}", tc.id, got.Name, got.Type, tc.name, tc.chType)
		}
		if !got.IsMember {
			t.Errorf("%s is_member = false; userBoot's channels[] IS the membership list", tc.id)
		}
	}

	u, err := db.GetUser("U2")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.DisplayName != "Pat" || u.WorkspaceID != "T1" {
		t.Errorf("user = %+v; want Pat in T1", u)
	}
}

// TestHydrateFirstSight_LeavesExistingRowsAlone is the reason this is
// first-SIGHT hydration rather than a second write path.
//
// The full upserts own every column, and userBoot carries none of
// is_starred, presence or a DM's derived "app" type. Running them over
// an existing row blanks all three — silently, and visibly only much
// later as a starred channel that stopped being starred.
func TestHydrateFirstSight_LeavesExistingRowsAlone(t *testing.T) {
	db, err := cache.New(":memory:")
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}
	defer db.Close()

	if err := db.UpsertWorkspace(cache.Workspace{ID: "T1", Name: "Acme"}); err != nil {
		t.Fatalf("UpsertWorkspace: %v", err)
	}
	if err := db.UpsertChannel(cache.Channel{
		ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel",
		IsMember: true, IsStarred: true,
	}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	if err := db.UpsertUser(cache.User{
		ID: "U2", WorkspaceID: "T1", Name: "pat", DisplayName: "Pat",
		AvatarURL: "https://cdn/pat.png", Presence: "active",
	}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	hydrateFirstSight(db, "T1", &bootstrap.Result{
		Channels: []boot.Channel{{ID: "C1", Name: "renamed", IsChannel: true}},
		Users:    []boot.User{{ID: "U2", Name: "pat", Profile: boot.UserProfile{DisplayName: "Pat"}}},
	})

	ch, err := db.GetChannel("C1")
	if err != nil {
		t.Fatalf("GetChannel: %v", err)
	}
	if !ch.IsStarred {
		t.Error("is_starred was blanked; hydration must not touch a row the cache already has")
	}
	if ch.Name != "general" {
		t.Errorf("name = %q; want the existing row untouched — renames arrive via edge revalidation", ch.Name)
	}

	u, err := db.GetUser("U2")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.Presence != "active" {
		t.Errorf("presence = %q; want active — no boot response carries presence", u.Presence)
	}
	if u.AvatarURL != "https://cdn/pat.png" {
		t.Errorf("avatar_url = %q; want the cached one preserved", u.AvatarURL)
	}
}

func TestApplyBootUsers_FillsTheMapsTheSidebarReads(t *testing.T) {
	wctx := &WorkspaceContext{
		UserNames:         map[string]string{},
		UserNamesByHandle: map[string]string{},
		BotUserIDs:        map[string]bool{},
		AvatarURLs:        &sync.Map{},
	}
	applyBootUsers(wctx, &bootstrap.Result{Users: []boot.User{
		{ID: "U1", Name: "pat", Profile: boot.UserProfile{DisplayName: "Pat", ImageOriginal: "https://cdn/pat.png"}},
		{ID: "U2", Name: "sam", Profile: boot.UserProfile{RealName: "Sam Real"}},
		{ID: "U3", Name: "handle-only"},
		// is_app_user, not is_bot: a Slack app sets the second and a
		// classic bot the first, and cache.User.IsBot is their union.
		// Reading only is_bot puts every app DM in the human DM list.
		{ID: "U4", Name: "appy", IsAppUser: true},
	}})

	want := map[string]string{"U1": "Pat", "U2": "Sam Real", "U3": "handle-only", "U4": "appy"}
	if !reflect.DeepEqual(wctx.UserNames, want) {
		t.Errorf("UserNames = %+v; want %+v", wctx.UserNames, want)
	}
	if got := wctx.UserNamesByHandle["pat"]; got != "Pat" {
		t.Errorf("UserNamesByHandle[pat] = %q; want Pat", got)
	}
	if !wctx.BotUserIDs["U4"] || wctx.BotUserIDs["U1"] {
		t.Errorf("BotUserIDs = %+v; want only U4", wctx.BotUserIDs)
	}
	if v, ok := wctx.AvatarURLs.Load("U1"); !ok || v.(string) != "https://cdn/pat.png" {
		t.Errorf("AvatarURLs[U1] = %v/%v; want the image_original URL", v, ok)
	}
}

// ---------------------------------------------------------------------
// bootMutedChannels
// ---------------------------------------------------------------------

func TestBootMutedChannels_MergesBothPrefs(t *testing.T) {
	tests := []struct {
		name   string
		muted  string
		legacy string
		want   []string
	}{
		{
			name:  "all_notifications_prefs is where mute lives today",
			muted: `{"channels":{"C1":{"muted":true},"C2":{"muted":false}}}`,
			want:  []string{"C1"},
		},
		{
			name:   "the legacy flat list still counts",
			legacy: "C3, C4",
			want:   []string{"C3", "C4"},
		},
		{
			name:   "both are merged, not one overriding the other",
			muted:  `{"channels":{"C1":{"muted":true}}}`,
			legacy: "C3",
			want:   []string{"C1", "C3"},
		},
		{
			name: "nothing muted",
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := &bootstrap.Result{MutePrefsRaw: tc.muted, LegacyMutedRaw: tc.legacy}
			got, err := bootMutedChannels{res}.GetMutedChannels(context.Background())
			if err != nil {
				t.Fatalf("GetMutedChannels: %v", err)
			}
			sort.Strings(got)
			if len(got) != len(tc.want) {
				t.Fatalf("muted = %v; want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("muted = %v; want %v", got, tc.want)
				}
			}
		})
	}
}

func TestBootConversations_MapsWhatTheSidebarBuilderReads(t *testing.T) {
	// buildChannelItem reads ID, Name, Topic, IsIM, IsMpIM, IsPrivate,
	// IsMember and User. Every one of those has to survive the trip
	// from client.userBoot, or the sidebar renders wrong rather than
	// empty -- which is harder to notice.
	res := &bootstrap.Result{
		Channels: []boot.Channel{
			{ID: "C1", Name: "general", IsChannel: true, IsGeneral: true,
				Topic: boot.TextBlock{Value: "company wide"}},
			{ID: "C2", Name: "secret", IsChannel: true, IsPrivate: true},
			{ID: "C3", Name: "mpdm-a--b--c", IsMPIM: true},
		},
		IMs:    []boot.IM{{ID: "D1", UserID: "U9", IsIM: true, IsOpen: true}},
		IsOpen: []string{"D1"},
	}

	got := bootConversations(res)
	if len(got) != 4 {
		t.Fatalf("got %d conversations; want 4 (3 channels + 1 IM)", len(got))
	}
	by := map[string]slack.Channel{}
	for _, c := range got {
		by[c.ID] = c
	}

	if c := by["C1"]; c.Name != "general" || c.Topic.Value != "company wide" {
		t.Errorf("C1 = %+v; want name and topic carried through", c)
	}
	// userBoot's channels[] is the user's OWN channel list, so every
	// entry is one they are in. Losing this bucketed every channel as
	// unjoined.
	for _, id := range []string{"C1", "C2", "C3"} {
		if !by[id].IsMember {
			t.Errorf("%s IsMember = false; userBoot only returns channels the user is in", id)
		}
	}
	if !by["C2"].IsPrivate {
		t.Error("C2 lost IsPrivate; it would render as a public channel")
	}
	if !by["C3"].IsMpIM {
		t.Error("C3 lost IsMpIM; group DMs would render as channels")
	}
	if d := by["D1"]; !d.IsIM || d.User != "U9" {
		t.Errorf("D1 = %+v; want IsIM with User U9 — the counterparty id is what names a DM", d)
	}
}

func TestBootConversations_SkipsArchivedAndClosed(t *testing.T) {
	// users.conversations was called with ExcludeArchived: true, so
	// keeping archived channels here would be a visible regression
	// dressed up as a fix. A closed DM is not in the sidebar either.
	res := &bootstrap.Result{
		Channels: []boot.Channel{
			{ID: "C1", Name: "live", IsChannel: true},
			{ID: "C2", Name: "dead", IsChannel: true, IsArchived: true},
		},
		IMs: []boot.IM{
			{ID: "D1", UserID: "U1", IsIM: true, IsOpen: true},
			{ID: "D2", UserID: "U2", IsIM: true},
			{ID: "D3", UserID: "U3", IsIM: true, IsArchived: true},
		},
		IsOpen: []string{"D1"},
	}
	got := bootConversations(res)
	ids := map[string]bool{}
	for _, c := range got {
		ids[c.ID] = true
	}
	if ids["C2"] {
		t.Error("archived channel kept")
	}
	if ids["D3"] {
		t.Error("archived DM kept")
	}
	if ids["D2"] {
		t.Error("closed DM kept; it is not in IsOpen and IsOpen is false on it")
	}
	if !ids["C1"] || !ids["D1"] {
		t.Errorf("dropped something live: %v", ids)
	}
}

func TestBootConversations_NilSafe(t *testing.T) {
	if got := bootConversations(nil); got != nil {
		t.Errorf("got %v; want nil", got)
	}
}
