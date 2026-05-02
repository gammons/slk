// Package mrkdwn translates CommonMark-style markdown in the compose
// box into Slack's wire formats: a mrkdwn fallback string (for the
// chat.postMessage `text` field and notifications) and a rich_text
// block (for the `blocks` array). The single entry point is Convert.
//
// The package preserves Slack wire-form tokens (<@U123>, <#C123|name>,
// <!here>, <https://...|label>) as opaque atoms so they don't get
// mangled by the CommonMark parser; they become typed elements (user,
// channel, broadcast, link) in the rich_text block.
package mrkdwn
