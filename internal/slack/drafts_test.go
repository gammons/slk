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

// uuidRe is declared in forward_test.go; reused here.

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
