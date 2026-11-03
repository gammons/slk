package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/core"
	"github.com/slack-go/slack"
)

func writeTestConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func readTestConfig(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// loadRules loads path with config.Load (so the file must still be
// valid TOML) and returns teamID's rules.
func loadRules(t *testing.T, path, teamID string) *core.Suppression {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load after edit: %v\n%s", err, readTestConfig(t, path))
	}
	w, _ := cfg.WorkspaceByTeamID(teamID)
	return w.SuppressRules()
}

func TestAppendSuppressRuleToSlugBlock(t *testing.T) {
	const orig = `# my config
[appearance]
theme = "dracula"

# Acme
[workspaces.acme]
team_id = "T01ACME"
theme = "nord"
`
	path := writeTestConfig(t, orig)
	if err := appendSuppressRule(path, "acme", "T01ACME", "Acme", core.SuppressRule{AuthorID: "B1", Note: "Deploy\nBot"}); err != nil {
		t.Fatal(err)
	}
	if err := appendSuppressRule(path, "acme", "T01ACME", "Acme", core.SuppressRule{AuthorID: "U2", ChannelID: "C3"}); err != nil {
		t.Fatal(err)
	}
	got := readTestConfig(t, path)
	if !strings.HasPrefix(got, strings.TrimRight(orig, "\n")) {
		t.Errorf("existing content changed:\n%s", got)
	}
	if strings.Count(got, "[workspaces.acme]") != 1 {
		t.Errorf("an existing workspace block must not be added again:\n%s", got)
	}
	s := loadRules(t, path, "T01ACME")
	if s.Len() != 2 || !s.Hides("C9", "B1") || !s.Hides("C3", "U2") || s.Hides("C9", "U2") {
		t.Errorf("rules = %+v", s.Rules())
	}
	if s.Rules()[0].Note != "Deploy Bot" {
		t.Errorf("note = %q, want newline sanitised", s.Rules()[0].Note)
	}
}

func TestAppendSuppressRuleCreatesFileAndBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.toml")
	if err := appendSuppressRule(path, "T01ACME", "T01ACME", "Acme", core.SuppressRule{AuthorID: "U1"}); err != nil {
		t.Fatal(err)
	}
	got := readTestConfig(t, path)
	if !strings.Contains(got, "# Acme\n[workspaces.T01ACME]\n") {
		t.Errorf("want a commented workspace block first:\n%s", got)
	}
	if s := loadRules(t, path, "T01ACME"); !s.Hides("C1", "U1") {
		t.Errorf("rules = %+v", s.Rules())
	}
	// The theme saver must still find the block it would write to.
	if err := saveWorkspaceTheme(path, "T01ACME", "T01ACME", "Acme", "nord"); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(readTestConfig(t, path), "[workspaces.T01ACME]"); n != 1 {
		t.Errorf("theme save added a second block (%d)", n)
	}
	loadRules(t, path, "T01ACME")
}

func TestRemoveSuppressRule(t *testing.T) {
	path := writeTestConfig(t, `[workspaces.acme]
team_id = "T01ACME"

[[workspaces.acme.suppress]]
author = "B1"

[[workspaces.acme.suppress]]
author = "B1"
channel = "C1" # only here

# Next workspace
[workspaces.T02OTHER]
theme = "nord"
`)
	removed, err := removeSuppressRule(path, "acme", core.SuppressRule{AuthorID: "B1", ChannelID: "C1"})
	if err != nil || !removed {
		t.Fatalf("removed=%v err=%v", removed, err)
	}
	got := readTestConfig(t, path)
	if strings.Contains(got, "C1") {
		t.Errorf("channel rule still present:\n%s", got)
	}
	if !strings.Contains(got, "# Next workspace\n[workspaces.T02OTHER]") {
		t.Errorf("the next block's comment must stay with it:\n%s", got)
	}
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("removal left a gap:\n%s", got)
	}
	s := loadRules(t, path, "T01ACME")
	if s.Len() != 1 || !s.Hides("C5", "B1") {
		t.Errorf("rules = %+v", s.Rules())
	}

	removed, err = removeSuppressRule(path, "acme", core.SuppressRule{AuthorID: "nobody"})
	if err != nil || removed {
		t.Errorf("absent rule: removed=%v err=%v", removed, err)
	}
}

func TestSuppressStoreAddRemove(t *testing.T) {
	path := writeTestConfig(t, "[[workspaces.T01ACME.suppress]]\nauthor = \"B1\"\n")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	key := func(teamID string) string { return teamID }
	name := func(string) string { return "Acme" }
	s := newSuppressStore(cfg, path, key, name)
	if !s.Hides("T01ACME", "C1", "B1") || s.Hides("T02", "C1", "B1") {
		t.Fatal("seeded rule should apply to its workspace only")
	}
	before := s.Rules("T01ACME")
	rule := core.SuppressRule{AuthorID: "U2", ChannelID: "C1"}
	if err := s.Add("T01ACME", rule); err != nil {
		t.Fatal(err)
	}
	if before.Has(rule) {
		t.Error("Add must replace the set, not change the old one")
	}
	if !s.Hides("T01ACME", "C1", "U2") || !loadRules(t, path, "T01ACME").Has(rule) {
		t.Error("added rule not applied or not saved")
	}
	if err := s.Add("T01ACME", rule); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(readTestConfig(t, path), `author = "U2"`); n != 1 {
		t.Errorf("adding a held rule again wrote it %d times", n)
	}
	if err := s.Remove("T01ACME", rule); err != nil {
		t.Fatal(err)
	}
	if s.Hides("T01ACME", "C1", "U2") || loadRules(t, path, "T01ACME").Has(rule) {
		t.Error("removed rule still applied or saved")
	}
}

func TestSuppressStoreRemoveInlineRuleRefuses(t *testing.T) {
	path := writeTestConfig(t, "[workspaces.T01ACME]\nsuppress = [{ author = \"B1\" }]\n")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s := newSuppressStore(cfg, path, func(id string) string { return id }, func(string) string { return "" })
	err = s.Remove("T01ACME", core.SuppressRule{AuthorID: "B1"})
	if !errors.Is(err, errSuppressRuleNotInFile) {
		t.Fatalf("err = %v, want errSuppressRuleNotInFile", err)
	}
	if !s.Hides("T01ACME", "C1", "B1") {
		t.Error("a rule that could not be removed from the file must stay applied")
	}
}

// A suppressed author's message is cached, but neither marks its
// channel unread nor bumps the mention badge; another author's does.
func TestOnMessageSuppressedAuthorDoesNotMarkUnread(t *testing.T) {
	for _, tc := range []struct {
		name, user, bot string
		wantUnread      bool
	}{
		{"suppressed user", "UNOISY", "", false},
		{"suppressed bot", "", "BNOISY", false},
		{"other author", "UOTHER", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, db := onMessageMentionFixture(t, "channel", "")
			set := core.NewSuppression([]core.SuppressRule{{AuthorID: "UNOISY", ChannelID: "C1"}, {AuthorID: "BNOISY"}})
			h.suppressed = set.Hides
			h.OnMessage("C1", tc.user, "100.0", "<!here> hi", "", "", false, false, nil, slack.Blocks{}, nil, tc.bot, "")
			state, err := db.GetChannelReadState("C1")
			if err != nil {
				t.Fatal(err)
			}
			if state.HasUnread != tc.wantUnread {
				t.Errorf("HasUnread = %v, want %v", state.HasUnread, tc.wantUnread)
			}
			if wantBadge := map[bool]int{true: 1, false: 0}[tc.wantUnread]; state.MentionCount != wantBadge {
				t.Errorf("MentionCount = %d, want %d", state.MentionCount, wantBadge)
			}
			if msgs, err := db.GetMessages("C1", 10, ""); err != nil || len(msgs) != 1 {
				t.Errorf("message should still be cached: %d, %v", len(msgs), err)
			}
		})
	}
}
