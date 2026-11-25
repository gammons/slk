package messages

import (
	"testing"

	emojiutil "github.com/gammons/slk/internal/emoji"
)

// A message carrying a codepoint-form name in its text must be
// recognized as using that emoji's image, or the cold fetch that fills
// the slot would never trigger a redraw.
func TestMessageUsesEmojiURL_CodepointFormInText(t *testing.T) {
	url, ok := emojiutil.URLForShortcode("1f44d", nil)
	if !ok {
		t.Fatal("no standard URL for 1f44d")
	}
	msg := MessageItem{Text: "thanks :1f44d:"}
	if !messageUsesEmojiURL(msg, nil, nil, url) {
		t.Error("the message's :1f44d: did not match its image")
	}
}

func TestMessageUsesEmojiURL_OrdinaryTextDoesNotMatch(t *testing.T) {
	url, ok := emojiutil.URLForShortcode("1f44d", nil)
	if !ok {
		t.Fatal("no standard URL for 1f44d")
	}
	msg := MessageItem{Text: "meet at 12:30:45 for :cafe:"}
	if messageUsesEmojiURL(msg, nil, nil, url) {
		t.Error("ordinary text matched the thumbsup image")
	}
}

// A workspace custom emoji named "heart" must not make a message
// written with the standard codepoint name look like it draws the
// custom image, or the reverse.
func TestMessageUsesEmojiURL_CodepointNameDespiteCustomAlias(t *testing.T) {
	stdHeart, ok := emojiutil.URLForShortcode("2764", nil)
	if !ok {
		t.Fatal("no standard URL for 2764")
	}
	customs := map[string]string{"heart": "https://e/custom-heart.png"}
	msg := MessageItem{Text: "love :2764:"}
	if !messageUsesEmojiURL(msg, nil, customs, stdHeart) {
		t.Error("the standard heart did not match its own image")
	}
	if messageUsesEmojiURL(msg, nil, customs, customs["heart"]) {
		t.Error("the codepoint name matched the custom heart image")
	}
	customMsg := MessageItem{Text: "love :heart:"}
	if !messageUsesEmojiURL(customMsg, nil, customs, customs["heart"]) {
		t.Error("the custom heart did not match its own image")
	}
}
