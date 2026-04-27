package messages

import (
	"fmt"
	"image/color"
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	emojiutil "github.com/gammons/slk/internal/emoji"
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
//
// Inline styles (bold, italic, link, mention) intentionally omit
// .Background() -- the outer MessageText style provides the background.
// Code styles use styles.Surface (a different bg) so they keep their own.
func boldStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true)
}
func italicStyle() lipgloss.Style {
	return lipgloss.NewStyle().Italic(true)
}
func strikethroughStyle() lipgloss.Style {
	return lipgloss.NewStyle().Strikethrough(true)
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
		Underline(true)
}
func mentionStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(styles.Primary).
		Bold(true)
}
func blockquoteStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(styles.TextMuted).
		BorderStyle(lipgloss.ThickBorder()).
		BorderLeft(true).
		BorderForeground(styles.TextMuted).
		PaddingLeft(1)
}

// reapplyBgAfterResets post-processes ANSI text to re-apply a background
// color after every ANSI reset sequence (\033[0m). This prevents inline
// styled text (bold, link, mention) from clearing the outer background
// when their ANSI reset fires.
// WordWrap wraps text to the given width using lipgloss.Width() for
// measurement. This is critical because muesli/reflow/wordwrap uses
// go-runewidth internally, which miscounts VS16 variation selector emoji.
// lipgloss v2 uses clipperhouse/displaywidth which handles these correctly.
func WordWrap(s string, limit int) string {
	if limit <= 0 {
		return s
	}
	var result strings.Builder
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			result.WriteByte('\n')
		}
		wrapLine(&result, line, limit)
	}
	return result.String()
}

// wrapLine wraps a single line at word boundaries using lipgloss.Width.
func wrapLine(buf *strings.Builder, line string, limit int) {
	words := strings.Fields(line)
	if len(words) == 0 {
		return
	}

	currentWidth := 0
	for i, word := range words {
		wordWidth := lipgloss.Width(word)
		if i == 0 {
			buf.WriteString(word)
			currentWidth = wordWidth
			continue
		}
		// +1 for the space before the word
		if currentWidth+1+wordWidth > limit {
			buf.WriteByte('\n')
			buf.WriteString(word)
			currentWidth = wordWidth
		} else {
			buf.WriteByte(' ')
			buf.WriteString(word)
			currentWidth += 1 + wordWidth
		}
	}
}

// ReapplyBgAfterResets is exported for use by other UI packages (e.g. sidebar).
// Handles both \x1b[m and \x1b[0m reset forms.
func ReapplyBgAfterResets(text string, bg string) string {
	if bg == "" {
		return text
	}
	// lipgloss v2 uses \x1b[m (no 0), but handle both forms
	text = strings.ReplaceAll(text, "\x1b[m", "\x1b[m"+bg)
	return text
}

var (
	cachedBgANSI  string
	cachedBgColor color.Color
)

// BgANSI returns the ANSI escape sequence for the current theme background.
// Exported so sidebar and other packages can use it.
// The result is cached and only recomputed when the background color changes.
func BgANSI() string {
	bg := styles.Background
	if bg == cachedBgColor && cachedBgANSI != "" {
		return cachedBgANSI
	}
	r, g, b, _ := bg.RGBA()
	cachedBgANSI = fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)
	cachedBgColor = bg
	return cachedBgANSI
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

	output := strings.Join(result, "\n")

	// Post-process: re-apply theme background after every ANSI reset so that
	// inline styled text (bold, link, mention) doesn't leave dark patches
	// where the terminal's default background shows through.
	output = ReapplyBgAfterResets(output, BgANSI())

	return output
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
	// Strip VS16 from text-default characters so width measurement
	// matches terminal rendering (many terminals render these as 1-wide
	// regardless of VS16).
	text = emojiutil.StripTextDefaultVS16(emoji.Sprint(text))

	return text
}
