package emoji

import "testing"

func TestStripSkinTone(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// Slack-style suffix.
		{"slack +1 tone 2", "+1::skin-tone-2", "+1"},
		{"slack thumbsup tone 5", "thumbsup::skin-tone-5", "thumbsup"},
		{"slack wave tone 3", "wave::skin-tone-3", "wave"},

		// kyokomi-style suffix.
		{"kyokomi thumbsup_tone1", "thumbsup_tone1", "thumbsup"},
		{"kyokomi wave_tone3", "wave_tone3", "wave"},
		{"kyokomi clap_tone5", "clap_tone5", "clap"},

		// No skin-tone suffix — passthrough.
		{"plain thumbsup", "thumbsup", "thumbsup"},
		{"plain +1", "+1", "+1"},
		{"plain wave", "wave", "wave"},
		{"plain heart", "heart", "heart"},
		{"empty", "", ""},

		// Custom emoji names that happen to look similar — must not strip.
		{"custom_emoji_named_tone6", "anything_tone6", "anything_tone6"},   // tone6 doesn't match 1-5
		{"custom my_tonemate", "my_tonemate", "my_tonemate"},                // doesn't match _toneN pattern exactly
		{"name with skin-tone in middle", "skin-tone-thing", "skin-tone-thing"}, // no "::" prefix

		// Edge: very short names.
		{"single char", "a", "a"},
		{"five chars", "abcde", "abcde"},

		// Slack-style as exact prefix-only is unusual; still pass through.
		{"slack form alone", "::skin-tone-2", ""}, // base becomes empty

		// Slack form first overrides if both somehow present.
		{"both forms", "thumbsup_tone1::skin-tone-2", "thumbsup_tone1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripSkinTone(tt.in)
			if got != tt.want {
				t.Errorf("StripSkinTone(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestStripSkinToneFromText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// kyokomi-style skin tones embedded in body text.
		{
			name: "single shortcode in sentence",
			in:   "I love :point_down_tone1: this thing!",
			want: "I love :point_down: this thing!",
		},
		{
			name: "thumbsup_tone3 alone",
			in:   ":thumbsup_tone3:",
			want: ":thumbsup:",
		},
		{
			name: "raised hand variant",
			in:   "see :raised_back_of_hand_tone5: there",
			want: "see :raised_back_of_hand: there",
		},

		// Slack-style suffix in body text.
		{
			name: "slack form in text",
			in:   ":+1::skin-tone-2: looks good",
			want: ":+1: looks good",
		},
		{
			name: "slack form thumbsup",
			in:   "great :thumbsup::skin-tone-5:",
			want: "great :thumbsup:",
		},

		// Multiple skin-toned shortcodes in one message.
		{
			name: "multiple",
			in:   ":wave_tone1: hi, :point_down_tone3: please :+1::skin-tone-4:",
			want: ":wave: hi, :point_down: please :+1:",
		},

		// Mixed plain and toned.
		{
			name: "mixed",
			in:   ":heart: vs :heart_tone1: vs :+1: vs :+1::skin-tone-2:",
			want: ":heart: vs :heart: vs :+1: vs :+1:",
		},

		// Plain text — no change.
		{
			name: "plain text no shortcodes",
			in:   "hello world, no emoji here",
			want: "hello world, no emoji here",
		},
		{
			name: "plain shortcodes",
			in:   "look at :thumbsup: and :wave:",
			want: "look at :thumbsup: and :wave:",
		},

		// Empty.
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripSkinToneFromText(tt.in)
			if got != tt.want {
				t.Errorf("StripSkinToneFromText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
