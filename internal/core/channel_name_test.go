package core

import "testing"

func TestChannelNameMatcher(t *testing.T) {
	m := ChannelNameMatcher("#Général")
	for _, tc := range []struct {
		typ, name string
		want      bool
	}{
		{"channel", "general", true},
		{"private", "GENERAL", true},
		{"dm", "general", false},
		{"group_dm", "general", false},
		{"channel", "general-2", false},
	} {
		if got := m(tc.typ, tc.name); got != tc.want {
			t.Errorf("match(%q, %q) = %v, want %v", tc.typ, tc.name, got, tc.want)
		}
	}
}

// "#" alone, or nothing, is no name: it never matches, not even an
// unnamed row.
func TestChannelNameMatcherEmpty(t *testing.T) {
	for _, name := range []string{"", "#"} {
		if ChannelNameMatcher(name)("channel", "") {
			t.Errorf("ChannelNameMatcher(%q) matched an unnamed channel", name)
		}
	}
}
