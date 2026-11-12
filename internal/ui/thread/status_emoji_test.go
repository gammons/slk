package thread

import (
	"bytes"
	stdimage "image"
	"io"
	"strings"
	"testing"

	emojiutil "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/peerstatus"
)

// A reply author's status emoji is drawn as an image through the shared
// RenderMessage, and its upload fires from the thread pane's View.
func TestReplyAuthorStatusEmojiAsImage(t *testing.T) {
	emojiutil.SetImageMode(true, 2)
	t.Cleanup(func() { emojiutil.SetImageMode(false, 2) })
	var kitty bytes.Buffer
	prev := imgpkg.KittyOutput
	imgpkg.KittyOutput = &kitty
	t.Cleanup(func() { imgpkg.KittyOutput = prev })

	url, _ := emojiutil.URLForShortcode("palm_tree", nil)
	placement := "\x1b[38;2;0;0;61m\U0010EEEE̅̅\U0010EEEE̅̍\x1b[39m"
	f := newFakePlaceFetcher()
	f.setPrerendered(emojiutil.EmojiCacheKey(url), stdimage.Pt(2, 1), imgpkg.Render{
		Cells: stdimage.Pt(2, 1),
		Lines: []string{placement},
		OnFlush: func(w io.Writer) error {
			_, err := io.WriteString(w, "<upload palm_tree>")
			return err
		},
	})

	m := New()
	m.SetEmojiContext(messages.EmojiContext{PlaceCtx: emojiutil.PlaceContext{Fetcher: f}, Cells: 2})
	m.SetUserStatuses(map[string]peerstatus.Status{"U2": {Emoji: ":palm_tree:", Text: "Vacation"}})
	m.SetThread(
		messages.MessageItem{TS: "199.0", UserID: "U1", UserName: "alice", Text: "parent", Timestamp: "10:00 AM"},
		[]messages.MessageItem{{TS: "200.0", UserID: "U2", UserName: "bob", Text: "reply", Timestamp: "10:01 AM"}},
		"C1", "199.0",
	)
	out := m.View(24, 80)
	if !strings.Contains(out, placement) {
		t.Errorf("reply author status emoji not drawn as an image:\n%q", out)
	}
	if strings.Contains(out, "🌴") {
		t.Errorf("reply header still draws the glyph:\n%q", out)
	}
	if got := strings.Count(kitty.String(), "<upload palm_tree>"); got != 1 {
		t.Errorf("status image uploaded %d times from View, want 1", got)
	}
}

// The parent is rendered outside the reply cache, and its uploads go
// out with the replies'. A status image only the parent shows (no reply
// by the same author) still reaches the terminal.
func TestParentAuthorStatusEmojiUploaded(t *testing.T) {
	emojiutil.SetImageMode(true, 2)
	t.Cleanup(func() { emojiutil.SetImageMode(false, 2) })
	var kitty bytes.Buffer
	prev := imgpkg.KittyOutput
	imgpkg.KittyOutput = &kitty
	t.Cleanup(func() { imgpkg.KittyOutput = prev })

	customs := map[string]string{"company-logo": "https://emoji.slack-edge.com/T1/company-logo/abc.png"}
	placement := "\x1b[38;2;0;0;62m\U0010EEEE̅̅\U0010EEEE̅̍\x1b[39m"
	f := newFakePlaceFetcher()
	f.setPrerendered(emojiutil.EmojiCacheKey(customs["company-logo"]), stdimage.Pt(2, 1), imgpkg.Render{
		Cells: stdimage.Pt(2, 1),
		Lines: []string{placement},
		OnFlush: func(w io.Writer) error {
			_, err := io.WriteString(w, "<upload company-logo>")
			return err
		},
	})

	// With a reply, and with none ("No replies yet" takes another path).
	for _, replies := range [][]messages.MessageItem{
		{{TS: "200.0", UserID: "U2", UserName: "bob", Text: "reply", Timestamp: "10:01 AM"}},
		nil,
	} {
		kitty.Reset()
		m := New()
		m.SetEmojiContext(messages.EmojiContext{PlaceCtx: emojiutil.PlaceContext{Fetcher: f}, Cells: 2, Customs: customs})
		m.SetUserStatuses(map[string]peerstatus.Status{"U1": {Emoji: ":company-logo:"}})
		m.SetThread(
			messages.MessageItem{TS: "199.0", UserID: "U1", UserName: "alice", Text: "parent", Timestamp: "10:00 AM"},
			replies, "C1", "199.0",
		)
		out := m.View(24, 80)
		if !strings.Contains(out, placement) {
			t.Fatalf("%d replies: parent author status emoji not drawn as an image:\n%q", len(replies), out)
		}
		if got := strings.Count(kitty.String(), "<upload company-logo>"); got != 1 {
			t.Errorf("%d replies: parent status image uploaded %d times from View, want 1", len(replies), got)
		}
	}
}
