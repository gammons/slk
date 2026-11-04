package emoji

import (
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// TokenKind discriminates between text and emoji tokens in the
// output of ResolveEmojiToTokens.
type TokenKind int

const (
	// TokenText is a literal run of non-emoji text.
	TokenText TokenKind = iota
	// TokenEmoji is one emoji — either a resolved :shortcode: match
	// or a Unicode grapheme cluster carrying emoji presentation.
	TokenEmoji
)

// Token is one chunk emitted by ResolveEmojiToTokens.
//
// For TokenText: Text holds the literal substring, URL is empty.
// For TokenEmoji: Text holds the plain-text representation used for
// yank, clipboard copy, in-buffer search, and cold-cache fallback
// (":name:" form for shortcodes/customs, the Unicode glyph for
// emoji that appeared as raw codepoints in the source). URL holds
// the Slack CDN URL for the image.
type Token struct {
	Kind TokenKind
	Text string
	URL  string
}

// ResolveEmojiToTokens scans text and emits a token stream. Every
// emoji that can be resolved to a Slack CDN URL becomes a
// TokenEmoji; everything else is folded into TokenText runs.
//
// Two detection paths run in a single linear pass:
//
//  1. ":shortcode:" matches (e.g., ":thumbsup:", ":party_parrot:").
//     Resolved via URLForShortcode, which consults the workspace
//     customs map first (with alias chains) and falls through to
//     the kyokomi builtin codemap.
//
//  2. Unicode emoji grapheme clusters embedded in the source text
//     (e.g., a literal "👍" in a Slack message body). Detected by
//     matching the cluster against the set of all kyokomi-known
//     emoji clusters; URL is built directly from the cluster's
//     codepoints.
//
// Unresolvable shortcodes (unknown name, alias cycle, etc.) pass
// through verbatim as TokenText so the user still sees the
// readable ":name:" form. Same for Unicode codepoints that aren't
// in the known-emoji set — they remain in the text run unchanged.
//
// Adjacent emoji produce adjacent TokenEmoji values with no
// intervening TokenText.
//
// customs may be nil; nil is treated as an empty workspace
// (kyokomi-only resolution).
func ResolveEmojiToTokens(text string, customs map[string]string) []Token {
	if text == "" {
		return nil
	}
	var tokens []Token
	var textBuf strings.Builder
	flushText := func() {
		if textBuf.Len() > 0 {
			tokens = append(tokens, Token{Kind: TokenText, Text: textBuf.String()})
			textBuf.Reset()
		}
	}

	// Linear byte-position walk. At each position we try (a) shortcode
	// match, (b) emoji-cluster match, then fall through to a single-rune
	// advance into the running text buffer.
	i := 0
	for i < len(text) {
		// (a) Shortcode pass.
		if text[i] == ':' {
			if end, name, ok := tryShortcodeAt(text, i); ok {
				source := ":" + name + ":"
				// A skin-toned emoji arrives as two shortcodes,
				// ":+1::skin-tone-2:" (RichTextToMrkdwn writes it so);
				// Slack draws one toned emoji, not the base and a swatch.
				if toneEnd, tone, ok := tryShortcodeAt(text, end); ok && strings.HasPrefix(tone, "skin-tone-") {
					if _, custom := customs[name]; !custom {
						if url, ok := skinTonedURL(name + "::" + tone); ok {
							flushText()
							tokens = append(tokens, Token{
								Kind: TokenEmoji,
								Text: source + ":" + tone + ":",
								URL:  url,
							})
							i = toneEnd
							continue
						}
					}
					// A tone per person has no swatch to fall back on,
					// so a pair with no image draws as the base alone
					// rather than leaving ":skin-tone-2-5:" as text.
					if _, _, ok := tonePerPerson(tone); ok {
						source += ":" + tone + ":"
						end = toneEnd
					}
				}
				if url, urlOK := URLForShortcode(name, customs); urlOK {
					flushText()
					tokens = append(tokens, Token{
						Kind: TokenEmoji,
						Text: source,
						URL:  url,
					})
					i = end
					continue
				}
				// Known to be a syntactically valid shortcode but not
				// resolvable. Fall through to default rune-consume so
				// the literal ":name:" appears verbatim in the next
				// text run.
			}
		}
		// (b) Emoji-cluster pass.
		if r, _ := utf8.DecodeRuneInString(text[i:]); r >= 0x80 {
			// Only attempt grapheme segmentation when the byte at i is
			// the start of a non-ASCII rune. Pure-ASCII fast path
			// avoids the uniseg.NewGraphemes call cost per byte.
			cluster, clusterLen, found := nextGraphemeCluster(text[i:])
			if found && isKnownEmojiCluster(cluster) {
				url := BuildStandardEmojiURL([]rune(cluster))
				if url != "" {
					flushText()
					tokens = append(tokens, Token{
						Kind: TokenEmoji,
						Text: cluster,
						URL:  url,
					})
					i += clusterLen
					continue
				}
			}
		}

		// Default: consume one rune into the text buffer.
		r, sz := utf8.DecodeRuneInString(text[i:])
		textBuf.WriteRune(r)
		i += sz
	}
	flushText()
	return tokens
}

// tryShortcodeAt attempts to read a ":name:" shortcode starting at
// text[i]. Returns (endByte, name, true) on success — endByte is the
// byte index immediately past the closing colon. Returns (0, "", false)
// if text[i] is not ':', if no closing colon is found before the next
// non-shortcode rune, or if the name is empty.
//
// The name character class matches shortcodeRe in render.go:
// [A-Za-z0-9_+-]. Mismatched chars terminate the scan with ok=false.
func tryShortcodeAt(text string, i int) (int, string, bool) {
	if i >= len(text) || text[i] != ':' {
		return 0, "", false
	}
	j := i + 1
	for j < len(text) {
		c := text[j]
		if c == ':' {
			break
		}
		if !isShortcodeChar(c) {
			return 0, "", false
		}
		j++
	}
	if j >= len(text) || text[j] != ':' {
		return 0, "", false
	}
	name := text[i+1 : j]
	if name == "" {
		return 0, "", false
	}
	return j + 1, name, true
}

func isShortcodeChar(c byte) bool {
	return (c >= 'A' && c <= 'Z') ||
		(c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') ||
		c == '_' || c == '+' || c == '-'
}

// emojiClusterSet is the set of all grapheme clusters known to
// resolve to standard emoji. Populated from the kyokomi codemap
// values on first lookup. Both the canonical form and a
// VS16-stripped form are inserted so source text using either
// presentation triggers a match.
var (
	emojiClusterSetOnce sync.Once
	emojiClusterSet     map[string]struct{}
)

func initEmojiClusterSet() {
	set := make(map[string]struct{}, 4096)
	for _, u := range emojilibCodeMap() {
		// kyokomi appends a trailing space for Sprint-style use; strip it.
		canonical := strings.TrimRight(u, " ")
		if canonical == "" {
			continue
		}
		set[canonical] = struct{}{}

		// Also insert the VS16-stripped form for source text that
		// uses bare codepoints without the variation selector.
		stripped := stripVS16(canonical)
		if stripped != canonical && stripped != "" {
			set[stripped] = struct{}{}
		}
	}
	emojiClusterSet = set
}

func isKnownEmojiCluster(cluster string) bool {
	emojiClusterSetOnce.Do(initEmojiClusterSet)
	_, ok := emojiClusterSet[cluster]
	return ok
}

func stripVS16(s string) string {
	if !strings.ContainsRune(s, vs16) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == vs16 {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// emojilibCodeMap returns the standard-emoji shortcode→glyph table
// (iamcal-derived). Kept as a thin indirection for the cluster-set
// initializer above.
func emojilibCodeMap() map[string]string {
	return slackCodeMap
}

// nextGraphemeCluster returns the first grapheme cluster of s, its
// byte length, and true if a cluster was extracted. Returns
// ("", 0, false) on empty input.
func nextGraphemeCluster(s string) (string, int, bool) {
	if s == "" {
		return "", 0, false
	}
	gr := uniseg.NewGraphemes(s)
	if !gr.Next() {
		return "", 0, false
	}
	cluster := gr.Str()
	return cluster, len(cluster), true
}
