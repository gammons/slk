package mentionpicker

import "testing"

// ids returns the filtered IDs in display order, for compact assertions.
func ids(m Model) []string {
	out := make([]string, 0, len(m.Filtered()))
	for _, u := range m.Filtered() {
		out = append(out, u.ID)
	}
	return out
}

// TestFilterMatchesWordInsideName: Slack's composer finds @eng-widgets
// from "widg". Whole-name prefix matching alone forced users to know
// the leading word.
func TestFilterMatchesWordInsideName(t *testing.T) {
	tests := []struct {
		query string
		want  bool
	}{
		{"eng", true},        // whole-name prefix
		{"eng-widg", true},   // whole-name prefix spanning a separator
		{"widg", true},       // word-boundary prefix
		{"widgets", true},    // whole trailing word
		{"engwidgets", true}, // separators removed
		{"engwidg", true},    // separators removed, partial
		{"idget", false},     // mid-word: Slack doesn't match this either
		{"xyz", false},
	}
	for _, tt := range tests {
		m := New()
		m.SetUsers([]User{
			{ID: "usergroup:S1", DisplayName: "eng-widgets", Username: "eng-widgets", InChannel: true},
		})
		m.Open()
		m.SetQuery(tt.query)
		got := len(m.Filtered()) == 1
		if got != tt.want {
			t.Errorf("query %q: matched = %v, want %v", tt.query, got, tt.want)
		}
	}
}

// TestFilterSeparatorVariants: spaces, dots and underscores split words
// the same way hyphens do.
func TestFilterSeparatorVariants(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{"Ada Lovelace", "love"},
		{"ada.lovelace", "love"},
		{"ada_lovelace", "love"},
		{"ada-lovelace", "love"},
		{"Ada Lovelace", "adalovelace"},
	}
	for _, tt := range tests {
		m := New()
		m.SetUsers([]User{{ID: "U1", DisplayName: tt.name, Username: "u", InChannel: true}})
		m.Open()
		m.SetQuery(tt.query)
		if len(m.Filtered()) != 1 {
			t.Errorf("name %q, query %q: got %d matches, want 1", tt.name, tt.query, len(m.Filtered()))
		}
	}
}

// TestFilterRanksWholeNamePrefixFirst: a user typing "widg" wants
// @widgets-lead before @eng-widgets, even though both match.
func TestFilterRanksWholeNamePrefixFirst(t *testing.T) {
	m := New()
	m.SetUsers([]User{
		{ID: "U1", DisplayName: "eng-widgets", Username: "eng-widgets", InChannel: true},
		{ID: "U2", DisplayName: "widgets-lead", Username: "widgets-lead", InChannel: true},
	})
	m.Open()
	m.SetQuery("widg")
	want := []string{"U2", "U1"}
	got := ids(m)
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("order = %v, want %v", got, want)
	}
}

// TestFilterOrdersEqualDisplayNamesByID guards sortRanked's final
// tie-break: sort.Slice is unstable, so input order must not leak into the
// picker when two candidates share a display name and match rank.
func TestFilterOrdersEqualDisplayNamesByID(t *testing.T) {
	m := New()
	m.SetUsers([]User{
		{ID: "U2", DisplayName: "Alex", Username: "alex-two", InChannel: true},
		{ID: "U1", DisplayName: "Alex", Username: "alex-one", InChannel: true},
	})
	m.Open()
	m.SetQuery("alex")

	got := ids(m)
	if len(got) != 2 || got[0] != "U1" || got[1] != "U2" {
		t.Errorf("order = %v, want [U1 U2]", got)
	}
}

// TestFilterMembershipTierBeatsRank: rank orders candidates within a
// tier; it must not promote a not-in-channel user above an in-channel one.
func TestFilterMembershipTierBeatsRank(t *testing.T) {
	m := New()
	m.SetUsers([]User{
		{ID: "U1", DisplayName: "eng-widgets", Username: "eng-widgets", InChannel: true},
		{ID: "U2", DisplayName: "widgets-lead", Username: "widgets-lead", InChannel: false},
	})
	m.Open()
	m.SetQuery("widg")
	got := ids(m)
	if len(got) != 2 || got[0] != "U1" {
		t.Errorf("order = %v, want in-channel U1 first", got)
	}
}

// TestFilterAccentInsensitiveWordMatch: folding applies to word matches
// too, not just whole-name prefixes.
func TestFilterAccentInsensitiveWordMatch(t *testing.T) {
	m := New()
	m.SetUsers([]User{{ID: "U1", DisplayName: "Team Mélanie", Username: "tm", InChannel: true}})
	m.Open()
	m.SetQuery("mela")
	if len(m.Filtered()) != 1 {
		t.Errorf("got %d matches, want 1", len(m.Filtered()))
	}
}

func TestMatchNameRanks(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  matchRank
	}{
		{"eng-widgets", "eng", rankPrefix},
		{"eng-widgets", "eng-widg", rankPrefix},
		{"eng-widgets", "widg", rankWord},
		{"eng-widgets", "engwidgets", rankSquashed},
		{"eng-widgets", "idget", rankNone},
		{"eng-widgets", "", rankPrefix},
	}
	for _, tt := range tests {
		if got := matchName(tt.name, tt.query, squash(tt.query)); got != tt.want {
			t.Errorf("matchName(%q, %q) = %v, want %v", tt.name, tt.query, got, tt.want)
		}
	}
}

// TestSquashLeavesSeparatorlessStringsAlone guards the fast path.
func TestSquashLeavesSeparatorlessStringsAlone(t *testing.T) {
	if got := squash("alice"); got != "alice" {
		t.Errorf("squash = %q, want alice", got)
	}
	if got := squash("a b-c_d.e"); got != "abcde" {
		t.Errorf("squash = %q, want abcde", got)
	}
}

// TestMatchNameTrailingSeparator: a name ending in a separator must not
// index past the end of the string.
func TestMatchNameTrailingSeparator(t *testing.T) {
	if got := matchName("team-", "x", "x"); got != rankNone {
		t.Errorf("matchName = %v, want rankNone", got)
	}
	if got := matchName("team--lead", "lead", "lead"); got != rankWord {
		t.Errorf("matchName = %v, want rankWord", got)
	}
}
