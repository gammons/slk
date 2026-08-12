package mentionpicker

import (
	"strings"

	"github.com/gammons/slk/internal/text"
)

// matchRank describes how well a candidate name matches the query.
// Lower is better: the picker sorts by rank before name, so a whole-name
// prefix hit outranks a hit on a word in the middle of the name.
//
// The three matching modes mirror what Slack's own composer does — typing
// "widg" finds @eng-widgets, and so does "engwidgets".
type matchRank int

const (
	rankPrefix   matchRank = iota // query is a prefix of the whole name
	rankWord                      // query is a prefix of a word inside the name
	rankSquashed                  // query matches once separators are removed
	rankNone                      // no match
)

// isSeparator reports whether b separates words in a display name or
// handle. Slack handles allow "-", "_" and "."; display names add
// spaces. All are ASCII, so byte-level tests are safe on UTF-8 input —
// no continuation byte of a multi-byte rune can collide with them.
func isSeparator(b byte) bool {
	return b == ' ' || b == '-' || b == '_' || b == '.'
}

// squash removes every separator from s, so "eng-widgets" and
// "engwidgets" compare equal.
func squash(s string) string {
	if !strings.ContainsAny(s, " -_.") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if !isSeparator(s[i]) {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// matchName ranks name against an already-folded query. squashedQuery is
// the query with separators removed; the caller computes it once per
// keystroke rather than per candidate.
func matchName(name, query, squashedQuery string) matchRank {
	if query == "" {
		return rankPrefix
	}
	n := text.Fold(name)
	if strings.HasPrefix(n, query) {
		return rankPrefix
	}
	// Word-boundary prefix: "widg" matches "eng-widgets". Runs of
	// separators are handled naturally — each one starts a new word, and
	// an empty word can only match an empty query, which returned above.
	for i := 0; i < len(n); i++ {
		if isSeparator(n[i]) && strings.HasPrefix(n[i+1:], query) {
			return rankWord
		}
	}
	if squashedQuery != "" && strings.HasPrefix(squash(n), squashedQuery) {
		return rankSquashed
	}
	return rankNone
}

// rankUser returns the better of the user's display-name and username
// ranks.
func rankUser(u User, query, squashedQuery string) matchRank {
	r := matchName(u.DisplayName, query, squashedQuery)
	if ru := matchName(u.Username, query, squashedQuery); ru < r {
		r = ru
	}
	return r
}
