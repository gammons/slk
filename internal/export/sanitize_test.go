package export

import "testing"

func TestSanitizeForFilename(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"ascii", "general", "general"},
		{"with-hyphen", "my-channel", "my-channel"},
		{"with-spaces", "my channel", "my-channel"},
		{"accented", "café", "café"},
		{"japanese", "日本語", "日本語"},
		{"cyrillic", "общий", "общий"},
		{"mixed", "café-général", "café-général"},
		{"all-special", "!@#$%", "unknown"},
		{"empty", "", "unknown"},
		{"underscores", "my_channel", "my_channel"},
		{"consecutive-special", "a!!b", "a-b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeForFilename(tc.in)
			if got != tc.want {
				t.Errorf("sanitizeForFilename(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
