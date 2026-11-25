package main

import (
	"testing"

	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui"
)

func TestOnViewOpenedShowsTheDialog(t *testing.T) {
	s := &captureSender{}
	h := &rtmEventHandler{program: s, workspaceID: "T1"}
	h.OnViewOpened(slackclient.View{ID: "V1", Title: "AI Budget Support", AppID: "A1", BotID: "B1"})

	if len(s.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(s.sent))
	}
	got, ok := s.sent[0].(ui.AppViewOpenedMsg)
	if !ok {
		t.Fatalf("sent %#v, want AppViewOpenedMsg", s.sent[0])
	}
	if got.TeamID != "T1" || got.View == nil || got.View.ViewID != "V1" || got.View.Title != "AI Budget Support" {
		t.Errorf("sent %+v, want the view for T1", got)
	}
	if got.View.ServiceID != "B1" {
		t.Errorf("service id = %q, want the view's bot id", got.View.ServiceID)
	}
}

func TestOnViewOpenedInactiveWorkspaceIsSilent(t *testing.T) {
	s := &captureSender{}
	h := &rtmEventHandler{program: s, isActive: func() bool { return false }}
	h.OnViewOpened(slackclient.View{ID: "V1", Title: "Giphy"})
	if len(s.sent) != 0 {
		t.Errorf("sent %v, want nothing for an inactive workspace", s.sent)
	}
}

func TestOnViewUpdated_InlineViewIsShown(t *testing.T) {
	s := &captureSender{}
	h := &rtmEventHandler{program: s, workspaceID: "T1"}
	h.OnViewUpdated("V1", &slackclient.View{ID: "V1", Title: "AI Budget Support"})

	if len(s.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(s.sent))
	}
	if _, ok := s.sent[0].(ui.AppViewUpdatedMsg); !ok {
		t.Fatalf("sent %#v, want AppViewUpdatedMsg", s.sent[0])
	}
}

func TestOnViewUpdated_NoInlineViewAsksForARefresh(t *testing.T) {
	s := &captureSender{}
	h := &rtmEventHandler{program: s, workspaceID: "T1"}
	h.OnViewUpdated("V1", nil)

	if len(s.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(s.sent))
	}
	got, ok := s.sent[0].(ui.AppViewNeedsRefreshMsg)
	if !ok {
		t.Fatalf("sent %#v, want AppViewNeedsRefreshMsg", s.sent[0])
	}
	if got.TeamID != "T1" || got.ViewID != "V1" {
		t.Errorf("sent %+v, want a refresh for V1 in T1", got)
	}
}

// Without a view or an ID there is nothing views.get could ask for.
func TestOnViewUpdated_NoViewAndNoIDIsIgnored(t *testing.T) {
	s := &captureSender{}
	h := &rtmEventHandler{program: s, workspaceID: "T1"}
	h.OnViewUpdated("", nil)
	if len(s.sent) != 0 {
		t.Errorf("sent %v, want nothing", s.sent)
	}
}
