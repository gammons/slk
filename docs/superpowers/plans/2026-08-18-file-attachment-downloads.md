# File Attachment Names & Downloads Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render file attachments with filenames and add a `d` keybinding that downloads them (authenticated) and opens them in the OS default app, with a picker modal for multi-file messages.

**Architecture:** Extract the files.slack.com auth logic from `internal/image/fetcher.go` into a shared `slackhttp.AuthResolver`; add a new `internal/filedl` downloader that uses it; generalize `internal/ui/linkpicker` into a chooser that handles both links and files; wire a `d` keybinding that dispatches `DownloadFileMsg` directly (1 file) or via the picker (2+ files), mirroring the existing `o`/link flow.

**Tech Stack:** Go, bubbletea v2, lipgloss v2, slack-go. Module: `github.com/gammons/slk`.

**Spec:** `docs/superpowers/specs/2026-08-18-file-attachment-downloads-design.md`

## Global Constraints

- No new third-party dependencies.
- Never log tokens or cookies; auth headers are only attached to `files.slack.com` URLs (host check is exact-equality, see existing spoofing test).
- Download destination: `filepath.Join(os.TempDir(), "slk-files")`; never overwrite an existing file (suffix `-2`, `-3`, ...).
- Non-image files only for the `d` keybinding; images keep their existing preview flow (`O`/`v`).
- TDD: failing test first for every behavior change.
- Run `go build ./... && go test ./...` before each commit.

---

### Task 1: Extract file-auth resolver into internal/slackhttp

The image fetcher owns the files.slack.com auth logic (`TeamAuth`, `teamIDFromFilesURL`, `authsForURL`, learned-auth caching). Move it to `internal/slackhttp` so the new file downloader shares the same code path.

**Files:**
- Create: `internal/slackhttp/fileauth.go`
- Create: `internal/slackhttp/fileauth_test.go`
- Modify: `internal/image/fetcher.go` (struct fields ~line 107-114, `SetAuths` ~line 156, `download` ~line 375, delete `authsForURL` ~line 423 and `teamIDFromFilesURL` ~line 524)
- Delete: `internal/image/auth_url_test.go` (tests move to slackhttp)

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces:
  - `slackhttp.TeamAuth{TeamID, Token, DCookie string}`
  - `slackhttp.NewAuthResolver(auths []TeamAuth) *AuthResolver`
  - `(*AuthResolver).AuthsForURL(rawURL string) []TeamAuth`
  - `(*AuthResolver).Learn(teamID string, a TeamAuth)`
  - `slackhttp.TeamIDFromFilesURL(rawURL string) string`
  - `image.TeamAuth` becomes an alias: `type TeamAuth = slackhttp.TeamAuth`
  - `image.Fetcher.SetAuths(auths []TeamAuth)` keeps its signature.

- [ ] **Step 1: Write the failing test**

Create `internal/slackhttp/fileauth_test.go`:

```go
package slackhttp

import "testing"

func TestTeamIDFromFilesURL(t *testing.T) {
	cases := []struct{ name, url, want string }{
		{"files-pri canonical", "https://files.slack.com/files-pri/T01ABCDEF-F0123/foo.png", "T01ABCDEF"},
		{"files-tmb canonical", "https://files.slack.com/files-tmb/T01ABCDEF-F0123/foo_360.png", "T01ABCDEF"},
		{"files canonical", "https://files.slack.com/files/T01ABCDEF/foo.png", "T01ABCDEF"},
		{"query string", "https://files.slack.com/files-pri/T01ABCDEF-F0123/foo.png?t=abc", "T01ABCDEF"},
		{"spoof host suffix", "https://attacker.com/files.slack.com/files-pri/T01ABCDEF-F/x.png", ""},
		{"other host", "https://example.com/files-pri/T01ABCDEF-F/x.png", ""},
	}
	for _, c := range cases {
		if got := TeamIDFromFilesURL(c.url); got != c.want {
			t.Errorf("%s: TeamIDFromFilesURL(%q) = %q, want %q", c.name, c.url, got, c.want)
		}
	}
}

func TestAuthResolverKnownTeam(t *testing.T) {
	r := NewAuthResolver([]TeamAuth{
		{TeamID: "T1", Token: "xoxc-1", DCookie: "d1"},
		{TeamID: "T2", Token: "xoxc-2", DCookie: "d2"},
	})
	got := r.AuthsForURL("https://files.slack.com/files-pri/T2-F/doc.pdf")
	if len(got) != 1 || got[0].TeamID != "T2" {
		t.Fatalf("AuthsForURL = %#v", got)
	}
}

func TestAuthResolverForeignTeamFallbackAndLearn(t *testing.T) {
	r := NewAuthResolver([]TeamAuth{
		{TeamID: "T1", Token: "xoxc-1", DCookie: "d1"},
		{TeamID: "T2", Token: "xoxc-2", DCookie: "d2"},
	})
	foreign := "https://files.slack.com/files-pri/T9-F/doc.pdf"
	if got := r.AuthsForURL(foreign); len(got) != 2 {
		t.Fatalf("expected 2 fallbacks, got %#v", got)
	}
	r.Learn("T9", TeamAuth{TeamID: "T2", Token: "xoxc-2", DCookie: "d2"})
	got := r.AuthsForURL(foreign)
	if len(got) != 1 || got[0].TeamID != "T2" {
		t.Fatalf("after Learn, AuthsForURL = %#v", got)
	}
	// Learn must not overwrite a registered team.
	r.Learn("T1", TeamAuth{TeamID: "T2", Token: "xoxc-2"})
	if got := r.AuthsForURL("https://files.slack.com/files-pri/T1-F/x"); len(got) != 1 || got[0].TeamID != "T1" {
		t.Fatalf("Learn overwrote registered team: %#v", got)
	}
}

func TestAuthResolverSkipsEmptyAndNonSlack(t *testing.T) {
	r := NewAuthResolver([]TeamAuth{{TeamID: "", Token: "x"}, {TeamID: "T1", Token: ""}})
	if got := r.AuthsForURL("https://files.slack.com/files-pri/T1-F/x"); len(got) != 0 {
		t.Fatalf("expected no auths, got %#v", got)
	}
	if got := r.AuthsForURL("https://example.com/x.png"); got != nil {
		t.Fatalf("non-Slack URL should get nil auths, got %#v", got)
	}
}
```

Move the remaining cases from `internal/image/auth_url_test.go` into this file (adjusting to the exported `TeamIDFromFilesURL`), then delete `internal/image/auth_url_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/slackhttp/ -run TestTeamIDFromFilesURL -v`
Expected: FAIL — `undefined: TeamIDFromFilesURL`.

- [ ] **Step 3: Write the implementation**

Create `internal/slackhttp/fileauth.go`:

```go
package slackhttp

import (
	"net/url"
	"strings"
	"sync"
)

// TeamAuth pairs a Slack workspace's xoxc token with its 'd' cookie.
// Both are required to authenticate fetches on files.slack.com.
type TeamAuth struct {
	TeamID  string
	Token   string // xoxc-...
	DCookie string
}

// AuthResolver decides which workspace credentials (if any) to attach
// to a files.slack.com request. Shared by the image fetcher and the
// file downloader. Safe for concurrent use.
type AuthResolver struct {
	mu           sync.RWMutex
	authsByTeam  map[string]TeamAuth
	fallbacks    []TeamAuth // ordered; tried in sequence for foreign teams
	learnedAuths map[string]TeamAuth
}

// NewAuthResolver builds a resolver from per-workspace credentials.
// Entries with empty TeamID or Token are skipped. The slice order is
// the fallback order for foreign-team URLs (Slack Connect).
func NewAuthResolver(auths []TeamAuth) *AuthResolver {
	byTeam := make(map[string]TeamAuth, len(auths))
	fallbacks := make([]TeamAuth, 0, len(auths))
	for _, a := range auths {
		if a.TeamID == "" || a.Token == "" {
			continue
		}
		byTeam[a.TeamID] = a
		fallbacks = append(fallbacks, a)
	}
	return &AuthResolver{
		authsByTeam:  byTeam,
		fallbacks:    fallbacks,
		learnedAuths: map[string]TeamAuth{},
	}
}

// AuthsForURL returns the ordered list of auths to try for rawURL.
// Non-Slack URLs return nil (request goes out unauthenticated). For
// files.slack.com: the URL's own team auth if registered, else a
// previously learned auth, else the full ordered fallback list.
func (r *AuthResolver) AuthsForURL(rawURL string) []TeamAuth {
	teamID := TeamIDFromFilesURL(rawURL)
	if teamID == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if a, ok := r.authsByTeam[teamID]; ok {
		return []TeamAuth{a}
	}
	if a, ok := r.learnedAuths[teamID]; ok {
		return []TeamAuth{a}
	}
	return r.fallbacks
}

// Learn records that auth worked for teamID, so future AuthsForURL
// calls for that foreign team skip the fallback search. No-op when
// teamID is empty, auth is unusable, or teamID is already registered
// or learned.
func (r *AuthResolver) Learn(teamID string, auth TeamAuth) {
	if teamID == "" || auth.TeamID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.authsByTeam[teamID]; ok {
		return
	}
	if _, ok := r.learnedAuths[teamID]; ok {
		return
	}
	r.learnedAuths[teamID] = auth
}

// TeamIDFromFilesURL extracts the team ID embedded in a Slack file URL.
// Returns "" for URLs that aren't on files.slack.com or don't match a
// recognized path pattern.
//
// The host check uses url.Parse + exact equality rather than substring
// matching: a substring check would accept hostile URLs like
// https://attacker.com/files.slack.com/files-pri/T01ABCDEF/x.png and
// AuthsForURL would then attach the workspace's xoxc Bearer + 'd' cookie
// to the request, leaking the session to the attacker.
func TeamIDFromFilesURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	if u.Host != "files.slack.com" {
		return ""
	}
	rest := u.Path
	for _, prefix := range []string{"/files-tmb/", "/files-pri/", "/files/"} {
		if !strings.HasPrefix(rest, prefix) {
			continue
		}
		seg := rest[len(prefix):]
		if j := strings.IndexByte(seg, '/'); j >= 0 {
			seg = seg[:j]
		}
		if prefix == "/files/" {
			return seg
		}
		if j := strings.IndexByte(seg, '-'); j >= 0 {
			return seg[:j]
		}
		return seg
	}
	return ""
}
```

Then modify `internal/image/fetcher.go`:

1. Add import `"github.com/gammons/slk/internal/slackhttp"`.
2. Replace the `TeamAuth` struct definition with:

```go
// TeamAuth aliases slackhttp.TeamAuth so existing callers keep
// compiling; new code should use slackhttp.TeamAuth directly.
type TeamAuth = slackhttp.TeamAuth
```

3. Replace the auth-state fields (`authsByTeam`, `fallbacks`, `learnedAuths`) with:

```go
	// resolver decides which workspace credentials authenticate a
	// files.slack.com fetch (per-team map + Slack Connect fallback
	// retry + learned foreign-team mapping).
	resolver *slackhttp.AuthResolver
```

4. In `NewFetcher`, replace `authsByTeam: map[string]TeamAuth{},` with `resolver: slackhttp.NewAuthResolver(nil),`.
5. Replace the body of `SetAuths` with:

```go
func (f *Fetcher) SetAuths(auths []TeamAuth) {
	f.resolver = slackhttp.NewAuthResolver(auths)
}
```

6. In `download`, replace `authsToTry := f.authsForURL(url)` with `authsToTry := f.resolver.AuthsForURL(url)`, replace `[]TeamAuth{{}}` with `[]slackhttp.TeamAuth{{}}`, replace `teamID := teamIDFromFilesURL(url)` with `teamID := slackhttp.TeamIDFromFilesURL(url)`, and replace the learning block:

```go
		if status == http.StatusOK && strings.HasPrefix(strings.ToLower(ct), "image/") {
			// Success. Remember which auth worked for foreign-team URLs.
			f.resolver.Learn(teamID, auth)
			return body, ct, nil
		}
```

7. Delete the `authsForURL` method and the `teamIDFromFilesURL` function from fetcher.go.

- [ ] **Step 4: Run tests**

Run: `go build ./... && go test ./internal/slackhttp/ ./internal/image/`
Expected: PASS (including any pre-existing image fetcher tests).

- [ ] **Step 5: Commit**

```bash
git add internal/slackhttp internal/image
git commit -m "Extract files.slack.com auth resolution into slackhttp.AuthResolver"
```

---

### Task 2: filedl downloader package

**Files:**
- Create: `internal/filedl/filedl.go`
- Create: `internal/filedl/filedl_test.go`

**Interfaces:**
- Consumes: `slackhttp.AuthResolver`, `slackhttp.TeamAuth`, `slackhttp.TeamIDFromFilesURL` (Task 1).
- Produces:
  - `filedl.New(resolver *slackhttp.AuthResolver, dir string) *Downloader`
  - `(*Downloader).Download(ctx context.Context, rawURL, name string) (path string, err error)`

- [ ] **Step 1: Write the failing test**

Create `internal/filedl/filedl_test.go`:

```go
package filedl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gammons/slk/internal/slackhttp"
)

func TestDownloadWritesFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("a,b,c"))
	}))
	defer srv.Close()
	d := New(slackhttp.NewAuthResolver(nil), t.TempDir())
	path, err := d.Download(context.Background(), srv.URL+"/report.csv", "report.csv")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "a,b,c" {
		t.Errorf("body = %q", body)
	}
	if filepath.Base(path) != "report.csv" {
		t.Errorf("path = %q", path)
	}
}

func TestGetSendsAuthHeaders(t *testing.T) {
	var gotAuth, gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCookie = r.Header.Get("Cookie")
		w.Write([]byte("x"))
	}))
	defer srv.Close()
	d := New(slackhttp.NewAuthResolver(nil), t.TempDir())
	_, _, err := d.get(context.Background(), srv.URL, slackhttp.TeamAuth{TeamID: "T1", Token: "xoxc-test", DCookie: "cookie-test"})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer xoxc-test" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotCookie != "d=cookie-test" {
		t.Errorf("Cookie = %q", gotCookie)
	}
}

func TestDownloadCollisionSuffixes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("v"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	d := New(slackhttp.NewAuthResolver(nil), dir)
	p1, err := d.Download(context.Background(), srv.URL, "a.csv")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := d.Download(context.Background(), srv.URL, "a.csv")
	if err != nil {
		t.Fatal(err)
	}
	if p1 == p2 {
		t.Fatalf("second download overwrote first: %q", p1)
	}
	if filepath.Base(p2) != "a-2.csv" {
		t.Errorf("p2 = %q", p2)
	}
}

func TestDownloadHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	d := New(slackhttp.NewAuthResolver(nil), t.TempDir())
	if _, err := d.Download(context.Background(), srv.URL, "x.csv"); err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"report.csv":        "report.csv",
		"../evil.csv":       "_evil.csv",
		"a/b\\c.csv":        "a_b_c.csv",
		"  spaced.csv  ":    "spaced.csv",
		"":                  "download",
		"...":               "download",
		"bell\aname.csv":    "bell_name.csv",
	}
	for in, want := range cases {
		if got := sanitizeName(in); got != want {
			t.Errorf("sanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/filedl/ -v`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Write the implementation**

Create `internal/filedl/filedl.go`:

```go
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
		body, status, err := d.get(ctx, rawURL, auth)
		if err != nil {
			lastErr = err
			continue
		}
		if status != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d", status)
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
func (d *Downloader) get(ctx context.Context, rawURL string, auth slackhttp.TeamAuth) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
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
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, resp.StatusCode, nil
	}
	body, err := io.ReadAll(resp.Body)
	return body, resp.StatusCode, err
}

// writeUnique writes body to dir/name, suffixing "-2", "-3", ...
// before the extension on collision. Never overwrites.
func (d *Downloader) writeUnique(name string, body []byte) (string, error) {
	if err := os.MkdirAll(d.dir, 0o755); err != nil {
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
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
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
```

- [ ] **Step 4: Run tests**

Run: `go build ./... && go test ./internal/filedl/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/filedl
git commit -m "Add filedl package for authenticated Slack file downloads"
```

---

### Task 3: Attachment DownloadURL + Size fields

**Files:**
- Modify: `internal/ui/messages/model.go:55-64` (Attachment struct)
- Modify: `cmd/slk/main.go` (`extractAttachments`, ~line 2609)
- Test: `cmd/slk/attachments_test.go`

**Interfaces:**
- Produces: `messages.Attachment.DownloadURL string` (auth-gated `url_private`), `messages.Attachment.Size int64`.

- [ ] **Step 1: Write the failing test**

Add to `cmd/slk/attachments_test.go`:

```go
// TestExtractAttachmentsPopulatesDownloadFields confirms every
// attachment carries the auth-gated URLPrivate for the `d` download
// keybinding, plus the byte size for the picker row.
func TestExtractAttachmentsPopulatesDownloadFields(t *testing.T) {
	files := []slack.File{
		{
			ID:         "F1",
			Mimetype:   "text/csv",
			Title:      "report.csv",
			Permalink:  "https://team.slack.com/files/U/F1",
			URLPrivate: "https://files.slack.com/files-pri/T-F1/report.csv",
			Size:       1234,
		},
	}
	atts := extractAttachments(files)
	if len(atts) != 1 {
		t.Fatalf("got %d attachments", len(atts))
	}
	if atts[0].DownloadURL != files[0].URLPrivate {
		t.Errorf("DownloadURL = %q", atts[0].DownloadURL)
	}
	if atts[0].Size != 1234 {
		t.Errorf("Size = %d", atts[0].Size)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/slk/ -run TestExtractAttachmentsPopulatesDownloadFields -v`
Expected: FAIL — unknown field `DownloadURL`.

- [ ] **Step 3: Implement**

In `internal/ui/messages/model.go`, extend the Attachment struct:

```go
type Attachment struct {
	Kind string // "image" or "file"
	Name string // display filename / title
	URL  string // permalink (preferred) or url_private

	// DownloadURL is the auth-gated url_private, used by the `d`
	// download keybinding. Size is the file size in bytes (0 when
	// Slack didn't provide one); shown in the file picker.
	DownloadURL string
	Size        int64

	// Populated only for Kind == "image":
	FileID string      // Slack file ID for cache key
	Mime   string      // e.g. "image/png"
	Thumbs []ThumbSpec // sorted ascending; empty for non-image
}
```

In `cmd/slk/main.go` `extractAttachments`, after building `att`:

```go
		att := messages.Attachment{Kind: kind, Name: name, URL: pickAttachmentURL(f, kind)}
		att.DownloadURL = f.URLPrivate
		att.Size = int64(f.Size)
```

(replacing the existing single `att := ...` line).

- [ ] **Step 4: Run tests**

Run: `go build ./... && go test ./cmd/slk/ ./internal/ui/messages/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/slk internal/ui/messages
git commit -m "Carry DownloadURL and Size on file attachments"
```

---

### Task 4: Render attachment filenames

**Files:**
- Modify: `internal/ui/messages/render.go:122-163` (`RenderAttachments` doc + `renderSingleAttachment`)
- Modify: `internal/ui/imgrender/imgrender.go:442-461` (`renderLegacyLine`)
- Test: `internal/ui/messages/render_test.go`, `internal/ui/imgrender/imgrender_test.go`

**Interfaces:**
- Consumes: `Attachment.Name` (Task 3). No signature changes.

- [ ] **Step 1: Update the failing tests**

In `internal/ui/messages/render_test.go`, rewrite `TestRenderAttachmentsImageMarker` and `TestRenderAttachmentsFileMarker`:

```go
// TestRenderAttachmentsImageMarker asserts that an Image attachment
// renders with an [Image] marker and its filename, hyperlinked (OSC 8)
// to the URL. The raw URL is not shown in the visible text.
func TestRenderAttachmentsImageMarker(t *testing.T) {
	got := RenderAttachments([]Attachment{
		{Kind: "image", Name: "photo.png", URL: "https://files.slack.com/abc/xyz.png"},
	})
	plain := ansi.Strip(got)
	if !strings.Contains(plain, "[Image]") {
		t.Errorf("expected [Image] marker, got %q", plain)
	}
	if !strings.Contains(plain, "photo.png") {
		t.Errorf("expected filename visible, got %q", plain)
	}
	if strings.Contains(plain, "https://files.slack.com") {
		t.Errorf("raw URL should not be visible, got %q", plain)
	}
	if !strings.Contains(got, "\x1b]8;;https://files.slack.com/abc/xyz.png") {
		t.Error("expected OSC 8 hyperlink escape on attachment line")
	}
}

// TestRenderAttachmentsFileMarker confirms non-image attachments use
// the [File] marker and show the filename.
func TestRenderAttachmentsFileMarker(t *testing.T) {
	got := ansi.Strip(RenderAttachments([]Attachment{
		{Kind: "file", Name: "design.pdf", URL: "https://files.slack.com/x.pdf"},
	}))
	if !strings.Contains(got, "[File]") {
		t.Errorf("expected [File] marker, got %q", got)
	}
	if !strings.Contains(got, "design.pdf") {
		t.Errorf("expected filename visible, got %q", got)
	}
}

// TestRenderAttachmentsNamelessFallsBackToURL covers files whose
// title and filename are both empty: the raw URL is the label.
func TestRenderAttachmentsNamelessFallsBackToURL(t *testing.T) {
	got := ansi.Strip(RenderAttachments([]Attachment{
		{Kind: "file", URL: "https://files.slack.com/raw"},
	}))
	if !strings.Contains(got, "https://files.slack.com/raw") {
		t.Errorf("expected URL fallback, got %q", got)
	}
}
```

In `internal/ui/imgrender/imgrender_test.go`, extend `TestRenderBlock_NonImage_FallsBackToLegacyLine` after the existing `[File]` assertion:

```go
	if !strings.Contains(res.Lines[0], "doc.pdf") {
		t.Fatalf("expected filename in fallback, got %q", res.Lines[0])
	}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/messages/ -run TestRenderAttachments -v`
Expected: FAIL — filename assertions fail / old expectations reversed.

- [ ] **Step 3: Implement**

In `internal/ui/messages/render.go`, replace the `RenderAttachments` doc comment's "Filenames are intentionally omitted..." paragraph with:

```go
// The filename is the link label; the raw URL is not shown (it's in
// the OSC 8 escape). Attachments with no name fall back to the URL.
```

and rewrite `renderSingleAttachment`:

```go
// renderSingleAttachment formats one attachment as the single-line
// "[Image] <name>" / "[File] <name>" form, with the name wrapped in an
// OSC 8 hyperlink to the URL. Falls back to the URL as label when the
// attachment has no name. The messages-pane image-rendering pipeline
// uses this when no inline renderer is available (ProtoOff, missing
// thumbs) and the thread pane uses it via RenderAttachments for all
// attachments.
func renderSingleAttachment(a Attachment) string {
	markerStyle := lipgloss.NewStyle().Foreground(styles.TextMuted).Bold(true)
	urlStyle := linkStyle()
	marker := "[File]"
	if a.Kind == "image" {
		marker = "[Image]"
	}
	label := a.Name
	if label == "" {
		label = a.URL
	}
	body := markerStyle.Render(marker) + " " + urlStyle.Render(label)
	return osc8Hyperlink(a.URL, body)
}
```

In `internal/ui/imgrender/imgrender.go`, update `renderLegacyLine` (including its doc comment, which currently says it mirrors the messages helper "byte-for-byte" — keep that intent, now with the name):

```go
// renderLegacyLine returns the single-line "[Image] <name>" or
// "[File] <name>" fallback used when inline rendering is unavailable
// for an attachment. Mirrors the internal/ui/messages
// renderSingleAttachment helper (Bold marker style, underlined link
// style, OSC 8 wrapping the entire body, URL as label when name is
// empty), but takes an imgrender.Block to keep imgrender independent
// of the messages package.
func renderLegacyLine(att Block) string {
	markerStyle := lipgloss.NewStyle().Foreground(styles.TextMuted).Bold(true)
	urlStyle := lipgloss.NewStyle().Foreground(styles.Primary).Underline(true)
	marker := "[File]"
	if att.Kind == "image" {
		marker = "[Image]"
	}
	label := att.Name
	if label == "" {
		label = att.URL
	}
	body := markerStyle.Render(marker) + " " + urlStyle.Render(label)
	// OSC 8 hyperlink: ESC ] 8 ;; URL ESC \ LABEL ESC ] 8 ;; ESC \
	return "\x1b]8;;" + att.URL + "\x1b\\" + body + "\x1b]8;;\x1b\\"
}
```

- [ ] **Step 4: Run tests**

Run: `go build ./... && go test ./internal/ui/... ./internal/export/`
Expected: PASS. (`internal/ui/thread/render_test.go` still passes — it asserts only the `[File]` marker, which is unchanged.)

- [ ] **Step 5: Commit**

```bash
git add internal/ui/messages internal/ui/imgrender
git commit -m "Show attachment filenames instead of raw URLs"
```

---

### Task 5: Generalize linkpicker into a chooser

**Files:**
- Modify: `internal/ui/linkpicker/model.go`
- Modify: `internal/ui/linkpicker/view.go`
- Modify: `internal/ui/linkpicker/model_test.go`
- Modify: `internal/ui/app.go:1089` (Open call site)

**Interfaces:**
- Produces:
  - `linkpicker.Item{URL, Label, Detail string; InApp bool; Index int}` — `Detail` is trailing muted info (e.g. file size); `Index` is assigned by `Open` (position in the input slice).
  - `(*Model).Open(title string, items []Item)` — signature change.
  - `(*Model).Title() string` (for tests).
- Consumers updated in this task: `App.openLinksOfSelected` call site.

- [ ] **Step 1: Update the failing tests**

In `internal/ui/linkpicker/model_test.go`, change every `m.Open(items3())` to `m.Open("Open link", items3())` and `m.Open(nil)` to `m.Open("Open link", nil)`. Add:

```go
func TestOpenAssignsIndexAndTitle(t *testing.T) {
	m := New()
	m.Open("Download file", items3())
	if m.Title() != "Download file" {
		t.Errorf("title = %q", m.Title())
	}
	m.HandleKey("j")
	item, chosen := m.HandleKey("enter")
	if !chosen || item.Index != 1 {
		t.Errorf("chosen item = %+v chosen=%v", item, chosen)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/linkpicker/ -v`
Expected: FAIL — compile error on Open signature / undefined Title.

- [ ] **Step 3: Implement**

Replace `internal/ui/linkpicker/model.go`:

```go
// Package linkpicker provides the modal overlay that lets the user
// pick one item from a message: which link to open (the `o`
// keybinding) or which file attachment to download (the `d`
// keybinding). The chosen item is dispatched as ui.OpenLinkMsg or
// ui.DownloadFileMsg by the mode handler, depending on the kind the
// App recorded when opening the picker.
package linkpicker

// Item is one selectable row.
type Item struct {
	URL   string
	Label string // filename for file rows; link label (may be empty) for links
	// Detail is trailing muted info shown after the label (e.g. file
	// size). Empty for link rows.
	Detail string
	// InApp marks links that the router will navigate inside slk
	// (active-workspace archive permalinks); rendered with a badge.
	InApp bool
	// Index is the item's position in the slice passed to Open,
	// assigned by Open so the dispatcher can map the chosen row back
	// to its source data.
	Index int
}

// Model is the picker overlay state.
type Model struct {
	title    string
	items    []Item
	selected int
	visible  bool
}

// New creates a hidden picker.
func New() *Model { return &Model{} }

// Open shows the picker over items with the given dialog title, first
// row selected.
func (m *Model) Open(title string, items []Item) {
	m.title = title
	m.items = items
	for i := range m.items {
		m.items[i].Index = i
	}
	m.selected = 0
	m.visible = true
}

// Close hides the picker and drops its items.
func (m *Model) Close() {
	m.visible = false
	m.items = nil
	m.selected = 0
}

// IsVisible reports whether the picker is showing.
func (m *Model) IsVisible() bool { return m.visible }

// Title returns the dialog title set by Open.
func (m *Model) Title() string { return m.title }

// Items returns the current rows (for rendering and tests).
func (m *Model) Items() []Item { return m.items }

// Selected returns the highlighted row index.
func (m *Model) Selected() int { return m.selected }

// HandleKey processes one key. Returns (item, true) when the user
// chose a row with enter (the picker closes itself); (Item{}, false)
// otherwise. esc/q close without choosing.
func (m *Model) HandleKey(key string) (Item, bool) {
	switch key {
	case "esc", "q":
		m.Close()
	case "j", "down":
		if m.selected < len(m.items)-1 {
			m.selected++
		}
	case "k", "up":
		if m.selected > 0 {
			m.selected--
		}
	case "enter":
		if len(m.items) == 0 {
			return Item{}, false
		}
		item := m.items[m.selected]
		m.Close()
		return item, true
	}
	return Item{}, false
}
```

In `internal/ui/linkpicker/view.go`, replace the hardcoded title render with `Render(m.title)`, replace the footer text with `"j/k move   enter select   esc/q close"`, and replace the row-text computation:

```go
	for i, it := range m.items {
		var parts []string
		if it.Label != "" {
			parts = append(parts, it.Label)
		}
		if it.URL != "" && it.URL != it.Label {
			parts = append(parts, it.URL)
		}
		if it.Detail != "" {
			parts = append(parts, it.Detail)
		}
		text := strings.Join(parts, "  ")

		// Everything below this point in the loop stays exactly as it
		// is today: badge computation (InApp -> " [slk]"), truncation
		// against innerWidth, and the selected/unselected row styles.
```

In `internal/ui/app.go` `openLinksOfSelected`, change the picker branch:

```go
		a.linkPicker.Open("Open link", items)
```

- [ ] **Step 4: Run tests**

Run: `go build ./... && go test ./internal/ui/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui
git commit -m "Generalize linkpicker into a titled chooser with detail rows"
```

---

### Task 6: DownloadFileMsg, reducer, and download command

**Files:**
- Modify: `internal/ui/msgs.go` (after `OpenLinkMsg`, ~line 476)
- Create: `internal/ui/reducer_files.go`
- Modify: `internal/ui/app.go` (App struct fields ~line 273, reducers list ~line 608, new setter + `downloadFileCmd` near `openURLCmd` ~line 2265)
- Test: `internal/ui/reducer_files_test.go`

**Interfaces:**
- Consumes: `filedl.Downloader` (Task 2), `messages.Attachment` (Task 3).
- Produces:
  - `ui.DownloadFileMsg{Attachment messages.Attachment}`
  - `(*App).SetFileDownloader(d *filedl.Downloader)`
  - App fields used by Task 7: `pickerKind string`, `pickerFiles []messages.Attachment`.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/reducer_files_test.go`:

```go
package ui

import (
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
)

func TestDownloadFileMsg_NoDownloader_Toasts(t *testing.T) {
	app := NewApp()
	att := messages.Attachment{Kind: "file", Name: "a.csv", DownloadURL: "https://x"}
	_, cmd := app.Update(DownloadFileMsg{Attachment: att})
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	msg, ok := cmd().(ToastMsg)
	if !ok {
		t.Fatalf("expected ToastMsg, got %#v", cmd())
	}
	if msg.Text != "File downloads unavailable" {
		t.Errorf("toast = %q", msg.Text)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestDownloadFileMsg -v`
Expected: FAIL — undefined `DownloadFileMsg`.

- [ ] **Step 3: Implement**

In `internal/ui/msgs.go`, after the `OpenLinkMsg` declaration:

```go
// DownloadFileMsg requests download + OS-open of a file attachment.
// Dispatched by the `d` keybinding (directly for single-file messages)
// and by the picker modal for multi-file messages. Handled by
// reduceFiles.
type DownloadFileMsg struct{ Attachment messages.Attachment }
```

In `internal/ui/app.go`:

1. Add import `"github.com/gammons/slk/internal/filedl"` (and `"context"` if not already imported).
2. Add App fields (next to the `linkPicker` field):

```go
	// fileDownloader downloads file attachments for the `d`
	// keybinding. Nil in tests; downloadFileCmd toasts when unset.
	fileDownloader *filedl.Downloader

	// pickerKind records what the linkpicker modal is choosing:
	// "links" (Enter dispatches OpenLinkMsg) or "files" (Enter
	// dispatches DownloadFileMsg from pickerFiles).
	pickerKind  string
	pickerFiles []messages.Attachment
```

3. Add a setter (next to `SetImageFetcher`):

```go
// SetFileDownloader wires the file attachment downloader used by the
// `d` keybinding.
func (a *App) SetFileDownloader(d *filedl.Downloader) {
	a.fileDownloader = d
}
```

4. Add the download command (next to `openURLCmd`):

```go
// downloadFileCmd downloads att to the temp download dir and opens it
// in the OS default app. Runs async; the user gets a toast either way.
func (a *App) downloadFileCmd(att messages.Attachment) tea.Cmd {
	return func() tea.Msg {
		if a.fileDownloader == nil {
			return ToastMsg{Text: "File downloads unavailable"}
		}
		path, err := a.fileDownloader.Download(context.Background(), att.DownloadURL, att.Name)
		if err != nil {
			log.Printf("file download failed: %v", err)
			return ToastMsg{Text: "Download failed: " + att.Name}
		}
		if err := launchOS(path); err != nil {
			log.Printf("file open failed: %v", err)
			return ToastMsg{Text: "Failed to open " + att.Name}
		}
		return ToastMsg{Text: "Downloaded " + att.Name}
	}
}
```

5. Register the reducer in the reducers list after `reduceLinks,`:

```go
		reduceFiles,
```

Create `internal/ui/reducer_files.go`:

```go
// internal/ui/reducer_files.go
//
// File-download routing: DownloadFileMsg (dispatched by the `d`
// keybinding or the picker modal) starts an async download + OS open
// via App.downloadFileCmd.
package ui

import (
	tea "charm.land/bubbletea/v2"
)

var reduceFiles reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	m, ok := msg.(DownloadFileMsg)
	if !ok {
		return nil, false
	}
	return a.downloadFileCmd(m.Attachment), true
}
```

- [ ] **Step 4: Run tests**

Run: `go build ./... && go test ./internal/ui/ -run TestDownloadFileMsg -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui
git commit -m "Add DownloadFileMsg reducer and async download command"
```

---

### Task 7: `d` keybinding and picker integration

**Files:**
- Modify: `internal/ui/keys.go` (KeyMap struct ~line 42, DefaultKeyMap ~line 103)
- Modify: `internal/ui/mode_normal.go:272` (after the OpenLink case)
- Modify: `internal/ui/mode_linkpicker.go`
- Modify: `internal/ui/app.go` (`openLinksOfSelected` picker branch ~line 1084; new `downloadFilesOfSelected` + `humanSize` after it)
- Test: `internal/ui/download_files_test.go`

**Interfaces:**
- Consumes: everything from Tasks 3, 5, 6.
- Produces: `KeyMap.DownloadFile` binding (`d`, help "download file in message"). Help overlay picks it up automatically via `help.FromKeyMap` reflection.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/download_files_test.go`:

```go
package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/messages"
)

func pressD(app *App) tea.Cmd {
	return app.handleNormalMode(tea.KeyPressMsg{Code: 'd', Text: "d"})
}

func fileAtt(name string) messages.Attachment {
	return messages.Attachment{
		Kind:        "file",
		Name:        name,
		URL:         "https://team.slack.com/files/U/F",
		DownloadURL: "https://files.slack.com/files-pri/T-F/" + name,
	}
}

func TestDownloadKey_NoFiles_Toasts(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{{TS: "1.0", Text: "plain"}})
	cmd := pressD(app)
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	if _, ok := cmd().(ToastMsg); !ok {
		t.Errorf("expected ToastMsg, got %#v", cmd())
	}
}

func TestDownloadKey_SingleFile_DispatchesDownloadFileMsg(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "x", Attachments: []messages.Attachment{fileAtt("a.csv")}},
	})
	cmd := pressD(app)
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	msg, ok := cmd().(DownloadFileMsg)
	if !ok {
		t.Fatalf("expected DownloadFileMsg, got %#v", cmd())
	}
	if msg.Attachment.Name != "a.csv" {
		t.Errorf("attachment = %q", msg.Attachment.Name)
	}
	if app.mode != ModeNormal {
		t.Errorf("mode = %v, want ModeNormal (no modal for single file)", app.mode)
	}
}

func TestDownloadKey_MultipleFiles_OpensPicker(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "x", Attachments: []messages.Attachment{fileAtt("a.csv"), fileAtt("b.pdf")}},
	})
	cmd := pressD(app)
	if cmd != nil {
		t.Errorf("expected nil cmd (modal opens), got %#v", cmd())
	}
	if app.mode != ModeLinkPicker {
		t.Fatalf("mode = %v, want ModeLinkPicker", app.mode)
	}
	if !app.linkPicker.IsVisible() {
		t.Fatal("picker not visible")
	}
	if app.linkPicker.Title() != "Download file" {
		t.Errorf("title = %q", app.linkPicker.Title())
	}
	items := app.linkPicker.Items()
	if len(items) != 2 || items[0].Label != "a.csv" || items[1].Label != "b.pdf" {
		t.Errorf("items = %#v", items)
	}
}

func TestFilePickerMode_EnterDispatchesDownloadFileMsg(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "x", Attachments: []messages.Attachment{fileAtt("a.csv"), fileAtt("b.pdf")}},
	})
	pressD(app)
	app.handleKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
	cmd := app.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected cmd from enter")
	}
	msg, ok := cmd().(DownloadFileMsg)
	if !ok {
		t.Fatalf("expected DownloadFileMsg, got %#v", cmd())
	}
	if msg.Attachment.Name != "b.pdf" {
		t.Errorf("attachment = %q", msg.Attachment.Name)
	}
	if app.mode != ModeNormal {
		t.Errorf("mode = %v after choose", app.mode)
	}
}

func TestFilePickerMode_EscCloses(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "x", Attachments: []messages.Attachment{fileAtt("a.csv"), fileAtt("b.pdf")}},
	})
	pressD(app)
	cmd := app.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		t.Errorf("expected nil cmd, got %#v", cmd())
	}
	if app.mode != ModeNormal || app.linkPicker.IsVisible() {
		t.Errorf("mode=%v visible=%v after esc", app.mode, app.linkPicker.IsVisible())
	}
}

func TestDownloadKey_FromThreadPanel(t *testing.T) {
	app := NewApp()
	parent := messages.MessageItem{TS: "1.0", Text: "parent"}
	replies := []messages.MessageItem{
		{TS: "1.0", Text: "parent"},
		{TS: "2.0", Text: "x", Attachments: []messages.Attachment{fileAtt("t.csv")}},
	}
	app.threadPanel.SetThread(parent, replies, "C1", "1.0")
	app.threadVisible = true
	app.focusedPanel = PanelThread
	for i := 0; i < len(replies); i++ {
		if sel := app.threadPanel.SelectedReply(); sel != nil && sel.TS == "2.0" {
			break
		}
		app.threadPanel.MoveDown()
	}
	cmd := pressD(app)
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	msg, ok := cmd().(DownloadFileMsg)
	if !ok || msg.Attachment.Name != "t.csv" {
		t.Errorf("got %#v", cmd())
	}
}

// Images are excluded: they already have the preview flow (O/v).
func TestDownloadKey_SkipsImageAttachments(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "x", Attachments: []messages.Attachment{
			{Kind: "image", Name: "p.png", DownloadURL: "https://files.slack.com/x"},
		}},
	})
	cmd := pressD(app)
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	if _, ok := cmd().(ToastMsg); !ok {
		t.Errorf("expected ToastMsg for image-only message, got %#v", cmd())
	}
}
```

Also add a regression test to `internal/ui/open_links_test.go` confirming link picking still dispatches OpenLinkMsg after the pickerKind change:

```go
// TestLinkPickerMode_LinkKindUnaffected guards the shared picker:
// opening it for links must still dispatch OpenLinkMsg, not
// DownloadFileMsg.
func TestLinkPickerMode_LinkKindUnaffected(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "<https://a.example/1> <https://b.example/2>"},
	})
	pressO(app)
	cmd := app.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg, ok := cmd().(OpenLinkMsg)
	if !ok {
		t.Fatalf("expected OpenLinkMsg, got %#v", cmd())
	}
	if msg.URL != "https://a.example/1" {
		t.Errorf("URL = %q", msg.URL)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestDownloadKey|TestFilePicker' -v`
Expected: FAIL — `d` is unbound; `pickerKind` undefined.

- [ ] **Step 3: Implement**

1. `internal/ui/keys.go`: add `DownloadFile key.Binding` to the KeyMap struct (after `OpenLink`) and to DefaultKeyMap:

```go
		DownloadFile:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "download file in message")),
```

2. `internal/ui/mode_normal.go`, after the OpenLink case:

```go
	case key.Matches(msg, a.keys.DownloadFile):
		return a.downloadFilesOfSelected()
```

3. `internal/ui/mode_linkpicker.go`, replace `handleLinkPickerMode`:

```go
// internal/ui/mode_linkpicker.go
//
// Key handler for ModeLinkPicker: the chooser modal opened by the `o`
// keybinding (multiple links) or the `d` keybinding (multiple file
// attachments). Enter dispatches OpenLinkMsg or DownloadFileMsg
// depending on the kind recorded when the picker was opened; esc/q
// closes.
package ui

import (
	tea "charm.land/bubbletea/v2"
)

func handleLinkPickerMode(a *App, msg tea.KeyMsg) tea.Cmd {
	item, chosen := a.linkPicker.HandleKey(msg.String())
	if chosen {
		a.SetMode(ModeNormal)
		if a.pickerKind == "files" {
			files := a.pickerFiles
			a.pickerFiles = nil
			if item.Index < 0 || item.Index >= len(files) {
				return nil
			}
			att := files[item.Index]
			return func() tea.Msg { return DownloadFileMsg{Attachment: att} }
		}
		url := item.URL
		return func() tea.Msg { return OpenLinkMsg{URL: url} }
	}
	if !a.linkPicker.IsVisible() {
		// esc/q closed the picker.
		a.SetMode(ModeNormal)
		a.pickerFiles = nil
	}
	return nil
}
```

4. `internal/ui/app.go`:
   - In `openLinksOfSelected`, in the default (picker) branch, before `a.linkPicker.Open(...)` add:

```go
		a.pickerKind = "links"
```

   - After `openLinksOfSelected`, add:

```go
// downloadFilesOfSelected implements the `d` keybinding: collect the
// downloadable (non-image) file attachments of the selected message
// (messages pane or thread panel). 0 files -> toast; 1 file ->
// dispatch DownloadFileMsg directly; 2+ -> open the picker modal in
// "files" mode. Mirrors openLinksOfSelected. Images are excluded: they
// already have the preview flow (O/v).
func (a *App) downloadFilesOfSelected() tea.Cmd {
	var atts []messages.Attachment
	switch a.focusedPanel {
	case PanelMessages:
		msg, ok := a.messagepane.SelectedMessage()
		if !ok {
			return nil
		}
		atts = msg.Attachments
	case PanelThread:
		reply := a.threadPanel.SelectedReply()
		if reply == nil {
			return nil
		}
		atts = reply.Attachments
	default:
		return nil
	}
	files := make([]messages.Attachment, 0, len(atts))
	for _, att := range atts {
		if att.Kind == "file" && att.DownloadURL != "" {
			files = append(files, att)
		}
	}
	switch len(files) {
	case 0:
		return func() tea.Msg { return ToastMsg{Text: "No files in message"} }
	case 1:
		att := files[0]
		return func() tea.Msg { return DownloadFileMsg{Attachment: att} }
	default:
		items := make([]linkpicker.Item, len(files))
		for i, f := range files {
			items[i] = linkpicker.Item{Label: f.Name, Detail: humanSize(f.Size)}
		}
		a.pickerKind = "files"
		a.pickerFiles = files
		a.linkPicker.Open("Download file", items)
		a.SetMode(ModeLinkPicker)
		return nil
	}
}

// humanSize formats a byte count for the file picker row. Returns ""
// when the size is unknown (0).
func humanSize(n int64) string {
	switch {
	case n <= 0:
		return ""
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}
```

(`fmt` is already imported in app.go; verify with `go build`.)

- [ ] **Step 4: Run tests**

Run: `go build ./... && go test ./internal/ui/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui
git commit -m "Add d keybinding to download file attachments with picker"
```

---

### Task 8: Wire the downloader in main.go

**Files:**
- Modify: `cmd/slk/main.go` (auth setup ~line 965, `app.SetImageFetcher` ~line 1068)

**Interfaces:**
- Consumes: `slackhttp.NewAuthResolver`, `filedl.New`, `(*App).SetFileDownloader`.

No unit test here — this is wiring; verified by `go build` plus the full test suite. (The `d` keybinding behaves correctly with a nil downloader, covered in Task 6.)

- [ ] **Step 1: Implement**

In `cmd/slk/main.go`, add imports:

```go
	"github.com/gammons/slk/internal/filedl"
	"github.com/gammons/slk/internal/slackhttp"
```

After `imageFetcher.SetAuths(auths)`:

```go
	// File attachment downloads (`d` keybinding) share the image
	// fetcher's auth mechanism via slackhttp.AuthResolver. The
	// downloader gets its own resolver instance; it learns foreign-team
	// (Slack Connect) auth independently of the image fetcher.
	fileDownloader := filedl.New(slackhttp.NewAuthResolver(auths),
		filepath.Join(os.TempDir(), "slk-files"))
```

After `app.SetImageFetcher(imageFetcher)`:

```go
	app.SetFileDownloader(fileDownloader)
```

- [ ] **Step 2: Build and run the full suite**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 3: Manual smoke test (optional, requires a live workspace)**

Run the app, find a message with a CSV/PDF attachment:
- `[File] report.csv` renders (no raw URL).
- `d` downloads and opens it; toast confirms.
- A message with 2+ files shows the "Download file" picker; j/k + enter picks one; esc closes.
- `?` help overlay lists `d` — "download file in message".

- [ ] **Step 4: Commit**

```bash
git add cmd/slk
git commit -m "Wire file downloader into the app"
```

---

## Self-Review Notes

- Spec coverage: filename rendering (Task 4), temp-dir download + OS open (Tasks 2, 6, 8), `d` keybinding 0/1/2+ behavior (Task 7), picker generalization (Task 5), shared auth (Task 1), tests throughout. Export format (`internal/export`) intentionally unchanged.
- The `o` key flow is guarded by a new regression test in Task 7.
- `Attachment.URL` semantics unchanged (still the permalink) — OSC-8 click behavior preserved.
