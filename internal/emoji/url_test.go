package emoji

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type urlFixture struct {
	Base    string            `json:"base"`
	Entries []urlFixtureEntry `json:"entries"`
}

type urlFixtureEntry struct {
	Name       string `json:"name"`
	Codepoints []rune `json:"codepoints"`
	URL        string `json:"url"`
}

func loadURLFixture(t *testing.T) urlFixture {
	t.Helper()
	path := filepath.Join("testdata", "slack_urls.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var f urlFixture
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if len(f.Entries) == 0 {
		t.Fatalf("fixture has no entries")
	}
	return f
}

func TestBuildStandardEmojiURL(t *testing.T) {
	fixture := loadURLFixture(t)
	for _, e := range fixture.Entries {
		got := BuildStandardEmojiURL(e.Codepoints)
		if got != e.URL {
			t.Errorf("BuildStandardEmojiURL(%q codepoints=%v) = %q, want %q",
				e.Name, e.Codepoints, got, e.URL)
		}
	}
}

func TestCodepointsForShortcode_Builtin(t *testing.T) {
	cases := []struct {
		name string
		want []rune // expected codepoints
	}{
		{"thumbsup", []rune{0x1F44D}},
		{"heart", []rune{0x2764, 0xFE0F}},
		{"male-astronaut", []rune{0x1F468, 0x200D, 0x1F680}},
		{"warning", []rune{0x26A0, 0xFE0F}},
		{"fire", []rune{0x1F525}},
	}
	for _, c := range cases {
		got, ok := CodepointsForShortcode(c.name)
		if !ok {
			t.Errorf("CodepointsForShortcode(%q): ok=false, want a standard-emoji hit", c.name)
			continue
		}
		if !runesEqual(got, c.want) {
			t.Errorf("CodepointsForShortcode(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCodepointsForShortcode_Unknown(t *testing.T) {
	if _, ok := CodepointsForShortcode("definitely_not_an_emoji_name_xyz"); ok {
		t.Errorf("CodepointsForShortcode(unknown): ok=true, want false")
	}
}

func runesEqual(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestBuildCustomEmojiURL(t *testing.T) {
	customs := map[string]string{
		"party_parrot": "https://emoji.slack-edge.com/T01/party_parrot/abc.gif",
		"company_logo": "https://emoji.slack-edge.com/T01/company_logo/def.png",
		"shipit":       "alias:rocket",       // alias to a built-in
		"yay":          "alias:party_parrot", // alias to a custom
		"chain_a":      "alias:chain_b",
		"chain_b":      "alias:chain_c",
		"chain_c":      "https://emoji.slack-edge.com/T01/chain_c/xyz.png",
		"loop_a":       "alias:loop_b",
		"loop_b":       "alias:loop_a",
	}

	cases := []struct {
		name    string
		wantURL string
		wantOK  bool
	}{
		// Direct custom: URL returned verbatim.
		{"party_parrot", "https://emoji.slack-edge.com/T01/party_parrot/abc.gif", true},
		{"company_logo", "https://emoji.slack-edge.com/T01/company_logo/def.png", true},

		// alias:<builtin>: resolves to the standard emoji URL.
		// rocket = U+1F680 = 1f680.png
		{"shipit", CDNBaseURL + "1f680.png", true},

		// alias:<custom>: resolves through to the custom's URL.
		{"yay", "https://emoji.slack-edge.com/T01/party_parrot/abc.gif", true},

		// Multi-hop alias chain.
		{"chain_a", "https://emoji.slack-edge.com/T01/chain_c/xyz.png", true},

		// Alias cycle: detected, returns ok=false.
		{"loop_a", "", false},

		// Unknown name: ok=false.
		{"never_defined", "", false},
	}
	for _, c := range cases {
		got, ok := BuildCustomEmojiURL(c.name, customs)
		if ok != c.wantOK || got != c.wantURL {
			t.Errorf("BuildCustomEmojiURL(%q) = (%q, %v), want (%q, %v)",
				c.name, got, ok, c.wantURL, c.wantOK)
		}
	}
}

func TestComposeSkinTonedCodepoints(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []rune
		ok   bool
	}{
		// Slack reaction-API form.
		{"slack thumbsup tone 3", "thumbsup::skin-tone-3", []rune{0x1F44D, 0x1F3FD}, true},
		{"slack +1 tone 2", "+1::skin-tone-2", []rune{0x1F44D, 0x1F3FC}, true},
		// kyokomi form.
		{"kyokomi wave tone 5", "wave_tone5", []rune{0x1F44B, 0x1F3FF}, true},
		// ZWJ sequence: the modifier belongs after the base codepoint,
		// before the ZWJ tail. Appending it instead builds a filename
		// Slack's CDN 404s. Every other case here has a single-codepoint
		// base, where the two orderings are indistinguishable.
		{"zwj person_running_facing_right tone 2", "person_running_facing_right::skin-tone-2",
			[]rune{0x1F3C3, 0x1F3FC, 0x200D, 0x27A1, 0xFE0F}, true},
		// No tone suffix.
		{"no suffix", "thumbsup", nil, false},
		// Tone out of range.
		{"slack tone 6", "thumbsup::skin-tone-6", nil, false},
		// Unknown base.
		{"unknown base", "definitely_not_an_emoji_xyz_tone3", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ComposeSkinTonedCodepoints(c.in)
			if ok != c.ok {
				t.Errorf("ComposeSkinTonedCodepoints(%q) ok = %v, want %v", c.in, ok, c.ok)
			}
			if ok && !runesEqual(got, c.want) {
				t.Errorf("ComposeSkinTonedCodepoints(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestURLForShortcode_SkinTonedFallback(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// Direct kyokomi hit — uses CodepointsForShortcode first.
		{"thumbsup_tone3 (kyokomi)", "thumbsup_tone3", CDNBaseURL + "1f44d-1f3fd.png"},
		// Slack-form fallback via ComposeSkinTonedCodepoints.
		{"thumbsup slack form", "thumbsup::skin-tone-2", CDNBaseURL + "1f44d-1f3fc.png"},
		{"+1 slack form (alias)", "+1::skin-tone-3", CDNBaseURL + "1f44d-1f3fd.png"},
		{"+1_tone3 (kyokomi miss)", "+1_tone3", CDNBaseURL + "1f44d-1f3fd.png"},
		// The ordering above, end to end as a CDN filename.
		{"zwj slack form", "person_running_facing_right::skin-tone-2",
			CDNBaseURL + "1f3c3-1f3fc-200d-27a1-fe0f.png"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := URLForShortcode(c.in, nil)
			if !ok || got != c.want {
				t.Errorf("URLForShortcode(%q) = (%q, %v), want (%q, true)", c.in, got, ok, c.want)
			}
		})
	}
}

func TestURLForShortcode(t *testing.T) {
	customs := map[string]string{
		"party_parrot": "https://emoji.slack-edge.com/T01/party_parrot/abc.gif",
		"thumbsup":     "https://emoji.slack-edge.com/T01/our_thumbs/def.png", // workspace override
	}
	cases := []struct {
		name    string
		wantURL string
		wantOK  bool
	}{
		// Workspace custom wins over kyokomi for the same name.
		{"thumbsup", "https://emoji.slack-edge.com/T01/our_thumbs/def.png", true},

		// Custom-only name.
		{"party_parrot", "https://emoji.slack-edge.com/T01/party_parrot/abc.gif", true},

		// kyokomi-only name.
		{"heart", CDNBaseURL + "2764-fe0f.png", true},

		// Unknown.
		{"never_defined", "", false},
	}
	for _, c := range cases {
		got, ok := URLForShortcode(c.name, customs)
		if ok != c.wantOK || got != c.wantURL {
			t.Errorf("URLForShortcode(%q) = (%q, %v), want (%q, %v)",
				c.name, got, ok, c.wantURL, c.wantOK)
		}
	}
}
