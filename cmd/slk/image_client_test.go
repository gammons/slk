package main

import (
	"testing"
	"time"

	"github.com/gammons/slk/internal/slackhttp"
)

// TestNewImageHTTPClientIsImageDest pins the WIRING, not the header
// set — internal/slackhttp owns the values. Without this, pointing the
// image fetcher back at NewBrowserHTTPClient would compile, pass every
// slackhttp test, and silently send avatars and thumbnails with XHR
// headers and no Referer again.
func TestNewImageHTTPClientIsImageDest(t *testing.T) {
	c := newImageHTTPClient()

	bt, ok := c.Transport.(*slackhttp.BrowserTransport)
	if !ok {
		t.Fatalf("image client transport is %T; want *slackhttp.BrowserTransport", c.Transport)
	}
	if bt.Dest != slackhttp.DestImage {
		t.Errorf("image client Dest = %v; want DestImage — avatars and thumbnails are the "+
			"highest-volume path (337 CDN requests vs 53 API calls per boot)", bt.Dest)
	}
	if bt.Env != nil {
		t.Errorf("image client Env = %v; want nil (asset fetches carry no _x_* params)", bt.Env)
	}
	if c.Timeout != 10*time.Second {
		t.Errorf("image client Timeout = %v; want 10s", c.Timeout)
	}
}
