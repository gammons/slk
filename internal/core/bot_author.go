package core

import "strings"

// BotAuthorID is the ID a bot message with no user is attributed to:
// its bot_id, followed by "/" and the username the message was posted
// under when it has one. An incoming webhook posts each message under
// its own username, so keying on the bot_id alone gave every sender of
// one webhook the first sender's name and avatar. Slack IDs never
// contain "/".
func BotAuthorID(botID, username string) string {
	if botID == "" || username == "" {
		return botID
	}
	return botID + "/" + username
}

// AuthorBotID returns the bot_id part of an author ID made by
// BotAuthorID, and any other author ID unchanged.
func AuthorBotID(authorID string) string {
	botID, _, _ := strings.Cut(authorID, "/")
	return botID
}
