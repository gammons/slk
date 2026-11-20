package messages

import (
	"image/color"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/bubbles/entrylist"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/styles"
)

// WrapStyles are the colours that turn a rendered message into list
// rows: the fill for the unselected row, the invisible and the tinted
// left border. Build them once per cache build (NewWrapStyles) and
// reuse them for every message, rather than allocating per message.
//
// Rows are filled and bordered by hand. The message text is already
// wrapped to the pane, so lipgloss Width().Render — which re-parses
// and re-wraps every ANSI and OSC 8 sequence — is not used here.
type WrapStyles struct {
	fillBG     color.Color
	invisGlyph string
	invisFG    color.Color
	invisBG    color.Color
	selFG      color.Color
	selBG      color.Color
	mentionFG  color.Color
	mentionBG  color.Color
	// mentionTint is the mention fill as an ANSI background sequence,
	// the form repaintBg takes.
	mentionTint string
	compact     bool
	focused     bool

	// stats, when non-nil, collects per-sub-step durations (channel
	// pane perf instrumentation).
	stats *entryPerfStats
}

// NewWrapStyles returns the styles for the current theme; focused picks
// the selection colours.
func NewWrapStyles(focused bool, compact bool) WrapStyles {
	glyph := styles.InvisibleLeftBorder().Left
	if glyph == "" {
		glyph = " "
	}
	return WrapStyles{
		fillBG:      styles.Background,
		invisGlyph:  glyph,
		invisFG:     styles.Background,
		invisBG:     styles.Background,
		selFG:       styles.SelectionBorderColor(focused),
		selBG:       styles.SelectionTintColor(focused),
		mentionFG:   styles.MentionHighlightBar(),
		mentionBG:   styles.MentionHighlightBg(),
		mentionTint: bgANSIFor(styles.MentionHighlightBg()),
		focused:     focused,
		compact:     compact,
	}
}

// SelectedVariant renders content as the selected row: the selection
// tint repainted over inner spans, filled to the row width, then the
// selection border. Split on "\n".
//
// The lipgloss style this replaces sets Background on the border style
// as well as on the fill, so the filled line is wrapped in the tint a
// second time. The bytes are what the golden frames compare.
func (s WrapStyles) SelectedVariant(content string, width int) []string {
	content = RepaintBgToSelectionTint(content, s.focused)
	inner := width - 1
	if inner < 0 {
		inner = 0
	}
	filled := fillLines(content, inner, s.selBG)
	if !isNoColor(s.selBG) && s.selBG != nil {
		for i, line := range filled {
			filled[i] = styledSpan(line, nil, s.selBG)
		}
	}
	return borderLines(filled, "▌", s.selFG, s.selBG)
}

// WrappedRows are the rows of one message, bordered and filled, ready
// for an entrylist.Entry. A pane adds what is its own: ID, Ref, hits, and
// the channel pane's trailing spacer rows.
type WrappedRows struct {
	// Lines is the unselected row, with the invisible left border.
	Lines []string
	// SelSrc is the content the selected row is built from
	// (EntryPayload.SelectedLines). It is not rendered here: doing so
	// for every message doubled the fill cost of a cache build, and
	// only the cursor row is ever drawn selected.
	SelSrc string
	// Plain mirrors the unbordered, filled content, so the border column
	// never reaches clipboard text. Pair it with Entry.ColOffset = 1.
	Plain []entrylist.PlainLine
	// ShortSrc is the content the short-timestamp selected variant is
	// built from, when that differs from SelSrc (a text selection
	// splices the short form into the row). Empty when SelSrc already
	// carries the short form.
	ShortSrc string
	// LinkHits are the clickable spans of Lines, in display columns,
	// ready to append to the entry's Hits (the renderer's own hits
	// carry image and reaction rects; links and file lines can only be
	// located after the final wrap, hence here). A span whose URL is
	// one of msg's file attachments is a HitFile; the rest HitLink.
	LinkHits []entrylist.Hit
}

// trimPlainPadding drops the leading and trailing spaces of a centred
// row's plain mirror from its Text, so copied text is the notice alone,
// while keeping one Bytes entry per display column: the padding columns
// map to the text's ends, so column-based selection and mouse mapping are
// unchanged.
func trimPlainPadding(p entrylist.PlainLine) entrylist.PlainLine {
	text := p.Text
	lead := len(text) - len(strings.TrimLeft(text, " "))
	trimmed := strings.TrimRight(text[lead:], " ")
	b := make([]int, len(p.Bytes))
	for i, off := range p.Bytes {
		off -= lead
		if off < 0 {
			off = 0
		}
		if off > len(trimmed) {
			off = len(trimmed)
		}
		b[i] = off
	}
	return entrylist.PlainLine{Text: trimmed, Bytes: b}
}

// WrapRows fills and borders a rendered message for a content pane's
// list. Only the selected variant carries the date-qualified timestamp:
// Lines and Plain keep the short form, so clipboard text and the
// mouse-to-column mapping are unchanged. The selected variant itself is
// not built here; SelSrc lets EntryPayload.SelectedLines build it for
// the cursor row.
func WrapRows(msg MessageItem, r RenderedMessage, width int, s WrapStyles) WrappedRows {
	rendered := r.Content
	renderedSel := rendered
	if !s.compact {
		renderedSel = SelectedHeader(rendered, r.Header, msg.TS, msg.Timestamp, r.HeaderBudget)
	}

	var bwT0 time.Time
	if s.stats != nil {
		bwT0 = time.Now()
	}
	bg, glyph, fg, borderBG := s.fillBG, s.invisGlyph, s.invisFG, s.invisBG
	content := rendered
	if r.MentionsSelf {
		content = repaintBg(rendered, s.mentionTint)
		bg, glyph, fg, borderBG = s.mentionBG, "▌", s.mentionFG, s.mentionBG
	}
	lines, filled := s.paint(content, width, bg, glyph, fg, borderBG)
	if s.stats != nil {
		s.stats.borderWrapTotal += time.Since(bwT0)
	}

	var plT0 time.Time
	if s.stats != nil {
		plT0 = time.Now()
	}
	plain := entrylist.PlainLines(filled)
	if core.IsSystemNoticeSubtype(msg.Subtype) {
		for i := range plain {
			plain[i] = trimPlainPadding(plain[i])
		}
	}
	if s.stats != nil {
		s.stats.plainLinesTotal += time.Since(plT0)
	}

	var shortSrc string
	if renderedSel != rendered {
		shortSrc = rendered
	}
	return WrappedRows{
		Lines:    lines,
		SelSrc:   renderedSel,
		Plain:    plain,
		ShortSrc: shortSrc,
		LinkHits: HitsFromSpans(ScanOsc8Spans(lines), msg),
	}
}

// paint fills content to width-1 and prepends a one-cell border.
// filled is the unbordered text PlainLines reads.
func (s WrapStyles) paint(content string, width int, bg color.Color, glyph string, fg, borderBG color.Color) (lines []string, filled string) {
	inner := width - 1
	if inner < 0 {
		inner = 0
	}
	raw := fillLines(content, inner, bg)
	return borderLines(raw, glyph, fg, borderBG), strings.Join(raw, "\n")
}

// styledSpan is lipgloss's border/fill styler: one SGR for the colours,
// the text, then a reset. Splitting the colours into two sequences is
// the same on screen and a different byte stream, which the golden
// frames reject.
func styledSpan(text string, fg, bg color.Color) string {
	var style ansi.Style
	if fg != nil && !isNoColor(fg) {
		style = style.ForegroundColor(fg)
	}
	if bg != nil && !isNoColor(bg) {
		style = style.BackgroundColor(bg)
	}
	if len(style) == 0 {
		return text
	}
	return style.Styled(text)
}

// fillLines pads each already-wrapped line to width cells.
//
// A line lipgloss would have styled is: the fill background, the
// line's own text, a reset, then (when short) spaces in the fill
// background and another reset. Content that already carries a
// background keeps it; the prefixed fill only shows in a gap that
// never set one, which is what Width().Render does without re-wrapping.
func fillLines(content string, width int, bg color.Color) []string {
	// lipgloss Width().Render reflows, then re-applies the open pen after
	// every newline. Message text is already wrapped, so the pen reset is
	// a straight scan (carryPen) and a line is reflowed only when it is
	// actually wider than the pane. Display width ignores the sequences
	// carryPen inserts, so it is measured once.
	orig := strings.Split(content, "\n")
	widths := make([]int, len(orig))
	overflow := false
	for i, line := range orig {
		widths[i] = ansi.StringWidth(line)
		if width > 0 && widths[i] > width {
			overflow = true
		}
	}
	if overflow && width > 0 {
		content = lipgloss.Wrap(content, width, "")
		orig = strings.Split(content, "\n")
		widths = make([]int, len(orig))
		for i, line := range orig {
			widths[i] = ansi.StringWidth(line)
		}
	} else {
		content = carryPen(content)
		orig = strings.Split(content, "\n")
	}
	for i, line := range orig {
		w := 0
		if i < len(widths) {
			w = widths[i]
		}
		orig[i] = fillLine(line, width, bg, w)
	}
	return orig
}

// carryPen is lipgloss's WrapWriter for text that does not need reflow:
// at each newline it resets the open SGR and OSC 8 link and puts them
// back on the next line. OSC payloads are copied in one slice. The
// byte-at-a-time parser is what made Width().Render slow on long
// hyperlink targets. The writer's Close reset is not part of
// lipgloss.Wrap's result (Close runs after the buffer is read), so it
// is not emitted here either.
func carryPen(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	var style uv.Style
	var link uv.Link
	i := 0
	for i < len(s) {
		if s[i] == '\n' {
			writePenBreak(&b, &style, &link)
			i++
			continue
		}
		if s[i] == 0x1b && i+1 < len(s) {
			switch s[i+1] {
			case '[':
				j := i + 2
				for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
					j++
				}
				if j >= len(s) {
					b.WriteString(s[i:])
					i = len(s)
					continue
				}
				b.WriteString(s[i : j+1])
				if s[j] == 'm' {
					uv.ReadStyle(parseSGR(s[i+2:j]), &style)
				}
				i = j + 1
				continue
			case ']':
				j := i + 2
				end := -1
				for j < len(s) {
					if s[j] == 0x07 {
						end = j + 1
						break
					}
					if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
						end = j + 2
						break
					}
					j++
				}
				if end < 0 {
					b.WriteString(s[i:])
					i = len(s)
					continue
				}
				data := s[i+2 : end-1]
				if s[end-1] == '\\' {
					data = s[i+2 : end-2]
				}
				b.WriteString(s[i:end])
				if strings.HasPrefix(data, "8;") {
					uv.ReadLink([]byte(data), &link)
				}
				i = end
				continue
			}
		}
		j := i + 1
		for j < len(s) && s[j] != '\n' && s[j] != 0x1b {
			j++
		}
		b.WriteString(s[i:j])
		i = j
	}
	// lipgloss.Wrap defers WrapWriter.Close, and the returned string is
	// captured before that defer runs, so the final pen reset is not
	// part of the result. Newline breaks above are.
	return b.String()
}

func writePenBreak(b *strings.Builder, style *uv.Style, link *uv.Link) {
	if !style.IsZero() {
		b.WriteString(ansi.ResetStyle)
	}
	if !link.IsZero() {
		b.WriteString(ansi.ResetHyperlink())
	}
	b.WriteByte('\n')
	if !link.IsZero() {
		b.WriteString(ansi.SetHyperlink(link.URL, link.Params))
	}
	if !style.IsZero() {
		b.WriteString(style.String())
	}
}

// parseSGR turns an SGR parameter string into the packed params ReadStyle
// expects. A colon marks a sub-parameter (underline style); a semicolon
// does not.
func parseSGR(params string) ansi.Params {
	if params == "" {
		return nil
	}
	var out ansi.Params
	n := 0
	seen := false
	hasMore := false
	flush := func() {
		if !seen {
			n = 0
		}
		out = append(out, ansi.Param(ansi.Parameter(n, hasMore)))
		n = 0
		seen = false
		hasMore = false
	}
	for i := 0; i < len(params); i++ {
		c := params[i]
		switch {
		case c >= '0' && c <= '9':
			seen = true
			n = n*10 + int(c-'0')
		case c == ':':
			hasMore = true
			flush()
		case c == ';':
			flush()
		}
	}
	flush()
	return out
}

func fillLine(line string, width int, bg color.Color, w int) string {
	pad := width - w
	if pad < 0 {
		pad = 0
	}
	spaces := strings.Repeat(" ", pad)
	if isNoColor(bg) || bg == nil {
		if pad == 0 {
			return line
		}
		return line + spaces
	}
	// Same shape as lipgloss Width().Render: the fill colour, the
	// line, a reset, then the shortfall in that colour.
	out := styledSpan(line, nil, bg)
	if pad > 0 {
		out += styledSpan(spaces, nil, bg)
	}
	return out
}

// borderLines prepends glyph, coloured with fg and bg, to each line.
// A NoColor is left unset, matching lipgloss's border styler.
func borderLines(lines []string, glyph string, fg, bg color.Color) []string {
	if glyph == "" {
		glyph = " "
	}
	prefix := styledSpan(glyph, fg, bg)
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = prefix + line
	}
	return out
}

func isNoColor(c color.Color) bool {
	_, ok := c.(lipgloss.NoColor)
	return ok
}
