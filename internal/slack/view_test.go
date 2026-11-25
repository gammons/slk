package slackclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestParseViewOpened_CapturedFrame(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "ws_view_opened.json"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := parseViewOpened(data)
	if err != nil {
		t.Fatalf("parseViewOpened: %v", err)
	}
	if v.ID != "V0EXAMPLE01" || v.AppID != "A0F827J2C" || v.BotID != "B0EXAMPLE01" {
		t.Errorf("ids = view %q app %q bot %q, want V0EXAMPLE01 A0F827J2C B0EXAMPLE01",
			v.ID, v.AppID, v.BotID)
	}
	if v.Title != "Giphy" || v.SubmitLabel != "Search" || v.CloseLabel != "Cancel" {
		t.Errorf("labels = title %q submit %q close %q, want Giphy Search Cancel",
			v.Title, v.SubmitLabel, v.CloseLabel)
	}
	if v.CallbackID != "giphy:search" {
		t.Errorf("callback_id = %q, want giphy:search", v.CallbackID)
	}
	if len(v.Blocks.BlockSet) != 1 {
		t.Errorf("block count = %d, want 1", len(v.Blocks.BlockSet))
	}
}

// The loading frame of the AI Budget Support modal repeats the title at
// the top level; the nested view is what slk draws.
func TestParseViewOpened_InlinesTheView(t *testing.T) {
	v, err := parseViewOpened([]byte(`{
	  "type":"view_opened","view_id":"V0CLE5MKQF9","title":{"text":"AI Budget Support"},
	  "view":{"id":"V0CLE5MKQF9","title":{"text":"AI Budget Support"},
	          "close":{"text":"Close"},
	          "blocks":[{"type":"section","text":{"type":"mrkdwn","text":"Loading..."}}]}}`))
	if err != nil {
		t.Fatalf("parseViewOpened: %v", err)
	}
	if v.ID != "V0CLE5MKQF9" || v.Title != "AI Budget Support" || v.CloseLabel != "Close" {
		t.Errorf("view = %+v, want the inline view", v)
	}
	if v.SubmitLabel != "" {
		t.Errorf("submit = %q, want empty for a read-only view", v.SubmitLabel)
	}
	if len(v.Blocks.BlockSet) != 1 {
		t.Errorf("block count = %d, want 1", len(v.Blocks.BlockSet))
	}
}

func TestParseViewOpened_NoViewObject(t *testing.T) {
	v, err := parseViewOpened([]byte(`{"type":"view_opened","view_id":"V1","title":{"text":"Polly"}}`))
	if err != nil {
		t.Fatalf("parseViewOpened: %v", err)
	}
	if v.ID != "V1" || v.Title != "Polly" {
		t.Errorf("view = %+v, want V1 Polly", v)
	}
}

func TestGetView_PostsViewID(t *testing.T) {
	var gotPath string
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = r.ParseForm()
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"view":{"id":"V0CLE5MKQF9",
		  "title":{"type":"plain_text","text":"AI Budget Support"},
		  "close":{"type":"plain_text","text":"Close"},
		  "blocks":[{"type":"section","text":{"type":"mrkdwn","text":"Your AI Budget"}}]}}`))
	}))
	defer srv.Close()
	c := NewClient("xoxc-test", "d-cookie")
	pointClientAtTestServer(t, c, srv)

	v, err := c.GetView(context.Background(), "V0CLE5MKQF9")
	if err != nil {
		t.Fatalf("GetView: %v", err)
	}
	if gotPath != "/api/views.get" {
		t.Errorf("path = %q, want /api/views.get", gotPath)
	}
	if gotForm.Get("view_id") != "V0CLE5MKQF9" {
		t.Errorf("view_id = %q, want V0CLE5MKQF9", gotForm.Get("view_id"))
	}
	if v.Title != "AI Budget Support" || len(v.Blocks.BlockSet) != 1 {
		t.Errorf("view = %+v, want the served view", v)
	}
}

func TestGetView_RefusesANullView(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"view":null}`))
	}))
	defer srv.Close()
	c := NewClient("xoxc-test", "d-cookie")
	pointClientAtTestServer(t, c, srv)

	if _, err := c.GetView(context.Background(), "V1"); err == nil {
		t.Fatal("GetView accepted a null view")
	}
}
