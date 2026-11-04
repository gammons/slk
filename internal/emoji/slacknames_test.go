package emoji

import "testing"

func TestCanonicalSlackName(t *testing.T) {
	// Workspace custom emoji: "thumbsup" shadows the standard alias of 👍;
	// "shipit" is a classic custom with no standard counterpart.
	customs := map[string]string{
		"thumbsup": "https://emoji.slack-edge.com/T0/thumbsup/abc.png",
		"shipit":   "https://emoji.slack-edge.com/T0/shipit/def.png",
	}
	tests := []struct {
		name    string // picker shortcode (no colons)
		customs map[string]string
		want    string // name Slack reactions.add accepts
	}{
		// Valid Slack aliases canonicalize to their primary short_name.
		{"thumbsup", nil, "+1"},
		{"+1", nil, "+1"},

		// Already-canonical names pass through.
		{"ok_hand", nil, "ok_hand"},
		{"joy", nil, "joy"},
		{"white_check_mark", nil, "white_check_mark"},
		{"tada", nil, "tada"},

		// Skin tones re-emit Slack's "::skin-tone-N" form on the canonical base.
		// kyokomi numbers tones 1-5 and Slack 2-6, so _tone3 is
		// skin-tone-4.
		{"thumbsup_tone3", nil, "+1::skin-tone-4"},
		{"thumbsup::skin-tone-2", nil, "+1::skin-tone-2"},
		{"+1::skin-tone-6", nil, "+1::skin-tone-6"},

		// Unknown names are sent verbatim.
		{"some_workspace_custom_emoji", nil, "some_workspace_custom_emoji"},

		// Custom emoji shadow standard names and MUST be sent verbatim,
		// even when the name is also a standard alias.
		{"thumbsup", customs, "thumbsup"},
		{"shipit", customs, "shipit"},
	}
	for _, tt := range tests {
		if got := CanonicalSlackName(tt.name, tt.customs); got != tt.want {
			t.Errorf("CanonicalSlackName(%q, customs=%v) = %q, want %q", tt.name, tt.customs != nil, got, tt.want)
		}
	}
}

// TestCodeMapParity sanity-checks that the iamcal-derived glyph table
// matches the glyphs slk previously got from kyokomi/emoji for a spread
// of emoji shapes (simple, VS16, keycap, ZWJ family, flag).
func TestCodeMapParity(t *testing.T) {
	want := map[string]string{
		":+1:":                 "👍",
		":heart:":              "❤️",
		":one:":                "1️⃣",
		":flag-fi:":            "🇫🇮",
		":man-woman-girl-boy:": "👨‍👩‍👧‍👦",
	}
	cm := CodeMap()
	for code, glyph := range want {
		if cm[code] != glyph {
			t.Errorf("CodeMap()[%q] = %q, want %q", code, cm[code], glyph)
		}
	}
}
