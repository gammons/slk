package image

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	imgcolor "image/color"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTrustedHost(t *testing.T) {
	for host, want := range map[string]bool{
		"files.slack.com":           true,
		"FILES.SLACK.COM":           true,
		"avatars.slack-edge.com":    true,
		"emoji.slack-edge.com":      true,
		"slack-edge.com":            true,
		"evilslack-edge.com":        false,
		"slack-edge.com.evil.test":  false,
		"files.slack.com.evil.test": false,
		"slack.com":                 false,
		"slack-imgs.com":            false, // proxies third-party URLs
		"example.com":               false,
		"":                          false,
	} {
		if got := TrustedHost(host); got != want {
			t.Errorf("TrustedHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestLoadPolicyDecide(t *testing.T) {
	const (
		slackThumb = "https://files.slack.com/files-tmb/T1-F1-abc/x_360.png"
		avatar     = "https://avatars.slack-edge.com/2026/u.png"
		external   = "https://tracker.example/pixel.png"
		gh         = "https://opengraph.githubassets.com/1/a/b"
	)
	cases := []struct {
		mode  LoadMode
		extra []string
		url   string
		want  Decision
	}{
		{LoadTrusted, nil, slackThumb, Load},
		{LoadTrusted, nil, avatar, Load},
		{LoadTrusted, nil, external, Ask},
		{LoadTrusted, nil, "http://files.slack.com/files-tmb/T1/x.png", Ask}, // trusted only over https
		{LoadTrusted, nil, "https://files.slack.com:8443/x.png", Load},
		{LoadTrusted, nil, "file:///etc/passwd", Block},
		{LoadTrusted, nil, "data:image/png;base64,AAAA", Block},
		{LoadTrusted, nil, "not a url", Block},
		{LoadTrusted, []string{"githubassets.com"}, gh, Load},
		{LoadTrusted, []string{"*.githubassets.com"}, gh, Load},
		{LoadTrusted, []string{"githubassets.com"}, "http://opengraph.githubassets.com/x", Ask},
		{LoadConfirm, nil, slackThumb, Ask},
		{LoadConfirm, []string{"githubassets.com"}, gh, Ask},
		{LoadOff, nil, slackThumb, Block},
		{LoadOff, nil, external, Block},
	}
	for _, c := range cases {
		p := NewLoadPolicy(c.mode, c.extra)
		if got := p.Decide(c.url); got != c.want {
			t.Errorf("%s %v Decide(%q) = %v, want %v", c.mode, c.extra, c.url, got, c.want)
		}
	}
}

func TestLoadPolicyAllowances(t *testing.T) {
	const a, b = "https://tracker.example/a.png", "https://tracker.example/b.png"

	p := NewLoadPolicy(LoadTrusted, nil)
	p.AllowURLs(a)
	if p.Decide(a) != Load || p.Decide(b) != Ask {
		t.Fatalf("AllowURLs: a=%v b=%v, want Load, Ask", p.Decide(a), p.Decide(b))
	}
	p.AllowHosts("tracker.example")
	if p.Decide(b) != Load {
		t.Fatalf("AllowHosts: b=%v, want Load", p.Decide(b))
	}

	// Confirm mode honours allowances too; off ignores them.
	c := NewLoadPolicy(LoadConfirm, nil)
	c.AllowURLs(a)
	if c.Decide(a) != Load {
		t.Errorf("confirm + allowed URL = %v, want Load", c.Decide(a))
	}
	off := NewLoadPolicy(LoadOff, nil)
	off.AllowURLs(a)
	if off.Decide(a) != Block {
		t.Errorf("off + allowed URL = %v, want Block", off.Decide(a))
	}

	var nilPolicy *LoadPolicy
	if nilPolicy.Decide(b) != Load {
		t.Errorf("nil policy should load everything")
	}
	nilPolicy.AllowURLs(a) // must not panic
}

func TestParseLoadMode(t *testing.T) {
	for in, want := range map[string]LoadMode{"trusted": LoadTrusted, " Confirm ": LoadConfirm, "off": LoadOff} {
		if got, ok := ParseLoadMode(in); !ok || got != want {
			t.Errorf("ParseLoadMode(%q) = %v, %v", in, got, ok)
		}
	}
	if _, ok := ParseLoadMode("of"); ok {
		t.Error("ParseLoadMode(\"of\") should not be ok")
	}
}

// A gated fetch the policy holds back makes no request at all, and is
// refused even when its bytes are already in the disk cache.
func TestFetcherGatedRefusedWithoutRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		w.Write(tinyPNG(t, 4, 4, imgcolor.RGBA{255, 0, 0, 255}))
	}))
	defer srv.Close()

	cache, _ := NewCache(t.TempDir(), 10)
	f := NewFetcher(cache, srv.Client())
	// Fill the disk cache before any policy exists.
	if _, err := f.Fetch(context.Background(), FetchRequest{Key: "k", URL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	hits.Store(0)

	f.SetLoadPolicy(NewLoadPolicy(LoadTrusted, nil))
	_, err := f.Fetch(context.Background(), FetchRequest{Key: "k", URL: srv.URL, Gated: true})
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("gated fetch err = %v, want ErrNotAllowed", err)
	}
	_, err = f.Fetch(context.Background(), FetchRequest{Key: "k2", URL: srv.URL + "/x", Gated: true})
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("gated uncached fetch err = %v, want ErrNotAllowed", err)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("refused fetches made %d requests", n)
	}
	// Ungated (avatars, emoji) is unaffected.
	if _, err := f.Fetch(context.Background(), FetchRequest{Key: "k3", URL: srv.URL + "/y"}); err != nil {
		t.Errorf("ungated fetch: %v", err)
	}
}

// An image loaded because its host is trusted may not be redirected to an
// untrusted host; one the user allowed may.
func TestFetcherGatedRedirectGuard(t *testing.T) {
	var landed atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bounce" {
			// Same server, but reached through a host that is not trusted.
			http.Redirect(w, r, "https://"+strings.Replace(r.Host, "127.0.0.1", "localhost", 1)+"/final", http.StatusFound)
			return
		}
		landed.Add(1)
		w.Header().Set("Content-Type", "image/png")
		w.Write(tinyPNG(t, 4, 4, imgcolor.RGBA{0, 0, 255, 255}))
	}))
	defer srv.Close()

	cache, _ := NewCache(t.TempDir(), 10)
	f := NewFetcher(cache, srv.Client())
	p := NewLoadPolicy(LoadTrusted, []string{"127.0.0.1"})
	f.SetLoadPolicy(p)

	_, err := f.Fetch(context.Background(), FetchRequest{Key: "r1", URL: srv.URL + "/bounce", Gated: true})
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("redirect off a trusted host: err = %v, want ErrNotAllowed", err)
	}
	if landed.Load() != 0 {
		t.Fatal("the redirect target was contacted")
	}

	p.AllowURLs(srv.URL + "/bounce2")
	_, err = f.Fetch(context.Background(), FetchRequest{Key: "r2", URL: srv.URL + "/bounce2", Gated: true})
	// /bounce2 is not a redirect on this server; it just must not be
	// refused by the policy.
	if errors.Is(err, ErrNotAllowed) {
		t.Fatalf("user-allowed URL refused: %v", err)
	}
}

// pngDeclaring returns a valid 1x1 PNG whose header claims w x h.
func pngDeclaring(t *testing.T, w, h uint32) []byte {
	b := tinyPNG(t, 1, 1, imgcolor.RGBA{0, 0, 0, 255})
	// 8-byte signature, then the IHDR chunk: length(4) "IHDR"(4) data(13) crc(4).
	binary.BigEndian.PutUint32(b[16:], w)
	binary.BigEndian.PutUint32(b[20:], h)
	binary.BigEndian.PutUint32(b[29:], crc32.ChecksumIEEE(b[12:29]))
	return b
}

func TestSafeDecodeRefusesHugeDimensions(t *testing.T) {
	if _, err := safeDecode(bytes.NewReader(pngDeclaring(t, 100_000, 100_000))); err == nil {
		t.Fatal("a 100000x100000 PNG decoded; want the pixel limit to refuse it")
	}
	img, err := safeDecode(bytes.NewReader(tinyPNG(t, 8, 8, imgcolor.RGBA{1, 2, 3, 255})))
	if err != nil || img.Bounds() != image.Rect(0, 0, 8, 8) {
		t.Fatalf("small PNG: %v, %v", img, err)
	}
}

func TestFetcherRefusesOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(make([]byte, maxImageBytes+1))
	}))
	defer srv.Close()
	f := &Fetcher{http: srv.Client()}
	if _, _, _, err := f.tryDownload(context.Background(), srv.URL, TeamAuth{}); err == nil {
		t.Fatal("a body over maxImageBytes was accepted")
	}
}
