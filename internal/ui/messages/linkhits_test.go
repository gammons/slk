package messages

import (
	"testing"

	"github.com/gammons/slk/internal/bubbles/entrylist"
	"github.com/gammons/slk/internal/core"
)

func TestScanOsc8Spans_NoLinks(t *testing.T) {
	lines := []string{"plain text", "\x1b[1mbold\x1b[0m only"}
	if got := ScanOsc8Spans(lines); len(got) != 0 {
		t.Errorf("spans = %+v; want none", got)
	}
}

func TestScanOsc8Spans_SingleSpan(t *testing.T) {
	// The shape render.go emits: OSC 8 open, styled label, close. The
	// surrounding text and the styling escapes cost no columns.
	lines := []string{
		"see " + osc8Hyperlink("https://x.example", "\x1b[4mdocs\x1b[0m") + " for more",
	}
	got := ScanOsc8Spans(lines)
	if len(got) != 1 {
		t.Fatalf("spans = %+v; want one", got)
	}
	s := got[0]
	if s.URL != "https://x.example" {
		t.Errorf("url = %q; want https://x.example", s.URL)
	}
	// "see " occupies columns 0-3, the label "docs" 4-7.
	if s.ColStart != 4 || s.ColEnd != 8 {
		t.Errorf("cols = [%d,%d); want [4,8)", s.ColStart, s.ColEnd)
	}
	if s.Row != 0 {
		t.Errorf("row = %d; want 0", s.Row)
	}
}

func TestScanOsc8Spans_TwoSpansOneRow(t *testing.T) {
	lines := []string{
		osc8Hyperlink("https://a.example", "one") + " and " + osc8Hyperlink("https://b.example", "two"),
	}
	got := ScanOsc8Spans(lines)
	if len(got) != 2 {
		t.Fatalf("spans = %+v; want two", got)
	}
	if got[0].URL != "https://a.example" || got[0].ColStart != 0 || got[0].ColEnd != 3 {
		t.Errorf("first = %+v; want a.example [0,3)", got[0])
	}
	// "one and " is 8 columns, so "two" spans [8,11).
	if got[1].URL != "https://b.example" || got[1].ColStart != 8 || got[1].ColEnd != 11 {
		t.Errorf("second = %+v; want b.example [8,11)", got[1])
	}
}

func TestScanOsc8Spans_WrappedSpanYieldsNothing(t *testing.T) {
	// A label that wraps - open on row 0, close on row 1 - yields no
	// span: the continuation row begins after the border and avatar
	// gutter the wrap inserted, and a rect covering it would make the
	// gutter and trailing padding clickable. Wrapped labels stay
	// reachable through `o` and the terminal's Shift+Click.
	lines := []string{
		"tail " + "\x1b]8;;https://long.example\x1b\\" + "first part",
		"second part\x1b]8;;\x1b\\ after",
	}
	if got := ScanOsc8Spans(lines); len(got) != 0 {
		t.Errorf("spans = %+v; want none for a wrapped label", got)
	}
}

func TestScanOsc8Spans_WideRunesCostColumns(t *testing.T) {
	// A wide rune before the span occupies two columns, and the label's
	// own wide runes count two columns each.
	lines := []string{"世 " + osc8Hyperlink("https://x.example", "a世b")}
	got := ScanOsc8Spans(lines)
	if len(got) != 1 {
		t.Fatalf("spans = %+v; want one", got)
	}
	// "世 " = 3 columns, label "a世b" = 4 columns.
	if got[0].ColStart != 3 || got[0].ColEnd != 7 {
		t.Errorf("cols = [%d,%d); want [3,7)", got[0].ColStart, got[0].ColEnd)
	}
}

func TestScanOsc8Spans_AttachmentLineShape(t *testing.T) {
	// The exact shape renderLegacyLine emits: the whole line inside one
	// span, marker styling inside the label.
	line := "\x1b]8;;https://files.example/F123\x1b\\" +
		"\x1b[1m[File]\x1b[0m \x1b[4mreport.pdf\x1b[0m" +
		"\x1b]8;;\x1b\\"
	got := ScanOsc8Spans([]string{line})
	if len(got) != 1 {
		t.Fatalf("spans = %+v; want one", got)
	}
	if got[0].URL != "https://files.example/F123" {
		t.Errorf("url = %q", got[0].URL)
	}
	// "[File] report.pdf" is 17 visible columns.
	if got[0].ColStart != 0 || got[0].ColEnd != 17 {
		t.Errorf("cols = [%d,%d); want [0,17)", got[0].ColStart, got[0].ColEnd)
	}
}

func TestHitsFromSpans_FileURLBecomesHitFile(t *testing.T) {
	msg := MessageItem{Attachments: []core.Attachment{
		{Kind: "image", URL: "https://files.example/img", FileID: "F1"},
		{Kind: "file", URL: "https://files.example/doc", DownloadURL: "https://files.example/dl"},
	}}
	spans := []Osc8Span{
		{Row: 0, ColStart: 1, ColEnd: 5, URL: "https://files.example/doc", Text: "[File] doc.pdf"},
		{Row: 1, ColStart: 1, ColEnd: 5, URL: "https://example.com/elsewhere", Text: "elsewhere"},
		{Row: 2, ColStart: 1, ColEnd: 5, URL: "https://files.example/img", Text: "[Image] img.png"},
	}
	got := HitsFromSpans(spans, msg)
	if len(got) != 3 {
		t.Fatalf("hits = %+v; want three", got)
	}
	if got[0].Kind != entrylist.HitFile || got[0].Aux != 1 {
		t.Errorf("doc span = %+v; want HitFile Aux=1 (the file attachment)", got[0])
	}
	if got[1].Kind != entrylist.HitLink {
		t.Errorf("external span = %+v; want HitLink", got[1])
	}
	// An image attachment's URL is not a HitFile: its line is the
	// "[Image] url" fallback, a hyperlink, not a download.
	if got[2].Kind != entrylist.HitLink {
		t.Errorf("image span = %+v; want HitLink (image fallback lines open, not download)", got[2])
	}
}

func TestHitsFromSpans_BodyLinkToAttachmentURLOpens(t *testing.T) {
	// A message body can link to an attached file's URL ("view this
	// file"). That link must open, not download: only a span whose
	// label IS the [File] line qualifies as HitFile.
	msg := MessageItem{
		Text: "view this file",
		Attachments: []core.Attachment{
			{Kind: "file", URL: "https://files.example/doc", DownloadURL: "https://files.example/dl"},
		},
	}
	spans := []Osc8Span{
		{Row: 0, ColStart: 1, ColEnd: 5, URL: "https://files.example/doc", Text: "view this file"},
		{Row: 2, ColStart: 1, ColEnd: 5, URL: "https://files.example/doc", Text: "[File] doc.pdf"},
	}
	got := HitsFromSpans(spans, msg)
	if got[0].Kind != entrylist.HitLink {
		t.Errorf("body link to the attachment URL = %+v; want HitLink (opens)", got[0])
	}
	if got[1].Kind != entrylist.HitFile || got[1].Aux != 0 {
		t.Errorf("[File] line = %+v; want HitFile Aux=0 (downloads)", got[1])
	}
}
