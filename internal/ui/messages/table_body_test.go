// Slack splits a bot's `markdown` block that holds a Markdown table into
// rich_text, table, rich_text. The body must keep all three, in order.
package messages

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/slack-go/slack"

	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

func richSection(s string) *slack.RichTextBlock {
	return slack.NewRichTextBlock("", slack.NewRichTextSection(slack.NewRichTextSectionTextElement(s, nil)))
}

func tableMessage() MessageItem {
	tb := &slack.TableBlock{
		Type:           slack.MBTTable,
		Rows:           [][]*slack.RichTextBlock{{richSection("eps"), richSection("Coverage")}, {richSection("0.19"), richSection("84.0%")}},
		ColumnSettings: []slack.ColumnSetting{{Align: slack.ColumnAlignmentRight}, {Align: slack.ColumnAlignmentRight}},
	}
	return MessageItem{
		Text:   "before | eps | Coverage | after (lossy fallback)",
		Blocks: blockkit.Parse(slack.Blocks{BlockSet: []slack.Block{richSection("Before the table:\n\n"), tb, richSection("After the table.")}}),
	}
}

func TestMessageTextSource_TableKeepsItsPlaceAndTheTextAfterIt(t *testing.T) {
	got := MessageTextSource(tableMessage())
	want := "Before the table:\n```\n eps │ Coverage\n─────┼─────────\n0.19 │    84.0%\n```\nAfter the table."
	if got != want {
		t.Errorf("MessageTextSource =\n%q\nwant\n%q", got, want)
	}
}

func TestTableMessage_RendersAlignedWithoutPlaceholder(t *testing.T) {
	msg := tableMessage()
	if BlocksCarryBody(msg) {
		t.Fatal("BlocksCarryBody = true; the table would be hidden")
	}
	out := ansi.Strip(WordWrap(RenderSlackMarkdownWith(MessageTextSource(msg), RenderSlackMarkdownOpts{Width: 40}), 40))
	for _, want := range []string{"Before the table:", "0.19 │    84.0%", "After the table."} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered body lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "unsupported") {
		t.Errorf("placeholder rendered:\n%s", out)
	}
}

// Several rich_text blocks without a table all contribute too; before,
// only the first did.
func TestMessageTextSource_AllRichTextBlocks(t *testing.T) {
	msg := MessageItem{Blocks: blockkit.Parse(slack.Blocks{BlockSet: []slack.Block{richSection("one"), richSection("two")}})}
	if got := MessageTextSource(msg); got != "one\ntwo" {
		t.Errorf("MessageTextSource = %q, want %q", got, "one\ntwo")
	}
}

// A message that is only a table still gets a body row.
func TestBlocksCarryBody_TableOnly(t *testing.T) {
	msg := MessageItem{Blocks: []blockkit.Block{blockkit.TableBlock{Rows: [][]string{{"a"}}}}}
	if BlocksCarryBody(msg) {
		t.Error("BlocksCarryBody = true for a table-only message")
	}
	if got := MessageTextSource(msg); got != "```\na\n```" {
		t.Errorf("MessageTextSource = %q", got)
	}
}

// Sections render through the block renderer, which hides the body row;
// a table among them is drawn there too, in its place, not in a body
// row above them.
func TestBlocksCarryBody_TableWithSections(t *testing.T) {
	msg := MessageItem{Blocks: []blockkit.Block{
		blockkit.SectionBlock{Text: "summary"},
		blockkit.TableBlock{Rows: [][]string{{"a"}}},
	}}
	if !BlocksCarryBody(msg) {
		t.Error("BlocksCarryBody = false; the table would be drawn above the section")
	}
}

// renderedRows renders msg through RenderMessage, as both panes do, and
// returns its plain rows.
func renderedRows(t *testing.T, msg MessageItem, width int) []string {
	t.Helper()
	msg.TS, msg.UserID, msg.UserName, msg.Timestamp = "1.0", "U1", "bot", "3:40 PM"
	m := New([]MessageItem{msg}, "general")
	r := RenderMessage(msg, width, m.renderContext(), RenderOptions{})
	return strings.Split(ansi.Strip(r.Content), "\n")
}

// tableRows returns the rows that hold a column separator.
func tableRows(rows []string) []string {
	var out []string
	for _, r := range rows {
		if strings.ContainsAny(r, "│┼") {
			out = append(out, r)
		}
	}
	return out
}

// separatorColumns returns the display column of each │ or ┼ in row.
func separatorColumns(row string) []int {
	var cols []int
	col := 0
	for _, r := range row {
		if r == '│' || r == '┼' {
			cols = append(cols, col)
		}
		col += ansi.StringWidth(string(r))
	}
	return cols
}

// Emoji, mrkdwn markers and a ``` inside cells must not change a cell's
// drawn width: every │ lines up with the header's.
func TestRenderMessage_TableColumnsAlignWithEmojiAndMarkup(t *testing.T) {
	emojiCell := slack.NewRichTextBlock("", slack.NewRichTextSection(
		slack.NewRichTextSectionEmojiElement("white_check_mark", 0, nil),
		slack.NewRichTextSectionTextElement(" done", nil),
	))
	tb := &slack.TableBlock{
		Type: slack.MBTTable,
		Rows: [][]*slack.RichTextBlock{
			{richSection("status"), richSection("note")},
			{emojiCell, richSection("a *b* _c_ ~d~")},
			{richSection("todo"), richSection("x ``` y")},
		},
	}
	msg := MessageItem{Blocks: blockkit.Parse(slack.Blocks{BlockSet: []slack.Block{richSection("Report:"), tb, richSection("End.")}})}
	rows := renderedRows(t, msg, 60)
	got := tableRows(rows)
	if len(got) != 4 {
		t.Fatalf("got %d table rows, want 4:\n%s", len(got), strings.Join(rows, "\n"))
	}
	want := separatorColumns(got[0])
	for _, r := range got[1:] {
		if cols := separatorColumns(r); len(cols) != len(want) || cols[0] != want[0] {
			t.Errorf("separators at %v in %q, want %v:\n%s", cols, r, want, strings.Join(got, "\n"))
		}
	}
	joined := strings.Join(rows, "\n")
	for _, s := range []string{"✅ done", "a *b* _c_ ~d~", "x ``` y", "End."} {
		if !strings.Contains(joined, s) {
			t.Errorf("rendered message lacks %q:\n%s", s, joined)
		}
	}
}

// A table after a header and a section is drawn after them, once.
func TestRenderMessage_TableAfterSectionKeepsOrder(t *testing.T) {
	for name, blocks := range map[string][]blockkit.Block{
		"header": {
			blockkit.HeaderBlock{Text: "Weekly numbers"},
			blockkit.TableBlock{Rows: [][]string{{"k", "v"}, {"a", "1"}}},
		},
		"section": {
			blockkit.SectionBlock{Text: "Weekly numbers"},
			blockkit.TableBlock{Rows: [][]string{{"k", "v"}, {"a", "1"}}},
		},
		"rich_text, section, table": {
			blockkit.Parse(slack.Blocks{BlockSet: []slack.Block{richSection("Intro")}})[0],
			blockkit.SectionBlock{Text: "Weekly numbers"},
			blockkit.TableBlock{Rows: [][]string{{"k", "v"}, {"a", "1"}}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			rows := renderedRows(t, MessageItem{Blocks: blocks}, 60)
			joined := strings.Join(rows, "\n")
			heading, table := strings.Index(joined, "Weekly numbers"), strings.Index(joined, "k │ v")
			if heading < 0 || table < 0 || heading > table {
				t.Errorf("want heading above table:\n%s", joined)
			}
			if n := strings.Count(joined, "a │ 1"); n != 1 {
				t.Errorf("table drawn %d times:\n%s", n, joined)
			}
			if intro := strings.Index(joined, "Intro"); name == "rich_text, section, table" && (intro < 0 || intro > heading) {
				t.Errorf("want rich_text above the section:\n%s", joined)
			}
		})
	}
}

// A footer after the text that follows a table must not pull the table
// out of the body: intro, table, outro and footer stay in block order,
// and the table is drawn once.
func TestRenderMessage_TableBeforeFooterKeepsOrder(t *testing.T) {
	for name, footer := range map[string]blockkit.Block{
		"context": blockkit.ContextBlock{Elements: []blockkit.ContextElement{{Text: "Sent by footer-bot"}}},
		"actions": blockkit.ActionsBlock{Elements: []blockkit.ActionElement{{Kind: "button", Label: "Sent by footer-bot"}}},
	} {
		t.Run(name, func(t *testing.T) {
			blocks := blockkit.Parse(slack.Blocks{BlockSet: []slack.Block{
				richSection("Intro"),
				&slack.TableBlock{Type: slack.MBTTable, Rows: [][]*slack.RichTextBlock{{richSection("k"), richSection("v")}, {richSection("a"), richSection("1")}}},
				richSection("Outro"),
			}})
			blocks = append(blocks, footer)
			joined := strings.Join(renderedRows(t, MessageItem{Blocks: blocks}, 60), "\n")
			at := func(s string) int { return strings.Index(joined, s) }
			intro, table, outro, foot := at("Intro"), at("k │ v"), at("Outro"), at("Sent by footer-bot")
			if intro < 0 || table < 0 || outro < 0 || foot < 0 || intro >= table || table >= outro || outro >= foot {
				t.Errorf("want intro, table, outro, footer in that order:\n%s", joined)
			}
			if n := strings.Count(joined, "a │ 1"); n != 1 {
				t.Errorf("table drawn %d times:\n%s", n, joined)
			}
		})
	}
}

// For a message with no other Block Kit, the body row draws the table
// exactly as the fenced code block it used to be.
func TestRenderMessageBody_MatchesFencedCodeBlock(t *testing.T) {
	msg := tableMessage()
	opts := RenderSlackMarkdownOpts{Width: 40}
	if got, want := renderMessageBody(msg, opts), RenderSlackMarkdownWith(MessageTextSource(msg), opts); got != want {
		t.Errorf("renderMessageBody =\n%q\nwant\n%q", got, want)
	}
}

// Export and copy read MessageTextSource. A cell holding ``` gets a
// longer fence there, so the table stays one code block in the
// CommonMark export, with the text after it outside the block and the
// right-aligned header keeping its leading padding.
func TestMessageTextSource_FenceOutlastsBackticksInCells(t *testing.T) {
	tb := &slack.TableBlock{
		Type:           slack.MBTTable,
		Rows:           [][]*slack.RichTextBlock{{richSection("id"), richSection("snippet")}, {richSection("1"), richSection("x ``` y")}},
		ColumnSettings: []slack.ColumnSetting{{Align: slack.ColumnAlignmentRight}},
	}
	msg := MessageItem{Blocks: blockkit.Parse(slack.Blocks{BlockSet: []slack.Block{richSection("Before *bold*"), tb, richSection("After *bold*")}})}
	src := MessageTextSource(msg)
	table := "id │ snippet\n───┼────────\n 1 │ x ``` y"
	if want := "Before *bold*\n````\n" + table + "\n````\nAfter *bold*"; src != want {
		t.Errorf("MessageTextSource =\n%q\nwant\n%q", src, want)
	}
	if got, want := SlackMrkdwnToCommonMark(src, nil, nil), "Before **bold**\n````\n"+table+"\n````\nAfter **bold**"; got != want {
		t.Errorf("CommonMark export =\n%q\nwant\n%q", got, want)
	}
}

// Without backticks in cells the fence stays three long, and the export
// keeps a right-aligned header's leading padding.
func TestMessageTextSource_ExportKeepsHeaderPadding(t *testing.T) {
	got := SlackMrkdwnToCommonMark(MessageTextSource(tableMessage()), nil, nil)
	want := "Before the table:\n```\n eps │ Coverage\n─────┼─────────\n0.19 │    84.0%\n```\nAfter the table."
	if got != want {
		t.Errorf("CommonMark export =\n%q\nwant\n%q", got, want)
	}
}

func TestCodeFence(t *testing.T) {
	for in, want := range map[string]string{"": "```", "a`b``c": "```", "x ``` y": "````", "`````": "``````"} {
		if got := codeFence(in); got != want {
			t.Errorf("codeFence(%q) = %q, want %q", in, got, want)
		}
	}
}
