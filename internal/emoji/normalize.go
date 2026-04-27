// Package emoji provides utilities for normalizing emoji presentation
// to match terminal rendering behavior.
package emoji

import (
	"unicode/utf8"
)

const vs16 = '\uFE0F' // Variation Selector 16 — requests emoji presentation
const vs16Str = "\uFE0F"

// NormalizeEmojiPresentation strips VS16 (U+FE0F) from text-default
// Extended_Pictographic characters at the start of a string.
// Use StripTextDefaultVS16 for strings with mixed text and emoji.
//
// Many emoji libraries (like kyokomi/emoji) include VS16 after
// text-default characters (e.g., ❤️ = U+2764 U+FE0F). Width libraries
// like displaywidth interpret VS16 as "width 2", but many terminals
// render these as 1-wide regardless of VS16. This mismatch causes
// overcounting — the measured width exceeds the rendered width, shifting
// TUI borders to the left.
//
// Stripping VS16 makes displaywidth return width 1 for these characters,
// matching terminal rendering. Characters with Emoji_Presentation=Yes
// (like 👍 U+1F44D) are NOT affected — they are already width 2 without
// VS16 and don't have VS16 to strip.
func NormalizeEmojiPresentation(s string) string {
	if len(s) == 0 {
		return s
	}

	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}

	// Only strip VS16 from text-default Extended_Pictographic characters.
	if !isTextDefaultEP(r) {
		return s
	}

	// Check if VS16 follows the first rune — if so, remove it.
	rest := s[size:]
	if len(rest) >= 3 && rest[0] == 0xEF && rest[1] == 0xB8 && rest[2] == 0x8F {
		return s[:size] + rest[3:]
	}

	return s
}

// StripTextDefaultVS16 scans an entire string and removes VS16 (U+FE0F)
// wherever it follows a text-default Extended_Pictographic character.
// This is for processing message body text that contains mixed content
// (plain text interspersed with emoji).
func StripTextDefaultVS16(s string) string {
	if len(s) == 0 {
		return s
	}

	// Fast path: if no VS16 bytes exist, nothing to strip.
	hasVS16 := false
	for i := 0; i+2 < len(s); i++ {
		if s[i] == 0xEF && s[i+1] == 0xB8 && s[i+2] == 0x8F {
			hasVS16 = true
			break
		}
	}
	if !hasVS16 {
		return s
	}

	// Scan through the string rune by rune. When we find a text-default
	// EP character followed by VS16, skip the VS16 bytes.
	buf := make([]byte, 0, len(s))
	i := 0
	prevRune := rune(0)
	prevIsEP := false

	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])

		if r == vs16 && prevIsEP {
			// Skip this VS16 — it follows a text-default EP character
			i += size
			prevIsEP = false
			continue
		}

		prevIsEP = isTextDefaultEP(r)
		prevRune = r
		buf = append(buf, s[i:i+size]...)
		i += size
	}

	_ = prevRune
	return string(buf)
}

// isTextDefaultEP returns true if the rune is an Extended_Pictographic
// character that has text presentation by default (Emoji_Presentation=No).
//
// These characters are rendered as 1-wide in many terminals even when
// followed by VS16. Stripping VS16 from them makes width measurement
// match actual terminal rendering.
//
// This covers the full Extended_Pictographic set with Emoji_Presentation=No
// from Unicode 15.1 emoji-data.txt.
func isTextDefaultEP(r rune) bool {
	switch {
	// Latin supplement
	case r == 0x00A9, r == 0x00AE: // ©, ®
		return true

	// General punctuation
	case r == 0x203C, r == 0x2049: // ‼, ⁉
		return true

	// Letterlike symbols
	case r == 0x2122, r == 0x2139: // ™, ℹ
		return true

	// Arrows
	case r >= 0x2194 && r <= 0x2199: // ↔–↙
		return true
	case r >= 0x21A9 && r <= 0x21AA: // ↩, ↪
		return true

	// Misc technical
	case r == 0x2328: // ⌨ keyboard
		return true
	case r == 0x23CF: // ⏏ eject
		return true
	case r >= 0x23E9 && r <= 0x23F3: // ⏩–⏳
		return true
	case r >= 0x23F8 && r <= 0x23FA: // ⏸–⏺
		return true

	// Enclosed alphanumerics
	case r == 0x24C2: // Ⓜ
		return true

	// Geometric shapes
	case r == 0x25AA, r == 0x25AB: // ▪, ▫
		return true
	case r == 0x25B6, r == 0x25C0: // ▶, ◀
		return true
	case r >= 0x25FB && r <= 0x25FE: // ◻–◾
		return true

	// Misc symbols
	case r >= 0x2600 && r <= 0x2604: // ☀–☄
		return true
	case r == 0x260E: // ☎
		return true
	case r == 0x2611: // ☑
		return true
	case r >= 0x2614 && r <= 0x2615: // ☔, ☕
		return true
	case r == 0x2618: // ☘
		return true
	case r == 0x261D: // ☝
		return true
	case r == 0x2620: // ☠
		return true
	case r >= 0x2622 && r <= 0x2623: // ☢, ☣
		return true
	case r == 0x2626: // ☦
		return true
	case r == 0x262A: // ☪
		return true
	case r >= 0x262E && r <= 0x262F: // ☮, ☯
		return true
	case r >= 0x2638 && r <= 0x263A: // ☸–☺
		return true
	case r == 0x2640, r == 0x2642: // ♀, ♂
		return true
	case r >= 0x2648 && r <= 0x2653: // ♈–♓ zodiac
		return true
	case r >= 0x265F && r <= 0x2660: // ♟, ♠
		return true
	case r == 0x2663: // ♣
		return true
	case r >= 0x2665 && r <= 0x2666: // ♥, ♦
		return true
	case r == 0x2668: // ♨
		return true
	case r == 0x267B: // ♻
		return true
	case r >= 0x267E && r <= 0x267F: // ♾, ♿
		return true
	case r >= 0x2692 && r <= 0x2697: // ⚒–⚗
		return true
	case r == 0x2699: // ⚙
		return true
	case r >= 0x269B && r <= 0x269C: // ⚛, ⚜
		return true
	case r >= 0x26A0 && r <= 0x26A1: // ⚠, ⚡
		return true
	case r == 0x26A7: // ⚧
		return true
	case r >= 0x26AA && r <= 0x26AB: // ⚪, ⚫
		return true
	case r >= 0x26B0 && r <= 0x26B1: // ⚰, ⚱
		return true
	case r >= 0x26BD && r <= 0x26BE: // ⚽, ⚾
		return true
	case r >= 0x26C4 && r <= 0x26C5: // ⛄, ⛅
		return true
	case r == 0x26C8: // ⛈
		return true
	case r >= 0x26CE && r <= 0x26CF: // ⛎, ⛏
		return true
	case r == 0x26D1: // ⛑
		return true
	case r >= 0x26D3 && r <= 0x26D4: // ⛓, ⛔
		return true
	case r >= 0x26E9 && r <= 0x26EA: // ⛩, ⛪
		return true
	case r >= 0x26F0 && r <= 0x26F5: // ⛰–⛵
		return true
	case r >= 0x26F7 && r <= 0x26FA: // ⛷–⛺
		return true
	case r == 0x26FD: // ⛽
		return true

	// Dingbats
	case r == 0x2702: // ✂
		return true
	case r == 0x2705: // ✅
		return true
	case r >= 0x2708 && r <= 0x270D: // ✈–✍
		return true
	case r == 0x270F: // ✏
		return true
	case r == 0x2712: // ✒
		return true
	case r == 0x2714, r == 0x2716: // ✔, ✖
		return true
	case r == 0x271D: // ✝
		return true
	case r == 0x2721: // ✡
		return true
	case r == 0x2728: // ✨
		return true
	case r >= 0x2733 && r <= 0x2734: // ✳, ✴
		return true
	case r == 0x2744: // ❄
		return true
	case r == 0x2747: // ❇
		return true
	case r == 0x274C, r == 0x274E: // ❌, ❎
		return true
	case r >= 0x2753 && r <= 0x2755: // ❓–❕
		return true
	case r == 0x2757: // ❗
		return true
	case r >= 0x2763 && r <= 0x2764: // ❣, ❤
		return true
	case r >= 0x2795 && r <= 0x2797: // ➕–➗
		return true
	case r == 0x27A1: // ➡
		return true
	case r == 0x27B0, r == 0x27BF: // ➰, ➿
		return true

	// Supplemental arrows
	case r >= 0x2934 && r <= 0x2935: // ⤴, ⤵
		return true

	// Misc symbols and arrows
	case r >= 0x2B05 && r <= 0x2B07: // ⬅–⬇
		return true
	case r >= 0x2B1B && r <= 0x2B1C: // ⬛, ⬜
		return true
	case r == 0x2B50, r == 0x2B55: // ⭐, ⭕
		return true

	// CJK symbols
	case r == 0x3030, r == 0x303D: // 〰, 〽
		return true
	case r == 0x3297, r == 0x3299: // ㊗, ㊙
		return true

	// Supplemental symbols (U+1Fxxx) — text-default Extended_Pictographic
	case r == 0x1F004, r == 0x1F0CF: // 🀄, 🃏
		return true
	case r >= 0x1F170 && r <= 0x1F171: // 🅰, 🅱
		return true
	case r == 0x1F17E, r == 0x1F17F: // 🅾, 🅿
		return true
	case r == 0x1F202, r == 0x1F237: // 🈂, 🈷
		return true
	case r >= 0x1F321 && r <= 0x1F32C: // 🌡–🌬
		return true
	case r == 0x1F336: // 🌶
		return true
	case r == 0x1F37D: // 🍽
		return true
	case r >= 0x1F396 && r <= 0x1F397: // 🎖, 🎗
		return true
	case r >= 0x1F399 && r <= 0x1F39B: // 🎙–🎛
		return true
	case r >= 0x1F39E && r <= 0x1F39F: // 🎞, 🎟
		return true
	case r >= 0x1F3CB && r <= 0x1F3CE: // 🏋–🏎
		return true
	case r >= 0x1F3D4 && r <= 0x1F3DF: // 🏔–🏟
		return true
	case r == 0x1F3F3, r == 0x1F3F5, r == 0x1F3F7: // 🏳, 🏵, 🏷
		return true
	case r == 0x1F43F, r == 0x1F441: // 🐿, 👁
		return true
	case r >= 0x1F4FD && r <= 0x1F4FE: // 📽, 📾
		return true
	case r >= 0x1F549 && r <= 0x1F54A: // 🕉, 🕊
		return true
	case r >= 0x1F56F && r <= 0x1F570: // 🕯, 🕰
		return true
	case r >= 0x1F573 && r <= 0x1F579: // 🕳–🕹
		return true
	case r == 0x1F587: // 🖇
		return true
	case r >= 0x1F58A && r <= 0x1F58D: // 🖊–🖍
		return true
	case r == 0x1F590: // 🖐
		return true
	case r >= 0x1F5A4 && r <= 0x1F5A5: // 🖤, 🖥
		return true
	case r == 0x1F5A8: // 🖨
		return true
	case r >= 0x1F5B1 && r <= 0x1F5B2: // 🖱, 🖲
		return true
	case r == 0x1F5BC: // 🖼
		return true
	case r >= 0x1F5C2 && r <= 0x1F5C4: // 🗂–🗄
		return true
	case r >= 0x1F5D1 && r <= 0x1F5D3: // 🗑–🗓
		return true
	case r >= 0x1F5DC && r <= 0x1F5DE: // 🗜–🗞
		return true
	case r == 0x1F5E1, r == 0x1F5E3, r == 0x1F5E8: // 🗡, 🗣, 🗨
		return true
	case r == 0x1F5EF, r == 0x1F5F3, r == 0x1F5FA: // 🗯, 🗳, 🗺
		return true
	case r >= 0x1F6CB && r <= 0x1F6CF: // 🛋–🛏
		return true
	case r >= 0x1F6E0 && r <= 0x1F6E5: // 🛠–🛥
		return true
	case r == 0x1F6E9, r == 0x1F6F0, r == 0x1F6F3: // 🛩, 🛰, 🛳
		return true
	}

	return false
}
