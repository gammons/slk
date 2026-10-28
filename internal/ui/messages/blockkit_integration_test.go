package messages

import (
	"encoding/json"
	goimage "image"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/slack-go/slack"

	"github.com/gammons/slk/internal/core/blocks"
	emojiutil "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

// TestRenderMessage_BlockKitEmojiFlushesCollected: a warm emoji in Block
// Kit text must hand its kitty upload back in BlockFlushes, or its
// placeholders are drawn but never resolved (#384).
func TestRenderMessage_BlockKitEmojiFlushesCollected(t *testing.T) {
	emojiutil.SetImageMode(true, 2)
	t.Cleanup(func() { emojiutil.SetImageMode(false, 2) })

	thumbURL := emojiutil.CDNBaseURL + "1f44d.png"
	ff := newFakePlaceFetcher()
	ff.setPrerendered(emojiutil.EmojiCacheKey(thumbURL), goimage.Pt(2, 1), imgpkg.Render{
		Cells:   goimage.Pt(2, 1),
		Lines:   []string{"\U0010EEEE\U0010EEEE"},
		OnFlush: func(io.Writer) error { return nil },
	})

	base := MessageItem{TS: "1700000000.000000", UserName: "github", UserID: "U-BOT", Timestamp: "1:23 PM"}
	section := base
	section.Text = "PR opened"
	section.Blocks = []blockkit.Block{blockkit.SectionBlock{Text: "New PR :thumbsup: 171"}}
	legacy := base
	legacy.LegacyAttachments = []blocks.LegacyAttachment{{Text: "New PR :thumbsup: 171"}}

	for name, msg := range map[string]MessageItem{"section block": section, "legacy attachment": legacy} {
		t.Run(name, func(t *testing.T) {
			m := New([]MessageItem{msg}, "general")
			m.SetEmojiContext(EmojiContext{PlaceCtx: emojiutil.PlaceContext{Fetcher: ff}, Cells: 2})
			r := RenderMessage(msg, 80, m.renderContext(), RenderOptions{})
			if !strings.Contains(r.Content, "\U0010EEEE") {
				t.Fatal("block text drew no emoji placeholder; the setup does not reach the warm path")
			}
			if len(r.BlockFlushes) != 1 {
				t.Errorf("BlockFlushes = %d, want 1 (the emoji's upload)", len(r.BlockFlushes))
			}
		})
	}
}

// renderedFor builds a model with a single message, runs buildCache
// at the given width, and returns the joined plain-text rendering of
// the first cache entry. Mirrors the existing test-helper pattern in
// plain_test.go and selection_test.go.
func renderedFor(t *testing.T, msg MessageItem, width int) string {
	t.Helper()
	m := New([]MessageItem{msg}, "general")
	m.buildCache(width, false)
	if len(m.cache) == 0 {
		t.Fatal("buildCache produced no entries")
	}
	var lines []string
	for _, e := range m.list.Entries() {
		if e.Ref == 0 {
			lines = e.Lines
			break
		}
	}
	if lines == nil {
		t.Fatal("no entry with msgIdx 0 in cache")
	}
	return ansi.Strip(strings.Join(lines, "\n"))
}

func TestRenderMessagePlainEmitsBlockKitContent(t *testing.T) {
	msg := MessageItem{
		TS:        "1700000000.000000",
		UserName:  "github",
		UserID:    "U-BOT",
		Text:      "PR opened",
		Timestamp: "1:23 PM",
		Blocks: []blockkit.Block{
			blockkit.HeaderBlock{Text: "Pull Request opened"},
			blockkit.SectionBlock{Text: "Pay system: bug fix for retry logic"},
		},
	}
	plain := renderedFor(t, msg, 100)
	// msg.Text ("PR opened") is Slack's notification fallback for a
	// blocks message: it is not drawn beside the blocks, and no empty
	// body row is left where it would have been. See BlocksCarryBody.
	if strings.Contains(plain, "PR opened") {
		t.Errorf("fallback text rendered alongside its blocks: %q", plain)
	}
	if lines := strings.Split(plain, "\n"); len(lines) < 2 || !strings.Contains(lines[1], "Pull Request opened") {
		t.Errorf("row after the header should be the header block, not an empty body row: %q", plain)
	}
	if !strings.Contains(plain, "Pull Request opened") {
		t.Errorf("missing header block: %q", plain)
	}
	if !strings.Contains(plain, "Pay system: bug fix for retry logic") {
		t.Errorf("missing section block: %q", plain)
	}
}

// TestBuildCache_BlocksCarryBodyReactionHitRow: dropping the body row
// must also drop it from the row arithmetic, or every reaction click on
// such a message lands one row below the pill.
func TestBuildCache_BlocksCarryBodyReactionHitRow(t *testing.T) {
	msg := MessageItem{
		TS:        "1700000000.000000",
		UserName:  "deploybot",
		UserID:    "U-BOT",
		Text:      "build #421 green",
		Timestamp: "9:02 AM",
		Blocks: []blockkit.Block{
			blockkit.SectionBlock{Text: "rollout: 100% of shards"},
		},
		Reactions: []ReactionItem{{Emoji: "tada", Count: 1}},
	}
	m := New([]MessageItem{msg}, "general")
	m.buildCache(100, false)
	for _, e := range m.list.Entries() {
		if e.Ref != 0 {
			continue
		}
		if len(reactionHitsOf(e)) == 0 {
			t.Fatal("no reaction hits recorded")
		}
		row := reactionHitsOf(e)[0].RowStart
		if row < 0 || row >= len(e.Lines) {
			t.Fatalf("reaction hit row %d outside the entry's %d lines", row, len(e.Lines))
		}
		if got := ansi.Strip(e.Lines[row]); !strings.Contains(got, "1") || strings.Contains(got, "rollout") {
			var all []string
			for _, l := range e.Lines {
				all = append(all, ansi.Strip(l))
			}
			t.Errorf("reaction hit row %d is %q, want the reaction line; entry: %q", row, got, all)
		}
		return
	}
	t.Fatal("no entry with msgIdx 0 in cache")
}

func TestRenderMessagePlainEmitsLegacyAttachment(t *testing.T) {
	msg := MessageItem{
		TS:        "1700000000.000000",
		UserName:  "pagerduty",
		UserID:    "U-BOT",
		Text:      "alert",
		Timestamp: "1:23 PM",
		LegacyAttachments: []blockkit.LegacyAttachment{{
			Color: "danger",
			Title: "Service down",
			Text:  "checkout-svc 5xx > 1%",
		}},
	}
	plain := renderedFor(t, msg, 100)
	if !strings.Contains(plain, "Service down") {
		t.Errorf("missing legacy title: %q", plain)
	}
	if !strings.Contains(plain, "█") {
		t.Errorf("missing color stripe glyph: %q", plain)
	}
}

// TestRenderMessagePlainPreservesPlainTextRendering guards against
// regressions: a message with no blocks/attachments renders exactly
// as before this task (text body present, no extra spacing).
func TestRenderMessagePlainPreservesPlainTextRendering(t *testing.T) {
	msg := MessageItem{
		TS:        "1700000000.000000",
		UserName:  "alice",
		Text:      "hello world",
		Timestamp: "1:00 PM",
	}
	plain := renderedFor(t, msg, 100)
	if !strings.Contains(plain, "hello world") {
		t.Errorf("plain text body missing: %q", plain)
	}
}

func TestRenderMessagePlainAppendsHintWhenInteractive(t *testing.T) {
	msg := MessageItem{
		TS:        "1700000000.000000",
		UserName:  "deploy-bot",
		Timestamp: "1:23 PM",
		Blocks: []blockkit.Block{
			blockkit.SectionBlock{Text: "Deploy?"},
			blockkit.ActionsBlock{Elements: []blockkit.ActionElement{
				{Kind: "button", Label: "Approve"},
			}},
		},
	}
	plain := renderedFor(t, msg, 100)
	if !strings.Contains(plain, "↗ open in Slack to interact") {
		t.Errorf("expected hint line, got %q", plain)
	}
}

func TestRenderMessagePlainOmitsHintWhenNotInteractive(t *testing.T) {
	msg := MessageItem{
		TS:        "1700000000.000000",
		UserName:  "github",
		Timestamp: "1:23 PM",
		Blocks: []blockkit.Block{
			blockkit.SectionBlock{Text: "PR merged"},
		},
	}
	plain := renderedFor(t, msg, 100)
	if strings.Contains(plain, "↗ open in Slack to interact") {
		t.Errorf("hint should not appear for non-interactive message: %q", plain)
	}
}

// TestMessageTextSource_NoBlocksReturnsRawText: the common case for
// user-typed messages. With no rich_text block on hand, the helper
// just hands msg.Text through unchanged.
func TestMessageTextSource_NoBlocksReturnsRawText(t *testing.T) {
	msg := MessageItem{Text: "hello world"}
	if got := MessageTextSource(msg); got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

// TestMessageTextSource_NonRichTextBlocksReturnRawText: messages that
// have block-kit content (header/section/etc.) but no rich_text body
// continue to return msg.Text, which is what copying the message yields.
// Those block types render separately via the blockkit renderer; whether
// msg.Text also gets a body row is BlocksCarryBody's call.
func TestMessageTextSource_NonRichTextBlocksReturnRawText(t *testing.T) {
	msg := MessageItem{
		Text: "PR opened",
		Blocks: []blockkit.Block{
			blockkit.HeaderBlock{Text: "Pull Request opened"},
			blockkit.SectionBlock{Text: "details"},
		},
	}
	if got := MessageTextSource(msg); got != "PR opened" {
		t.Errorf("got %q, want %q", got, "PR opened")
	}
}

// TestBlocksCarryBody_ContentBlocks: content-bearing blocks draw the
// body themselves, so msg.Text is only Slack's notification fallback and
// must not get a body row of its own.
func TestBlocksCarryBody_ContentBlocks(t *testing.T) {
	msg := MessageItem{
		Text: "PR opened",
		Blocks: []blockkit.Block{
			blockkit.HeaderBlock{Text: "Pull Request opened"},
			blockkit.SectionBlock{Text: "details"},
		},
	}
	if !BlocksCarryBody(msg) {
		t.Error("header + section blocks should carry the body")
	}
}

// TestBlocksCarryBody_KeepsBodyRow: a block set with nothing in it must
// keep the body row, because rendering blank is worse than rendering the
// text twice.
func TestBlocksCarryBody_KeepsBodyRow(t *testing.T) {
	for name, blocks := range map[string][]blockkit.Block{
		"no blocks":     nil,
		"empty section": {blockkit.SectionBlock{}},
		"empty header":  {blockkit.HeaderBlock{}},
		"divider only":  {blockkit.DividerBlock{}},
		"unknown only":  {blockkit.UnknownBlock{Type: "video"}},
	} {
		t.Run(name, func(t *testing.T) {
			msg := MessageItem{Text: "the only readable content", Blocks: blocks}
			if BlocksCarryBody(msg) {
				t.Error("blocks with no content must keep the body row")
			}
		})
	}
}

// TestMessageTextSource_RichTextOverridesLossyText: the bug-fix
// contract. When a message has a rich_text block, the helper
// reconstructs the body from it instead of using Slack's
// newline-stripped text fallback.
func TestMessageTextSource_RichTextOverridesLossyText(t *testing.T) {
	rt := blockkit.RichTextBlock{Elements: []slack.RichTextElement{
		&slack.RichTextSection{
			Type: slack.RTESection,
			Elements: []slack.RichTextSectionElement{
				&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "line1"},
				&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "\n"},
				&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "line2"},
			},
		},
	}}
	msg := MessageItem{
		Text:   "line1 line2", // Slack's lossy fallback (newline → space)
		Blocks: []blockkit.Block{rt},
	}
	got := MessageTextSource(msg)
	if !strings.Contains(got, "line1\nline2") {
		t.Errorf("got %q, want it to contain %q (newline preserved from rich_text)", got, "line1\nline2")
	}
}

// TestBlocksCarryBody_RichTextBodyKeepsBodyRow: a rich_text body renders
// through the body row, so a message carrying one keeps that row even
// when a content-bearing section block sits beside it.
func TestBlocksCarryBody_RichTextBodyKeepsBodyRow(t *testing.T) {
	rt := blockkit.RichTextBlock{Elements: []slack.RichTextElement{
		&slack.RichTextSection{
			Type: slack.RTESection,
			Elements: []slack.RichTextSectionElement{
				&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "rich body"},
			},
		},
	}}
	msg := MessageItem{
		Text:   "rich body",
		Blocks: []blockkit.Block{rt, blockkit.SectionBlock{Text: "details"}},
	}
	if BlocksCarryBody(msg) {
		t.Error("a rich_text body must keep its body row")
	}
}

// TestRichTextTonePerPersonEmojiKeepsNextEmoji: a two-tone 🤝 from
// Slack's composer becomes ":handshake::skin-tone-2-5:" in the body, and
// the 🎉 after it must survive the glyph render and the Markdown export.
func TestRichTextTonePerPersonEmojiKeepsNextEmoji(t *testing.T) {
	rt := blockkit.RichTextBlock{Elements: []slack.RichTextElement{
		&slack.RichTextSection{
			Type: slack.RTESection,
			Elements: []slack.RichTextSectionElement{
				&slack.RichTextSectionEmojiElement{Type: slack.RTSEEmoji, Name: "handshake", Unicode: "1faf1-1f3fb-200d-1faf2-1f3fe"},
				&slack.RichTextSectionEmojiElement{Type: slack.RTSEEmoji, Name: "tada", Unicode: "1f389"},
			},
		},
	}}
	msg := MessageItem{
		TS:        "1700000000.000000",
		UserName:  "alice",
		Timestamp: "1:23 PM",
		Text:      ":handshake::skin-tone-2-5::tada:",
		Blocks:    []blockkit.Block{rt},
	}
	if got := renderedFor(t, msg, 80); !strings.Contains(got, "🤝") || !strings.Contains(got, "🎉") {
		t.Errorf("rendered %q, want 🤝 and 🎉", got)
	}
	if got, want := SlackMrkdwnToCommonMark(MessageTextSource(msg), nil, nil), "🤝🎉"; got != want {
		t.Errorf("export body = %q, want %q", got, want)
	}
}

// TestRenderMessagePlainRichTextProducesMultipleLines: integration
// test for the bug — a rendered rich_text-bodied message must
// produce multiple body lines, not one squashed line. Mirrors the
// GitHub Pending Review reproduction.
func TestRenderMessagePlainRichTextProducesMultipleLines(t *testing.T) {
	rt := blockkit.RichTextBlock{Elements: []slack.RichTextElement{
		&slack.RichTextSection{
			Type: slack.RTESection,
			Elements: []slack.RichTextSectionElement{
				&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "PR #1: fix retries"},
				&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "\n"},
				&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "PR #2: docs typo"},
				&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "\n"},
				&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "PR #3: bump deps"},
			},
		},
	}}
	msg := MessageItem{
		TS:        "1700000000.000000",
		UserName:  "github",
		UserID:    "U-BOT",
		Text:      "PR #1: fix retries  PR #2: docs typo  PR #3: bump deps", // lossy
		Timestamp: "1:23 PM",
		Blocks:    []blockkit.Block{rt},
	}
	plain := renderedFor(t, msg, 100)
	for _, want := range []string{"PR #1: fix retries", "PR #2: docs typo", "PR #3: bump deps"} {
		if !strings.Contains(plain, want) {
			t.Errorf("missing %q in rendered output: %q", want, plain)
		}
	}
	// Each PR should be on its own line — count occurrences of "PR #"
	// at line boundaries.
	prLines := 0
	for _, line := range strings.Split(plain, "\n") {
		if strings.Contains(line, "PR #") {
			prLines++
		}
	}
	if prLines < 3 {
		t.Errorf("expected >=3 lines containing 'PR #' (one per PR), got %d. Full output:\n%s", prLines, plain)
	}
}

// TestBuildCache_EphemeralLabelReactionHitRow: the label row must enter
// the row arithmetic, or every hit below it lands one row high.
func TestBuildCache_EphemeralLabelReactionHitRow(t *testing.T) {
	msg := MessageItem{
		TS: "1700000000.000000", UserName: "slackbot", UserID: "USLACKBOT",
		Text: "not in channel", Timestamp: "9:02 AM", Ephemeral: true,
		Reactions: []ReactionItem{{Emoji: "tada", Count: 1}},
	}
	m := New([]MessageItem{msg}, "general")
	m.buildCache(100, false)
	for _, e := range m.list.Entries() {
		if e.Ref != 0 {
			continue
		}
		if len(reactionHitsOf(e)) == 0 {
			t.Fatal("no reaction hits recorded")
		}
		row := reactionHitsOf(e)[0].RowStart
		if row < 0 || row >= len(e.Lines) {
			t.Fatalf("reaction hit row %d outside the entry's %d lines", row, len(e.Lines))
		}
		if got := ansi.Strip(e.Lines[row]); !strings.Contains(got, "1") || strings.Contains(got, "not in channel") {
			t.Errorf("reaction hit row %d is %q, want the reaction line", row, got)
		}
		return
	}
	t.Fatal("no entry with msgIdx 0 in cache")
}

func TestEphemeralButtonsHintSaysPressB(t *testing.T) {
	msg := MessageItem{
		TS: "1700000000.000000", UserName: "Slackbot", Timestamp: "1:23 PM", Ephemeral: true,
		Text: "not in channel",
		LegacyAttachments: []blockkit.LegacyAttachment{{
			Actions: []blockkit.LegacyAction{{Name: "ignore", Text: "Dismiss", Type: "button"}},
		}},
	}
	plain := renderedFor(t, msg, 100)
	if !strings.Contains(plain, "b to respond") || strings.Contains(plain, "open in Slack") {
		t.Errorf("want the b hint instead of the Slack one: %q", plain)
	}
	msg.Ephemeral = false
	if plain := renderedFor(t, msg, 100); !strings.Contains(plain, "↗ open in Slack to interact") {
		t.Errorf("a non-ephemeral keeps the Slack hint: %q", plain)
	}
}

// TestRenderMessage_AttachmentMentionShowsTitle: a link Slack's web client
// sent as an app chip (a rich_text attachment_mention, e.g. a Linear
// issue) shows the chip's title in place of the URL, the way the official
// client does, and `o` still offers the URL with the title as its label.
func TestRenderMessage_AttachmentMentionShowsTitle(t *testing.T) {
	const url = "https://linear.app/acme/issue/ENG-42/fix-the-thing"
	var blocks slack.Blocks
	raw := `[{"type":"rich_text","block_id":"b1","elements":[{"type":"rich_text_section","elements":[` +
		`{"type":"text","text":"see "},` +
		`{"type":"attachment_mention","url":"` + url + `","text":"Fix the thing","app_id":"A0123456789","product_name":"Linear"}` +
		`]}]}]`
	if err := json.Unmarshal([]byte(raw), &blocks); err != nil {
		t.Fatalf("unmarshal blocks: %v", err)
	}
	msg := MessageItem{
		TS:        "1700000000.000100",
		UserName:  "grant",
		UserID:    "U1",
		Timestamp: "10:00 AM",
		Text:      "see <" + url + ">",
		Blocks:    blockkit.Parse(blocks),
	}
	got := renderedFor(t, msg, 100)
	if !strings.Contains(got, "see Fix the thing") {
		t.Errorf("rendered body should show the chip title; got:\n%s", got)
	}
	if strings.Contains(got, "linear.app") {
		t.Errorf("rendered body should not show the raw URL; got:\n%s", got)
	}
	links := MessageLinks(msg)
	if len(links) == 0 || links[0].URL != url || links[0].Label != "Fix the thing" {
		t.Errorf("MessageLinks = %+v, want first link %q labelled %q", links, url, "Fix the thing")
	}
}

// TestMessageLinks_AttachmentMentionTitleUnescaped: the chip title is
// entity-escaped inside <url|title>; the link picker shows it decoded,
// the same as the pane does.
func TestMessageLinks_AttachmentMentionTitleUnescaped(t *testing.T) {
	var blocks slack.Blocks
	raw := `[{"type":"rich_text","block_id":"b1","elements":[{"type":"rich_text_section","elements":[` +
		`{"type":"attachment_mention","url":"https://example.com/x","text":"A -> B & <C>"}]}]}]`
	if err := json.Unmarshal([]byte(raw), &blocks); err != nil {
		t.Fatalf("unmarshal blocks: %v", err)
	}
	msg := MessageItem{TS: "1700000000.000100", Text: "<https://example.com/x>", Blocks: blockkit.Parse(blocks)}
	links := MessageLinks(msg)
	if len(links) != 1 || links[0].Label != "A -> B & <C>" {
		t.Errorf("MessageLinks = %+v, want one link labelled %q", links, "A -> B & <C>")
	}
	if got := renderedFor(t, msg, 100); !strings.Contains(got, "A -> B & <C>") {
		t.Errorf("rendered body should show the decoded title; got:\n%s", got)
	}
}
