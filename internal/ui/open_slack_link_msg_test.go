package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	imgpkg "github.com/gammons/slk/internal/image"
)

func TestOpenSlackLinkMsg_OpensThreadFromAnyMode(t *testing.T) {
	for _, mode := range []Mode{ModeNormal, ModeInsert, ModeChannelFinder, ModeOpenSlackLink} {
		t.Run(mode.String(), func(t *testing.T) {
			app, opened := finderPermalinkApp(t)
			var fetchedThread string
			app.setThreadFetcherForTest(func(_ ids.ChannelID, threadTS ids.ThreadTS) core.Msg {
				fetchedThread = string(threadTS)
				return nil
			})
			app.SetMode(mode)
			if mode == ModeOpenSlackLink {
				app.slackLinkInput = "half-typed"
			}

			const url = "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139?thread_ts=1779284700.000100"
			_, cmd := app.Update(OpenSlackLinkMsg{URL: url})
			drainCmd(cmd)

			if app.mode != ModeNormal {
				t.Errorf("mode = %v, want normal", app.mode)
			}
			if app.slackLinkInput != "" {
				t.Errorf("dialog input = %q, want it cleared", app.slackLinkInput)
			}
			if !app.threadVisible || fetchedThread != "1779284700.000100" {
				t.Errorf("thread visible=%v fetched=%q", app.threadVisible, fetchedThread)
			}
			if *opened != "" {
				t.Errorf("browser opened %q", *opened)
			}
		})
	}
}

func TestOpenSlackLinkMsg_UnknownWorkspaceOpensBrowser(t *testing.T) {
	app, opened := finderPermalinkApp(t)
	app.SetMode(ModeNormal)
	const url = "https://elsewhere.slack.com/archives/C054JFCBN69/p1779284733270139"
	_, cmd := app.Update(OpenSlackLinkMsg{URL: url})
	drainCmd(cmd)
	if *opened != url {
		t.Fatalf("browser opened %q, want %q", *opened, url)
	}
}

func TestOpenSlackLinkMsg_ClosesConfirmPromptAndPreview(t *testing.T) {
	const url = "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139?thread_ts=1779284700.000100"
	for _, tc := range []struct {
		name  string
		setup func(app *App, confirmed *bool)
	}{
		{"confirm prompt", func(app *App, confirmed *bool) {
			app.openConfirmPrompt("Open link?", "Open https://example.com?", func() tea.Msg {
				*confirmed = true
				return nil
			})
		}},
		{"image preview", func(app *App, _ *bool) {
			p := imgpkg.NewPreview(imgpkg.PreviewInput{Name: "x.png"})
			app.preview.Open(&p, "C054JFCBN69", "1779284733.270139", 0)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, opened := finderPermalinkApp(t)
			app.setThreadFetcherForTest(func(ids.ChannelID, ids.ThreadTS) core.Msg { return nil })
			app.SetMode(ModeNormal)
			var confirmed bool
			tc.setup(app, &confirmed)

			_, cmd := app.Update(OpenSlackLinkMsg{URL: url})
			drainCmd(cmd)

			if app.confirmPrompt.IsVisible() {
				t.Error("confirm prompt still visible")
			}
			if app.preview.Active() {
				t.Error("image preview still active")
			}
			if app.mode != ModeNormal {
				t.Errorf("mode = %v, want normal", app.mode)
			}
			if !app.threadVisible {
				t.Error("thread not opened")
			}
			// The dismissed prompt is declined, and a key that would have
			// answered it now goes to normal mode instead.
			app.Update(keyCode(tea.KeyEnter))
			if confirmed {
				t.Error("dismissed prompt ran its confirm action")
			}
			if *opened != "" {
				t.Errorf("browser opened %q", *opened)
			}
		})
	}
}
