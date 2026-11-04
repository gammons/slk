package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/demo"
	"github.com/gammons/slk/internal/ui"
)

// TestApplyUISettings_VersionCommandShowsReleaseBuild wires the demo
// workspace the way main wires a real one, with release-sized build
// metadata (GoReleaser stamps the full commit and an RFC 3339 date), so
// the status bar carries a channel name and the presence and connection
// pills. It types :version on an 80-column screen and expects the short
// identity, and that in this layout the frame does not grow past the
// terminal (toasts are not clipped, so a long enough channel name or
// breadcrumb still would).
func TestApplyUISettings_VersionCommandShowsReleaseBuild(t *testing.T) {
	oldVersion, oldCommit, oldDate := version, commit, date
	t.Cleanup(func() { version, commit, date = oldVersion, oldCommit, oldDate })
	version = "v1.2.3"
	commit = "0123456789abcdef0123456789abcdef01234567"
	date = "2026-01-02T03:04:05Z"

	d, err := demo.New("hero", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	app := ui.NewApp()
	applyUISettings(app, uiSettings{TimestampFormat: demo.TimestampFormat})
	d.Install(app)
	deliverDemo(t, app, tea.WindowSizeMsg{Width: 80, Height: 24}, 0)
	for _, msg := range d.StartupMsgs() {
		deliverDemo(t, app, msg, 0)
	}
	// The demo's active workspace; main delivers this from the RTM feed.
	deliverDemo(t, app, ui.StatusChangeMsg{TeamID: "T0LUMEN", Presence: "active"}, 0)
	before := ansi.Strip(app.View().Content)
	if !strings.Contains(before, "#general") || !strings.Contains(before, "● Active ● Connected") {
		t.Fatalf("precondition: the status bar should show the channel and both pills:\n%s", before)
	}

	for _, r := range ":version" {
		app.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	frame := ansi.Strip(app.View().Content)
	if !strings.Contains(frame, "v1.2.3 · 0123456") {
		t.Fatalf("status bar should show the short build identity, got:\n%s", frame)
	}
	for i, line := range strings.Split(frame, "\n") {
		if w := ansi.StringWidth(line); w > 80 {
			t.Errorf("frame line %d is %d cells wide, want at most 80:\n%s", i, w, line)
		}
	}
}
