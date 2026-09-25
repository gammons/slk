// Package text provides string utilities for search and matching.
package text

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Fold returns s with diacritics removed and lowercased, suitable for
// case- and accent-insensitive substring matching.
//
//	Fold("Mélanie")  == "melanie"
//	Fold("François") == "francois"
//	Fold("Café")     == "cafe"
//
// Characters with no decomposition (CJK, emoji, plain ASCII) pass through
// unchanged apart from lowercasing. Fold uses NFD (canonical) decomposition,
// not NFKD — ligatures and the German Eszett are NOT expanded, so
// Fold("ß") == "ß" and Fold("ﬃ") == "ﬃ". This is deliberate; NFKD changes
// far more than diacritics.
//
// If the transform pipeline ever returns an error (it should not for
// in-memory strings with this chain), Fold falls back to strings.ToLower(s)
// so matching degrades gracefully instead of panicking.
func Fold(s string) string {
	// ASCII fast path.
	//
	// ASCII code points have no canonical decomposition and carry no
	// combining marks, so NFD -> remove Mn -> NFC is the identity on
	// them; only the lowercasing is load-bearing. foldSlow is the
	// reference implementation and TestFoldFastPathMatchesSlow (plus
	// FuzzFold) pin the two to identical output, so this cannot drift.
	//
	// This matters because Fold is called once per candidate inside the
	// filter loop of ten pickers -- channelfinder, channelpicker,
	// sidebar, emojipicker, reactionpicker, mentionpicker,
	// workspacefinder, themeswitcher, presencemenu, help -- and the
	// transform chain allocates on every single call. Measured on 62k
	// emoji entries with a rare query (full scan, no early exit):
	//
	//	compose picker    135 ms / 542 MB / 372k allocs
	//	              ->  1.59 ms / 0 B   / 0 allocs
	//	reaction picker   271 ms / 1.12 GB / 768k allocs
	//	              ->  2.96 ms / 611 B  / 1 alloc
	//
	// Allocations reach zero because strings.ToLower returns its input
	// unchanged when there is nothing to lower, which is the common
	// case for already-lowercase names.
	//
	// See issue #165 for the full measurements and methodology.
	if isASCII(s) {
		return strings.ToLower(s)
	}
	return foldSlow(s)
}

// foldSlow is the general Unicode implementation: the reference
// behaviour Fold's ASCII fast path must match exactly.
func foldSlow(s string) string {
	// Build the chain per call: transform.Chain returns a *chain with
	// internal buffers and cursor state that transform.String mutates,
	// so it is NOT safe to share across goroutines. Per-call allocation
	// is acceptable here now that the ASCII fast path keeps this off
	// the hot filter loops.
	chain := transform.Chain(
		norm.NFD,
		runes.Remove(runes.In(unicode.Mn)),
		norm.NFC,
	)
	out, _, err := transform.String(chain, s)
	if err != nil {
		return strings.ToLower(s)
	}
	return strings.ToLower(out)
}

// isASCII reports whether s is entirely ASCII.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
