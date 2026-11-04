package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/demo"
	"github.com/gammons/slk/internal/ui"
)

// TestApplyUISettings_StayInInsertAfterSend wires the demo workspace the way
// main wires a real one, sends a message from #general, and reads the mode
// off the status bar: INSERT with the option on, NORMAL with it off.
func TestApplyUISettings_StayInInsertAfterSend(t *testing.T) {
	for _, tc := range []struct {
		stay bool
		want string
	}{
		{stay: true, want: "INSERT"},
		{stay: false, want: "NORMAL"},
	} {
		d, err := demo.New("hero", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		app := ui.NewApp()
		applyUISettings(app, uiSettings{TimestampFormat: demo.TimestampFormat, StayInInsertAfterSend: tc.stay})
		d.Install(app)
		deliverDemo(t, app, tea.WindowSizeMsg{Width: 120, Height: 40}, 0)
		for _, msg := range d.StartupMsgs() {
			deliverDemo(t, app, msg, 0)
		}

		for _, r := range "ihello" {
			app.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
		app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

		frame := ansi.Strip(app.View().Content)
		if !strings.Contains(frame, tc.want) {
			t.Errorf("stay=%v: status bar should show %s after a send:\n%s", tc.stay, tc.want, frame)
		}
	}
}

// TestUISettingsFromConfig_StayInInsertAfterSend reads the option from a
// config file the way run() does: unset means off.
func TestUISettingsFromConfig_StayInInsertAfterSend(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{"unset", "", false},
		{"on", "[compose]\nstay_in_insert_after_send = true\n", true},
		{"off", "[compose]\nstay_in_insert_after_send = false\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := uiSettingsFromConfig(cfg).StayInInsertAfterSend; got != tc.want {
				t.Errorf("StayInInsertAfterSend = %v, want %v", got, tc.want)
			}
		})
	}
}
