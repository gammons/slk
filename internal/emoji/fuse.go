package emoji

// FuseModifierSequences fixes a common kyokomi/emoji output quirk: the
// library inserts a literal space (its ReplacePadding character) after
// each :shortcode: it expands. For Slack-style multi-shortcode reactions
// like ":+1::skin-tone-2:", this produces "👍 🏻 " — but the SPACE
// between 👍 and 🏻 breaks the grapheme-cluster fusion that terminals
// rely on to render skin-toned emoji as a single 4-cell glyph.
//
// This function removes any space that sits directly between an emoji
// modifier base (Extended_Pictographic with Emoji_Modifier_Base=Yes,
// approximated as any rune in the Symbols/Pictographs blocks) and a
// skin-tone modifier (U+1F3FB–U+1F3FF). The result preserves the visual
// content while letting the grapheme segmenter (and our width cache)
// recognize the sequence as a single cluster.
//
// Example:
//
//	in:  "👍 🏻 1"   (kyokomi output for :+1::skin-tone-2:)
//	out: "👍🏻 1"    (fused; "👍🏻" hits the cache as a single cluster)
func FuseModifierSequences(s string) string {
	if len(s) < 9 { // emoji (≥4) + space (1) + modifier (4)
		return s
	}

	out := make([]byte, 0, len(s))
	i := 0
	for i < len(s) {
		// Look for: <emoji-base bytes>0x20<modifier bytes>
		// Specifically, if we have just emitted what could be an emoji
		// base and the next bytes are " 🏻", "fuse" by skipping the space.
		if s[i] == 0x20 && i+4 < len(s) &&
			s[i+1] == 0xF0 && s[i+2] == 0x9F && s[i+3] == 0x8F &&
			s[i+4] >= 0xBB && s[i+4] <= 0xBF {
			// Check the byte immediately before the space looks like the
			// end of a 4-byte UTF-8 emoji (high bit set, continuation byte
			// pattern). If yes, drop the space.
			if len(out) > 0 && out[len(out)-1] >= 0x80 {
				// Skip the space; copy the modifier on the next iteration.
				i++
				continue
			}
		}
		out = append(out, s[i])
		i++
	}
	return string(out)
}
