package messages

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A hyperlink whose label wraps is split into one complete OSC 8 span
// per row, so every segment stays clickable.
func TestWordWrapReopensWrappedLinks(t *testing.T) {
	const url = "https://example.com/a/very/long/path/that/wraps#note_123"
	wrapped := WordWrap(osc8Hyperlink(url, url), 20)
	lines := strings.Split(wrapped, "\n")
	if len(lines) < 2 {
		t.Fatalf("link did not wrap: %q", wrapped)
	}
	spans := ScanOsc8Spans(lines)
	if len(spans) != len(lines) {
		t.Fatalf("spans = %d for %d wrapped rows: %q", len(spans), len(lines), wrapped)
	}
	for _, span := range spans {
		if span.URL != url {
			t.Errorf("span URL = %q, want %q", span.URL, url)
		}
	}
}

// Each span covers exactly its visible label segment, and the wrap
// leaves the surrounding text alone.
func TestWordWrapWrappedLinkSpansMatchColumns(t *testing.T) {
	const url = "https://example.com/a/very/long/path/that/wraps#note_123"
	wrapped := WordWrap("before "+osc8Hyperlink(url, url)+" after", 30)
	lines := strings.Split(wrapped, "\n")
	spans := ScanOsc8Spans(lines)
	if len(spans) != 2 {
		t.Fatalf("spans = %+v, want two wrapped segments", spans)
	}
	var label strings.Builder
	for i, span := range spans {
		if span.Row != i+1 {
			t.Errorf("span %d row = %d, want %d", i, span.Row, i+1)
		}
		if span.ColStart != 0 {
			t.Errorf("span %d starts at col %d, want 0", i, span.ColStart)
		}
		plain := ansi.Strip(lines[span.Row])
		if got := ansi.Cut(plain, span.ColStart, span.ColEnd); got != span.Text {
			t.Errorf("span %d covers %q, want %q", i, got, span.Text)
		}
		label.WriteString(span.Text)
	}
	if label.String() != url {
		t.Errorf("label = %q, want %q", label.String(), url)
	}
	plain := strings.Split(ansi.Strip(wrapped), "\n")
	if plain[0] != "before" || plain[len(plain)-1] != "after" {
		t.Errorf("surrounding text = %q, want before/after", plain)
	}
	if !strings.Contains(strings.Join(plain, ""), url) {
		t.Errorf("visible output lost the URL: %q", plain)
	}
}

// A link label with spaces wraps across words too; each row still gets
// its own markers.
func TestWordWrapReopensSpacedLinkLabels(t *testing.T) {
	const url = "https://example.com"
	const label = "one two three four five six seven"
	wrapped := WordWrap(osc8Hyperlink(url, label), 20)
	lines := strings.Split(wrapped, "\n")
	if len(lines) < 2 {
		t.Fatalf("label did not wrap: %q", wrapped)
	}
	spans := ScanOsc8Spans(lines)
	if len(spans) != len(lines) {
		t.Fatalf("spans = %d for %d wrapped rows: %q", len(spans), len(lines), wrapped)
	}
	for _, span := range spans {
		if span.URL != url {
			t.Errorf("span URL = %q, want %q", span.URL, url)
		}
	}
}
