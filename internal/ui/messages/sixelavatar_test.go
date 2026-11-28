package messages

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// With sixel capture on, a sixel avatar is recorded as an image at the
// avatar's cells (row 0, right of the border) and the text there is
// blank, so the halfblock avatar does not show under the painted one.
// The halfblock avatar is the image's fallback.
func TestRenderMessage_SixelAvatarIsAnImage(t *testing.T) {
	msg := MessageItem{TS: "1700000000.000000", UserName: "lola", UserID: "U1", Text: "hello there", Timestamp: "11:08 AM"}
	m := New([]MessageItem{msg}, "general")
	r := RenderMessage(msg, 100, m.renderContext(), RenderOptions{
		Avatar: "AVAV\nAVAV", AvatarSixel: []byte("\x1bPq#0~-\x1b\\"), AvatarSixelKey: "avatar-U1-abc", SixelRows: true,
	})
	if len(r.SixelRows) != 1 {
		t.Fatalf("SixelRows = %d images, want the avatar's one", len(r.SixelRows))
	}
	got := r.SixelRows[0]
	if got.row != 0 || got.col != 1 || got.width != 4 || got.height != 2 || got.key != "avatar-U1-abc" {
		t.Errorf("avatar image = %+v, want row 0 col 1, 4x2, key avatar-U1-abc", got)
	}
	if !got.margin {
		t.Error("avatar image does not carry the blank-gutter margin flag")
	}
	if strings.Join(got.fallback, "\n") != "AVAV\nAVAV" {
		t.Errorf("fallback = %q, want the halfblock avatar", got.fallback)
	}
	if strings.Contains(ansi.Strip(r.Content), "AVAV") {
		t.Errorf("content still draws the halfblock avatar under the image:\n%s", ansi.Strip(r.Content))
	}
	if !strings.Contains(ansi.Strip(r.Content), "hello there") {
		t.Errorf("content lost the message body:\n%s", ansi.Strip(r.Content))
	}
}

// Without sixel capture (the draft preview), or without a sixel avatar,
// the halfblock avatar is drawn as text and nothing is recorded.
func TestRenderMessage_SixelAvatarNeedsCaptureAndBytes(t *testing.T) {
	msg := MessageItem{TS: "1700000000.000000", UserName: "lola", UserID: "U1", Text: "hello there", Timestamp: "11:08 AM"}
	m := New([]MessageItem{msg}, "general")
	for name, opt := range map[string]RenderOptions{
		"no capture": {Avatar: "AVAV\nAVAV", AvatarSixel: []byte("x"), AvatarSixelKey: "k"},
		"no bytes":   {Avatar: "AVAV\nAVAV", SixelRows: true},
	} {
		t.Run(name, func(t *testing.T) {
			r := RenderMessage(msg, 100, m.renderContext(), opt)
			if len(r.SixelRows) != 0 {
				t.Errorf("SixelRows = %+v, want none", r.SixelRows)
			}
			if !strings.Contains(ansi.Strip(r.Content), "AVAV") {
				t.Errorf("halfblock avatar missing:\n%s", ansi.Strip(r.Content))
			}
		})
	}
}

// The channel pane places the sixel avatar of a message in view, at the
// avatar's cells, through the same pipeline as its images.
func TestView_SixelAvatarIsPlaced(t *testing.T) {
	msg := MessageItem{TS: "1700000000.000000", UserName: "lola", UserID: "U1", Text: "hello there", Timestamp: "11:08 AM"}
	m := New([]MessageItem{msg}, "general")
	m.SetAvatarFunc(func(string) string { return "AVAV\nAVAV" })
	m.SetSixelAvatarFunc(func(userID string) (string, []byte, bool) {
		return "avatar-" + userID, []byte("\x1bPq#0~-\x1b\\"), true
	})
	_ = m.View(30, 100)
	got := m.SixelPlacements()
	if len(got) != 1 {
		t.Fatalf("SixelPlacements() = %+v, want the avatar's one", got)
	}
	if got[0].Col != 1 || got[0].Cols != 4 || got[0].Rows != 2 {
		t.Errorf("avatar placement = %+v, want col 1, 4x2", got[0])
	}
	if got[0].MarginSGR != SelectionTintBgANSI(false) {
		t.Errorf("selected avatar MarginSGR = %q, want the selection tint %q", got[0].MarginSGR, SelectionTintBgANSI(false))
	}
}

// A message that mentions the user paints its avatar gutter on the
// mention tint; the sixel margin must be repainted with the same colour.
func TestView_SixelAvatarMarginUsesMentionTint(t *testing.T) {
	msgs := []MessageItem{
		{TS: "1700000000.000000", UserName: "lola", UserID: "U1", Text: "hello <@U9>", Timestamp: "11:08 AM"},
		{TS: "1700000001.000000", UserName: "max", UserID: "U2", Text: "second", Timestamp: "11:09 AM"},
	}
	m := New(msgs, "general")
	m.SetCurrentUser("U9")
	m.SetAvatarFunc(func(string) string { return "AVAV\nAVAV" })
	m.SetSixelAvatarFunc(func(userID string) (string, []byte, bool) {
		return "avatar-" + userID, []byte("\x1bPq#0~-\x1b\\"), true
	})
	_ = m.View(30, 100)
	got := m.SixelPlacements()
	if len(got) != 2 {
		t.Fatalf("SixelPlacements() = %+v, want both avatars", got)
	}
	if got[0].MarginSGR != MentionTintBgANSI() {
		t.Errorf("mention avatar MarginSGR = %q, want the mention tint %q", got[0].MarginSGR, MentionTintBgANSI())
	}
}

// Hiding the cursor drops the selection tint without moving the cursor
// index. The avatar keeps its normal margin then.
func TestView_SixelAvatarMarginKeptWhenCursorHidden(t *testing.T) {
	msg := MessageItem{TS: "1700000000.000000", UserName: "lola", UserID: "U1", Text: "hello", Timestamp: "11:08 AM"}
	m := New([]MessageItem{msg}, "general")
	m.SetAvatarFunc(func(string) string { return "AVAV\nAVAV" })
	m.SetSixelAvatarFunc(func(userID string) (string, []byte, bool) {
		return "avatar-" + userID, []byte("\x1bPq#0~-\x1b\\"), true
	})
	m.SetCursorHighlight(false)
	_ = m.View(30, 100)
	got := m.SixelPlacements()
	if len(got) != 1 {
		t.Fatalf("SixelPlacements() = %+v, want the avatar's one", got)
	}
	if got[0].MarginSGR != BgANSI() {
		t.Errorf("MarginSGR = %q with the cursor hidden, want the theme background %q", got[0].MarginSGR, BgANSI())
	}
}
