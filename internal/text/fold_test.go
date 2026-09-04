package text

import (
	"strings"
	"testing"
)

func TestFold(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"ascii lower", "cafe", "cafe"},
		{"ascii mixed case", "Foo Bar", "foo bar"},
		{"french acute", "Mélanie", "melanie"},
		{"french acute upper", "MÉLANIE", "melanie"},
		{"french cedilla", "François", "francois"},
		{"french grave/acute mix", "Café", "cafe"},
		{"spanish tilde n", "año", "ano"},
		{"portuguese tilde a", "São", "sao"},
		{"german umlaut", "Müller", "muller"},
		{"vietnamese tones", "Việt", "viet"},
		{"mixed script (cjk passes through)", "東京 Tōkyō", "東京 tokyo"},
		{"already folded passes through", "melanie", "melanie"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Fold(tc.in)
			if got != tc.want {
				t.Errorf("Fold(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestFoldMatchesBothDirections proves that folding makes substring matching
// symmetric across diacritics: query and candidate compare equal after fold
// regardless of which side carries the accents.
func TestFoldMatchesBothDirections(t *testing.T) {
	name := "Mélanie"
	for _, q := range []string{"melanie", "Mélanie", "MELANIE", "mél"} {
		if !strings.Contains(Fold(name), Fold(q)) {
			t.Errorf("Fold(%q) should contain Fold(%q)", name, q)
		}
	}
}

// TestFoldDoesNotPanicOnSingleCombiningMark guards against a regression
// where a lone combining mark (no base character) trips the transform.
func TestFoldDoesNotPanicOnSingleCombiningMark(t *testing.T) {
	// U+0301 COMBINING ACUTE ACCENT, in isolation.
	_ = Fold("\u0301")
}

// foldInputs is shared by the fast-path equivalence test and the fuzz
// seed corpus: every ASCII shape that could plausibly reach a picker
// filter, plus the non-ASCII cases that must still take the slow path.
var foldInputs = []string{
	"",
	"a",
	"A",
	"Z",
	"cafe",
	"Foo Bar",
	"CUSTOM_00042",
	"custom_00042",
	"tada",
	"+1",
	"-1",
	"white_check_mark",
	"party-parrot",
	"0123456789",
	"~!@#$%^&*()_+{}|:\"<>?[]\\;',./",
	"\t\n\r ",
	"\x00\x01\x7f",
	"MiXeD_CaSe-42",
	// Non-ASCII: must route to foldSlow and fold identically.
	"Mélanie",
	"MÉLANIE",
	"François",
	"año",
	"São",
	"Müller",
	"Việt",
	"東京 Tōkyō",
	"ß",
	"ﬃ",
	"\u0301",
	"a\u0301",
	"🎉",
	"👍🏽",
	"e\u0301chec",
}

// TestFoldFastPathMatchesSlow pins Fold's ASCII fast path to foldSlow,
// the reference implementation. The fast path is only sound because
// NFD -> remove Mn -> NFC is the identity on ASCII; if that assumption
// is ever wrong, or the chain changes, this fails instead of silently
// returning different results for ASCII than for everything else.
func TestFoldFastPathMatchesSlow(t *testing.T) {
	for _, in := range foldInputs {
		if got, want := Fold(in), foldSlow(in); got != want {
			t.Errorf("Fold(%q) = %q, but foldSlow(%q) = %q", in, got, in, want)
		}
	}
}

// TestIsASCII covers the boundary between the two paths.
func TestIsASCII(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", true},
		{"abc", true},
		{"\x7f", true},          // last ASCII byte
		{"\u0080", false},       // first non-ASCII rune
		{"caf\u00e9", false},    // é
		{"abc\u00e9def", false}, // non-ASCII in the middle
		{"🎉", false},
	}
	for _, tc := range cases {
		if got := isASCII(tc.in); got != tc.want {
			t.Errorf("isASCII(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// FuzzFold asserts the same equivalence on arbitrary input. The seed
// corpus alone runs during a normal `go test`, so this guards the
// invariant even without -fuzz.
func FuzzFold(f *testing.F) {
	for _, in := range foldInputs {
		f.Add(in)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if got, want := Fold(s), foldSlow(s); got != want {
			t.Errorf("Fold(%q) = %q, but foldSlow(%q) = %q", s, got, s, want)
		}
	})
}

func BenchmarkFold(b *testing.B) {
	// The realistic hot-loop input: a lowercase ASCII emoji or channel
	// name, folded once per candidate per keystroke.
	b.Run("ascii_lower", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = Fold("white_check_mark")
		}
	})
	b.Run("ascii_mixed_case", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = Fold("White_Check_Mark")
		}
	})
	b.Run("non_ascii", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = Fold("Mélanie")
		}
	})
}
