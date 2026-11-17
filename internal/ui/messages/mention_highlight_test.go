package messages

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/gammons/slk/internal/ui/styles"
)

func TestSelfMentionsInText(t *testing.T) {
	self := SelfMentions{UserID: "UME", Groups: map[string]bool{"SMINE": true}}
	for _, c := range []struct {
		text string
		want bool
	}{
		{"hi <@UME>", true},
		{"hi <@UMEX>", false},
		{"hi <@UOTHER>", false},
		{"<!here> standup", true},
		{"<!channel|@channel> standup", true},
		{"<!everyone>", true},
		{"ping <!subteam^SMINE|@platform>", true},
		{"ping <!subteam^SMINE>", true},
		{"ping <!subteam^SOTHER|@design>", false},
		{"no mentions", false},
	} {
		if got := self.InText(c.text); got != c.want {
			t.Errorf("InText(%q) = %v, want %v", c.text, got, c.want)
		}
	}
	if (SelfMentions{}).InText("<!here> <@UME>") {
		t.Error("the zero SelfMentions matched a mention")
	}
}

// Mentions that reach the user get selfMentionStyle; others keep
// mentionStyle.
func TestRenderSelfMentionStyle(t *testing.T) {
	opts := RenderSlackMarkdownOpts{
		UserNames:  map[string]string{"UME": "me", "UOTHER": "bob"},
		UserGroups: map[string]string{"SMINE": "platform", "SOTHER": "design"},
		Self:       SelfMentions{UserID: "UME", Groups: map[string]bool{"SMINE": true}},
	}
	out := RenderSlackMarkdownWith("<@UME> <@UOTHER> <!here> <!subteam^SMINE> <!subteam^SOTHER>", opts)
	for _, want := range []string{
		selfMentionStyle().Render("@me"),
		mentionStyle().Render("@bob"),
		selfMentionStyle().Render("@here"),
		selfMentionStyle().Render("@platform"),
		mentionStyle().Render("@design"),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%q", want, out)
		}
	}

	opts.Self = SelfMentions{}
	if out := RenderSlackMarkdownWith("<!here>", opts); !strings.Contains(out, mentionStyle().Render("@here")) {
		t.Errorf("without a user, @here should keep the plain mention style: %q", out)
	}
}

// Someone else's message that mentions the user gets the highlight tint
// and bar on its unselected row; the user's own message does not, and
// the selected row keeps the selection look.
func TestWrapRows_MentionHighlight(t *testing.T) {
	tint := bgSGRParams(bgANSIFor(styles.MentionHighlightBg()))
	bar := fgParams(styles.MentionHighlightBar())
	items := []MessageItem{
		{TS: "1.000001", UserID: "UOTHER", UserName: "bob", Text: "<@UME> look", Timestamp: "3:40 PM"},
		{TS: "1.000002", UserID: "UME", UserName: "me", Text: "<!here> look", Timestamp: "3:41 PM"},
		{TS: "1.000003", UserID: "UOTHER", UserName: "bob", Text: "plain", Timestamp: "3:42 PM"},
	}
	m := New(items, "general")
	m.SetCurrentUser("UME")
	const width = 40
	for i, want := range []bool{true, false, false} {
		r := RenderMessage(items[i], width, m.renderContext(), RenderOptions{})
		if r.MentionsSelf != want {
			t.Errorf("item %d: MentionsSelf = %v, want %v", i, r.MentionsSelf, want)
		}
		w := WrapRows(items[i], r, width, NewWrapStyles(true, false))
		lines := strings.Join(w.Lines, "\n")
		if got := strings.Contains(lines, tint) && strings.Contains(lines, bar); got != want {
			t.Errorf("item %d: highlighted rows = %v, want %v:\n%q", i, got, want, lines)
		}
		if strings.Contains(strings.Join(w.Selected, "\n"), tint) {
			t.Errorf("item %d: selected rows carry the mention tint", i)
		}
	}
}

// A bot that puts the mention in a section block or a legacy
// attachment, with only a summary in Text, still highlights the row.
func TestRenderMessage_MentionInBlocks(t *testing.T) {
	m := New(nil, "general")
	m.SetCurrentUser("UME")
	for name, msg := range map[string]MessageItem{
		"section":    {TS: "1.1", UserID: "UBOT", Text: "Review requested", Blocks: []blockkit.Block{blockkit.SectionBlock{Text: "<@UME> please review"}}},
		"attachment": {TS: "1.2", UserID: "UBOT", Text: "Review requested", LegacyAttachments: []blockkit.LegacyAttachment{{Text: "<@UME> please review"}}},
	} {
		if !RenderMessage(msg, 60, m.renderContext(), RenderOptions{}).MentionsSelf {
			t.Errorf("%s: mention not detected", name)
		}
	}
}

// fgParams is c's 24-bit foreground SGR parameters, as the bar's
// border renders it.
func fgParams(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("38;2;%d;%d;%d", r>>8, g>>8, b>>8)
}
