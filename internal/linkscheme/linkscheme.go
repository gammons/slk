// Package linkscheme holds the one list of URL schemes slk treats as a
// clickable link inside Slack's <url> and <url|label> forms.
//
// The list is an explicit allowlist, never "any scheme": javascript:, data:
// and vbscript: must not become links. It also keeps Slack's own
// angle-bracket forms (<#C…|name>, <@U…>, <!…>) from matching a link regex,
// since none of them starts with one of these schemes.
package linkscheme

import (
	"regexp"
	"strings"
)

// Prefixes lists the start of every allowed link target, the scheme and the
// punctuation that follows it. Scheme matching is case-sensitive.
var Prefixes = []string{
	"http://", "https://", "ftp://", "ftps://", "sftp://", "ssh://",
	"file:", "mailto:", "tel:",
}

// Alternation is a regexp alternation (without a surrounding group) that
// matches any of Prefixes. Embed it as `(?:` + linkscheme.Alternation + `)`.
var Alternation = func() string {
	quoted := make([]string, len(Prefixes))
	for i, p := range Prefixes {
		quoted[i] = regexp.QuoteMeta(p)
	}
	return strings.Join(quoted, "|")
}()

// HasPrefix reports whether s starts with one of Prefixes.
func HasPrefix(s string) bool {
	for _, p := range Prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
