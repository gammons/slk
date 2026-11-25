package emoji

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// CodepointsFromName resolves Slack's codepoint-form name for a
// standard emoji: lowercase hex codepoints joined by "-". Some clients
// post reactions under this name instead of the shortcode, and
// integrations write it into text: "1f44d" is :thumbsup:, and
// "1f44d-1f3fd" is its medium tone.
//
// ok is false unless every part is 1-6 lowercase hex digits naming a
// Unicode scalar value, at most 10 parts, and the decoded sequence is
// an emoji slk knows. Uppercase is rejected: Slack only sends the
// lowercase form, and accepting it would make ordinary words of a-f
// hex digits resolve to unwanted glyphs. The emoji check is what keeps
// ":cafe:" and the ":30:" inside "12:30:45" from becoming 쫾 and "0".
func CodepointsFromName(name string) ([]rune, bool) {
	if name == "" {
		return nil, false
	}
	parts := strings.Split(name, "-")
	if len(parts) > 10 {
		return nil, false
	}
	cps := make([]rune, 0, len(parts))
	for _, p := range parts {
		if len(p) == 0 || len(p) > 6 {
			return nil, false
		}
		for i := 0; i < len(p); i++ {
			c := p[i]
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return nil, false
			}
		}
		n, err := strconv.ParseUint(p, 16, 32)
		if err != nil || n == 0 || n > 0x10FFFF || (n >= 0xD800 && n <= 0xDFFF) {
			return nil, false
		}
		cps = append(cps, rune(n))
	}
	if !isEmojiSequence(cps) {
		return nil, false
	}
	return cps, true
}

// isEmojiSequence reports whether cps is a sequence slk knows to be an
// emoji: a single known cluster (including flags and keycaps), or a
// known base followed by emoji scalars and modifiers (skin tones, VS16,
// ZWJ components, tags), which covers toned and joined sequences whose
// full form is not in the shortcode map.
func isEmojiSequence(cps []rune) bool {
	if len(cps) == 0 {
		return false
	}
	if isKnownEmojiCluster(string(cps)) {
		return true
	}
	if !isKnownEmojiScalar(cps[0]) {
		return false
	}
	for _, r := range cps[1:] {
		if !isKnownEmojiScalar(r) && !isEmojiModifier(r) {
			return false
		}
	}
	return true
}

// isKnownEmojiScalar reports whether r alone is a standard emoji, with
// or without its VS16 presentation selector.
func isKnownEmojiScalar(r rune) bool {
	return isKnownEmojiCluster(string(r)) || isKnownEmojiCluster(string([]rune{r, vs16}))
}

// isEmojiModifier reports whether r may follow an emoji base without
// naming another emoji: joiners, presentation selectors, keycap
// combiners, hair components, regional indicators and tag characters.
func isEmojiModifier(r rune) bool {
	switch {
	case r == 0x200D, r == 0xFE0E, r == 0xFE0F, r == 0x20E3:
		return true
	case r >= 0x1F3FB && r <= 0x1F3FF: // skin tones
		return true
	case r >= 0x1F1E6 && r <= 0x1F1FF: // regional indicators
		return true
	case r >= 0xE0020 && r <= 0xE007F: // tags
		return true
	case r >= 0x1F9B0 && r <= 0x1F9B3: // hair components
		return true
	}
	return false
}

// ShortcodeGlyph resolves a Slack-style shortcode name (no colons) to
// its glyph, without a trailing space: the standard shortcode map
// first, then a codepoint-form name such as "1f44d".
func ShortcodeGlyph(name string) (string, bool) {
	if g, ok := slackCodeMap[":"+name+":"]; ok {
		return strings.TrimRight(g, " "), true
	}
	if cps, ok := CodepointsFromName(name); ok {
		return string(cps), true
	}
	return "", false
}

// The standard emoji table has aliases for the same glyph. These maps
// pick one spelling per glyph so a codepoint-form name lands on the
// same history entry and image as its shortcode: the canonical
// shortcode name, and the glyph spelling whose codepoints Slack's CDN
// asset is named after (the map's value, so a needed VS16 is kept).
var (
	emojiNamingOnce          sync.Once
	canonicalNameByGlyph     map[string]string
	canonicalGlyphByStripped map[string]string
)

type emojiNaming struct{ name, glyph string }

func initEmojiNaming() {
	best := make(map[string]emojiNaming, 2048)
	for key, v := range slackCodeMap {
		glyph := strings.TrimRight(v, " ")
		if glyph == "" {
			continue
		}
		name := strings.Trim(key, ":")
		canonical := name
		if c, ok := slackNameToCanonical[name]; ok {
			canonical = c
		}
		stripped := stripVS16(glyph)
		cur, ok := best[stripped]
		// Deterministic tiebreak across the aliases sharing one glyph:
		// the lexicographically smaller canonical name wins, and between
		// its spellings the form that keeps VS16 (the one Slack names
		// the asset after) does.
		if !ok || canonical < cur.name || (canonical == cur.name && len(glyph) > len(cur.glyph)) {
			best[stripped] = emojiNaming{name: canonical, glyph: glyph}
		}
	}
	canonicalNameByGlyph = make(map[string]string, len(best))
	canonicalGlyphByStripped = make(map[string]string, len(best))
	for stripped, e := range best {
		canonicalNameByGlyph[stripped] = e.name
		canonicalGlyphByStripped[stripped] = e.glyph
	}
}

// canonicalGlyphFor returns the glyph spelling Slack's asset is named
// after for cps, "" when the sequence is not a known standard emoji.
func canonicalGlyphFor(cps []rune) string {
	emojiNamingOnce.Do(initEmojiNaming)
	return canonicalGlyphByStripped[stripVS16(string(cps))]
}

// canonicalShortcodeFor returns the canonical shortcode name for cps,
// false when the sequence is not a known standard emoji.
func canonicalShortcodeFor(cps []rune) (string, bool) {
	emojiNamingOnce.Do(initEmojiNaming)
	name, ok := canonicalNameByGlyph[stripVS16(string(cps))]
	return name, ok
}

// CodepointFormCanonicalName returns the picker's canonical shortcode
// for a codepoint-form name, so a use of ":1f44d:" ranks the "+1" row
// the way ":thumbsup:" does, and a toned sequence keeps its
// "::skin-tone-N" suffix. ok is false for any other name.
func CodepointFormCanonicalName(name string) (string, bool) {
	cps, ok := CodepointsFromName(name)
	if !ok {
		return "", false
	}
	if canonical, ok := canonicalShortcodeFor(cps); ok {
		return canonical, true
	}
	if len(cps) > 1 {
		if tone, ok := SlackSkinToneNumber(cps[len(cps)-1]); ok {
			if base, ok := canonicalShortcodeFor(cps[:len(cps)-1]); ok {
				return fmt.Sprintf("%s::skin-tone-%d", base, tone), true
			}
		}
	}
	return "", false
}
