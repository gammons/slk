package config

import "testing"

func TestIsTeamIDKey(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"T01ABCDEF", true},
		{"E0123456", true},
		{"work", false},
		{"acme-corp", false},
		{"T01", false},       // only 2 chars after T; regex requires 6+
		{"T0", false},        // too short
		{"t01abcdef", false}, // lowercase not a Slack team ID
		{"", false},
	}
	for _, tc := range cases {
		if got := isTeamIDKey(tc.key); got != tc.want {
			t.Errorf("isTeamIDKey(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

func TestSlugify(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Acme Inc.", "acme-inc"},
		{"ACME", "acme"},
		{"  hello  world  ", "hello-world"},
		{"foo/bar_baz", "foo-bar-baz"},
		{"---trim---", "trim"},
		{"", ""},
		{"!!!", ""},
	}
	for _, tc := range cases {
		if got := Slugify(tc.in); got != tc.want {
			t.Errorf("Slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
