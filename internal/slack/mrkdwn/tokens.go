package mrkdwn

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Sentinel runes from the Unicode private-use area. Guaranteed not to
// collide with real text and treated as opaque characters by goldmark.
const (
	sentinelStart = '\uE000'
	sentinelEnd   = '\uE001'
)

type tokenKind int

const (
	tokUser tokenKind = iota
	tokChannel
	tokBroadcast
	tokLink
)

// token holds the original wire-form payload for one <...> match.
//
// For tokUser:      id = "U12345",        label = ""
// For tokChannel:   id = "C12345",        label = "general" (may be "")
// For tokBroadcast: id = "here" / "channel" / "subteam^S01",
//
//	label = "" or "@team" (only for subteam form)
//
// For tokLink:      id = "https://x.com", label = "Slack" (may be "")
type token struct {
	kind  tokenKind
	id    string
	label string
}

// Patterns are tried in order; first match wins. None overlap given
// their leading characters (<@, <#, <!, <h).
var (
	reUser      = regexp.MustCompile(`<@([UW][A-Z0-9]+)>`)
	reChannel   = regexp.MustCompile(`<#([CG][A-Z0-9]+)(?:\|([^>]*))?>`)
	reBroadcast = regexp.MustCompile(`<!([a-z]+(?:\^[A-Za-z0-9]+)?)(?:\|([^>]*))?>`)
	reLink      = regexp.MustCompile(`<(https?://[^|>]+)(?:\|([^>]*))?>`)
)

// tokenize replaces all Slack wire-form tokens in s with sentinel
// markers and returns the rewritten string plus an ordered table.
// The marker for table index N is the three-rune sequence
// sentinelStart, decimal digits of N, sentinelEnd.
func tokenize(s string) (string, []token) {
	var table []token
	var b strings.Builder
	b.Grow(len(s))

	i := 0
	for i < len(s) {
		// Fast path: skip ahead until we see '<'.
		j := strings.IndexByte(s[i:], '<')
		if j < 0 {
			b.WriteString(s[i:])
			break
		}
		b.WriteString(s[i : i+j])
		i += j

		// Try each pattern at position i.
		matched := false
		for _, p := range []struct {
			re   *regexp.Regexp
			kind tokenKind
		}{
			{reUser, tokUser},
			{reChannel, tokChannel},
			{reBroadcast, tokBroadcast},
			{reLink, tokLink},
		} {
			loc := p.re.FindStringSubmatchIndex(s[i:])
			if loc == nil || loc[0] != 0 {
				continue
			}
			// loc is relative to s[i:].
			full := s[i : i+loc[1]]
			tok := token{kind: p.kind}
			tok.id = s[i+loc[2] : i+loc[3]]
			// Optional label group only exists for patterns with 2 captures.
			if len(loc) >= 6 && loc[4] >= 0 {
				tok.label = s[i+loc[4] : i+loc[5]]
			}
			table = append(table, tok)
			b.WriteRune(sentinelStart)
			b.WriteString(strconv.Itoa(len(table) - 1))
			b.WriteRune(sentinelEnd)
			i += len(full)
			matched = true
			break
		}
		if !matched {
			// Not a recognised Slack token; leave the '<' in place.
			b.WriteByte('<')
			i++
		}
	}

	return b.String(), table
}

// parseSentinel inspects s starting at byte offset start. If a
// sentinel-wrapped numeric index lives there, returns (index,
// end-byte-offset, true). The end offset points one byte past the
// closing sentinel rune.
func parseSentinel(s string, start int) (int, int, bool) {
	if start >= len(s) {
		return 0, 0, false
	}
	r, sz := utf8.DecodeRuneInString(s[start:])
	if r != sentinelStart {
		return 0, 0, false
	}
	digitStart := start + sz
	pos := digitStart
	for pos < len(s) {
		c := s[pos] // digits are ASCII, byte-level check is safe
		if c >= '0' && c <= '9' {
			pos++
			continue
		}
		break
	}
	if pos == digitStart {
		return 0, 0, false
	}
	if pos >= len(s) {
		return 0, 0, false
	}
	r, sz = utf8.DecodeRuneInString(s[pos:])
	if r != sentinelEnd {
		return 0, 0, false
	}
	idx, err := strconv.Atoi(s[digitStart:pos])
	if err != nil {
		return 0, 0, false
	}
	return idx, pos + sz, true
}

// detokenizeText restores all sentinel markers in s back to their
// original Slack wire-form tokens. Used to build the mrkdwn fallback
// (where mentions stay as <@U123>).
func detokenizeText(s string, table []token) string {
	if len(table) == 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		idx, end, ok := parseSentinel(s, i)
		if !ok {
			r, sz := utf8.DecodeRuneInString(s[i:])
			if sz == 0 {
				break
			}
			b.WriteRune(r)
			i += sz
			continue
		}
		if idx < 0 || idx >= len(table) {
			// Index out of range — emit raw bytes literally as a
			// safety fallback. Should never happen in practice.
			b.WriteString(s[i:end])
			i = end
			continue
		}
		b.WriteString(wireForm(table[idx]))
		i = end
	}
	return b.String()
}

// wireForm reconstructs the original <...> Slack wire token.
func wireForm(t token) string {
	switch t.kind {
	case tokUser:
		return "<@" + t.id + ">"
	case tokChannel:
		if t.label == "" {
			return "<#" + t.id + ">"
		}
		return "<#" + t.id + "|" + t.label + ">"
	case tokBroadcast:
		if t.label == "" {
			return "<!" + t.id + ">"
		}
		return "<!" + t.id + "|" + t.label + ">"
	case tokLink:
		if t.label == "" {
			return "<" + t.id + ">"
		}
		return "<" + t.id + "|" + t.label + ">"
	}
	return ""
}
