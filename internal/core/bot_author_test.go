package core

import "testing"

func TestBotAuthorID(t *testing.T) {
	tests := []struct{ botID, username, want string }{
		{"B1", "Alice", "B1/Alice"},
		{"B1", "", "B1"},
		{"", "Alice", ""},
	}
	for _, tt := range tests {
		got := BotAuthorID(tt.botID, tt.username)
		if got != tt.want {
			t.Errorf("BotAuthorID(%q, %q) = %q, want %q", tt.botID, tt.username, got, tt.want)
		}
		if back := AuthorBotID(got); back != tt.botID {
			t.Errorf("AuthorBotID(%q) = %q, want %q", got, back, tt.botID)
		}
	}
	if got := AuthorBotID("U1"); got != "U1" {
		t.Errorf("AuthorBotID(U1) = %q, want U1", got)
	}
}
