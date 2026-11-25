package emoji

import (
	"regexp"
	"strings"
)

// shortcodeRe matches Slack-style emoji shortcodes embedded in text:
// a colon, a name made of letters/digits/_/+/-, then a closing colon.
// Anchored by the colons; non-greedy isn't needed because the inner
// class disallows colons. Matches what Slack permits in custom emoji
// names plus the kyokomi built-in set.
var shortcodeRe = regexp.MustCompile(`:[A-Za-z0-9_+\-]+:`)

// ResolveShortcodesInText substitutes safe Slack-style :shortcode:
// sequences in s with their Unicode glyphs (with a trailing space, for
// byte-for-byte parity with the previous emoji.Sprint(text) behavior).
// Shortcodes whose resolved Unicode form fails ShouldRenderUnicode (ZWJ
// sequences, flag pairs, skin-tone modifiers) are left as the literal
// :name: text so they render as readable shortcodes instead of broken
// glyphs. Unknown shortcodes pass through unchanged.
func ResolveShortcodesInText(s string) string {
	return shortcodeRe.ReplaceAllStringFunc(s, func(match string) string {
		glyph, ok := ShortcodeGlyph(strings.Trim(match, ":"))
		if !ok {
			// Unrecognized shortcode; pass through.
			return match
		}
		resolved := glyph + " "
		if ShouldRenderUnicode(resolved) {
			return resolved
		}
		return match
	})
}
