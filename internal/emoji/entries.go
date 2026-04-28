package emoji

import (
	"sort"
	"strings"

	kyoemoji "github.com/kyokomi/emoji/v2"
)

// placeholderGlyph is the single-cell stand-in for image-backed custom
// emojis (which have no displayable Unicode form).
const placeholderGlyph = "□"

// aliasPrefix marks an alias-style custom emoji value, e.g. "alias:thumbsup".
const aliasPrefix = "alias:"

// maxAliasHops caps recursion when resolving chained aliases.
const maxAliasHops = 4

// EmojiEntry is one row in the inline emoji selector.
//
// Name is the shortcode without surrounding colons (e.g. "rocket").
// Display is a single-grapheme preview cell rendered next to the name.
// For built-in and alias-resolved emojis this is the Unicode glyph; for
// image-backed custom emojis it is placeholderGlyph.
type EmojiEntry struct {
	Name    string
	Display string
}

// BuildEntries assembles the searchable emoji list from the kyokomi
// built-in codemap plus the workspace's custom emoji map (as returned by
// Slack's emoji.list, name -> URL-or-"alias:target"). The result is
// deduped (custom shadows built-in) and sorted alphabetically by name.
//
// Pass nil customs for built-ins only.
func BuildEntries(customs map[string]string) []EmojiEntry {
	codemap := kyoemoji.CodeMap()
	byName := make(map[string]EmojiEntry, len(codemap)+len(customs))

	// Built-ins. CodeMap keys are like ":rocket:" and values include a
	// trailing space (kyokomi convention); strip both.
	for code, glyph := range codemap {
		name := strings.Trim(code, ":")
		if name == "" {
			continue
		}
		byName[name] = EmojiEntry{
			Name:    name,
			Display: strings.TrimSpace(glyph),
		}
	}

	// Customs override built-ins of the same name.
	for name, value := range customs {
		if name == "" {
			continue
		}
		byName[name] = EmojiEntry{
			Name:    name,
			Display: resolveCustomDisplay(name, value, customs, codemap),
		}
	}

	out := make([]EmojiEntry, 0, len(byName))
	for _, e := range byName {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// resolveCustomDisplay returns the preview glyph for a custom emoji.
// Alias chains are followed up to maxAliasHops; cycles, dead ends, and
// URL-backed customs all fall back to placeholderGlyph.
func resolveCustomDisplay(name, value string, customs, codemap map[string]string) string {
	if !strings.HasPrefix(value, aliasPrefix) {
		// URL-backed (or anything else we don't understand).
		return placeholderGlyph
	}

	visited := map[string]bool{name: true}
	target := strings.TrimPrefix(value, aliasPrefix)

	for hops := 0; hops < maxAliasHops; hops++ {
		if target == "" {
			return placeholderGlyph
		}
		// A custom emoji of the same name shadows a built-in, so check
		// customs first when following an alias chain. This also makes
		// cycle detection work: without it, "a -> alias:b, b -> alias:a"
		// would short-circuit to the built-in :b: (🅱️) on the first hop.
		if next, ok := customs[target]; ok {
			if visited[target] {
				return placeholderGlyph // cycle
			}
			visited[target] = true
			if !strings.HasPrefix(next, aliasPrefix) {
				// Chain terminates at a URL-backed custom.
				return placeholderGlyph
			}
			target = strings.TrimPrefix(next, aliasPrefix)
			continue
		}
		// No custom override: fall back to the built-in codemap.
		if glyph, ok := codemap[":"+target+":"]; ok {
			return strings.TrimSpace(glyph)
		}
		return placeholderGlyph
	}
	return placeholderGlyph
}
