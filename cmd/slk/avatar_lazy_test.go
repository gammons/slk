package main

import (
	"fmt"
	"sync"
	"testing"
)

func TestLazyAvatar(t *testing.T) {
	img := avatarPNGServer(t)
	srv, hits := usersInfoServer(t, map[string]string{"U_RAY": fmt.Sprintf(`{"image_72":"%s/72"}`, img.URL)})
	db := newTestDB(t)
	seedEmptyAvatarUser(t, db, "U_RAY")
	avc, ready := newReadyAvatarCache(t)
	wctx := &WorkspaceContext{
		AvatarURLs:   &sync.Map{},
		UserResolver: newUserResolver("T1", newTestClient(t, srv), db, avc, nil, nil, nil),
	}

	t.Run("no workspace yet", func(t *testing.T) {
		if got := lazyAvatar(nil, avc, "U_RAY"); got != "" {
			t.Errorf("lazyAvatar(nil wctx) = %q; want empty", got)
		}
	})

	t.Run("hit returns the render", func(t *testing.T) {
		avc.PreloadSync("U_HIT", img.URL+"/hit")
		<-ready
		if lazyAvatar(wctx, avc, "U_HIT") == "" {
			t.Error("cached avatar not returned")
		}
	})

	t.Run("miss with a URL preloads it", func(t *testing.T) {
		wctx.AvatarURLs.Store("U_URL", img.URL+"/url")
		if got := lazyAvatar(wctx, avc, "U_URL"); got != "" {
			t.Errorf("miss returned %q; want empty until the avatar lands", got)
		}
		if got := <-ready; got != "U_URL" {
			t.Fatalf("onReady for %q; want U_URL", got)
		}
		if n := hits.get("U_URL"); n != 0 {
			t.Errorf("users.info calls for a user with a URL = %d; want 0", n)
		}
	})

	// The bug: a user who never uploaded an avatar has no URL, and the
	// render path used to give up here, leaving the slot blank forever.
	t.Run("miss without a URL backfills from users.info", func(t *testing.T) {
		if got := lazyAvatar(wctx, avc, "U_RAY"); got != "" {
			t.Errorf("miss returned %q; want empty until the avatar lands", got)
		}
		if got := <-ready; got != "U_RAY" {
			t.Fatalf("onReady for %q; want U_RAY", got)
		}
		if got := storedAvatarURL(t, db, "U_RAY"); got != img.URL+"/72" {
			t.Errorf("stored avatar_url = %q; want the users.info image_72", got)
		}
		if lazyAvatar(wctx, avc, "U_RAY") == "" {
			t.Error("backfilled avatar not returned on the next render")
		}
	})
}
