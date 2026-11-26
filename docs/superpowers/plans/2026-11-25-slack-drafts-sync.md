# Slack Drafts Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** slk's composer text becomes Slack's own draft for that conversation (saved while typing, restored on open, marked sent on send), plus a `:drafts` list to browse, jump to and delete drafts.

**Architecture:** `internal/slack/drafts.go` speaks Slack's undocumented `drafts.*` web-client API, byte-matched to scrubbed captures. A per-workspace `draftEngine` in `cmd/slk` owns draft ids, create-vs-update, per-conversation serialization and conflicts, behind a new `core.DraftService` port. The UI (`internal/ui/drafts.go`) compares every composer slot with the text Slack last had every 2 s and saves what changed; drafts loaded from Slack are restored only where the user has typed nothing.

**Tech Stack:** Go, bubbletea v2, slack-go v0.23 (types only for drafts), stdlib `testing`.

**Spec:** `docs/superpowers/specs/2026-11-25-slack-drafts-sync-design.md` (captures in `docs/superpowers/specs/2026-11-25-slack-drafts-captures/`). Read both before starting.

## Global Constraints

- Read `AGENTS.md` first. `internal/ui` does no I/O: no `internal/slack`, `cache`, `net/http` imports there (`internal/ui/boundary_test.go` enforces it).
- Tests: stdlib `testing` only, white-box (`package ui`, `package slackclient`, `package main`, …). No testify.
- `App.Update` behaviour is added through a reducer in the chain (`app.go` `dispatchReducers`), never in `Update` itself.
- Every new mode gets an entry in `modeHandlers`, `closeModal`, `IsModalOverlay`, `String`, and `TestQuitPromptClosesEveryModal`'s cases.
- Every new `:` command gets a help entry in `DefaultKeyMap` (`TestHelpListsEveryCommand`).
- Wire forms must match the captures field for field. `_x_*` envelope fields come from `slackhttp.WithReason`, never the form.
- `client_last_updated_ts` is unix **milliseconds** of the last local edit. `drafts.delete`'s is unix milliseconds of now.
- `_x_reason`: `MessageInput:updateDraft` for create/update/delete, `client-v2-boot-team` for list.
- Toast texts, verbatim: `Replaced a newer draft from another device`, `Slack already has a draft here that slk won't overwrite`, `Draft deleted`, `Couldn't delete draft`, `This draft has no recipient`, `That conversation is unavailable`, `Draft sync is off ([compose] sync_drafts)`.
- Config: `[compose] sync_drafts`, default **true**.
- Before every commit: `gofmt -l .` prints nothing; the package you touched passes `go test`.
- New reusable helpers go into the `AGENTS.md` tables in the same commit (Task 9 does the final sweep).

## Review Focus

1. **A send racing a save.** The user types "hello", a save is in flight, they press Enter. Expect exactly one message posted, the draft marked sent, and no draft resurrected afterwards by the late save answer. (Task 4: `TestSendWithDraft_WaitsForInFlightSave`; Task 6: `TestDraftSync_SendIsNotADiscardOrAResave`.)
2. **Whitespace-only composer.** A slot holding `"   \n"` must count as empty: discard, never a save that `mrkdwn.Convert` turns into no block. (Task 6: `TestDraftSync_WhitespaceIsEmpty`.)
3. **Startup before the team is known.** The composer can be bound with an empty team ID before the workspace resolves. Expect no save for a slot whose team is "" or whose team's drafts have not been listed. (Task 6: `TestDraftSync_NothingBeforeFirstList`.)
4. **A permanent server error.** A save that keeps failing (e.g. `channel_not_found`) must not hammer Slack every 2 s. Expect backoff. (Task 6: `TestDraftSync_TransientErrorBacksOff`.)
5. **Drafts list after the rows changed underneath.** A reload arriving while the list is open must not make Enter/`d` act on a different draft than the highlighted one. (Task 7: `TestDraftList_ActsOnTheRowShown`.)

---

### Task 1: Wire layer: list, create, update, delete

**Files:**
- Create: `internal/slack/drafts.go`
- Create: `internal/slack/drafts_test.go`
- Create (copies): `internal/slack/testdata/drafts_create_autosave.json`, `drafts_update_autosave.json`, `drafts_list.json`

**Interfaces:**
- Produces (package `slackclient`):
  - `type Draft struct { ID string; DateCreated int64; LastUpdatedTS string; Blocks slack.Blocks; FileIDs []string; IsFromComposer, IsDeleted, IsSent bool; DateScheduled int64; Destinations []DraftDestination }`
  - `type DraftDestination struct { ChannelID, ThreadTS string; UserIDs []string; Broadcast bool }`
  - `func (d Draft) Attached() bool`, `func (d Draft) Writable() bool`
  - `type DraftWrite struct { ChannelID, ThreadTS, Text string; Edited time.Time }`
  - `type DraftRejectedError struct{ Method, Code string }`; `func DraftRejectionCode(err error) string`
  - `func (c *Client) ListDrafts(ctx) ([]Draft, error)`
  - `func (c *Client) CreateDraft(ctx, DraftWrite) (Draft, error)`
  - `func (c *Client) UpdateDraft(ctx, draftID string, w DraftWrite) (Draft, error)`
  - `func (c *Client) DeleteDraft(ctx, draftID string) error`

- [ ] **Step 1: Copy the captures into testdata**

```bash
cp docs/superpowers/specs/2026-11-25-slack-drafts-captures/drafts_create_autosave.json \
   docs/superpowers/specs/2026-11-25-slack-drafts-captures/drafts_update_autosave.json \
   docs/superpowers/specs/2026-11-25-slack-drafts-captures/drafts_list.json \
   internal/slack/testdata/
```

- [ ] **Step 2: Write the failing tests**

`internal/slack/drafts_test.go`:

```go
package slackclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"
)

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// assertDraftForm compares a draft form with a capture: same fields,
// same values, nothing extra. Fields in shapes are checked against
// their pattern instead (client_msg_id is random).
func assertDraftForm(t *testing.T, got url.Values, capture string, shapes map[string]*regexp.Regexp) {
	t.Helper()
	want := capturedForm(t, capture)
	for k := range want {
		if re, ok := shapes[k]; ok {
			if !re.MatchString(got.Get(k)) {
				t.Errorf("%s = %q, want shape %s", k, got.Get(k), re)
			}
			continue
		}
		if got.Get(k) != want.Get(k) {
			t.Errorf("%s =\n  %q\nwant\n  %q", k, got.Get(k), want.Get(k))
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected field %s=%q (not in %s)", k, got.Get(k), capture)
		}
	}
}

func TestDraftForms_MatchCaptures(t *testing.T) {
	w := DraftWrite{ChannelID: "C0EXAMPLE01", Text: "draft text 1", Edited: time.UnixMilli(1795617838196)}
	create, err := createDraftForm(w, newClientMsgID())
	if err != nil {
		t.Fatal(err)
	}
	assertDraftForm(t, create, "drafts_create_autosave.json", map[string]*regexp.Regexp{"client_msg_id": uuidRe})

	update, err := updateDraftForm("Dr0EXAMPLE01", w)
	if err != nil {
		t.Fatal(err)
	}
	assertDraftForm(t, update, "drafts_update_autosave.json", nil)
}

// A thread destination carries thread_ts and broadcast:false, in the
// order drafts.list reports them.
func TestDraftForms_ThreadDestination(t *testing.T) {
	f, err := createDraftForm(DraftWrite{ChannelID: "C1", ThreadTS: "10.5", Text: "x"}, "id")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := f.Get("destinations"), `[{"channel_id":"C1","thread_ts":"10.5","broadcast":false}]`; got != want {
		t.Errorf("destinations = %s, want %s", got, want)
	}
}

// drafts.list's kinds, as captured, classify as the spec's table says.
func TestDraft_KindsFromCapture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "drafts_list.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Response struct {
			Drafts []Draft `json:"drafts"`
		} `json:"response"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	d := c.Response.Drafts
	if len(d) != 6 {
		t.Fatalf("got %d drafts, want 6", len(d))
	}
	want := []struct{ attached, writable bool }{
		{true, true},   // channel
		{true, true},   // thread with a user mention
		{true, true},   // Slackbot DM (user_ids USLACK)
		{true, true},   // DM thread
		{false, false}, // unaddressed "New message"
		{false, false}, // stashed "Save as draft"
	}
	for i, w := range want {
		if d[i].Attached() != w.attached || d[i].Writable() != w.writable {
			t.Errorf("draft %d: attached=%v writable=%v, want %v %v", i, d[i].Attached(), d[i].Writable(), w.attached, w.writable)
		}
	}
	if d[1].Destinations[0].ThreadTS == "" || len(d[1].Blocks.BlockSet) != 1 {
		t.Errorf("thread draft decoded as %+v", d[1])
	}
	scheduled := d[0]
	scheduled.DateScheduled = 1
	files := d[0]
	files.FileIDs = []string{"F1"}
	if scheduled.Writable() || files.Writable() {
		t.Error("a scheduled draft or one with files must not be writable")
	}
}

// draftServer answers drafts.* with the bodies given per method, in
// order, and records every form it received.
type draftServer struct {
	mu     sync.Mutex
	bodies map[string][]string
	forms  map[string][]url.Values
}

func newDraftServer(t *testing.T, bodies map[string][]string) (*Client, *draftServer) {
	t.Helper()
	ds := &draftServer{bodies: bodies, forms: map[string][]url.Values{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		method := filepath.Base(r.URL.Path)
		ds.mu.Lock()
		defer ds.mu.Unlock()
		ds.forms[method] = append(ds.forms[method], r.PostForm)
		queue := ds.bodies[method]
		if len(queue) == 0 {
			t.Errorf("unexpected call to %s", method)
			_, _ = w.Write([]byte(`{"ok":false,"error":"unexpected"}`))
			return
		}
		body := queue[0]
		if len(queue) > 1 {
			ds.bodies[method] = queue[1:]
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return newTestClient(srv), ds
}

func TestListDrafts_PagesByNextTS(t *testing.T) {
	c, ds := newDraftServer(t, map[string][]string{"drafts.list": {
		`{"ok":true,"has_more":true,"drafts":[{"id":"Dr1","last_updated_ts":"3.0"},{"id":"Dr2","last_updated_ts":"2.0"}]}`,
		`{"ok":true,"has_more":false,"drafts":[{"id":"Dr2","last_updated_ts":"2.0"},{"id":"Dr3","last_updated_ts":"1.0"}]}`,
	}})
	got, err := c.ListDrafts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != "Dr1" || got[2].ID != "Dr3" {
		t.Fatalf("drafts = %+v, want Dr1 Dr2 Dr3 (Dr2 once)", got)
	}
	forms := ds.forms["drafts.list"]
	if len(forms) != 2 {
		t.Fatalf("%d list calls, want 2", len(forms))
	}
	if forms[0].Get("is_active") != "true" || forms[0].Get("limit") != "25" || forms[0].Has("next_ts") {
		t.Errorf("first page form = %v", forms[0])
	}
	if forms[1].Get("next_ts") != "2.0" {
		t.Errorf("second page next_ts = %q, want 2.0", forms[1].Get("next_ts"))
	}
}

// A server that keeps saying has_more without moving on ends the walk.
func TestListDrafts_StopsWithoutProgress(t *testing.T) {
	page := `{"ok":true,"has_more":true,"drafts":[{"id":"Dr1","last_updated_ts":"3.0"}]}`
	c, ds := newDraftServer(t, map[string][]string{"drafts.list": {page}})
	got, err := c.ListDrafts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(ds.forms["drafts.list"]) != 2 {
		t.Errorf("got %d drafts over %d calls, want 1 over 2", len(got), len(ds.forms["drafts.list"]))
	}
}

func TestDraftCalls_RefusalIsDraftRejectedError(t *testing.T) {
	c, _ := newDraftServer(t, map[string][]string{
		"drafts.create": {`{"ok":false,"error":"attached_draft_exists"}`},
		"drafts.update": {`{"ok":false,"error":"draft_has_conflict"}`},
		"drafts.delete": {`{"ok":false,"error":"draft_delete_invalid"}`},
	})
	ctx := context.Background()
	w := DraftWrite{ChannelID: "C1", Text: "x"}
	if _, err := c.CreateDraft(ctx, w); DraftRejectionCode(err) != "attached_draft_exists" {
		t.Errorf("create err = %v", err)
	}
	if _, err := c.UpdateDraft(ctx, "Dr1", w); DraftRejectionCode(err) != "draft_has_conflict" {
		t.Errorf("update err = %v", err)
	}
	if err := c.DeleteDraft(ctx, "Dr1"); DraftRejectionCode(err) != "draft_delete_invalid" {
		t.Errorf("delete err = %v", err)
	}
	if DraftRejectionCode(context.Canceled) != "" {
		t.Error("a transport error is not a rejection")
	}
}

func TestUpdateDraft_ReturnsTheStoredDraft(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "drafts_update_autosave.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	cl, _ := newDraftServer(t, map[string][]string{"drafts.update": {string(c.Response)}})
	d, err := cl.UpdateDraft(context.Background(), "Dr0EXAMPLE01", DraftWrite{ChannelID: "C0EXAMPLE01", Text: "draft text 1"})
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "Dr0EXAMPLE01" || d.LastUpdatedTS != "1795617850.177844" {
		t.Errorf("draft = %+v", d)
	}
}

// drafts.delete is not captured: draft_id and client_last_updated_ts
// (now, unix ms) follow the clients that call it against live
// workspaces (see the spec's Evidence).
func TestDeleteDraft_Form(t *testing.T) {
	c, ds := newDraftServer(t, map[string][]string{"drafts.delete": {`{"ok":true}`}})
	before := time.Now().UnixMilli()
	if err := c.DeleteDraft(context.Background(), "Dr1"); err != nil {
		t.Fatal(err)
	}
	f := ds.forms["drafts.delete"][0]
	if f.Get("draft_id") != "Dr1" {
		t.Errorf("draft_id = %q", f.Get("draft_id"))
	}
	if ms, err := parseMillis(f.Get("client_last_updated_ts")); err != nil || ms < before {
		t.Errorf("client_last_updated_ts = %q, want now in unix ms", f.Get("client_last_updated_ts"))
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/slack -run 'Draft' -count=1`
Expected: FAIL to compile: `undefined: createDraftForm`, `undefined: Draft`, …

- [ ] **Step 4: Implement**

`internal/slack/drafts.go`:

```go
package slackclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/slack-go/slack"

	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/slack/mrkdwn"
	"github.com/gammons/slk/internal/slackhttp"
)

// Draft is one draft as drafts.list, drafts.create and drafts.update
// return it (undocumented web-client API). Captures:
// docs/superpowers/specs/2026-11-25-slack-drafts-captures/.
type Draft struct {
	ID            string `json:"id"`
	DateCreated   int64  `json:"date_created"`
	LastUpdatedTS string `json:"last_updated_ts"`
	Blocks        slack.Blocks `json:"blocks"`
	FileIDs       []string     `json:"file_ids"`
	// IsFromComposer is true for a draft saved with Slack's "Save as
	// draft" and for one started in "New message"; false for the text
	// sitting in a conversation's composer (an attached draft).
	IsFromComposer bool               `json:"is_from_composer"`
	IsDeleted      bool               `json:"is_deleted"`
	IsSent         bool               `json:"is_sent"`
	DateScheduled  int64              `json:"date_scheduled"`
	Destinations   []DraftDestination `json:"destinations"`
}

// DraftDestination is where a draft will be sent. A DM reads back with
// UserIDs too; slk writes ChannelID only, as the web client does.
type DraftDestination struct {
	ChannelID string   `json:"channel_id"`
	ThreadTS  string   `json:"thread_ts,omitempty"`
	UserIDs   []string `json:"user_ids,omitempty"`
	Broadcast bool     `json:"broadcast,omitempty"`
}

// Attached reports a conversation's composer draft: one destination, not
// saved with "Save as draft". Slack keeps one per destination.
func (d Draft) Attached() bool { return !d.IsFromComposer && len(d.Destinations) == 1 }

// Writable reports an attached draft slk may restore and overwrite: not
// scheduled, no files.
func (d Draft) Writable() bool {
	return d.Attached() && d.DateScheduled == 0 && len(d.FileIDs) == 0
}

// DraftWrite is the content of a create or update: the destination, the
// composer text (markdown, mentions in wire form) and when it was last
// edited.
type DraftWrite struct {
	ChannelID string
	ThreadTS  string
	Text      string
	Edited    time.Time
}

// DraftRejectedError is a drafts.* {"ok":false,"error":Code}, for
// example attached_draft_exists or draft_has_conflict.
type DraftRejectedError struct{ Method, Code string }

func (e *DraftRejectedError) Error() string { return e.Method + ": " + e.Code }

// DraftRejectionCode returns the code of a *DraftRejectedError in err's
// chain, "" for any other error and for nil.
func DraftRejectionCode(err error) string {
	var r *DraftRejectedError
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

// draftReason is the _x_reason the web client's composer autosave sends
// (drafts_create_autosave.json, drafts_update_autosave.json).
const draftReason = "MessageInput:updateDraft"

const (
	draftListPageLimit = 25
	// draftListMaxPages bounds the walk so a server that never says
	// has_more=false cannot loop: 1000 drafts.
	draftListMaxPages = 40
)

// ListDrafts returns the user's active drafts, newest first, following
// drafts.list's next_ts paging (the oldest last_updated_ts so far).
func (c *Client) ListDrafts(ctx context.Context) ([]Draft, error) {
	ctx = slackhttp.WithReason(ctx, "client-v2-boot-team")
	var out []Draft
	seen := map[string]bool{}
	nextTS := ""
	for range draftListMaxPages {
		form := url.Values{"is_active": {"true"}, "limit": {strconv.Itoa(draftListPageLimit)}}
		if nextTS != "" {
			form.Set("next_ts", nextTS)
		}
		raw, err := c.postForm(ctx, "drafts.list", form)
		if err != nil {
			return nil, err
		}
		var resp struct {
			OK      bool    `json:"ok"`
			Error   string  `json:"error"`
			Drafts  []Draft `json:"drafts"`
			HasMore bool    `json:"has_more"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("parsing drafts.list: %w (body=%s)", err, truncateForLog(raw))
		}
		if !resp.OK {
			return nil, &DraftRejectedError{Method: "drafts.list", Code: resp.Error}
		}
		for _, d := range resp.Drafts {
			if !seen[d.ID] {
				seen[d.ID] = true
				out = append(out, d)
			}
		}
		if !resp.HasMore || len(resp.Drafts) == 0 {
			return out, nil
		}
		last := resp.Drafts[len(resp.Drafts)-1].LastUpdatedTS
		if last == nextTS {
			debuglog.General("drafts.list: has_more without progress at next_ts=%s", nextTS)
			return out, nil
		}
		nextTS = last
	}
	debuglog.General("drafts.list: stopped after %d pages", draftListMaxPages)
	return out, nil
}

// CreateDraft makes the attached draft of w's destination.
func (c *Client) CreateDraft(ctx context.Context, w DraftWrite) (Draft, error) {
	form, err := createDraftForm(w, newClientMsgID())
	if err != nil {
		return Draft{}, err
	}
	return c.draftCall(ctx, "drafts.create", form)
}

// UpdateDraft replaces draftID's text with w's.
func (c *Client) UpdateDraft(ctx context.Context, draftID string, w DraftWrite) (Draft, error) {
	form, err := updateDraftForm(draftID, w)
	if err != nil {
		return Draft{}, err
	}
	return c.draftCall(ctx, "drafts.update", form)
}

// DeleteDraft deletes draftID. Not captured; the shape is the one
// open-source clients use against live workspaces (spec, Evidence).
func (c *Client) DeleteDraft(ctx context.Context, draftID string) error {
	form := url.Values{
		"draft_id":               {draftID},
		"client_last_updated_ts": {strconv.FormatInt(time.Now().UnixMilli(), 10)},
	}
	_, err := c.draftCall(ctx, "drafts.delete", form)
	return err
}

func (c *Client) draftCall(ctx context.Context, method string, form url.Values) (Draft, error) {
	ctx = slackhttp.WithReason(ctx, draftReason)
	debuglog.General("%s: draft_id=%s", method, form.Get("draft_id"))
	raw, err := c.postForm(ctx, method, form)
	if err != nil {
		return Draft{}, err
	}
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Draft Draft  `json:"draft"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Draft{}, fmt.Errorf("parsing %s: %w (body=%s)", method, err, truncateForLog(raw))
	}
	if !resp.OK {
		debuglog.General("%s: rejected error=%s", method, resp.Error)
		return Draft{}, &DraftRejectedError{Method: method, Code: resp.Error}
	}
	return resp.Draft, nil
}

// draftForm is the fields create and update share.
func draftForm(w DraftWrite) (url.Values, error) {
	_, block := mrkdwn.Convert(w.Text)
	if block == nil {
		return nil, fmt.Errorf("draft text converts to no block")
	}
	blocks, err := marshalUnescaped([]slack.Block{block})
	if err != nil {
		return nil, fmt.Errorf("encoding draft blocks: %w", err)
	}
	dest, err := draftDestinations(w.ChannelID, w.ThreadTS)
	if err != nil {
		return nil, err
	}
	return url.Values{
		"blocks":           {blocks},
		"attachments":      {"[]"},
		"destinations":     {dest},
		"file_ids":         {"[]"},
		"is_from_composer": {"false"},
	}, nil
}

func createDraftForm(w DraftWrite, clientMsgID string) (url.Values, error) {
	form, err := draftForm(w)
	if err != nil {
		return nil, err
	}
	form.Set("client_msg_id", clientMsgID)
	return form, nil
}

func updateDraftForm(draftID string, w DraftWrite) (url.Values, error) {
	form, err := draftForm(w)
	if err != nil {
		return nil, err
	}
	form.Set("draft_id", draftID)
	form.Set("client_last_updated_ts", strconv.FormatInt(w.Edited.UnixMilli(), 10))
	return form, nil
}

// draftDestinations is the one-destination list, with thread_ts and
// broadcast:false for a thread, in drafts.list's key order.
func draftDestinations(channelID, threadTS string) (string, error) {
	if threadTS == "" {
		return marshalUnescaped([]struct {
			ChannelID string `json:"channel_id"`
		}{{channelID}})
	}
	return marshalUnescaped([]struct {
		ChannelID string `json:"channel_id"`
		ThreadTS  string `json:"thread_ts"`
		Broadcast bool   `json:"broadcast"`
	}{{channelID, threadTS, false}})
}

// parseMillis reads a unix-millisecond client_last_updated_ts.
func parseMillis(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) }
```

(`parseMillis` exists for the delete test; keep it unexported.)

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/slack -run 'Draft' -count=1 -v`
Expected: PASS. If `TestDraftForms_MatchCaptures` reports a `blocks` mismatch, print `mrkdwn.Convert("draft text 1")`'s JSON and compare with the capture; the capture is the truth.

- [ ] **Step 6: Commit**

```bash
gofmt -l internal/slack
go vet ./internal/slack
git add internal/slack/drafts.go internal/slack/drafts_test.go internal/slack/testdata/drafts_*.json
git commit -m "feat(slack): drafts.list/create/update/delete client"
```

---

### Task 2: Send a message as a draft

**Files:**
- Modify: `internal/slack/client.go` (`postMessage`, `SendMessage`, `SendReply`, lines ~1045-1080 and ~1310-1322)
- Modify: `internal/slack/link_chips.go` (`sendWithChips`, line ~446)
- Modify: `internal/slack/drafts.go` (add `DraftRef`, `SendDraft`, `plainPostForm`)
- Test: `internal/slack/drafts_test.go`

**Interfaces:**
- Consumes: Task 1's file.
- Produces: `type DraftRef struct{ ID, LastUpdatedTS string }`; `func (c *Client) SendDraft(ctx, channelID, threadTS, text string, broadcast bool, ref DraftRef) (ts, mrkdwn string, err error)`. A refusal comes back as `slack.SlackErrorResponse{Err: "draft_…"}` (wrapped by `fmt.Errorf("sending message: %w")`).

- [ ] **Step 1: Write the failing tests** (append to `drafts_test.go`; add `"errors"` and `"github.com/slack-go/slack"` to its imports)

```go
// chat.postMessage's draft fields are documented by Slack but not
// captured from the web client.
func TestSendDraft_PostsDraftFields(t *testing.T) {
	c, ds := newDraftServer(t, map[string][]string{"chat.postMessage": {`{"ok":true,"ts":"5.0"}`}})
	ts, mr, err := c.SendDraft(context.Background(), "C1", "10.0", "hi **there**", true,
		DraftRef{ID: "Dr1", LastUpdatedTS: "1795617850.177844"})
	if err != nil {
		t.Fatal(err)
	}
	if ts != "5.0" || mr != "hi *there*" {
		t.Errorf("ts, mrkdwn = %q, %q", ts, mr)
	}
	f := ds.forms["chat.postMessage"][0]
	for k, want := range map[string]string{
		"draft_id":                      "Dr1",
		"current_draft_last_updated_ts": "1795617850.177844",
		"channel":                       "C1",
		"thread_ts":                     "10.0",
		"reply_broadcast":               "true",
		"text":                          "hi *there*",
	} {
		if f.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, f.Get(k), want)
		}
	}
	if !uuidRe.MatchString(f.Get("client_msg_id")) || f.Get("blocks") == "" {
		t.Errorf("client_msg_id = %q, blocks = %q", f.Get("client_msg_id"), f.Get("blocks"))
	}
}

func TestSendDraft_RefusalIsSlackErrorResponse(t *testing.T) {
	c, _ := newDraftServer(t, map[string][]string{"chat.postMessage": {`{"ok":false,"error":"draft_already_sent"}`}})
	_, _, err := c.SendDraft(context.Background(), "C1", "", "hi", false, DraftRef{ID: "Dr1"})
	var r slack.SlackErrorResponse
	if !errors.As(err, &r) || r.Err != "draft_already_sent" {
		t.Errorf("err = %v, want SlackErrorResponse draft_already_sent", err)
	}
}
```

(`mr` for `"hi **there**"` is mrkdwn.Convert's output; if Convert renders bold differently, change the expected value to what `mrkdwn.Convert` returns, not the other way round.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/slack -run SendDraft -count=1`
Expected: FAIL to compile: `undefined: DraftRef`.

- [ ] **Step 3: Implement**

In `link_chips.go`, give `sendWithChips` an `extra url.Values` parameter merged into the chip form:

```go
func (c *Client) sendWithChips(ctx context.Context, channelID, threadTS, text string, broadcast bool, block *slack.RichTextBlock, extra url.Values) (ts, mr string, sent bool, err error) {
	// ... unchanged up to chipPostForm ...
	form, err := chipPostForm(channelID, threadTS, broadcast, mr, block, chipURLs, newClientMsgID())
	if err != nil {
		return "", "", true, err
	}
	for k, v := range extra {
		form[k] = v
	}
	ts, err = c.postChips(ctx, form)
	// ... unchanged ...
}
```

In `client.go`, thread a `*DraftRef` through `postMessage`; `SendMessage` and `SendReply` pass `nil`:

```go
func (c *Client) postMessage(ctx context.Context, channelID, threadTS, text string, broadcast bool, ref *DraftRef) (string, string, error) {
	mr, block := mrkdwn.Convert(text)
	extra := ref.fields()
	if ts, sentMr, sent, err := c.sendWithChips(ctx, channelID, threadTS, text, broadcast, block, extra); sent {
		return ts, sentMr, err
	}
	if ref != nil {
		// slack-go has no option for the draft fields, so a draft goes
		// out as the hand-rolled form the chips path already sends.
		form, err := plainPostForm(channelID, threadTS, broadcast, mr, block, newClientMsgID())
		if err != nil {
			return "", "", err
		}
		for k, v := range extra {
			form[k] = v
		}
		ts, err := c.postChips(ctx, form)
		return ts, mr, err
	}
	// ... existing slack-go path unchanged ...
}
```

`c.postMessage(ctx, channelID, "", text, false)` → `c.postMessage(ctx, channelID, "", text, false, nil)` in `SendMessage`; same in `SendReply`.

Append to `drafts.go`:

```go
// DraftRef names the draft a message is sent from, so Slack marks it
// sent with the post (chat.postMessage draft_id and
// current_draft_last_updated_ts; documented, not captured).
type DraftRef struct {
	ID            string
	LastUpdatedTS string // the draft's last_updated_ts as Slack returned it
}

func (r *DraftRef) fields() url.Values {
	if r == nil {
		return nil
	}
	return url.Values{"draft_id": {r.ID}, "current_draft_last_updated_ts": {r.LastUpdatedTS}}
}

// SendDraft posts text as SendMessage (threadTS "") or SendReply does,
// marking ref sent with it. Slack's refusal is slack.SlackErrorResponse,
// e.g. draft_already_sent: nothing was posted.
func (c *Client) SendDraft(ctx context.Context, channelID, threadTS, text string, broadcast bool, ref DraftRef) (string, string, error) {
	ts, mr, err := c.postMessage(ctx, channelID, threadTS, text, broadcast, &ref)
	if err != nil {
		return "", "", fmt.Errorf("sending message: %w", err)
	}
	return ts, mr, nil
}

// plainPostForm is chat.postMessage's fields for a message without
// chips: chipPostForm without unfurl.
func plainPostForm(channelID, threadTS string, broadcast bool, mr string, block *slack.RichTextBlock, clientMsgID string) (url.Values, error) {
	form := url.Values{
		"channel":       {channelID},
		"text":          {mr},
		"client_msg_id": {clientMsgID},
	}
	if block != nil {
		blocks, err := marshalUnescaped([]slack.Block{block})
		if err != nil {
			return nil, fmt.Errorf("encoding blocks: %w", err)
		}
		form.Set("blocks", blocks)
	}
	if threadTS != "" {
		form.Set("thread_ts", threadTS)
		if broadcast {
			form.Set("reply_broadcast", "true")
		}
	}
	return form, nil
}
```

- [ ] **Step 4: Run the package's tests**

Run: `go test ./internal/slack -count=1`
Expected: PASS (the chip tests still pass with the extra `nil` argument).

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/slack
git add internal/slack
git commit -m "feat(slack): SendDraft marks a draft sent with chat.postMessage"
```

---

### Task 3: The `DraftService` port

**Files:**
- Create: `internal/core/drafts.go`
- Modify: `internal/core/ports.go` (add `DraftService` after `CommandService`, line ~72)
- Modify: `internal/core/adapters.go` (add `DraftServiceFuncs` after the command adapter, line ~121)
- Create: `internal/core/drafts_test.go`

**Interfaces:**
- Produces (package `core`): `DraftKey{TeamID, ChannelID, ThreadTS string}`; `Draft{ID string; Key DraftKey; Text string; Updated, Scheduled time.Time; FileCount int; Saved bool}` with `Writable() bool`; `DraftSaveResult{Draft Draft; Replaced bool}`; `DraftChange{Key DraftKey; Text string; Edited time.Time}`; `ErrDraftHandsOff`; `DraftService` (`Load`, `Save`, `Discard`, `Delete`); `DraftServiceFuncs`; `NewDraftService(DraftServiceFuncs) DraftService`.

- [ ] **Step 1: Write the failing tests**

`internal/core/drafts_test.go`:

```go
package core

import (
	"errors"
	"testing"
	"time"
)

func TestDraftWritable(t *testing.T) {
	base := Draft{ID: "Dr1", Key: DraftKey{TeamID: "T1", ChannelID: "C1"}}
	cases := []struct {
		name string
		mut  func(*Draft)
		want bool
	}{
		{"attached", func(*Draft) {}, true},
		{"thread", func(d *Draft) { d.Key.ThreadTS = "1.0" }, true},
		{"saved", func(d *Draft) { d.Saved = true }, false},
		{"no recipient", func(d *Draft) { d.Key.ChannelID = "" }, false},
		{"scheduled", func(d *Draft) { d.Scheduled = time.Unix(1, 0) }, false},
		{"files", func(d *Draft) { d.FileCount = 2 }, false},
	}
	for _, c := range cases {
		d := base
		c.mut(&d)
		if got := d.Writable(); got != c.want {
			t.Errorf("%s: Writable() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDraftServiceNilFuncsAreUnsupported(t *testing.T) {
	s := NewDraftService(DraftServiceFuncs{})
	if _, err := s.Load("T1"); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Load err = %v", err)
	}
	if _, err := s.Save(DraftKey{}, "x", time.Time{}); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Save err = %v", err)
	}
	if err := s.Discard(DraftKey{}); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Discard err = %v", err)
	}
	if err := s.Delete("T1", "Dr1"); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Delete err = %v", err)
	}
}

func TestDraftServicePassesArguments(t *testing.T) {
	key := DraftKey{TeamID: "T1", ChannelID: "C1", ThreadTS: "1.0"}
	at := time.Unix(100, 0)
	var got []string
	s := NewDraftService(DraftServiceFuncs{
		Save: func(k DraftKey, text string, edited time.Time) (DraftSaveResult, error) {
			if k != key || text != "hi" || !edited.Equal(at) {
				t.Errorf("Save(%v, %q, %v)", k, text, edited)
			}
			got = append(got, "save")
			return DraftSaveResult{Replaced: true}, nil
		},
	})
	res, err := s.Save(key, "hi", at)
	if err != nil || !res.Replaced || len(got) != 1 {
		t.Errorf("res=%+v err=%v calls=%v", res, err, got)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/core -run Draft -count=1`
Expected: FAIL to compile: `undefined: Draft`.

- [ ] **Step 3: Implement**

`internal/core/drafts.go`:

```go
package core

import (
	"errors"
	"time"
)

// DraftKey names a conversation's composer: a channel or DM, or a thread
// in one when ThreadTS is set. compose.Model keys its slots the same way.
type DraftKey struct {
	TeamID    string
	ChannelID string
	ThreadTS  string
}

// Draft is one of the user's active Slack drafts.
type Draft struct {
	ID string
	// Key is the draft's conversation; ChannelID is "" when the draft
	// has no single recipient (a "New message" draft).
	Key DraftKey
	// Text is the composer markdown, mentions in wire form (<@U…>).
	Text    string
	Updated time.Time
	// Scheduled is when Slack will send it; zero unless scheduled.
	Scheduled time.Time
	FileCount int
	// Saved is Slack's is_from_composer: a draft kept with "Save as
	// draft", or started in "New message". Never a composer's text.
	Saved bool
}

// Writable reports a draft slk restores into a composer and overwrites:
// a conversation's composer text, unscheduled, without files.
func (d Draft) Writable() bool {
	return !d.Saved && d.Key.ChannelID != "" && d.Scheduled.IsZero() && d.FileCount == 0
}

// DraftSaveResult is a save's answer. Replaced reports that a newer
// draft from another device was overwritten.
type DraftSaveResult struct {
	Draft    Draft
	Replaced bool
}

// DraftChange is a slot whose text Slack does not have yet; Text "" means
// the draft is to be discarded. Text is in wire form.
type DraftChange struct {
	Key    DraftKey
	Text   string
	Edited time.Time
}

// ErrDraftHandsOff is Save's answer when the conversation's Slack draft is
// one slk won't overwrite (scheduled, with files, or a deleted draft
// still holding the slot).
var ErrDraftHandsOff = errors.New("slack already has a draft here that slk won't overwrite")
```

In `ports.go`, after `CommandService`:

```go
// DraftService syncs composer drafts with Slack's own (drafts.*). Every
// method is blocking; callers run it from a tea.Cmd. teamID / key.TeamID
// names the workspace, which need not be the active one.
//
// Build one with NewDraftService.
type DraftService interface {
	// Load lists the workspace's active drafts.
	Load(teamID string) ([]Draft, error)
	// Save makes text (wire form) the conversation's draft. edited is
	// when the text was last changed. ErrDraftHandsOff when the
	// conversation's draft is one slk won't overwrite.
	Save(key DraftKey, text string, edited time.Time) (DraftSaveResult, error)
	// Discard deletes the conversation's draft, if slk knows of one.
	Discard(key DraftKey) error
	// Delete deletes one draft by id (the Drafts list).
	Delete(teamID, draftID string) error
}
```

(`ports.go` needs `"time"` in its imports if it does not have it.)

In `adapters.go`, after the command adapter:

```go
// DraftServiceFuncs bundles the closures behind a DraftService. A nil
// func makes its method return errors.ErrUnsupported.
type DraftServiceFuncs struct {
	Load    func(teamID string) ([]Draft, error)
	Save    func(key DraftKey, text string, edited time.Time) (DraftSaveResult, error)
	Discard func(key DraftKey) error
	Delete  func(teamID, draftID string) error
}

// NewDraftService builds a DraftService from fns. Used by
// cmd/slk/main.go (production wiring) and tests.
func NewDraftService(fns DraftServiceFuncs) DraftService { return draftAdapter{fns: fns} }

type draftAdapter struct{ fns DraftServiceFuncs }

func (d draftAdapter) Load(teamID string) ([]Draft, error) {
	if d.fns.Load == nil {
		return nil, errors.ErrUnsupported
	}
	return d.fns.Load(teamID)
}

func (d draftAdapter) Save(key DraftKey, text string, edited time.Time) (DraftSaveResult, error) {
	if d.fns.Save == nil {
		return DraftSaveResult{}, errors.ErrUnsupported
	}
	return d.fns.Save(key, text, edited)
}

func (d draftAdapter) Discard(key DraftKey) error {
	if d.fns.Discard == nil {
		return errors.ErrUnsupported
	}
	return d.fns.Discard(key)
}

func (d draftAdapter) Delete(teamID, draftID string) error {
	if d.fns.Delete == nil {
		return errors.ErrUnsupported
	}
	return d.fns.Delete(teamID, draftID)
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/core -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/core
git add internal/core
git commit -m "feat(core): DraftService port"
```

---

### Task 4: The per-workspace draft engine

**Files:**
- Create: `cmd/slk/drafts.go`
- Create: `cmd/slk/drafts_test.go`

**Interfaces:**
- Consumes: `slackclient.Draft`, `DraftWrite`, `DraftRef`, `DraftRejectionCode` (Tasks 1-2); `core.Draft`, `DraftSaveResult`, `ErrDraftHandsOff` (Task 3).
- Produces (package `main`):
  - `type draftAPI interface { ListDrafts(ctx) ([]slackclient.Draft, error); CreateDraft(ctx, slackclient.DraftWrite) (slackclient.Draft, error); UpdateDraft(ctx, string, slackclient.DraftWrite) (slackclient.Draft, error); DeleteDraft(ctx, string) error }` (`*slackclient.Client` satisfies it)
  - `type draftDest struct{ channelID, threadTS string }`
  - `func newDraftEngine(api draftAPI, teamID string) *draftEngine`
  - `(*draftEngine).Load(ctx) ([]core.Draft, error)`, `Save(ctx, draftDest, text string, edited time.Time) (core.DraftSaveResult, error)`, `Discard(ctx, draftDest) error`, `Delete(ctx, draftID string) error`
  - `type draftSender interface { SendMessage(ctx, ch, text string) (string, string, error); SendReply(ctx, ch, threadTS, text string, broadcast bool) (string, string, error); SendDraft(ctx, ch, threadTS, text string, broadcast bool, ref slackclient.DraftRef) (string, string, error) }`
  - `func sendWithDraft(ctx, client draftSender, drafts *draftEngine, channelID, threadTS, text string, broadcast bool) (ts, mrkdwn string, err error)` (nil `drafts` = plain send)
  - `func coreDraft(teamID string, d slackclient.Draft) core.Draft`

- [ ] **Step 1: Write the failing tests**

`cmd/slk/drafts_test.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/slack-go/slack"

	"github.com/gammons/slk/internal/core"
	slackclient "github.com/gammons/slk/internal/slack"
)

// fakeDraftAPI records every call and answers from its funcs; a nil
// func answers ok with a fresh draft.
type fakeDraftAPI struct {
	mu     sync.Mutex
	calls  []string
	list   func() ([]slackclient.Draft, error)
	create func(w slackclient.DraftWrite) (slackclient.Draft, error)
	update func(id string, w slackclient.DraftWrite) (slackclient.Draft, error)
	delete func(id string) error
	n      int
}

func (f *fakeDraftAPI) record(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
}

func (f *fakeDraftAPI) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeDraftAPI) ListDrafts(context.Context) ([]slackclient.Draft, error) {
	f.record("list")
	if f.list == nil {
		return nil, nil
	}
	return f.list()
}

func (f *fakeDraftAPI) CreateDraft(_ context.Context, w slackclient.DraftWrite) (slackclient.Draft, error) {
	f.record("create " + w.ChannelID + " " + w.Text)
	if f.create != nil {
		return f.create(w)
	}
	f.mu.Lock()
	f.n++
	id := fmt.Sprintf("Dr%d", f.n)
	f.mu.Unlock()
	return attached(id, w.ChannelID, w.ThreadTS, "1.0"), nil
}

func (f *fakeDraftAPI) UpdateDraft(_ context.Context, id string, w slackclient.DraftWrite) (slackclient.Draft, error) {
	f.record(fmt.Sprintf("update %s %s %d", id, w.Text, w.Edited.UnixMilli()))
	if f.update != nil {
		return f.update(id, w)
	}
	return attached(id, w.ChannelID, w.ThreadTS, "2.0"), nil
}

func (f *fakeDraftAPI) DeleteDraft(_ context.Context, id string) error {
	f.record("delete " + id)
	if f.delete != nil {
		return f.delete(id)
	}
	return nil
}

func attached(id, channelID, threadTS, ts string) slackclient.Draft {
	return slackclient.Draft{ID: id, LastUpdatedTS: ts,
		Destinations: []slackclient.DraftDestination{{ChannelID: channelID, ThreadTS: threadTS}}}
}

func rejected(code string) error { return &slackclient.DraftRejectedError{Method: "drafts.x", Code: code} }

var c1 = draftDest{channelID: "C1"}

func TestDraftEngine_CreatesThenUpdates(t *testing.T) {
	api := &fakeDraftAPI{}
	e := newDraftEngine(api, "T1")
	ctx := context.Background()
	res, err := e.Save(ctx, c1, "one", time.UnixMilli(1000))
	if err != nil || res.Draft.ID != "Dr1" || res.Draft.Key != (core.DraftKey{TeamID: "T1", ChannelID: "C1"}) {
		t.Fatalf("first save = %+v, %v", res, err)
	}
	if _, err := e.Save(ctx, c1, "two", time.UnixMilli(2000)); err != nil {
		t.Fatal(err)
	}
	want := []string{"create C1 one", "update Dr1 two 2000"}
	if got := api.Calls(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
}

func TestDraftEngine_ConflictRetriesWithNow(t *testing.T) {
	api := &fakeDraftAPI{list: func() ([]slackclient.Draft, error) {
		return []slackclient.Draft{attached("Dr7", "C1", "", "1.0")}, nil
	}}
	tries := 0
	api.update = func(id string, w slackclient.DraftWrite) (slackclient.Draft, error) {
		tries++
		if tries == 1 {
			return slackclient.Draft{}, rejected("draft_has_conflict")
		}
		return attached(id, "C1", "", "3.0"), nil
	}
	e := newDraftEngine(api, "T1")
	e.now = func() time.Time { return time.UnixMilli(9000) }
	ctx := context.Background()
	if _, err := e.Load(ctx); err != nil {
		t.Fatal(err)
	}
	res, err := e.Save(ctx, c1, "mine", time.UnixMilli(1000))
	if err != nil || !res.Replaced {
		t.Fatalf("save = %+v, %v; want Replaced", res, err)
	}
	want := []string{"list", "update Dr7 mine 1000", "update Dr7 mine 9000"}
	if got := api.Calls(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
}

func TestDraftEngine_AdoptsAnotherDevicesDraft(t *testing.T) {
	api := &fakeDraftAPI{}
	api.create = func(slackclient.DraftWrite) (slackclient.Draft, error) {
		return slackclient.Draft{}, rejected("attached_draft_exists")
	}
	api.list = func() ([]slackclient.Draft, error) {
		return []slackclient.Draft{attached("Dr9", "C1", "", "1.0")}, nil
	}
	e := newDraftEngine(api, "T1")
	res, err := e.Save(context.Background(), c1, "mine", time.UnixMilli(5))
	if err != nil || !res.Replaced || res.Draft.ID != "Dr9" {
		t.Fatalf("save = %+v, %v; want Dr9 replaced", res, err)
	}
	want := []string{"create C1 mine", "list", "update Dr9 mine 5"}
	if got := api.Calls(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
}

// A deleted draft can hold the slot: create refuses and the list does not
// show it. slk keeps the text to itself.
func TestDraftEngine_DeletedDraftHoldingTheSlotIsHandsOff(t *testing.T) {
	api := &fakeDraftAPI{}
	api.create = func(slackclient.DraftWrite) (slackclient.Draft, error) {
		return slackclient.Draft{}, rejected("attached_draft_exists")
	}
	e := newDraftEngine(api, "T1")
	if _, err := e.Save(context.Background(), c1, "mine", time.Time{}); !errors.Is(err, core.ErrDraftHandsOff) {
		t.Fatalf("err = %v, want ErrDraftHandsOff", err)
	}
}

func TestDraftEngine_HandsOffDraftsAreNeverWritten(t *testing.T) {
	sched := attached("Dr1", "C1", "", "1.0")
	sched.DateScheduled = 1800000000
	files := attached("Dr2", "C2", "", "1.0")
	files.FileIDs = []string{"F1"}
	api := &fakeDraftAPI{list: func() ([]slackclient.Draft, error) { return []slackclient.Draft{sched, files}, nil }}
	e := newDraftEngine(api, "T1")
	ctx := context.Background()
	if _, err := e.Load(ctx); err != nil {
		t.Fatal(err)
	}
	for _, d := range []draftDest{c1, {channelID: "C2"}} {
		if _, err := e.Save(ctx, d, "x", time.Time{}); !errors.Is(err, core.ErrDraftHandsOff) {
			t.Errorf("%v: err = %v, want ErrDraftHandsOff", d, err)
		}
		if err := e.Discard(ctx, d); err != nil {
			t.Errorf("%v: discard err = %v", d, err)
		}
	}
	if got := api.Calls(); fmt.Sprint(got) != "[list]" {
		t.Errorf("calls = %v, want only the list", got)
	}
}

// An update of a draft that was sent or deleted elsewhere makes a new one.
func TestDraftEngine_GoneDraftIsRecreated(t *testing.T) {
	api := &fakeDraftAPI{list: func() ([]slackclient.Draft, error) {
		return []slackclient.Draft{attached("Dr7", "C1", "", "1.0")}, nil
	}}
	api.update = func(string, slackclient.DraftWrite) (slackclient.Draft, error) {
		return slackclient.Draft{}, rejected("draft_already_sent")
	}
	e := newDraftEngine(api, "T1")
	ctx := context.Background()
	_, _ = e.Load(ctx)
	res, err := e.Save(ctx, c1, "again", time.UnixMilli(1))
	if err != nil || res.Draft.ID != "Dr1" {
		t.Fatalf("save = %+v, %v; want new Dr1", res, err)
	}
}

func TestDraftEngine_DiscardDeletesAndForgets(t *testing.T) {
	api := &fakeDraftAPI{}
	e := newDraftEngine(api, "T1")
	ctx := context.Background()
	if err := e.Discard(ctx, c1); err != nil { // nothing known: no call
		t.Fatal(err)
	}
	_, _ = e.Save(ctx, c1, "x", time.Time{})
	if err := e.Discard(ctx, c1); err != nil {
		t.Fatal(err)
	}
	if err := e.Discard(ctx, c1); err != nil { // forgotten: no second delete
		t.Fatal(err)
	}
	want := []string{"create C1 x", "delete Dr1"}
	if got := api.Calls(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
}

// Two saves for one conversation never overlap: the second starts after
// the first answered.
func TestDraftEngine_OneCallInFlightPerConversation(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	api := &fakeDraftAPI{}
	api.create = func(w slackclient.DraftWrite) (slackclient.Draft, error) {
		entered <- struct{}{}
		<-release
		return attached("Dr1", w.ChannelID, "", "1.0"), nil
	}
	e := newDraftEngine(api, "T1")
	ctx := context.Background()
	done := make(chan struct{})
	go func() { _, _ = e.Save(ctx, c1, "one", time.Time{}); done <- struct{}{} }()
	<-entered
	go func() { _, _ = e.Save(ctx, c1, "two", time.Time{}); done <- struct{}{} }()
	select {
	case <-entered:
		t.Fatal("second save reached Slack while the first was in flight")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-done
	<-done
	want := []string{"create C1 one", "update Dr1 two -62135596800000"}
	if got := api.Calls(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
}

// fakeSender records which send it was asked for.
type fakeSender struct {
	calls   []string
	ref     slackclient.DraftRef
	refusal error
}

func (s *fakeSender) SendMessage(_ context.Context, ch, text string) (string, string, error) {
	s.calls = append(s.calls, "plain "+ch)
	return "5.0", text, nil
}

func (s *fakeSender) SendReply(_ context.Context, ch, thread, text string, _ bool) (string, string, error) {
	s.calls = append(s.calls, "reply "+ch+" "+thread)
	return "5.0", text, nil
}

func (s *fakeSender) SendDraft(_ context.Context, ch, _ string, text string, _ bool, ref slackclient.DraftRef) (string, string, error) {
	s.calls = append(s.calls, "draft "+ch+" "+ref.ID)
	s.ref = ref
	if s.refusal != nil {
		return "", "", fmt.Errorf("sending message: %w", s.refusal)
	}
	return "5.0", text, nil
}

func TestSendWithDraft_MarksTheDraftSent(t *testing.T) {
	api := &fakeDraftAPI{}
	e := newDraftEngine(api, "T1")
	ctx := context.Background()
	_, _ = e.Save(ctx, c1, "hello", time.Time{})
	s := &fakeSender{}
	if _, _, err := sendWithDraft(ctx, s, e, "C1", "", "hello", false); err != nil {
		t.Fatal(err)
	}
	if s.ref != (slackclient.DraftRef{ID: "Dr1", LastUpdatedTS: "1.0"}) {
		t.Errorf("ref = %+v", s.ref)
	}
	// Sent: the engine forgot it, so the next send is plain.
	if _, _, err := sendWithDraft(ctx, s, e, "C1", "", "again", false); err != nil {
		t.Fatal(err)
	}
	want := []string{"draft C1 Dr1", "plain C1"}
	if fmt.Sprint(s.calls) != fmt.Sprint(want) {
		t.Errorf("sends = %v, want %v", s.calls, want)
	}
}

func TestSendWithDraft_RefusalFallsBackAndDeletesTheDraft(t *testing.T) {
	api := &fakeDraftAPI{}
	e := newDraftEngine(api, "T1")
	ctx := context.Background()
	_, _ = e.Save(ctx, draftDest{"C1", "9.0"}, "hello", time.Time{})
	s := &fakeSender{refusal: slack.SlackErrorResponse{Err: "draft_already_sent"}}
	if _, _, err := sendWithDraft(ctx, s, e, "C1", "9.0", "hello", false); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(s.calls) != "[draft C1 Dr1 reply C1 9.0]" {
		t.Errorf("sends = %v", s.calls)
	}
	if got := api.Calls(); got[len(got)-1] != "delete Dr1" {
		t.Errorf("calls = %v, want a final delete Dr1", got)
	}
}

func TestSendWithDraft_WaitsForInFlightSave(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	api := &fakeDraftAPI{}
	api.create = func(w slackclient.DraftWrite) (slackclient.Draft, error) {
		close(entered)
		<-release
		return attached("Dr1", "C1", "", "4.0"), nil
	}
	e := newDraftEngine(api, "T1")
	ctx := context.Background()
	go func() { _, _ = e.Save(ctx, c1, "hello", time.Time{}) }()
	<-entered
	s := &fakeSender{}
	sent := make(chan struct{})
	go func() { _, _, _ = sendWithDraft(ctx, s, e, "C1", "", "hello", false); close(sent) }()
	select {
	case <-sent:
		t.Fatal("send did not wait for the in-flight save")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-sent
	if s.ref.ID != "Dr1" || s.ref.LastUpdatedTS != "4.0" {
		t.Errorf("ref = %+v, want the saved draft", s.ref)
	}
}

func TestSendWithDraft_NilEngineSendsPlain(t *testing.T) {
	s := &fakeSender{}
	if _, _, err := sendWithDraft(context.Background(), s, nil, "C1", "", "x", false); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(s.calls) != "[plain C1]" {
		t.Errorf("sends = %v", s.calls)
	}
}

func TestCoreDraft(t *testing.T) {
	var blocks slack.Blocks
	if err := json.Unmarshal([]byte(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"user","user_id":"U1"},{"type":"text","text":" hi"}]}]}]`), &blocks); err != nil {
		t.Fatal(err)
	}
	d := attached("Dr1", "C1", "9.0", "1795617850.177844")
	d.Blocks = blocks
	d.DateScheduled = 1800000000
	d.FileIDs = []string{"F1", "F2"}
	got := coreDraft("T1", d)
	want := core.Draft{
		ID: "Dr1", Key: core.DraftKey{TeamID: "T1", ChannelID: "C1", ThreadTS: "9.0"},
		Text: "<@U1> hi", Updated: time.Unix(1795617850, 0), Scheduled: time.Unix(1800000000, 0), FileCount: 2,
	}
	if got.ID != want.ID || got.Key != want.Key || got.Text != want.Text || !got.Updated.Equal(want.Updated) ||
		!got.Scheduled.Equal(want.Scheduled) || got.FileCount != 2 || got.Saved {
		t.Errorf("coreDraft = %+v, want %+v", got, want)
	}
	unaddressed := slackclient.Draft{ID: "Dr2", IsFromComposer: true}
	if g := coreDraft("T1", unaddressed); g.Key.ChannelID != "" || !g.Saved {
		t.Errorf("unaddressed = %+v", g)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./cmd/slk -run 'DraftEngine|SendWithDraft|CoreDraft' -count=1`
Expected: FAIL to compile: `undefined: newDraftEngine`.

- [ ] **Step 3: Implement**

`cmd/slk/drafts.go`:

```go
package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/export"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

// draftAPI is the part of *slackclient.Client the draft engine uses.
type draftAPI interface {
	ListDrafts(ctx context.Context) ([]slackclient.Draft, error)
	CreateDraft(ctx context.Context, w slackclient.DraftWrite) (slackclient.Draft, error)
	UpdateDraft(ctx context.Context, draftID string, w slackclient.DraftWrite) (slackclient.Draft, error)
	DeleteDraft(ctx context.Context, draftID string) error
}

// draftDest is a conversation within the engine's workspace.
type draftDest struct{ channelID, threadTS string }

// draftEntry is what the engine knows of a conversation's attached draft.
type draftEntry struct {
	id            string
	lastUpdatedTS string
	// handsOff: scheduled or with files; slk never writes it.
	handsOff bool
}

// draftEngine is one workspace's side of draft sync: which attached
// draft each conversation has, whether to create or update it, and one
// Slack call at a time per conversation, so saves and the send that
// marks a draft sent never overlap or reorder.
type draftEngine struct {
	api    draftAPI
	teamID string
	now    func() time.Time

	mu    sync.Mutex
	index map[draftDest]draftEntry
	locks map[draftDest]*sync.Mutex
	busy  map[draftDest]int
}

func newDraftEngine(api draftAPI, teamID string) *draftEngine {
	return &draftEngine{
		api: api, teamID: teamID, now: time.Now,
		index: map[draftDest]draftEntry{},
		locks: map[draftDest]*sync.Mutex{},
		busy:  map[draftDest]int{},
	}
}

// lock takes dest's lock and returns its release.
func (e *draftEngine) lock(dest draftDest) func() {
	e.mu.Lock()
	l, ok := e.locks[dest]
	if !ok {
		l = &sync.Mutex{}
		e.locks[dest] = l
	}
	e.busy[dest]++
	e.mu.Unlock()
	l.Lock()
	return func() {
		l.Unlock()
		e.mu.Lock()
		if e.busy[dest]--; e.busy[dest] == 0 {
			delete(e.busy, dest)
		}
		e.mu.Unlock()
	}
}

func (e *draftEngine) entry(dest draftDest) (draftEntry, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	ent, ok := e.index[dest]
	return ent, ok
}

func (e *draftEngine) put(dest draftDest, d slackclient.Draft) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.index[dest] = draftEntry{id: d.ID, lastUpdatedTS: d.LastUpdatedTS, handsOff: !d.Writable()}
}

func (e *draftEngine) drop(dest draftDest) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.index, dest)
}

func destOf(d slackclient.Draft) draftDest {
	return draftDest{d.Destinations[0].ChannelID, d.Destinations[0].ThreadTS}
}

// reload lists the drafts and rebuilds the index. A conversation another
// caller holds the lock of keeps what the engine knew: its call in
// flight may postdate the list. own is the caller's conversation, whose
// entry the list does replace.
func (e *draftEngine) reload(ctx context.Context, own *draftDest) ([]slackclient.Draft, error) {
	drafts, err := e.api.ListDrafts(ctx)
	if err != nil {
		return nil, err
	}
	next := map[draftDest]draftEntry{}
	for _, d := range drafts {
		if !d.Attached() || d.IsDeleted || d.IsSent {
			continue
		}
		next[destOf(d)] = draftEntry{id: d.ID, lastUpdatedTS: d.LastUpdatedTS, handsOff: !d.Writable()}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for dest := range e.busy {
		if own != nil && dest == *own {
			continue
		}
		if old, ok := e.index[dest]; ok {
			next[dest] = old
		} else {
			delete(next, dest)
		}
	}
	e.index = next
	return drafts, nil
}

// Load lists the workspace's active drafts for the UI.
func (e *draftEngine) Load(ctx context.Context) ([]core.Draft, error) {
	drafts, err := e.reload(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := make([]core.Draft, 0, len(drafts))
	for _, d := range drafts {
		if d.IsDeleted || d.IsSent {
			continue
		}
		out = append(out, coreDraft(e.teamID, d))
	}
	return out, nil
}

// Save makes text dest's attached draft. slk's text wins every conflict
// (spec, Conflicts).
func (e *draftEngine) Save(ctx context.Context, dest draftDest, text string, edited time.Time) (core.DraftSaveResult, error) {
	unlock := e.lock(dest)
	defer unlock()
	w := slackclient.DraftWrite{ChannelID: dest.channelID, ThreadTS: dest.threadTS, Text: text, Edited: edited}
	ent, ok := e.entry(dest)
	switch {
	case ok && ent.handsOff:
		return core.DraftSaveResult{}, core.ErrDraftHandsOff
	case ok:
		return e.update(ctx, dest, ent.id, w)
	}
	return e.create(ctx, dest, w, true)
}

// create makes dest's draft. adopt allows the one recovery from
// attached_draft_exists: another device made the draft, so list and
// overwrite it. Without adopt that answer means a slot slk cannot use.
func (e *draftEngine) create(ctx context.Context, dest draftDest, w slackclient.DraftWrite, adopt bool) (core.DraftSaveResult, error) {
	d, err := e.api.CreateDraft(ctx, w)
	if err == nil {
		e.put(dest, d)
		return core.DraftSaveResult{Draft: coreDraft(e.teamID, d)}, nil
	}
	if slackclient.DraftRejectionCode(err) != "attached_draft_exists" {
		return core.DraftSaveResult{}, err
	}
	if !adopt {
		return core.DraftSaveResult{}, core.ErrDraftHandsOff
	}
	if _, err := e.reload(ctx, &dest); err != nil {
		return core.DraftSaveResult{}, err
	}
	ent, ok := e.entry(dest)
	if !ok || ent.handsOff {
		// A deleted draft can hold the slot without being listed.
		return core.DraftSaveResult{}, core.ErrDraftHandsOff
	}
	res, err := e.update(ctx, dest, ent.id, w)
	if err == nil {
		res.Replaced = true
	}
	return res, err
}

func (e *draftEngine) update(ctx context.Context, dest draftDest, id string, w slackclient.DraftWrite) (core.DraftSaveResult, error) {
	d, err := e.api.UpdateDraft(ctx, id, w)
	replaced := false
	if slackclient.DraftRejectionCode(err) == "draft_has_conflict" {
		// Another device edited it later than this text: slk's wins.
		w.Edited = e.now()
		d, err = e.api.UpdateDraft(ctx, id, w)
		replaced = true
	}
	switch slackclient.DraftRejectionCode(err) {
	case "":
		if err != nil {
			return core.DraftSaveResult{}, err
		}
	case "draft_not_found", "draft_already_deleted", "draft_already_sent":
		e.drop(dest)
		return e.create(ctx, dest, w, false)
	case "draft_update_invalid":
		e.drop(dest)
		return core.DraftSaveResult{}, core.ErrDraftHandsOff
	default:
		return core.DraftSaveResult{}, err
	}
	e.put(dest, d)
	return core.DraftSaveResult{Draft: coreDraft(e.teamID, d), Replaced: replaced}, nil
}

// Discard deletes dest's draft if the engine knows a writable one. A
// refusal means it is gone already; a transport error is returned so
// the caller retries.
func (e *draftEngine) Discard(ctx context.Context, dest draftDest) error {
	unlock := e.lock(dest)
	defer unlock()
	ent, ok := e.entry(dest)
	if !ok || ent.handsOff {
		return nil
	}
	if err := e.api.DeleteDraft(ctx, ent.id); err != nil && slackclient.DraftRejectionCode(err) == "" {
		return err
	}
	e.drop(dest)
	return nil
}

// Delete deletes one draft by id, any kind (the Drafts list).
func (e *draftEngine) Delete(ctx context.Context, id string) error {
	var dest *draftDest
	e.mu.Lock()
	for d, ent := range e.index {
		if ent.id == id {
			d := d
			dest = &d
			break
		}
	}
	e.mu.Unlock()
	if dest != nil {
		unlock := e.lock(*dest)
		defer unlock()
	}
	if err := e.api.DeleteDraft(ctx, id); err != nil {
		return err
	}
	if dest != nil {
		e.drop(*dest)
	}
	return nil
}

// beginSend takes dest's lock, waiting out a save in flight, and returns
// its writable draft (nil when none). done releases the lock; done(true)
// after the post succeeded with the ref forgets the draft.
func (e *draftEngine) beginSend(dest draftDest) (*slackclient.DraftRef, func(sent bool)) {
	unlock := e.lock(dest)
	var ref *slackclient.DraftRef
	if ent, ok := e.entry(dest); ok && !ent.handsOff {
		ref = &slackclient.DraftRef{ID: ent.id, LastUpdatedTS: ent.lastUpdatedTS}
	}
	return ref, func(sent bool) {
		if sent && ref != nil {
			e.drop(dest)
		}
		unlock()
	}
}

// draftSender is the part of *slackclient.Client sendWithDraft uses.
type draftSender interface {
	SendMessage(ctx context.Context, channelID, text string) (string, string, error)
	SendReply(ctx context.Context, channelID, threadTS, text string, broadcast bool) (string, string, error)
	SendDraft(ctx context.Context, channelID, threadTS, text string, broadcast bool, ref slackclient.DraftRef) (string, string, error)
}

// sendWithDraft posts text to the conversation (a thread when threadTS is
// set), marking its draft sent with the post. When Slack refuses the
// draft fields (draft_*: nothing was posted) it posts plainly and deletes
// the draft. drafts nil (sync off) is a plain send.
func sendWithDraft(ctx context.Context, client draftSender, drafts *draftEngine, channelID, threadTS, text string, broadcast bool) (string, string, error) {
	plain := func() (string, string, error) {
		if threadTS == "" {
			return client.SendMessage(ctx, channelID, text)
		}
		return client.SendReply(ctx, channelID, threadTS, text, broadcast)
	}
	if drafts == nil {
		return plain()
	}
	dest := draftDest{channelID, threadTS}
	ref, done := drafts.beginSend(dest)
	if ref == nil {
		ts, mr, err := plain()
		done(false)
		return ts, mr, err
	}
	ts, mr, err := client.SendDraft(ctx, channelID, threadTS, text, broadcast, *ref)
	var refusal slack.SlackErrorResponse
	if errors.As(err, &refusal) && strings.HasPrefix(refusal.Err, "draft_") {
		debuglog.General("chat.postMessage: draft refused (%s), sending without it", refusal.Err)
		ts, mr, err = plain()
		if err == nil {
			_ = drafts.api.DeleteDraft(ctx, ref.ID)
			drafts.drop(dest)
		}
		done(false)
		return ts, mr, err
	}
	done(err == nil)
	return ts, mr, err
}

// coreDraft is d as the UI sees it. Text is the composer markdown the
// edit path (E) builds from the same blocks, mentions in wire form.
func coreDraft(teamID string, d slackclient.Draft) core.Draft {
	out := core.Draft{
		ID:        d.ID,
		Key:       core.DraftKey{TeamID: teamID},
		Text:      messages.EditSource(messages.MessageItem{Blocks: blockkit.Parse(d.Blocks)}),
		FileCount: len(d.FileIDs),
		Saved:     d.IsFromComposer,
	}
	if len(d.Destinations) == 1 {
		out.Key.ChannelID = d.Destinations[0].ChannelID
		out.Key.ThreadTS = d.Destinations[0].ThreadTS
	}
	if t, ok := export.TimeFromTS(d.LastUpdatedTS); ok {
		out.Updated = t
	}
	if d.DateScheduled != 0 {
		out.Scheduled = time.Unix(d.DateScheduled, 0)
	}
	return out
}
```

Check: `export.TimeFromTS(ts string) (time.Time, bool)` — confirm the signature in `internal/export/window.go` and that it returns whole seconds (the test expects `time.Unix(1795617850, 0)`). `messages.MessageItem.Blocks` must be `[]blockkit.Block`; if it is another field name, use the one `EditSource` iterates.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./cmd/slk -run 'DraftEngine|SendWithDraft|CoreDraft' -count=1 -race -v`
Expected: PASS, no race reports.

- [ ] **Step 5: Commit**

```bash
gofmt -l cmd/slk
git add cmd/slk/drafts.go cmd/slk/drafts_test.go
git commit -m "feat(slk): per-workspace draft engine and sendWithDraft"
```

---

### Task 5: Composer slot access

**Files:**
- Modify: `internal/ui/compose/model.go` (add after `OfferDraft`, line ~1041)
- Create: `internal/ui/compose/slots_test.go`

**Interfaces:**
- Produces (package `compose`):
  - `type Slot struct { TeamID, ChannelID, ThreadTS, Text string; Visible bool }`
  - `func (m *Model) Slots() []Slot`
  - `func (m *Model) SlotText(teamID, channelID, threadTS string) (text string, visible bool)`
  - `func (m *Model) ApplyRemoteDraft(teamID, channelID, threadTS, text string) bool`
  - `func (m *Model) ClearSlot(teamID, channelID, threadTS string)`

- [ ] **Step 1: Write the failing tests**

`internal/ui/compose/slots_test.go`:

```go
package compose

import (
	"sort"
	"testing"

	"github.com/gammons/slk/internal/core"
)

func slotSet(m *Model) map[string]Slot {
	out := map[string]Slot{}
	for _, s := range m.Slots() {
		out[s.ChannelID+"/"+s.ThreadTS] = s
	}
	return out
}

func TestSlots_VisibleEvenEmptyAndEveryStoredOne(t *testing.T) {
	m := New("general")
	m.SetDraftContext("T1", "C1", "")
	m.SetValue("one")
	m.SetDraftContext("T1", "C2", "")
	got := slotSet(&m)
	if len(got) != 2 {
		keys := make([]string, 0, len(got))
		for k := range got {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		t.Fatalf("slots = %v, want C1 and C2", keys)
	}
	if s := got["C1/"]; s.Text != "one" || s.Visible || s.TeamID != "T1" {
		t.Errorf("C1 = %+v", s)
	}
	if s := got["C2/"]; s.Text != "" || !s.Visible {
		t.Errorf("C2 = %+v", s)
	}
}

func TestSlots_DetachedComposerHasNoVisibleSlot(t *testing.T) {
	m := New("general")
	if len(m.Slots()) != 0 {
		t.Errorf("slots = %+v, want none", m.Slots())
	}
}

func TestSlotText(t *testing.T) {
	m := New("general")
	m.SetDraftContext("T1", "C1", "")
	m.SetValue("one")
	m.SetDraftContext("T1", "C2", "")
	m.SetValue("two")
	if txt, vis := m.SlotText("T1", "C1", ""); txt != "one" || vis {
		t.Errorf("C1 = %q, %v", txt, vis)
	}
	if txt, vis := m.SlotText("T1", "C2", ""); txt != "two" || !vis {
		t.Errorf("C2 = %q, %v", txt, vis)
	}
	if txt, vis := m.SlotText("T1", "C9", ""); txt != "" || vis {
		t.Errorf("C9 = %q, %v", txt, vis)
	}
}

func TestApplyRemoteDraft(t *testing.T) {
	m := New("general")
	m.SetDraftContext("T1", "C1", "")
	if !m.ApplyRemoteDraft("T1", "C1", "", "theirs") || m.Value() != "theirs" {
		t.Errorf("empty visible: value = %q", m.Value())
	}
	if m.ApplyRemoteDraft("T1", "C1", "", "newer") || m.Value() != "theirs" {
		t.Errorf("non-empty visible must not be replaced: value = %q", m.Value())
	}
	if !m.ApplyRemoteDraft("T1", "C2", "", "stored") {
		t.Error("inactive slot not written")
	}
	m.SetDraftContext("T1", "C2", "")
	if m.Value() != "stored" {
		t.Errorf("restored C2 = %q", m.Value())
	}
	if !m.ApplyRemoteDraft("T1", "C1", "", "") {
		t.Error("clearing an inactive slot")
	}
	if txt, _ := m.SlotText("T1", "C1", ""); txt != "" {
		t.Errorf("C1 after clear = %q", txt)
	}
	if m.ApplyRemoteDraft("T1", "", "", "x") {
		t.Error("a slot without a channel is not a conversation")
	}
}

func TestApplyRemoteDraft_ClearKeepsAttachments(t *testing.T) {
	m := New("general")
	m.SetDraftContext("T1", "C1", "")
	m.SetValue("text")
	m.AddAttachment(core.PendingAttachment{Filename: "a.png"})
	m.SetDraftContext("T1", "C2", "")
	m.ApplyRemoteDraft("T1", "C1", "", "")
	m.SetDraftContext("T1", "C1", "")
	if m.Value() != "" || len(m.Attachments()) != 1 {
		t.Errorf("value %q, %d attachments", m.Value(), len(m.Attachments()))
	}
}

func TestClearSlot(t *testing.T) {
	m := New("general")
	m.SetDraftContext("T1", "C1", "")
	m.SetValue("one")
	m.SetDraftContext("T1", "C2", "")
	m.SetValue("two")
	m.ClearSlot("T1", "C2", "")
	m.ClearSlot("T1", "C1", "")
	if m.Value() != "" {
		t.Errorf("visible = %q", m.Value())
	}
	if txt, _ := m.SlotText("T1", "C1", ""); txt != "" {
		t.Errorf("C1 = %q", txt)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/ui/compose -run 'Slot|ApplyRemote|ClearSlot' -count=1`
Expected: FAIL to compile: `m.Slots undefined`.

- [ ] **Step 3: Implement** (in `model.go`, after `OfferDraft`)

```go
// Slot is one conversation's composer text, as draft sync reads it.
type Slot struct {
	TeamID, ChannelID, ThreadTS string
	Text                        string
	// Visible is the slot on screen (the composer is bound to it).
	Visible bool
}

// Slots lists every conversation this composer holds: the bound one,
// even empty, then each stored one. A detached composer has no bound slot.
func (m *Model) Slots() []Slot {
	out := make([]Slot, 0, len(m.drafts)+1)
	if k := m.activeKey; k.channelID != "" {
		out = append(out, Slot{k.teamID, k.channelID, k.threadTS, m.input.Value(), true})
	}
	for k, d := range m.drafts {
		out = append(out, Slot{k.teamID, k.channelID, k.threadTS, d.text, false})
	}
	return out
}

// SlotText returns the text held for a conversation and whether it is
// the visible one. A conversation with nothing stored reads "".
func (m *Model) SlotText(teamID, channelID, threadTS string) (string, bool) {
	key := draftKey{teamID: teamID, channelID: channelID, threadTS: threadTS}
	if channelID != "" && key == m.activeKey {
		return m.input.Value(), true
	}
	return m.drafts[key].text, false
}

// ApplyRemoteDraft writes text, the conversation's draft from Slack, into
// its slot. A stored slot is overwritten (text "" clears its text and
// keeps its attachments); the visible composer is written only when it
// is empty, so text on screen is never replaced. The caller decides
// whether the slot may change at all (draft sync compares it with what
// Slack last had). Reports whether it wrote.
func (m *Model) ApplyRemoteDraft(teamID, channelID, threadTS, text string) bool {
	if channelID == "" {
		return false
	}
	key := draftKey{teamID: teamID, channelID: channelID, threadTS: threadTS}
	if key == m.activeKey {
		if text == "" || m.input.Value() != "" {
			return false
		}
		m.input.SetValue(text)
		m.autoGrow()
		m.dirty()
		return true
	}
	d := m.drafts[key]
	d.text = text
	if d.isEmpty() {
		delete(m.drafts, key)
	} else {
		m.drafts[key] = d
	}
	return true
}

// ClearSlot empties a conversation's text, visible or not, keeping its
// attachments. For a draft the user deleted from the Drafts list.
func (m *Model) ClearSlot(teamID, channelID, threadTS string) {
	if channelID == "" {
		return
	}
	key := draftKey{teamID: teamID, channelID: channelID, threadTS: threadTS}
	if key == m.activeKey {
		m.closePickers()
		m.input.Reset()
		m.input.SetHeight(1)
		m.dirty()
		return
	}
	m.ApplyRemoteDraft(teamID, channelID, threadTS, "")
}
```

- [ ] **Step 4: Run the package's tests**

Run: `go test ./internal/ui/compose -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/ui/compose
git add internal/ui/compose
git commit -m "feat(compose): slot access for draft sync"
```

---

### Task 6: Draft sync in the App

**Files:**
- Create: `internal/ui/drafts.go`
- Create: `internal/ui/reducer_drafts.go`
- Create: `internal/ui/drafts_test.go`
- Modify: `internal/ui/app.go` (fields near `commandSvc` line ~556 and `marksOverlay` line ~613; init near line ~993; `reduceDrafts` in the `dispatchReducers` list line ~1172; `SetDraftService` next to `SetCommandService` line ~3543)
- Modify: `internal/ui/mode_insert.go` (the two send branches, lines ~249 and ~313)
- Modify: `internal/ui/services_helpers_test.go` (add `setDraftServiceForTest`)

**Interfaces:**
- Consumes: `core.DraftService`, `core.DraftKey`, `core.Draft`, `core.DraftChange`, `core.ErrDraftHandsOff` (Task 3); `compose.Slot` and the slot methods (Task 5).
- Produces (package `ui`):
  - `type DraftsStaleMsg struct{ TeamID string }` (exported; `cmd/slk` sends it)
  - `func (a *App) SetDraftService(s core.DraftService)` (nil: sync off)
  - `func (a *App) UnsyncedDrafts() []core.DraftChange`
  - unexported, used by Task 7: `a.drafts *draftSync` with `rows map[string][]core.Draft`; `a.draftSvc`; `a.composerFor(threadTS string) *compose.Model`; `a.loadDraftsCmd(teamID string) tea.Cmd`; `draftDeleteConfirmedMsg{teamID string; draft core.Draft}`; `draftDeletedMsg`; `(d *draftSync) removeRow(teamID, id string)`; `a.refreshDraftList()` (declared in Task 7, called here only through a nil-safe hook, see below)

- [ ] **Step 1: Add the test service setter** (`services_helpers_test.go`)

```go
func (a *App) setDraftServiceForTest(fns core.DraftServiceFuncs) {
	a.SetDraftService(core.NewDraftService(fns))
}
```

- [ ] **Step 2: Write the failing tests**

`internal/ui/drafts_test.go`:

```go
package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
)

type draftCall struct {
	op     string
	key    core.DraftKey
	text   string
	edited time.Time
}

// draftSyncApp is an App on T1/C1 with draft sync wired to a recording
// service and a clock the test moves. saveErr answers every Save.
// T1's drafts have been listed (empty), so the comparison runs.
func draftSyncApp(t *testing.T, saveErr error) (*App, *[]draftCall, *time.Time) {
	t.Helper()
	a := newTestApp(t, withActiveTeam("T1"), withActiveChannel("C1"))
	a.compose.SetDraftContext("T1", "C1", "")
	clock := time.Unix(1_800_000_000, 0)
	a.now = func() time.Time { return clock }
	var calls []draftCall
	a.setDraftServiceForTest(core.DraftServiceFuncs{
		Save: func(k core.DraftKey, text string, edited time.Time) (core.DraftSaveResult, error) {
			calls = append(calls, draftCall{"save", k, text, edited})
			if saveErr != nil {
				return core.DraftSaveResult{}, saveErr
			}
			return core.DraftSaveResult{Draft: core.Draft{ID: "Dr-" + k.ChannelID, Key: k, Text: text}}, nil
		},
		Discard: func(k core.DraftKey) error {
			calls = append(calls, draftCall{op: "discard", key: k})
			return nil
		},
		Delete: func(teamID, id string) error {
			calls = append(calls, draftCall{op: "delete " + id})
			return nil
		},
	})
	a.Update(draftsLoadedMsg{teamID: "T1"})
	return a, &calls, &clock
}

// tick runs one comparison at the App's clock, runs its saves and feeds
// their answers back through Update.
func tick(t *testing.T, a *App) {
	t.Helper()
	for _, c := range a.syncDrafts(a.now()) {
		a.Update(c())
	}
}

func ops(calls []draftCall) string {
	var s []string
	for _, c := range calls {
		s = append(s, fmt.Sprintf("%s %s/%s %q", c.op, c.key.ChannelID, c.key.ThreadTS, c.text))
	}
	return strings.Join(s, "; ")
}

func TestDraftSync_VisibleTextSavedOnceItStaysTheSame(t *testing.T) {
	a, calls, clock := draftSyncApp(t, nil)
	a.compose.SetValue("hel")
	tick(t, a)
	first := *clock
	a.compose.SetValue("hello")
	*clock = clock.Add(2 * time.Second)
	tick(t, a)
	if len(*calls) != 0 {
		t.Fatalf("saved while typing: %s", ops(*calls))
	}
	seen := *clock
	*clock = clock.Add(2 * time.Second)
	tick(t, a)
	if got := ops(*calls); got != `save C1/ "hello"` {
		t.Fatalf("calls = %s", got)
	}
	if !(*calls)[0].edited.Equal(seen) || seen.Equal(first) {
		t.Errorf("edited = %v, want %v (when the text was first seen)", (*calls)[0].edited, seen)
	}
	*clock = clock.Add(2 * time.Second)
	tick(t, a)
	if len(*calls) != 1 {
		t.Errorf("saved again without a change: %s", ops(*calls))
	}
}

func TestDraftSync_LeftSlotSavedAtTheNextTick(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	a.compose.SetValue("hello")
	a.compose.SetDraftContext("T1", "C2", "")
	tick(t, a)
	if got := ops(*calls); got != `save C1/ "hello"` {
		t.Errorf("calls = %s", got)
	}
}

func TestDraftSync_EmptiedSlotDiscards(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	a.compose.SetValue("hello")
	a.compose.SetDraftContext("T1", "C2", "")
	tick(t, a)
	a.compose.SetDraftContext("T1", "C1", "")
	a.compose.SetValue("")
	a.compose.SetDraftContext("T1", "C2", "") // C1 now stores nothing at all
	tick(t, a)
	if got := ops(*calls); got != `save C1/ "hello"; discard C1/ ""` {
		t.Errorf("calls = %s", got)
	}
}

func TestDraftSync_WhitespaceIsEmpty(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	a.compose.SetValue("  \n ")
	a.compose.SetDraftContext("T1", "C2", "")
	tick(t, a)
	if len(*calls) != 0 {
		t.Errorf("calls = %s, want none", ops(*calls))
	}
}

func TestDraftSync_NothingBeforeFirstList(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	a.compose.SetDraftContext("T2", "C5", "") // T2 never listed
	a.compose.SetValue("x")
	a.compose.SetDraftContext("", "C6", "") // team not known yet
	a.compose.SetValue("y")
	a.compose.SetDraftContext("", "", "")
	tick(t, a)
	tick(t, a)
	if len(*calls) != 0 {
		t.Errorf("calls = %s, want none", ops(*calls))
	}
}

func TestDraftSync_EditModeTextIsNotADraft(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	a.editing.Begin("C1", "1.0", PanelMessages, "")
	a.compose.SetValue("text of a sent message")
	tick(t, a)
	tick(t, a)
	if len(*calls) != 0 {
		t.Errorf("calls = %s, want none", ops(*calls))
	}
}

func TestDraftSync_SendIsNotADiscardOrAResave(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	a.compose.SetValue("hello")
	tick(t, a)
	tick(t, a) // saved
	a.SetMode(ModeInsert)
	a.compose.Focus()
	a.Update(keyCode(tea.KeyEnter))
	tick(t, a)
	tick(t, a)
	if got := ops(*calls); got != `save C1/ "hello"` {
		t.Errorf("calls = %s, want only the save before the send", got)
	}
}

// A save answer that lands after the send must not bring the draft back.
func TestDraftSync_LateSaveAnswerAfterSendIsIgnored(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	a.compose.SetValue("hello")
	tick(t, a)
	saves := a.syncDrafts(a.now()) // in flight
	a.noteDraftSent(core.DraftKey{TeamID: "T1", ChannelID: "C1"})
	a.compose.Reset()
	for _, c := range saves {
		a.Update(c())
	}
	tick(t, a)
	tick(t, a)
	if got := ops(*calls); got != `save C1/ "hello"` {
		t.Errorf("calls = %s", got)
	}
}

func TestDraftSync_HandsOffToastsOnceAndStops(t *testing.T) {
	a, calls, _ := draftSyncApp(t, core.ErrDraftHandsOff)
	a.compose.SetValue("one")
	a.compose.SetDraftContext("T1", "C2", "")
	tick(t, a)
	if !strings.Contains(statusbarText(a), "won't overwrite") {
		t.Errorf("toast = %q", statusbarText(a))
	}
	a.compose.SetDraftContext("T1", "C1", "")
	a.compose.SetValue("two")
	a.compose.SetDraftContext("T1", "C2", "")
	tick(t, a)
	if len(*calls) != 1 {
		t.Errorf("calls = %s, want one", ops(*calls))
	}
}

func TestDraftSync_TransientErrorBacksOff(t *testing.T) {
	a, calls, clock := draftSyncApp(t, errors.New("timeout"))
	a.compose.SetValue("one")
	a.compose.SetDraftContext("T1", "C2", "")
	tick(t, a)
	tick(t, a)
	if len(*calls) != 1 {
		t.Fatalf("retried at once: %s", ops(*calls))
	}
	*clock = clock.Add(5 * time.Second)
	tick(t, a)
	if len(*calls) != 2 {
		t.Errorf("no retry after the backoff: %s", ops(*calls))
	}
}

func TestDraftsLoaded_RestoresWhereNothingWasTyped(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	a.Update(draftsLoadedMsg{teamID: "T1", drafts: []core.Draft{
		{ID: "Dr1", Key: core.DraftKey{TeamID: "T1", ChannelID: "C1"}, Text: "one"},
		{ID: "Dr2", Key: core.DraftKey{TeamID: "T1", ChannelID: "C2"}, Text: "two"},
		{ID: "Dr3", Key: core.DraftKey{TeamID: "T1", ChannelID: "C1", ThreadTS: "10.0"}, Text: "three"},
		{ID: "Dr4", Key: core.DraftKey{TeamID: "T1", ChannelID: "C3"}, Text: "saved", Saved: true},
	}})
	if a.compose.Value() != "one" {
		t.Errorf("visible = %q", a.compose.Value())
	}
	if txt, _ := a.compose.SlotText("T1", "C2", ""); txt != "two" {
		t.Errorf("C2 = %q", txt)
	}
	if txt, _ := a.threadCompose.SlotText("T1", "C1", "10.0"); txt != "three" {
		t.Errorf("thread = %q", txt)
	}
	if txt, _ := a.compose.SlotText("T1", "C3", ""); txt != "" {
		t.Errorf("a saved draft was restored: %q", txt)
	}
	tick(t, a)
	tick(t, a)
	if len(*calls) != 0 {
		t.Errorf("restores were saved back: %s", ops(*calls))
	}
}

func TestDraftsLoaded_NeverReplacesTypedText(t *testing.T) {
	a, _, _ := draftSyncApp(t, nil)
	a.compose.SetValue("mine")
	a.compose.SetDraftContext("T1", "C2", "")
	a.compose.SetValue("mine too")
	a.Update(draftsLoadedMsg{teamID: "T1", drafts: []core.Draft{
		{ID: "Dr1", Key: core.DraftKey{TeamID: "T1", ChannelID: "C1"}, Text: "theirs"},
		{ID: "Dr2", Key: core.DraftKey{TeamID: "T1", ChannelID: "C2"}, Text: "theirs"},
	}})
	if txt, _ := a.compose.SlotText("T1", "C1", ""); txt != "mine" {
		t.Errorf("C1 = %q", txt)
	}
	if a.compose.Value() != "mine too" {
		t.Errorf("visible = %q", a.compose.Value())
	}
}

func TestDraftsLoaded_VanishedDraftClearsAnUnchangedStoredSlot(t *testing.T) {
	a, _, _ := draftSyncApp(t, nil)
	k := core.DraftKey{TeamID: "T1", ChannelID: "C2"}
	a.Update(draftsLoadedMsg{teamID: "T1", drafts: []core.Draft{{ID: "Dr2", Key: k, Text: "two"}}})
	a.Update(draftsLoadedMsg{teamID: "T1"}) // sent from the phone
	if txt, _ := a.compose.SlotText("T1", "C2", ""); txt != "" {
		t.Errorf("C2 = %q, want cleared", txt)
	}
}

func TestUnsyncedDrafts(t *testing.T) {
	a, _, _ := draftSyncApp(t, nil)
	a.compose.SetValue("typed just before quitting")
	got := a.UnsyncedDrafts()
	if len(got) != 1 || got[0].Key != (core.DraftKey{TeamID: "T1", ChannelID: "C1"}) || got[0].Text != "typed just before quitting" {
		t.Errorf("UnsyncedDrafts = %+v", got)
	}
}

func TestDraftTick_ReschedulesItself(t *testing.T) {
	a, _, _ := draftSyncApp(t, nil)
	if _, cmd := a.Update(draftTickMsg{}); cmd == nil {
		t.Error("tick returned no command")
	}
}

func TestDraftSync_OffDoesNothing(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	a.compose.SetDraftContext("T1", "C1", "")
	a.compose.SetValue("x")
	if cmds := a.syncDrafts(a.now()); len(cmds) != 0 || a.UnsyncedDrafts() != nil {
		t.Error("sync ran with no draft service")
	}
	if _, cmd := a.Update(DraftsStaleMsg{TeamID: "T1"}); cmd != nil {
		t.Error("loaded drafts with no draft service")
	}
}
```

- [ ] **Step 3: Run to verify they fail**

Run: `go test ./internal/ui -run 'DraftSync|DraftsLoaded|UnsyncedDrafts|DraftTick' -count=1`
Expected: FAIL to compile: `undefined: draftsLoadedMsg`.

- [ ] **Step 4: Implement `internal/ui/drafts.go`**

```go
// internal/ui/drafts.go
//
// Draft sync: the composer's text as Slack's own drafts (spec:
// docs/superpowers/specs/2026-11-25-slack-drafts-sync-design.md). Every
// draftTickInterval the App compares each composer slot with the text
// Slack last had from it and saves what changed, through DraftService.
// Comparing, rather than hooking each way text gets into a composer
// (keys, paste, the external editor, emoji, /msg), catches them all.
package ui

import (
	"errors"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/ui/compose"
)

// draftTickInterval is how often the comparison runs. The visible slot is
// saved once its text stays the same for a whole interval, 2-4 s after
// the last keystroke. A var so tests may shorten it.
var draftTickInterval = 2 * time.Second

// draftRetryMax caps the backoff of a save that keeps failing.
const draftRetryMax = time.Minute

// DraftsStaleMsg asks the App to reload a workspace's drafts. cmd/slk
// sends it on every connect.
type DraftsStaleMsg struct{ TeamID string }

type (
	draftTickMsg    struct{}
	draftsLoadedMsg struct {
		teamID string
		drafts []core.Draft
		err    error
	}
	// draftSavedMsg answers a Save (text != "") or a Discard (text "").
	draftSavedMsg struct {
		key  core.DraftKey
		text string
		gen  int
		res  core.DraftSaveResult
		err  error
	}
	draftDeleteConfirmedMsg struct {
		teamID string
		draft  core.Draft
	}
	draftDeletedMsg struct {
		teamID string
		draft  core.Draft
		err    error
	}
)

type draftObserved struct {
	text string
	at   time.Time
}

type draftRetry struct {
	n  int
	at time.Time
}

// draftSync is the App's draft sync state. Text is composer form
// (mentions as @names).
type draftSync struct {
	ticking bool
	// loaded: the workspace's drafts were listed at least once. No slot
	// of a workspace is compared before, so slk never creates a draft
	// Slack already has.
	loaded map[string]bool
	// synced is the text Slack last had from slk, per slot.
	synced map[core.DraftKey]string
	// observed is the text the last comparison saw and when it first
	// saw it: the debounce, and the edit time sent to Slack.
	observed map[core.DraftKey]draftObserved
	inflight map[core.DraftKey]string
	// gen is bumped by a send and a delete, so a save answer that was
	// in flight across them is ignored.
	gen   map[core.DraftKey]int
	retry map[core.DraftKey]draftRetry
	// handsOff: Slack's draft here is one slk won't overwrite; the slot
	// is left alone until a list shows a writable draft there.
	handsOff map[core.DraftKey]bool
	// rows is each workspace's active drafts, newest first (the list).
	rows map[string][]core.Draft
}

func newDraftSync() *draftSync {
	return &draftSync{
		loaded:   map[string]bool{},
		synced:   map[core.DraftKey]string{},
		observed: map[core.DraftKey]draftObserved{},
		inflight: map[core.DraftKey]string{},
		gen:      map[core.DraftKey]int{},
		retry:    map[core.DraftKey]draftRetry{},
		handsOff: map[core.DraftKey]bool{},
		rows:     map[string][]core.Draft{},
	}
}

// SetDraftService wires draft sync. nil turns it off: no draft calls,
// drafts stay in memory, :drafts refuses.
func (a *App) SetDraftService(s core.DraftService) { a.draftSvc = s }

// composerFor is the composer holding a conversation's slot: the thread
// composer for a thread, else the channel composer.
func (a *App) composerFor(threadTS string) *compose.Model {
	if threadTS != "" {
		return &a.threadCompose
	}
	return &a.compose
}

// draftText is a slot's text as sync compares it: whitespace-only is
// empty.
func draftText(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return s
}

// eachDraftSlot calls fn for every slot of both composers and for every
// slot Slack has text for that the composers no longer hold (emptied and
// left). The composer being edited (E) reports the draft held aside.
func (a *App) eachDraftSlot(fn func(key core.DraftKey, text string, visible bool)) {
	seen := map[core.DraftKey]bool{}
	for _, s := range append(a.compose.Slots(), a.threadCompose.Slots()...) {
		key := core.DraftKey{TeamID: s.TeamID, ChannelID: s.ChannelID, ThreadTS: s.ThreadTS}
		text := s.Text
		if s.Visible && a.editing.IsActive() && (s.ThreadTS != "") == (a.editing.Panel() == PanelThread) {
			text = a.editing.StashedDraft()
		}
		seen[key] = true
		fn(key, draftText(text), s.Visible)
	}
	for key := range a.drafts.synced {
		if !seen[key] {
			fn(key, "", false)
		}
	}
}

// syncable reports a slot the comparison may act on.
func (d *draftSync) syncable(key core.DraftKey) bool {
	return key.TeamID != "" && d.loaded[key.TeamID] && !d.handsOff[key]
}

// syncDrafts runs one comparison at now and returns the saves to make.
func (a *App) syncDrafts(now time.Time) []tea.Cmd {
	if a.draftSvc == nil {
		return nil
	}
	d := a.drafts
	var cmds []tea.Cmd
	a.eachDraftSlot(func(key core.DraftKey, text string, visible bool) {
		if !d.syncable(key) {
			return
		}
		if text == d.synced[key] {
			delete(d.observed, key)
			return
		}
		if _, busy := d.inflight[key]; busy {
			return
		}
		if r, ok := d.retry[key]; ok && now.Before(r.at) {
			return
		}
		obs, ok := d.observed[key]
		if !ok || obs.text != text {
			obs = draftObserved{text: text, at: now}
			d.observed[key] = obs
			if visible {
				return // still being typed: wait for a whole interval
			}
		}
		d.inflight[key] = text
		cmds = append(cmds, a.saveDraftCmd(key, text, obs.at))
	})
	return cmds
}

func (a *App) saveDraftCmd(key core.DraftKey, text string, edited time.Time) tea.Cmd {
	svc, gen := a.draftSvc, a.drafts.gen[key]
	wire := ""
	if text != "" {
		wire = a.composerFor(key.ThreadTS).TranslateMentionsForSend(text)
	}
	return func() tea.Msg {
		if text == "" {
			return draftSavedMsg{key: key, gen: gen, err: svc.Discard(key)}
		}
		res, err := svc.Save(key, wire, edited)
		return draftSavedMsg{key: key, text: text, gen: gen, res: res, err: err}
	}
}

// UnsyncedDrafts is every slot whose text Slack does not have, for the
// save on quit. Text is wire form; "" discards.
func (a *App) UnsyncedDrafts() []core.DraftChange {
	if a.draftSvc == nil {
		return nil
	}
	d, now := a.drafts, a.now()
	var out []core.DraftChange
	a.eachDraftSlot(func(key core.DraftKey, text string, _ bool) {
		if !d.syncable(key) || text == d.synced[key] {
			return
		}
		edited := now
		if obs, ok := d.observed[key]; ok && obs.text == text {
			edited = obs.at
		}
		wire := ""
		if text != "" {
			wire = a.composerFor(key.ThreadTS).TranslateMentionsForSend(text)
		}
		out = append(out, core.DraftChange{Key: key, Text: wire, Edited: edited})
	})
	return out
}

// noteDraftSent records that the composer of key was just sent from: the
// send marks the draft sent, so its now-empty slot is not a discard, and
// a save answer in flight is ignored.
func (a *App) noteDraftSent(key core.DraftKey) {
	d := a.drafts
	d.gen[key]++
	delete(d.synced, key)
	delete(d.observed, key)
	d.removeRowsFor(key)
}

func (a *App) startDraftTick() tea.Cmd {
	if a.drafts.ticking {
		return nil
	}
	a.drafts.ticking = true
	return draftTickCmd()
}

func draftTickCmd() tea.Cmd {
	return tea.Tick(draftTickInterval, func(time.Time) tea.Msg { return draftTickMsg{} })
}

func (a *App) reduceDraftTick() tea.Cmd {
	cmds := a.syncDrafts(a.now())
	return tea.Batch(append(cmds, draftTickCmd())...)
}

func (a *App) loadDraftsCmd(teamID string) tea.Cmd {
	svc := a.draftSvc
	if svc == nil || teamID == "" {
		return nil
	}
	return func() tea.Msg {
		ds, err := svc.Load(teamID)
		return draftsLoadedMsg{teamID: teamID, drafts: ds, err: err}
	}
}

func (a *App) reduceDraftsLoaded(m draftsLoadedMsg) tea.Cmd {
	if m.err != nil {
		debuglog.General("drafts: load %s failed: %v", m.teamID, m.err)
		return nil
	}
	d := a.drafts
	d.loaded[m.teamID] = true
	d.rows[m.teamID] = sortDrafts(m.drafts)
	listed := map[core.DraftKey]bool{}
	for _, dr := range m.drafts {
		if !dr.Writable() {
			continue
		}
		listed[dr.Key] = true
		delete(d.handsOff, dr.Key)
		a.restoreRemoteDraft(dr)
	}
	for key, synced := range d.synced {
		if key.TeamID != m.teamID || listed[key] {
			continue
		}
		if _, busy := d.inflight[key]; busy {
			continue
		}
		c := a.composerFor(key.ThreadTS)
		cur, visible := c.SlotText(key.TeamID, key.ChannelID, key.ThreadTS)
		if visible || draftText(cur) != synced {
			continue
		}
		c.ApplyRemoteDraft(key.TeamID, key.ChannelID, key.ThreadTS, "")
		delete(d.synced, key)
		delete(d.observed, key)
	}
	a.draftRowsChanged()
	return a.startDraftTick()
}

// restoreRemoteDraft puts a writable draft from Slack into its slot when
// the user has typed nothing there since the last sync. The visible
// composer only takes it when empty (compose.ApplyRemoteDraft).
func (a *App) restoreRemoteDraft(dr core.Draft) {
	d, key := a.drafts, dr.Key
	if _, busy := d.inflight[key]; busy {
		return
	}
	c := a.composerFor(key.ThreadTS)
	text := draftText(c.MentionsForEdit(dr.Text))
	cur, visible := c.SlotText(key.TeamID, key.ChannelID, key.ThreadTS)
	if draftText(cur) != d.synced[key] {
		return // typed since: slk's text wins and is saved by the tick
	}
	if visible && a.editing.IsActive() && (key.ThreadTS != "") == (a.editing.Panel() == PanelThread) {
		return
	}
	if draftText(cur) == text || c.ApplyRemoteDraft(key.TeamID, key.ChannelID, key.ThreadTS, text) {
		d.synced[key] = text
		delete(d.observed, key)
	}
}

func (a *App) reduceDraftSaved(m draftSavedMsg) tea.Cmd {
	d := a.drafts
	delete(d.inflight, m.key)
	if m.gen != d.gen[m.key] {
		return nil // sent or deleted while this was in flight
	}
	switch {
	case m.err == nil:
		delete(d.retry, m.key)
		if m.text == "" {
			delete(d.synced, m.key)
			d.removeRowsFor(m.key)
		} else {
			d.synced[m.key] = m.text
			d.upsertRow(m.res.Draft)
		}
		a.draftRowsChanged()
		if m.res.Replaced {
			return toastWithClear(a, "Replaced a newer draft from another device", 3*time.Second)
		}
	case errors.Is(m.err, core.ErrDraftHandsOff):
		d.handsOff[m.key] = true
		return toastWithClear(a, "Slack already has a draft here that slk won't overwrite", 4*time.Second)
	default:
		r := d.retry[m.key]
		r.n++
		wait := min(draftTickInterval<<r.n, draftRetryMax)
		r.at = a.now().Add(wait)
		d.retry[m.key] = r
		debuglog.General("drafts: save %v failed (retry in %s): %v", m.key, wait, m.err)
	}
	return nil
}

// sortDrafts returns ds newest first.
func sortDrafts(ds []core.Draft) []core.Draft {
	out := slices.Clone(ds)
	slices.SortStableFunc(out, func(x, y core.Draft) int { return y.Updated.Compare(x.Updated) })
	return out
}

// upsertRow puts a saved writable draft into its workspace's rows,
// replacing the conversation's previous one.
func (d *draftSync) upsertRow(dr core.Draft) {
	if dr.ID == "" {
		return
	}
	rows := d.rows[dr.Key.TeamID]
	rows = slices.DeleteFunc(slices.Clone(rows), func(r core.Draft) bool {
		return r.ID == dr.ID || (r.Writable() && r.Key == dr.Key)
	})
	d.rows[dr.Key.TeamID] = sortDrafts(append(rows, dr))
}

// removeRowsFor drops a conversation's writable draft from the rows.
func (d *draftSync) removeRowsFor(key core.DraftKey) {
	d.rows[key.TeamID] = slices.DeleteFunc(slices.Clone(d.rows[key.TeamID]), func(r core.Draft) bool {
		return r.Writable() && r.Key == key
	})
}

func (d *draftSync) removeRow(teamID, id string) {
	d.rows[teamID] = slices.DeleteFunc(slices.Clone(d.rows[teamID]), func(r core.Draft) bool { return r.ID == id })
}

// draftRowsChanged redraws the Drafts list when it is open. Task 7
// gives it a body; until then the list does not exist.
func (a *App) draftRowsChanged() {}
```

`internal/ui/reducer_drafts.go`:

```go
// internal/ui/reducer_drafts.go
//
// Draft sync's messages (drafts.go): the reload a connect asks for, a
// list's arrival, the comparison tick, save answers, and the Drafts
// list's delete.
package ui

import tea "charm.land/bubbletea/v2"

var reduceDrafts reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case DraftsStaleMsg:
		return a.loadDraftsCmd(m.TeamID), true
	case draftsLoadedMsg:
		return a.reduceDraftsLoaded(m), true
	case draftTickMsg:
		return a.reduceDraftTick(), true
	case draftSavedMsg:
		return a.reduceDraftSaved(m), true
	}
	return nil, false
}
```

In `app.go`:
- App fields, next to `commandSvc`: `draftSvc core.DraftService` (with a comment: nil when draft sync is off) and `drafts *draftSync`.
- In `NewApp`'s struct literal, next to `marksOverlay: marks.New(),`: `drafts: newDraftSync(),`.
- In `dispatchReducers(...)`, after `reduceSlashCommand,`: `reduceDrafts,`.

In `mode_insert.go`, note the send at each reset (team: the composers are bound with `a.activeTeamID`):

```go
			// thread branch, replacing the reset and the two locals after it
				a.threadCompose.Reset()
				threadTS := a.threadPanel.ThreadTS()
				channelID := a.threadPanel.ChannelID()
				a.noteDraftSent(core.DraftKey{TeamID: a.activeTeamID, ChannelID: channelID, ThreadTS: threadTS})
```

```go
			// channel branch
			channelID := a.activeChannelID
			a.compose.Reset()
			a.noteDraftSent(core.DraftKey{TeamID: a.activeTeamID, ChannelID: channelID})
```

(Add the `core` import to `mode_insert.go` if missing.)

- [ ] **Step 5: Run the new tests, then the package**

Run: `go test ./internal/ui -run 'DraftSync|DraftsLoaded|UnsyncedDrafts|DraftTick' -count=1 -v`
Expected: PASS.

Run: `go test ./internal/ui -count=1`
Expected: PASS (`boundary_test.go` included: no forbidden imports).

- [ ] **Step 6: Commit**

```bash
gofmt -l internal/ui
git add internal/ui
git commit -m "feat(ui): sync composer drafts with Slack"
```

---

### Task 7: The `:drafts` list

**Files:**
- Create: `internal/ui/draftlist/draftlist.go`
- Create: `internal/ui/draftlist/draftlist_test.go`
- Create: `internal/ui/drafts_list.go`
- Create: `internal/ui/mode_drafts.go`
- Create: `internal/ui/drafts_list_test.go`
- Modify: `internal/ui/drafts.go` (give `draftRowsChanged` its body; remove the placeholder comment)
- Modify: `internal/ui/reducer_drafts.go` (two more cases)
- Modify: `internal/ui/mode.go` (`ModeDrafts` before `modeCount`; `IsModalOverlay`; `String` → `"DRAFTS"`)
- Modify: `internal/ui/mode_handlers.go` (`ModeDrafts: handleDraftsMode,`)
- Modify: `internal/ui/app.go` (field `draftList draftlist.Model` + `draftListItems []core.Draft`; init `draftList: draftlist.New(),`; `closeModal`: `case ModeDrafts: a.draftList.Close()`)
- Modify: `internal/ui/view_overlays.go` (composite after the marks overlay; add to `overlayActive`)
- Modify: `internal/ui/command.go` (`"drafts": cmdDrafts,`)
- Modify: `internal/ui/keys.go` (`DraftsList` field + keyless help entry)
- Modify: `internal/ui/quit_prompt_modals_test.go` (a `ModeDrafts` case)
- Modify: `internal/ui/golden_dialogs_test.go` (a `dialog_drafts` scenario) + new golden file

**Interfaces:**
- Consumes: Task 6's `a.drafts.rows`, `a.draftSvc`, `a.composerFor`, `a.loadDraftsCmd`, `draftDeleteConfirmedMsg`, `draftDeletedMsg`, `(*draftSync).removeRow`; `a.channels.Lookup`; `a.applyLocation`; `a.openConfirmPrompt`.
- Produces (package `draftlist`): `Row{Destination, Age, Marker, Preview string}`; `Action` (`None`, `Jump`, `Delete`); `New()`, `(*Model).SetRows`, `Open`, `Close`, `IsVisible`, `HandleKey(keyStr) (Action, int)`, `View(w, h)`, `ViewOverlay(w, h, bg)`.

- [ ] **Step 1: Write the widget's failing test**

`internal/ui/draftlist/draftlist_test.go`:

```go
package draftlist

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func rows() []Row {
	return []Row{
		{Destination: "#general", Age: "5m", Preview: "hello there"},
		{Destination: "#ops › thread", Age: "2h", Marker: "scheduled Dec 1 9:00 AM", Preview: "deploy"},
	}
}

func TestHandleKey(t *testing.T) {
	m := New()
	m.SetRows(rows())
	m.Open()
	if act, _ := m.HandleKey("j"); act != None {
		t.Errorf("j = %v", act)
	}
	if act, i := m.HandleKey("enter"); act != Jump || i != 1 {
		t.Errorf("enter = %v %d, want Jump 1", act, i)
	}
	m.HandleKey("k")
	if act, i := m.HandleKey("d"); act != Delete || i != 0 {
		t.Errorf("d = %v %d, want Delete 0", act, i)
	}
	m.HandleKey("esc")
	if m.IsVisible() {
		t.Error("esc did not close")
	}
}

func TestHandleKey_EmptyListActsOnNothing(t *testing.T) {
	m := New()
	m.Open()
	for _, k := range []string{"enter", "d"} {
		if act, _ := m.HandleKey(k); act != None {
			t.Errorf("%s on empty list = %v", k, act)
		}
	}
}

func TestView(t *testing.T) {
	m := New()
	m.SetRows(rows())
	m.Open()
	v := ansi.Strip(m.View(100, 30))
	for _, want := range []string{"Drafts", "#general", "5m", "hello there", "scheduled Dec 1 9:00 AM"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	m.SetRows(nil)
	if v := ansi.Strip(m.View(100, 30)); !strings.Contains(v, "No drafts") {
		t.Errorf("empty view:\n%s", v)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/ui/draftlist -count=1`
Expected: FAIL: package does not exist.

- [ ] **Step 3: Implement the widget**

`internal/ui/draftlist/draftlist.go` (same structure as `internal/ui/marks/marks.go`; read it first):

```go
// Package draftlist is the :drafts dialog: the active workspace's Slack
// drafts, newest first. Enter jumps to a draft's conversation, d
// deletes it. Drawn through internal/bubbles/chrome like the marks list.
package draftlist

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/muesli/reflow/truncate"

	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
)

// Row is one draft as the dialog draws it.
type Row struct {
	Destination string // "#general", "#general › thread", "alice"
	Age         string // "5m", "3h", "Jan 2"
	Marker      string // hands-off kind: "scheduled …", "2 files", "saved"
	Preview     string // one line of the draft's text
}

// Action is what a key asked of the highlighted row.
type Action int

const (
	None Action = iota
	Jump
	Delete
)

// Model is the dialog.
type Model struct {
	rows     []Row
	selected int
	visible  bool
}

var _ interface {
	chrome.Dialog
	chrome.Boxed
} = (*Model)(nil)

func New() Model { return Model{} }

// SetRows replaces the rows and clamps the selection.
func (m *Model) SetRows(rows []Row) {
	m.rows = rows
	m.selected = max(min(m.selected, len(rows)-1), 0)
}

// Open shows the dialog with the first row selected.
func (m *Model) Open() {
	m.visible = true
	m.selected = 0
}

func (m *Model) Close() { m.visible = false }

func (m Model) IsVisible() bool { return m.visible }

// HandleKey moves the selection or reports an action on the highlighted
// row with its index. Esc closes.
func (m *Model) HandleKey(keyStr string) (Action, int) {
	switch keyStr {
	case "esc":
		m.Close()
	case "down", "j", "ctrl+n":
		if m.selected < len(m.rows)-1 {
			m.selected++
		}
	case "up", "k", "ctrl+p":
		if m.selected > 0 {
			m.selected--
		}
	case "enter":
		if len(m.rows) > 0 {
			return Jump, m.selected
		}
	case "d", "delete", "backspace":
		if len(m.rows) > 0 {
			return Delete, m.selected
		}
	}
	return None, 0
}

const (
	maxVisibleRows = 10
	boxChromeRows  = 6
)

func boxWidth(termWidth int) int { return max(min(termWidth*2/3, 100), 30) }

func listRows(termHeight int) int { return max(min(maxVisibleRows, termHeight-boxChromeRows), 1) }

// BoxSize returns the outer size of the rendered box.
func (m *Model) BoxSize(termWidth, termHeight int) (int, int) {
	start, end := chrome.Window(m.selected, len(m.rows), listRows(termHeight))
	return boxWidth(termWidth), max(end-start, 1) + boxChromeRows
}

// View renders the box alone (tests).
func (m Model) View(termWidth, termHeight int) string { return m.renderBox(termWidth, termHeight) }

// ViewOverlay renders the box centred over a dimmed background.
func (m Model) ViewOverlay(termWidth, termHeight int, background string) string {
	if !m.visible {
		return background
	}
	return overlay.DimmedOverlay(termWidth, termHeight, background, m.renderBox(termWidth, termHeight), 0.5)
}

func (m Model) renderBox(termWidth, termHeight int) string {
	s := messages.ChromeStyles()
	width := boxWidth(termWidth)
	start, end := chrome.Window(m.selected, len(m.rows), listRows(termHeight))
	list := chrome.List{Start: start, End: end, Total: len(m.rows)}
	contentWidth := list.ContentWidth(width - 4)
	dest := s.Row.Foreground(styles.Accent).Bold(true)
	muted := s.Row.Foreground(styles.TextMuted)
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		r := m.rows[i]
		d, content, mu := dest, s.Row, muted
		if i == m.selected {
			d, content, mu = dest.Foreground(styles.Primary), s.RowSelected, s.RowSelected
		}
		line := d.Render(r.Destination) + content.Render("  ") + mu.Render(r.Age)
		if r.Marker != "" {
			line += content.Render("  ") + mu.Render("["+r.Marker+"]")
		}
		line += content.Render("  " + r.Preview)
		if lipgloss.Width(line) > contentWidth {
			line = truncate.StringWithTail(line, uint(contentWidth), "…")
		}
		if pad := contentWidth - lipgloss.Width(line); pad > 0 {
			line += content.Render(strings.Repeat(" ", pad))
		}
		lines = append(lines, line)
	}
	return chrome.Frame{
		Title: "Drafts",
		Body:  list.Rows(s, m.selected, lines),
		Empty: "No drafts",
	}.Render(s, width)
}
```

`chrome.Dialog` is `IsVisible()` + `Close()`; `chrome.Boxed` is `BoxSize` (`internal/bubbles/chrome/contract.go`).

- [ ] **Step 3b: Run the widget test**

Run: `go test ./internal/ui/draftlist -count=1`
Expected: PASS.

- [ ] **Step 4: Write the App-level failing tests**

`internal/ui/drafts_list_test.go`:

```go
package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/core"
)

func listDrafts() []core.Draft {
	return []core.Draft{
		{ID: "Dr1", Key: core.DraftKey{TeamID: "T1", ChannelID: "C2"}, Text: "hello there", Updated: time.Unix(1_800_000_000-300, 0)},
		{ID: "Dr2", Key: core.DraftKey{TeamID: "T1", ChannelID: "C3", ThreadTS: "10.0"}, Text: "deploy", Updated: time.Unix(1_800_000_000-7200, 0),
			Scheduled: time.Date(2027, 12, 1, 9, 0, 0, 0, time.Local)},
		{ID: "Dr3", Key: core.DraftKey{TeamID: "T1"}, Text: "nobody yet", Saved: true, Updated: time.Unix(1_700_000_000, 0)},
	}
}

func TestCmdDrafts_OffRefuses(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	executeCommand(a, "drafts")
	if a.mode == ModeDrafts || !strings.Contains(statusbarText(a), "sync_drafts") {
		t.Errorf("mode %v, toast %q", a.mode, statusbarText(a))
	}
}

func TestCmdDrafts_OpensAndReloads(t *testing.T) {
	a, _, _ := draftSyncApp(t, nil)
	a.drafts.rows["T1"] = listDrafts()
	loads := 0
	a.setDraftServiceForTest(core.DraftServiceFuncs{Load: func(string) ([]core.Draft, error) { loads++; return nil, nil }})
	cmd := executeCommand(a, "drafts")
	if a.mode != ModeDrafts || !a.draftList.IsVisible() {
		t.Fatalf("mode %v visible %v", a.mode, a.draftList.IsVisible())
	}
	if cmd == nil {
		t.Fatal("no reload command")
	}
	cmd()
	if loads != 1 {
		t.Errorf("loads = %d", loads)
	}
	v := ansi.Strip(a.draftList.View(a.width, a.height))
	// Dr3 is saved but has no recipient: its label says so, no marker.
	for _, want := range []string{"hello there", "5m", "2h", "scheduled Dec 1 9:00 AM", "(no recipient)"} {
		if !strings.Contains(v, want) {
			t.Errorf("list lacks %q", want)
		}
	}
}

func TestDraftList_EnterJumps(t *testing.T) {
	a := jumpMarkTestApp(t)
	a.SetDraftService(core.NewDraftService(core.DraftServiceFuncs{}))
	a.drafts.rows["T1"] = listDrafts()
	executeCommand(a, "drafts")
	a.Update(keyCode(tea.KeyEnter))
	if a.mode != ModeNormal || a.draftList.IsVisible() {
		t.Errorf("mode %v, list visible %v", a.mode, a.draftList.IsVisible())
	}
	if a.pendingLinkNav == nil || a.pendingLinkNav.Location.ChannelID != "C2" {
		t.Errorf("pending nav = %+v, want C2", a.pendingLinkNav)
	}
}

func TestDraftList_NoRecipientDoesNotJump(t *testing.T) {
	a, _, _ := draftSyncApp(t, nil)
	a.drafts.rows["T1"] = listDrafts()[2:]
	executeCommand(a, "drafts")
	a.Update(keyCode(tea.KeyEnter))
	if !strings.Contains(statusbarText(a), "no recipient") {
		t.Errorf("toast = %q", statusbarText(a))
	}
}

func TestDraftList_DeleteConfirmsThenDeletesAndClearsTheSlot(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	a.Update(draftsLoadedMsg{teamID: "T1", drafts: []core.Draft{
		{ID: "Dr1", Key: core.DraftKey{TeamID: "T1", ChannelID: "C1"}, Text: "on screen"},
	}})
	executeCommand(a, "drafts")
	a.Update(keyPress('d'))
	if a.mode != ModeConfirm {
		t.Fatalf("mode = %v, want ModeConfirm", a.mode)
	}
	_, cmd := a.Update(keyCode(tea.KeyEnter)) // confirm
	for _, m := range deliveredMsgs[draftDeleteConfirmedMsg](t, cmd) {
		_, del := a.Update(m)
		a.Update(del())
	}
	if ops(*calls) != `delete Dr1 / ""` {
		t.Errorf("calls = %s", ops(*calls))
	}
	if a.compose.Value() != "" || len(a.drafts.rows["T1"]) != 0 {
		t.Errorf("value %q rows %v", a.compose.Value(), a.drafts.rows["T1"])
	}
	if !strings.Contains(statusbarText(a), "Draft deleted") {
		t.Errorf("toast = %q", statusbarText(a))
	}
}

// A reload while the list is open must not shift what Enter acts on.
func TestDraftList_ActsOnTheRowShown(t *testing.T) {
	a := jumpMarkTestApp(t)
	a.SetDraftService(core.NewDraftService(core.DraftServiceFuncs{}))
	a.drafts.rows["T1"] = listDrafts()
	executeCommand(a, "drafts")
	a.Update(draftsLoadedMsg{teamID: "T1", drafts: listDrafts()[1:]}) // Dr1 gone
	a.Update(keyCode(tea.KeyEnter))
	if a.pendingLinkNav == nil || a.pendingLinkNav.Location.ChannelID != "C3" {
		t.Errorf("pending nav = %+v, want C3 (the first row now shown)", a.pendingLinkNav)
	}
}
```

`jumpMarkTestApp` (an App in T1 whose every channel resolves) is in `jump_mark_test.go`; `keyCode`/`keyPress` are in `modekeys_test.go`; Enter confirms the confirm prompt (`TestConfirmPromptModifierGrid`).

- [ ] **Step 5: Run to verify they fail**

Run: `go test ./internal/ui -run 'CmdDrafts|DraftList' -count=1`
Expected: FAIL to compile: `undefined: ModeDrafts`.

- [ ] **Step 6: Implement the App glue**

`internal/ui/drafts_list.go`:

```go
// internal/ui/drafts_list.go
//
// The :drafts dialog's glue: rows from draft sync's per-workspace drafts,
// opening, jumping and deleting.
package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/draftlist"
)

func cmdDrafts(a *App, _ commandArgs) tea.Cmd {
	if a.draftSvc == nil {
		return toastWithClear(a, "Draft sync is off ([compose] sync_drafts)", 3*time.Second)
	}
	a.refreshDraftList()
	a.draftList.Open()
	a.SetMode(ModeDrafts)
	return a.loadDraftsCmd(a.activeTeamID)
}

// refreshDraftList rebuilds the dialog's rows. draftListItems keeps the
// drafts in the order shown, so a key acts on the row it highlights.
func (a *App) refreshDraftList() {
	items := a.drafts.rows[a.activeTeamID]
	a.draftListItems = items
	now := a.now()
	rows := make([]draftlist.Row, 0, len(items))
	for _, d := range items {
		rows = append(rows, draftlist.Row{
			Destination: a.draftLabel(d.Key),
			Age:         draftAge(now, d.Updated),
			Marker:      draftMarker(d),
			Preview:     strings.Join(strings.Fields(a.composerFor(d.Key.ThreadTS).MentionsForEdit(d.Text)), " "),
		})
	}
	a.draftList.SetRows(rows)
}

// draftLabel names a draft's conversation.
func (a *App) draftLabel(k core.DraftKey) string {
	if k.ChannelID == "" {
		return "(no recipient)"
	}
	label := k.ChannelID
	if name, typ, ok := a.channels.Lookup(ids.ChannelID(k.ChannelID)); ok {
		label = name
		if typ == "channel" || typ == "private" {
			label = "#" + name
		}
	}
	if k.ThreadTS != "" {
		label += " › thread"
	}
	return label
}

func draftAge(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return t.Format("Jan 2")
}

// draftMarker names a hands-off draft's kind (spec, Behaviour).
func draftMarker(d core.Draft) string {
	switch {
	case !d.Scheduled.IsZero():
		return "scheduled " + d.Scheduled.Format("Jan 2 3:04 PM")
	case d.FileCount == 1:
		return "1 file"
	case d.FileCount > 1:
		return fmt.Sprintf("%d files", d.FileCount)
	case d.Saved && d.Key.ChannelID != "":
		return "saved"
	}
	return ""
}

func (a *App) jumpToDraft(d core.Draft) tea.Cmd {
	if d.Key.ChannelID == "" {
		return toastWithClear(a, "This draft has no recipient", 2*time.Second)
	}
	loc := Location{TeamID: ids.TeamID(d.Key.TeamID), ChannelID: ids.ChannelID(d.Key.ChannelID)}
	if d.Key.ThreadTS != "" {
		loc.MessageTS = ids.MessageTS(d.Key.ThreadTS)
		loc.ThreadTS = ids.ThreadTS(d.Key.ThreadTS)
	}
	cmd, ok := a.applyLocation(loc, false)
	if !ok {
		return toastWithClear(a, "That conversation is unavailable", 3*time.Second)
	}
	return cmd
}

func (a *App) confirmDeleteDraft(d core.Draft) {
	teamID := a.activeTeamID
	body := a.draftLabel(d.Key) + ": " + strings.Join(strings.Fields(a.composerFor(d.Key.ThreadTS).MentionsForEdit(d.Text)), " ")
	a.openConfirmPrompt("Delete draft?", body, func() tea.Msg {
		return draftDeleteConfirmedMsg{teamID: teamID, draft: d}
	})
}

func (a *App) deleteDraftCmd(teamID string, d core.Draft) tea.Cmd {
	svc := a.draftSvc
	if svc == nil {
		return nil
	}
	return func() tea.Msg {
		return draftDeletedMsg{teamID: teamID, draft: d, err: svc.Delete(teamID, d.ID)}
	}
}

func (a *App) reduceDraftDeleted(m draftDeletedMsg) tea.Cmd {
	if m.err != nil {
		return toastWithClear(a, "Couldn't delete draft", 3*time.Second)
	}
	a.drafts.removeRow(m.teamID, m.draft.ID)
	if k := m.draft.Key; m.draft.Writable() {
		// The user asked: the slot goes too, even the visible one.
		a.composerFor(k.ThreadTS).ClearSlot(k.TeamID, k.ChannelID, k.ThreadTS)
		a.drafts.gen[k]++
		delete(a.drafts.synced, k)
		delete(a.drafts.observed, k)
	}
	a.draftRowsChanged()
	return toastWithClear(a, "Draft deleted", 2*time.Second)
}
```

`internal/ui/mode_drafts.go`:

```go
// internal/ui/mode_drafts.go
//
// The :drafts dialog's keys: Enter jumps to the highlighted draft's
// conversation, d asks to delete it, Esc closes.
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/draftlist"
)

func handleDraftsMode(a *App, msg tea.KeyMsg) tea.Cmd {
	act, i := a.draftList.HandleKey(normalizeFinderKey(msg))
	if act != draftlist.None && i < len(a.draftListItems) {
		d := a.draftListItems[i]
		if act == draftlist.Delete {
			a.confirmDeleteDraft(d) // switches to ModeConfirm, closing the list
			return nil
		}
		a.draftList.Close()
		a.SetMode(ModeNormal)
		return a.jumpToDraft(d)
	}
	if !a.draftList.IsVisible() {
		a.SetMode(ModeNormal)
	}
	return nil
}
```

In `drafts.go`, replace the placeholder `draftRowsChanged`:

```go
// draftRowsChanged redraws the Drafts list when it is open.
func (a *App) draftRowsChanged() {
	if a.draftList.IsVisible() {
		a.refreshDraftList()
	}
}
```

In `reducer_drafts.go`, add:

```go
	case draftDeleteConfirmedMsg:
		return a.deleteDraftCmd(m.teamID, m.draft), true
	case draftDeletedMsg:
		return a.reduceDraftDeleted(m), true
```

Mode plumbing:
- `mode.go`: `ModeDrafts` right before `modeCount`; add to `IsModalOverlay`'s case list; `case ModeDrafts: return "DRAFTS"` in `String`.
- `mode_handlers.go`: `ModeDrafts: handleDraftsMode,` after `ModeMarks`.
- `app.go`: fields `draftList draftlist.Model` and `draftListItems []core.Draft` next to `marksOverlay`; `draftList: draftlist.New(),` in `NewApp`; `case ModeDrafts: a.draftList.Close()` in `closeModal` after `ModeMarks`.
- `view_overlays.go`: after the marks block, `if a.draftList.IsVisible() { screen = a.draftList.ViewOverlay(a.width, a.height, screen) }`; add `a.draftList.IsVisible() ||` to `overlayActive`.
- `command.go`: `"drafts": cmdDrafts,` in `commands`.
- `keys.go`: field `DraftsList key.Binding` after `MarksDelete`; in `DefaultKeyMap`, after `MarksDelete:`, `DraftsList: key.NewBinding(key.WithHelp(":drafts", "list Slack drafts")),`.
- `quit_prompt_modals_test.go`: add

```go
		ModeDrafts: {
			func(a *App) { a.draftList.Open() },
			func(a *App) bool { return a.draftList.IsVisible() },
		},
```

- `golden_dialogs_test.go`: in `dialogGoldens()`, add `draftsShown := func(a *App) bool { return a.draftList.IsVisible() }` next to `marksShown`, and after the marks scenarios:

```go
		{
			name: "dialog_drafts", want: "Drafts", shown: draftsShown,
			open: func(t *testing.T, a *App) {
				a.draftList.SetRows([]draftlist.Row{
					{Destination: "#general", Age: "5m", Preview: "hello there"},
					{Destination: "#ops › thread", Age: "2h", Marker: "scheduled Dec 1 9:00 AM", Preview: "deploy at nine"},
					{Destination: "(no recipient)", Age: "Jan 2", Marker: "saved", Preview: "an idea for later"},
				})
				a.draftList.Open()
				a.SetMode(ModeDrafts)
			},
		},
```

(import `github.com/gammons/slk/internal/ui/draftlist` there.)

- [ ] **Step 7: Bless the new golden, then run everything in the package**

Run: `go test ./internal/ui -run 'TestGolden_Dialogs/dialog_drafts' -update -count=1`
Then: `git status --short internal/ui/testdata` must show exactly one new file, `dialog_drafts.ansi`. Open it with `cat` and check the box reads as intended.

Run: `go test ./internal/ui/... -count=1`
Expected: PASS, including `TestQuitPromptClosesEveryModal`, `TestHelpListsEveryCommand`, the modeHandlers count test and `TestGolden_DialogsArePairwiseDistinct`.

- [ ] **Step 8: Commit**

```bash
gofmt -l internal/ui
git add internal/ui
git commit -m "feat(ui): :drafts list to browse, jump to and delete drafts"
```

---

### Task 8: Wire it up in `cmd/slk`, plus the option

**Files:**
- Modify: `internal/config/config.go` (`Compose.SyncDrafts *bool`, `EffectiveSyncDrafts`)
- Modify: `internal/config/config_test.go`
- Modify: `cmd/slk/workspace.go` (`Drafts *draftEngine` on `WorkspaceContext`)
- Modify: `cmd/slk/connect.go` (create the engine; send `DraftsStaleMsg` in `OnConnect`)
- Modify: `cmd/slk/main.go` (`Send`/`SendReply` through `sendWithDraft`; `SetDraftService`)
- Modify: `cmd/slk/shutdown.go`, `cmd/slk/shutdown_test.go`
- Create: `cmd/slk/drafts_wiring_test.go`

**Interfaces:**
- Consumes: everything above. `ui.DraftsStaleMsg`, `(*ui.App).UnsyncedDrafts()`, `(*ui.App).SetDraftService`.
- Produces: `func (c Config) EffectiveSyncDrafts() bool`; `func flushDrafts(ctx, router *workspaceRouter, changes []core.DraftChange)`; `func draftService(router *workspaceRouter) core.DraftService`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestSyncDrafts(t *testing.T) {
	cases := []struct {
		name, body string
		want       bool
	}{
		{"unset is on", "", true},
		{"on", "[compose]\nsync_drafts = true\n", true},
		{"off", "[compose]\nsync_drafts = false\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := loadConfigString(t, tc.body).EffectiveSyncDrafts(); got != tc.want {
				t.Errorf("EffectiveSyncDrafts = %v, want %v", got, tc.want)
			}
		})
	}
}
```

Replace `cmd/slk/shutdown_test.go`:

```go
package main

import (
	"context"
	"testing"
	"time"

	"github.com/gammons/slk/internal/core"
)

// recordingSaver counts SavePendingTheme calls and hands out drafts.
type recordingSaver struct {
	saves  int
	drafts []core.DraftChange
}

func (r *recordingSaver) SavePendingTheme()                  { r.saves++ }
func (r *recordingSaver) UnsyncedDrafts() []core.DraftChange { return r.drafts }

// TestShutdown_SavesPendingTheme pins the exit flush: a theme cycle
// saves only after a pause, so shutdown must save one still pending.
func TestShutdown_SavesPendingTheme(t *testing.T) {
	app := &recordingSaver{}
	shutdown(app, newWorkspaceRouter())
	if app.saves != 1 {
		t.Errorf("SavePendingTheme called %d times, want 1", app.saves)
	}
}

func TestFlushDrafts_SavesAndDiscardsThroughEachWorkspace(t *testing.T) {
	api := &fakeDraftAPI{}
	eng := newDraftEngine(api, "T1")
	_, _ = eng.Save(context.Background(), draftDest{channelID: "C2"}, "old", time.Time{})
	router := newWorkspaceRouter()
	router.Add(&WorkspaceContext{TeamID: "T1", Drafts: eng})
	flushDrafts(context.Background(), router, []core.DraftChange{
		{Key: core.DraftKey{TeamID: "T1", ChannelID: "C1"}, Text: "typed", Edited: time.UnixMilli(7)},
		{Key: core.DraftKey{TeamID: "T1", ChannelID: "C2"}},
		{Key: core.DraftKey{TeamID: "T9", ChannelID: "C1"}, Text: "unknown workspace"},
	})
	want := "[create C2 old create C1 typed delete Dr1]"
	if got := fmt.Sprint(api.Calls()); got != want {
		t.Errorf("calls = %s, want %s", got, want)
	}
}
```

(add `"fmt"` to the imports.)

`cmd/slk/drafts_wiring_test.go`:

```go
package main

import (
	"testing"
	"time"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui"
)

func TestDraftService_RoutesByWorkspace(t *testing.T) {
	api := &fakeDraftAPI{}
	router := newWorkspaceRouter()
	router.Add(&WorkspaceContext{TeamID: "T1", Drafts: newDraftEngine(api, "T1")})
	svc := draftService(router)
	res, err := svc.Save(core.DraftKey{TeamID: "T1", ChannelID: "C1"}, "hi", time.Time{})
	if err != nil || res.Draft.ID != "Dr1" {
		t.Fatalf("save = %+v, %v", res, err)
	}
	if _, err := svc.Load("T9"); err == nil {
		t.Error("unknown workspace loaded")
	}
	if err := svc.Discard(core.DraftKey{TeamID: "T9"}); err == nil {
		t.Error("unknown workspace discarded")
	}
}

func TestOnConnect_AsksForDrafts(t *testing.T) {
	sender := &captureSender{}
	h := &rtmEventHandler{program: sender, workspaceID: "T1",
		wsCtx: &WorkspaceContext{TeamID: "T1", Drafts: newDraftEngine(&fakeDraftAPI{}, "T1")}}
	h.askForDrafts()
	found := false
	for _, m := range sender.sent {
		if m == (ui.DraftsStaleMsg{TeamID: "T1"}) {
			found = true
		}
	}
	if !found {
		t.Errorf("sent %v, want DraftsStaleMsg{T1}", sender.sent)
	}
	off := &captureSender{}
	(&rtmEventHandler{program: off, workspaceID: "T1", wsCtx: &WorkspaceContext{TeamID: "T1"}}).askForDrafts()
	if len(off.sent) != 0 {
		t.Errorf("sync off sent %v", off.sent)
	}
}
```

(`captureSender` is declared in `reconnect_sync_test.go` with a `sent []tea.Msg` field.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/config ./cmd/slk -run 'SyncDrafts|Shutdown|FlushDrafts|DraftService_|OnConnect_AsksForDrafts' -count=1`
Expected: FAIL to compile.

- [ ] **Step 3: Implement**

`config.go`, in `Compose`:

```go
	// SyncDrafts makes the composer's unsent text Slack's own draft for
	// the conversation (saved while typing, restored on open, marked
	// sent on send), and enables :drafts. A *bool so unset (nil) is
	// told from an explicit false; see EffectiveSyncDrafts. Default on.
	SyncDrafts *bool `toml:"sync_drafts"`
```

and next to `EffectiveShowJumpOverlay`:

```go
// EffectiveSyncDrafts reports [compose] sync_drafts: an explicit value
// wins, unset is on.
func (c Config) EffectiveSyncDrafts() bool {
	if c.Compose.SyncDrafts != nil {
		return *c.Compose.SyncDrafts
	}
	return true
}
```

`workspace.go`, on `WorkspaceContext`:

```go
	// Drafts syncs this workspace's composer drafts with Slack. Nil when
	// [compose] sync_drafts is off.
	Drafts *draftEngine
```

`connect.go`, right after the `wctx := &WorkspaceContext{…}` literal:

```go
	if cfg.EffectiveSyncDrafts() {
		wctx.Drafts = newDraftEngine(client, client.TeamID())
	}
```

and in `OnConnect`, right after the `ConnectionStateMsg` send, `h.askForDrafts()`, with:

```go
// askForDrafts asks the App to reload the workspace's drafts: on the
// first connect that is the first list, after a reconnect it catches
// drafts changed on other devices meanwhile.
func (h *rtmEventHandler) askForDrafts() {
	if h.wsCtx == nil || h.wsCtx.Drafts == nil {
		return
	}
	h.program.Send(ui.DraftsStaleMsg{TeamID: h.workspaceID})
}
```

`cmd/slk/drafts.go`, append:

```go
// draftTimeout bounds one draft call from the UI.
const draftTimeout = 15 * time.Second

// draftService is the App's DraftService: each call goes to the engine
// of the workspace it names, which need not be the active one.
func draftService(router *workspaceRouter) core.DraftService {
	engine := func(teamID string) (*draftEngine, error) {
		if w := router.ByID(teamID); w != nil && w.Drafts != nil {
			return w.Drafts, nil
		}
		return nil, fmt.Errorf("drafts: workspace %q is unavailable", teamID)
	}
	call := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), draftTimeout)
	}
	return core.NewDraftService(core.DraftServiceFuncs{
		Load: func(teamID string) ([]core.Draft, error) {
			e, err := engine(teamID)
			if err != nil {
				return nil, err
			}
			ctx, cancel := call()
			defer cancel()
			return e.Load(ctx)
		},
		Save: func(k core.DraftKey, text string, edited time.Time) (core.DraftSaveResult, error) {
			e, err := engine(k.TeamID)
			if err != nil {
				return core.DraftSaveResult{}, err
			}
			ctx, cancel := call()
			defer cancel()
			return e.Save(ctx, draftDest{k.ChannelID, k.ThreadTS}, text, edited)
		},
		Discard: func(k core.DraftKey) error {
			e, err := engine(k.TeamID)
			if err != nil {
				return err
			}
			ctx, cancel := call()
			defer cancel()
			return e.Discard(ctx, draftDest{k.ChannelID, k.ThreadTS})
		},
		Delete: func(teamID, id string) error {
			e, err := engine(teamID)
			if err != nil {
				return err
			}
			ctx, cancel := call()
			defer cancel()
			return e.Delete(ctx, id)
		},
	})
}

// flushDrafts saves the drafts the App had not synced yet, on quit,
// within ctx. Failures are logged; the text stays in memory only.
func flushDrafts(ctx context.Context, router *workspaceRouter, changes []core.DraftChange) {
	for _, c := range changes {
		w := router.ByID(c.Key.TeamID)
		if w == nil || w.Drafts == nil {
			continue
		}
		dest := draftDest{c.Key.ChannelID, c.Key.ThreadTS}
		var err error
		if c.Text == "" {
			err = w.Drafts.Discard(ctx, dest)
		} else {
			_, err = w.Drafts.Save(ctx, dest, c.Text, c.Edited)
		}
		if err != nil {
			debuglog.General("drafts: quit flush %v: %v", c.Key, err)
		}
	}
}
```

(add `"fmt"` to `drafts.go`'s imports.)

`shutdown.go`: add `UnsyncedDrafts() []core.DraftChange` to `pendingSaver` (doc: "drafts typed since the last sync, saved before the connections close"), and at the top of `shutdown`, after `SavePendingTheme`:

```go
	// Draft sync saves while typing, but up to two seconds behind.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	flushDrafts(ctx, router, app.UnsyncedDrafts())
	cancel()
```

`main.go`:
- In `Send` (line ~998) replace `client.SendMessage(ctx, chIDStr, text)` with `sendWithDraft(ctx, client, wctx.Drafts, chIDStr, "", text, false)`.
- In `SendReply` (line ~1253) replace `client.SendReply(ctx, chIDStr, threadTSStr, text, broadcast)` with `sendWithDraft(ctx, client, wctx.Drafts, chIDStr, threadTSStr, text, broadcast)`.
- After `app.SetCommandService(...)` (line ~1454): 

```go
		if cfg.EffectiveSyncDrafts() {
			app.SetDraftService(draftService(router))
		}
```

- [ ] **Step 4: Run the tests, then build**

Run: `go test ./internal/config ./cmd/slk -count=1 -race`
Expected: PASS.

Run: `go build ./... && go vet ./...`
Expected: no output.

- [ ] **Step 5: Commit**

```bash
gofmt -l .
git add internal/config cmd/slk
git commit -m "feat(slk): wire draft sync, [compose] sync_drafts, flush on quit"
```

---

### Task 9: Docs and the full check

**Files:**
- Modify: `AGENTS.md` (Shared code tables; Test helpers table)
- Modify: `README.md` or the config docs, wherever `stay_in_insert_after_send` is documented (`grep -rn stay_in_insert_after_send --include=*.md .`)

- [ ] **Step 1: Document the option** next to `stay_in_insert_after_send`, in the same style:

```toml
# Make the composer's unsent text Slack's own draft for the conversation:
# saved while you type, restored when you open the conversation (on any
# device), marked sent when you send. Also enables :drafts. Default true.
sync_drafts = true
```

- [ ] **Step 2: Add rows to AGENTS.md**

"App state and actions" table:

| Need | Use |
|---|---|
| Sync composer text with Slack's drafts; what Slack last had per slot; restore a draft from Slack without replacing typed text | `draftSync` (`internal/ui/drafts.go`): the 2 s `draftTickMsg` comparison `(*App).syncDrafts`, `(*App).restoreRemoteDraft`, `(*App).noteDraftSent(key)` at a send's composer reset, `(*App).UnsyncedDrafts()` for quit. Port `core.DraftService` (wired in `cmd/slk` by `draftService(router)`); per-workspace engine `draftEngine` (`cmd/slk/drafts.go`) owns ids, create vs update, one call per conversation and conflicts |
| A composer's slots as draft sync reads them | `(*compose.Model).Slots`, `SlotText`, `ApplyRemoteDraft` (inactive slots, or the visible one only when empty), `ClearSlot` |
| Post a message marking its draft sent | `sendWithDraft(ctx, client, wctx.Drafts, channelID, threadTS, text, broadcast)` (`cmd/slk/drafts.go`), over `(*slackclient.Client).SendDraft` |
| Slack's drafts API | `(*slackclient.Client).ListDrafts`, `CreateDraft`, `UpdateDraft`, `DeleteDraft` (`internal/slack/drafts.go`); refusals are `*DraftRejectedError`, read with `DraftRejectionCode(err)` |

"UI chrome" table:

| Need | Use |
|---|---|
| The `:drafts` dialog | `internal/ui/draftlist` (`Row`, `HandleKey` → `Jump`/`Delete` + index); glue in `internal/ui/drafts_list.go` (`refreshDraftList`, `draftListItems` is the order shown, `draftLabel`, `draftAge`, `draftMarker`) |

"Test helpers" table:

| Need | Use |
|---|---|
| An App on T1/C1 with draft sync wired to a recording service and a movable clock; run one comparison and feed its answers back | `draftSyncApp(t, saveErr)`, `tick(t, a)`, `ops(calls)` (`internal/ui/drafts_test.go`) |
| A scripted drafts API / sender for the engine | `fakeDraftAPI`, `fakeSender`, `attached(id, ch, thread, ts)`, `rejected(code)` (`cmd/slk/drafts_test.go`, `package main`) |
| Compare a draft form with a capture, some fields by shape | `assertDraftForm(t, got, capture, shapes)` (`internal/slack/drafts_test.go`) |

- [ ] **Step 3: Full verification**

Run, in order, and read each output:

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./... -race -count=1
golangci-lint run
```

Expected: no build/vet output, `gofmt -l` empty, every package `ok`, linter clean.

- [ ] **Step 4: Manual check against a real workspace** (needs the user's Slack; ask them to run it)

1. `SLK_DEBUG=1 slk`, open a channel, type `testing drafts`, wait 5 s. In the web client, the channel shows the draft.
2. Edit the draft in the web client; in slk, `:drafts` shows it; open the channel (composer empty there): the web client's text appears.
3. Send from slk; the web client's draft disappears. `slk-debug.log` shows `chat.postMessage` without a `draft refused` line.
4. Delete a draft from `:drafts`; it is gone from the web client.

- [ ] **Step 5: Commit**

```bash
git add AGENTS.md README.md docs
git commit -m "docs: draft sync helpers and the sync_drafts option"
```
