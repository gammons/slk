package messages

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/bubbles/entrylist"
)

// osc8Open is the opening marker of an OSC 8 hyperlink, carrying the
// URL as its payload up to the ST terminator (ESC \). RenderSlackMarkdown
// and the attachment line renderer both emit these around every link
// label, so the terminal itself can offer them (Shift+Click where the
// app holds the mouse) and this scanner can find their columns.
const osc8Open = "\x1b]8;;"

// Osc8Span is one OSC 8 hyperlink that opens and closes on the same
// rendered row, in that row's display columns - the same frame
// HitTest takes (column 0 is the row's first display column, border
// and avatar gutter included, escape sequences zero-width).
type Osc8Span struct {
	Row              int
	ColStart, ColEnd int // [ColStart, ColEnd)
	URL              string
	// Text is the span's label with escape sequences stripped. The
	// attachment-line classifier reads it: a [File] line's label
	// begins with its marker, a body link to the same URL ("view this
	// file") does not.
	Text string
}

// ScanOsc8Spans returns the OSC 8 hyperlink spans of lines. A span that
// wraps - opens on one row, closes on a later one - yields nothing:
// the continuation row's content starts after the border and avatar
// gutter the wrap inserted, so a rect covering it would make padding
// and gutter clickable. A wrapped label stays reachable through `o`
// and the terminal's own Shift+Click. Text between markers is measured
// with ansi.StringWidth, so any other escapes inside the label
// (colour, underline) cost no columns and wide runes cost two. Nested
// open markers keep the outer span. Malformed markers (an open with no
// terminator on the row) are skipped: the renderer never emits them,
// and a half-sequence must not shift the column count of what follows.
func ScanOsc8Spans(lines []string) []Osc8Span {
	var spans []Osc8Span
	for row, line := range lines {
		col := 0
		openURL := ""
		spanStart := 0
		var labelText []string
		rest := line
		for rest != "" {
			i := strings.Index(rest, osc8Open)
			if i < 0 {
				if openURL != "" {
					labelText = append(labelText, rest)
				}
				break
			}
			if openURL != "" {
				labelText = append(labelText, rest[:i])
			}
			col += ansi.StringWidth(rest[:i])
			payload := rest[i+len(osc8Open):]
			st := strings.Index(payload, "\x1b\\")
			if st < 0 {
				// Malformed: no terminator. The marker is zero-width,
				// so consume just it and keep scanning the rest.
				rest = payload
				continue
			}
			url := payload[:st]
			rest = payload[st+2:]
			if url == "" {
				if openURL != "" {
					spans = append(spans, Osc8Span{
						Row: row, ColStart: spanStart, ColEnd: col, URL: openURL,
						Text: ansi.Strip(strings.Join(labelText, "")),
					})
					openURL = ""
					labelText = nil
				}
			} else if openURL == "" {
				openURL = url
				spanStart = col
				labelText = nil
			}
		}
	}
	return spans
}

// HitsFromSpans turns scanned spans into hit rects: a span that IS a
// file attachment's [File] line becomes a HitFile for that attachment
// (a click downloads it, as `d` would), every other span a HitLink (a
// click opens it, as `o` would). A span qualifies as a [File] line when
// its URL is one of msg's file attachments AND its label begins with
// the line's marker - a body link to the same URL ("view this file")
// opens rather than downloads. Matching by URL rather than by render
// order is what keeps the classification on the right attachment: the
// rows WrapRows produced are the rows being classified, so layout
// cannot shift a rect onto another attachment's rows.
func HitsFromSpans(spans []Osc8Span, msg MessageItem) []entrylist.Hit {
	if len(spans) == 0 {
		return nil
	}
	fileIdx := make(map[string]int, len(msg.Attachments))
	for i, att := range msg.Attachments {
		if att.Kind == "file" && att.URL != "" {
			fileIdx[att.URL] = i
		}
	}
	hits := make([]entrylist.Hit, 0, len(spans))
	for _, s := range spans {
		h := entrylist.Hit{
			RowStart: s.Row, RowEnd: s.Row + 1,
			ColStart: s.ColStart, ColEnd: s.ColEnd,
			Key: s.URL,
		}
		if idx, ok := fileIdx[s.URL]; ok && strings.HasPrefix(s.Text, "[File]") {
			h.Kind = entrylist.HitFile
			h.Aux = idx
		} else {
			h.Kind = entrylist.HitLink
		}
		hits = append(hits, h)
	}
	return hits
}

// ScanLinkHits is ScanOsc8Spans plus HitsFromSpans for callers that
// have no attachments to classify (all spans become HitLink).
func ScanLinkHits(lines []string) []entrylist.Hit {
	spans := ScanOsc8Spans(lines)
	if len(spans) == 0 {
		return nil
	}
	hits := make([]entrylist.Hit, 0, len(spans))
	for _, s := range spans {
		hits = append(hits, entrylist.Hit{
			RowStart: s.Row, RowEnd: s.Row + 1,
			ColStart: s.ColStart, ColEnd: s.ColEnd,
			Kind: entrylist.HitLink, Key: s.URL,
		})
	}
	return hits
}
