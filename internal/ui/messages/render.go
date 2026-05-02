package messages

import (
	"fmt"
	"image/color"
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	emojiutil "github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/kyokomi/emoji/v2"
	"github.com/rivo/uniseg"
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
	userMentionRe = regexp.MustCompile(`<@([A-Z0-9]+)>`)
	// channelMentionRe matches both wire forms Slack accepts:
	//   <#CHANNELID>          — bare ID (sometimes emitted by other clients,
	//                           and what we used to emit ourselves)
	//   <#CHANNELID|name>     — ID with embedded display name
	// Group 1 is the ID; group 2 (optional) is the embedded name. When
	// group 2 is empty we fall back to the channelNames map and finally
	// to "channel" so the user sees something readable rather than the
	// raw <#CID> token.
	channelMentionRe = regexp.MustCompile(`<#([A-Z0-9]+)(?:\|([^>]+))?>`)

	// Slack escapes &, <, > in user-typed text per
	// https://api.slack.com/reference/surfaces/formatting#escaping.
	// We decode AFTER all markup regexes (which consume legitimate
	// <...> markers) so escaped user input doesn't get reinterpreted
	// as Slack markup. Using a NewReplacer rather than html.UnescapeString
	// to avoid decoding entities Slack does not produce.
	slackEntityDecoder = strings.NewReplacer(
		"&lt;", "<",
		"&gt;", ">",
		"&amp;", "&",
	)
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
// each prefixed with a [Image] or [File] marker followed by the URL. The
// whole line is wrapped in an OSC 8 hyperlink escape so it's clickable in
// modern terminals. Returns "" if there are no attachments.
//
// Filenames are intentionally omitted: most Slack file names are noisy
// (e.g. UUID-style image names) and including them in addition to the
// already-long URL pushed message lines past the panel width.
//
// Output format per attachment:
//   [Image] https://files.slack.com/...
//
// Callers must pass the result through WordWrap before composing it into
// a width-bounded layout, since file URLs frequently exceed the panel
// content width.
func RenderAttachments(attachments []Attachment) string {
	if len(attachments) == 0 {
		return ""
	}
	lines := make([]string, 0, len(attachments))
	for _, a := range attachments {
		lines = append(lines, renderSingleAttachment(a))
	}
	return strings.Join(lines, "\n")
}

// renderSingleAttachment formats one attachment as the legacy single-line
// "[Image] <url>" or "[File] <url>" form, wrapped in an OSC 8 hyperlink.
// The messages-pane image-rendering pipeline uses this when no inline
// renderer is available (ProtoOff, missing thumbs) and the thread pane
// uses it via RenderAttachments for all attachments.
func renderSingleAttachment(a Attachment) string {
	markerStyle := lipgloss.NewStyle().Foreground(styles.TextMuted).Bold(true)
	urlStyle := linkStyle()
	marker := "[File]"
	if a.Kind == "image" {
		marker = "[Image]"
	}
	body := markerStyle.Render(marker) + " " + urlStyle.Render(a.URL)
	return osc8Hyperlink(a.URL, body)
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
// Words wider than limit are hard-broken via ansi.Hardwrap so no output line
// exceeds the limit. Leaving an overlong line intact would cause the
// terminal to soft-wrap it on its own, which lipgloss height arithmetic
// can't see and which would push downstream layout (e.g. the thread
// compose box) over content above it.
func wrapLine(buf *strings.Builder, line string, limit int) {
	words := strings.Fields(line)
	if len(words) == 0 {
		return
	}

	currentWidth := 0
	writeWord := func(w string) {
		// w may itself be wider than limit; hard-break by display columns.
		// ansi.Hardwrap is ANSI-aware and grapheme-aware.
		wWidth := lipgloss.Width(w)
		if wWidth <= limit {
			buf.WriteString(w)
			currentWidth = wWidth
			return
		}
		wrapped := ansi.Hardwrap(w, limit, false)
		buf.WriteString(wrapped)
		// Track width of the trailing segment so a following word can
		// share its line if it fits.
		if nl := strings.LastIndexByte(wrapped, '\n'); nl >= 0 {
			currentWidth = lipgloss.Width(wrapped[nl+1:])
		} else {
			currentWidth = lipgloss.Width(wrapped)
		}
	}

	for i, word := range words {
		wordWidth := lipgloss.Width(word)
		if i == 0 {
			writeWord(word)
			continue
		}
		// +1 for the space before the word
		if currentWidth+1+wordWidth > limit {
			buf.WriteByte('\n')
			writeWord(word)
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

	// Selection-tint ANSI cache, focused/unfocused. Recomputed when
	// the underlying SelectionTintColor changes (via Apply()).
	cachedSelTintBgFocusedANSI    string
	cachedSelTintBgFocusedColor   color.Color
	cachedSelTintBgUnfocusedANSI  string
	cachedSelTintBgUnfocusedColor color.Color
)

// SelectionTintBgANSI returns the ANSI 24-bit bg escape for the current
// SelectionTintColor at the given focus state. Used by the messages and
// thread panels to repaint inner explicit-bg styles (Username, Timestamp,
// MessageText, RenderSlackMarkdown's reset-reapplications, etc.) so the
// tint reaches every cell of the selected row, not just the trailing
// whitespace and gutter.
func SelectionTintBgANSI(focused bool) string {
	c := styles.SelectionTintColor(focused)
	if focused {
		if c == cachedSelTintBgFocusedColor && cachedSelTintBgFocusedANSI != "" {
			return cachedSelTintBgFocusedANSI
		}
		cachedSelTintBgFocusedANSI = bgANSIFor(c)
		cachedSelTintBgFocusedColor = c
		return cachedSelTintBgFocusedANSI
	}
	if c == cachedSelTintBgUnfocusedColor && cachedSelTintBgUnfocusedANSI != "" {
		return cachedSelTintBgUnfocusedANSI
	}
	cachedSelTintBgUnfocusedANSI = bgANSIFor(c)
	cachedSelTintBgUnfocusedColor = c
	return cachedSelTintBgUnfocusedANSI
}

// RepaintBgToSelectionTint replaces every occurrence of the theme
// background SGR parameters in s with the SelectionTintColor parameters.
// Used to rebuild a selected-message variant from a rendered "normal"
// message string without re-running the full render pipeline.
//
// Inner styles like Username/Timestamp/MessageText set Background(styles.Background)
// explicitly, and RenderSlackMarkdown emits BgANSI()+FgANSI() after every
// \x1b[m reset to avoid dark patches around inline-styled spans. Those
// theme-bg escapes show through as dark cells on the tinted row unless
// we substitute them. styles.Surface (used by code blocks) is intentionally
// untouched — code blocks keep their distinct surface background even on
// a selected row.
//
// Implementation note: lipgloss/v2 combines multiple SGR codes into a
// single escape sequence (e.g. "\x1b[1;38;2;R;G;B;48;2;R;G;Bm" for
// bold + fg + bg), so substituting the framed "\x1b[48;2;R;G;Bm" form
// would miss every occurrence where the bg is bundled with other
// attributes. We strip the "\x1b[" prefix and "m" suffix and match
// just the bg-parameter substring "48;2;R;G;B", which appears
// verbatim regardless of how lipgloss bundles other SGR params around
// it.
func RepaintBgToSelectionTint(s string, focused bool) string {
	from := bgSGRParams(BgANSI())
	to := bgSGRParams(SelectionTintBgANSI(focused))
	if from == "" || from == to {
		return s
	}
	return strings.ReplaceAll(s, from, to)
}

// bgSGRParams strips the "\x1b[" prefix and "m" suffix from a bg ANSI
// escape, returning just the parameter substring (e.g. "48;2;26;26;46").
// Returns "" if the input doesn't have the expected framing.
func bgSGRParams(ansi string) string {
	const prefix = "\x1b["
	const suffix = "m"
	if !strings.HasPrefix(ansi, prefix) || !strings.HasSuffix(ansi, suffix) {
		return ""
	}
	return ansi[len(prefix) : len(ansi)-len(suffix)]
}

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
// like <@U1234> are resolved to display names. If channelNames is provided,
// bare <#C1234> channel mentions (without an embedded name) are resolved
// to #channel-name; mentions that already carry the embedded |name form
// don't need the map.
func RenderSlackMarkdown(text string, userNames map[string]string, channelNames map[string]string) string {
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
			quoted = slackEntityDecoder.Replace(quoted)
			line = blockquoteStyle().Render(quoted)
		} else {
			line = renderInlineFormatting(line, userNames, channelNames)
			// Decode Slack-escaped entities after markup regexes have
			// consumed legitimate <...> markers, so escaped user input
			// (e.g. literal "<@U1>") doesn't become a fake mention.
			line = slackEntityDecoder.Replace(line)
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

func renderInlineFormatting(text string, userNames map[string]string, channelNames map[string]string) string {
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

	// Links with labels: <url|label> -> just the label, wrapped in an OSC 8
	// hyperlink escape so it's clickable in modern terminals. We don't
	// append the raw URL: every terminal slk targets supports OSC 8 (or its
	// own URL auto-detection / shift-click), and the trailing "(url)"
	// duplicated noise on every labeled link.
	text = linkWithLabelRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := linkWithLabelRe.FindStringSubmatch(match)
		url, label := parts[1], parts[2]
		return osc8Hyperlink(url, linkStyle().Render(label))
	})

	// Bare links: <url> -> url, wrapped in OSC 8 so it's clickable.
	text = linkBareRe.ReplaceAllStringFunc(text, func(match string) string {
		url := linkBareRe.FindStringSubmatch(match)[1]
		return osc8Hyperlink(url, linkStyle().Render(url))
	})

	// Channel mentions: <#C1234|channel-name> -> #channel-name, or
	// <#C1234> -> #resolved-name (via channelNames map). When the
	// channel can't be resolved we render "#unknown" so the user sees
	// something readable rather than the raw <#CID> token.
	text = channelMentionRe.ReplaceAllStringFunc(text, func(match string) string {
		groups := channelMentionRe.FindStringSubmatch(match)
		channelID := groups[1]
		name := ""
		if len(groups) > 2 {
			name = groups[2]
		}
		if name == "" && channelNames != nil {
			if resolved, ok := channelNames[channelID]; ok {
				name = resolved
			}
		}
		if name == "" {
			name = "unknown"
		}
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
	// Strip skin-tone modifier suffixes from shortcodes first; toned
	// emoji render inconsistently across terminals and break alignment.
	text = emoji.Sprint(emojiutil.StripSkinToneFromText(text))

	return text
}

// plainLine pairs an ANSI-stripped line with a column→byte index. Bytes
// has length `displayWidth + 1`; for each visible column c in
// [0, displayWidth), Bytes[c] is the byte offset in Text where the
// grapheme cluster occupying column c starts. Bytes[displayWidth] is
// always len(Text) — a sentinel for clean slicing.
//
// Slicing columns [from, to) is `Text[Bytes[from]:Bytes[to]]`. This
// preserves multi-rune clusters (ZWJ sequences, skin-tone modifiers,
// variation selectors) intact so clipboard text reads as written.
//
// Wide clusters (W>1) span multiple columns whose Bytes entries point
// at the SAME starting byte offset. Slicing into the middle of a wide
// cluster simply slices to the end of the cluster's columns; the
// resulting bytes never split a cluster.
//
// Zero-width clusters (combining marks, ZWJ joiners as standalone
// clusters) are appended to Text but do not consume a column. Their
// bytes attach to whatever column comes next (or to the column before
// when they trail a base cluster, since the next Bytes[] entry already
// points past them).
type plainLine struct {
	Text  string
	Bytes []int
}

// plainLines returns column-aligned plain mirrors of each line in s.
// See `plainLine` for the shape and slicing contract.
//
// Width measurement uses uniseg.Graphemes.Width() — the same model
// (wcwidth-style cell counting) that drives the rest of the messages
// pipeline. Custom emoji whose actual terminal width disagrees with
// uniseg's reported width may be off by one column; that's acceptable
// for selection: the worst case is a one-cell drift between the
// visible highlight and the underlying text, never a crash.
func plainLines(s string) []plainLine {
	stripped := ansi.Strip(s)
	rawLines := strings.Split(stripped, "\n")
	out := make([]plainLine, len(rawLines))
	for i, line := range rawLines {
		out[i] = buildPlainLine(line)
	}
	return out
}

// buildPlainLine walks the grapheme clusters of `line` and constructs
// the (Text, Bytes) pair. Text is line itself (already ANSI-stripped);
// we only need to compute the column→byte map.
func buildPlainLine(line string) plainLine {
	if line == "" {
		return plainLine{Text: "", Bytes: []int{0}}
	}
	g := uniseg.NewGraphemes(line)
	bytesMap := make([]int, 0, len(line))
	byteOffset := 0
	for g.Next() {
		cluster := g.Str()
		w := g.Width()
		if w <= 0 {
			// Zero-width cluster: bytes go into Text but no column is
			// produced. The byte offset advances; the next W>0 cluster
			// will record its starting byte at the post-advanced offset
			// (so leading combining marks attach to the next column,
			// and trailing combining marks attach to the previous one
			// because the next Bytes[] entry already points past them).
			byteOffset += len(cluster)
			continue
		}
		// Wide cluster spans w columns, all pointing at the same byte
		// offset (the start of this cluster).
		for k := 0; k < w; k++ {
			bytesMap = append(bytesMap, byteOffset)
		}
		byteOffset += len(cluster)
	}
	// Final sentinel: byte offset just past everything in `line`.
	bytesMap = append(bytesMap, len(line))
	return plainLine{Text: line, Bytes: bytesMap}
}

// displayWidthOfPlain returns the display column count of a plainLine.
func displayWidthOfPlain(p plainLine) int {
	if len(p.Bytes) == 0 {
		return 0
	}
	return len(p.Bytes) - 1
}

// sliceColumns returns the substring covering display columns [from, to)
// of a plainLine. Out-of-range arguments are clamped. Slicing into the
// middle of a wide cluster includes the entire cluster (because the
// columns share a byte offset); this is by design — clipboard text
// reads as written.
func sliceColumns(p plainLine, from, to int) string {
	width := displayWidthOfPlain(p)
	if from < 0 {
		from = 0
	}
	if to > width {
		to = width
	}
	if from >= to {
		return ""
	}
	return p.Text[p.Bytes[from]:p.Bytes[to]]
}

// PlainLine is the exported alias of plainLine for sibling UI packages
// (e.g. thread) that maintain their own render caches and need to do
// the same column→byte plain-text mirror lookups for selection.
type PlainLine = plainLine

// PlainLines is the exported form of plainLines.
func PlainLines(s string) []PlainLine { return plainLines(s) }

// DisplayWidthOfPlain is the exported form of displayWidthOfPlain.
func DisplayWidthOfPlain(p PlainLine) int { return displayWidthOfPlain(p) }

// SliceColumns is the exported form of sliceColumns.
func SliceColumns(p PlainLine, from, to int) string { return sliceColumns(p, from, to) }
