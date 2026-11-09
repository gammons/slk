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
	msg, ok := cmd().(downloadFileResultMsg)
	if !ok {
		t.Fatalf("expected downloadFileResultMsg, got %#v", cmd())
	}
	app.Update(msg)
	if got := statusbarText(app); !strings.Contains(got, "File downloads unavailable") {
		t.Errorf("toast = %q", got)
	}
}

func TestDownloadFileMsg_OverlappingDownloadsKeepProgressVisible(t *testing.T) {
	app := newTestApp(t)
	app.SetFileService(core.NewFileService(nil, func(context.Context, string, string) (string, error) {
		return "/tmp/download", nil
	}))
	app.SetDesktopService(core.NewDesktopService(core.DesktopServiceFuncs{
		Open: func(string) error { return nil },
	}))

	_, first := app.Update(DownloadFileMsg{Attachment: messages.Attachment{Name: "a.csv", DownloadURL: "https://x/a"}})
	_, second := app.Update(DownloadFileMsg{Attachment: messages.Attachment{Name: "b.csv", DownloadURL: "https://x/b"}})
	if first == nil || second == nil {
		t.Fatal("expected both download commands")
	}

	app.Update(first())
	if got := statusbarText(app); !strings.Contains(got, "Downloading b.csv") {
		t.Errorf("status bar = %q, want remaining download progress after a.csv completes", got)
	}
	if got := statusbarText(app); !strings.Contains(got, "Downloaded a.csv") {
		t.Errorf("status bar = %q, want completed download result to remain visible", got)
	}

	app.Update(second())
	if got := statusbarText(app); !strings.Contains(got, "Downloaded b.csv") {
		t.Errorf("status bar = %q, want final download result", got)
	}
	if len(app.pendingDownloads) != 0 {
		t.Errorf("pending downloads = %v, want none", app.pendingDownloads)
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
	result, ok := cmd().(downloadFileResultMsg)
	if !ok {
		t.Fatalf("download command returned %T, want downloadFileResultMsg", cmd())
	}
	app.Update(result)
	if got := statusbarText(app); !strings.Contains(got, "Downloaded a.csv") {
		t.Errorf("status bar = %q, want download result", got)
	}
}
