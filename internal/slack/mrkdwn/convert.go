package mrkdwn

import (
	"strings"

	"github.com/slack-go/slack"
)

// Convert parses mrkdwn in input and returns slack's RichTextBlock,
// ready for sending.
//
// input may contain Slack wire-form tokens (<@U…>, <#C…|name>, <!here>,
// <https://…|label>); these become typed elements (user, channel, broadcast, link)
// in the block.
//
// For empty / whitespace-only input, returns nil.
func Convert(input string) *slack.RichTextBlock {
	if strings.TrimSpace(input) == "" {
		return nil
	}

	// N.B: for now we always parse with the angled tokens.
	//
	// In the future, it would be architecturally more sound to move the mention
	// substitution logic from the compose to here, so that e.g. a user typing <!here>
	// into the compose box is kept verbatim, same as upstream Slack.
	block := parseBlock(input, true)

	if len(block.Elements) == 0 {
		return nil
	}
	return block
}
