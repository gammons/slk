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

func rejected(code string) error {
	return &slackclient.DraftRejectedError{Method: "drafts.x", Code: code}
}

var c1 = draftDest{channelID: "C1"}

func TestDraftEngine_CreatesThenUpdates(t *testing.T) {
	api := &fakeDraftAPI{}
	e := newDraftEngine(api, "T1")
	ctx := context.Background()
	res, err := e.Save(ctx, c1, "one", time.UnixMilli(1000), nil)
	if err != nil || res.Draft.ID != "Dr1" || res.Draft.Key != (core.DraftKey{TeamID: "T1", ChannelID: "C1"}) {
		t.Fatalf("first save = %+v, %v", res, err)
	}
	if _, err := e.Save(ctx, c1, "two", time.UnixMilli(2000), nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"create C1 one", "update Dr1 two 2000"}
	if got := api.Calls(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
}

// A Save whose current reports false (a send/delete for the same
// conversation ran first) makes no API call and answers ErrDraftStale.
func TestDraftEngine_SaveNoLongerCurrentMakesNoAPICall(t *testing.T) {
	api := &fakeDraftAPI{}
	e := newDraftEngine(api, "T1")
	ctx := context.Background()
	_, err := e.Save(ctx, c1, "one", time.Time{}, func() bool { return false })
	if !errors.Is(err, core.ErrDraftStale) {
		t.Fatalf("err = %v, want ErrDraftStale", err)
	}
	if got := api.Calls(); len(got) != 0 {
		t.Errorf("calls = %v, want none", got)
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
	res, err := e.Save(ctx, c1, "mine", time.UnixMilli(1000), nil)
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
	res, err := e.Save(context.Background(), c1, "mine", time.UnixMilli(5), nil)
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
	if _, err := e.Save(context.Background(), c1, "mine", time.Time{}, nil); !errors.Is(err, core.ErrDraftHandsOff) {
		t.Fatalf("err = %v, want ErrDraftHandsOff", err)
	}
}

// A failed adoption remembers the hands-off slot, so a second Save for
// the same conversation short-circuits instead of repeating the
// create -> attached_draft_exists -> list round trip.
func TestDraftEngine_CreateHandsOffIsRemembered(t *testing.T) {
	api := &fakeDraftAPI{}
	api.create = func(slackclient.DraftWrite) (slackclient.Draft, error) {
		return slackclient.Draft{}, rejected("attached_draft_exists")
	}
	e := newDraftEngine(api, "T1")
	ctx := context.Background()
	if _, err := e.Save(ctx, c1, "mine", time.Time{}, nil); !errors.Is(err, core.ErrDraftHandsOff) {
		t.Fatalf("first save err = %v, want ErrDraftHandsOff", err)
	}
	before := len(api.Calls())
	if _, err := e.Save(ctx, c1, "mine again", time.Time{}, nil); !errors.Is(err, core.ErrDraftHandsOff) {
		t.Fatalf("second save err = %v, want ErrDraftHandsOff", err)
	}
	if got := len(api.Calls()); got != before {
		t.Errorf("second save made %d API call(s), want 0 (calls=%v)", got-before, api.Calls())
	}
}

// draft_update_invalid also remembers the hands-off slot, so a second
// Save makes no API calls either.
func TestDraftEngine_UpdateInvalidHandsOffIsRemembered(t *testing.T) {
	api := &fakeDraftAPI{list: func() ([]slackclient.Draft, error) {
		return []slackclient.Draft{attached("Dr7", "C1", "", "1.0")}, nil
	}}
	api.update = func(string, slackclient.DraftWrite) (slackclient.Draft, error) {
		return slackclient.Draft{}, rejected("draft_update_invalid")
	}
	e := newDraftEngine(api, "T1")
	ctx := context.Background()
	if _, err := e.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Save(ctx, c1, "mine", time.Time{}, nil); !errors.Is(err, core.ErrDraftHandsOff) {
		t.Fatalf("first save err = %v, want ErrDraftHandsOff", err)
	}
	before := len(api.Calls())
	if _, err := e.Save(ctx, c1, "mine again", time.Time{}, nil); !errors.Is(err, core.ErrDraftHandsOff) {
		t.Fatalf("second save err = %v, want ErrDraftHandsOff", err)
	}
	if got := len(api.Calls()); got != before {
		t.Errorf("second save made %d API call(s), want 0 (calls=%v)", got-before, api.Calls())
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
		if _, err := e.Save(ctx, d, "x", time.Time{}, nil); !errors.Is(err, core.ErrDraftHandsOff) {
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
	res, err := e.Save(ctx, c1, "again", time.UnixMilli(1), nil)
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
	_, _ = e.Save(ctx, c1, "x", time.Time{}, nil)
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

// A "gone" refusal (the draft no longer exists by the time Discard
// asked) drops the entry and answers nil, same as a clean delete.
func TestDraftEngine_DiscardGoneCodeDropsEntryAndForgets(t *testing.T) {
	for _, code := range []string{"draft_not_found", "draft_already_deleted", "draft_already_sent", "draft_delete_invalid"} {
		t.Run(code, func(t *testing.T) {
			api := &fakeDraftAPI{delete: func(string) error { return rejected(code) }}
			e := newDraftEngine(api, "T1")
			ctx := context.Background()
			_, _ = e.Save(ctx, c1, "x", time.Time{}, nil)
			if err := e.Discard(ctx, c1); err != nil {
				t.Fatalf("err = %v, want nil (%s means gone)", err, code)
			}
			if _, ok := e.entry(c1); ok {
				t.Errorf("entry not dropped after a %s refusal", code)
			}
		})
	}
}

// Any other refusal (internal_error, invalid_auth, …) does not mean
// the draft is gone: Discard keeps the entry and returns the error, so
// the caller retries with backoff.
func TestDraftEngine_DiscardOtherRefusalKeepsEntryAndReturnsError(t *testing.T) {
	api := &fakeDraftAPI{delete: func(string) error { return rejected("internal_error") }}
	e := newDraftEngine(api, "T1")
	ctx := context.Background()
	_, _ = e.Save(ctx, c1, "x", time.Time{}, nil)
	err := e.Discard(ctx, c1)
	if err == nil || slackclient.DraftRejectionCode(err) != "internal_error" {
		t.Fatalf("err = %v, want internal_error", err)
	}
	if _, ok := e.entry(c1); !ok {
		t.Error("entry dropped after a non-gone refusal; want it kept for a retry")
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
	go func() { _, _ = e.Save(ctx, c1, "one", time.Time{}, nil); done <- struct{}{} }()
	<-entered
	go func() { _, _ = e.Save(ctx, c1, "two", time.Time{}, nil); done <- struct{}{} }()
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

// A Save for another conversation that both starts and finishes while a
// reload's ListDrafts call is in flight must not be erased when that
// call's (now stale) list commits.
func TestDraftEngine_ReloadKeepsWritesMadeDuringTheList(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	api := &fakeDraftAPI{}
	api.list = func() ([]slackclient.Draft, error) {
		close(entered)
		<-release
		return nil, nil // the snapshot taken before C2 was written
	}
	e := newDraftEngine(api, "T1")
	ctx := context.Background()

	loadDone := make(chan struct{})
	go func() {
		_, _ = e.Load(ctx)
		close(loadDone)
	}()
	<-entered

	c2 := draftDest{channelID: "C2"}
	if _, err := e.Save(ctx, c2, "hello", time.Time{}, nil); err != nil {
		t.Fatal(err)
	}

	close(release)
	<-loadDone

	// If the stale list had won, this would be a second "create C2 ...".
	if _, err := e.Save(ctx, c2, "world", time.Time{}, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"list", "create C2 hello", "update Dr1 world -62135596800000"}
	if got := api.Calls(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
}

// dropIfMatches (behind Delete's fix for its lookup/lock TOCTOU) must
// leave an entry alone when its id no longer matches, and only remove
// it when it still does.
func TestDraftEngine_DropIfMatchesOnlyRemovesMatchingID(t *testing.T) {
	api := &fakeDraftAPI{}
	e := newDraftEngine(api, "T1")
	e.put(c1, attached("Dr1", "C1", "", "1.0"))

	e.dropIfMatches(c1, "Dr9")
	if _, ok := e.entry(c1); !ok {
		t.Fatal("dropIfMatches removed the entry for a mismatched id")
	}

	e.dropIfMatches(c1, "Dr1")
	if _, ok := e.entry(c1); ok {
		t.Fatal("dropIfMatches left the entry in place for a matching id")
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
	_, _ = e.Save(ctx, c1, "hello", time.Time{}, nil)
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
	_, _ = e.Save(ctx, draftDest{"C1", "9.0"}, "hello", time.Time{}, nil)
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
	go func() { _, _ = e.Save(ctx, c1, "hello", time.Time{}, nil) }()
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

// A ctx that is already done when the destination is busy must never
// block or fail the send: sendWithDraft posts plainly instead.
func TestSendWithDraft_CancelledCtxWhileBusyPostsPlain(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	api := &fakeDraftAPI{}
	api.create = func(w slackclient.DraftWrite) (slackclient.Draft, error) {
		close(entered)
		<-release
		return attached("Dr1", "C1", "", "4.0"), nil
	}
	e := newDraftEngine(api, "T1")
	go func() { _, _ = e.Save(context.Background(), c1, "hello", time.Time{}, nil) }()
	<-entered

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &fakeSender{}
	if _, _, err := sendWithDraft(ctx, s, e, "C1", "", "hello", false); err != nil {
		close(release)
		t.Fatalf("sendWithDraft err = %v", err)
	}
	close(release)
	if fmt.Sprint(s.calls) != "[plain C1]" {
		t.Errorf("sends = %v, want a plain post", s.calls)
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
