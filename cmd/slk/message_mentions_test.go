package main

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/slack/edge"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/slack-go/slack"
)

func TestMessageMentions_CachedContent(t *testing.T) {
	for _, tc := range []struct {
		name, content string
	}{
		{"scalar body", `"text":"hi <@U123>"`},
		{"rich text user", `"blocks":[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"hi "},{"type":"user","user_id":"U123"}]}]}]`},
		{"section text", `"blocks":[{"type":"section","text":{"type":"mrkdwn","text":"hi <@U123>"}}]`},
		{"section field", `"blocks":[{"type":"section","fields":[{"type":"mrkdwn","text":"hi <@U123>"}]}]`},
		{"context text", `"blocks":[{"type":"context","elements":[{"type":"mrkdwn","text":"hi <@U123>"}]}]`},
		{"forwarded pretext", `"attachments":[{"pretext":"hi <@U123>"}]`},
		{"forwarded text", `"attachments":[{"text":"hi <@U123>"}]`},
		{"forwarded field", `"attachments":[{"fields":[{"title":"Note","value":"hi <@U123>"}]}]`},
		{"nested section text", `"attachments":[{"blocks":[{"type":"section","text":{"type":"mrkdwn","text":"hi <@U123>"}}]}]`},
		{"nested section field", `"attachments":[{"blocks":[{"type":"section","fields":[{"type":"mrkdwn","text":"hi <@U123>"}]}]}]`},
		{"nested context", `"attachments":[{"blocks":[{"type":"context","elements":[{"type":"mrkdwn","text":"hi <@U123>"}]}]}]`},
		{"missing raw JSON", ""},
		{"malformed raw JSON", "{"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newCacheForTest(t)
			if err := db.UpsertUser(cache.User{ID: "U123", WorkspaceID: "T1", DisplayName: "Alice"}); err != nil {
				t.Fatal(err)
			}
			names := newUserNameStore(map[string]string{"UAUTHOR": "Author"})
			row := cache.Message{TS: "1700000001.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "UAUTHOR", Text: "hi <@U123>", RawJSON: tc.content}
			if tc.content != "" && tc.content != "{" {
				row.RawJSON = `{"ts":"1700000001.000000","user":"UAUTHOR",` + tc.content + `}`
				var raw slack.Message
				if err := json.Unmarshal([]byte(row.RawJSON), &raw); err != nil {
					t.Fatal(err)
				}
				row.Text = raw.Text
			}
			if err := db.UpsertMessage(row); err != nil {
				t.Fatal(err)
			}
			items := loadCachedMessages(db, "USELF", "C1", names, "3:04 PM", nil)
			if len(items) != 1 {
				t.Fatalf("cached messages = %d, want 1", len(items))
			}
			if name, _ := names.Get("U123"); name != "Alice" {
				t.Fatalf("cached mention U123 did not hydrate the name store: %q", name)
			}
			app := ui.NewApp()
			app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			app.Update(ui.WorkspaceSwitchedMsg{TeamID: "T1", UserNames: names.Snapshot(), Channels: []sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}}})
			app.Update(ui.ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
			app.Update(ui.MessagesLoadedMsg{ChannelID: "C1", Messages: items})
			if frame := ansi.Strip(app.View().Content); !strings.Contains(frame, "hi @Alice") || strings.Contains(frame, "hi @U123") {
				t.Fatalf("cached mention did not render its local name:\n%s", frame)
			}
		})
	}
}

func TestMessageMentions_UnknownUser(t *testing.T) {
	for _, route := range []string{"convertAndCacheHistory", "fetchChannelMessages", "fetchThreadReplies", "enrichCachedRow", "messageItemFromEvent", "repeated mentions"} {
		t.Run(route, func(t *testing.T) {
			db := newCacheForTest(t)
			names := newUserNameStore(map[string]string{"UAUTHOR": "Author"})
			const raw = `{"ts":"1700000002.000000","user":"UAUTHOR","text":"Shared message","thread_ts":"1700000001.000000","attachments":[{"text":"hi <@U123>"}]}`
			var upstream slack.Message
			if err := json.Unmarshal([]byte(raw), &upstream); err != nil {
				t.Fatal(err)
			}
			srv := newFakeSlack(t, map[string]string{
				"/api/auth.test":             `{"ok":true,"team_id":"T1","user_id":"USELF"}`,
				"/api/conversations.history": `{"ok":true,"has_more":false,"messages":[` + raw + `]}`,
				"/api/conversations.replies": `{"ok":true,"has_more":false,"messages":[{"ts":"1700000001.000000","user":"UAUTHOR","text":"Parent","reply_count":1},` + raw + `]}`,
			})
			client := newTestClient(t, srv.Server)
			if err := client.Connect(context.Background()); err != nil {
				t.Fatal(err)
			}
			batcher := &fakeBatcher{res: []edge.User{edgeUserRecord("U123", "alice", "Alice", "", "T1", 1, false)}}
			watch := newResolvedWatch(1, nil)
			resolver := newUserResolver("T1", client, db, nil, watch.send, batcher, nil)
			resolver.names = names
			wctx := &WorkspaceContext{TeamID: "T1", UserNames: names, UserResolver: resolver}
			router := newWorkspaceRouter()
			router.Add(wctx)
			router.Set(wctx)
			// Snapshot before discovery: only the real resolver event may give
			// this App the missing name, never a later store snapshot.
			app := ui.NewApp()
			app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			app.Update(ui.WorkspaceSwitchedMsg{TeamID: "T1", UserNames: names.Snapshot(), Channels: []sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}}})
			app.Update(ui.ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
			var items []messages.MessageItem
			switch route {
			case "convertAndCacheHistory":
				items = convertAndCacheHistory(client, "C1", []slack.Message{upstream}, db, names, "3:04 PM", router)
			case "fetchChannelMessages":
				items = fetchChannelMessages(client, "C1", db, names, "3:04 PM", nil, router)
			case "fetchThreadReplies":
				items = fetchThreadReplies(client, "C1", "1700000001.000000", db, names, "3:04 PM", nil, router)
			case "enrichCachedRow":
				items = []messages.MessageItem{enrichCachedRow(db, "USELF", "C1", cache.Message{TS: upstream.Timestamp, WorkspaceID: "T1", UserID: "UAUTHOR", Text: upstream.Text, RawJSON: raw}, names, "3:04 PM", "test", router, nil)}
			default:
				if route == "repeated mentions" {
					upstream.Text = "hi <@U123> <@UAUTHOR> <@U123>"
					upstream.Attachments[0].Pretext = "hi <@U123> <@UAUTHOR>"
				}
				h := &rtmEventHandler{db: db, userNames: names, wsCtx: wctx, tsFormat: "3:04 PM"}
				items = []messages.MessageItem{h.messageItemFromEvent("UAUTHOR", "UAUTHOR", "", upstream.Timestamp, upstream.Text, "", "", false, nil, upstream.Blocks, upstream.Attachments)}
			}
			if len(items) != 1 || items[0].UserName != "Author" {
				t.Fatalf("conversion must retain a message from the known author: %+v", items)
			}
			app.Update(ui.MessagesLoadedMsg{ChannelID: "C1", Messages: items})
			if before := ansi.Strip(app.View().Content); !strings.Contains(before, "hi @U123") {
				t.Fatalf("precondition: unresolved attachment not visible:\n%s", before)
			}
			select {
			case <-watch.done:
			case <-time.After(2 * time.Second):
				t.Fatal("missing resolution for attachment-only user U123")
			}
			for _, event := range watch.messages() {
				app.Update(event)
			}
			if after := ansi.Strip(app.View().Content); !strings.Contains(after, "hi @Alice") || strings.Contains(after, "hi @U123") {
				t.Fatalf("resolved attachment not refreshed:\n%s", after)
			}
			if got := batcher.calls(); !reflect.DeepEqual(got, []map[string]int64{{"U123": 0}}) {
				t.Fatalf("profile batches = %v, want only U123 once (never known UAUTHOR)", got)
			}
		})
	}
}

func TestMessageMentions_SourceWorkspace(t *testing.T) {
	for _, route := range []string{"history", "cache"} {
		t.Run(route, func(t *testing.T) {
			db := newCacheForTest(t)
			if err := db.UpsertWorkspace(cache.Workspace{ID: "T2", Name: "Other"}); err != nil {
				t.Fatal(err)
			}
			watch := newResolvedWatch(1, nil)
			var workspaces [2]*WorkspaceContext
			var batchers [2]*fakeBatcher
			router := newWorkspaceRouter()
			for i := range workspaces {
				team := fmt.Sprintf("T%d", i+1)
				names := newUserNameStore(map[string]string{"UAUTHOR": "Author"})
				batchers[i] = &fakeBatcher{res: []edge.User{edgeUserRecord("U123", "alice", "Alice", "", team, 1, false)}}
				r := newUserResolver(team, nil, db, nil, watch.send, batchers[i], nil)
				r.names = names
				workspaces[i] = &WorkspaceContext{TeamID: team, UserNames: names, UserResolver: r}
				router.Add(workspaces[i])
			}
			router.Set(workspaces[1])
			const raw = `{"ts":"1700000001.000000","user":"UAUTHOR","text":"Shared message","attachments":[{"text":"hi <@U123>"}]}`
			if route == "history" {
				srv := newFakeSlack(t, map[string]string{"/api/auth.test": `{"ok":true,"team_id":"T1","user_id":"USELF"}`})
				client := newTestClient(t, srv.Server)
				if err := client.Connect(context.Background()); err != nil {
					t.Fatal(err)
				}
				var upstream slack.Message
				if err := json.Unmarshal([]byte(raw), &upstream); err != nil {
					t.Fatal(err)
				}
				convertAndCacheHistory(client, "C1", []slack.Message{upstream}, db, workspaces[0].UserNames, "3:04 PM", router)
			} else {
				enrichCachedRow(db, "USELF", "C1", cache.Message{TS: "1700000001.000000", WorkspaceID: "T1", UserID: "UAUTHOR", RawJSON: raw}, workspaces[0].UserNames, "3:04 PM", "test", router, nil)
			}
			select {
			case <-watch.done:
			case <-time.After(2 * time.Second):
				t.Fatal("missing source-workspace resolution for U123")
			}
			for _, event := range watch.messages() {
				if resolved, ok := event.(ui.UserResolvedMsg); ok && resolved.TeamID != "T1" {
					t.Errorf("U123 resolved in %s, want source T1", resolved.TeamID)
				}
			}
			if name, _ := workspaces[0].UserNames.Get("U123"); name != "Alice" {
				t.Errorf("source T1 name = %q, want Alice", name)
			}
			if name, _ := workspaces[1].UserNames.Get("U123"); name != "" {
				t.Errorf("active T2 learned source mention: %q", name)
			}
			if got := batchers[1].calls(); len(got) != 0 {
				t.Errorf("active T2 received profile work: %v", got)
			}
			workspaces[1].UserResolver.pendingMu.Lock()
			pending := len(workspaces[1].UserResolver.pending)
			workspaces[1].UserResolver.pendingMu.Unlock()
			if pending != 0 {
				t.Errorf("active T2 has %d queued mentions", pending)
			}
		})
	}
}

func TestMessageMentions_LiteralAndUnknownFallback(t *testing.T) {
	names := newUserNameStore(map[string]string{"UAUTHOR": "Author", "U777": "WrongLiteral", "U888": "WrongEscaped"})
	h := &rtmEventHandler{userNames: names, tsFormat: "3:04 PM"}
	item := h.messageItemFromEvent("UAUTHOR", "UAUTHOR", "", "1700000001.000000", "literal @U777 escaped &lt;@U888&gt; unresolved <@U999>", "", "", false, nil, slack.Blocks{}, nil)
	app := ui.NewApp()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app.Update(ui.WorkspaceSwitchedMsg{TeamID: "T1", UserNames: names.Snapshot(), Channels: []sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}}})
	app.Update(ui.ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	app.Update(ui.MessagesLoadedMsg{ChannelID: "C1", Messages: []messages.MessageItem{item}})
	frame := ansi.Strip(app.View().Content)
	for _, want := range []string{"literal @U777", "escaped <@U888>", "unresolved @U999"} {
		if !strings.Contains(frame, want) {
			t.Errorf("literal/unresolved fallback missing %q:\n%s", want, frame)
		}
	}
	if strings.Contains(frame, "WrongLiteral") || strings.Contains(frame, "WrongEscaped") {
		t.Fatalf("literal text was interpreted as a user:\n%s", frame)
	}
}
