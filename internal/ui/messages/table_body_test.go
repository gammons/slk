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
// a table among them must keep the body row, or it is lost.
func TestBlocksCarryBody_TableWithSections(t *testing.T) {
	msg := MessageItem{Blocks: []blockkit.Block{
		blockkit.SectionBlock{Text: "summary"},
		blockkit.TableBlock{Rows: [][]string{{"a"}}},
	}}
	if BlocksCarryBody(msg) {
		t.Error("BlocksCarryBody = true; the table would not render")
	}
}
