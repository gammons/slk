package messages

import (
	"fmt"
	"image/color"
	"io"
	"regexp"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	emojiutil "github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/rivo/uniseg"
)

var (
	// Slack formatting patterns. italic is NOT a regex because Go's
	// RE2 doesn't support lookahead/lookbehind, and the correct
	// "intra-word `_` is literal" rule needs to know what's on either
	// side of the delimiter. See renderItalics below.
	boldRe          = regexp.MustCompile(`\*([^*\n]+)\*`)
	strikethroughRe = regexp.MustCompile(`~([^~\n]+)~`)
	inlineCodeRe    = regexp.MustCompile("`([^`\n]+)`")
	codeBlockRe     = regexp.MustCompile("(?s)```(.+?)```")

	// Slack link patterns: <url|label> or <url>.
	// linkWithLabelRe matches both http(s) URLs and mailto: addresses
	// (Slack auto-linkifies typed emails into <mailto:X|X> form). The
	// scheme restriction means we do NOT match channel mentions
	// <#CHANNEL_ID|name>, group mentions <!subteam^...|@team>, or
	// other Slack-internal angle-bracket forms — those are handled by
	// dedicated regexes below.
	linkWithLabelRe = regexp.MustCompile(`<((?:https?://|mailto:)[^|>]+)\|([^>]+)>`)
	linkBareRe      = regexp.MustCompile(`<((?:https?://|mailto:)[^>]+)>`)

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

	// Slack date tokens: <!date^TIMESTAMP^FORMAT|FALLBACK> (an optional
	// ^LINK segment may precede the |). Common in bot/unfurl content
	// (Linear, Jira, GitHub). We render the precomputed FALLBACK text
	// (already localized by Slack) rather than re-deriving the date from
	// the format string. Group 1 is the fallback.
	dateTokenRe = regexp.MustCompile(`<!date\^[^|>]*\|([^>]*)>`)

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
// The filename is the link label; the raw URL is not shown (it's in
// the OSC 8 escape). Attachments with no name fall back to the URL.
//
// Output format per attachment:
//
//	[Image] photo.png
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

// renderSingleAttachment formats one attachment as the single-line
// "[Image] <name>" / "[File] <name>" form, with the name wrapped in an
// OSC 8 hyperlink to the URL. Falls back to the URL as label when the
// attachment has no name. The messages-pane image-rendering pipeline
// uses this when no inline renderer is available (ProtoOff, missing
// thumbs) and the thread pane uses it via RenderAttachments for all
// attachments.
func renderSingleAttachment(a Attachment) string {
	markerStyle := lipgloss.NewStyle().Foreground(styles.TextMuted).Bold(true)
	urlStyle := linkStyle()
	marker := "[File]"
	if a.Kind == "image" {
		marker = "[Image]"
	}
	label := a.Name
	if label == "" {
		label = a.URL
	}
	body := markerStyle.Render(marker) + " " + urlStyle.Render(label)
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
	cachedBgANSI              string
	cachedBgColor             color.Color
	cachedSidebarBgANSI       string
	cachedSidebarBgColor      color.Color
	cachedFgANSI              string
	cachedFgColor             color.Color
	cachedSidebarFgANSI       string
	cachedSidebarFgColor      color.Color
	cachedSidebarMutedFgANSI  string
	cachedSidebarMutedFgColor color.Color

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
// bold + fg + bg), so the substitution must reach the bg parameter
// even when it's bundled with other attributes. We delegate to
// substituteBgSGR, which is grammar-aware: it walks each SGR sequence
// and substitutes only complete bg tokens. Grammar awareness also
// makes ANSI 16 bg params like "40" safe to substitute — a naive
// substring replacement would collide with literal digits in content
// and with 256-color sub-arguments such as "38;5;40".
func RepaintBgToSelectionTint(s string, focused bool) string {
	from := bgSGRParams(BgANSI())
	to := bgSGRParams(SelectionTintBgANSI(focused))
	if from == "" || from == to {
		return s
	}
	return substituteBgSGR(s, from, to)
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

// substituteBgSGR walks s, finds every SGR sequence (\x1b[...m), tokenizes
// its parameter list with awareness of extended-color groups (38;5;N,
// 38;2;R;G;B, 48;5;N, 48;2;R;G;B), and substitutes any bg-token whose
// stringified form equals from with to. Non-SGR text and SGR sequences
// that don't contain a matching bg token are passed through unchanged.
//
// Grammar awareness matters because a bg token can be a single param
// ("40"–"47", "100"–"107") that may otherwise collide with arbitrary
// digit substrings — both inside non-SGR content and as arguments to
// 256-color FG codes like "38;5;40". Splitting on ";" without grammar
// knowledge would corrupt those.
//
// If from == to the input is returned unchanged.
func substituteBgSGR(s, from, to string) string {
	if from == "" || from == to {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		start := strings.Index(s[i:], "\x1b[")
		if start < 0 {
			b.WriteString(s[i:])
			break
		}
		// Copy plain text before the escape verbatim.
		b.WriteString(s[i : i+start])
		seqStart := i + start
		// Find the terminating 'm'. SGR parameters are digits and ';';
		// anything else (e.g. another '\x1b') means this isn't a well-formed
		// SGR sequence and we bail back to plain copy.
		j := seqStart + 2
		for j < len(s) && s[j] != 'm' {
			c := s[j]
			if c != ';' && (c < '0' || c > '9') {
				break
			}
			j++
		}
		if j >= len(s) || s[j] != 'm' {
			// Not an SGR; copy "\x1b[" and continue scanning past it.
			b.WriteString("\x1b[")
			i = seqStart + 2
			continue
		}
		// s[seqStart+2:j] is the param list, s[j] == 'm'.
		params := splitSGRParams(s[seqStart+2 : j])
		out := make([]string, 0, len(params))
		for _, p := range params {
			if p.isBg && p.text == from {
				out = append(out, to)
			} else {
				out = append(out, p.text)
			}
		}
		b.WriteString("\x1b[")
		b.WriteString(strings.Join(out, ";"))
		b.WriteString("m")
		i = j + 1
	}
	return b.String()
}

// sgrParam is one logical SGR parameter. For extended-color groups
// (38;5;N, 38;2;R;G;B, 48;5;N, 48;2;R;G;B) text contains the joined
// form (e.g. "48;5;40" or "48;2;26;26;46") and isBg is true for any
// bg variant. For single parameters text is just the number (e.g.
// "40", "1", "31") and isBg is true only when the number is in the
// basic bg range (40–47, 100–107).
type sgrParam struct {
	text string
	isBg bool
}

// splitSGRParams tokenizes an SGR parameter list with awareness of
// extended-color sub-sequences so a "40" appearing as an argument to
// "38;5" is not mistaken for a standalone bg-black token.
func splitSGRParams(params string) []sgrParam {
	if params == "" {
		return nil
	}
	parts := strings.Split(params, ";")
	out := make([]sgrParam, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		switch p {
		case "38", "48":
			isBg := p == "48"
			// Look ahead for "5;N" or "2;R;G;B".
			if i+1 < len(parts) {
				switch parts[i+1] {
				case "5":
					if i+2 < len(parts) {
						out = append(out, sgrParam{
							text: strings.Join(parts[i:i+3], ";"),
							isBg: isBg,
						})
						i += 2
						continue
					}
				case "2":
					if i+4 < len(parts) {
						out = append(out, sgrParam{
							text: strings.Join(parts[i:i+5], ";"),
							isBg: isBg,
						})
						i += 4
						continue
					}
				}
			}
			// Malformed — treat as a bare param so we don't drop data.
			out = append(out, sgrParam{text: p, isBg: false})
		default:
			out = append(out, sgrParam{
				text: p,
				isBg: isBasicBgParam(p),
			})
		}
	}
	return out
}

// isBasicBgParam reports whether s is a single SGR parameter representing
// a basic 16-color background: "40"–"47" or "100"–"107".
func isBasicBgParam(s string) bool {
	switch s {
	case "40", "41", "42", "43", "44", "45", "46", "47",
		"100", "101", "102", "103", "104", "105", "106", "107":
		return true
	}
	return false
}

// bgANSIFor returns the ANSI background-color escape for c.
// For ansi.BasicColor it emits the native 16-color SGR (\x1b[40m–\x1b[47m,
// \x1b[100m–\x1b[107m) so the user's terminal palette is honored.
// For ansi.IndexedColor it emits the 256-color form (\x1b[48;5;Nm).
// Otherwise it falls back to truecolor (\x1b[48;2;R;G;Bm).
// A nil color (transparent theme value) emits the default-background
// reset (\x1b[49m) so the terminal's own background shows through.
func bgANSIFor(c color.Color) string {
	if c == nil {
		return "\x1b[49m"
	}
	switch v := c.(type) {
	case ansi.BasicColor:
		if v < 8 {
			return fmt.Sprintf("\x1b[%dm", 40+int(v))
		}
		if v < 16 {
			return fmt.Sprintf("\x1b[%dm", 100+int(v-8))
		}
		// out-of-range BasicColor: fall through to RGBA
	case ansi.IndexedColor:
		return fmt.Sprintf("\x1b[48;5;%dm", int(v))
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)
}

// fgANSIFor returns the ANSI foreground-color escape for c.
// See bgANSIFor for the type-switch rationale. A nil color emits the
// default-foreground reset (\x1b[39m).
func fgANSIFor(c color.Color) string {
	if c == nil {
		return "\x1b[39m"
	}
	switch v := c.(type) {
	case ansi.BasicColor:
		if v < 8 {
			return fmt.Sprintf("\x1b[%dm", 30+int(v))
		}
		if v < 16 {
			return fmt.Sprintf("\x1b[%dm", 90+int(v-8))
		}
	case ansi.IndexedColor:
		return fmt.Sprintf("\x1b[38;5;%dm", int(v))
	}
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

// SidebarMutedFgANSI is the muted sidebar foreground escape — the
// counterpart of SidebarFgANSI for rows styled as ChannelNormal or
// ChannelMuted. Callers in the sidebar pass this to
// ReapplyBgAfterResets so the dimmer foreground survives an inline
// ANSI reset emitted by a styled prefix glyph (DM presence, group_dm,
// etc.). Without it, the post-reset span falls back to the bright
// SidebarFgANSI and read rows render visibly brighter than they
// should.
func SidebarMutedFgANSI() string {
	fg := styles.SidebarTextMuted
	if fg == cachedSidebarMutedFgColor && cachedSidebarMutedFgANSI != "" {
		return cachedSidebarMutedFgANSI
	}
	cachedSidebarMutedFgANSI = fgANSIFor(fg)
	cachedSidebarMutedFgColor = fg
	return cachedSidebarMutedFgANSI
}

// RenderSlackMarkdownOpts extends the legacy 3-arg RenderSlackMarkdown
// signature with optional emoji-image rendering. When PlaceCtx.Fetcher
// is non-nil AND emoji.ImageModeActive() returns true, emoji are
// rendered as kitty image placements via emoji.Place; otherwise the
// legacy glyph/shortcode-text path runs (byte-identical to the
// 3-arg form).
//
// EmojiFlushes accumulates kitty image upload callbacks for the warm
// path. nil disables flush collection (cold-only callers, or callers
// that don't care about flushes — e.g., tests).
type RenderSlackMarkdownOpts struct {
	UserNames    map[string]string
	ChannelNames map[string]string
	UserGroups   map[string]string

	// Emoji-image opts (zero values disable the image path).
	PlaceCtx     emojiutil.PlaceContext
	EmojiCells   int                      // 0 falls back to 2
	Customs      map[string]string        // workspace custom emoji map; may be nil
	EmojiFlushes *[]func(io.Writer) error // append-only; may be nil
}

// RenderSlackMarkdown converts Slack-flavored markdown and emoji shortcodes
// into lipgloss-styled terminal output. If userNames is provided, user mentions
// like <@U1234> are resolved to display names. If channelNames is provided,
// bare <#C1234> channel mentions (without an embedded name) are resolved
// to #channel-name; mentions that already carry the embedded |name form
// don't need the map.
//
// RenderSlackMarkdown is the legacy 3-arg entry point preserved for all
// callers that don't need emoji-image rendering (tests, threads view
// preview, etc.). New code should use RenderSlackMarkdownWith to get the
// image-path branch.
func RenderSlackMarkdown(text string, userNames map[string]string, channelNames map[string]string) string {
	return RenderSlackMarkdownWith(text, RenderSlackMarkdownOpts{
		UserNames:    userNames,
		ChannelNames: channelNames,
	})
}

// RenderSlackMarkdownWith is the full-featured entry point. See
// RenderSlackMarkdownOpts for the per-call configuration. With a zero
// opts struct it is byte-identical to RenderSlackMarkdown.
func RenderSlackMarkdownWith(text string, opts RenderSlackMarkdownOpts) string {
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
			line = renderInlineFormattingWith(line, opts)
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

func renderInlineFormattingWith(text string, opts RenderSlackMarkdownOpts) string {
	userNames := opts.UserNames
	channelNames := opts.ChannelNames
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

	// Italic — manual scan so intra-word underscores (e.g.,
	// `is_unpaid_yes`, `hello_world_foo`) stay literal, per
	// CommonMark's intraword-underscore rule. The previous regex
	// `_X_` matched any pair of underscores, italicizing identifiers
	// that contain a `_` and stripping the underscores from the
	// visible output.
	text = renderItalics(text)

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
	// For mailto: URLs the visible text drops the scheme prefix so the
	// user sees just the email address; the OSC 8 target keeps the
	// mailto: scheme so terminal click-handlers can open a mail client.
	text = linkBareRe.ReplaceAllStringFunc(text, func(match string) string {
		url := linkBareRe.FindStringSubmatch(match)[1]
		visible := strings.TrimPrefix(url, "mailto:")
		return osc8Hyperlink(url, linkStyle().Render(visible))
	})

	// Date tokens: <!date^TS^FORMAT|FALLBACK> -> FALLBACK. Done before
	// channel/user mentions; the token never contains <#/<@ forms.
	text = dateTokenRe.ReplaceAllStringFunc(text, func(match string) string {
		return dateTokenRe.FindStringSubmatch(match)[1]
	})

	// Usergroup mentions: <!subteam^SID|@label> -> @label, or bare
	// <!subteam^SID> -> @handle via the workspace-scoped usergroup map
	// (fallback "@group"). Shares usergroupMentionRe / usergroupDisplay
	// with FlattenMrkdwn (flatten.go).
	text = usergroupMentionRe.ReplaceAllStringFunc(text, func(match string) string {
		groups := usergroupMentionRe.FindStringSubmatch(match)
		return mentionStyle().Render(usergroupDisplay(opts.UserGroups, groups[1], groups[2]))
	})

	// Broadcast mentions: <!here> / <!channel> / <!everyone> (labels,
	// when present, duplicate the keyword and are dropped) -> styled
	// @here / @channel / @everyone. Shares specialMentionRe with
	// FlattenMrkdwn (flatten.go). Runs after the date and subteam
	// passes so their `<!...>` forms are already consumed.
	text = specialMentionRe.ReplaceAllStringFunc(text, func(match string) string {
		return mentionStyle().Render("@" + specialMentionRe.FindStringSubmatch(match)[1])
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

	// Emoji resolution.
	//
	// Image path (kitty + emoji_images=on): tokenize the text and
	// render emoji as kitty image placements via emoji.Place. The
	// width math (set up by Phase 4) already reports the configured
	// cell footprint for every image-renderable cluster, so layout
	// is deterministic regardless of font.
	//
	// Legacy path: the glyph/shortcode-text substitution that
	// retained ":name:" for multi-codepoint sequences. See
	// internal/emoji/shouldrender.go. StripSkinToneFromText runs
	// first because skin-toned shortcodes (e.g. :wave_tone3:)
	// should resolve as their base name (:wave:) rather than be
	// left as literal text.
	//
	// Image path: preserve skin-tone suffixes — the URL builder
	// routes them to the correct per-tone Slack CDN asset. The
	// old StripSkinToneFromText call was a glyph-rendering width
	// workaround that doesn't apply to kitty-image placements.
	if emojiutil.ImageModeActive() && opts.PlaceCtx.Fetcher != nil {
		tokens := emojiutil.ResolveEmojiToTokens(text, opts.Customs)
		text = renderEmojiTokensInline(tokens, opts.PlaceCtx, opts.EmojiCells, opts.EmojiFlushes)
	} else {
		text = emojiutil.ResolveShortcodesInText(emojiutil.StripSkinToneFromText(text))
	}

	return text
}

// renderEmojiTokensInline walks a Token stream and returns the
// rendered inline string. Emoji tokens are placed via emoji.Place
// when the image path is active (emoji.ImageModeActive() AND
// placeCtx.Fetcher != nil); otherwise they render as their plain-text
// representation (the source-form text already captured on the
// Token).
//
// Kitty image upload callbacks collected on the warm path are
// appended to *flushes when non-nil. flushes left nil disables
// collection (caller doesn't care; cold-path callers).
func renderEmojiTokensInline(
	tokens []emojiutil.Token,
	placeCtx emojiutil.PlaceContext,
	cells int,
	flushes *[]func(io.Writer) error,
) string {
	if cells <= 0 {
		cells = 2
	}
	imageOK := emojiutil.ImageModeActive() && placeCtx.Fetcher != nil

	var b strings.Builder
	for _, tok := range tokens {
		switch tok.Kind {
		case emojiutil.TokenText:
			b.WriteString(tok.Text)
		case emojiutil.TokenEmoji:
			if imageOK && tok.URL != "" {
				placement, flush, ok := emojiutil.Place(placeCtx, tok.URL, cells)
				if ok {
					b.WriteString(placement)
					if flush != nil && flushes != nil {
						*flushes = append(*flushes, flush)
					}
					continue
				}
			}
			// Fallback: plain-text form (":name:" for unresolved
			// shortcodes / image-mode off, or the source-form glyph
			// for raw-codepoint emoji that bypassed Place).
			b.WriteString(tok.Text)
		}
	}
	return b.String()
}

// SlackMrkdwnToCommonMark converts Slack-flavored mrkdwn into CommonMark
// markdown. It is the plain-text sibling of RenderSlackMarkdown: same input
// format, but the output is CommonMark rather than lipgloss-styled ANSI.
func SlackMrkdwnToCommonMark(text string, userNames map[string]string, channelNames map[string]string) string {
	return SlackMrkdwnToCommonMarkWithUserGroups(text, userNames, channelNames, nil)
}

// SlackMrkdwnToCommonMarkWithUserGroups is SlackMrkdwnToCommonMark with
// a workspace-scoped Slack usergroup map for resolving bare subteam IDs.
func SlackMrkdwnToCommonMarkWithUserGroups(text string, userNames map[string]string, channelNames map[string]string, userGroups map[string]string) string {
	// Protect code blocks: extract, convert, and replace with placeholders.
	var codeBlocks []string
	text = codeBlockRe.ReplaceAllStringFunc(text, func(match string) string {
		inner := codeBlockRe.FindStringSubmatch(match)[1]
		inner = strings.TrimSpace(inner)
		placeholder := fmt.Sprintf("\x00CB%d\x00", len(codeBlocks))
		codeBlocks = append(codeBlocks, "```\n"+inner+"\n```")
		return placeholder
	})

	// Protect inline code.
	var inlineCodes []string
	text = inlineCodeRe.ReplaceAllStringFunc(text, func(match string) string {
		inner := inlineCodeRe.FindStringSubmatch(match)[1]
		placeholder := fmt.Sprintf("\x00IC%d\x00", len(inlineCodes))
		inlineCodes = append(inlineCodes, "`"+inner+"`")
		return placeholder
	})

	lines := strings.Split(text, "\n")
	var result []string
	for _, line := range lines {
		if strings.HasPrefix(line, "&gt; ") || strings.HasPrefix(line, "> ") {
			quoted := strings.TrimPrefix(line, "&gt; ")
			quoted = strings.TrimPrefix(quoted, "> ")
			quoted = slackEntityDecoder.Replace(quoted)
			line = "> " + quoted
		} else {
			line = slackMrkdwnToCommonMarkInline(line, userNames, channelNames, userGroups)
			line = slackEntityDecoder.Replace(line)
		}
		result = append(result, line)
	}

	output := strings.Join(result, "\n")

	// Restore inline code placeholders.
	for i, code := range inlineCodes {
		output = strings.Replace(output, fmt.Sprintf("\x00IC%d\x00", i), code, 1)
	}
	// Restore code block placeholders.
	for i, block := range codeBlocks {
		output = strings.Replace(output, fmt.Sprintf("\x00CB%d\x00", i), block, 1)
	}

	return output
}

// slackMrkdwnToCommonMarkInline converts inline Slack formatting tokens
// to their CommonMark equivalents without any ANSI styling.
func slackMrkdwnToCommonMarkInline(text string, userNames map[string]string, channelNames map[string]string, userGroups map[string]string) string {
	text = boldRe.ReplaceAllString(text, "**$1**")

	text = strikethroughRe.ReplaceAllString(text, "~~$1~~")

	text = linkWithLabelRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := linkWithLabelRe.FindStringSubmatch(match)
		url, label := parts[1], parts[2]
		return "[" + label + "](" + url + ")"
	})

	text = linkBareRe.ReplaceAllStringFunc(text, func(match string) string {
		url := linkBareRe.FindStringSubmatch(match)[1]
		return strings.TrimPrefix(url, "mailto:")
	})

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
		return "#" + name
	})

	text = userMentionRe.ReplaceAllStringFunc(text, func(match string) string {
		userID := userMentionRe.FindStringSubmatch(match)[1]
		name := userID
		if userNames != nil {
			if resolved, ok := userNames[userID]; ok {
				name = resolved
			}
		}
		return "@" + name
	})

	text = usergroupMentionRe.ReplaceAllStringFunc(text, func(match string) string {
		groups := usergroupMentionRe.FindStringSubmatch(match)
		return usergroupDisplay(userGroups, groups[1], groups[2])
	})

	text = specialMentionRe.ReplaceAllStringFunc(text, func(match string) string {
		return "@" + specialMentionRe.FindStringSubmatch(match)[1]
	})

	text = resolveShortcodesCommonMark(emojiutil.StripSkinToneFromText(text))

	return text
}

var commonMarkShortcodeRe = regexp.MustCompile(`:[A-Za-z0-9_+\-]+:`)

func resolveShortcodesCommonMark(s string) string {
	return commonMarkShortcodeRe.ReplaceAllStringFunc(s, func(match string) string {
		resolved := emojiutil.Sprint(match)
		if resolved == match {
			return match
		}
		return strings.TrimRight(resolved, " ")
	})
}

// renderItalics wraps `_X_` runs in italicStyle when the surrounding
// `_` characters sit at word boundaries — start/end of text, or
// adjacent to a non-word rune (whitespace, punctuation, ANSI escape
// bytes, …). Intra-word `_` (alphanumeric on BOTH sides) is left
// literal, matching CommonMark's underscore-emphasis rule and the
// behavior of Slack's own web/mobile clients.
//
// We scan rune-by-rune so multi-byte UTF-8 (e.g. accented letters as
// word chars) is handled correctly. The body of an italic run may
// contain any rune except `_` and `\n`, matching the legacy regex.
func renderItalics(text string) string {
	if !strings.ContainsRune(text, '_') {
		return text
	}
	runes := []rune(text)
	var b strings.Builder
	b.Grow(len(text))
	i := 0
	for i < len(runes) {
		if runes[i] != '_' {
			b.WriteRune(runes[i])
			i++
			continue
		}
		// Candidate opener at position i. The `_` opens emphasis only
		// when the preceding rune is start-of-text or a non-word rune.
		if i > 0 && isItalicWordRune(runes[i-1]) {
			b.WriteRune(runes[i])
			i++
			continue
		}
		// Find a candidate closing `_` within the same logical line.
		j := i + 1
		for j < len(runes) && runes[j] != '_' && runes[j] != '\n' {
			j++
		}
		if j >= len(runes) || runes[j] != '_' || j == i+1 {
			// No close, or empty body (`__`): not italic, emit `_` literally.
			b.WriteRune(runes[i])
			i++
			continue
		}
		// The closing `_` only counts when the FOLLOWING rune is
		// end-of-text or a non-word rune.
		if j+1 < len(runes) && isItalicWordRune(runes[j+1]) {
			b.WriteRune(runes[i])
			i++
			continue
		}
		inner := string(runes[i+1 : j])
		b.WriteString(italicStyle().Render(inner))
		i = j + 1
	}
	return b.String()
}

// isItalicWordRune reports whether r counts as a "word" rune for
// CommonMark's intra-word underscore rule: letters, digits, and `_`
// itself. Anything else (whitespace, punctuation, ANSI control bytes,
// emoji glyphs) acts as a boundary.
func isItalicWordRune(r rune) bool {
	if r == '_' {
		return true
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r)
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
	// Use the grapheme-only iterator (FirstGraphemeClusterInString)
	// instead of uniseg.NewGraphemes/.Next(): the latter runs uniseg's
	// full StepString state machine, which also computes word/sentence/
	// line-break boundaries we never use. That sentence-break work was
	// ~67% of a full buildCache (the ~500ms thread-open / channel-switch
	// rebuild). The grapheme cluster boundaries and widths produced are
	// identical (verified by TestBuildPlainLine_MatchesUnisegGraphemes).
	bytesMap := make([]int, 0, len(line))
	byteOffset := 0
	state := -1
	rest := line
	for len(rest) > 0 {
		var cluster string
		var w int
		cluster, rest, w, state = uniseg.FirstGraphemeClusterInString(rest, state)
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
