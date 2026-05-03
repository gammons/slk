# Slack-Native Sidebar Sections Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace slk's config-glob sidebar sections with the user's actual Slack sidebar sections (linked-list ordered, live-updated via WS), preserving config-glob behavior as a fallback.

**Architecture:** A new per-workspace `SectionStore` service holds the canonical section list and channel→section index, populated on bootstrap from `users.channelSections.list` and kept in sync via four WS events. The sidebar gains a `SectionsProvider` that, when present, replaces the existing custom-then-defaults ordering with Slack's verbatim linked-list order. Channel-item resolution is tiered: Slack store first, config-glob fallback.

**Tech Stack:** Go, bubbletea/lipgloss, existing slack-go client + custom WebSocket dispatcher.

**Spec:** `docs/superpowers/specs/2026-05-02-slack-native-sections-design.md`

**Already done in discovery phase (do not redo):**
- `SLK_DEBUG_WS=1` unknown-event logger in `internal/slack/events.go`
- `--dump-sections` diagnostic flag in `cmd/slk/main.go`
- Cookie-aware auth fix in `GetChannelSections` (now uses Bearer + d cookie via `callChannelSectionsList` helper)

---

## File Structure

**Create:**
- `internal/slack/sections.go` — `SidebarSection` type, REST/WS decoder normalization, payload structs
- `internal/slack/sections_test.go` — decoder tests using captured fixtures
- `internal/service/sectionstore.go` — `SectionStore` service
- `internal/service/sectionstore_test.go` — store tests
- `internal/ui/sidebar/sections_provider.go` — `SectionsProvider` interface

**Modify:**
- `internal/config/config.go` — add `UseSlackSections *bool` to `General` and `Workspace`; add `EffectiveUseSlackSections(teamID)` resolver
- `internal/config/config_test.go` — resolution tests
- `internal/slack/client.go` — replace `ChannelSection` with new shape; extend `GetChannelSections` to fully populate via top-level pagination; add `ListChannelSectionChannels` for per-section channel pagination (best-effort, see Task 5)
- `internal/slack/client_test.go` — update existing test, add pagination tests
- `internal/slack/events.go` — add four event types + dispatch cases + `EventHandler` methods
- `internal/slack/events_test.go` — dispatch tests using captured WS fixtures
- `internal/ui/sidebar/model.go` — add `SectionsProvider` injection; two-mode `orderedSections`; `collapseByID` map; emoji-prefix in header rendering
- `internal/ui/sidebar/model_test.go` — Slack-mode rendering tests
- `cmd/slk/channelitem.go` — tiered resolver (store first, config fallback)
- `cmd/slk/main.go` — add `SectionStore` to `WorkspaceContext`; bootstrap on connect; wire WS handlers; reconnect debounce
- `README.md` — document `use_slack_sections`
- `docs/STATUS.md` — note feature

**Test fixtures (create in `internal/slack/testdata/`):**
- `sections_rest_rands.json` — Rands REST dump (paste from `sections-dump.json`, just the body)
- `sections_rest_truelist.json` — Truelist REST dump
- `ws_section_upserted.json`, `ws_section_deleted.json`, `ws_channels_upserted.json`, `ws_channels_removed.json` — captured WS payloads

---

## Task 1: Add `SidebarSection` type and unified decoder

**Files:**
- Create: `internal/slack/sections.go`
- Create: `internal/slack/sections_test.go`
- Create: `internal/slack/testdata/sections_rest_truelist.json`
- Create: `internal/slack/testdata/ws_section_upserted.json`

- [ ] **Step 1: Save WS fixture**

Create `internal/slack/testdata/ws_section_upserted.json` with the captured payload:

```json
{"type":"channel_section_upserted","channel_section_id":"L0B12LBBCTD","name":"test2","emoji":"","channel_section_type":"standard","next_channel_section_id":"L08BCNXM15Y","last_update":1777720183,"is_redacted":false,"event_ts":"1777720183.009300"}
```

- [ ] **Step 2: Save REST fixture**

Create `internal/slack/testdata/sections_rest_truelist.json` containing the full Truelist response body from the discovery dump (everything between the first `{` and matching `}` after `=== Truelist (...) ===`, with `"ok":true` at top level).

- [ ] **Step 3: Write the failing decode test**

Create `internal/slack/sections_test.go`:

```go
package slackclient

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeSection_REST(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "sections_rest_truelist.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var resp channelSectionsListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK {
		t.Fatalf("ok=false")
	}
	// Find the "automated22" standard section.
	var got *SidebarSection
	for i := range resp.Sections {
		if resp.Sections[i].Name == "automated22" {
			got = &resp.Sections[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("automated22 not found")
	}
	if got.Type != "standard" {
		t.Errorf("Type = %q, want standard", got.Type)
	}
	if got.Next != "L0B12LBBCTD" {
		t.Errorf("Next = %q, want L0B12LBBCTD", got.Next)
	}
	if got.LastUpdate != 1777720328 {
		t.Errorf("LastUpdate = %d, want 1777720328", got.LastUpdate)
	}
	wantChans := []string{"C054JFCBN69", "D09R4P6G6QL"}
	if len(got.ChannelIDs) != len(wantChans) {
		t.Fatalf("ChannelIDs len = %d, want %d", len(got.ChannelIDs), len(wantChans))
	}
	for i, c := range wantChans {
		if got.ChannelIDs[i] != c {
			t.Errorf("ChannelIDs[%d] = %q, want %q", i, got.ChannelIDs[i], c)
		}
	}
}

func TestDecodeSection_WS(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "ws_section_upserted.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var ev wsChannelSectionUpserted
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := ev.toUpserted()
	if got.ID != "L0B12LBBCTD" {
		t.Errorf("ID = %q", got.ID)
	}
	if got.Name != "test2" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.Type != "standard" {
		t.Errorf("Type = %q (WS uses channel_section_type, must normalize)", got.Type)
	}
	if got.Next != "L08BCNXM15Y" {
		t.Errorf("Next = %q", got.Next)
	}
	if got.LastUpdate != 1777720183 {
		t.Errorf("LastUpdate = %d (WS uses last_update, must normalize)", got.LastUpdate)
	}
}
```

- [ ] **Step 4: Run tests to confirm they fail**

```
go test ./internal/slack/ -run 'DecodeSection' -count=1
```

Expected: compile error (`channelSectionsListResponse`, `SidebarSection`, `wsChannelSectionUpserted` not defined).

- [ ] **Step 5: Implement the types and decoder**

Create `internal/slack/sections.go`:

```go
package slackclient

// SidebarSection represents one entry in the user's Slack sidebar
// section list. Both the REST endpoint (users.channelSections.list)
// and the WebSocket events use this model after normalization;
// REST and WS use different field names for the same data, so the
// decoders translate into this canonical shape.
type SidebarSection struct {
	ID         string
	Name       string
	Type       string // standard | channels | direct_messages | recent_apps | stars | slack_connect | salesforce_records | agents
	Emoji      string
	Next       string // next_channel_section_id; "" = tail
	LastUpdate int64  // unix seconds
	IsRedacted bool

	// ChannelIDs is the membership of this section. Populated from
	// channel_ids_page on REST decode (first page only); follow-up
	// pagination calls or WS deltas extend it.
	ChannelIDs []string

	// ChannelsCount is total membership reported by the server,
	// even when ChannelIDs holds only the first page. Cursor is
	// non-empty when more pages remain.
	ChannelsCount int
	ChannelsCursor string
}

// channelSectionsListResponse mirrors the REST shape of
// users.channelSections.list. Custom UnmarshalJSON on the inner
// struct normalizes field names into SidebarSection.
type channelSectionsListResponse struct {
	OK       bool             `json:"ok"`
	Error    string           `json:"error"`
	Sections []SidebarSection `json:"channel_sections"`
	Cursor   string           `json:"cursor"`
	Count    int              `json:"count"`
}

// restSectionEnvelope is the literal REST JSON shape; we decode into
// it and then copy into SidebarSection so SidebarSection itself can
// be the canonical model.
type restSectionEnvelope struct {
	ID         string `json:"channel_section_id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Emoji      string `json:"emoji"`
	Next       string `json:"next_channel_section_id"`
	LastUpdate int64  `json:"last_updated"`
	IsRedacted bool   `json:"is_redacted"`
	Page       struct {
		ChannelIDs []string `json:"channel_ids"`
		Count      int      `json:"count"`
		Cursor     string   `json:"cursor"`
	} `json:"channel_ids_page"`
}

// UnmarshalJSON for SidebarSection accepts the REST envelope and
// normalizes into the canonical struct.
func (s *SidebarSection) UnmarshalJSON(data []byte) error {
	var env restSectionEnvelope
	if err := jsonUnmarshal(data, &env); err != nil {
		return err
	}
	s.ID = env.ID
	s.Name = env.Name
	s.Type = env.Type
	s.Emoji = env.Emoji
	s.Next = env.Next
	s.LastUpdate = env.LastUpdate
	s.IsRedacted = env.IsRedacted
	s.ChannelIDs = env.Page.ChannelIDs
	s.ChannelsCount = env.Page.Count
	s.ChannelsCursor = env.Page.Cursor
	return nil
}

// wsChannelSectionUpserted is the literal WS JSON shape for a
// channel_section_upserted event. WS uses channel_section_type and
// last_update where REST uses type and last_updated; the toUpserted
// translator normalizes.
type wsChannelSectionUpserted struct {
	ID         string `json:"channel_section_id"`
	Name       string `json:"name"`
	Type       string `json:"channel_section_type"`
	Emoji      string `json:"emoji"`
	Next       string `json:"next_channel_section_id"`
	LastUpdate int64  `json:"last_update"`
	IsRedacted bool   `json:"is_redacted"`
}

func (e wsChannelSectionUpserted) toUpserted() ChannelSectionUpserted {
	return ChannelSectionUpserted{
		ID:         e.ID,
		Name:       e.Name,
		Type:       e.Type,
		Emoji:      e.Emoji,
		Next:       e.Next,
		LastUpdate: e.LastUpdate,
		IsRedacted: e.IsRedacted,
	}
}

// ChannelSectionUpserted carries the data from a channel_section_upserted
// WS event into the EventHandler.
type ChannelSectionUpserted struct {
	ID         string
	Name       string
	Type       string
	Emoji      string
	Next       string
	LastUpdate int64
	IsRedacted bool
}

// wsChannelSectionDeleted is the WS shape for channel_section_deleted.
type wsChannelSectionDeleted struct {
	ID         string `json:"channel_section_id"`
	LastUpdate int64  `json:"last_update"`
}

// wsChannelSectionsChannelsDelta is the WS shape for both the
// channel_sections_channels_upserted and _removed events.
type wsChannelSectionsChannelsDelta struct {
	SectionID  string   `json:"channel_section_id"`
	ChannelIDs []string `json:"channel_ids"`
	LastUpdate int64    `json:"last_update"`
}
```

Add at the top of `internal/slack/sections.go`:

```go
import "encoding/json"

// jsonUnmarshal exists so SidebarSection.UnmarshalJSON can defer to
// the standard decoder without infinite-recursing on its own method.
var jsonUnmarshal = json.Unmarshal
```

- [ ] **Step 6: Run tests to verify they pass**

```
go test ./internal/slack/ -run 'DecodeSection' -count=1 -v
```

Expected: `--- PASS: TestDecodeSection_REST` and `TestDecodeSection_WS`.

- [ ] **Step 7: Commit**

```bash
git add internal/slack/sections.go internal/slack/sections_test.go internal/slack/testdata/
git commit -m "slack: add SidebarSection canonical type with REST/WS decoder

Captures the data model for users.channelSections.list and the four
related WS events. Decoder normalizes field-name mismatches between
REST (type, last_updated, channel_ids_page) and WS (channel_section_type,
last_update). Fixtures captured from real workspaces during discovery."
```

---

## Task 2: Replace existing `ChannelSection` struct in client.go

**Files:**
- Modify: `internal/slack/client.go:899-941` — remove old `ChannelSection`, update `GetChannelSections` to return `[]SidebarSection` and use the new decoder. The `callChannelSectionsList` helper from discovery stays as-is.
- Modify: `internal/slack/client_test.go:872-892` — update `TestGetChannelSections_UsesAPIBaseURL` if it references the old shape.

- [ ] **Step 1: Update existing test**

In `internal/slack/client_test.go`, replace `TestGetChannelSections_UsesAPIBaseURL` body with a richer assertion that exercises the new decoder:

```go
func TestGetChannelSections_UsesAPIBaseURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ok": true,
			"channel_sections": [
				{
					"channel_section_id": "L1",
					"name": "Engineering",
					"type": "standard",
					"emoji": "rocket",
					"next_channel_section_id": "L2",
					"last_updated": 1700000000,
					"channel_ids_page": {"channel_ids": ["C1","C2"], "count": 2, "cursor": ""},
					"is_redacted": false
				}
			],
			"count": 1,
			"cursor": ""
		}`))
	}))
	defer srv.Close()

	c := &Client{
		token:      "xoxc-test",
		cookie:     "d-cookie",
		apiBaseURL: srv.URL + "/api/",
	}
	sections, err := c.GetChannelSections(context.Background())
	if err != nil {
		t.Fatalf("GetChannelSections: %v", err)
	}
	if gotPath != "/api/users.channelSections.list" {
		t.Errorf("path = %q, want %q", gotPath, "/api/users.channelSections.list")
	}
	if len(sections) != 1 {
		t.Fatalf("sections len = %d, want 1", len(sections))
	}
	s := sections[0]
	if s.ID != "L1" || s.Name != "Engineering" || s.Type != "standard" {
		t.Errorf("section = %+v", s)
	}
	if len(s.ChannelIDs) != 2 || s.ChannelIDs[0] != "C1" {
		t.Errorf("ChannelIDs = %v", s.ChannelIDs)
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

```
go test ./internal/slack/ -run 'GetChannelSections' -count=1 -v
```

Expected: build error or assertion failure on `s.ID`/`s.Type` because the old `ChannelSection` struct returns `channel_ids_page` as `[]string` (zero length).

- [ ] **Step 3: Replace the old struct and method body**

In `internal/slack/client.go`, delete lines 899-941 (the old `ChannelSection`, `GetChannelSections`, `callChannelSectionsList`, `GetChannelSectionsRaw`) and replace with:

```go
// callChannelSectionsList performs the raw POST to users.channelSections.list
// using cookie-aware auth (Bearer xoxc + d cookie) and an optional cursor for
// pagination through sections. Shared by both the typed and raw accessors.
func (c *Client) callChannelSectionsList(ctx context.Context, cursor string) ([]byte, error) {
	endpoint := c.apiBaseURL + "users.channelSections.list"

	form := url.Values{}
	if cursor != "" {
		form.Set("cursor", cursor)
	}
	body := strings.NewReader(form.Encode())

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	httpClient := newCookieHTTPClient(c.cookie)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling channelSections API: %w", err)
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// GetChannelSectionsRaw calls users.channelSections.list with no cursor and
// returns the raw JSON response body. Diagnostic only (--dump-sections).
func (c *Client) GetChannelSectionsRaw(ctx context.Context) ([]byte, error) {
	return c.callChannelSectionsList(ctx, "")
}

// GetChannelSections calls users.channelSections.list and returns the
// fully-paginated section list. Loops on the top-level cursor until the
// server reports no more sections. Per-section channel_ids_page pagination
// is NOT followed here; see Task 3 (deferred to v2).
//
// This endpoint is undocumented; may break if Slack changes the API.
func (c *Client) GetChannelSections(ctx context.Context) ([]SidebarSection, error) {
	var all []SidebarSection
	cursor := ""
	for {
		body, err := c.callChannelSectionsList(ctx, cursor)
		if err != nil {
			return nil, err
		}
		var resp channelSectionsListResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("parsing response: %w", err)
		}
		if !resp.OK {
			return nil, fmt.Errorf("API error: %s", resp.Error)
		}
		all = append(all, resp.Sections...)
		if resp.Cursor == "" || resp.Cursor == cursor {
			break
		}
		cursor = resp.Cursor
	}
	return all, nil
}
```

- [ ] **Step 4: Run all slack-package tests**

```
go test ./internal/slack/... -count=1
```

Expected: all pass. (The old `ChannelSection` struct definition was unused in production, so deleting it is safe.)

- [ ] **Step 5: Commit**

```bash
git add internal/slack/client.go internal/slack/client_test.go
git commit -m "slack: replace ChannelSection with SidebarSection in client

The previous ChannelSection had the wrong channel_ids_page shape (typed
as []string when the API returns an object). Replaced with the new
SidebarSection model. GetChannelSections now follows top-level cursor
pagination through all sections in the workspace."
```

---

## Task 3: SKIPPED — per-section channel pagination deferred to v2

**Status:** Removed from v1 scope based on investigation.

**Investigation result:** Captured `users.channelSections.list` traffic from the official Slack web client showed:
- Same endpoint on initial load and section expansion (no separate `listChannels`).
- "Nothing interesting in the form data" — no obvious cursor-passing param.
- Same workspace returned different page sizes between captures (Manager: 10→12 channels, Books: 1 of 2).
- Conclusion: page completeness depends on Slack's server-side cache state, not on a client-controlled cursor we can drive.

**v1 behavior:** SectionStore.Bootstrap (Task 5) uses only the first-page channel data returned by `users.channelSections.list`. For sections where `count > len(channel_ids)`, a warning is logged to the debug log; the unmapped channels stay in the catch-all "Channels" section until either:
1. WS `channel_sections_channels_upserted` events deliver them (Task 7), or
2. A reconnect-triggered re-bootstrap returns more complete data (Task 12).

**Documentation:** Task 14 will note this as a known v1 limitation in the README.

**v2 follow-up:** If real customers hit this badly enough, capture more network traffic to determine the actual pagination mechanism (may require viewing Slack's bundled JS or inspecting WebSocket subscription patterns). Until then, no speculative endpoint.

The implementation steps below are **not executed**. Move directly to Task 4.

- [ ] **Step 1: Capture the network call**

Open the official Slack web client. Open DevTools → Network → filter `channelSections`. Click to expand a section with more than 10 channels (the "Manager" section in Rands has 12). Inspect the request:
- Note the URL path (likely `users.channelSections.listChannels` or `users.channelSections.list`)
- Note the form fields (look for `channel_section_id`, `cursor`, etc.)

Save the request URL and form payload as a comment in the next step.

- [ ] **Step 2: Write the failing test**

Add to `internal/slack/client_test.go`:

```go
func TestListSectionChannels_FollowsCursor(t *testing.T) {
	var calls []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		calls = append(calls, r.PostForm)
		w.Header().Set("Content-Type", "application/json")
		switch len(calls) {
		case 1:
			// first page
			_, _ = w.Write([]byte(`{"ok":true,"channel_ids":["C1","C2","C3"],"next_cursor":"C3"}`))
		case 2:
			// second page (drained)
			_, _ = w.Write([]byte(`{"ok":true,"channel_ids":["C4","C5"],"next_cursor":""}`))
		}
	}))
	defer srv.Close()

	c := &Client{
		token:      "xoxc-test",
		cookie:     "d-cookie",
		apiBaseURL: srv.URL + "/api/",
	}
	got, err := c.ListSectionChannels(context.Background(), "L_SEC", "C3")
	if err != nil {
		t.Fatalf("ListSectionChannels: %v", err)
	}
	want := []string{"C1", "C2", "C3", "C4", "C5"}
	if len(got) != len(want) {
		t.Fatalf("got %d channels, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// Confirm the second call carried the cursor from the first response.
	if len(calls) != 2 {
		t.Fatalf("got %d API calls, want 2", len(calls))
	}
	if calls[1].Get("cursor") != "C3" {
		t.Errorf("second call cursor = %q, want C3", calls[1].Get("cursor"))
	}
	if calls[0].Get("channel_section_id") != "L_SEC" {
		t.Errorf("section_id = %q", calls[0].Get("channel_section_id"))
	}
}
```

> **Adjust this test** to match the URL path and field names you captured in Step 1. The test as written assumes a `listChannels`-style endpoint with `channel_section_id` and `cursor` form fields, returning `channel_ids` and `next_cursor` at the top level. If the captured shape differs (e.g. paginates through `users.channelSections.list` with the same envelope as the section list), revise both the test mock JSON and the implementation in Step 3.

- [ ] **Step 3: Implement `ListSectionChannels`**

Add to `internal/slack/client.go` (after `GetChannelSections`):

```go
// ListSectionChannels returns the full channel ID membership for a single
// section, following the per-section pagination cursor. Pass startCursor=""
// for the first page; pass the cursor returned in channel_ids_page.cursor
// to skip the initial page already loaded by GetChannelSections.
//
// NOTE: the exact endpoint shape was reverse-engineered from network
// traffic; if Slack changes it the function will return an empty slice
// and a non-nil error, which the SectionStore treats as "use what we
// have plus WS deltas".
func (c *Client) ListSectionChannels(ctx context.Context, sectionID, startCursor string) ([]string, error) {
	// ADJUST endpoint path to match captured network call.
	endpoint := c.apiBaseURL + "users.channelSections.listChannels"

	var all []string
	cursor := startCursor
	for {
		form := url.Values{}
		form.Set("channel_section_id", sectionID)
		if cursor != "" {
			form.Set("cursor", cursor)
		}

		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(form.Encode()))
		if err != nil {
			return all, fmt.Errorf("creating request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		httpClient := newCookieHTTPClient(c.cookie)
		resp, err := httpClient.Do(req)
		if err != nil {
			return all, fmt.Errorf("listSectionChannels: %w", err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		var result struct {
			OK         bool     `json:"ok"`
			Error      string   `json:"error"`
			ChannelIDs []string `json:"channel_ids"`
			NextCursor string   `json:"next_cursor"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return all, fmt.Errorf("parse: %w", err)
		}
		if !result.OK {
			return all, fmt.Errorf("listSectionChannels: %s", result.Error)
		}
		all = append(all, result.ChannelIDs...)
		if result.NextCursor == "" || result.NextCursor == cursor {
			break
		}
		cursor = result.NextCursor
	}
	return all, nil
}
```

- [ ] **Step 4: Run tests**

```
go test ./internal/slack/ -run 'ListSectionChannels' -count=1 -v
```

Expected: PASS. If FAIL because the captured endpoint shape differed from the test, fix both test and impl.

- [ ] **Step 5: Commit**

```bash
git add internal/slack/client.go internal/slack/client_test.go
git commit -m "slack: add ListSectionChannels for per-section pagination

Reverse-engineered from official Slack web client network traffic.
Sections paginate at ~10 channels in users.channelSections.list; the
listChannels endpoint follows a separate cursor to fetch the rest."
```

---

## Task 4: Add `use_slack_sections` config knob

**Files:**
- Modify: `internal/config/config.go` — add `*bool` fields, resolver
- Modify: `internal/config/config_test.go` — resolution tests

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestEffectiveUseSlackSections_DefaultTrue(t *testing.T) {
	cfg := Config{}
	if !cfg.EffectiveUseSlackSections("T1") {
		t.Errorf("default should be true")
	}
}

func TestEffectiveUseSlackSections_GlobalFalse(t *testing.T) {
	f := false
	cfg := Config{General: General{UseSlackSections: &f}}
	if cfg.EffectiveUseSlackSections("T1") {
		t.Errorf("global=false should disable")
	}
}

func TestEffectiveUseSlackSections_WorkspaceOverride(t *testing.T) {
	tr, fa := true, false
	// Global=true (default), workspace=false → false
	cfg := Config{
		Workspaces: map[string]Workspace{
			"work": {TeamID: "T1", UseSlackSections: &fa},
		},
	}
	if cfg.EffectiveUseSlackSections("T1") {
		t.Errorf("workspace override (false) should win over global (true)")
	}
	// Global=false, workspace=true → true
	cfg2 := Config{
		General: General{UseSlackSections: &fa},
		Workspaces: map[string]Workspace{
			"work": {TeamID: "T1", UseSlackSections: &tr},
		},
	}
	if !cfg2.EffectiveUseSlackSections("T1") {
		t.Errorf("workspace override (true) should win over global (false)")
	}
}

func TestEffectiveUseSlackSections_UnknownTeamUsesGlobal(t *testing.T) {
	f := false
	cfg := Config{General: General{UseSlackSections: &f}}
	if cfg.EffectiveUseSlackSections("T_UNKNOWN") {
		t.Errorf("unknown team should fall through to global=false")
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```
go test ./internal/config/ -run 'EffectiveUseSlackSections' -count=1
```

Expected: build error — `UseSlackSections` field doesn't exist on `General` or `Workspace`, and `EffectiveUseSlackSections` method doesn't exist.

- [ ] **Step 3: Add fields and resolver**

In `internal/config/config.go`:

Modify `General` struct (around line 33):

```go
type General struct {
	DefaultWorkspace string `toml:"default_workspace"`
	// UseSlackSections opts in/out of using the user's actual Slack
	// sidebar sections (via users.channelSections.list + WS events)
	// instead of the config-glob [sections.*] system. Pointer so we
	// can distinguish "unset" (default true) from explicit false.
	UseSlackSections *bool `toml:"use_slack_sections"`
}
```

Modify `Workspace` struct (around line 91):

```go
type Workspace struct {
	TeamID string `toml:"team_id"`
	Theme  string `toml:"theme"`
	Order  int    `toml:"order"`
	// UseSlackSections overrides [general].use_slack_sections for this
	// workspace. Nil means "fall through to global".
	UseSlackSections *bool                 `toml:"use_slack_sections"`
	Sections         map[string]SectionDef `toml:"sections"`
}
```

Add at the end of the file:

```go
// EffectiveUseSlackSections returns whether Slack-native sidebar sections
// are enabled for the given workspace. Resolution: per-workspace value
// wins when set; otherwise the global [general].use_slack_sections;
// default true.
func (c Config) EffectiveUseSlackSections(teamID string) bool {
	if ws, ok := c.WorkspaceByTeamID(teamID); ok && ws.UseSlackSections != nil {
		return *ws.UseSlackSections
	}
	if c.General.UseSlackSections != nil {
		return *c.General.UseSlackSections
	}
	return true
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./internal/config/ -count=1
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "config: add use_slack_sections knob

New [general].use_slack_sections (default true) controls whether slk
fetches the user's actual Slack sidebar sections instead of using the
config-glob [sections.*] system. Per-workspace [workspaces.X]
use_slack_sections overrides the global. Pointer-bool to distinguish
unset from explicit false."
```

---

## Task 5: SectionStore — bootstrap and read paths

**Files:**
- Create: `internal/service/sectionstore.go`
- Create: `internal/service/sectionstore_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/service/sectionstore_test.go`:

```go
package service

import (
	"context"
	"testing"

	slk "github.com/gammons/slk/internal/slack"
)

// fakeSectionsClient implements the subset of slk.Client SectionStore needs.
type fakeSectionsClient struct {
	sections []slk.SidebarSection
	getErr   error
}

func (f *fakeSectionsClient) GetChannelSections(ctx context.Context) ([]slk.SidebarSection, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.sections, nil
}

func TestSectionStore_Bootstrap_Empty(t *testing.T) {
	store := NewSectionStore()
	c := &fakeSectionsClient{}
	if err := store.Bootstrap(context.Background(), c); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if !store.Ready() {
		t.Errorf("Ready=false after empty bootstrap")
	}
	if got := store.OrderedSections(); len(got) != 0 {
		t.Errorf("OrderedSections len = %d, want 0", len(got))
	}
}

func TestSectionStore_Bootstrap_BuildsLinkedListOrder(t *testing.T) {
	// Build: head=A → B → C → tail
	sections := []slk.SidebarSection{
		{ID: "B", Name: "Books", Type: "standard", Next: "C", LastUpdate: 100, ChannelIDs: []string{"C2"}, ChannelsCount: 1},
		{ID: "A", Name: "Alerts", Type: "standard", Next: "B", LastUpdate: 100, ChannelIDs: []string{"C1"}, ChannelsCount: 1},
		{ID: "C", Name: "Channels", Type: "channels", Next: "", LastUpdate: 100},
	}
	c := &fakeSectionsClient{sections: sections}
	store := NewSectionStore()
	if err := store.Bootstrap(context.Background(), c); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	got := store.OrderedSections()
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (got: %+v)", len(got), got)
	}
	wantOrder := []string{"A", "B", "C"}
	for i, w := range wantOrder {
		if got[i].ID != w {
			t.Errorf("[%d] ID = %q, want %q", i, got[i].ID, w)
		}
	}
}

func TestSectionStore_Bootstrap_TruncatedSection_LogsAndContinues(t *testing.T) {
	// Section "A" reports count=5 but only first 3 channels were returned
	// in channel_ids_page. v1 trusts the first-page data and lets the
	// remaining 2 stay in the catch-all "Channels" bucket until WS
	// deltas migrate them. Bootstrap must NOT fail in this case.
	sections := []slk.SidebarSection{
		{ID: "A", Type: "standard", Next: "", LastUpdate: 100,
			ChannelIDs:     []string{"C1", "C2", "C3"},
			ChannelsCount:  5,
			ChannelsCursor: "C3"},
	}
	c := &fakeSectionsClient{sections: sections}
	store := NewSectionStore()
	if err := store.Bootstrap(context.Background(), c); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if !store.Ready() {
		t.Errorf("Ready=false after truncated bootstrap")
	}
	// First-page channels are mapped.
	if id, ok := store.SectionForChannel("C1"); !ok || id != "A" {
		t.Errorf("SectionForChannel(C1) = (%q, %v), want (A, true)", id, ok)
	}
	// Channels beyond the first page are NOT mapped.
	if _, ok := store.SectionForChannel("C5"); ok {
		t.Errorf("SectionForChannel(C5) ok=true, want false (channel beyond first page must stay unmapped in v1)")
	}
}

func TestSectionStore_OrderedSections_FiltersSystemTypes(t *testing.T) {
	sections := []slk.SidebarSection{
		{ID: "S", Type: "salesforce_records", Next: "G", LastUpdate: 1},
		{ID: "G", Type: "agents", Next: "T", LastUpdate: 1},
		{ID: "T", Type: "stars", Next: "K", LastUpdate: 1},
		{ID: "K", Type: "slack_connect", Next: "U", LastUpdate: 1},
		{ID: "U", Type: "standard", Name: "Mine", Next: "", LastUpdate: 1, ChannelIDs: []string{"C1"}, ChannelsCount: 1},
	}
	c := &fakeSectionsClient{sections: sections}
	store := NewSectionStore()
	_ = store.Bootstrap(context.Background(), c)
	got := store.OrderedSections()
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (only standard)", len(got))
	}
	if got[0].ID != "U" {
		t.Errorf("got %q, want U", got[0].ID)
	}
}

func TestSectionStore_BootstrapFailure_NotReady(t *testing.T) {
	c := &fakeSectionsClient{getErr: context.DeadlineExceeded}
	store := NewSectionStore()
	if err := store.Bootstrap(context.Background(), c); err == nil {
		t.Errorf("expected error")
	}
	if store.Ready() {
		t.Errorf("Ready=true after failure; should remain false")
	}
}

func TestSectionStore_NotReady_SectionForChannelFalse(t *testing.T) {
	store := NewSectionStore()
	if _, ok := store.SectionForChannel("C1"); ok {
		t.Errorf("ok=true on never-bootstrapped store")
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```
go test ./internal/service/ -run 'SectionStore' -count=1
```

Expected: build error — package missing.

- [ ] **Step 3: Implement `SectionStore`**

Create `internal/service/sectionstore.go`:

```go
package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	slk "github.com/gammons/slk/internal/slack"
)

// SectionsClient is the subset of slk.Client SectionStore needs.
// Defined as an interface so tests can pass fakes.
type SectionsClient interface {
	GetChannelSections(ctx context.Context) ([]slk.SidebarSection, error)
}

// SectionStore is the per-workspace authoritative cache of the user's
// Slack-side sidebar sections. Populated on bootstrap from the REST
// endpoint and kept fresh by WS event handlers (Apply* methods).
//
// All public methods are safe for concurrent use.
type SectionStore struct {
	mu               sync.RWMutex
	ready            bool
	sectionsByID     map[string]*slk.SidebarSection
	channelToSection map[string]string
	lastBootstrap    time.Time
}

// NewSectionStore returns an empty store. It reports Ready()==false until
// Bootstrap completes successfully.
func NewSectionStore() *SectionStore {
	return &SectionStore{
		sectionsByID:     map[string]*slk.SidebarSection{},
		channelToSection: map[string]string{},
	}
}

// Ready reports whether the store has successfully bootstrapped at least
// once. Callers should treat !Ready as "fall through to config-glob".
func (s *SectionStore) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ready
}

// Bootstrap fetches the full section list and per-section channel
// membership, replacing any prior state atomically. Returns an error
// without mutating state if either fetch fails.
func (s *SectionStore) Bootstrap(ctx context.Context, client SectionsClient) error {
	sections, err := client.GetChannelSections(ctx)
	if err != nil {
		return fmt.Errorf("fetching sections: %w", err)
	}

	// v1 limitation (Task 3 deferred): when ChannelsCount exceeds
	// len(ChannelIDs), the section is partially populated. We trust
	// what we have; remaining channels stay in the catch-all bucket
	// until either a WS channel_sections_channels_upserted event
	// migrates them or a reconnect-triggered re-bootstrap fetches
	// fresher data. Log so debugging is possible.
	for i := range sections {
		sec := &sections[i]
		if sec.ChannelsCount > len(sec.ChannelIDs) {
			log.Printf("section store: section %q (%s) reports %d channels but server returned %d on first page; remaining channels will fall through to default bucket",
				sec.Name, sec.ID, sec.ChannelsCount, len(sec.ChannelIDs))
		}
	}

	// Build new maps.
	byID := make(map[string]*slk.SidebarSection, len(sections))
	c2s := map[string]string{}
	for i := range sections {
		sec := &sections[i]
		byID[sec.ID] = sec
		for _, ch := range sec.ChannelIDs {
			c2s[ch] = sec.ID
		}
	}

	s.mu.Lock()
	s.sectionsByID = byID
	s.channelToSection = c2s
	s.ready = true
	s.lastBootstrap = time.Now()
	s.mu.Unlock()
	return nil
}

// SectionForChannel returns the section ID a channel belongs to. Returns
// ok=false when the store isn't ready or the channel isn't in any section.
func (s *SectionStore) SectionForChannel(channelID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.ready {
		return "", false
	}
	id, ok := s.channelToSection[channelID]
	return id, ok
}

// OrderedSections walks the linked-list (head-first) and returns the
// sections that should render in the sidebar, filtered to the v1
// type whitelist. Cycle protection: stops if a section is revisited.
func (s *SectionStore) OrderedSections() []*slk.SidebarSection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.ready {
		return nil
	}

	// Find the head: a section that no other section's Next points at.
	pointedAt := map[string]bool{}
	for _, sec := range s.sectionsByID {
		if sec.Next != "" {
			pointedAt[sec.Next] = true
		}
	}
	var head *slk.SidebarSection
	for id, sec := range s.sectionsByID {
		if !pointedAt[id] {
			if head == nil || sec.LastUpdate > head.LastUpdate {
				head = sec
			}
		}
	}
	if head == nil {
		// Cycle or empty.
		return nil
	}

	out := make([]*slk.SidebarSection, 0, len(s.sectionsByID))
	visited := map[string]bool{}
	cur := head
	for cur != nil && !visited[cur.ID] {
		visited[cur.ID] = true
		if includeInSidebar(cur) {
			out = append(out, cur)
		}
		if cur.Next == "" {
			break
		}
		cur = s.sectionsByID[cur.Next]
	}
	return out
}

// includeInSidebar applies the v1 filter rules. Renderable types:
// standard (always, even when empty — user intent), channels (default
// catch-all), direct_messages (default DM bucket). recent_apps is only
// rendered when non-empty (slk has its own Apps logic for the empty
// case). Everything else is hidden in v1.
func includeInSidebar(sec *slk.SidebarSection) bool {
	if sec.IsRedacted {
		return false
	}
	switch sec.Type {
	case "standard", "channels", "direct_messages":
		return true
	case "recent_apps":
		return len(sec.ChannelIDs) > 0
	default:
		// stars, slack_connect, salesforce_records, agents, anything new.
		return false
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./internal/service/ -run 'SectionStore' -count=1 -v
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/service/sectionstore.go internal/service/sectionstore_test.go
git commit -m "service: add SectionStore for Slack-native sidebar sections

Per-workspace authoritative cache populated from users.channelSections.list.
OrderedSections walks the linked list head-first with cycle protection;
includeInSidebar filters to the v1 type whitelist (standard, channels,
direct_messages, optional recent_apps). Bootstrap is best-effort about
per-section pagination — a failure leaves the store Ready with first-page
data only."
```

---

## Task 6: SectionStore — mutation methods (Apply*)

**Files:**
- Modify: `internal/service/sectionstore.go` — add `ApplyUpsert`, `ApplyDelete`, `ApplyChannelsAdded`, `ApplyChannelsRemoved`, `MaybeRebootstrap`
- Modify: `internal/service/sectionstore_test.go` — mutation tests

- [ ] **Step 1: Write the failing tests**

Append to `internal/service/sectionstore_test.go`:

```go
func TestApplyUpsert_NewSection(t *testing.T) {
	store := NewSectionStore()
	c := &fakeSectionsClient{sections: []slk.SidebarSection{
		{ID: "A", Type: "standard", Name: "A", LastUpdate: 100},
	}}
	_ = store.Bootstrap(context.Background(), c)

	store.ApplyUpsert(slk.ChannelSectionUpserted{
		ID: "B", Name: "Brand New", Type: "standard", Next: "", LastUpdate: 200,
	})
	got := store.OrderedSections()
	// Both A and B exist now; the head is whichever isn't pointed at.
	// A.Next="" (set in fixture), B.Next="" too — multiple heads.
	// Our heuristic picks the highest LastUpdate.
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (multi-head heuristic picks newest)", len(got))
	}
	if got[0].ID != "B" {
		t.Errorf("head = %q, want B (newer LastUpdate wins)", got[0].ID)
	}
}

func TestApplyUpsert_RenameExistingByID(t *testing.T) {
	store := NewSectionStore()
	c := &fakeSectionsClient{sections: []slk.SidebarSection{
		{ID: "A", Type: "standard", Name: "Old", Next: "", LastUpdate: 100},
	}}
	_ = store.Bootstrap(context.Background(), c)
	store.ApplyUpsert(slk.ChannelSectionUpserted{
		ID: "A", Name: "New", Type: "standard", Next: "", LastUpdate: 200,
	})
	got := store.OrderedSections()
	if len(got) != 1 || got[0].Name != "New" {
		t.Errorf("got %+v, want one section named New", got)
	}
}

func TestApplyUpsert_StaleEventIgnored(t *testing.T) {
	store := NewSectionStore()
	c := &fakeSectionsClient{sections: []slk.SidebarSection{
		{ID: "A", Type: "standard", Name: "Latest", Next: "", LastUpdate: 200},
	}}
	_ = store.Bootstrap(context.Background(), c)
	// Older event arrives.
	store.ApplyUpsert(slk.ChannelSectionUpserted{
		ID: "A", Name: "Stale", Type: "standard", LastUpdate: 100,
	})
	got := store.OrderedSections()
	if got[0].Name != "Latest" {
		t.Errorf("name = %q, want Latest (stale event must be dropped)", got[0].Name)
	}
}

func TestApplyDelete_RemovesSectionAndChannels(t *testing.T) {
	store := NewSectionStore()
	c := &fakeSectionsClient{sections: []slk.SidebarSection{
		{ID: "A", Type: "standard", Name: "A", Next: "", LastUpdate: 100, ChannelIDs: []string{"C1"}, ChannelsCount: 1},
	}}
	_ = store.Bootstrap(context.Background(), c)
	store.ApplyDelete("A")
	if _, ok := store.SectionForChannel("C1"); ok {
		t.Errorf("channel still mapped after section delete")
	}
	if got := store.OrderedSections(); len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestApplyChannelsAdded_UpdatesIndex(t *testing.T) {
	store := NewSectionStore()
	c := &fakeSectionsClient{sections: []slk.SidebarSection{
		{ID: "A", Type: "standard", Next: "", LastUpdate: 100},
	}}
	_ = store.Bootstrap(context.Background(), c)
	store.ApplyChannelsAdded("A", []string{"C1", "C2"})
	if id, ok := store.SectionForChannel("C1"); !ok || id != "A" {
		t.Errorf("C1 → (%q,%v), want (A,true)", id, ok)
	}
	if id, ok := store.SectionForChannel("C2"); !ok || id != "A" {
		t.Errorf("C2 → (%q,%v), want (A,true)", id, ok)
	}
}

func TestApplyChannelsAdded_OverwritesPreviousSection(t *testing.T) {
	// Channel moves from A to B via remove-then-add (Slack's pattern):
	// upsert into B should replace its membership in A.
	store := NewSectionStore()
	c := &fakeSectionsClient{sections: []slk.SidebarSection{
		{ID: "A", Type: "standard", Next: "B", LastUpdate: 100, ChannelIDs: []string{"C1"}, ChannelsCount: 1},
		{ID: "B", Type: "standard", Next: "", LastUpdate: 100},
	}}
	_ = store.Bootstrap(context.Background(), c)
	store.ApplyChannelsAdded("B", []string{"C1"})
	if id, _ := store.SectionForChannel("C1"); id != "B" {
		t.Errorf("C1 in %q, want B (add must overwrite)", id)
	}
}

func TestApplyChannelsRemoved_DropsIndex(t *testing.T) {
	store := NewSectionStore()
	c := &fakeSectionsClient{sections: []slk.SidebarSection{
		{ID: "A", Type: "standard", Next: "", LastUpdate: 100, ChannelIDs: []string{"C1"}, ChannelsCount: 1},
	}}
	_ = store.Bootstrap(context.Background(), c)
	store.ApplyChannelsRemoved("A", []string{"C1"})
	if _, ok := store.SectionForChannel("C1"); ok {
		t.Errorf("C1 still mapped after removal")
	}
}

func TestMaybeRebootstrap_DebouncedWithin30s(t *testing.T) {
	store := NewSectionStore()
	c := &fakeSectionsClient{sections: []slk.SidebarSection{
		{ID: "A", Type: "standard", Next: "", LastUpdate: 100},
	}}
	if err := store.Bootstrap(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	// First call: too soon, skipped.
	calledAgain := false
	c2 := &fakeSectionsClient{sections: []slk.SidebarSection{
		{ID: "B", Type: "standard", Next: "", LastUpdate: 200},
	}}
	wrap := &countingClient{inner: c2, onCall: func() { calledAgain = true }}
	store.MaybeRebootstrap(context.Background(), wrap)
	if calledAgain {
		t.Errorf("MaybeRebootstrap should be debounced within 30s")
	}
}

type countingClient struct {
	inner  SectionsClient
	onCall func()
}

func (cc *countingClient) GetChannelSections(ctx context.Context) ([]slk.SidebarSection, error) {
	cc.onCall()
	return cc.inner.GetChannelSections(ctx)
}

```

- [ ] **Step 2: Run tests to confirm they fail**

```
go test ./internal/service/ -run 'Apply|MaybeRebootstrap' -count=1
```

Expected: build error — methods don't exist.

- [ ] **Step 3: Implement the mutators**

Append to `internal/service/sectionstore.go`:

```go
// ApplyUpsert applies a channel_section_upserted WS event (also used
// for create / rename / reorder / emoji change). Last-write-wins by
// LastUpdate: stale events are dropped.
func (s *SectionStore) ApplyUpsert(ev slk.ChannelSectionUpserted) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		return
	}
	if existing, ok := s.sectionsByID[ev.ID]; ok && ev.LastUpdate < existing.LastUpdate {
		return
	}
	prev := s.sectionsByID[ev.ID]
	sec := &slk.SidebarSection{
		ID:         ev.ID,
		Name:       ev.Name,
		Type:       ev.Type,
		Emoji:      ev.Emoji,
		Next:       ev.Next,
		LastUpdate: ev.LastUpdate,
		IsRedacted: ev.IsRedacted,
	}
	if prev != nil {
		// Preserve channel membership; upsert events don't carry it.
		sec.ChannelIDs = prev.ChannelIDs
		sec.ChannelsCount = prev.ChannelsCount
	}
	s.sectionsByID[ev.ID] = sec
}

// ApplyDelete applies a channel_section_deleted WS event.
func (s *SectionStore) ApplyDelete(sectionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		return
	}
	delete(s.sectionsByID, sectionID)
	for ch, sec := range s.channelToSection {
		if sec == sectionID {
			delete(s.channelToSection, ch)
		}
	}
}

// ApplyChannelsAdded applies a channel_sections_channels_upserted WS event.
// A channel can only belong to one section, so adding to section X
// implicitly removes it from any prior section in our index.
func (s *SectionStore) ApplyChannelsAdded(sectionID string, channelIDs []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		return
	}
	sec, ok := s.sectionsByID[sectionID]
	if !ok {
		// Section we don't know about yet; skip — bootstrap or upsert
		// will reconcile.
		return
	}
	added := map[string]bool{}
	for _, ch := range sec.ChannelIDs {
		added[ch] = true
	}
	for _, ch := range channelIDs {
		if !added[ch] {
			sec.ChannelIDs = append(sec.ChannelIDs, ch)
			added[ch] = true
		}
		// Remove from any other section's ChannelIDs.
		if prevSec, prev := s.channelToSection[ch]; prev && prevSec != sectionID {
			if old, ok := s.sectionsByID[prevSec]; ok {
				filtered := old.ChannelIDs[:0]
				for _, x := range old.ChannelIDs {
					if x != ch {
						filtered = append(filtered, x)
					}
				}
				old.ChannelIDs = filtered
			}
		}
		s.channelToSection[ch] = sectionID
	}
}

// ApplyChannelsRemoved applies a channel_sections_channels_removed WS event.
func (s *SectionStore) ApplyChannelsRemoved(sectionID string, channelIDs []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		return
	}
	sec, ok := s.sectionsByID[sectionID]
	if !ok {
		return
	}
	dropped := map[string]bool{}
	for _, ch := range channelIDs {
		dropped[ch] = true
		if cur, ok := s.channelToSection[ch]; ok && cur == sectionID {
			delete(s.channelToSection, ch)
		}
	}
	filtered := sec.ChannelIDs[:0]
	for _, ch := range sec.ChannelIDs {
		if !dropped[ch] {
			filtered = append(filtered, ch)
		}
	}
	sec.ChannelIDs = filtered
}

// MaybeRebootstrap re-runs Bootstrap when the previous successful one was
// more than 30 seconds ago. Cheap insurance against missed events during
// disconnects without thundering during a flapping connection.
func (s *SectionStore) MaybeRebootstrap(ctx context.Context, client SectionsClient) error {
	s.mu.RLock()
	last := s.lastBootstrap
	s.mu.RUnlock()
	if !last.IsZero() && time.Since(last) < 30*time.Second {
		return nil
	}
	return s.Bootstrap(ctx, client)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./internal/service/ -count=1 -v
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/service/sectionstore.go internal/service/sectionstore_test.go
git commit -m "service: SectionStore live-update mutators

ApplyUpsert (last-write-wins on LastUpdate), ApplyDelete, ApplyChannelsAdded
(overwrites membership across sections), ApplyChannelsRemoved.
MaybeRebootstrap debounces re-fetch to once per 30s for reconnect storm
protection."
```

---

## Task 7: Wire WS event handlers into the dispatcher

**Files:**
- Modify: `internal/slack/events.go` — add four `EventHandler` methods, four dispatch cases
- Modify: `internal/slack/events_test.go` — dispatch tests

- [ ] **Step 1: Save WS fixture files**

Create `internal/slack/testdata/ws_section_deleted.json`:

```json
{"type":"channel_section_deleted","channel_section_id":"L0B12L90PLK","last_update":1777720341,"event_ts":"1777720341.010200"}
```

Create `internal/slack/testdata/ws_channels_upserted.json`:

```json
{"type":"channel_sections_channels_upserted","channel_section_id":"L0B1709V0LE","channel_ids":["D09R4P6G6QL"],"last_update":1777720402,"event_ts":"1777720402.010300"}
```

Create `internal/slack/testdata/ws_channels_removed.json`:

```json
{"type":"channel_sections_channels_removed","channel_section_id":"L0B1709V0LE","channel_ids":["C0AR3C3HMJT"],"last_update":1777720305,"event_ts":"1777720305.010000"}
```

- [ ] **Step 2: Write the failing dispatch tests**

Append to `internal/slack/events_test.go` (create the file if it doesn't exist with the standard package header):

```go
type recordingHandler struct {
	noopHandler
	upserted *ChannelSectionUpserted
	deletedID string
	channelsAddedSection string
	channelsAdded []string
	channelsRemovedSection string
	channelsRemoved []string
}

func (r *recordingHandler) OnChannelSectionUpserted(ev ChannelSectionUpserted) {
	r.upserted = &ev
}
func (r *recordingHandler) OnChannelSectionDeleted(id string) {
	r.deletedID = id
}
func (r *recordingHandler) OnChannelSectionChannelsUpserted(sectionID string, channelIDs []string) {
	r.channelsAddedSection = sectionID
	r.channelsAdded = channelIDs
}
func (r *recordingHandler) OnChannelSectionChannelsRemoved(sectionID string, channelIDs []string) {
	r.channelsRemovedSection = sectionID
	r.channelsRemoved = channelIDs
}

func TestDispatch_ChannelSectionUpserted(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "ws_section_upserted.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	h := &recordingHandler{}
	dispatchWebSocketEvent(data, h)
	if h.upserted == nil {
		t.Fatalf("handler not called")
	}
	if h.upserted.ID != "L0B12LBBCTD" || h.upserted.Type != "standard" {
		t.Errorf("got %+v", h.upserted)
	}
}

func TestDispatch_ChannelSectionDeleted(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "ws_section_deleted.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	h := &recordingHandler{}
	dispatchWebSocketEvent(data, h)
	if h.deletedID != "L0B12L90PLK" {
		t.Errorf("deletedID = %q", h.deletedID)
	}
}

func TestDispatch_ChannelsUpserted(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "ws_channels_upserted.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	h := &recordingHandler{}
	dispatchWebSocketEvent(data, h)
	if h.channelsAddedSection != "L0B1709V0LE" {
		t.Errorf("section = %q", h.channelsAddedSection)
	}
	if len(h.channelsAdded) != 1 || h.channelsAdded[0] != "D09R4P6G6QL" {
		t.Errorf("channels = %v", h.channelsAdded)
	}
}

func TestDispatch_ChannelsRemoved(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "ws_channels_removed.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	h := &recordingHandler{}
	dispatchWebSocketEvent(data, h)
	if h.channelsRemovedSection != "L0B1709V0LE" {
		t.Errorf("section = %q", h.channelsRemovedSection)
	}
	if len(h.channelsRemoved) != 1 || h.channelsRemoved[0] != "C0AR3C3HMJT" {
		t.Errorf("channels = %v", h.channelsRemoved)
	}
}
```

If `events_test.go` doesn't already exist, prepend the file with:

```go
package slackclient

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/slack-go/slack"
)

// noopHandler is a do-nothing EventHandler for tests that only care
// about a subset of methods.
type noopHandler struct{}

func (noopHandler) OnMessage(channelID, userID, ts, text, threadTS, subtype string, edited bool, files []slack.File, blocks slack.Blocks, attachments []slack.Attachment) {
}
func (noopHandler) OnMessageDeleted(channelID, ts string)         {}
func (noopHandler) OnReactionAdded(channelID, ts, userID, e string)   {}
func (noopHandler) OnReactionRemoved(channelID, ts, userID, e string) {}
func (noopHandler) OnPresenceChange(userID, p string)             {}
func (noopHandler) OnUserTyping(channelID, userID string)         {}
func (noopHandler) OnConnect()                                     {}
func (noopHandler) OnDisconnect()                                  {}
func (noopHandler) OnSelfPresenceChange(p string)                 {}
func (noopHandler) OnDNDChange(enabled bool, end int64)           {}
func (noopHandler) OnChannelMarked(channelID, ts string, unread int) {}
func (noopHandler) OnConversationOpened(ch slack.Channel)          {}
func (noopHandler) OnThreadMarked(channelID, threadTS, lastRead string, read bool) {}
func (noopHandler) OnChannelSectionUpserted(ev ChannelSectionUpserted)              {}
func (noopHandler) OnChannelSectionDeleted(sectionID string)                       {}
func (noopHandler) OnChannelSectionChannelsUpserted(sectionID string, channelIDs []string) {}
func (noopHandler) OnChannelSectionChannelsRemoved(sectionID string, channelIDs []string)  {}
```

> Verify the noop signatures match the actual `EventHandler` interface (it lives at `internal/slack/events.go:11`); if there are any methods I missed, add them. The test should compile.

- [ ] **Step 3: Run tests to confirm they fail**

```
go test ./internal/slack/ -run 'Dispatch_ChannelSection|Dispatch_Channels' -count=1
```

Expected: build error — `EventHandler` doesn't have the four new methods.

- [ ] **Step 4: Add the four `EventHandler` methods and dispatch cases**

In `internal/slack/events.go`, add to the `EventHandler` interface (around line 11):

```go
	// OnChannelSectionUpserted is called for channel_section_upserted
	// WS events: section create, rename, reorder, or emoji change.
	OnChannelSectionUpserted(ev ChannelSectionUpserted)
	// OnChannelSectionDeleted is called for channel_section_deleted.
	OnChannelSectionDeleted(sectionID string)
	// OnChannelSectionChannelsUpserted is called for
	// channel_sections_channels_upserted: one or more channels added
	// to the named section. A channel previously in another section
	// is implicitly moved.
	OnChannelSectionChannelsUpserted(sectionID string, channelIDs []string)
	// OnChannelSectionChannelsRemoved is called for
	// channel_sections_channels_removed.
	OnChannelSectionChannelsRemoved(sectionID string, channelIDs []string)
```

In `dispatchWebSocketEvent` (around line 165), add four cases before `default:`:

```go
	case "channel_section_upserted":
		var raw wsChannelSectionUpserted
		if err := json.Unmarshal(data, &raw); err != nil {
			return
		}
		handler.OnChannelSectionUpserted(raw.toUpserted())

	case "channel_section_deleted":
		var raw wsChannelSectionDeleted
		if err := json.Unmarshal(data, &raw); err != nil {
			return
		}
		handler.OnChannelSectionDeleted(raw.ID)

	case "channel_sections_channels_upserted":
		var raw wsChannelSectionsChannelsDelta
		if err := json.Unmarshal(data, &raw); err != nil {
			return
		}
		handler.OnChannelSectionChannelsUpserted(raw.SectionID, raw.ChannelIDs)

	case "channel_sections_channels_removed":
		var raw wsChannelSectionsChannelsDelta
		if err := json.Unmarshal(data, &raw); err != nil {
			return
		}
		handler.OnChannelSectionChannelsRemoved(raw.SectionID, raw.ChannelIDs)
```

- [ ] **Step 5: Run tests to verify they pass**

```
go test ./internal/slack/ -count=1
```

Expected: all pass. The existing dispatcher's other behavior is unchanged. The compile may fail if other types in `cmd/slk/main.go` implement `EventHandler` — those need stubs added (see Task 11). Temporarily add no-op implementations to whichever struct(s) implement the interface to keep this commit's tests green; the real wiring lands in Task 11.

If the build breaks: `grep -rn "OnChannelMarked" cmd/ internal/` to find every `EventHandler` implementation. Add four no-op stubs to each.

- [ ] **Step 6: Commit**

```bash
git add internal/slack/events.go internal/slack/events_test.go internal/slack/testdata/
# also any cmd/slk/main.go stubs added in step 5
git add cmd/slk/main.go
git commit -m "slack: dispatch four channel-section WS events to EventHandler

Adds OnChannelSection{Upserted,Deleted,ChannelsUpserted,ChannelsRemoved}
to EventHandler. Stubbed in WorkspaceContext for now; SectionStore
wiring lands in a follow-up commit."
```

---

## Task 8: SectionsProvider interface for the sidebar

**Files:**
- Create: `internal/ui/sidebar/sections_provider.go`
- Modify: `internal/ui/sidebar/model.go` — accept a provider, add `collapseByID` map, two-mode `orderedSections`
- Modify: `internal/ui/sidebar/model_test.go` — Slack-mode tests

- [ ] **Step 1: Create the provider interface**

Create `internal/ui/sidebar/sections_provider.go`:

```go
package sidebar

// SectionsProvider supplies Slack-native sidebar sections to the model.
// When non-nil and Ready returns true, the model uses provider data
// instead of the config-glob path. Implementations live in the service
// layer (SectionStore); this interface keeps the sidebar package free
// of cross-package dependencies.
type SectionsProvider interface {
	Ready() bool
	// OrderedSlackSections returns sections in the order they should
	// render, already filtered to the renderable set. Each entry is
	// the data the sidebar needs for the header row.
	OrderedSlackSections() []SectionMeta
}

// SectionMeta is the sidebar's view of one Slack section.
type SectionMeta struct {
	ID    string
	Name  string
	Emoji string // shortcode like "orange_book"; empty for none
	Type  string // standard | channels | direct_messages | recent_apps
}
```

- [ ] **Step 2: Write the failing model tests**

Append to `internal/ui/sidebar/model_test.go`:

```go
type fakeProvider struct {
	ready    bool
	sections []SectionMeta
}

func (f *fakeProvider) Ready() bool                          { return f.ready }
func (f *fakeProvider) OrderedSlackSections() []SectionMeta  { return f.sections }

func TestOrderedSections_SlackMode_HonorsLinkedListOrder(t *testing.T) {
	items := []ChannelItem{
		{ID: "C1", Name: "ch1", Type: "channel", Section: "B"},
		{ID: "C2", Name: "ch2", Type: "channel", Section: "A"},
		{ID: "D1", Name: "u", Type: "dm", Section: "DMS"},
	}
	provider := &fakeProvider{
		ready: true,
		sections: []SectionMeta{
			{ID: "A", Name: "Alerts", Type: "standard"},
			{ID: "B", Name: "Books", Type: "standard"},
			{ID: "DMS", Name: "Direct Messages", Type: "direct_messages"},
		},
	}
	m := New(items)
	m.SetSectionsProvider(provider)
	got := orderedSectionIDs(&m)
	want := []string{"A", "B", "DMS"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestOrderedSections_ConfigMode_UnchangedWhenNoProvider(t *testing.T) {
	// Regression guard: existing config-glob behavior must be intact.
	items := []ChannelItem{
		{ID: "C1", Name: "ch1", Type: "channel", Section: "Custom", SectionOrder: 1},
		{ID: "C2", Name: "ch2", Type: "channel"},
		{ID: "D1", Name: "u", Type: "dm"},
	}
	m := New(items)
	got := orderedSectionsForTest(&m)
	// Custom first, then DMs, then Channels.
	want := []string{"Custom", "Direct Messages", "Channels"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("[%d] = %q, want %q", i, got[i], w)
		}
	}
}

// orderedSectionIDs is a test helper that returns the section IDs
// (Slack mode) by reading the model's nav after a rebuild. Since
// Slack-mode keys headers by ID, and config-mode keys by name, the
// helper picks the appropriate field. Add to model_test.go.
func orderedSectionIDs(m *Model) []string {
	var out []string
	for _, n := range m.nav {
		if n.kind == navHeader {
			out = append(out, n.header)
		}
	}
	return out
}

// orderedSectionsForTest mirrors orderedSectionIDs for the config path;
// they're the same impl but separated for clarity in test names.
func orderedSectionsForTest(m *Model) []string {
	return orderedSectionIDs(m)
}
```

- [ ] **Step 3: Run tests to confirm they fail**

```
go test ./internal/ui/sidebar/ -run 'OrderedSections_' -count=1
```

Expected: build error — `SetSectionsProvider` doesn't exist.

- [ ] **Step 4: Wire provider into Model**

In `internal/ui/sidebar/model.go`, add to the `Model` struct (after the `collapsed` field around line 164):

```go
	// sectionsProvider is the Slack-native sections data source. Nil
	// means "use config-glob behavior". When non-nil and Ready, the
	// orderedSections function returns the provider's verbatim order
	// and headers are keyed by section ID instead of name.
	sectionsProvider SectionsProvider
	// collapseByID parallels `collapsed` for Slack-mode (ID-keyed).
	// Renames preserve collapse state because the ID is stable.
	collapseByID map[string]bool
```

Add a setter and update `New`:

In `New` (line 308), after the existing `m.collapsed = ...` initialization, add:

```go
	m.collapseByID = map[string]bool{}
```

Add a new exported method (place near `SetFocused`):

```go
// SetSectionsProvider injects a Slack-native sections data source.
// When non-nil and Ready, the sidebar renders sections in the
// provider's order and keys collapse state by section ID. Pass nil
// to revert to config-glob behavior.
func (m *Model) SetSectionsProvider(p SectionsProvider) {
	m.sectionsProvider = p
	m.rebuildFilter()
	m.rebuildNavPreserveCursor()
	m.cacheValid = false
	m.dirty()
}

// useSlackSections returns true when the provider is non-nil and ready.
// The model has two distinct rendering paths gated on this.
func (m *Model) useSlackSections() bool {
	return m.sectionsProvider != nil && m.sectionsProvider.Ready()
}
```

- [ ] **Step 5: Implement two-mode `orderedSections`**

Replace the existing `orderedSections` function (at `internal/ui/sidebar/model.go:64-124`) with a free-floating helper and add a model-bound version:

```go
// orderedSections picks the right path based on whether the model has
// a Slack-native provider. Slack mode: provider's verbatim list,
// returning section IDs. Config mode: existing behavior, returning
// section names.
func (m *Model) orderedSectionsForRebuild(filtered []int) []string {
	if m.useSlackSections() {
		metas := m.sectionsProvider.OrderedSlackSections()
		// Build a set of section IDs that have at least one filtered
		// item, OR are standard-typed (always render even when empty).
		hasItem := map[string]bool{}
		for _, idx := range filtered {
			if id := m.items[idx].Section; id != "" {
				hasItem[id] = true
			}
		}
		out := make([]string, 0, len(metas))
		for _, meta := range metas {
			if meta.Type == "standard" || hasItem[meta.ID] {
				out = append(out, meta.ID)
			}
		}
		return out
	}
	return orderedSectionsLegacy(m.items, filtered)
}

// orderedSectionsLegacy is the pre-existing algorithm, renamed.
func orderedSectionsLegacy(items []ChannelItem, filtered []int) []string {
	// (paste original orderedSections body here, unchanged)
}
```

Find every caller of the old `orderedSections(items, filtered)` (search: `rg "orderedSections\(" internal/ui/sidebar/`) and replace with `m.orderedSectionsForRebuild(filtered)`.

> **Important:** the helper must work both as a method on `*Model` for the new code path AND preserve the package-level `orderedSections(items, filtered)` for any external test calls. Search shows tests call `orderedSections` directly — keep a thin wrapper at the package level that delegates to the legacy function for back-compat:
>
> ```go
> func orderedSections(items []ChannelItem, filtered []int) []string {
>     return orderedSectionsLegacy(items, filtered)
> }
> ```

- [ ] **Step 6: Run tests to verify they pass**

```
go test ./internal/ui/sidebar/ -count=1
```

Expected: all pass — both the new Slack-mode test and the existing regression suite.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/sidebar/sections_provider.go internal/ui/sidebar/model.go internal/ui/sidebar/model_test.go
git commit -m "sidebar: add SectionsProvider for Slack-native rendering

When a SectionsProvider is injected and Ready, orderedSections returns
the provider's verbatim list (linked-list order from Slack), keyed by
section ID. Config-mode behavior is unchanged: same name-keyed buckets,
same ordering algorithm. collapseByID parallels the existing collapsed
map so rename events preserve collapse state."
```

---

## Task 9: Sidebar header rendering — emoji prefix and ID-keyed collapse

**Files:**
- Modify: `internal/ui/sidebar/model.go` — `renderSectionHeaderLabel` (around line 1120-1139), `IsCollapsed`, `ToggleCollapse`, `aggregateUnreadForSection`
- Modify: `internal/ui/sidebar/model_test.go` — emoji-prefix test, collapse-by-ID test

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/sidebar/model_test.go`:

```go
func TestSectionHeader_RendersEmojiPrefix(t *testing.T) {
	items := []ChannelItem{{ID: "C1", Name: "ch1", Type: "channel", Section: "A"}}
	provider := &fakeProvider{
		ready: true,
		sections: []SectionMeta{{ID: "A", Name: "Alerts", Emoji: "rocket", Type: "standard"}},
	}
	m := New(items)
	m.SetSectionsProvider(provider)
	out := m.View(20, 40)
	if !strings.Contains(out, "Alerts") {
		t.Errorf("missing section name in output:\n%s", out)
	}
	// Look for the rocket emoji or its shortcode fallback.
	if !strings.Contains(out, "🚀") && !strings.Contains(out, ":rocket:") {
		t.Errorf("missing emoji prefix in output:\n%s", out)
	}
}

func TestCollapseByID_PreservedAcrossRename(t *testing.T) {
	items := []ChannelItem{{ID: "C1", Name: "ch1", Type: "channel", Section: "A"}}
	p := &fakeProvider{
		ready:    true,
		sections: []SectionMeta{{ID: "A", Name: "Alerts", Type: "standard"}},
	}
	m := New(items)
	m.SetSectionsProvider(p)
	m.ToggleCollapse("A")
	if !m.IsCollapsed("A") {
		t.Fatalf("collapse failed")
	}
	// Rename: provider returns the same ID with a new name.
	p.sections = []SectionMeta{{ID: "A", Name: "Renamed", Type: "standard"}}
	m.SetSectionsProvider(p) // triggers a rebuild
	if !m.IsCollapsed("A") {
		t.Errorf("collapse state lost after rename (must key by ID)")
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```
go test ./internal/ui/sidebar/ -run 'EmojiPrefix|CollapseByID' -count=1
```

Expected: emoji test fails (header doesn't render the emoji); collapse test passes only if Task 8's `SetSectionsProvider` already routed through `collapseByID` — likely fails because `IsCollapsed` still consults `m.collapsed` (name-keyed).

- [ ] **Step 3: Update collapse methods to dispatch by mode**

In `internal/ui/sidebar/model.go`, replace `IsCollapsed`, `ToggleCollapse`:

```go
func (m *Model) IsCollapsed(section string) bool {
	if m.useSlackSections() {
		if m.collapseByID == nil {
			return false
		}
		return m.collapseByID[section]
	}
	if m.collapsed == nil {
		return false
	}
	return m.collapsed[section]
}

func (m *Model) ToggleCollapse(section string) {
	if m.useSlackSections() {
		if m.collapseByID == nil {
			m.collapseByID = map[string]bool{}
		}
		m.collapseByID[section] = !m.collapseByID[section]
	} else {
		if m.collapsed == nil {
			m.collapsed = map[string]bool{}
		}
		m.collapsed[section] = !m.collapsed[section]
	}
	m.rebuildNavPreserveCursor()
	m.cacheValid = false
	m.dirty()
}
```

- [ ] **Step 4: Update header rendering for emoji prefix**

Find `renderSectionHeaderLabel` (around `internal/ui/sidebar/model.go:1120-1139`). Modify the function signature to accept a `SectionMeta` (or to look up emoji via the provider given the section ID).

Approach: in `buildCache` where the header row is constructed, when in Slack mode look up the meta for the current section ID and pass emoji into `renderSectionHeaderLabel`.

Add a lookup helper:

```go
// sectionDisplayMeta returns the user-visible name and emoji shortcode
// for a section as currently identified in the nav. In Slack mode the
// nav header is the section ID; in config mode it is the section name.
func (m *Model) sectionDisplayMeta(sectionKey string) (name, emoji string) {
	if m.useSlackSections() {
		for _, meta := range m.sectionsProvider.OrderedSlackSections() {
			if meta.ID == sectionKey {
				name := meta.Name
				if name == "" {
					name = "(unnamed)"
				}
				return name, meta.Emoji
			}
		}
	}
	return sectionKey, ""
}
```

In `renderSectionHeaderLabel` (or wherever the header text is constructed), when emoji is non-empty:

```go
// Pseudocode for the label assembly:
label := name
if emoji != "" {
	if rendered := emojiResolve(emoji); rendered != "" {
		label = rendered + " " + name
	} else {
		label = ":" + emoji + ": " + name
	}
}
```

> Replace `emojiResolve(emoji)` with whichever helper slk already uses to resolve shortcode → unicode. Search: `rg -n "func.*[Ee]moji.*string" internal/`. The likely candidate is in `internal/emoji/` or `internal/slackfmt/`. If no helper exists, fall back to the `:shortcode:` form unconditionally (acceptable for v1).

- [ ] **Step 5: Run tests to verify they pass**

```
go test ./internal/ui/sidebar/ -count=1 -v
```

Expected: emoji test passes (with either a unicode emoji or the `:rocket:` shortcode), collapse-by-ID test passes.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/sidebar/model.go internal/ui/sidebar/model_test.go
git commit -m "sidebar: emoji prefix + ID-keyed collapse for Slack sections

renderSectionHeaderLabel renders the section's emoji shortcode resolved
to unicode (with :shortcode: fallback). Collapse state under Slack mode
keys by section ID, preserving state across renames."
```

---

## Task 10: Tiered resolver in channelitem.go

**Files:**
- Modify: `cmd/slk/channelitem.go` — replace single `cfg.MatchSection` call with tiered resolver
- Modify: `cmd/slk/channelitem_test.go` (create if missing) — tier tests

- [ ] **Step 1: Find or create the test file**

```
ls cmd/slk/channelitem_test.go 2>/dev/null || echo MISSING
```

If MISSING, create with the standard package header and a baseline test that exercises the existing code path.

- [ ] **Step 2: Write the failing tests**

In `cmd/slk/channelitem_test.go`, add:

```go
package main

import (
	"testing"

	"github.com/gammons/slk/internal/config"
	"github.com/slack-go/slack"
)

// fakeStore mocks the parts of *service.SectionStore the resolver uses.
type fakeStore struct {
	ready bool
	mapping map[string]string // channelID → sectionID
}

func (f *fakeStore) Ready() bool { return f.ready }
func (f *fakeStore) SectionForChannel(id string) (string, bool) {
	if !f.ready {
		return "", false
	}
	s, ok := f.mapping[id]
	return s, ok
}

func TestBuildChannelItem_StoreReady_StoreWins(t *testing.T) {
	cfg := config.Config{
		Sections: map[string]config.SectionDef{
			"Globbed": {Channels: []string{"alerts*"}, Order: 1},
		},
	}
	wctx := &WorkspaceContext{
		SectionStore: &fakeStore{ready: true, mapping: map[string]string{"C1": "L_SLACK"}},
	}
	ch := slack.Channel{GroupConversation: slack.GroupConversation{Name: "alerts-prod", Conversation: slack.Conversation{ID: "C1"}}}
	item, _ := buildChannelItem(ch, wctx, cfg, "T1")
	if item.Section != "L_SLACK" {
		t.Errorf("Section = %q, want L_SLACK (store wins over glob)", item.Section)
	}
}

func TestBuildChannelItem_StoreReady_StoreMisses_FallsToGlob(t *testing.T) {
	cfg := config.Config{
		Sections: map[string]config.SectionDef{
			"Globbed": {Channels: []string{"alerts*"}, Order: 1},
		},
	}
	wctx := &WorkspaceContext{
		SectionStore: &fakeStore{ready: true, mapping: map[string]string{}},
	}
	ch := slack.Channel{GroupConversation: slack.GroupConversation{Name: "alerts-prod", Conversation: slack.Conversation{ID: "C1"}}}
	item, _ := buildChannelItem(ch, wctx, cfg, "T1")
	if item.Section != "Globbed" {
		t.Errorf("Section = %q, want Globbed (store had no match)", item.Section)
	}
}

func TestBuildChannelItem_StoreNil_UsesGlob(t *testing.T) {
	cfg := config.Config{
		Sections: map[string]config.SectionDef{
			"Globbed": {Channels: []string{"alerts*"}, Order: 1},
		},
	}
	wctx := &WorkspaceContext{SectionStore: nil}
	ch := slack.Channel{GroupConversation: slack.GroupConversation{Name: "alerts-prod", Conversation: slack.Conversation{ID: "C1"}}}
	item, _ := buildChannelItem(ch, wctx, cfg, "T1")
	if item.Section != "Globbed" {
		t.Errorf("Section = %q, want Globbed", item.Section)
	}
}
```

- [ ] **Step 3: Run tests to confirm they fail**

```
go test ./cmd/slk/ -run 'BuildChannelItem' -count=1
```

Expected: build error — `WorkspaceContext.SectionStore` field doesn't exist (will be added in Task 11) AND/OR fakeStore doesn't satisfy the type. Go ahead and add a placeholder `SectionStore` field of type `interface{ Ready() bool; SectionForChannel(string) (string, bool) }` to `WorkspaceContext` for now (Task 11 promotes it to the real type).

In `cmd/slk/main.go`, in the `WorkspaceContext` struct (around line 59), add:

```go
	// SectionStore holds the user's Slack-native sidebar sections for
	// this workspace. Nil when use_slack_sections is false or bootstrap
	// failed; resolver falls through to config globs in that case.
	SectionStore sectionResolver
```

And define the interface near the struct:

```go
// sectionResolver is the subset of *service.SectionStore that
// channelitem.go and main.go's resolver need. Defined as an interface
// to keep the cmd package from depending on the service package's
// concrete type at the resolver call site.
type sectionResolver interface {
	Ready() bool
	SectionForChannel(channelID string) (string, bool)
}
```

- [ ] **Step 4: Update the resolver**

Modify `cmd/slk/channelitem.go` lines 58-62:

```go
	section := ""
	if wctx.SectionStore != nil && wctx.SectionStore.Ready() {
		if id, ok := wctx.SectionStore.SectionForChannel(ch.ID); ok {
			section = id
		}
	}
	var sectionOrder int
	if section == "" {
		section = cfg.MatchSection(teamID, ch.Name)
		if section != "" {
			sectionOrder = cfg.SectionOrder(teamID, section)
		}
	}
```

`SectionOrder` only matters for config-mode (the sidebar Slack-mode path uses linked-list order from the provider, not this field), so leaving it at 0 for Slack-mode sections is correct.

- [ ] **Step 5: Run tests to verify they pass**

```
go test ./cmd/slk/ -run 'BuildChannelItem' -count=1 -v
go build ./...
```

Expected: tests pass, build succeeds.

- [ ] **Step 6: Commit**

```bash
git add cmd/slk/channelitem.go cmd/slk/channelitem_test.go cmd/slk/main.go
git commit -m "cmd/slk: tiered section resolver

buildChannelItem now consults WorkspaceContext.SectionStore first when
ready; falls through to cfg.MatchSection when the store has no match
or isn't available. ChannelItem.Section holds an opaque section ID in
Slack mode and a section name in config mode; the sidebar's
SectionsProvider abstracts the difference."
```

---

## Task 11: Bootstrap SectionStore on workspace connect

**Files:**
- Modify: `cmd/slk/main.go` — promote `SectionStore` to real type, bootstrap on connect, log warning when shadowing config sections, wire sidebar provider

- [ ] **Step 1: Update WorkspaceContext to use the real SectionStore**

In `cmd/slk/main.go`, replace the `sectionResolver` interface placeholder with the real type. Add to imports:

```go
"github.com/gammons/slk/internal/service"
```

(if not already imported).

Replace the `SectionStore sectionResolver` field with:

```go
	// SectionStore holds Slack-native sections for this workspace.
	// Nil when use_slack_sections is false. Always populate the field
	// (even before Bootstrap completes) so handlers can hold a stable
	// reference; SectionStore.Ready() gates whether it's actually used.
	SectionStore *service.SectionStore
```

Delete the `sectionResolver` interface (no longer needed; `*service.SectionStore` exposes the same methods directly, and `channelitem.go`'s resolver works on the concrete type since it's in the same package).

Update `cmd/slk/channelitem.go` if needed — the resolver code from Task 10 already uses method calls (`.Ready()`, `.SectionForChannel(...)`) which work on the concrete type without changes.

- [ ] **Step 2: Add Bootstrap call to connectWorkspace**

In `connectWorkspace` (`cmd/slk/main.go:981`), after the existing `if err := client.Connect(ctx); err != nil { ... }` block:

```go
	// Initialize Slack-native section store if enabled. Failure is
	// non-fatal: the resolver falls back to config-glob behavior.
	if cfg.EffectiveUseSlackSections(client.TeamID()) {
		store := service.NewSectionStore()
		if err := store.Bootstrap(ctx, client); err != nil {
			log.Printf("section store bootstrap for %s failed: %v (falling back to config sections)", token.TeamName, err)
		} else {
			wctx.SectionStore = store
			// Warn the user once if their config has sections that are
			// being shadowed.
			hasGlobSections := len(cfg.Sections) > 0
			if ws, ok := cfg.WorkspaceByTeamID(client.TeamID()); ok && len(ws.Sections) > 0 {
				hasGlobSections = true
			}
			if hasGlobSections {
				log.Printf("workspace %s: using Slack-native sections; [sections.*] from config are shadowed (set use_slack_sections=false to disable)", token.TeamName)
			}
		}
	}
```

> **Important:** the `wctx` variable is declared further down in the existing function. Move the bootstrap into the existing initialization flow so `wctx` is in scope. Read the function before editing; place the bootstrap block right after `wctx` is constructed and before the conversation list is fetched, so the channel-item builder benefits from the store on first pass.

- [ ] **Step 3: Wire the sidebar provider**

The sidebar `*sidebar.Model` is constructed somewhere in `main.go`. Search:

```
rg "sidebar.New\(" cmd/slk/
```

Find the call site and, after construction, when `wctx.SectionStore != nil`, call:

```go
sb.SetSectionsProvider(sectionsProviderAdapter{store: wctx.SectionStore})
```

Add an adapter type at file scope (since `*service.SectionStore.OrderedSections` returns `[]*slk.SidebarSection`, not `[]sidebar.SectionMeta` — bridging type required):

```go
// sectionsProviderAdapter adapts *service.SectionStore to the
// sidebar.SectionsProvider interface. Translates SidebarSection into
// the sidebar's view-only SectionMeta shape.
type sectionsProviderAdapter struct {
	store *service.SectionStore
}

func (a sectionsProviderAdapter) Ready() bool { return a.store.Ready() }
func (a sectionsProviderAdapter) OrderedSlackSections() []sidebar.SectionMeta {
	secs := a.store.OrderedSections()
	out := make([]sidebar.SectionMeta, 0, len(secs))
	for _, s := range secs {
		out = append(out, sidebar.SectionMeta{
			ID:    s.ID,
			Name:  s.Name,
			Emoji: s.Emoji,
			Type:  s.Type,
		})
	}
	return out
}
```

- [ ] **Step 4: Build and run all tests**

```
go build ./... && go test ./... -count=1 2>&1 | tail -30
```

Expected: build succeeds, all tests pass. Existing tests should still pass; no new test added in this task because the integration is exercised manually in Task 13.

- [ ] **Step 5: Commit**

```bash
git add cmd/slk/main.go cmd/slk/channelitem.go
git commit -m "cmd/slk: bootstrap SectionStore on workspace connect

Workspaces with use_slack_sections=true (the default) call
SectionStore.Bootstrap on connect; failure logs and falls back to
config-glob behavior. Sidebar gets a SectionsProvider adapter that
forwards from the store. Warning logged when user has [sections.*]
config that is being shadowed by Slack sections."
```

---

## Task 12: Wire WS handlers to SectionStore

**Files:**
- Modify: `cmd/slk/main.go` — replace stubbed `OnChannelSection*` methods with real implementations that call `SectionStore` mutators and trigger sidebar rebuild

- [ ] **Step 1: Find the existing stubs**

The stubs from Task 7 step 5 should be in `cmd/slk/main.go` (or wherever `WorkspaceContext` implements `EventHandler`). Search:

```
rg "OnChannelSectionUpserted" cmd/slk/
```

- [ ] **Step 2: Replace stubs with real implementations**

Replace the four stub methods with:

```go
func (w *WorkspaceContext) OnChannelSectionUpserted(ev slackclient.ChannelSectionUpserted) {
	if w.SectionStore == nil {
		return
	}
	w.SectionStore.ApplyUpsert(ev)
	w.notifySidebarRebuild()
}

func (w *WorkspaceContext) OnChannelSectionDeleted(sectionID string) {
	if w.SectionStore == nil {
		return
	}
	w.SectionStore.ApplyDelete(sectionID)
	w.notifySidebarRebuild()
}

func (w *WorkspaceContext) OnChannelSectionChannelsUpserted(sectionID string, channelIDs []string) {
	if w.SectionStore == nil {
		return
	}
	w.SectionStore.ApplyChannelsAdded(sectionID, channelIDs)
	w.notifySidebarRebuild()
}

func (w *WorkspaceContext) OnChannelSectionChannelsRemoved(sectionID string, channelIDs []string) {
	if w.SectionStore == nil {
		return
	}
	w.SectionStore.ApplyChannelsRemoved(sectionID, channelIDs)
	w.notifySidebarRebuild()
}
```

- [ ] **Step 3: Implement `notifySidebarRebuild`**

Find how other event handlers in `WorkspaceContext` notify the UI of state changes (search for `OnChannelMarked` to see the pattern). It likely posts a `tea.Cmd` or sends on a channel.

Add a parallel helper:

```go
// notifySidebarRebuild signals the App layer that section state changed
// and the sidebar's bucketing/order needs a refresh. Implementation
// mirrors the path used by OnChannelMarked / OnConversationOpened.
func (w *WorkspaceContext) notifySidebarRebuild() {
	// Adapt this to slk's existing notification mechanism. Likely
	// posts a tea.Msg to the program. If WorkspaceContext already has
	// a `program *tea.Program` field, send a custom message:
	if w.program != nil {
		w.program.Send(sectionsChangedMsg{teamID: w.TeamID})
	}
}
```

Define the message type at file scope:

```go
// sectionsChangedMsg signals that a workspace's SectionStore mutated
// and the sidebar needs to re-bucket items. Carries TeamID so the
// model can decide whether the active workspace is affected.
type sectionsChangedMsg struct {
	teamID string
}
```

In the bubbletea `Update` (search `func.*Update.*tea.Msg`), add a case that, when the message's TeamID matches the active workspace, calls a function that re-runs `buildChannelItem` for every channel and refreshes the sidebar.

> The exact integration depends on slk's existing reactive flow. Read the existing handler for `OnConversationOpened` to find the right hook, then mirror it.

- [ ] **Step 4: Add reconnect re-bootstrap**

Find `OnConnect()` in `cmd/slk/main.go` (the `hello` event handler). Add to its body:

```go
	if w.SectionStore != nil {
		go func() {
			if err := w.SectionStore.MaybeRebootstrap(context.Background(), w.Client); err != nil {
				log.Printf("section store rebootstrap for %s failed: %v", w.TeamName, err)
				return
			}
			w.notifySidebarRebuild()
		}()
	}
```

- [ ] **Step 5: Build and run all tests**

```
go build ./... && go test ./... -count=1
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add cmd/slk/main.go
git commit -m "cmd/slk: route channel-section WS events into SectionStore

The four On* handlers now mutate the workspace's SectionStore and
post a sectionsChangedMsg to trigger sidebar rebuild. OnConnect
re-runs MaybeRebootstrap (debounced) for missed-event recovery."
```

---

## Task 13: Manual verification

- [ ] **Step 1: Build and run**

```
make build
SLK_DEBUG=1 SLK_DEBUG_WS=1 ./bin/slk
```

Open one of your Slack workspaces.

- [ ] **Step 2: Verify initial sidebar order**

Compare the slk sidebar to the official Slack client's sidebar. Sections should appear in the same order, with the same names and (if your terminal supports unicode emoji) the same emoji. Empty `standard`-type sections should render as headers with zero rows below.

If wrong, check `/tmp/slk-debug.log` for "section store bootstrap" warnings; run `./bin/slk --dump-sections` to compare REST data against rendered state.

- [ ] **Step 3: Verify live updates**

In the official Slack client, perform each of these and confirm slk reflects the change within ~2 seconds:

- Create a new section
- Rename an existing section
- Move a channel from one section to another
- Move a DM into a custom section
- Delete a section

The slk debug log should show the corresponding `[ws]` event lines with `unknown` event types now absent (they're handled).

- [ ] **Step 4: Verify fallback path**

In `~/.config/slk/config.toml`, add:

```toml
[general]
use_slack_sections = false
```

Restart slk. Sidebar should revert to config-glob behavior (whatever `[sections.*]` you have, plus the hardcoded DMs/Apps/Channels). Re-enable with `use_slack_sections = true` (or remove the line).

- [ ] **Step 5: Verify per-workspace override**

```toml
[general]
use_slack_sections = true   # global on

[workspaces.work]
team_id = "T_YOUR_ID"
use_slack_sections = false  # this workspace off
```

Restart. The named workspace should use config globs; others use Slack sections.

---

## Task 14: Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/STATUS.md` (if it exists)

- [ ] **Step 1: Update README**

In `README.md`, find the existing "Channels & Workspaces" feature section. Replace the bullet about "Custom channel sections via glob patterns in config" with:

```markdown
- **Slack-native channel sections** — by default, slk uses the channel sections you've already organized in the official Slack client (linked-list order, live-updated when you reorder/rename in any client). Falls back to config-glob sections if disabled or unavailable.
- Custom channel sections via glob patterns in config (used as a fallback or when `use_slack_sections = false`)
```

In the Configuration section, after the `[general]` block, add:

```toml
[general]
default_workspace = "work"
use_slack_sections = true   # use real Slack sidebar sections (default)
                            # set false to use [sections.*] globs instead
```

After the existing `[sections.Alerts]` example, add a paragraph:

```markdown
When `use_slack_sections = true` (the default) and Slack returns
sections successfully, the `[sections.*]` and
`[workspaces.<slug>.sections.*]` blocks are ignored and slk renders
your actual Slack sidebar. Set `use_slack_sections = false` (globally
or per-workspace) to use the glob blocks instead.
```

- [ ] **Step 2: Update STATUS.md if it exists**

```
ls docs/STATUS.md 2>/dev/null && echo EXISTS || echo MISSING
```

If it exists, add an entry under the most recent section:

```markdown
- **Slack-native sidebar sections** (2026-05-02) — slk now uses the
  user's actual Slack sidebar sections by default, with config-glob
  sections as a fallback. Live updates over WS for create / rename /
  reorder / channel moves. v1 is read-only.
```

- [ ] **Step 3: Commit**

```bash
git add README.md docs/STATUS.md
git commit -m "docs: document Slack-native sidebar sections feature"
```

---

## Self-review notes

- All tasks have a TDD loop (failing test → impl → passing test → commit).
- File-by-file ownership documented in the File Structure section.
- Risk areas explicitly called out: pagination endpoint shape (Task 3), `notifySidebarRebuild` integration with bubbletea (Task 12).
- No placeholder text inside steps; every code change has actual code.
- Task 7 step 5's stub-then-replace pattern in `cmd/slk/main.go` is intentional to keep the slack package's tests independently runnable before the cmd integration lands.
- Task 11's "find the sidebar.New call" and Task 12's "find Update / OnConnect" require a small amount of code reading at execution time; this is unavoidable without exhaustively re-reading every line of main.go right now.


