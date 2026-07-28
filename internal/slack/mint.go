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

	"github.com/gammons/slk/internal/slackhttp"
)

var apiTokenRE = regexp.MustCompile(`"api_token":"([^"]+)"`)

// newMintRequest builds the workspace page-load request used to scrape the
// api_token. It attaches the `d` cookie and navigation-shaped headers: a real
// browser loading a workspace page sends these, NOT the Origin/Sec-Fetch-Mode:
// cors of an XHR call (which strict proxies reject with 403 — see #111).
func newMintRequest(ctx context.Context, url, dCookie string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.AddCookie(&http.Cookie{Name: "d", Value: dCookie})
	req.Header.Set("User-Agent", slackhttp.UserAgent())
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	return req, nil
}

// MintToken mints a fresh xoxc token for a workspace by loading its page with
// the desktop `d` cookie and scraping the embedded api_token.
//
// Loading the workspace page is a top-level *navigation*, not an XHR/CORS
// call, so we deliberately use a plain HTTP client here rather than the
// browser XHR transport used for the ongoing API/WebSocket traffic. That
// transport stamps every request with Origin: https://app.slack.com and
// Sec-Fetch-Mode: cors — correct for a fetch from app.slack.com, but a
// contradictory signature on a page navigation that strict corporate
// edges/proxies reject with HTTP 403 (#111). mintTokenAt sets
// navigation-appropriate headers instead. Using a plain client (no cookie
// jar) also avoids sending the `d` cookie twice.
func MintToken(ctx context.Context, domain, dCookie string) (string, error) {
	client := &http.Client{
		// Bound the request so a hung/half-open connection (captive portal,
		// offline) can't stall onboarding or the startup re-mint indefinitely.
		Timeout: 15 * time.Second,
	}
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
		req, err := newMintRequest(ctx, baseURL, dCookie)
		if err != nil {
			return "", err
		}

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

// MintDiagnostics captures what a workspace page load returned, for
// troubleshooting mint failures (#111). It never includes secrets.
type MintDiagnostics struct {
	Domain       string
	RequestURL   string
	Status       int
	FinalURL     string // URL after following redirects
	BodyBytes    int
	HasAPIToken  bool
	LoginMarkers []string // signs the page is a signed-out / login page
	Err          string
}

// MintDiag performs the mint page load and returns diagnostics instead of the
// token, using the exact same request shape as MintToken. Safe to print: it
// reports sizes, status, and markers — never the cookie or token.
func MintDiag(ctx context.Context, domain, dCookie string) MintDiagnostics {
	client := &http.Client{Timeout: 15 * time.Second}
	return mintDiagAt(ctx, client, fmt.Sprintf("https://%s.slack.com", domain), dCookie, domain)
}

func mintDiagAt(ctx context.Context, client *http.Client, url, dCookie, domain string) MintDiagnostics {
	d := MintDiagnostics{Domain: domain, RequestURL: url}
	req, err := newMintRequest(ctx, url, dCookie)
	if err != nil {
		d.Err = err.Error()
		return d
	}
	resp, err := client.Do(req)
	if err != nil {
		d.Err = err.Error()
		return d
	}
	defer resp.Body.Close()
	d.Status = resp.StatusCode
	if resp.Request != nil && resp.Request.URL != nil {
		d.FinalURL = resp.Request.URL.String()
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		d.Err = err.Error()
		return d
	}
	d.BodyBytes = len(body)
	d.HasAPIToken = apiTokenRE.Match(body)
	d.LoginMarkers = detectLoginMarkers(body, d.FinalURL)
	return d
}

// detectLoginMarkers returns the names of any signed-out/login indicators
// found in the response body or final URL.
func detectLoginMarkers(body []byte, finalURL string) []string {
	var found []string
	lc := strings.ToLower(string(body))
	lu := strings.ToLower(finalURL)

	if strings.Contains(lu, "/signin") || strings.Contains(lu, "slack.com/signin") {
		found = append(found, "redirected-to-signin")
	}
	for name, needle := range map[string]string{
		"signin-prompt": "sign in to",
		"signed-out":    "signed out",
		"email-prompt":  "enter your email",
		"workspace-url": "find your workspace",
	} {
		if strings.Contains(lc, needle) {
			found = append(found, name)
		}
	}
	return found
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
