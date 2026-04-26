package messages

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
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

	// Styles for formatted text
	boldStyle          = lipgloss.NewStyle().Bold(true)
	italicStyle        = lipgloss.NewStyle().Italic(true)
	strikethroughStyle = lipgloss.NewStyle().Strikethrough(true)
	codeStyle          = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#E0A030")).
				Background(lipgloss.Color("#2A2A3A"))
	codeBlockStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E0A030")).
			Background(lipgloss.Color("#2A2A3A")).
			Padding(0, 1)
	linkStyle = lipgloss.NewStyle().
			Foreground(styles.Primary).
			Underline(true)
	mentionStyle = lipgloss.NewStyle().
			Foreground(styles.Primary).
			Bold(true)
	blockquoteStyle = lipgloss.NewStyle().
			Foreground(styles.TextMuted).
			BorderStyle(lipgloss.ThickBorder()).
			BorderLeft(true).
			BorderForeground(styles.TextMuted).
			PaddingLeft(1)
)

// RenderSlackMarkdown converts Slack-flavored markdown and emoji shortcodes
// into lipgloss-styled terminal output. If userNames is provided, user mentions
// like <@U1234> are resolved to display names.
func RenderSlackMarkdown(text string, userNames map[string]string) string {
	// Handle code blocks first (before other formatting to avoid conflicts)
	text = codeBlockRe.ReplaceAllStringFunc(text, func(match string) string {
		inner := codeBlockRe.FindStringSubmatch(match)[1]
		inner = strings.TrimSpace(inner)
		return "\n" + codeBlockStyle.Render(inner) + "\n"
	})

	// Process line by line for blockquotes
	lines := strings.Split(text, "\n")
	var result []string
	for _, line := range lines {
		if strings.HasPrefix(line, "&gt; ") || strings.HasPrefix(line, "> ") {
			quoted := strings.TrimPrefix(line, "&gt; ")
			quoted = strings.TrimPrefix(quoted, "> ")
			line = blockquoteStyle.Render(quoted)
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
		return codeStyle.Render(inner)
	})

	// Bold
	text = boldRe.ReplaceAllStringFunc(text, func(match string) string {
		inner := boldRe.FindStringSubmatch(match)[1]
		return boldStyle.Render(inner)
	})

	// Italic
	text = italicRe.ReplaceAllStringFunc(text, func(match string) string {
		inner := italicRe.FindStringSubmatch(match)[1]
		return italicStyle.Render(inner)
	})

	// Strikethrough
	text = strikethroughRe.ReplaceAllStringFunc(text, func(match string) string {
		inner := strikethroughRe.FindStringSubmatch(match)[1]
		return strikethroughStyle.Render(inner)
	})

	// Links with labels: <url|label> -> label (styled)
	text = linkWithLabelRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := linkWithLabelRe.FindStringSubmatch(match)
		return linkStyle.Render(parts[2])
	})

	// Bare links: <url> -> url (styled)
	text = linkBareRe.ReplaceAllStringFunc(text, func(match string) string {
		url := linkBareRe.FindStringSubmatch(match)[1]
		return linkStyle.Render(url)
	})

	// Channel mentions: <#C1234|channel-name> -> #channel-name
	text = channelMentionRe.ReplaceAllStringFunc(text, func(match string) string {
		name := channelMentionRe.FindStringSubmatch(match)[1]
		return mentionStyle.Render("#" + name)
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
		return mentionStyle.Render("@" + name)
	})

	// Emoji shortcodes: :red_circle: -> 🔴
	text = emoji.Sprint(text)

	return text
}
