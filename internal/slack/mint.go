package slackclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var apiTokenRE = regexp.MustCompile(`"api_token":"([^"]+)"`)

// MintToken mints a fresh xoxc token for a workspace by loading its page with
// the desktop `d` cookie and scraping the embedded api_token. It uses a
// browser-shaped HTTP client with the cookie set.
func MintToken(ctx context.Context, domain, dCookie string) (string, error) {
	// No envelope: this fetches the workspace HTML page, not an /api/
	// endpoint, and real clients send no telemetry params there.
	client := newCookieHTTPClient(dCookie, nil)
	// Bound the request so a hung/half-open connection (captive portal,
	// offline) can't stall onboarding or the startup re-mint indefinitely.
	client.Timeout = 15 * time.Second
	return mintTokenAt(ctx, client, fmt.Sprintf("https://%s.slack.com", domain), dCookie)
}

// mintTokenAt is the testable core: GET baseURL with the d cookie, scrape
// api_token. The cookie is attached explicitly so httptest servers (which are
// not *.slack.com) still receive it.
//
// Slack rate-limits repeated page loads (e.g. minting several workspaces in a
// row, or frequent re-runs) with HTTP 429. On 429 we honor Retry-After and
// retry a few times rather than failing onboarding outright.
func mintTokenAt(ctx context.Context, client *http.Client, baseURL, dCookie string) (string, error) {
	const maxAttempts = 3

	for attempt := 1; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
		if err != nil {
			return "", err
		}
		req.AddCookie(&http.Cookie{Name: "d", Value: dCookie})

		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			wait := parseRetryAfter(resp.Header.Get("Retry-After"), 2*time.Second)
			resp.Body.Close()
			if attempt >= maxAttempts {
				return "", fmt.Errorf("mint token: Slack rate-limited the request (HTTP 429); wait a minute and run --add-workspace again")
			}
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return "", ctx.Err()
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return "", fmt.Errorf("mint token: status %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return "", err
		}
		m := apiTokenRE.FindSubmatch(body)
		if m == nil {
			return "", fmt.Errorf("mint token: api_token not found (is the desktop app signed in?)")
		}
		return string(m[1]), nil
	}
}

// parseRetryAfter reads a Retry-After header value (delay in seconds) and
// returns the wait, capped to a sane maximum. Falls back to def when the
// header is absent or unparseable.
func parseRetryAfter(h string, def time.Duration) time.Duration {
	wait := def
	if secs, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && secs >= 0 {
		wait = time.Duration(secs) * time.Second
	}
	const maxWait = 10 * time.Second
	if wait > maxWait {
		wait = maxWait
	}
	return wait
}
