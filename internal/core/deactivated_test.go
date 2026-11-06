package core

import "testing"

func TestIsHiddenPeerConversation(t *testing.T) {
	hidden := map[string]bool{"U1": true}
	for _, tc := range []struct {
		name, convType, peer string
		hidden               map[string]bool
		want                 bool
	}{
		{"dm with a hidden user", "dm", "U1", hidden, true},
		{"app dm with a hidden user", "app", "U1", hidden, true},
		{"dm with someone else", "dm", "U2", hidden, false},
		{"group dm has no single peer", "group_dm", "U1", hidden, false},
		{"channel", "channel", "U1", hidden, false},
		{"no peer", "dm", "", map[string]bool{"": true}, false},
		{"nothing hidden", "dm", "U1", nil, false},
	} {
		if got := IsHiddenPeerConversation(tc.convType, tc.peer, tc.hidden); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
