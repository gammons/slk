package messages

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/styles"
)

func TestHighlightSearchTerms_PlainText(t *testing.T) {
	got := HighlightSearchTerms("deploy went fine", []string{"deploy"}, "\x1b[7m", "\x1b[27m")
	want := "\x1b[7mdeploy\x1b[27m went fine"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHighlightSearchTerms_WordPrefixOnly(t *testing.T) {
	// "ploy" is not at a word start; must not highlight inside "deploy".
	got := HighlightSearchTerms("deploy", []string{"ploy"}, "[", "]")
	if got != "deploy" {
		t.Errorf("mid-word match highlighted: %q", got)
	}
	// but "dep" at word start highlights the prefix
	got = HighlightSearchTerms("deploy", []string{"dep"}, "[", "]")
	if got != "[dep]loy" {
		t.Errorf("prefix: %q", got)
	}
}

func TestHighlightSearchTerms_CaseAndAccentInsensitive(t *testing.T) {
	got := HighlightSearchTerms("Café open", []string{"cafe"}, "[", "]")
	if got != "[Café] open" {
		t.Errorf("fold: %q", got)
	}
}

func TestHighlightSearchTerms_DecomposedAccentPreservesRuneMapping(t *testing.T) {
	got := HighlightSearchTerms("Cafe\u0301 open", []string{"cafe"}, "[", "]")
	if got != "[Cafe]\u0301 open" {
		t.Errorf("fold: %q", got)
	}
}

func TestHighlightSearchTerms_SkipsANSISequences(t *testing.T) {
	in := "\x1b[31mdeploy\x1b[0m fine"
	got := HighlightSearchTerms(in, []string{"deploy"}, "[", "]")
	// The ANSI color sequence is preserved; visible text "deploy" is
	// wrapped; active sequences are re-applied after the highlight end.
	if !strings.Contains(got, "[deploy]") {
		t.Errorf("match not highlighted across ANSI: %q", got)
	}
	if !strings.Contains(got, "\x1b[31m") {
		t.Errorf("original ANSI dropped: %q", got)
	}
}

func TestHighlightSearchTerms_NoTerms(t *testing.T) {
	if got := HighlightSearchTerms("anything", nil, "[", "]"); got != "anything" {
		t.Errorf("no-op expected: %q", got)
	}
}

func TestHighlightSearchTerms_MultipleTermsAndOccurrences(t *testing.T) {
	got := HighlightSearchTerms("foo bar foo", []string{"foo", "bar"}, "[", "]")
	if got != "[foo] [bar] [foo]" {
		t.Errorf("got %q", got)
	}
}

func TestHighlightSearchTerms_OSC8HyperlinkBEL(t *testing.T) {
	// OSC 8 hyperlink with BEL terminators: the label is highlighted,
	// the URL bytes inside the OSC are never touched.
	in := "\x1b]8;;https://x\x07deploy\x1b]8;;\x07 fine"
	got := HighlightSearchTerms(in, []string{"deploy"}, "[", "]")
	want := "\x1b]8;;https://x\x07[deploy]\x1b]8;;\x07 fine"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHighlightSearchTerms_OSC8HyperlinkST_URLNotHighlighted(t *testing.T) {
	// ST (\x1b\\) terminators, as emitted by osc8Hyperlink. The URL
	// contains the search term; corrupting it would break the hyperlink,
	// so it must pass through byte-identical.
	in := "\x1b]8;;https://deploy.example\x1b\\deploy\x1b]8;;\x1b\\"
	got := HighlightSearchTerms(in, []string{"deploy"}, "[", "]")
	want := "\x1b]8;;https://deploy.example\x1b\\[deploy]\x1b]8;;\x1b\\"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHighlightSearchTerms_OSCPreservesWordBoundaryState(t *testing.T) {
	// An OSC sequence between letters must not create a false word
	// start: "ploy" after the OSC is still mid-word ("de…ploy").
	in := "de\x1b]8;;x\x07ploy"
	got := HighlightSearchTerms(in, []string{"ploy"}, "[", "]")
	if got != in {
		t.Errorf("mid-word match highlighted across OSC: %q", got)
	}
}

func TestHighlightSearchTerms_BareTrailingEscape(t *testing.T) {
	got := HighlightSearchTerms("deploy\x1b", []string{"deploy"}, "[", "]")
	want := "[deploy]\x1b"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHighlightSearchTerms_UnterminatedOSC(t *testing.T) {
	// Unterminated OSC consumes the rest of the string as one opaque
	// ANSI segment.
	in := "deploy \x1b]8;;https://x"
	got := HighlightSearchTerms(in, []string{"deploy"}, "[", "]")
	want := "[deploy] \x1b]8;;https://x"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHighlightSearchTerms_NonCSIEscape(t *testing.T) {
	// \x1b(B (charset designation): ESC plus the next byte are consumed
	// as an opaque 2-byte segment; the byte after that is visible text.
	in := "fine \x1b(B deploy"
	got := HighlightSearchTerms(in, []string{"deploy"}, "[", "]")
	want := "fine \x1b(B [deploy]"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSearchHighlightSGR_EndRestoresThemeColors(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })
	start, end, ok := SearchHighlightSGR()
	if !ok {
		t.Fatal("SearchHighlightSGR returned !ok")
	}
	if start == "" {
		t.Fatal("empty start sequence")
	}
	bg, fg := BgANSI(), FgANSI()
	if bg == "" || fg == "" {
		t.Fatalf("theme ANSI helpers empty: bg=%q fg=%q", bg, fg)
	}
	bi := strings.Index(end, bg)
	fi := strings.Index(end, fg)
	if bi < 0 || fi < 0 {
		t.Fatalf("end %q does not restore theme bg/fg (bg=%q fg=%q)", end, bg, fg)
	}
	if fi < bi {
		t.Errorf("fg restored before bg in %q", end)
	}
	if bi == 0 {
		t.Errorf("no close/reset sequence before bg restore in %q", end)
	}
}

func BenchmarkHighlightSearchTerms_ASCII(b *testing.B) {
	line := strings.Repeat("the quick brown fox jumps over the lazy dog ", 4)
	terms := []string{"deploy"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		HighlightSearchTerms(line, terms, "\x1b[7m", "\x1b[27m")
	}
}
