package avatar

import "testing"

func TestSizedURL(t *testing.T) {
	const s3 = "https://s3-us-west-2.amazonaws.com/slack-files2/avatars/"
	const edge = "https://avatars.slack-edge.com/"
	cases := []struct {
		name string
		in   string
		px   int
		want string
	}{
		// Rewritten.
		{"s3 jpg", s3 + "2023-05-31/5349743656757_da76a902a76dda11cdbb_original.jpg", 72,
			edge + "2023-05-31/5349743656757_da76a902a76dda11cdbb_72.jpg"},
		{"s3 png keeps png", s3 + "2026-04-05/10849023461394_23b3ca254f8d12a7cc34_original.png", 72,
			edge + "2026-04-05/10849023461394_23b3ca254f8d12a7cc34_72.png"},
		{"s3 other region", "https://s3-eu-west-1.amazonaws.com/slack-files2/avatars/2024-01-02/1_ab_original.jpg", 72,
			edge + "2024-01-02/1_ab_72.jpg"},
		{"edge gif", edge + "2024-01-02/111_abc_original.gif", 72,
			edge + "2024-01-02/111_abc_72.gif"},
		{"uppercase ext kept verbatim", s3 + "2024-01-02/1_ab_original.JPG", 72,
			edge + "2024-01-02/1_ab_72.JPG"},
		{"other px", s3 + "2024-01-02/1_ab_original.png", 48,
			edge + "2024-01-02/1_ab_48.png"},

		// Unchanged.
		{"already sized", edge + "2026-04-08/10866830453622_8c4c7dfa336e8ea7c80b_32.png", 72,
			edge + "2026-04-08/10866830453622_8c4c7dfa336e8ea7c80b_32.png"},
		{"gravatar", "https://secure.gravatar.com/avatar/abc123?s=32&d=https%3A%2F%2Fa.slack-edge.com%2Fdf10d%2Fimg%2Favatars%2Fava_0001-32.png", 72,
			"https://secure.gravatar.com/avatar/abc123?s=32&d=https%3A%2F%2Fa.slack-edge.com%2Fdf10d%2Fimg%2Favatars%2Fava_0001-32.png"},
		{"query string", s3 + "2024-01-02/1_ab_original.jpg?x=1", 72,
			s3 + "2024-01-02/1_ab_original.jpg?x=1"},
		{"fragment", s3 + "2024-01-02/1_ab_original.jpg#f", 72,
			s3 + "2024-01-02/1_ab_original.jpg#f"},
		{"lookalike host", "https://avatars.slack-edge.com.evil.example/2024-01-02/1_ab_original.png", 72,
			"https://avatars.slack-edge.com.evil.example/2024-01-02/1_ab_original.png"},
		{"lookalike s3 host", "https://s3-us-west-2.amazonaws.com.evil.example/slack-files2/avatars/2024-01-02/1_ab_original.png", 72,
			"https://s3-us-west-2.amazonaws.com.evil.example/slack-files2/avatars/2024-01-02/1_ab_original.png"},
		{"missing date", edge + "1_ab_original.png", 72,
			edge + "1_ab_original.png"},
		{"http scheme", "http://avatars.slack-edge.com/2024-01-02/1_ab_original.png", 72,
			"http://avatars.slack-edge.com/2024-01-02/1_ab_original.png"},
		{"empty", "", 72, ""},
		{"zero px", s3 + "2024-01-02/1_ab_original.png", 0,
			s3 + "2024-01-02/1_ab_original.png"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SizedURL(tc.in, tc.px); got != tc.want {
				t.Errorf("SizedURL(%q, %d)\n got %q\nwant %q", tc.in, tc.px, got, tc.want)
			}
		})
	}
}
