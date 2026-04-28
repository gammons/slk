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
