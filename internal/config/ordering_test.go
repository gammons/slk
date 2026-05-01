package config

import (
	"reflect"
	"testing"

	slack "github.com/gammons/slk/internal/slack"
)

func TestOrderTokens(t *testing.T) {
	tests := []struct {
		name   string
		tokens []slack.Token
		cfg    Config
		want   []string // expected TeamIDs, in order
	}{
		{
			name: "all ordered, distinct values",
			tokens: []slack.Token{
				{TeamID: "T1", TeamName: "Work"},
				{TeamID: "T2", TeamName: "Side"},
				{TeamID: "T3", TeamName: "Oss"},
			},
			cfg: Config{Workspaces: map[string]Workspace{
				"work": {TeamID: "T1", Order: 3},
				"side": {TeamID: "T2", Order: 1},
				"oss":  {TeamID: "T3", Order: 2},
			}},
			want: []string{"T2", "T3", "T1"},
		},
		{
			name: "ties broken by slug alphabetically",
			tokens: []slack.Token{
				{TeamID: "T1"},
				{TeamID: "T2"},
			},
			cfg: Config{Workspaces: map[string]Workspace{
				"zebra": {TeamID: "T1", Order: 1},
				"apple": {TeamID: "T2", Order: 1},
			}},
			want: []string{"T2", "T1"}, // apple before zebra
		},
		{
			name: "ordered, then unordered configured (by slug), then unconfigured (by team id)",
			tokens: []slack.Token{
				{TeamID: "T1"},
				{TeamID: "T2"},
				{TeamID: "T3"},
				{TeamID: "T4"},
				{TeamID: "T5"},
			},
			cfg: Config{Workspaces: map[string]Workspace{
				"work":  {TeamID: "T1", Order: 1},
				"side":  {TeamID: "T2", Order: 2},
				"zebra": {TeamID: "T3"}, // configured but no order
				"apple": {TeamID: "T4"}, // configured but no order
				// T5: not in config at all
			}},
			want: []string{"T1", "T2", "T4", "T3", "T5"},
		},
		{
			name: "explicit order = 0 treated as unordered",
			tokens: []slack.Token{
				{TeamID: "T1"},
				{TeamID: "T2"},
			},
			cfg: Config{Workspaces: map[string]Workspace{
				"first":  {TeamID: "T1", Order: 0},
				"second": {TeamID: "T2", Order: 1},
			}},
			want: []string{"T2", "T1"},
		},
		{
			name: "negative order treated as unordered",
			tokens: []slack.Token{
				{TeamID: "T1"},
				{TeamID: "T2"},
			},
			cfg: Config{Workspaces: map[string]Workspace{
				"a": {TeamID: "T1", Order: -5},
				"b": {TeamID: "T2", Order: 1},
			}},
			want: []string{"T2", "T1"},
		},
		{
			name:   "empty token list returns empty slice",
			tokens: nil,
			cfg:    Config{},
			want:   []string{},
		},
		{
			name: "no config block at all",
			tokens: []slack.Token{
				{TeamID: "T2"},
				{TeamID: "T1"},
			},
			cfg:  Config{},
			want: []string{"T1", "T2"}, // alphabetical by team ID
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OrderTokens(tt.tokens, tt.cfg)
			gotIDs := make([]string, len(got))
			for i, ot := range got {
				gotIDs[i] = ot.Token.TeamID
			}
			if !reflect.DeepEqual(gotIDs, tt.want) {
				t.Errorf("OrderTokens = %v, want %v", gotIDs, tt.want)
			}
		})
	}
}

func TestOrderTokensPreservesSlug(t *testing.T) {
	tokens := []slack.Token{{TeamID: "T1"}}
	cfg := Config{Workspaces: map[string]Workspace{
		"work": {TeamID: "T1", Order: 1},
	}}
	got := OrderTokens(tokens, cfg)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Slug != "work" {
		t.Errorf("Slug = %q, want %q", got[0].Slug, "work")
	}
	if got[0].Order != 1 {
		t.Errorf("Order = %d, want 1", got[0].Order)
	}
}
