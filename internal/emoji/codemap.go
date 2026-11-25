package emoji

import "strings"

// CodeMap returns the standard-emoji shortcode→glyph map, keyed by
// ":name:" (all Slack aliases) with bare-glyph values (no trailing
// space). It is slk's replacement for kyokomi/emoji's CodeMap(), sourced
// from the bundled iamcal/emoji-data table (slacknames_gen.go).
//
// The returned map is shared and must not be mutated by callers (all
// current callers only read it).
func CodeMap() map[string]string { return slackCodeMap }

// Sprint substitutes Slack-style :shortcode: sequences in s with their
// Unicode glyph, appending a trailing space after each — byte-for-byte
// compatible with the kyokomi/emoji Sprint() it replaces. A codepoint-
// form name (":1f44d:") resolves like its shortcode. Unknown shortcodes
// and non-shortcode text (including ASCII emoticons like ":)") pass
// through unchanged.
func Sprint(s string) string {
	return shortcodeRe.ReplaceAllStringFunc(s, func(match string) string {
		if glyph, ok := ShortcodeGlyph(strings.Trim(match, ":")); ok {
			return glyph + " "
		}
		return match
	})
}
