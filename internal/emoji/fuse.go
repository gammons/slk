// This file replaces an earlier FuseModifierSequences approach. Trying
// to merge the kyokomi-emitted space between an emoji base and a skin
// tone modifier worked well in tests but kept hitting corner cases in
// the real terminal (uniseg vs. uax29 disagreed about cluster boundaries,
// some terminals didn't render the fused form the same way as the
// space-separated form, etc.). Removing the skin-tone designation
// entirely is more reliable and visually acceptable.

package emoji

import "strings"

// StripSkinTone removes any Slack-style or kyokomi-style skin-tone
// modifier suffix from a reaction shortcode name, returning the bare
// base shortcode.
//
// Slack's reaction API names skin-toned variants by suffixing the base
// name with "::skin-tone-N" (e.g. "+1::skin-tone-2"). kyokomi uses a
// different convention: "_toneN" appended directly to the shortcode
// (e.g. "thumbsup_tone1"). Both forms render as a multi-cluster emoji
// sequence whose width depends on terminal-specific fusion behavior
// that's hard to predict and easy to mismeasure, breaking border
// alignment in the TUI.
//
// We trade visual skin-tone fidelity for reliable layout: the pill
// shows the base emoji (e.g. 👍) regardless of the chosen skin tone.
//
// Examples:
//
//	StripSkinTone("+1::skin-tone-2") == "+1"
//	StripSkinTone("thumbsup_tone3")  == "thumbsup"
//	StripSkinTone("wave")            == "wave"
//	StripSkinTone("custom_emoji")    == "custom_emoji"  (untouched)
func StripSkinTone(name string) string {
	// Slack form: "<base>::skin-tone-N"
	if i := strings.Index(name, "::skin-tone-"); i >= 0 {
		return name[:i]
	}

	// kyokomi form: "<base>_toneN" where N is 1-5.
	// Match "_tone" followed by exactly one digit at the end of the string.
	if len(name) >= 6 {
		end := name[len(name)-6:]
		if end[0] == '_' && end[1] == 't' && end[2] == 'o' && end[3] == 'n' && end[4] == 'e' &&
			end[5] >= '1' && end[5] <= '5' {
			return name[:len(name)-6]
		}
	}

	return name
}
