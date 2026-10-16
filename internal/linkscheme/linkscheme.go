// Package linkscheme holds the one list of URL schemes slk treats as a
// clickable link inside Slack's <url> and <url|label> forms.
//
// The list is an explicit allowlist, never "any scheme": javascript:, data:
// and vbscript: must not become links. It also keeps Slack's own
// angle-bracket forms (<#C…|name>, <@U…>, <!…>) from matching a link regex,
// since none of them starts with one of these schemes.
package linkscheme

// Alternation is a regexp alternation (without a surrounding group) that
// matches the start of an allowed link target. Embed it as
// `(?:` + linkscheme.Alternation + `)`.
const Alternation = `https?://|ftps?://|sftp://|ssh://|file:|mailto:|tel:`
