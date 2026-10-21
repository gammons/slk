package emoji

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
		// Slack reaction-API form. Slack numbers tones 2-6 (1 is the
		// default yellow), so skin-tone-2 is the lightest modifier,
		// U+1F3FB; a rich_text emoji element with "skin_tone": 2
		// carries "unicode": "1f44d-1f3fb".
		{"slack +1 tone 2", "+1::skin-tone-2", []rune{0x1F44D, 0x1F3FB}, true},
		{"slack thumbsup tone 3", "thumbsup::skin-tone-3", []rune{0x1F44D, 0x1F3FC}, true},
		{"slack tone 6", "thumbsup::skin-tone-6", []rune{0x1F44D, 0x1F3FF}, true},
		// kyokomi form numbers them 1-5.
		{"kyokomi wave tone 5", "wave_tone5", []rune{0x1F44B, 0x1F3FF}, true},
		// ZWJ sequence: the modifier belongs after the base codepoint,
		// before the ZWJ tail. Appending it instead builds a filename
		// Slack's CDN 404s. Every other case here has a single-codepoint
		// base, where the two orderings are indistinguishable.
		{"zwj person_running_facing_right tone 2", "person_running_facing_right::skin-tone-2",
			[]rune{0x1F3C3, 0x1F3FB, 0x200D, 0x27A1, 0xFE0F}, true},
		// A VS16 right after the base is replaced by the modifier, not
		// kept after it: ✌️ is 270C FE0F, ✌🏼 is 270C 1F3FC.
		{"vs16 base", "v::skin-tone-3", []rune{0x270C, 0x1F3FC}, true},
		{"vs16 base in zwj", "woman-lifting-weights::skin-tone-2",
			[]rune{0x1F3CB, 0x1F3FB, 0x200D, 0x2640, 0xFE0F}, true},
		// No tone suffix.
		{"no suffix", "thumbsup", nil, false},
		// Tone out of range.
		{"slack tone 1", "thumbsup::skin-tone-1", nil, false},
		{"slack tone 7", "thumbsup::skin-tone-7", nil, false},
		// Base that takes no skin tone: there is no toned image.
		{"untoneable base", "tada::skin-tone-2", nil, false},
		// Takes a tone per person and has no single-tone image, so
		// one modifier can't name an asset.
		{"two-tone-only base", "people_holding_hands::skin-tone-2", nil, false},
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
		{"thumbsup slack form", "thumbsup::skin-tone-2", CDNBaseURL + "1f44d-1f3fb.png"},
		{"+1 slack form (alias)", "+1::skin-tone-3", CDNBaseURL + "1f44d-1f3fc.png"},
		{"+1_tone3 (kyokomi miss)", "+1_tone3", CDNBaseURL + "1f44d-1f3fd.png"},
		// The ordering above, end to end as a CDN filename.
		{"zwj slack form", "person_running_facing_right::skin-tone-2",
			CDNBaseURL + "1f3c3-1f3fb-200d-27a1-fe0f.png"},
		{"vs16 slack form", "v::skin-tone-3", CDNBaseURL + "270c-1f3fc.png"},
		// A tone per person, as Slack names it ("::skin-tone-A-B").
		// These images can't be composed: the toned handshake is
		// 1FAF1 and 1FAF2, not 1F91D.
		{"handshake two tones", "handshake::skin-tone-2-5",
			CDNBaseURL + "1faf1-1f3fb-200d-1faf2-1f3fe.png"},
		{"people holding hands two tones", "people_holding_hands::skin-tone-2-6",
			CDNBaseURL + "1f9d1-1f3fb-200d-1f91d-200d-1f9d1-1f3ff.png"},
		{"people holding hands same tone", "people_holding_hands::skin-tone-2-2",
			CDNBaseURL + "1f9d1-1f3fb-200d-1f91d-200d-1f9d1-1f3fb.png"},
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

func TestURLForShortcode_TonePerPersonMisses(t *testing.T) {
	for _, in := range []string{
		"thumbsup::skin-tone-2-5",  // one person: no such image
		"handshake::skin-tone-2-2", // Slack sends this as skin-tone-2
		"handshake::skin-tone-2-7",
		"handshake::skin-tone-1-5",
	} {
		if got, ok := URLForShortcode(in, nil); ok {
			t.Errorf("URLForShortcode(%q) = %q, want no URL", in, got)
		}
	}
}

// TestURLForShortcode_EveryTonePerPersonImage: every entry of the
// generated table is reachable from a Slack name, and its image carries
// the entry's two tones in order. Guards the next regeneration.
func TestURLForShortcode_EveryTonePerPersonImage(t *testing.T) {
	names := map[string]string{}
	for code, glyph := range slackCodeMap {
		name := strings.Trim(code, ":")
		if prev, ok := names[glyph]; !ok || name < prev {
			names[glyph] = name
		}
	}
	if len(slackTonePerPersonImages) == 0 {
		t.Fatal("slackTonePerPersonImages is empty")
	}
	for key, stem := range slackTonePerPersonImages {
		runes := []rune(key)
		base, first, second := string(runes[:len(runes)-2]), runes[len(runes)-2], runes[len(runes)-1]
		name, ok := names[base]
		if !ok {
			t.Errorf("%s (%s): no Slack name has its base glyph", stem, key)
			continue
		}
		a, _ := SlackSkinToneNumber(first)
		b, _ := SlackSkinToneNumber(second)
		in := fmt.Sprintf("%s::skin-tone-%d-%d", name, a, b)
		if got, ok := URLForShortcode(in, nil); !ok || got != CDNBaseURL+stem+".png" {
			t.Errorf("URLForShortcode(%q) = (%q, %v), want %q", in, got, ok, CDNBaseURL+stem+".png")
		}
		var tones []rune
		for _, cp := range strings.Split(stem, "-") {
			if n, err := strconv.ParseInt(cp, 16, 32); err == nil {
				if _, ok := SlackSkinToneNumber(rune(n)); ok {
					tones = append(tones, rune(n))
				}
			}
		}
		if !runesEqual(tones, []rune{first, second}) {
			t.Errorf("%s: tones %U, want %U %U", stem, tones, first, second)
		}
	}
}
