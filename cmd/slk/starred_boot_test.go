package main

import (
	"context"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/bootstrap"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/service"
	"github.com/gammons/slk/internal/slackhttp"
	"github.com/gammons/slk/internal/ui"
)

func TestStarredBoot_ReconnectReplacesMembership(t *testing.T) {
	// The Grid response carries string IDs even when stars.list is restricted.
	// Object entries below are synthetic compatibility probes, not a captured
	// userBoot schema, and must not be interpreted as conversation IDs.
	bodies := map[string]string{
		"/api/auth.test":                  `{"ok":true,"team_id":"T1","user_id":"USELF"}`,
		"/api/stars.list":                 `{"ok":false,"error":"enterprise_is_restricted"}`,
		"/api/client.userBoot":            `{"ok":true,"starred":["C_STAR_A",{"type":"channel","channel":"C_STAR_B"}],"channels":[{"id":"C_STAR_A","name":"starred-alpha"},{"id":"C_STAR_B","name":"starred-beta"},{"id":"C_OPEN","name":"open"}]}`,
		"/api/client.counts":              `{"ok":true,"channels":[],"ims":[],"mpims":[],"threads":{"has_unreads":false}}`,
		"/api/users.channelSections.list": `{"ok":true,"channel_sections":[{"channel_section_id":"ST","type":"stars","next_channel_section_id":"CH","channel_ids_page":{"channel_ids":[],"count":0}},{"channel_section_id":"CH","name":"Channels","type":"channels","channel_ids_page":{"channel_ids":[],"count":0}}],"cursor":""}`,
		"/api/users.getPresence":          `{"ok":true,"presence":"active"}`,
		"/api/dnd.info":                   `{"ok":true,"dnd_enabled":false}`,
	}
	srv := newFakeSlack(t, bodies)
	client := newTestClient(t, srv.Server)
	// Real socket I/O would prevent synctest from advancing time. Keep the
	// real client and shared fixture handler, but dispatch requests in-process.
	client.HTTPClient().Transport.(*slackhttp.BrowserTransport).Inner = handlerTransport{srv.Config.Handler}

	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		if err := client.Connect(ctx); err != nil {
			t.Fatal(err)
		}
		booted, err := bootstrap.Run(ctx, bootstrap.Deps{WorkspaceID: "T1", Boot: bootAdapter{client}, Counts: countsAdapter{client}})
		if err != nil {
			t.Fatal(err)
		}
		store := service.NewSectionStore()
		if err := store.Bootstrap(ctx, bootSectionsClient{client, booted.Starred}); err != nil {
			t.Fatal(err)
		}
		wctx := &WorkspaceContext{
			TeamID: "T1", UserID: "USELF", Client: client,
			UserNames: newUserNameStore(nil), SectionStore: store,
		}
		for _, channel := range bootConversations(booted) {
			item, _ := buildChannelItem(channel, wctx, config.Default(), "T1")
			wctx.Channels = append(wctx.Channels, item)
		}
		app := ui.NewApp()
		app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		app.Update(ui.WorkspaceSwitchedMsg{
			TeamID: "T1", Channels: slices.Clone(wctx.Channels),
			SectionsProvider: sectionsProviderAdapter{store},
		})
		// Keep the content pane's header and composer distinct from both
		// channels whose sidebar membership we inspect below.
		app.Update(ui.ChannelSelectedMsg{ID: "C_OPEN", Name: "open", Type: "channel"})

		assertStarred := func(starID, starName, otherID, otherName string) {
			t.Helper()
			if section, ok := store.SectionForChannel(starID); !ok || section != "ST" {
				t.Errorf("%s section = %q (found %t), want ST", starID, section, ok)
			}
			if section, ok := store.SectionForChannel(otherID); ok && section == "ST" {
				t.Errorf("%s retained obsolete Starred membership", otherID)
			}
			frame := ansi.Strip(app.View().Content)
			_, fromStarred, found := strings.Cut(frame, "Starred")
			if !found {
				t.Fatalf("sidebar has no Starred section:\n%s", frame)
			}
			starred, ordinary, found := strings.Cut(fromStarred, "Channels")
			if !found {
				t.Fatalf("sidebar has no Channels section after Starred:\n%s", frame)
			}
			if !strings.Contains(starred, starName) || strings.Contains(starred, otherName) ||
				!strings.Contains(ordinary, otherName) || strings.Contains(ordinary, starName) {
				t.Errorf("want %s under Starred and %s under Channels:\n%s", starName, otherName, frame)
			}
		}
		assertRequests := func(wantBootCalls int) {
			t.Helper()
			srv.mu.Lock()
			defer srv.mu.Unlock()
			bootCalls, starsListCalls := 0, 0
			for _, request := range srv.reqs {
				switch request.path {
				case "/api/client.userBoot":
					bootCalls++
				case "/api/stars.list":
					starsListCalls++
				}
			}
			if starsListCalls != 0 || bootCalls != wantBootCalls {
				t.Errorf("membership requests: boot=%d stars.list=%d, want boot=%d stars.list=0", bootCalls, starsListCalls, wantBootCalls)
			}
		}
		assertStarred("C_STAR_A", "starred-alpha", "C_STAR_B", "starred-beta")
		assertRequests(1) // Startup reuses the existing boot snapshot.

		notices := make(chan tea.Msg, 16)
		h := &rtmEventHandler{
			wsCtx: wctx, workspaceID: "T1", connected: true,
			sectionRefreshAt: time.Now(),
			program:          sendFunc(func(msg tea.Msg) { notices <- msg }),
		}
		// Expire both OnConnect's gate and SectionStore's debounce without
		// sleeping in real time or reaching into the store's private state.
		time.Sleep(31 * time.Second)
		bodies["/api/client.userBoot"] = `{"ok":true,"starred":[{"type":"channel","channel":"C_STAR_A"},"C_STAR_B"],"channels":[{"id":"C_STAR_A","name":"starred-alpha"},{"id":"C_STAR_B","name":"starred-beta"},{"id":"C_OPEN","name":"open"}]}`
		h.OnConnect()
		synctest.Wait()
		for len(notices) > 0 {
			app.Update(<-notices)
		}

		assertStarred("C_STAR_B", "starred-beta", "C_STAR_A", "starred-alpha")
		assertRequests(2) // Reconnect fetches a fresh boot snapshot.
	})
}
