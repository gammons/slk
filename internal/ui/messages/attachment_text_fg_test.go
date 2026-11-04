package messages

import (
	stdimage "image"
	"strings"
	"testing"

	emojiutil "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

// explicitFgAt reports whether the first occurrence of word in out is
// drawn with an explicit foreground, following the SGR state from the
// start of out. found is false when word is absent.
func explicitFgAt(out, word string) (explicit, found bool) {
	fg := false
	for i := 0; i < len(out); {
		if strings.HasPrefix(out[i:], word) {
			return fg, true
		}
		if out[i] != 0x1b || i+1 == len(out) {
			i++
			continue
		}
		switch out[i+1] {
		case '[':
			j := i + 2
			for j < len(out) && (out[j] < 0x40 || out[j] > 0x7e) {
				j++
			}
			if j < len(out) && out[j] == 'm' {
				fg = applySGRFg(fg, out[i+2:j])
			}
			i = j + 1
		case ']':
			end := strings.Index(out[i:], "\x1b\\")
			if end < 0 {
				return fg, false
			}
			i += end + 2
		default:
			i += 2
		}
	}
	return false, false
}

func applySGRFg(fg bool, params string) bool {
	ps := strings.Split(params, ";")
	for k := 0; k < len(ps); k++ {
		switch p := ps[k]; {
		case p == "" || p == "0" || p == "39":
			fg = false
		case p == "38" || p == "48":
			if p == "38" {
				fg = true
			}
			if k+1 < len(ps) && ps[k+1] == "5" {
				k += 2
			} else if k+1 < len(ps) && ps[k+1] == "2" {
				k += 4
			}
		case len(p) == 2 && (p[0] == '3' || p[0] == '9') && p[1] >= '0' && p[1] <= '7':
			fg = true
		}
	}
	return fg
}

// TestLegacyAttachment_PlainLinesKeepTheThemeText: the stripe before
// each attachment line ends in a reset, so a line with no styled span
// of its own (a bullet, plain prose) fell back to the terminal's
// default foreground — unreadable where that differs from the theme's.
func TestLegacyAttachment_PlainLinesKeepTheThemeText(t *testing.T) {
	msg := MessageItem{
		TS: "1700000000.000000", UserName: "lola", UserID: "U1", Text: "a must watch", Timestamp: "11:08 AM",
		LegacyAttachments: []blockkit.LegacyAttachment{{
			Text:   "*Two key ideas:*\n• rewire your brains\nQuestions & feedback welcome! cc <@U1>",
			Footer: "Thread in Slack conversation",
		}},
	}
	m := New([]MessageItem{msg}, "general")
	out := RenderMessage(msg, 100, m.renderContext(), RenderOptions{Avatar: "AVAV\nAVAV"}).Content
	for _, word := range []string{"rewire", "Questions"} {
		if explicit, found := explicitFgAt(out, word); !found || !explicit {
			t.Errorf("%q drawn with the terminal's default foreground (found=%v):\n%q", word, found, out)
		}
	}
}

// TestBlockKitLines_KeepTheThemeTextBesideAnAvatar: the avatar gutter
// closes with a reset on every line, so section, pretext and title
// lines need the text colour back too.
func TestBlockKitLines_KeepTheThemeTextBesideAnAvatar(t *testing.T) {
	msg := MessageItem{
		TS: "1700000000.000000", UserName: "bot", UserID: "U1", Text: "x", Timestamp: "11:08 AM",
		Blocks:            []blockkit.Block{blockkit.SectionBlock{Text: "*Head*\nsecondline plain"}},
		LegacyAttachments: []blockkit.LegacyAttachment{{Pretext: "*pre*\npretextplain", Title: "TitleWord", Text: "body"}},
	}
	m := New([]MessageItem{msg}, "general")
	out := RenderMessage(msg, 100, m.renderContext(), RenderOptions{Avatar: "AVAV\nAVAV"}).Content
	for _, word := range []string{"secondline", "pretextplain", "TitleWord"} {
		if explicit, found := explicitFgAt(out, word); !found || !explicit {
			t.Errorf("%q drawn with the terminal's default foreground (found=%v)", word, found)
		}
	}
}

// TestInlineEmoji_TextAfterItKeepsTheThemeText: an image placement ends
// in "default foreground", which isn't a reset, so the text after an
// inline custom emoji took the terminal's default colour.
func TestInlineEmoji_TextAfterItKeepsTheThemeText(t *testing.T) {
	emojiutil.SetImageMode(true, 2)
	t.Cleanup(func() { emojiutil.SetImageMode(false, 2) })
	url := emojiutil.CDNBaseURL + "1f44d.png"
	ff := newFakePlaceFetcher()
	ff.setPrerendered(emojiutil.EmojiCacheKey(url), stdimage.Pt(2, 1), imgpkg.Render{
		Cells: stdimage.Pt(2, 1),
		Lines: []string{"\x1b[38;2;0;0;42m\U0010EEEE\U0010EEEE\x1b[39m"},
	})
	msg := MessageItem{TS: "1.0", UserName: "james", UserID: "U1", Text: "I (:thumbsup:) noticed a drift", Timestamp: "3:09 PM"}
	m := New([]MessageItem{msg}, "general")
	m.SetEmojiContext(EmojiContext{PlaceCtx: emojiutil.PlaceContext{Fetcher: ff}, Cells: 2})
	out := RenderMessage(msg, 100, m.renderContext(), RenderOptions{Avatar: "AVAV\nAVAV"}).Content
	if !strings.Contains(out, "\U0010EEEE") {
		t.Fatalf("emoji not placed as an image:\n%q", out)
	}
	if explicit, found := explicitFgAt(out, "noticed"); !found || !explicit {
		t.Errorf("text after the emoji drawn with the terminal's default foreground:\n%q", out)
	}
}
