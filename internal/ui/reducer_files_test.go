package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/messages"
)

func TestDownloadFileMsg_NoDownloader_Toasts(t *testing.T) {
	app := NewApp()
	att := messages.Attachment{Kind: "file", Name: "a.csv", DownloadURL: "https://x"}
	_, cmd := app.Update(DownloadFileMsg{Attachment: att})
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	msg, ok := cmd().(ToastMsg)
	if !ok {
		t.Fatalf("expected ToastMsg, got %#v", cmd())
	}
	if msg.Text != "File downloads unavailable" {
		t.Errorf("toast = %q", msg.Text)
	}
}

func TestDownloadFileMsg_ShowsProgressUntilResult(t *testing.T) {
	app := newTestApp(t)
	app.SetFileService(core.NewFileService(nil, func(context.Context, string, string) (string, error) {
		return "/tmp/a.csv", nil
	}))
	app.SetDesktopService(core.NewDesktopService(core.DesktopServiceFuncs{
		Open: func(string) error { return nil },
	}))
	att := messages.Attachment{Kind: "file", Name: "a.csv", DownloadURL: "https://x"}

	_, cmd := app.Update(DownloadFileMsg{Attachment: att})
	if got := statusbarText(app); got == "" || !strings.Contains(got, "Downloading a.csv") {
		t.Fatalf("status bar = %q, want download progress", got)
	}
	if cmd == nil {
		t.Fatal("expected download command")
	}
	result, ok := cmd().(ToastMsg)
	if !ok {
		t.Fatalf("download command returned %T, want ToastMsg", cmd())
	}
	app.Update(result)
	if got := statusbarText(app); !strings.Contains(got, "Downloaded a.csv") {
		t.Errorf("status bar = %q, want download result", got)
	}
}
