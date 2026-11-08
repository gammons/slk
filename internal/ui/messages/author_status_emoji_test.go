package messages

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/url"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	emojiutil "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/peerstatus"
)

func TestAuthorStatusEmojiUsesMessageImage(t *testing.T) {
	t.Setenv("TMUX", "")
	emojiutil.SetImageMode(true, 2)
	t.Cleanup(func() { emojiutil.SetImageMode(false, 2) })
	for _, tc := range []struct {
		name, body, url string
		status          peerstatus.Status
		customs         map[string]string
	}{
		{"same emoji in body", "📟", emojiutil.CDNBaseURL + "1f4df.png", peerstatus.Status{Emoji: ":pager:", Text: "On call"}, nil},
		{"status only", "On call", emojiutil.CDNBaseURL + "1f4df.png", peerstatus.Status{Emoji: ":pager:", Text: "On call"}, nil},
		{"custom status", "On call", "https://example.test/custom-pager.png", peerstatus.Status{Emoji: ":pager:", Text: "On call"}, map[string]string{"pager": "https://example.test/custom-pager.png"}},
		{"unknown status fallback", "On call", emojiutil.CDNBaseURL + "1f4ac.png", peerstatus.Status{Emoji: ":unknown_status_fixture:", Text: "On call"}, nil},
		{"huddle takes priority", "On call", emojiutil.CDNBaseURL + "1f3a7.png", peerstatus.Status{Emoji: ":pager:", Huddle: peerstatus.HuddleActive}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url := tc.url
			key := emojiutil.EmojiCacheKey(url)
			src, err := png.Decode(bytes.NewReader(makeTestPNG(2, 2)))
			if err != nil {
				t.Fatal(err)
			}
			kitty := imgpkg.NewKittyRenderer(imgpkg.NewRegistry())
			kitty.SetSource(key, src)
			placed := kitty.RenderKey(key, image.Pt(2, 1))
			fetcher := newFakePlaceFetcher()
			fetcher.setPrerendered(key, image.Pt(2, 1), placed)
			msg := MessageItem{TS: "1.0", UserID: "U_FIXTURE", UserName: "fixture", Text: tc.body}
			m := New([]MessageItem{msg}, "fixture")
			m.SetUserStatuses(map[string]peerstatus.Status{"U_FIXTURE": tc.status})
			m.SetEmojiContext(EmojiContext{PlaceCtx: emojiutil.PlaceContext{Fetcher: fetcher}, Cells: 2, Customs: tc.customs})
			rendered := RenderMessage(msg, 80, m.renderContext(), RenderOptions{})
			if !strings.ContainsRune(rendered.Header, imgpkg.PlaceholderRune) || strings.Contains(rendered.Header, "📟") {
				t.Fatalf("profile-status pager did not use the message image: %q", rendered.Header)
			}
			height := lipgloss.Height(rendered.Content)
			canvas := lipgloss.NewCanvas(80, height)
			canvas.Compose(lipgloss.NewLayer(rendered.Content))
			for y := range height {
				for x := range 80 {
					cell := canvas.CellAt(x, y)
					if cell == nil || !strings.HasPrefix(cell.Content, string(imgpkg.PlaceholderRune)) {
						continue
					}
					if cell.Style.Fg == nil {
						t.Fatal("pager image placement lost its image ID")
					}
					r, g, b, _ := cell.Style.Fg.RGBA()
					if gotID := (r>>8)<<16 | (g>>8)<<8 | b>>8; gotID != placed.ID {
						t.Fatalf("pager references image %d, want %d", gotID, placed.ID)
					}
				}
			}
			var uploads bytes.Buffer
			for _, flush := range rendered.BodyFlushes {
				if err := flush(&uploads); err != nil {
					t.Fatal(err)
				}
			}
			if want := fmt.Sprintf("\x1b_Ga=T,f=100,t=d,i=%d,", placed.ID); !strings.Contains(uploads.String(), want) {
				t.Fatalf("profile pager was not uploaded for its placement: %q", uploads.String())
			}
		})
	}
}

func TestAuthorStatusEmojiFetchFailurePreservesGlyph(t *testing.T) {
	t.Setenv("TMUX", "")
	emojiutil.SetImageMode(true, 2)
	t.Cleanup(func() { emojiutil.SetImageMode(false, 2) })
	for _, tc := range []struct {
		name, shortcode string
	}{
		{"standard glyph", "pager"},
		{"custom status fallback", "team_status_fixture"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Each run needs its own failed URL: Place's negative cache is
			// process-scoped, and must not poison other image tests.
			imageURL := (&url.URL{Scheme: "https", Host: "example.test", Path: t.TempDir() + "/status.png"}).String()
			fetcher := newFakePlaceFetcher()
			fetcher.err = errors.New("synthetic emoji download failure")
			ready := make(chan any, 1)
			status := peerstatus.Status{Emoji: ":" + tc.shortcode + ":", Text: "On call"}
			msg := MessageItem{TS: "1.0", UserID: "U_FIXTURE", UserName: "fixture", Text: "Message"}
			m := New([]MessageItem{msg}, "fixture")
			m.SetUserStatuses(map[string]peerstatus.Status{"U_FIXTURE": status})
			m.SetEmojiContext(EmojiContext{
				PlaceCtx: emojiutil.PlaceContext{
					Fetcher: fetcher, SendMsg: func(msg any) { ready <- msg },
				},
				Cells: 2, Customs: map[string]string{tc.shortcode: imageURL},
			})
			// The cold render starts the fetch. Wait for the real Place
			// failure notification before checking the fallback frames.
			RenderMessage(msg, 80, m.renderContext(), RenderOptions{})
			select {
			case <-ready:
			case <-time.After(2 * time.Second):
				t.Fatal("failed emoji fetch did not notify the renderer")
			}
			want := "fixture " + status.Glyph(time.Now())
			for _, phase := range []string{"after failure", "subsequent render"} {
				rendered := RenderMessage(msg, 80, m.renderContext(), RenderOptions{})
				if got := strings.TrimSpace(ansi.Strip(rendered.Header)); got != want {
					t.Errorf("%s: status header = %q, want existing glyph %q", phase, got, want)
				}
			}
		})
	}
}

func TestAuthorStatusSuffixNilOptionsPreservesGlyph(t *testing.T) {
	t.Setenv("TMUX", "")
	emojiutil.SetImageMode(true, 2)
	t.Cleanup(func() { emojiutil.SetImageMode(false, 2) })
	statuses := map[string]peerstatus.Status{
		"U_FIXTURE": {Emoji: ":pager:", Text: "On call"},
	}
	got := AuthorStatusSuffix(statuses, "U_FIXTURE", time.Now(), nil)
	if plain := ansi.Strip(got); plain != " 📟" {
		t.Fatalf("nil options status suffix = %q, want the existing pager glyph", plain)
	}
}
