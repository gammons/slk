package messages

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	emojiutil "github.com/gammons/slk/internal/emoji"
)

// resolveInlineEmojiSixel replaces the markers an emoji InlineSixel sink
// wrote with spaces and records a sixel placement for each image whose
// payload is already warm.
//
// A marker encodes the image index (see emoji.InlineMarkerIndex), so a
// marker split by a wrap or a literal rune in the message cannot consume
// another image's placement. Cols are relative to the message content;
// the caller adds contentColBase, as it does for every other inline
// image.
func resolveInlineEmojiSixel(content string, sink *emojiutil.InlineSixel, dst []SixelImage) (string, []SixelImage) {
	images := sink.Images()
	if len(images) == 0 {
		return content, dst
	}
	cells := sink.Cells()
	lead := emojiutil.InlineMarkerLead()
	lines := strings.Split(content, "\n")
	for row := 0; row < len(lines); row++ {
		rs := []rune(lines[row])
		if !hasInlineMarker(rs, cells) {
			continue
		}
		var b strings.Builder
		for i := 0; i < len(rs); {
			if cells == 2 && rs[i] == lead {
				if i+1 < len(rs) {
					if idx, ok := emojiutil.InlineMarkerIndex(rs[i+1]); ok {
						addInlineSixel(row, idx, ansi.StringWidth(b.String()), cells, images, &dst)
						b.WriteString(strings.Repeat(" ", cells))
						i += 2
						continue
					}
				}
				// A marker whose two runes a wrap put on different rows:
				// the image cannot be drawn as one row, so drop it but
				// keep its two cells blank. The next row may start with
				// blockquote borders, padding or SGR resets, so look for
				// the first content rune rather than rune zero.
				if !hasContentRune(rs[i+1:]) && row+1 < len(lines) {
					nrs := []rune(lines[row+1])
					if pos, r := firstContentRune(nrs); pos >= 0 {
						if _, ok := emojiutil.InlineMarkerIndex(r); ok {
							b.WriteString(" ")
							nrs[pos] = ' '
							lines[row+1] = string(nrs)
							i++
							continue
						}
					}
				}
				// A literal lead rune: keep the caller's text.
				b.WriteRune(rs[i])
				i++
				continue
			}
			if cells == 1 {
				if idx, ok := emojiutil.InlineMarkerIndex(rs[i]); ok {
					addInlineSixel(row, idx, ansi.StringWidth(b.String()), 1, images, &dst)
					b.WriteString(" ")
					i++
					continue
				}
			}
			// A literal rune, or an index rune without its lead.
			b.WriteRune(rs[i])
			i++
		}
		lines[row] = b.String()
	}
	return strings.Join(lines, "\n"), dst
}

// hasContentRune reports whether rs has a visible rune other than the
// spaces and blockquote decoration a rendered row may begin or end
// with. ANSI sequences are skipped.
func hasContentRune(rs []rune) bool {
	pos, _ := firstContentRune(rs)
	return pos >= 0
}

// firstContentRune returns the index and value of the first visible,
// non-decoration rune in rs, skipping ANSI sequences and the borders
// and padding blockquoteStyle adds to each row.
func firstContentRune(rs []rune) (int, rune) {
	for i := 0; i < len(rs); {
		switch {
		case rs[i] == '\x1b':
			i = skipEscape(rs, i)
		case isDecorationRune(rs[i]):
			i++
		default:
			return i, rs[i]
		}
	}
	return -1, 0
}

// isDecorationRune reports whether r is one of the cells a rendered
// row can carry before its content: spaces and blockquote borders.
func isDecorationRune(r rune) bool {
	switch r {
	case ' ', '\t', '│', '┃', '▌', '|', '>':
		return true
	}
	return false
}

// skipEscape returns the index just past the ANSI escape sequence
// starting at rs[i]. Unterminated sequences consume the rest.
func skipEscape(rs []rune, i int) int {
	if i+1 >= len(rs) {
		return len(rs)
	}
	switch rs[i+1] {
	case '[':
		for j := i + 2; j < len(rs); j++ {
			if rs[j] >= 0x40 && rs[j] <= 0x7e {
				return j + 1
			}
		}
		return len(rs)
	case ']':
		for j := i + 2; j < len(rs); j++ {
			if rs[j] == '\a' {
				return j + 1
			}
			if rs[j] == '\x1b' && j+1 < len(rs) && rs[j+1] == '\\' {
				return j + 2
			}
		}
		return len(rs)
	default:
		return i + 2
	}
}

// hasInlineMarker reports whether the line can contain a marker.
func hasInlineMarker(rs []rune, cells int) bool {
	for _, r := range rs {
		if cells == 1 {
			if _, ok := emojiutil.InlineMarkerIndex(r); ok {
				return true
			}
			continue
		}
		if r == emojiutil.InlineMarkerLead() {
			return true
		}
	}
	return false
}

// addInlineSixel records the placement of the image at idx, when its
// payload is warm and the index is one the sink emitted.
func addInlineSixel(row, idx, col, cells int, images []emojiutil.InlineImage, dst *[]SixelImage) {
	if idx < 0 || idx >= len(images) || len(images[idx].Bytes) == 0 {
		return
	}
	img := images[idx]
	*dst = append(*dst, SixelImage{
		row: row, col: col,
		key: img.Key, bytes: img.Bytes, fallback: img.Fallback,
		height: 1, width: cells,
	})
}
