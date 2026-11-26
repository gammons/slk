package main

import (
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/slack-go/slack"
)

// TestMessageAuthorBotIdentity is the headline of the bot-avatar fix: a
// bot_message (no user, only bot_id + username) is keyed on the bot_id and
// its username and uses the message's username, so a name and avatar can
// attach — while human messages keep resolving by user ID.
func TestMessageAuthorBotIdentity(t *testing.T) {
	userNames := newUserNameStore(nil)

	// Bot message: empty user, bot_id + username present.
	bot := slack.Message{Msg: slack.Msg{User: "", BotID: "B123", Username: "Deploybot"}}
	uid, name := messageAuthor(bot, userNames, nil, nil)
	if uid != "B123/Deploybot" {
		t.Errorf("bot author ID: want B123/Deploybot, got %q", uid)
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

// TestMessageAuthorTellsWebhookSendersApart: an incoming webhook posts each
// message under its own username. Two senders of one bot_id get their own
// author ID and name, and a name cached for one is not used for the other.
func TestMessageAuthorTellsWebhookSendersApart(t *testing.T) {
	userNames := newUserNameStore(nil)
	userNames.Set("B1", "Alice") // a row from before senders were told apart

	alice := slack.Message{Msg: slack.Msg{BotID: "B1", Username: "Alice"}}
	bob := slack.Message{Msg: slack.Msg{BotID: "B1", Username: "Bob"}}
	uidA, nameA := messageAuthor(alice, userNames, nil, nil)
	uidB, nameB := messageAuthor(bob, userNames, nil, nil)
	if uidA != "B1/Alice" || nameA != "Alice" {
		t.Errorf("first sender = %q/%q, want B1/Alice and Alice", uidA, nameA)
	}
	if uidB != "B1/Bob" || nameB != "Bob" {
		t.Errorf("second sender = %q/%q, want B1/Bob and Bob", uidB, nameB)
	}

	h := &rtmEventHandler{}
	if got := h.messageAuthor("", "B1", "Bob"); got != "B1/Bob" {
		t.Errorf("WebSocket author ID = %q, want B1/Bob", got)
	}
	if got := h.messageAuthor("", "B1", ""); got != "B1" {
		t.Errorf("WebSocket author ID without a username = %q, want B1", got)
	}
}

// TestEnrichCachedRowRekeysBareBotRows: a row cached with the bare bot_id
// (before senders were told apart) is read back under the sender in its
// raw_json, not under the name cached for the bot_id.
func TestEnrichCachedRowRekeysBareBotRows(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertUser(cache.User{ID: "B1", WorkspaceID: "T1", Name: "Alice", DisplayName: "Alice", IsBot: true}); err != nil {
		t.Fatal(err)
	}
	row := cache.Message{TS: "1700000001.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "B1",
		Text: "hi", RawJSON: `{"type":"message","subtype":"bot_message","bot_id":"B1","username":"Bob","text":"hi"}`}
	item := enrichCachedRow(db, "USELF", "C1", row, newUserNameStore(nil), "3:04 PM", "test", nil, nil)
	if item.UserID != "B1/Bob" || item.UserName != "Bob" {
		t.Errorf("cached bot row = %q/%q, want B1/Bob and Bob", item.UserID, item.UserName)
	}
}

// TestEnrichCachedRowNamesSenderWithoutUserRow: a row cached under a
// sender's author ID whose bots.info lookup failed has no users row. It
// still shows the sender's name, not the raw author ID.
func TestEnrichCachedRowNamesSenderWithoutUserRow(t *testing.T) {
	db := newTestDB(t)
	row := cache.Message{TS: "1700000002.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "B1/Alice",
		Text: "hi", RawJSON: `{"type":"message","subtype":"bot_message","bot_id":"B1","username":"Alice","text":"hi"}`}
	item := enrichCachedRow(db, "USELF", "C1", row, newUserNameStore(nil), "3:04 PM", "test", nil, nil)
	if item.UserID != "B1/Alice" || item.UserName != "Alice" {
		t.Errorf("cached bot row = %q/%q, want B1/Alice and Alice", item.UserID, item.UserName)
	}
}
