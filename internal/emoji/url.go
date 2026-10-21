package emoji

import (
	"fmt"
	"strings"
)

// CDNBaseURL is the prefix Slack uses for its standard-emoji asset
// images. The "16.0" version segment changes when Slack ships a new
// emoji generation; if a future asset reorganization breaks our URLs,
// updating this constant is the single edit needed.
//
// "google-small" is Slack's Google-style emoji set at the smallest
// pre-rendered size (~16x16px). Workspace admins on Slack web can
// pick between Apple / Google / Twitter / Slack-classic; v1 of this
// renderer hardcodes the google set, matching the default for most
// workspaces. Per-workspace style detection is a follow-up.
const CDNBaseURL = "https://a.slack-edge.com/production-standard-emoji-assets/16.0/google-small/"

// vs16 is U+FE0F, the variation selector that requests emoji
// presentation for a base codepoint. Slack's CDN PRESERVES VS16 in
// URL paths (e.g., :heart: at U+2764 U+FE0F serves as
// "2764-fe0f.png", not "2764.png"). The earlier design doc
// hypothesized VS16 was stripped; empirical fetching against the CDN
// (a 403 for "2764.png" vs a 200 for "2764-fe0f.png") disproved
// that. ZWJ (U+200D) sequences and regional-indicator flag pairs
// are also preserved verbatim.
//
// The constant is retained because tokens.go uses it for scanning
// raw emoji-presentation sequences in message text.
const vs16 = '\uFE0F'

// BuildStandardEmojiURL returns the Slack CDN URL for a standard
// (kyokomi-known or Unicode-property-detected) emoji's codepoint
// sequence. Codepoints are lowercase-hex, dash-joined; ALL
// codepoints are preserved (including U+FE0F / VS16), matching
// Slack's CDN naming.
//
// Returns "" if codepoints is empty.
func BuildStandardEmojiURL(codepoints []rune) string {
	if len(codepoints) == 0 {
		return ""
	}
	parts := make([]string, 0, len(codepoints))
	for _, r := range codepoints {
		parts = append(parts, fmt.Sprintf("%x", r))
	}
	return CDNBaseURL + strings.Join(parts, "-") + ".png"
}

// CodepointsForShortcode resolves a Slack-style shortcode name (no
// colons) to its Unicode codepoint sequence using the kyokomi
// codemap. Returns (codepoints, true) on hit, (nil, false) on miss.
//
// Shortcodes that aren't in the kyokomi codemap (Slack-specific
// names, workspace customs) are not resolved here — call
// BuildCustomEmojiURL with the workspace customs map for those.
//
// VS16 and ZWJ are returned verbatim and preserved through URL
// construction (see BuildStandardEmojiURL).
func CodepointsForShortcode(name string) ([]rune, bool) {
	u, ok := slackCodeMap[":"+name+":"]
	if !ok {
		return nil, false
	}
	return []rune(strings.TrimRight(u, " ")), true
}

// customEmojiAliasPrefix marks a Slack custom-emoji alias entry
// (e.g., "alias:thumbsup"). Mirrors aliasPrefix in entries.go; kept
// separate so the URL-building code is self-contained at a glance.
//
// (Note: maxAliasHops is already declared in entries.go with the
// same value/semantic and is reused here rather than re-declared.)
const customEmojiAliasPrefix = "alias:"

// BuildCustomEmojiURL resolves a shortcode name to a URL using the
// workspace's customs map first, then (if the customs map has an
// "alias:<target>" entry) the kyokomi codemap for the target.
//
// Returns (url, true) on hit. Returns ("", false) when:
//   - name is not in customs and is not a kyokomi builtin reachable
//     through an alias entry
//   - the alias chain cycles or exceeds maxAliasHops
//
// Direct kyokomi-known names (no customs entry) are intentionally
// NOT resolved here — call CodepointsForShortcode +
// BuildStandardEmojiURL for those. This separation keeps each
// function single-purpose and lets callers fall back to glyph
// rendering at the right granularity when one path fails.
func BuildCustomEmojiURL(name string, customs map[string]string) (string, bool) {
	visited := make(map[string]struct{}, maxAliasHops)
	current := name
	for hop := 0; hop < maxAliasHops; hop++ {
		if _, seen := visited[current]; seen {
			return "", false // cycle
		}
		visited[current] = struct{}{}

		entry, ok := customs[current]
		if !ok {
			return "", false
		}
		if !strings.HasPrefix(entry, customEmojiAliasPrefix) {
			// Direct custom URL.
			return entry, true
		}
		target := strings.TrimPrefix(entry, customEmojiAliasPrefix)

		// If the alias target is a kyokomi builtin, resolve it now.
		// If it's another custom name, loop and follow.
		if cps, ok := CodepointsForShortcode(target); ok {
			return BuildStandardEmojiURL(cps), true
		}
		current = target
	}
	return "", false // exceeded max hops
}

// URLForShortcode resolves a Slack-style shortcode name (no colons)
// to a Slack CDN URL using the workspace customs map first
// (including alias resolution) and then the kyokomi codemap.
// Returns (url, true) on hit, ("", false) when both lookups miss
// or when an alias chain cycles.
//
// Workspace customs win over kyokomi for the same name — this
// matches the precedence already used in
// internal/emoji/entries.go:resolveCustomDisplay and the picker
// preview behavior.
func URLForShortcode(name string, customs map[string]string) (string, bool) {
	if u, ok := BuildCustomEmojiURL(name, customs); ok {
		return u, true
	}
	if cps, ok := CodepointsForShortcode(name); ok {
		return BuildStandardEmojiURL(cps), true
	}
	// Fallback for skin-toned variants that aren't pre-resolved in
	// kyokomi (e.g. "+1::skin-tone-3", "+1_tone3", or
	// "handshake::skin-tone-2-5").
	return skinTonedURL(name)
}

// skinTonedURL resolves a skin-toned name to its image on Slack's CDN:
// one tone is composed from the base emoji, a tone per person is
// looked up.
func skinTonedURL(name string) (string, bool) {
	if cps, ok := ComposeSkinTonedCodepoints(name); ok {
		return BuildStandardEmojiURL(cps), true
	}
	return tonePerPersonURL(name)
}

// tonePerPersonURL resolves Slack's name for a two-person emoji with a
// tone per person, "<base>::skin-tone-A-B" (A and B are 2-6 and may
// match), using the image table generated from iamcal.
func tonePerPersonURL(name string) (string, bool) {
	i := strings.Index(name, "::")
	if i < 0 {
		return "", false
	}
	first, second, ok := tonePerPerson(name[i+2:])
	if !ok {
		return "", false
	}
	baseCps, ok := CodepointsForShortcode(name[:i])
	if !ok {
		return "", false
	}
	stem, ok := slackTonePerPersonImages[string(baseCps)+string([]rune{first, second})]
	if !ok {
		return "", false
	}
	return CDNBaseURL + stem + ".png", true
}

// tonePerPerson parses Slack's "skin-tone-A-B" into the two people's
// modifier codepoints.
func tonePerPerson(tone string) (rune, rune, bool) {
	tail, ok := strings.CutPrefix(tone, "skin-tone-")
	if !ok || len(tail) != 3 || tail[1] != '-' {
		return 0, 0, false
	}
	first, ok1 := slackSkinTone(tail[0])
	second, ok2 := slackSkinTone(tail[2])
	return first, second, ok1 && ok2
}

// slackSkinTone maps the digit of Slack's "skin-tone-N" (2-6; 1 is the
// default yellow) to its modifier codepoint.
func slackSkinTone(digit byte) (rune, bool) {
	if digit < '2' || digit > '6' {
		return 0, false
	}
	return lightestSkinTone + rune(digit-'2'), true
}

// SlackSkinToneNumber is the inverse of slackSkinTone: Slack's number
// (2-6) for a skin tone modifier codepoint.
func SlackSkinToneNumber(modifier rune) (int, bool) {
	if modifier < lightestSkinTone || modifier > lightestSkinTone+4 {
		return 0, false
	}
	return int(modifier-lightestSkinTone) + 2, true
}

// lightestSkinTone is U+1F3FB, the lightest skin tone modifier; the
// other four follow it in order.
const lightestSkinTone rune = 0x1F3FB

// ComposeSkinTonedCodepoints decomposes a skin-toned shortcode name
// (either Slack's "::skin-tone-N" or kyokomi's "_toneN" form), looks
// up the base shortcode, and inserts the matching skin-tone modifier
// codepoint right after the base (first) codepoint.
//
// The skin-tone modifier applies to the base emoji — the first
// codepoint in the sequence. For simple emojis (e.g. 👍 = U+1F44D)
// this is equivalent to appending. For ZWJ sequences (e.g.
// 🏃‍➡️ = U+1F3C3 U+200D U+27A1 U+FE0F) the modifier MUST go
// after the base codepoint and BEFORE the ZWJ, producing
// "1f3c3-1f3fb-200d-27a1-fe0f" — NOT "1f3c3-200d-27a1-fe0f-1f3fb",
// which 404s on Slack's CDN. A VS16 right after the base is replaced
// by the modifier (✌️ 270C FE0F becomes 270C 1F3FB). With those two
// rules the result matches iamcal's image name for every single-tone
// variant in v16.0.0.
//
// Used by URLForShortcode as a fallback for skin-toned variants that
// aren't in the codemap (e.g. ":+1::skin-tone-4:" is not, but ":+1:"
// is, and combining its codepoint with U+1F3FD produces the correct
// asset URL).
//
// Returns (codepoints, true) on success. Returns (nil, false) when:
//   - name has no recognizable tone suffix
//   - the tone number is out of range
//   - the base shortcode isn't in the codemap
//   - the base emoji takes no skin tone (no toned image exists)
func ComposeSkinTonedCodepoints(name string) ([]rune, bool) {
	base, modifier, ok := splitSkinToneSuffix(name)
	if !ok {
		return nil, false
	}
	baseCps, ok := CodepointsForShortcode(base)
	if !ok {
		return nil, false
	}
	if _, ok := slackSkinToneBases[string(baseCps)]; !ok {
		return nil, false
	}
	tail := baseCps[1:]
	if len(tail) > 0 && tail[0] == vs16 {
		tail = tail[1:]
	}
	out := make([]rune, 0, len(tail)+2)
	out = append(out, baseCps[0], modifier)
	return append(out, tail...), true
}

// splitSkinToneSuffix splits a skin-toned shortcode name into the base
// name and its skin tone modifier codepoint. Returns ("", 0, false) if
// the name has no recognizable suffix.
//
// The two forms number the same five tones differently: Slack counts
// 2-6 (1 is the default yellow), kyokomi 1-5.
//
//	"+1::skin-tone-2"  → ("+1", U+1F3FB, true)        // Slack
//	"thumbsup_tone1"   → ("thumbsup", U+1F3FB, true)  // kyokomi
//
// Note: this overlaps in shape with StripSkinTone in fuse.go, which
// returns just the base name and is used by the legacy glyph-rendering
// fallback path. Different return shape, different purpose — keeping
// them separate is intentional.
func splitSkinToneSuffix(name string) (string, rune, bool) {
	// Slack form: "<base>::skin-tone-N".
	if i := strings.Index(name, "::skin-tone-"); i >= 0 {
		tail := name[i+len("::skin-tone-"):]
		if len(tail) == 1 {
			if modifier, ok := slackSkinTone(tail[0]); ok {
				return name[:i], modifier, true
			}
		}
	}
	// kyokomi form: "<base>_toneN" where N is 1-5, exactly 6 trailing chars.
	if len(name) >= 7 {
		end := name[len(name)-6:]
		if end[0] == '_' && end[1] == 't' && end[2] == 'o' && end[3] == 'n' && end[4] == 'e' &&
			end[5] >= '1' && end[5] <= '5' {
			return name[:len(name)-6], lightestSkinTone + rune(end[5]-'1'), true
		}
	}
	return "", 0, false
}
