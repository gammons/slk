// Package filedl downloads Slack file attachments (url_private) with
// workspace auth and saves them to a local directory so the user can
// open them in an OS application.
package filedl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/gammons/slk/internal/slackhttp"
)

// Downloader fetches auth-gated Slack files into dir. Safe for
// concurrent use.
type Downloader struct {
	http     *http.Client
	resolver *slackhttp.AuthResolver
	dir      string
}

// New creates a Downloader that authenticates via resolver and saves
// into dir (created on first use).
func New(resolver *slackhttp.AuthResolver, dir string) *Downloader {
	return &Downloader{
		http:     &http.Client{Timeout: 60 * time.Second},
		resolver: resolver,
		dir:      dir,
	}
}

// Download fetches rawURL and writes it under dir with a sanitized
// version of name, returning the saved path. On filename collision a
// numeric suffix is appended; existing files are never overwritten.
func (d *Downloader) Download(ctx context.Context, rawURL, name string) (string, error) {
	if rawURL == "" {
		return "", fmt.Errorf("no download URL for %q", name)
	}
	auths := d.resolver.AuthsForURL(rawURL)
	if len(auths) == 0 {
		// Non-Slack URL: one unauthenticated attempt.
		auths = []slackhttp.TeamAuth{{}}
	}
	teamID := slackhttp.TeamIDFromFilesURL(rawURL)
	var lastErr error
	for _, auth := range auths {
		body, contentType, status, err := d.get(ctx, rawURL, auth)
		if err != nil {
			lastErr = err
			continue
		}
		if status != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d", status)
			continue
		}
		// Slack answers a failed-auth url_private request with its
		// 200 text/html login page; treat that as an auth failure and
		// try the next workspace's credentials.
		if strings.HasPrefix(strings.ToLower(contentType), "text/html") {
			lastErr = fmt.Errorf("HTTP 200 with text/html (auth failure)")
			continue
		}
		path, err := d.writeUnique(name, body)
		if err != nil {
			return "", err
		}
		d.resolver.Learn(teamID, auth)
		return path, nil
	}
	return "", fmt.Errorf("download %q: %w", name, lastErr)
}

// get issues one HTTP GET with the given auth attached (if non-empty).
// It returns the response Content-Type alongside the status; the body
// is read only on 200.
func (d *Downloader) get(ctx context.Context, rawURL string, auth slackhttp.TeamAuth) ([]byte, string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", 0, err
	}
	if auth.Token != "" {
		req.Header.Set("Authorization", "Bearer "+auth.Token)
	}
	if auth.DCookie != "" {
		// Inline cookie header: a shared cookie jar can hold only one
		// 'd' value at a time but workspaces may have different ones.
		req.Header.Set("Cookie", "d="+auth.DCookie)
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return nil, "", 0, err
	}
	defer resp.Body.Close()
	contentType := resp.Header.Get("Content-Type")
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, contentType, resp.StatusCode, nil
	}
	body, err := io.ReadAll(resp.Body)
	return body, contentType, resp.StatusCode, err
}

// writeUnique writes body to dir/name, suffixing "-2", "-3", ...
// before the extension on collision. Never overwrites.
func (d *Downloader) writeUnique(name string, body []byte) (string, error) {
	if err := os.MkdirAll(d.dir, 0o700); err != nil {
		return "", err
	}
	base := sanitizeName(name)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; ; i++ {
		candidate := base
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d%s", stem, i, ext)
		}
		path := filepath.Join(d.dir, candidate)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(body); err != nil {
			f.Close()
			return "", err
		}
		if err := f.Close(); err != nil {
			return "", err
		}
		return path, nil
	}
}

// sanitizeName makes a Slack file title safe for use as a local
// filename: path separators and control characters become '_', leading
// dots and surrounding whitespace are stripped, and an empty result
// falls back to "download".
func sanitizeName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	name = strings.TrimLeft(name, ".")
	if name == "" {
		return "download"
	}
	return name
}
