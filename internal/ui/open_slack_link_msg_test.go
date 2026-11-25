package ui

import (
	"testing"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
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
