package messages

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gammons/slk/internal/text"
	"github.com/gammons/slk/internal/ui/styles"
)

// SearchHighlightSGR derives the raw open/close SGR sequences for
// SearchHighlightStyle by rendering a sentinel and splitting on it —
// works for any lipgloss color profile without hand-building escapes.
//
// The close sequence is a bare reset (\x1b[m), which in plain body text
// would leave the terminal's default bg/fg showing until the next escape
// or EOL. The rendered body has already been through ReapplyBgAfterResets
// (inside RenderSlackMarkdownWith), so instead of re-scanning the whole
// string — which would double-inject after every pre-existing reset — we
// append the theme bg+fg restore (the same style argument render.go uses)
// to the close sequence itself, targeting exactly the resets the
// highlighter introduces.
func SearchHighlightSGR() (start, end string, ok bool) {
	parts := strings.SplitN(styles.SearchHighlightStyle().Render("\x00"), "\x00", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", "", false
	}
	return parts[0], parts[1] + BgANSI() + FgANSI(), true
}

// HighlightSearchTerms wraps case- and accent-insensitive word-prefix
// occurrences of terms in s with hlStart/hlEnd. s may contain ANSI
// escape sequences (CSI/SGR, OSC hyperlinks, other escapes): they are
// skipped during matching, preserved byte-identical in the output, and
// any CSI SGR sequences active at a match start are re-emitted after
// hlEnd so the highlight does not clobber surrounding styling.
//
// terms must already be folded (text.Fold). Matching is per-rune
// folded comparison, which keeps a 1:1 position mapping for the
// diacritics Fold removes. Matches spanning a styled-segment boundary
// are not highlighted at all — the whole term must fall within one
// visible segment. Acceptable for v1.
func HighlightSearchTerms(s string, terms []string, hlStart, hlEnd string) string {
	if len(terms) == 0 || s == "" {
		return s
	}

	type seg struct {
		text   string // visible run or escape sequence
		isANSI bool
		opaque bool // ANSI but not CSI: never reset/re-applied (OSC, 2-byte escapes)
	}
	// Segment s into visible-text runs and ANSI escapes. Every branch
	// below advances i by at least one byte, so zero-length segments
	// (and the infinite loop they caused on non-CSI escapes) are
	// structurally impossible.
	var segs []seg
	for i := 0; i < len(s); {
		if s[i] != 0x1b {
			j := strings.IndexByte(s[i:], 0x1b)
			if j < 0 {
				j = len(s) - i
			}
			segs = append(segs, seg{text: s[i : i+j]})
			i += j
			continue
		}
		if i+1 >= len(s) {
			// Bare trailing ESC: opaque 1-byte segment.
			segs = append(segs, seg{text: s[i:], isANSI: true, opaque: true})
			break
		}
		switch s[i+1] {
		case '[': // CSI: parameter bytes through final byte in 0x40-0x7e
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				j++ // include final byte (truncated CSI: take what's there)
			}
			segs = append(segs, seg{text: s[i:j], isANSI: true})
			i = j
		case ']': // OSC: terminated by BEL or ST (\x1b\), terminator included
			j := i + 2
			end := len(s) // unterminated OSC consumes the rest of the string
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
			// Opaque: the payload (e.g. an OSC-8 URL) must never be
			// matched or highlighted — corrupting it breaks the
			// hyperlink — and must not enter the SGR re-apply list.
			segs = append(segs, seg{text: s[i:end], isANSI: true, opaque: true})
			i = end
		default:
			// Any other escape: consume ESC plus the next byte as an
			// opaque 2-byte segment (e.g. \x1b(B charset designation).
			segs = append(segs, seg{text: s[i : i+2], isANSI: true, opaque: true})
			i += 2
		}
	}

	var out strings.Builder
	var active []string // SGR sequences since last reset, for re-apply
	prevRune := rune(0) // last visible rune across segments (word boundary)
	for _, sg := range segs {
		if sg.isANSI {
			// prevRune deliberately persists across all ANSI segments
			// (including OSC) so escapes don't fake word boundaries.
			out.WriteString(sg.text)
			if sg.opaque {
				continue
			}
			if sg.text == "\x1b[0m" || sg.text == "\x1b[m" {
				active = active[:0]
			} else {
				active = append(active, sg.text)
			}
			continue
		}
		runes := []rune(sg.text)
		folded := make([]string, len(runes))
		if len(runes) == len(sg.text) && utf8.ValidString(sg.text) {
			// ASCII is one byte per rune, so folding the segment once keeps
			// each byte aligned with the corresponding original rune.
			foldedText := text.Fold(sg.text)
			for i := range folded {
				folded[i] = foldedText[i : i+1]
			}
		} else {
			// Preserve one entry per original rune when folding can change
			// byte or rune counts, as with decomposed combining marks.
			for i, r := range runes {
				folded[i] = text.Fold(string(r))
			}
		}
		for i := 0; i < len(runes); {
			atWordStart := !unicode.IsLetter(prevRune) && !unicode.IsDigit(prevRune)
			matched := 0
			if atWordStart {
				for _, term := range terms {
					if n := prefixMatchLen(folded, i, term); n > 0 {
						matched = n
						break
					}
				}
			}
			if matched > 0 {
				out.WriteString(hlStart)
				out.WriteString(string(runes[i : i+matched]))
				out.WriteString(hlEnd)
				for _, a := range active {
					out.WriteString(a)
				}
				prevRune = runes[i+matched-1]
				i += matched
				continue
			}
			out.WriteRune(runes[i])
			prevRune = runes[i]
			i++
		}
	}
	return out.String()
}

// prefixMatchLen reports how many runes starting at folded[i] are
// consumed matching term, or 0 if term is not a prefix there.
func prefixMatchLen(folded []string, i int, term string) int {
	rest := term
	n := 0
	for i+n < len(folded) && rest != "" {
		f := folded[i+n]
		if !strings.HasPrefix(rest, f) {
			return 0
		}
		rest = rest[len(f):]
		n++
	}
	if rest != "" {
		return 0
	}
	return n
}
