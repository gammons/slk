package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/slack-go/slack"
)

func TestChannelMentions_ResolvePublicChannelOutsideSidebar(t *testing.T) {
	for _, route := range []string{"history", "channel fetch", "thread fetch", "cache", "live"} {
		t.Run(route, func(t *testing.T) {
			db := newCacheForTest(t)
			const raw = `{"ts":"1700000002.000000","user":"UAUTHOR","text":"Shared message","thread_ts":"1700000001.000000","attachments":[{"text":"See <#C9>"}]}`
			var upstream slack.Message
			if err := json.Unmarshal([]byte(raw), &upstream); err != nil {
				t.Fatal(err)
			}
			// Rich-text channel elements become bare mentions; Slack's scalar
			// fallback need not contain the ID that is actually rendered.
			if route == "history" {
				const rich = `{"ts":"1700000002.000000","user":"UAUTHOR","text":"See public-news","blocks":[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"See "},{"type":"channel","channel_id":"C9"}]}]}]}`
				if err := json.Unmarshal([]byte(rich), &upstream); err != nil {
					t.Fatal(err)
				}
				upstream.Attachments = nil
			}
			srv := newFakeSlack(t, map[string]string{
				"/api/auth.test":             `{"ok":true,"team_id":"T1","user_id":"USELF"}`,
				"/api/conversations.info":    `{"ok":true,"channel":{"id":"C9","name":"public-news","is_channel":true,"is_member":false}}`,
				"/api/conversations.history": `{"ok":true,"has_more":false,"messages":[` + raw + `]}`,
				"/api/conversations.replies": `{"ok":true,"has_more":false,"messages":[{"ts":"1700000001.000000","user":"UAUTHOR","text":"Parent","reply_count":1},` + raw + `]}`,
			})
			client := newTestClient(t, srv.Server)
			if err := client.Connect(context.Background()); err != nil {
				t.Fatal(err)
			}
			names := newUserNameStore(map[string]string{"UAUTHOR": "Author"})
			resolver := newChannelResolver("T1", db, client.GetConversationInfo)
			wctx := &WorkspaceContext{TeamID: "T1", UserNames: names, ChannelResolver: resolver,
				Channels: []sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}}}
			router := newWorkspaceRouter()
			router.Add(wctx)
			other := &WorkspaceContext{TeamID: "T2"}
			router.Add(other)
			router.Set(other)

			app := ui.NewApp()
			app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			channelNames, seq := resolver.names.SnapshotForUI()
			app.Update(ui.WorkspaceSwitchedMsg{TeamID: "T1", UserNames: names.Snapshot(), ChannelNames: channelNames, Channels: wctx.Channels})
			app.Update(ui.ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
			var items []messages.MessageItem
			switch route {
			case "history":
				items = convertAndCacheHistory(client, "C1", []slack.Message{upstream}, db, names, "3:04 PM", router)
			case "channel fetch":
				items = fetchChannelMessages(client, "C1", db, names, "3:04 PM", nil, router)
			case "thread fetch":
				items = fetchThreadReplies(client, "C1", "1700000001.000000", db, names, "3:04 PM", nil, router)
			case "cache":
				if err := db.UpsertMessage(cache.Message{TS: upstream.Timestamp, ChannelID: "C1", WorkspaceID: "T1", UserID: "UAUTHOR", Text: upstream.Text, RawJSON: raw}); err != nil {
					t.Fatal(err)
				}
				items = loadCachedMessages(db, "USELF", "C1", names, "3:04 PM", router)
			case "live":
				h := &rtmEventHandler{db: db, userNames: names, wsCtx: wctx, tsFormat: "3:04 PM"}
				items = []messages.MessageItem{h.messageItemFromEvent("UAUTHOR", "UAUTHOR", "", upstream.Timestamp, upstream.Text, "", "", false, nil, upstream.Blocks, upstream.Attachments)}
			}
			app.Update(ui.MessagesLoadedMsg{ChannelID: "C1", Messages: items})
			if before := ansi.Strip(app.View().Content); !strings.Contains(before, "See #unknown") {
				t.Fatalf("precondition: unresolved channel mention not visible:\n%s", before)
			}
			// Install only after the old frame was rendered. This also exercises
			// resolutions completing between a workspace snapshot and activation.
			events := make(chan tea.Msg, 1)
			resolver.names.NotifyFrom(seq, uiChannelNameNotifier("T1", func(msg tea.Msg) { events <- msg }))
			select {
			case event := <-events:
				app.Update(event)
			case <-time.After(2 * time.Second):
				t.Fatal("channel mention never resolved through the source workspace")
			}
			if after := ansi.Strip(app.View().Content); !strings.Contains(after, "See #public-news") || strings.Contains(after, "See #unknown") {
				t.Fatalf("public channel mention did not refresh in place:\n%s", after)
			}
			if len(wctx.Channels) != 1 || len(wctx.FinderItems) != 0 {
				t.Fatalf("resolving a mention changed conversation membership: %+v", wctx.Channels)
			}
			if ch, err := db.GetChannel("C9"); err != nil || ch.Name != "public-news" || ch.IsMember {
				t.Fatalf("resolved channel metadata = %+v, %v; want unjoined public-news", ch, err)
			}
		})
	}
}
