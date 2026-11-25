package messages

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/bubbles/entrylist"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/gammons/slk/internal/ui/styles"
)

// rowSelected builds the selected variant the pane builds for the cursor
// row. WrapRows itself no longer does.
func rowSelected(w WrappedRows, width int, focused, compact bool) []string {
	return NewWrapStyles(focused, compact).SelectedVariant(w.SelSrc, width)
}

// legacyPaint is the lipgloss Width().Render path WrapRows used to take.
// The fast path must stay visually identical to it.
func legacyPaint(content string, width int, focused, mention bool) (lines, selected []string, filled string) {
	fill := lipgloss.NewStyle().Background(styles.Background)
	invis := lipgloss.NewStyle().BorderStyle(styles.InvisibleLeftBorder()).BorderLeft(true).
		BorderForeground(styles.Background).BorderBackground(styles.Background)
	selStyle := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).
		BorderForeground(styles.SelectionBorderColor(focused)).
		BorderBackground(styles.SelectionTintColor(focused)).
		Background(styles.SelectionTintColor(focused))
	mentionTint := bgANSIFor(styles.MentionHighlightBg())
	mentionFill := lipgloss.NewStyle().Background(styles.MentionHighlightBg())
	mentionBorder := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).
		BorderForeground(styles.MentionHighlightBar()).
		BorderBackground(styles.MentionHighlightBg())

	var filledNormal string
	if mention {
		filledNormal = mentionFill.Width(width - 1).Render(repaintBg(content, mentionTint))
		lines = strings.Split(mentionBorder.Render(filledNormal), "\n")
	} else {
		filledNormal = fill.Width(width - 1).Render(content)
		lines = strings.Split(invis.Render(filledNormal), "\n")
	}
	selFill := lipgloss.NewStyle().Background(styles.SelectionTintColor(focused)).Width(width - 1)
	selected = strings.Split(selStyle.Render(selFill.Render(RepaintBgToSelectionTint(content, focused))), "\n")
	return lines, selected, filledNormal
}

func TestCarryPenMatchesWrapWriter(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })
	msg := MessageItem{
		TS: "1700000000.000000", UserID: "U1", UserName: "alice",
		Text:      "build #421 green",
		Timestamp: "9:02 AM",
		Blocks: []blockkit.Block{
			blockkit.SectionBlock{Text: "*rollout*: 100% of shards on v2.4.1"},
		},
		LegacyAttachments: []blockkit.LegacyAttachment{{
			Text: "<https://example.com/" + strings.Repeat("x", 80) + "|blocked>",
		}},
	}
	m := New([]MessageItem{msg}, "general")
	content := RenderMessage(msg, 80, m.renderContext(), RenderOptions{}).Content
	if got, want := carryPen(ansi.Wrap(content, 79, "")), lipgloss.Wrap(content, 79, ""); got != want {
		t.Fatalf("carryPen(ansi.Wrap) differs from lipgloss.Wrap\n got %q\nwant %q", got, want)
	}
}

func TestWrapRowsMatchesLipgloss_RenderedMessage(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })
	msg := MessageItem{
		TS: "1700000000.000000", UserID: "U1", UserName: "alice",
		Text:      "build #421 green",
		Timestamp: "9:02 AM",
		Blocks: []blockkit.Block{
			blockkit.SectionBlock{Text: "*rollout*: 100% of shards on v2.4.1"},
		},
	}
	m := New([]MessageItem{msg}, "general")
	for _, width := range []int{20, 40, 80, 120} {
		r := RenderMessage(msg, width, m.renderContext(), RenderOptions{})
		got := WrapRows(msg, r, width, NewWrapStyles(true, false))
		wantLines, _, _ := legacyPaint(r.Content, width, true, false)
		if g, w := strings.Join(got.Lines, "\n"), strings.Join(wantLines, "\n"); g != w {
			t.Errorf("width %d lines differ\n got %q\nwant %q", width, g, w)
		}
		_, wantSel, _ := legacyPaint(got.SelSrc, width, true, false)
		if g, w := strings.Join(rowSelected(got, width, true, false), "\n"), strings.Join(wantSel, "\n"); g != w {
			t.Errorf("width %d selected differs\n got %q\nwant %q", width, g, w)
		}
	}
}

func TestWrapRowsMatchesLipgloss(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	styled := styles.MessageText.Render
	link := func(url, label string) string {
		return "\x1b]8;;" + url + "\x1b\\" + styled(label) + "\x1b]8;;\x1b\\"
	}
	cases := []struct {
		name    string
		content string
		mention bool
		width   int
		notice  bool
	}{
		{name: "plain", content: styled("hello"), width: 40},
		{name: "mention", content: styled("hello "), mention: true, width: 40},
		{name: "wide", content: styled("👋 hello 🎉"), width: 30},
		{name: "osc8", content: styled("see ") + link("https://example.com/a", "the doc") + styled(" now"), width: 50},
		{name: "empty", content: "", width: 20},
		{name: "blank-line", content: styled("top") + "\n\n" + styled("bottom"), width: 24},
		{name: "notice", content: "   joined the channel   ", width: 40, notice: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := MessageItem{Text: "x"}
			if tc.notice {
				msg.Subtype = "channel_join"
			}
			got := WrapRows(msg, RenderedMessage{Content: tc.content, MentionsSelf: tc.mention}, tc.width, NewWrapStyles(true, false))
			wantLines, wantSel, wantFilled := legacyPaint(tc.content, tc.width, true, tc.mention)

			if g, w := strings.Join(got.Lines, "\n"), strings.Join(wantLines, "\n"); g != w {
				t.Errorf("lines bytes differ\n got %q\nwant %q", g, w)
			}
			compareCells(t, "lines", strings.Join(got.Lines, "\n"), strings.Join(wantLines, "\n"), tc.width)
			gotSel := rowSelected(got, tc.width, true, false)
			if g, w := strings.Join(gotSel, "\n"), strings.Join(wantSel, "\n"); g != w {
				t.Errorf("selected bytes differ\n got %q\nwant %q", g, w)
			}
			compareCells(t, "selected", strings.Join(gotSel, "\n"), strings.Join(wantSel, "\n"), tc.width)

			wantPlain := entrylist.PlainLines(wantFilled)
			if tc.notice {
				for i := range wantPlain {
					wantPlain[i] = trimPlainPadding(wantPlain[i])
				}
			}
			if len(got.Plain) != len(wantPlain) {
				t.Fatalf("plain lines = %d, lipgloss = %d", len(got.Plain), len(wantPlain))
			}
			for i := range got.Plain {
				if got.Plain[i].Text != wantPlain[i].Text {
					t.Errorf("plain[%d] = %q, lipgloss = %q", i, got.Plain[i].Text, wantPlain[i].Text)
				}
			}
			if gotHits, wantHits := HitsFromSpans(ScanOsc8Spans(got.Lines), msg), HitsFromSpans(ScanOsc8Spans(wantLines), msg); fmt.Sprint(gotHits) != fmt.Sprint(wantHits) {
				t.Errorf("link hits = %+v, lipgloss = %+v", gotHits, wantHits)
			}
		})
	}
}

func compareCells(t *testing.T, kind, got, want string, width int) {
	t.Helper()
	gh, wh := lipgloss.Height(got), lipgloss.Height(want)
	if gh != wh {
		t.Fatalf("%s height = %d, lipgloss = %d\ngot:\n%s\nwant:\n%s", kind, gh, wh, ansi.Strip(got), ansi.Strip(want))
	}
	if gh == 0 {
		return
	}
	gc := lipgloss.NewCanvas(width, gh)
	wc := lipgloss.NewCanvas(width, wh)
	gc.Compose(lipgloss.NewLayer(got))
	wc.Compose(lipgloss.NewLayer(want))
	for y := 0; y < gh; y++ {
		for x := 0; x < width; x++ {
			a, b := gc.CellAt(x, y), wc.CellAt(x, y)
			if a == nil && b == nil {
				continue
			}
			if a == nil || b == nil || a.Content != b.Content || a.Width != b.Width || !a.Style.Equal(&b.Style) || a.Link != b.Link {
				t.Fatalf("%s cell (%d,%d) differs\n got %#v\nwant %#v\nline got: %q\nline want: %q",
					kind, x, y, a, b, lineAt(got, y), lineAt(want, y))
			}
		}
	}
}

func lineAt(s string, y int) string {
	lines := strings.Split(s, "\n")
	if y < 0 || y >= len(lines) {
		return ""
	}
	return lines[y]
}
