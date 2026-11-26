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
	res, err := svc.Save(core.DraftKey{TeamID: "T1", ChannelID: "C1"}, "hi", time.Time{}, nil)
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
