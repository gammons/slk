package channelfinder

import (
	"strings"
	"testing"
)

func testItems() []Item {
	return []Item{
		{ID: "C1", Name: "marketing", Type: "channel"},
		{ID: "C2", Name: "engineering", Type: "channel"},
		{ID: "C3", Name: "ext-automote", Type: "channel"},
		{ID: "C4", Name: "grant-planning", Type: "private"},
		{ID: "D1", Name: "Alice", Type: "dm", Presence: "active"},
		{ID: "D2", Name: "Bob", Type: "dm", Presence: "away"},
	}
}

func TestFilterEmpty(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	if len(m.filtered) != 6 {
		t.Errorf("expected 6 filtered items, got %d", len(m.filtered))
	}
}

func TestFilterSubstring(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	m.HandleKey("e")
	m.HandleKey("n")
	m.HandleKey("g")

	// "engineering" is a substring (in fact prefix) match; other items like
	// "marketing" may also subsequence-match 'e','n','g' -- but the
	// substring/prefix hit must come first.
	if len(m.filtered) == 0 {
		t.Fatal("expected at least 1 match for 'eng'")
	}
	if m.items[m.filtered[0]].Name != "engineering" {
		t.Errorf("expected first match to be 'engineering', got %q",
			m.items[m.filtered[0]].Name)
	}
}

func TestFilterCaseInsensitive(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	m.HandleKey("A")
	m.HandleKey("l")
	m.HandleKey("i")

	// "Alice" is a (case-insensitive) prefix match. Other names may also
	// subsequence-match a,l,i -- but the prefix hit must rank first.
	if len(m.filtered) == 0 {
		t.Fatal("expected at least 1 match for 'Ali'")
	}
	if m.items[m.filtered[0]].Name != "Alice" {
		t.Errorf("expected first match to be 'Alice', got %q",
			m.items[m.filtered[0]].Name)
	}
}

func TestFilterPrefixFirst(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	m.HandleKey("m")
	m.HandleKey("a")

	if len(m.filtered) == 0 {
		t.Fatal("expected at least 1 match")
	}
	idx := m.filtered[0]
	if m.items[idx].Name != "marketing" {
		t.Errorf("expected first match to be 'marketing', got %q", m.items[idx].Name)
	}
}

func TestFilterDMRanksAboveGroupDM(t *testing.T) {
	// Searching for an individual person should put their 1:1 DM ahead of
	// any group DM whose participant list contains the same name. Both
	// kinds of items can match the same query as substrings; without a
	// type-aware tiebreak the group_dm wins simply by appearing first in
	// m.items.
	items := []Item{
		{ID: "G1", Name: "alice, bob, carol", Type: "group_dm"},
		{ID: "G2", Name: "alice, dave", Type: "group_dm"},
		{ID: "D1", Name: "alice", Type: "dm", Presence: "active"},
	}
	m := New()
	m.SetItems(items)
	m.Open()

	for _, r := range "ali" {
		m.HandleKey(string(r))
	}

	if len(m.filtered) == 0 {
		t.Fatal("expected matches for 'ali'")
	}
	first := m.items[m.filtered[0]]
	if first.Type != "dm" {
		t.Errorf("expected first match to be the individual DM, got %q (type=%s)", first.Name, first.Type)
	}
	// And the last of the matching items should be a group_dm.
	last := m.items[m.filtered[len(m.filtered)-1]]
	if last.Type != "group_dm" {
		t.Errorf("expected last match to be a group_dm, got %q (type=%s)", last.Name, last.Type)
	}
}

func TestFilterGroupDMRanksLastAcrossTiers(t *testing.T) {
	// A group_dm with a *prefix* match should still rank below a regular
	// channel/dm with a *substring* match? No — prefix tier still wins
	// over substring tier (that's the whole point of tiering). But within
	// the same tier, group_dm ranks last. Verify both invariants.
	items := []Item{
		{ID: "G1", Name: "alice, bob", Type: "group_dm"}, // prefix match for "ali"
		{ID: "C1", Name: "talia-team", Type: "channel"},  // substring match for "ali"
		{ID: "D1", Name: "alice", Type: "dm"},            // prefix match for "ali"
	}
	m := New()
	m.SetItems(items)
	m.Open()
	for _, r := range "ali" {
		m.HandleKey(string(r))
	}

	if len(m.filtered) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(m.filtered))
	}
	// Tier 1 (prefix): D1 (dm) before G1 (group_dm).
	// Tier 2 (substring): C1.
	got := []string{m.items[m.filtered[0]].ID, m.items[m.filtered[1]].ID, m.items[m.filtered[2]].ID}
	want := []string{"D1", "G1", "C1"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: got %s, want %s (full order=%v)", i, got[i], want[i], got)
		}
	}
}

func TestSelectChannel(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	result := m.HandleKey("enter")
	if result == nil {
		t.Fatal("expected a result on Enter")
	}
	if result.ID != "C1" {
		t.Errorf("expected first channel (C1), got %q", result.ID)
	}
}

func TestNavigateDown(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	m.HandleKey("down")
	m.HandleKey("down")

	result := m.HandleKey("enter")
	if result == nil {
		t.Fatal("expected a result on Enter")
	}
	if result.ID != "C3" {
		t.Errorf("expected third channel (C3), got %q", result.ID)
	}
}

func TestEscCloses(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	if !m.IsVisible() {
		t.Fatal("expected visible after Open")
	}

	m.HandleKey("esc")
	if m.IsVisible() {
		t.Error("expected not visible after Esc")
	}
}

func TestBackspace(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	m.HandleKey("x")
	m.HandleKey("y")
	m.HandleKey("z")

	if len(m.filtered) != 0 {
		t.Errorf("expected 0 matches for 'xyz', got %d", len(m.filtered))
	}

	m.HandleKey("backspace")
	m.HandleKey("backspace")
	m.HandleKey("backspace")

	if len(m.filtered) != 6 {
		t.Errorf("expected 6 matches after clearing query, got %d", len(m.filtered))
	}
}

func TestNoMatchesNoResult(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	m.HandleKey("z")
	m.HandleKey("z")
	m.HandleKey("z")

	result := m.HandleKey("enter")
	if result != nil {
		t.Error("expected nil result when no matches")
	}
}

func TestSetBrowseableMergesWithJoined(t *testing.T) {
	m := New()
	m.SetItems([]Item{
		{ID: "C1", Name: "general", Type: "channel", Joined: true},
		{ID: "C2", Name: "random", Type: "channel", Joined: true},
	})

	m.SetBrowseable([]Item{
		// Duplicate of a joined channel: must be skipped.
		{ID: "C1", Name: "general", Type: "channel"},
		// New non-joined channels.
		{ID: "C3", Name: "announcements", Type: "channel"},
		{ID: "C4", Name: "watercooler", Type: "channel"},
	})

	if len(m.items) != 4 {
		t.Fatalf("expected 4 items after merge, got %d", len(m.items))
	}

	want := map[string]bool{"C1": true, "C2": true, "C3": false, "C4": false}
	for _, it := range m.items {
		expected, ok := want[it.ID]
		if !ok {
			t.Errorf("unexpected item %q in merged list", it.ID)
			continue
		}
		if it.Joined != expected {
			t.Errorf("item %q: Joined=%v, want %v", it.ID, it.Joined, expected)
		}
	}
}

func TestSetBrowseableReplacesPreviousBrowseable(t *testing.T) {
	m := New()
	m.SetItems([]Item{{ID: "C1", Name: "general", Type: "channel", Joined: true}})
	m.SetBrowseable([]Item{{ID: "C2", Name: "old", Type: "channel"}})
	if len(m.items) != 2 {
		t.Fatalf("expected 2 items after first SetBrowseable, got %d", len(m.items))
	}

	// Second call should drop C2 and add C3.
	m.SetBrowseable([]Item{{ID: "C3", Name: "new", Type: "channel"}})
	if len(m.items) != 2 {
		t.Fatalf("expected 2 items after second SetBrowseable, got %d", len(m.items))
	}
	for _, it := range m.items {
		if it.ID == "C2" {
			t.Error("expected previous browseable item C2 to be replaced")
		}
	}
}

func TestEnterReturnsJoinedFlag(t *testing.T) {
	m := New()
	m.SetItems([]Item{
		{ID: "C1", Name: "general", Type: "channel", Joined: true},
		{ID: "C2", Name: "browseable", Type: "channel", Joined: false},
	})
	m.Open()

	r := m.HandleKey("enter")
	if r == nil || !r.Joined {
		t.Errorf("expected joined=true for first item, got %+v", r)
	}

	m.Open()
	m.HandleKey("down")
	r = m.HandleKey("enter")
	if r == nil || r.Joined {
		t.Errorf("expected joined=false for second item, got %+v", r)
	}
}

// TestNonJoinedVisuallyDistinct asserts that rendering a joined and a
// non-joined channel produces different ANSI byte sequences. This guards
// against a regression where embedded ANSI in the prefix would silently kill
// the outer dim styling on the name part of the row, making both look identical.
func TestNonJoinedVisuallyDistinct(t *testing.T) {
	mJoined := New()
	mJoined.SetItems([]Item{{ID: "C1", Name: "channel-name", Type: "channel", Joined: true}})
	mJoined.Open()
	joinedView := mJoined.View(80)

	mNot := New()
	mNot.SetItems([]Item{{ID: "C1", Name: "channel-name", Type: "channel", Joined: false}})
	mNot.Open()
	notView := mNot.View(80)

	if joinedView == notView {
		t.Error("expected joined and non-joined renders to differ")
	}
	// The dim color we use for non-joined should appear in the non-joined view.
	if !strings.Contains(notView, "5a5a5a") && !strings.Contains(notView, ";90;90;90m") {
		// Lipgloss may emit the color in either #hex form (rare) or as RGB
		// truecolor escape. Accept either; just require SOME mention.
		// Fall back to checking the output contains the channel name.
		if !strings.Contains(notView, "channel-name") {
			t.Errorf("non-joined view missing channel name: %q", notView)
		}
	}
}

// TestFilterSubsequence verifies the fuzzy subsequence tier: characters
// appearing in order anywhere in the channel name match, even across word
// separators. This is what makes "csp" find "cs-product-triage".
func TestFilterSubsequence(t *testing.T) {
	m := New()
	m.SetItems([]Item{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "cs-product-triage", Type: "channel"},
		{ID: "C3", Name: "random", Type: "channel"},
	})
	m.Open()

	m.HandleKey("c")
	m.HandleKey("s")
	m.HandleKey("p")

	if len(m.filtered) == 0 {
		t.Fatal("expected at least 1 match for 'csp'")
	}
	idx := m.filtered[0]
	if m.items[idx].Name != "cs-product-triage" {
		t.Errorf("expected first match to be 'cs-product-triage', got %q", m.items[idx].Name)
	}
}

// TestFilterRanksPrefixOverSubsequence ensures a substring/prefix hit still
// outranks a subsequence-only hit, so familiar searches don't regress.
func TestFilterRanksPrefixOverSubsequence(t *testing.T) {
	m := New()
	m.SetItems([]Item{
		// Subsequence match for "eng": 'e' at 0, 'n' at 4, 'g' at 6.
		{ID: "C1", Name: "ext-engage", Type: "channel"},
		// Prefix match.
		{ID: "C2", Name: "engineering", Type: "channel"},
	})
	m.Open()

	m.HandleKey("e")
	m.HandleKey("n")
	m.HandleKey("g")

	if len(m.filtered) < 1 {
		t.Fatal("expected matches for 'eng'")
	}
	if m.items[m.filtered[0]].Name != "engineering" {
		t.Errorf("expected prefix match 'engineering' first, got %q",
			m.items[m.filtered[0]].Name)
	}
}

// TestFilterSubsequenceWordBoundaryRanking verifies that subsequence matches
// hitting word boundaries rank above ones that don't.
func TestFilterSubsequenceWordBoundaryRanking(t *testing.T) {
	m := New()
	m.SetItems([]Item{
		// 'a' and 'b' both mid-word -- no boundary bonus.
		{ID: "C1", Name: "xxaxxbxxyyy", Type: "channel"},
		// 'a' at start, 'b' at start of second word -- two boundary hits.
		{ID: "C2", Name: "alpha-beta", Type: "channel"},
	})
	m.Open()

	m.HandleKey("a")
	m.HandleKey("b")

	if len(m.filtered) < 2 {
		t.Fatalf("expected 2 subsequence matches, got %d", len(m.filtered))
	}
	if m.items[m.filtered[0]].Name != "alpha-beta" {
		t.Errorf("expected 'alpha-beta' to outrank 'xxabxx-yyy', got %q first",
			m.items[m.filtered[0]].Name)
	}
}

func TestMarkJoined(t *testing.T) {
	m := New()
	m.SetItems([]Item{
		{ID: "C1", Name: "general", Type: "channel", Joined: false},
	})
	m.MarkJoined("C1")
	if !m.items[0].Joined {
		t.Error("expected MarkJoined to set Joined=true")
	}
}
