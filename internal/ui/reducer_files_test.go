package ui

import (
	"testing"

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
