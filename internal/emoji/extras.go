package emoji

// extraProbeChars are Unicode characters that appear in the
// Extended_Pictographic set or are commonly rendered as emoji glyphs by
// terminals, but are NOT present in the kyokomi/emoji codemap. Without
// these, the probe never tests them and width measurement falls back to
// displaywidth's spec-based view (typically 1), which may disagree with
// the terminal's actual rendering.
//
// The list is conservative: it covers the misc-symbols and dingbats
// blocks (U+2600–U+27BF) plus a few outliers commonly used as Slack
// reactions. It deliberately excludes ranges already well-covered by
// kyokomi (most U+1Fxxx emoji).
//
// This is a curated supplement, not a full Unicode TR51 enumeration.
// Add entries as you discover misrendered characters in real use.
var extraProbeChars = []string{
	// Stars and shapes
	"\u2605", // ★ black star
	"\u2606", // ☆ white star
	"\u2729", // ✩ stress outlined star
	"\u272A", // ✪ circled white star
	"\u2730", // ✰ shadowed white star
	"\u2731", // ✱ heavy asterisk
	"\u2732", // ✲ open centre asterisk
	"\u2733", // ✳ eight spoked asterisk
	"\u2734", // ✴ eight pointed star
	"\u2735", // ✵ eight pointed pinwheel
	"\u2736", // ✶ six pointed black star
	"\u2737", // ✷ eight pointed rectilinear star
	"\u2738", // ✸ heavy eight pointed black star
	"\u273D", // ✽ heavy teardrop spoked asterisk
	"\u2740", // ❀ white florette
	"\u2741", // ❁ eight petalled outlined black florette
	"\u2742", // ❂ circled open centre eight pointed star
	"\u2743", // ❃ heavy teardrop spoked pinwheel asterisk
	"\u2744", // ❄ snowflake (kyokomi has :snowflake: with VS16)
	"\u2745", // ❅ tight trifoliate snowflake
	"\u2746", // ❆ heavy chevron snowflake
	"\u2748", // ❈ heavy sparkle
	"\u2749", // ❉ balloon-spoked asterisk

	// Hearts and card suits (kyokomi has only the filled forms)
	"\u2661", // ♡ white heart suit
	"\u2662", // ♢ white diamond suit
	"\u2664", // ♤ white spade suit
	"\u2667", // ♧ white club suit
	"\u2763", // ❣ heart exclamation
	"\u2765", // ❥ rotated heavy black heart bullet

	// Brackets, ornaments
	"\u2754", // ❔ white question mark
	"\u2756", // ❖ black diamond minus white x
	"\u275B", // ❛ heavy single turned comma quotation mark
	"\u275C", // ❜ heavy single comma quotation mark
	"\u275D", // ❝ heavy double turned comma quotation mark
	"\u275E", // ❞ heavy double comma quotation mark
	"\u2764", // ❤ heavy black heart (kyokomi has it but worth probing alone)

	// Misc symbols not in kyokomi
	"\u2600", // ☀ black sun (text-default, kyokomi has via :sunny:)
	"\u2601", // ☁ cloud (kyokomi has :cloud:)
	"\u2602", // ☂ umbrella (kyokomi has :umbrella2:)
	"\u2603", // ☃ snowman (kyokomi has :snowman:)
	"\u2604", // ☄ comet (kyokomi has :comet:)
	"\u260E", // ☎ phone (kyokomi has :phone:)

	// Arrows (kyokomi has many arrows but not all variants)
	"\u2190", // ← leftwards arrow
	"\u2191", // ↑ upwards arrow
	"\u2192", // → rightwards arrow
	"\u2193", // ↓ downwards arrow

	// Math/punctuation that some terminals widen
	"\u2014", // — em dash
	"\u2026", // … horizontal ellipsis
	"\u2042", // ⁂ asterism
	"\u2055", // ⁕ flower punctuation mark
}
