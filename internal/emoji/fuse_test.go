package emoji

import "testing"

func TestFuseModifierSequences(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// Empty/short — pass through.
		{"empty", "", ""},
		{"short", "ab", "ab"},

		// kyokomi output for :+1::skin-tone-2: — fuse the space.
		{
			name: "thumbs up + skin tone 1",
			in:   "\U0001F44D \U0001F3FB", // 👍 🏻
			want: "\U0001F44D\U0001F3FB",   // 👍🏻
		},
		{
			name: "wave + skin tone 5",
			in:   "\U0001F44B \U0001F3FF", // 👋 🏿
			want: "\U0001F44B\U0001F3FF",   // 👋🏿
		},

		// Trailing kyokomi padding preserved.
		{
			name: "thumbs up + skin tone + trailing space",
			in:   "\U0001F44D \U0001F3FB ", // 👍 🏻 (trailing space)
			want: "\U0001F44D\U0001F3FB ",   // 👍🏻 (trailing space kept)
		},

		// Already fused — no change.
		{
			name: "already fused",
			in:   "\U0001F44D\U0001F3FB",
			want: "\U0001F44D\U0001F3FB",
		},

		// Space not after an emoji — preserve.
		{
			name: "space then modifier alone",
			in:   " \U0001F3FB",
			want: " \U0001F3FB",
		},
		{
			name: "ASCII char then space then modifier",
			in:   "a \U0001F3FB",
			want: "a \U0001F3FB",
		},

		// Pill pattern: emoji + space + modifier + space + count.
		{
			name: "pill with count",
			in:   "\U0001F44D \U0001F3FB 1",
			want: "\U0001F44D\U0001F3FB 1",
		},

		// Multiple modifier sequences in one string.
		{
			name: "two pills",
			in:   "\U0001F44D \U0001F3FB 1 \U0001F44B \U0001F3FF 2",
			want: "\U0001F44D\U0001F3FB 1 \U0001F44B\U0001F3FF 2",
		},

		// Plain text — no change.
		{
			name: "plain text",
			in:   "hello world",
			want: "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FuseModifierSequences(tt.in)
			if got != tt.want {
				t.Errorf("FuseModifierSequences(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
