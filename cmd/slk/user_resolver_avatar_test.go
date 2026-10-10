package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gammons/slk/internal/avatar"
	"github.com/gammons/slk/internal/cache"
	imgpkg "github.com/gammons/slk/internal/image"
)

// avatarPNGServer serves a 16x16 PNG at every path: the avatar CDN.
func avatarPNGServer(t *testing.T) *httptest.Server {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	body := buf.Bytes()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// userHits counts users.info requests per user ID.
type userHits struct {
	mu sync.Mutex
	n  map[string]int
}

func (h *userHits) add(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.n == nil {
		h.n = map[string]int{}
	}
	h.n[id]++
}

func (h *userHits) get(id string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.n[id]
}

// usersInfoServer fakes users.info. profiles maps a user ID to the JSON
// of its `profile` object; an ID not in it gets user_not_found.
func usersInfoServer(t *testing.T, profiles map[string]string) (*httptest.Server, *userHits) {
	t.Helper()
	hits := &userHits{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.FormValue("user")
		hits.add(id)
		w.Header().Set("Content-Type", "application/json")
		p, ok := profiles[id]
		if !ok {
			_, _ = w.Write([]byte(`{"ok":false,"error":"user_not_found"}`))
			return
		}
		_, _ = fmt.Fprintf(w, `{"ok":true,"user":{"id":%q,"name":"n","team_id":"T1","profile":%s}}`, id, p)
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

// newReadyAvatarCache is an avatar cache whose onReady reports user IDs.
func newReadyAvatarCache(t *testing.T) (*avatar.Cache, <-chan string) {
	t.Helper()
	ic, err := imgpkg.NewCache(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	avc := avatar.NewCache(imgpkg.NewFetcher(ic, http.DefaultClient), nil, false)
	ready := make(chan string, 16)
	avc.SetOnReady(func(id string) { ready <- id })
	return avc, ready
}

// seedEmptyAvatarUser inserts the row an edge users/info record leaves
// for a user with no custom image: everything but avatar_url.
func seedEmptyAvatarUser(t *testing.T, db *cache.DB, id string) {
	t.Helper()
	if err := db.UpsertUser(cache.User{ID: id, WorkspaceID: "T1", Name: "ray", DisplayName: "Ray", Presence: "active"}); err != nil {
		t.Fatal(err)
	}
}

func storedAvatarURL(t *testing.T, db *cache.DB, id string) string {
	t.Helper()
	u, err := db.GetUser(id)
	if err != nil {
		t.Fatal(err)
	}
	return u.AvatarURL
}

func TestWantsAvatarBackfill(t *testing.T) {
	for id, want := range map[string]bool{
		"U018F2W7X7D": true,
		"W012ABCDEF":  true, // Enterprise Grid user IDs
		"B0123BOT":    false,
		"":            false,
	} {
		if got := wantsAvatarBackfill(id); got != want {
			t.Errorf("wantsAvatarBackfill(%q) = %v; want %v", id, got, want)
		}
	}
}

func TestBackfillAvatar_StoresAndPreloadsLargestSize(t *testing.T) {
	img := avatarPNGServer(t)
	cases := []struct {
		name, profile, want string
	}{
		{"72 48 32", fmt.Sprintf(`{"image_32":"%[1]s/32","image_48":"%[1]s/48","image_72":"%[1]s/72"}`, img.URL), img.URL + "/72"},
		{"48 32", fmt.Sprintf(`{"image_32":"%[1]s/32","image_48":"%[1]s/48"}`, img.URL), img.URL + "/48"},
		{"32 only", fmt.Sprintf(`{"image_32":"%s/32"}`, img.URL), img.URL + "/32"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := usersInfoServer(t, map[string]string{"U_RAY": tc.profile})
			db := newTestDB(t)
			seedEmptyAvatarUser(t, db, "U_RAY")
			avc, ready := newReadyAvatarCache(t)
			r := newUserResolver("T1", newTestClient(t, srv), db, avc, nil, nil, nil)

			r.backfillAvatar("U_RAY")

			if got := <-ready; got != "U_RAY" {
				t.Fatalf("onReady for %q; want U_RAY", got)
			}
			if got := storedAvatarURL(t, db, "U_RAY"); got != tc.want {
				t.Errorf("stored avatar_url = %q; want %q", got, tc.want)
			}
			if avc.Get("U_RAY") == "" {
				t.Error("avatar not rendered")
			}
		})
	}
}

// A user the edge batch resolved this session has an avatar_url in
// SQLite but none in wctx.AvatarURLs, so a render before their avatar
// lands asks for a backfill. That must not cost a users.info call per
// newly seen author: edge batching exists to remove exactly that.
func TestBackfillAvatar_UsesStoredURLWithoutUsersInfo(t *testing.T) {
	img := avatarPNGServer(t)
	srv, hits := usersInfoServer(t, map[string]string{"U_EDGE": fmt.Sprintf(`{"image_72":"%s/72"}`, img.URL)})
	db := newTestDB(t)
	if err := db.UpsertUser(cache.User{ID: "U_EDGE", WorkspaceID: "T1", Name: "e", DisplayName: "E", AvatarURL: img.URL + "/stored"}); err != nil {
		t.Fatal(err)
	}
	avc, ready := newReadyAvatarCache(t)
	r := newUserResolver("T1", newTestClient(t, srv), db, avc, nil, nil, nil)

	r.backfillAvatar("U_EDGE")

	if got := <-ready; got != "U_EDGE" {
		t.Fatalf("onReady for %q; want U_EDGE", got)
	}
	if n := hits.get("U_EDGE"); n != 0 {
		t.Errorf("users.info calls = %d; want 0 (the stored URL was enough)", n)
	}
	if got := storedAvatarURL(t, db, "U_EDGE"); got != img.URL+"/stored" {
		t.Errorf("avatar_url = %q; want the stored one kept", got)
	}
}

// A brand-new author renders before the resolver's edge batch has
// written their row. users.info is the resolver's job then, not the
// backfill's: calling it here would cost one call per newly seen
// author, and FillUserAvatarURL would have no row to fill, so nothing
// would be saved for the next launch. The backfill steps aside and
// releases its once-per-session token; UserResolvedMsg re-renders the
// row once it exists, and that render's request goes through.
func TestBackfillAvatar_UnresolvedUserWaitsForTheResolver(t *testing.T) {
	img := avatarPNGServer(t)
	srv, hits := usersInfoServer(t, map[string]string{"U_NEW": fmt.Sprintf(`{"image_72":"%s/72"}`, img.URL)})
	db := newTestDB(t)
	avc, ready := newReadyAvatarCache(t)
	r := newUserResolver("T1", newTestClient(t, srv), db, avc, nil, nil, nil)

	r.avatarTried.Store("U_NEW", struct{}{}) // RequestAvatar's claim
	r.backfillAvatar("U_NEW")

	if n := hits.get("U_NEW"); n != 0 {
		t.Errorf("users.info calls before the user's row exists = %d; want 0", n)
	}
	if _, held := r.avatarTried.Load("U_NEW"); held {
		t.Fatal("once-per-session token still held; the post-resolution render could never retry")
	}

	seedEmptyAvatarUser(t, db, "U_NEW") // the resolver's edge batch lands
	r.RequestAvatar("U_NEW")            // the UserResolvedMsg re-render

	if got := <-ready; got != "U_NEW" {
		t.Fatalf("onReady for %q; want U_NEW", got)
	}
	if n := hits.get("U_NEW"); n != 1 {
		t.Errorf("users.info calls = %d; want 1", n)
	}
	if got := storedAvatarURL(t, db, "U_NEW"); got != img.URL+"/72" {
		t.Errorf("stored avatar_url = %q; want it saved for the next launch", got)
	}
}

// Nothing to store when users.info fails or has no image. The avatar
// cache is nil: any Preload with a URL would panic.
func TestBackfillAvatar_NoImageOrErrorWritesNothing(t *testing.T) {
	srv, hits := usersInfoServer(t, map[string]string{"U_NOIMG": `{"display_name":"No Image"}`})
	db := newTestDB(t)
	seedEmptyAvatarUser(t, db, "U_NOIMG")
	seedEmptyAvatarUser(t, db, "U_ERR")
	r := newUserResolver("T1", newTestClient(t, srv), db, nil, nil, nil, nil)

	r.backfillAvatar("U_NOIMG")
	r.backfillAvatar("U_ERR")

	for _, id := range []string{"U_NOIMG", "U_ERR"} {
		if hits.get(id) != 1 {
			t.Errorf("users.info calls for %s = %d; want 1", id, hits.get(id))
		}
		if got := storedAvatarURL(t, db, id); got != "" {
			t.Errorf("%s avatar_url = %q; want it left empty", id, got)
		}
	}
}

// RequestAvatar is called on every render of an author row with no
// avatar URL; one users.info call per user per session, whatever the
// outcome, is what keeps that from becoming a call per frame.
func TestRequestAvatar_OncePerUserPerSession(t *testing.T) {
	img := avatarPNGServer(t)
	srv, hits := usersInfoServer(t, map[string]string{"U_RAY": fmt.Sprintf(`{"image_72":"%s/72"}`, img.URL)})
	db := newTestDB(t)
	seedEmptyAvatarUser(t, db, "U_RAY")
	avc, ready := newReadyAvatarCache(t)
	r := newUserResolver("T1", newTestClient(t, srv), db, avc, nil, nil, nil)

	for i := 0; i < 5; i++ {
		r.RequestAvatar("U_RAY")
	}
	<-ready // the backfill finished: users.info answered, row filled, avatar rendered

	if got := hits.get("U_RAY"); got != 1 {
		t.Errorf("users.info calls = %d; want 1", got)
	}
}

// RequestAvatar runs inside View(); it must never wait on users.info.
func TestRequestAvatar_DoesNotBlockTheCaller(t *testing.T) {
	const requests = userResolverConcurrency * 4
	release := make(chan struct{})
	arrived := make(chan struct{}, requests)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		arrived <- struct{}{}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"U1","name":"n","team_id":"T1","profile":{}}}`))
	}))
	defer srv.Close()
	db := newTestDB(t)
	for i := 0; i < requests; i++ {
		seedEmptyAvatarUser(t, db, fmt.Sprintf("U%03d", i))
	}
	r := newUserResolver("T1", newTestClient(t, srv), db, nil, nil, nil, nil)

	returned := make(chan struct{})
	go func() {
		for i := 0; i < requests; i++ {
			r.RequestAvatar(fmt.Sprintf("U%03d", i))
		}
		close(returned)
	}()
	// No timeout by design: a RequestAvatar that waits on a round trip
	// hangs here, and the goroutine dump names it.
	<-returned
	close(release)
	// Drain: backfillAvatar reads SQLite before it calls users.info, so
	// once every request has reached the server no goroutine touches the
	// DB again, and none can race t.TempDir's RemoveAll.
	for i := 0; i < requests; i++ {
		<-arrived
	}
}
