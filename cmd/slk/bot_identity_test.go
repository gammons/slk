package main

import (
	"testing"

	"github.com/slack-go/slack"
)

// TestMessageAuthorBotIdentity is the headline of the bot-avatar fix: a
// bot_message (no user, only bot_id + username) is keyed on the bot_id and
// uses the message's username, so a name and avatar can attach — while
// human messages keep resolving by user ID.
func TestMessageAuthorBotIdentity(t *testing.T) {
	userNames := newUserNameStore(nil)

	// Bot message: empty user, bot_id + username present.
	bot := slack.Message{Msg: slack.Msg{User: "", BotID: "B123", Username: "Deploybot"}}
	uid, name := messageAuthor(bot, userNames, nil, nil)
	if uid != "B123" {
		t.Errorf("bot author ID: want B123, got %q", uid)
	}
	if name != "Deploybot" {
		t.Errorf("bot author name: want Deploybot, got %q", name)
	}

	// Human message: user present, name pre-cached (so no db/resolver needed).
	userNames.Set("U1", "Alice")
	human := slack.Message{Msg: slack.Msg{User: "U1"}}
	uid2, name2 := messageAuthor(human, userNames, nil, nil)
	if uid2 != "U1" {
		t.Errorf("human author ID: want U1, got %q", uid2)
	}
	if name2 != "Alice" {
		t.Errorf("human author name: want Alice, got %q", name2)
	}
}
