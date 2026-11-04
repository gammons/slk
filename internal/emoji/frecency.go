package emoji

import (
	"regexp"
	"sort"
	"strings"

	slktext "github.com/gammons/slk/internal/text"
)

// Frecency ranks the emoji the user has used, most frecent first (the
// order the frecent_emoji cache table returns them in: use count
// discounted by days since last use). Names are as recorded, which is
// Slack's canonical short name (CanonicalSlackName). The zero value
// ranks nothing.
type Frecency struct {
	rank map[string]int
}

// NewFrecency builds a Frecency from names in most-to-least frecent
// order. A repeated name keeps its first (better) rank. A skin-toned
// name ("+1::skin-tone-4") also ranks its base emoji ("+1") unless the
// base ranks better on its own: the pickers list base names only, so
// that is the row a toned use can lift. Only Slack's "::skin-tone-N"
// form counts as toned, the form CanonicalSlackName records; a name
// ending "_tone1" may be a custom emoji of its own.
func NewFrecency(names []string) Frecency {
	f := Frecency{rank: make(map[string]int, len(names))}
	for i, n := range names {
		keys := []string{n}
		if j := strings.Index(n, "::skin-tone-"); j > 0 {
			keys = append(keys, n[:j])
		}
		for _, k := range keys {
			if _, dup := f.rank[k]; !dup && k != "" {
				f.rank[k] = i
			}
		}
	}
	return f
}

// Len is the number of ranked emoji.
func (f Frecency) Len() int { return len(f.rank) }

// Rank returns name's position in the history (0 is the most frecent)
// and whether it is there at all. name may be any alias or skin-tone
// spelling of a recorded emoji ("thumbsup" finds a recorded "+1",
// "+1_tone2" a recorded "+1::skin-tone-3"); customs is the workspace's
// custom emoji map, which shadows standard names as it does in
// CanonicalSlackName.
func (f Frecency) Rank(name string, customs map[string]string) (int, bool) {
	if len(f.rank) == 0 {
		return 0, false
	}
	if r, ok := f.rank[name]; ok {
		return r, true
	}
	r, ok := f.rank[CanonicalSlackName(name, customs)]
	return r, ok
}

// MatchOptions tunes Match for one picker.
type MatchOptions struct {
	// Substring also matches names that contain the query, after
	// every name that starts with it.
	Substring bool
	// Limit caps the result; 0 means no cap.
	Limit int
	// Frecency puts the emoji the user uses first.
	Frecency Frecency
	// Customs is the workspace custom emoji map, for Frecency.Rank.
	Customs map[string]string
}

// Match returns the entries whose name matches query (case- and
// accent-insensitive, via text.Fold), in the order an emoji picker
// shows them:
//
//  1. emoji the user has used whose name starts with query, most
//     frecent first;
//  2. with Substring, used emoji whose name contains query, most
//     frecent first;
//  3. every other name that starts with query, in entries' order;
//  4. with Substring, every other name that contains query.
//
// A used emoji appears once even when several of its aliases match
// (the first match in that order wins), so "+1" and "thumbsup" do not
// fill two rows. An empty query matches everything. entries is
// expected in alphabetical order (BuildEntries' order); name reads an
// entry's emoji name.
func Match[E any](entries []E, name func(E) string, query string, opt MatchOptions) []E {
	q := slktext.Fold(query)
	type used struct {
		entry     E
		substring bool
		rank      int
		pos       int
	}
	var usedHits []used
	var prefix, substring []E
	limit := opt.Limit
	for i, e := range entries {
		n := name(e)
		folded := slktext.Fold(n)
		isPrefix := strings.HasPrefix(folded, q)
		if !isPrefix && (!opt.Substring || !strings.Contains(folded, q)) {
			continue
		}
		if rank, ok := opt.Frecency.Rank(n, opt.Customs); ok {
			usedHits = append(usedHits, used{entry: e, substring: !isPrefix, rank: rank, pos: i})
			continue
		}
		// The unused tiers only ever show their first limit entries.
		if isPrefix && (limit == 0 || len(prefix) < limit) {
			prefix = append(prefix, e)
		} else if !isPrefix && (limit == 0 || len(substring) < limit) {
			substring = append(substring, e)
		}
	}

	sort.Slice(usedHits, func(i, j int) bool {
		a, b := usedHits[i], usedHits[j]
		if a.substring != b.substring {
			return !a.substring
		}
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		return a.pos < b.pos
	})
	out := make([]E, 0, len(usedHits)+len(prefix)+len(substring))
	seen := make(map[int]bool, len(usedHits))
	for _, h := range usedHits {
		if seen[h.rank] {
			continue
		}
		seen[h.rank] = true
		out = append(out, h.entry)
	}
	out = append(out, prefix...)
	out = append(out, substring...)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// usedShortcodeRe matches one :shortcode: in typed text, with Slack's
// "::skin-tone-N" suffix when it has one (so ":+1::skin-tone-2:" is one
// emoji, not ":+1:" then ":skin-tone-2:").
var usedShortcodeRe = regexp.MustCompile(`:([A-Za-z0-9_+\-]+(?:::skin-tone-[1-6])?):`)

// UsedInText returns the emoji s uses as :shortcode:s, under the name
// the history records them by (CanonicalSlackName), each once, in
// order of first use. Shortcodes that name no emoji, built-in or in
// customs (the ":30:" of "12:30:45"), are skipped. s is the text as
// typed; a shortcode inside a code span counts too.
func UsedInText(s string, customs map[string]string) []string {
	var out []string
	seen := map[string]bool{}
	for i := 0; i < len(s); {
		m := usedShortcodeRe.FindStringSubmatchIndex(s[i:])
		if m == nil {
			break
		}
		end := i + m[1]
		name := s[i+m[2] : i+m[3]]
		_, custom := customs[name]
		if _, builtin := slackCodeMap[":"+StripSkinTone(name)+":"]; !custom && !builtin {
			// Not an emoji: its closing colon may open the next one,
			// as in "12:30:+1::skin-tone-5:".
			i = end - 1
			continue
		}
		i = end
		name = CanonicalSlackName(name, customs)
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}
