package emoji

import (
	"strings"
	"testing"
)

func TestCodepointsFromName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []rune
		ok   bool
	}{
		{"thumbsup", "1f44d", []rune{0x1F44D}, true},
		{"medium tone", "1f44d-1f3fd", []rune{0x1F44D, 0x1F3FD}, true},
		{"one digit", "a9", []rune{0xA9}, true},
		{"flag pair", "1f1fa-1f1f8", []rune{0x1F1FA, 0x1F1F8}, true},
		{"uppercase rejected", "1F44D", nil, false},
		{"not hex", "shrug", nil, false},
		{"hex word rejected", "cafe", nil, false},
		{"digit rejected", "30", nil, false},
		{"control rejected", "a", nil, false},
		{"emoji plus non-emoji", "1f44d-41", nil, false},
		{"trailing hyphen", "1f44d-", nil, false},
		{"leading hyphen", "-1f44d", nil, false},
		{"too long part", "1234567", nil, false},
		{"above unicode", "110000", nil, false},
		{"surrogate", "d800", nil, false},
		{"zero", "0", nil, false},
		{"empty", "", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := CodepointsFromName(c.in)
			if ok != c.ok {
				t.Fatalf("CodepointsFromName(%q) ok = %v, want %v", c.in, ok, c.ok)
			}
			if string(got) != string(c.want) {
				t.Errorf("CodepointsFromName(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestShortcodeGlyph_ResolvesCodepointNames(t *testing.T) {
	if got, ok := ShortcodeGlyph("thumbsup"); !ok || got != "👍" {
		t.Errorf("ShortcodeGlyph(thumbsup) = %q, %v, want the thumbsup glyph", got, ok)
	}
	if got, ok := ShortcodeGlyph("1f44d"); !ok || got != "👍" {
		t.Errorf("ShortcodeGlyph(1f44d) = %q, %v, want the thumbsup glyph", got, ok)
	}
	if got, ok := ShortcodeGlyph("1f44d-1f3fd"); !ok || got != "👍🏽" {
		t.Errorf("ShortcodeGlyph(1f44d-1f3fd) = %q, %v, want the toned thumbsup", got, ok)
	}
	if _, ok := ShortcodeGlyph("definitely_not_an_emoji"); ok {
		t.Error("an unknown name resolved")
	}
	if _, ok := ShortcodeGlyph("cafe"); ok {
		t.Error("a hex-shaped word resolved")
	}
}

func TestSprint_LeavesOrdinaryTextAlone(t *testing.T) {
	for _, in := range []string{"12:30:45", "a :cafe: word", "a:a"} {
		if got := Sprint(in); got != in {
			t.Errorf("Sprint(%q) = %q, want it unchanged", in, got)
		}
	}
}

func TestSprint_ResolvesCodepointNames(t *testing.T) {
	if got, want := Sprint("thanks :1f44d:"), "thanks 👍 "; got != want {
		t.Errorf("Sprint = %q, want %q", got, want)
	}
	// Single-scalar codepoint names that are emoji on their own resolve
	// too. They are hex-shaped, but the emoji check accepts them.
	for name, want := range map[string]string{"a9": "©", "ae": "®", "2122": "™"} {
		if got, ok := ShortcodeGlyph(name); !ok || got != want {
			t.Errorf("ShortcodeGlyph(%q) = %q, %v, want %q", name, got, ok, want)
		}
	}
}

func TestResolveShortcodesInText_CodepointName(t *testing.T) {
	if got, want := ResolveShortcodesInText(":1f44d:"), "👍 "; got != want {
		t.Errorf("single codepoint name = %q, want %q", got, want)
	}
	// A skin-toned name is multi-codepoint: the glyph-width policy
	// leaves it as text in plain text.
	if got := ResolveShortcodesInText(":1f44d-1f3fd:"); got != ":1f44d-1f3fd:" {
		t.Errorf("skin-toned name = %q, want the shortcode kept", got)
	}
}

func TestURLForShortcode_CodepointName(t *testing.T) {
	url, ok := URLForShortcode("1f44d", nil)
	if !ok {
		t.Fatal("URLForShortcode(1f44d) missed")
	}
	if !strings.HasSuffix(url, "/1f44d.png") {
		t.Errorf("url = %q, want the thumbsup asset", url)
	}
	// A bare sequence must ask for the asset Slack has: the heart is
	// 2764-fe0f.png, and 2764.png 404s.
	url, ok = URLForShortcode("2764", nil)
	if !ok {
		t.Fatal("URLForShortcode(2764) missed")
	}
	if !strings.HasSuffix(url, "/2764-fe0f.png") {
		t.Errorf("url = %q, want the VS16 heart asset", url)
	}
}

func TestCodepointFormCanonicalName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"1f44d", "+1"},
		{"1f44d-1f3fd", "+1::skin-tone-4"},
		{"2764", "heart"},
	}
	for _, c := range cases {
		got, ok := CodepointFormCanonicalName(c.in)
		if !ok || got != c.want {
			t.Errorf("CodepointFormCanonicalName(%q) = %q, %v, want %q", c.in, got, ok, c.want)
		}
	}
	if _, ok := CodepointFormCanonicalName("not_an_emoji"); ok {
		t.Error("an ordinary name resolved")
	}
}
