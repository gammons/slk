package config

import (
	"sort"

	slack "github.com/gammons/slk/internal/slack"
)

// OrderedToken is a token paired with its config-derived ordering metadata.
// Slug is the [workspaces.<slug>] key from config, or "" if the workspace
// has no config block. Order is the value of the `order` field, or 0
// when unset/non-positive.
type OrderedToken struct {
	Token slack.Token
	Slug  string
	Order int
}

// OrderTokens returns the tokens sorted into a stable, user-configurable
// order:
//
//  1. Configured with Order > 0 — sorted ascending by Order, ties
//     broken alphabetically by slug.
//  2. Configured but Order <= 0 — sorted alphabetically by slug.
//  3. Not in config at all — sorted alphabetically by TeamID.
//
// Bucket boundaries are stable: any bucket-1 entry precedes every
// bucket-2 entry, which precedes every bucket-3 entry.
func OrderTokens(tokens []slack.Token, cfg Config) []OrderedToken {
	if len(tokens) == 0 {
		return nil
	}

	// Build a TeamID -> (slug, Workspace) lookup.
	type cfgEntry struct {
		slug string
		ws   Workspace
	}
	byTeam := make(map[string]cfgEntry, len(cfg.Workspaces))
	for slug, ws := range cfg.Workspaces {
		if ws.TeamID != "" {
			byTeam[ws.TeamID] = cfgEntry{slug: slug, ws: ws}
		}
	}

	out := make([]OrderedToken, 0, len(tokens))
	for _, tok := range tokens {
		ot := OrderedToken{Token: tok}
		if entry, ok := byTeam[tok.TeamID]; ok {
			ot.Slug = entry.slug
			if entry.ws.Order > 0 {
				ot.Order = entry.ws.Order
			}
		}
		out = append(out, ot)
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		ba, bb := bucket(a), bucket(b)
		if ba != bb {
			return ba < bb
		}
		switch ba {
		case 1:
			if a.Order != b.Order {
				return a.Order < b.Order
			}
			return a.Slug < b.Slug
		case 2:
			return a.Slug < b.Slug
		default: // 3
			return a.Token.TeamID < b.Token.TeamID
		}
	})
	return out
}

// bucket returns 1, 2, or 3 per the rules in OrderTokens.
func bucket(o OrderedToken) int {
	if o.Slug != "" && o.Order > 0 {
		return 1
	}
	if o.Slug != "" {
		return 2
	}
	return 3
}
