package messages

import (
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/kyokomi/emoji/v2"
)

var (
	// Slack formatting patterns
	boldRe          = regexp.MustCompile(`\*([^*\n]+)\*`)
	italicRe        = regexp.MustCompile(`_([^_\n]+)_`)
	strikethroughRe = regexp.MustCompile(`~([^~\n]+)~`)
	inlineCodeRe    = regexp.MustCompile("`([^`\n]+)`")
	codeBlockRe     = regexp.MustCompile("(?s)```(.+?)```")

	// Slack link patterns: <url|label> or <url>
	linkWithLabelRe = regexp.MustCompile(`<([^|>]+)\|([^>]+)>`)
	linkBareRe      = regexp.MustCompile(`<(https?://[^>]+)>`)

	// Slack user/channel mentions: <@U1234> <#C1234|channel-name>
	userMentionRe    = regexp.MustCompile(`<@([A-Z0-9]+)>`)
	channelMentionRe = regexp.MustCompile(`<#[A-Z0-9]+\|([^>]+)>`)
)

// Render styles -- functions that read current theme colors so they
// update correctly when the theme changes.
func boldStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Background(styles.Background)
}
func italicStyle() lipgloss.Style {
	return lipgloss.NewStyle().Italic(true).Background(styles.Background)
}
func strikethroughStyle() lipgloss.Style {
	return lipgloss.NewStyle().Strikethrough(true).Background(styles.Background)
}
func codeStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(styles.Warning).
		Background(styles.Surface)
}
func codeBlockStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(styles.Warning).
		Background(styles.Surface).
		Padding(0, 1)
}
func linkStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(styles.Primary).
		Background(styles.Background).
		Underline(true)
}
func mentionStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(styles.Primary).
		Background(styles.Background).
		Bold(true)
}
func blockquoteStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(styles.TextMuted).
		Background(styles.Background).
		BorderStyle(lipgloss.ThickBorder()).
		BorderLeft(true).
		BorderForeground(styles.TextMuted).
		PaddingLeft(1)
}

// RenderSlackMarkdown converts Slack-flavored markdown and emoji shortcodes
// into lipgloss-styled terminal output. If userNames is provided, user mentions
// like <@U1234> are resolved to display names.
func RenderSlackMarkdown(text string, userNames map[string]string) string {
	// Handle code blocks first (before other formatting to avoid conflicts)
	text = codeBlockRe.ReplaceAllStringFunc(text, func(match string) string {
		inner := codeBlockRe.FindStringSubmatch(match)[1]
		inner = strings.TrimSpace(inner)
		return "\n" + codeBlockStyle().Render(inner) + "\n"
	})

	// Process line by line for blockquotes
	lines := strings.Split(text, "\n")
	var result []string
	for _, line := range lines {
		if strings.HasPrefix(line, "&gt; ") || strings.HasPrefix(line, "> ") {
			quoted := strings.TrimPrefix(line, "&gt; ")
			quoted = strings.TrimPrefix(quoted, "> ")
			line = blockquoteStyle().Render(quoted)
		} else {
			line = renderInlineFormatting(line, userNames)
		}
		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

func renderInlineFormatting(text string, userNames map[string]string) string {
	// Inline code (before bold/italic to avoid conflicts inside code)
	text = inlineCodeRe.ReplaceAllStringFunc(text, func(match string) string {
		inner := inlineCodeRe.FindStringSubmatch(match)[1]
		return codeStyle().Render(inner)
	})

	// Bold
	text = boldRe.ReplaceAllStringFunc(text, func(match string) string {
		inner := boldRe.FindStringSubmatch(match)[1]
		return boldStyle().Render(inner)
	})

	// Italic
	text = italicRe.ReplaceAllStringFunc(text, func(match string) string {
		inner := italicRe.FindStringSubmatch(match)[1]
		return italicStyle().Render(inner)
	})

	// Strikethrough
	text = strikethroughRe.ReplaceAllStringFunc(text, func(match string) string {
		inner := strikethroughRe.FindStringSubmatch(match)[1]
		return strikethroughStyle().Render(inner)
	})

	// Links with labels: <url|label> -> label (styled)
	text = linkWithLabelRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := linkWithLabelRe.FindStringSubmatch(match)
		return linkStyle().Render(parts[2])
	})

	// Bare links: <url> -> url (styled)
	text = linkBareRe.ReplaceAllStringFunc(text, func(match string) string {
		url := linkBareRe.FindStringSubmatch(match)[1]
		return linkStyle().Render(url)
	})

	// Channel mentions: <#C1234|channel-name> -> #channel-name
	text = channelMentionRe.ReplaceAllStringFunc(text, func(match string) string {
		name := channelMentionRe.FindStringSubmatch(match)[1]
		return mentionStyle().Render("#" + name)
	})

	// User mentions: <@U1234> -> @DisplayName (or @U1234 if not resolved)
	text = userMentionRe.ReplaceAllStringFunc(text, func(match string) string {
		userID := userMentionRe.FindStringSubmatch(match)[1]
		name := userID
		if userNames != nil {
			if resolved, ok := userNames[userID]; ok {
				name = resolved
			}
		}
		return mentionStyle().Render("@" + name)
	})

	// Emoji shortcodes: :red_circle: -> 🔴
	text = emoji.Sprint(text)

	return text
}
