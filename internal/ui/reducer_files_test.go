package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/statusbar"
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
	if !msg.Failed {
		t.Fatal("unavailable downloader result must be marked failed")
	}
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
	if got := statusbarText(app); strings.Contains(got, "Downloaded a.csv") {
		t.Errorf("status bar = %q, want only the remaining download progress", got)
	}

	app.Update(second())
	if got := statusbarText(app); !strings.Contains(got, "Downloaded b.csv") {
		t.Errorf("status bar = %q, want final download result", got)
	}
	if len(app.pendingDownloads) != 0 {
		t.Errorf("pending downloads = %v, want none", app.pendingDownloads)
	}
}

func TestDownloadFileMsg_OverlappingFailuresAreShownAfterDownloadsFinish(t *testing.T) {
	oldToastTick := toastTick
	toastTick = func(_ time.Duration, tick func(time.Time) tea.Msg) tea.Cmd {
		return func() tea.Msg { return tick(time.Time{}) }
	}
	t.Cleanup(func() { toastTick = oldToastTick })

	app := newTestApp(t)
	app.SetFileService(core.NewFileService(nil, func(context.Context, string, string) (string, error) {
		return "", errors.New("download failed")
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
		t.Fatalf("status bar = %q, want remaining download progress", got)
	}
	if got := statusbarText(app); strings.Contains(got, "Download failed") {
		t.Fatalf("status bar = %q, want failure deferred until downloads finish", got)
	}

	_, clearFirst := app.Update(second())
	if got := statusbarText(app); !strings.Contains(got, "Download failed: a.csv") {
		t.Fatalf("status bar = %q, want first failure after downloads finish", got)
	}
	if got := statusbarText(app); strings.Contains(got, "Download failed: b.csv") {
		t.Fatalf("status bar = %q, want failures shown one at a time", got)
	}
	if clearFirst == nil {
		t.Fatal("expected first failure clear command")
	}

	_, copyClear := app.Update(statusbar.CopiedMsg{N: 1})
	if got := statusbarText(app); !strings.Contains(got, "Copied 1 chars") {
		t.Fatalf("status bar = %q, want newer copy toast", got)
	}
	_, retryFirst := app.Update(clearFirst())
	if retryFirst == nil {
		t.Fatal("stale failure timer should re-arm behind the newer toast")
	}
	if got := statusbarText(app); !strings.Contains(got, "Copied 1 chars") {
		t.Fatalf("status bar = %q, stale failure timer replaced the newer toast", got)
	}

	app.Update(copyClear())
	_, retryAfterCopy := app.Update(retryFirst())
	if retryAfterCopy == nil {
		t.Fatal("failure timer should re-arm after the newer toast clears")
	}
	_, clearSecond := app.Update(retryAfterCopy())
	if got := statusbarText(app); !strings.Contains(got, "Download failed: b.csv") {
		t.Fatalf("status bar = %q, want second failure after re-armed timer", got)
	}
	if clearSecond == nil {
		t.Fatal("expected second failure clear command")
	}

	app.Update(clearSecond())
	if got := statusbarText(app); strings.Contains(got, "Download failed") {
		t.Errorf("status bar = %q, want failure queue drained", got)
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
