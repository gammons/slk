package emoji

//go:generate go run gen_slacknames.go

import "fmt"

// CanonicalSlackName resolves a picker shortcode (no colons) to the emoji
// name Slack's reactions.add / reactions.remove API accepts. The customs
// map is the workspace's emoji.list (name -> URL or "alias:target"); pass
// nil if unavailable.
//
// The picker is sourced from the iamcal table (slacknames_gen.go), so a
// pick is always a valid Slack short_name — but it may be a secondary
// alias (e.g. "thumbsup" rather than "+1" for 👍). Slack records exactly
// one canonical short_name per glyph, which is what its own web client
// sends; this maps any alias to that canonical form so the wire name and
// the locally stored reaction name stay consistent.
//
// Resolution order:
//  1. Custom-emoji shadow: a workspace custom emoji (or custom alias) wins
//     over any standard name — Slack stores it under that exact name, so
//     it MUST be sent verbatim. Checked first; without this a custom
//     ":facepalm:" would be rewritten to the standard ":face_palm:" and
//     the wrong (standard) reaction would be applied.
//  2. Skin-tone suffix ("+1_tone3" / "+1::skin-tone-3"): canonicalize the
//     base name and re-attach Slack's "::skin-tone-N" form.
//  3. Direct alias hit: the name is already a known Slack short_name or
//     alias — return its canonical form.
//  4. Fallback: return the name unchanged (unknown name sent verbatim).
func CanonicalSlackName(name string, customs map[string]string) string {
	if _, ok := customs[name]; ok {
		return name
	}
	if base, modifier, ok := splitSkinToneSuffix(name); ok {
		tone, _ := SlackSkinToneNumber(modifier)
		return fmt.Sprintf("%s::skin-tone-%d", CanonicalSlackName(base, customs), tone)
	}
	if canonical, ok := slackNameToCanonical[name]; ok {
		return canonical
	}
	return name
}
