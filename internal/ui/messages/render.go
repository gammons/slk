package messages

import (
	"fmt"
	"image/color"
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

	// Slack link patterns: <url|label> or <url>.
	// linkWithLabelRe requires https?:// so it does NOT match channel
	// mentions <#CHANNEL_ID|name>, group mentions <!subteam^...|@team>,
	// or other Slack-internal angle-bracket forms. Those are handled by
	// dedicated regexes below.
	linkWithLabelRe = regexp.MustCompile(`<(https?://[^|>]+)\|([^>]+)>`)
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
// They DO set Foreground(TextPrimary) explicitly: lipgloss emits an
// ANSI reset (\x1b[m) at the end of each styled span which clears the
// surrounding foreground, so without an explicit fg the styled text
// would render in the terminal's default foreground (often light gray)
// instead of the theme's text color. This is especially visible on
// light-background themes (e.g. Slack Default) with italic system
// messages like "has joined the channel".
// Code styles use styles.Surface (a different bg) so they keep their own.
func boldStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(styles.TextPrimary)
}
func italicStyle() lipgloss.Style {
	return lipgloss.NewStyle().Italic(true).Foreground(styles.TextPrimary)
}
func strikethroughStyle() lipgloss.Style {
	return lipgloss.NewStyle().Strikethrough(true).Foreground(styles.TextPrimary)
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

// RenderAttachments returns a styled string with one line per attachment,
// each prefixed with a [Image] or [File] marker, the filename, and the URL.
// The whole line is wrapped in an OSC 8 hyperlink escape so it's clickable
// in modern terminals. Returns "" if there are no attachments.
//
// Output format per attachment:
//   [Image] screenshot.png  https://files.slack.com/...
func RenderAttachments(attachments []Attachment) string {
	if len(attachments) == 0 {
		return ""
	}
	markerStyle := lipgloss.NewStyle().Foreground(styles.TextMuted).Bold(true)
	urlStyle := linkStyle()

	lines := make([]string, 0, len(attachments))
	for _, a := range attachments {
		marker := "[File]"
		if a.Kind == "image" {
			marker = "[Image]"
		}
		styledMarker := markerStyle.Render(marker)
		styledURL := urlStyle.Render(a.URL)
		// Wrap the visible body (marker + name + URL) in OSC 8 so the
		// terminal makes the entire line clickable / openable.
		body := styledMarker + " " + a.Name + "  " + styledURL
		lines = append(lines, osc8Hyperlink(a.URL, body))
	}
	return strings.Join(lines, "\n")
}

// osc8Hyperlink wraps the rendered label in an OSC 8 hyperlink escape so
// terminals that support it (alacritty >=0.11, kitty, iterm2, wezterm, foot,
// recent gnome-terminal) make `label` clickable. Terminals without OSC 8
// support display only the label (they ignore the escape sequence).
//
// The format is: ESC ] 8 ;; URL ESC \ LABEL ESC ] 8 ;; ESC \
//
// We use the BEL terminator (\x07) instead of ESC \ for compatibility with
// some terminals that mishandle the latter; both are valid per the spec.
func osc8Hyperlink(url, label string) string {
	return "\x1b]8;;" + url + "\x1b\\" + label + "\x1b]8;;\x1b\\"
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
//
// The `style` argument is one or more ANSI escape sequences (commonly a bg
// color, or a bg+fg pair) that will be re-emitted after every reset so that
// inline styled spans don't leak the terminal's defaults through. Callers
// that only need to restore the background can pass just BgANSI(); callers
// that also need to restore the foreground (so plain text following a styled
// span — e.g. the body after a <@user> mention — keeps the theme text color)
// should pass BgANSI()+FgANSI().
func ReapplyBgAfterResets(text string, style string) string {
	if style == "" {
		return text
	}
	// lipgloss v2 uses \x1b[m (no 0), but handle both forms
	text = strings.ReplaceAll(text, "\x1b[m", "\x1b[m"+style)
	return text
}

var (
	cachedBgANSI         string
	cachedBgColor        color.Color
	cachedSidebarBgANSI  string
	cachedSidebarBgColor color.Color
	cachedFgANSI         string
	cachedFgColor        color.Color
	cachedSidebarFgANSI  string
	cachedSidebarFgColor color.Color
)

// bgANSIFor returns the ANSI 24-bit background-color escape for c.
func bgANSIFor(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)
}

// fgANSIFor returns the ANSI 24-bit foreground-color escape for c.
func fgANSIFor(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r>>8, g>>8, b>>8)
}

// BgANSI returns the ANSI escape sequence for the current theme background.
// Exported so sidebar and other packages can use it.
// The result is cached and only recomputed when the background color changes.
func BgANSI() string {
	bg := styles.Background
	if bg == cachedBgColor && cachedBgANSI != "" {
		return cachedBgANSI
	}
	cachedBgANSI = bgANSIFor(bg)
	cachedBgColor = bg
	return cachedBgANSI
}

// SidebarBgANSI returns the ANSI escape sequence for the current theme's
// sidebar background. The sidebar uses this instead of BgANSI so that
// inline-styled glyphs (private/DM prefixes, cursor, unread dots) re-apply
// the correct sidebar color after their ANSI reset, rather than leaking
// the message-pane background through (most visible on themes like
// Slack Default where the sidebar bg differs from the message bg).
func SidebarBgANSI() string {
	bg := styles.SidebarBackground
	if bg == cachedSidebarBgColor && cachedSidebarBgANSI != "" {
		return cachedSidebarBgANSI
	}
	cachedSidebarBgANSI = bgANSIFor(bg)
	cachedSidebarBgColor = bg
	return cachedSidebarBgANSI
}

// FgANSI returns the ANSI escape for the current theme's primary text
// foreground. Combine with BgANSI when re-applying styles after resets so
// plain text following an inline-styled span (e.g. text after a mention or
// an italic system phrase like "has joined the channel") keeps the theme's
// text color instead of falling back to the terminal default.
func FgANSI() string {
	fg := styles.TextPrimary
	if fg == cachedFgColor && cachedFgANSI != "" {
		return cachedFgANSI
	}
	cachedFgANSI = fgANSIFor(fg)
	cachedFgColor = fg
	return cachedFgANSI
}

// SidebarFgANSI is like FgANSI but for the sidebar's primary text color.
func SidebarFgANSI() string {
	fg := styles.SidebarText
	if fg == cachedSidebarFgColor && cachedSidebarFgANSI != "" {
		return cachedSidebarFgANSI
	}
	cachedSidebarFgANSI = fgANSIFor(fg)
	cachedSidebarFgColor = fg
	return cachedSidebarFgANSI
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

	// Post-process: re-apply theme background AND foreground after every
	// ANSI reset so that inline styled text (bold, link, mention) doesn't
	// leave dark patches (where the terminal's default bg shows through)
	// or revert plain text following the styled span to the terminal's
	// default fg (most noticeable on light-bg themes like Slack Default,
	// where text after a mention would otherwise render in a light gray).
	output = ReapplyBgAfterResets(output, BgANSI()+FgANSI())

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

	// Links with labels: <url|label> -> "label (url)"; the label is wrapped
	// in an OSC 8 hyperlink escape so it's clickable in modern terminals,
	// and the raw URL is shown after the label so it's also visible to
	// terminals without OSC 8 support and copy-paste-friendly.
	text = linkWithLabelRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := linkWithLabelRe.FindStringSubmatch(match)
		url, label := parts[1], parts[2]
		return osc8Hyperlink(url, linkStyle().Render(label)) + " (" + url + ")"
	})

	// Bare links: <url> -> url, wrapped in OSC 8 so it's clickable.
	text = linkBareRe.ReplaceAllStringFunc(text, func(match string) string {
		url := linkBareRe.FindStringSubmatch(match)[1]
		return osc8Hyperlink(url, linkStyle().Render(url))
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
