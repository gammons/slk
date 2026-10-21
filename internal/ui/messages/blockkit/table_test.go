package blockkit

import (
	"testing"

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
	if got, want := RichTextToPlain(rt), "bold and more label @U1 :tada:"; got != want {
		t.Errorf("RichTextToPlain = %q, want %q", got, want)
	}
}

func TestTableRendersNoBlockLines(t *testing.T) {
	out := Render([]Block{TableBlock{Rows: [][]string{{"a"}}}}, Context{}, 40)
	if len(out.Lines) != 0 {
		t.Errorf("table rendered %d block lines; it belongs to the message body", len(out.Lines))
	}
}
