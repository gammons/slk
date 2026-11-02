package blockkit

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/slack-go/slack"
)

func cell(s string) *slack.RichTextBlock {
	return slack.NewRichTextBlock("", slack.NewRichTextSection(slack.NewRichTextSectionTextElement(s, nil)))
}

func TestParseTable(t *testing.T) {
	in := &slack.TableBlock{
		Type: slack.MBTTable,
		Rows: [][]*slack.RichTextBlock{{cell("ε"), cell("Coverage")}, {cell("0.19"), nil}},
		ColumnSettings: []slack.ColumnSetting{
			{Align: slack.ColumnAlignmentLeft}, {Align: slack.ColumnAlignmentRight},
		},
	}
	got := Parse(slack.Blocks{BlockSet: []slack.Block{in}})
	tb, ok := got[0].(TableBlock)
	if !ok {
		t.Fatalf("parsed %T, want TableBlock", got[0])
	}
	if len(tb.Rows) != 2 || tb.Rows[0][0] != "ε" || tb.Rows[1][1] != "" {
		t.Errorf("rows = %q", tb.Rows)
	}
	if len(tb.Align) != 2 || tb.Align[1] != "right" {
		t.Errorf("align = %q", tb.Align)
	}
}

func TestTableToText(t *testing.T) {
	tb := TableBlock{
		Rows:  [][]string{{"name", "n", "note"}, {"café", "1,025", "ok"}, {"日本", "7"}},
		Align: []string{"left", "right", "center"},
	}
	want := "" +
		"name │     n │ note\n" +
		"─────┼───────┼─────\n" +
		"café │ 1,025 │  ok\n" +
		"日本 │     7 │"
	if got := TableToText(tb); got != want {
		t.Errorf("TableToText =\n%s\nwant\n%s", got, want)
	}
	if got := TableToText(TableBlock{}); got != "" {
		t.Errorf("empty table = %q", got)
	}
	// A single row is a header with nothing under it: no rule.
	if got := TableToText(TableBlock{Rows: [][]string{{"a", "b"}}}); got != "a │ b" {
		t.Errorf("one-row table = %q", got)
	}
}

func TestRichTextToPlain(t *testing.T) {
	bold := &slack.RichTextSectionTextStyle{Bold: true}
	rt := RichTextBlock{Elements: []slack.RichTextElement{
		slack.NewRichTextSection(
			slack.NewRichTextSectionTextElement("bold", bold),
			slack.NewRichTextSectionTextElement(" and\nmore ", nil),
			slack.NewRichTextSectionLinkElement("https://x.example", "label", nil),
			slack.NewRichTextSectionTextElement(" ", nil),
			slack.NewRichTextSectionUserElement("U1", nil),
			slack.NewRichTextSectionTextElement(" ", nil),
			slack.NewRichTextSectionEmojiElement("tada", 0, nil),
		),
	}}
	if got, want := RichTextToPlain(rt), "bold and more label @U1 🎉"; got != want {
		t.Errorf("RichTextToPlain = %q, want %q", got, want)
	}
}

// Emoji resolve to glyphs here, not later in the mrkdwn pass, so the
// width TableToText measures is the width drawn. A skin tone is
// dropped; a multi-codepoint glyph (❤️ is ❤ + VS16) keeps its shortcode,
// as in the message body.
func TestRichTextToPlain_Emoji(t *testing.T) {
	rt := RichTextBlock{Elements: []slack.RichTextElement{
		slack.NewRichTextSection(
			slack.NewRichTextSectionEmojiElement("white_check_mark", 0, nil),
			slack.NewRichTextSectionEmojiElement("+1", 3, nil),
			slack.NewRichTextSectionTextElement(" ", nil),
			slack.NewRichTextSectionEmojiElement("heart", 0, nil),
		),
	}}
	if got, want := RichTextToPlain(rt), "✅ 👍 :heart:"; got != want {
		t.Errorf("RichTextToPlain = %q, want %q", got, want)
	}
}

func TestRichTextToPlain_ListItemsAreSeparated(t *testing.T) {
	item := func(s string) slack.RichTextElement {
		return slack.NewRichTextSection(slack.NewRichTextSectionTextElement(s, nil))
	}
	list := slack.NewRichTextList(slack.RTEListBullet, 0, item("alpha"), item("beta"), item("gamma"))
	rt := RichTextBlock{Elements: []slack.RichTextElement{list}}
	if got, want := RichTextToPlain(rt), "alpha beta gamma"; got != want {
		t.Errorf("RichTextToPlain = %q, want %q", got, want)
	}
}

func TestTableToText_EmojiWidth(t *testing.T) {
	got := TableToText(TableBlock{Rows: [][]string{{"ok", "n"}, {"✅", "1"}, {"no", "2"}}})
	lines := strings.Split(got, "\n")
	want := ansi.StringWidth(lines[0][:strings.Index(lines[0], "│")])
	for _, l := range lines[2:] {
		if col := ansi.StringWidth(l[:strings.Index(l, "│")]); col != want {
			t.Errorf("│ at column %d in %q, want %d:\n%s", col, l, want, got)
		}
	}
}

func TestTableRendersNoBlockLines(t *testing.T) {
	out := Render([]Block{TableBlock{Rows: [][]string{{"a"}}}}, Context{}, 40)
	if len(out.Lines) != 0 {
		t.Errorf("table rendered %d block lines; it belongs to the message body", len(out.Lines))
	}
}

// When the other blocks render the body, the table is drawn among them,
// in block order, through Context.RenderCode.
func TestRender_TableInPlaceAmongBodyBlocks(t *testing.T) {
	blocks := []Block{
		HeaderBlock{Text: "Results"},
		TableBlock{Rows: [][]string{{"k", "v"}, {"a", "1"}}},
		SectionBlock{Text: "after"},
	}
	var gotWidth int
	ctx := Context{RenderCode: func(code string, width int) string {
		gotWidth = width
		return "<" + strings.ReplaceAll(code, "\n", ">\n<") + ">"
	}}
	out := Render(blocks, ctx, 30)
	text := ansi.Strip(strings.Join(out.Lines, "\n"))
	head, table, after := strings.Index(text, "Results"), strings.Index(text, "<k │ v>"), strings.Index(text, "after")
	if head < 0 || table < 0 || after < 0 || !(head < table && table < after) {
		t.Errorf("want header, table, section in order; got:\n%s", text)
	}
	if !strings.Contains(text, "<a │ 1>") {
		t.Errorf("table body row missing:\n%s", text)
	}
	if gotWidth != 30 {
		t.Errorf("RenderCode width = %d, want 30", gotWidth)
	}
}

// Without RenderCode the table is drawn plain, never wider than width.
func TestRender_TablePlainFallbackFitsWidth(t *testing.T) {
	blocks := []Block{
		SectionBlock{Text: "x"},
		TableBlock{Rows: [][]string{{"a long header cell", "another long one"}}},
	}
	out := Render(blocks, Context{}, 12)
	if !strings.Contains(strings.Join(out.Lines, ""), "a long") {
		t.Fatalf("table not drawn: %q", out.Lines)
	}
	for _, l := range out.Lines {
		if w := ansi.StringWidth(l); w > 12 {
			t.Errorf("line %q is %d columns, want <= 12", l, w)
		}
	}
}

// A legacy attachment has no message body to carry its nested blocks'
// tables, so the attachment draws them.
func TestRenderLegacy_NestedTable(t *testing.T) {
	att := LegacyAttachment{Blocks: []Block{TableBlock{Rows: [][]string{{"k", "v"}, {"a", "1"}}}}}
	out := RenderLegacy([]LegacyAttachment{att}, Context{}, 40)
	text := ansi.Strip(strings.Join(out.Lines, "\n"))
	if !strings.Contains(text, "k │ v") || !strings.Contains(text, "a │ 1") {
		t.Errorf("nested table missing:\n%s", text)
	}
}
