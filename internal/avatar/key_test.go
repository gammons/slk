package avatar

import (
	"strings"
	"testing"
)

// A bot author ID carries a "/" (core.BotAuthorID). The disk cache uses
// the key as a file name, so the key must not contain one, and a Slack
// ID must keep its old key.
func TestAvatarKeyEscapesBotAuthorIDs(t *testing.T) {
	if k := fetchKey("B1/Alice", "https://example.com/a.png"); strings.Contains(k, "/") {
		t.Errorf("fetchKey(B1/Alice) = %q, has a slash", k)
	}
	if k := avatarKey("U123"); k != "avatar-U123" {
		t.Errorf("avatarKey(U123) = %q, want avatar-U123", k)
	}
	if avatarKey("B1/Alice") == avatarKey("B1/Bob") {
		t.Error("two senders share an avatar key")
	}
}
