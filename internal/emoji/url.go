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
	// kyokomi (e.g. "+1::skin-tone-3" or "+1_tone3"). Compose the
	// base emoji's codepoints with the skin-tone modifier codepoint
	// and build the URL directly. Slack's CDN serves these at the
	// expected combined-codepoint path.
	if cps, ok := ComposeSkinTonedCodepoints(name); ok {
		return BuildStandardEmojiURL(cps), true
	}
	return "", false
}

// skinToneCodepoints maps the "tone1".."tone5" suffix digit to the
// Unicode skin tone modifier codepoint.
var skinToneCodepoints = [...]rune{
	1: 0x1F3FB, // tone1
	2: 0x1F3FC, // tone2
	3: 0x1F3FD, // tone3
	4: 0x1F3FE, // tone4
	5: 0x1F3FF, // tone5
}

// ComposeSkinTonedCodepoints decomposes a skin-toned shortcode name
// (either Slack's "::skin-tone-N" or kyokomi's "_toneN" form), looks
// up the base shortcode in the kyokomi codemap, and inserts the
// matching skin-tone modifier codepoint right after the base (first)
// codepoint.
//
// The skin-tone modifier applies to the base emoji — the first
// codepoint in the sequence. For simple emojis (e.g. 👍 = U+1F44D)
// this is equivalent to appending. For ZWJ sequences (e.g.
// 🏃‍➡️ = U+1F3C3 U+200D U+27A1 U+FE0F) the modifier MUST go
// after the base codepoint and BEFORE the ZWJ, producing
// "1f3c3-1f3fb-200d-27a1-fe0f" — NOT "1f3c3-200d-27a1-fe0f-1f3fb",
// which 404s on Slack's CDN.
//
// Used by URLForShortcode as a fallback for skin-toned variants that
// aren't pre-resolved in kyokomi's codemap (e.g. ":+1_tone3:" is not
// in the codemap, but ":+1:" is, and combining its codepoint with
// U+1F3FD produces the correct asset URL).
//
// Returns (codepoints, true) on success. Returns (nil, false) when:
//   - name has no recognizable tone suffix
//   - the tone digit is out of range
//   - the base shortcode isn't in the kyokomi codemap
func ComposeSkinTonedCodepoints(name string) ([]rune, bool) {
	base, tone, ok := splitSkinToneSuffix(name)
	if !ok {
		return nil, false
	}
	if tone < 1 || tone > 5 {
		return nil, false
	}
	baseCps, ok := CodepointsForShortcode(base)
	if !ok {
		return nil, false
	}
	// Insert the skin-tone modifier after the first codepoint (the
	// base emoji), before any ZWJ / VS16 tail. This matches the
	// codepoint ordering Slack's CDN uses for skin-toned ZWJ emoji
	// assets.
	out := make([]rune, len(baseCps)+1)
	out[0] = baseCps[0]
	out[1] = skinToneCodepoints[tone]
	copy(out[2:], baseCps[1:])
	return out, true
}

// splitSkinToneSuffix splits a skin-toned shortcode name into the base
// name and tone digit. Returns (base, toneDigit, true) on success, or
// ("", 0, false) if the name has no recognizable suffix.
//
// Recognized forms:
//
//	"+1::skin-tone-3"  → ("+1", 3, true)        // Slack reaction API
//	"thumbsup_tone3"   → ("thumbsup", 3, true)  // kyokomi
//
// Note: this overlaps in shape with StripSkinTone in fuse.go, which
// returns just the base name and is used by the legacy glyph-rendering
// fallback path. Different return shape, different purpose — keeping
// them separate is intentional.
func splitSkinToneSuffix(name string) (string, int, bool) {
	// Slack form: "<base>::skin-tone-N"
	if i := strings.Index(name, "::skin-tone-"); i >= 0 {
		tail := name[i+len("::skin-tone-"):]
		if len(tail) == 1 && tail[0] >= '1' && tail[0] <= '5' {
			return name[:i], int(tail[0] - '0'), true
		}
	}
	// kyokomi form: "<base>_toneN" where N is 1-5, exactly 6 trailing chars.
	if len(name) >= 7 {
		end := name[len(name)-6:]
		if end[0] == '_' && end[1] == 't' && end[2] == 'o' && end[3] == 'n' && end[4] == 'e' &&
			end[5] >= '1' && end[5] <= '5' {
			return name[:len(name)-6], int(end[5] - '0'), true
		}
	}
	return "", 0, false
}
