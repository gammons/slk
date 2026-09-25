package newmessagepicker

import (
	"fmt"
	"testing"
)

func testUsers() []User {
	return []User{
		{ID: "U1", DisplayName: "Alice Chen", Username: "alice", Recency: 500},
		{ID: "U2", DisplayName: "Bob Singh", Username: "bob", Recency: 400},
		{ID: "U3", DisplayName: "Carla Diaz", Username: "carla", Recency: 300},
		{ID: "U4", DisplayName: "Dan Evans", Username: "dan", Recency: 200},
		{ID: "U5", DisplayName: "Eva Frank", Username: "eva", IsExternal: true, Recency: 100},
	}
}

func TestNew_NotVisibleByDefault(t *testing.T) {
	m := New()
	if m.IsVisible() {
		t.Error("expected new model to not be visible")
	}
}

func TestOpen_MakesVisible(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	if !m.IsVisible() {
		t.Error("expected Open() to make model visible")
	}
}

func TestClose_HidesModel(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.Close()
	if m.IsVisible() {
		t.Error("expected Close() to hide model")
	}
}

func TestOpen_ResetsState(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	// Simulate dirty state from a previous session.
	m.query = "old query"
	m.selected["U1"] = struct{}{}
	m.highlight = 3

	m.Close()
	m.Open()

	if m.query != "" {
		t.Errorf("expected empty query after Open, got %q", m.query)
	}
	if len(m.selected) != 0 {
		t.Errorf("expected empty selection after Open, got %d entries", len(m.selected))
	}
	if m.highlight != 0 {
		t.Errorf("expected highlight=0 after Open, got %d", m.highlight)
	}
}

func TestSetCurrentUserID_ExcludesSelfFromList(t *testing.T) {
	users := testUsers()
	m := New()
	m.SetCurrentUserID("U2") // Bob is "self"
	m.SetUsers(users)
	m.Open()

	for _, idx := range m.filtered {
		if m.users[idx].ID == "U2" {
			t.Error("self user U2 should not appear in filtered list")
		}
	}
}

func TestFilter_EmptyQuerySortsByRecencyDesc(t *testing.T) {
	m := New()
	m.SetUsers(testUsers()) // Alice=500, Bob=400, Carla=300, Dan=200, Eva=100
	m.Open()

	if len(m.filtered) != 5 {
		t.Fatalf("expected 5 users, got %d", len(m.filtered))
	}
	wantOrder := []string{"U1", "U2", "U3", "U4", "U5"}
	for i, want := range wantOrder {
		got := m.users[m.filtered[i]].ID
		if got != want {
			t.Errorf("position %d: want %s, got %s", i, want, got)
		}
	}
}

func TestFilter_EmptyQueryTieBreaksAlphabetically(t *testing.T) {
	users := []User{
		{ID: "U1", DisplayName: "Charlie", Username: "c", Recency: 100},
		{ID: "U2", DisplayName: "Alice", Username: "a", Recency: 100},
		{ID: "U3", DisplayName: "Bob", Username: "b", Recency: 100},
	}
	m := New()
	m.SetUsers(users)
	m.Open()

	wantOrder := []string{"U2", "U3", "U1"} // Alice, Bob, Charlie
	for i, want := range wantOrder {
		got := m.users[m.filtered[i]].ID
		if got != want {
			t.Errorf("position %d: want %s, got %s", i, want, got)
		}
	}
}

func TestFilter_PrefixBeatsSubstring(t *testing.T) {
	users := []User{
		{ID: "U1", DisplayName: "Marcus", Username: "marcus", Recency: 100},
		{ID: "U2", DisplayName: "Alice Marketing", Username: "amark", Recency: 999},
	}
	m := New()
	m.SetUsers(users)
	m.Open()
	m.setQuery("mar")

	if len(m.filtered) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(m.filtered))
	}
	if m.users[m.filtered[0]].ID != "U1" {
		t.Errorf("prefix match should come first, got %s", m.users[m.filtered[0]].ID)
	}
}

func TestFilter_SubstringBeatsSubsequence(t *testing.T) {
	users := []User{
		{ID: "U1", DisplayName: "Stephanie", Username: "steph", Recency: 100},    // contains "eph"
		{ID: "U2", DisplayName: "Edward Phillips", Username: "ep", Recency: 999}, // subseq e-p-h
	}
	m := New()
	m.SetUsers(users)
	m.Open()
	m.setQuery("eph")

	if m.filtered[0] != 0 {
		t.Errorf("substring match should rank first, got user at index %d", m.filtered[0])
	}
}

func TestFilter_CaseInsensitive(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.setQuery("ALICE")

	if len(m.filtered) == 0 {
		t.Fatal("expected at least 1 match for ALICE")
	}
	if m.users[m.filtered[0]].ID != "U1" {
		t.Errorf("expected Alice (U1) as first match, got %s", m.users[m.filtered[0]].ID)
	}
}

func TestFilter_MatchesUsernameHandle(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.setQuery("dan") // Dan Evans has Username="dan"

	if len(m.filtered) == 0 {
		t.Fatal("expected match for handle 'dan'")
	}
	if m.users[m.filtered[0]].ID != "U4" {
		t.Errorf("expected Dan (U4) as first match, got %s", m.users[m.filtered[0]].ID)
	}
}

func TestFilter_NoMatchesReturnsEmpty(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.setQuery("xyzqq")

	if len(m.filtered) != 0 {
		t.Errorf("expected 0 matches for unmatchable query, got %d", len(m.filtered))
	}
}

func TestFilter_ExcludesSelfEvenOnMatch(t *testing.T) {
	users := testUsers()
	m := New()
	m.SetCurrentUserID("U1") // Alice is self
	m.SetUsers(users)
	m.Open()
	m.setQuery("alice")

	for _, idx := range m.filtered {
		if m.users[idx].ID == "U1" {
			t.Error("self user should be excluded even when query matches")
		}
	}
}

func TestFilter_RecencyTieBreaksWithinSameTier(t *testing.T) {
	users := []User{
		{ID: "U1", DisplayName: "alice older", Username: "a1", Recency: 100},
		{ID: "U2", DisplayName: "alice newer", Username: "a2", Recency: 999},
	}
	m := New()
	m.SetUsers(users)
	m.Open()
	m.setQuery("alice") // both prefix-match

	if m.users[m.filtered[0]].ID != "U2" {
		t.Errorf("higher-recency match should come first, got %s", m.users[m.filtered[0]].ID)
	}
}

func TestHandleKey_DownMovesHighlight(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	// 5 users, highlight starts at 0.
	m.HandleKey("down")
	if m.highlight != 1 {
		t.Errorf("expected highlight=1 after down, got %d", m.highlight)
	}
}

func TestHandleKey_UpMovesHighlight(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.HandleKey("down")
	m.HandleKey("down")
	m.HandleKey("up")
	if m.highlight != 1 {
		t.Errorf("expected highlight=1 after down,down,up, got %d", m.highlight)
	}
}

func TestHandleKey_DownClampsAtEnd(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	for i := 0; i < 20; i++ {
		m.HandleKey("down")
	}
	if m.highlight != len(m.filtered)-1 {
		t.Errorf("expected highlight clamped at %d, got %d", len(m.filtered)-1, m.highlight)
	}
}

func TestHandleKey_UpClampsAtStart(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.HandleKey("up")
	if m.highlight != 0 {
		t.Errorf("expected highlight=0 clamped, got %d", m.highlight)
	}
}

func TestHandleKey_CtrlNAndCtrlPAliasNavigation(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.HandleKey("ctrl+n")
	if m.highlight != 1 {
		t.Errorf("ctrl+n should be alias for down")
	}
	m.HandleKey("ctrl+p")
	if m.highlight != 0 {
		t.Errorf("ctrl+p should be alias for up")
	}
}

func TestHandleKey_PrintableRuneAppendsToQueryAndRefilters(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.HandleKey("a")
	m.HandleKey("l")
	m.HandleKey("i")
	if m.query != "ali" {
		t.Errorf("expected query=ali, got %q", m.query)
	}
	if len(m.filtered) == 0 || m.users[m.filtered[0]].ID != "U1" {
		t.Errorf("expected Alice (U1) first, got %v", m.filtered)
	}
}

func TestHandleKey_BackspaceRemovesLastRune(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.HandleKey("a")
	m.HandleKey("l")
	m.HandleKey("backspace")
	if m.query != "a" {
		t.Errorf("expected query=a after backspace, got %q", m.query)
	}
}

func TestHandleKey_SpaceTogglesHighlightedUserIntoSelection(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	// highlight starts at 0 -> Alice (U1)

	m.HandleKey(" ")
	if _, ok := m.selected["U1"]; !ok {
		t.Error("expected U1 in selection after space")
	}
	if len(m.selected) != 1 {
		t.Errorf("expected 1 selection, got %d", len(m.selected))
	}
}

func TestHandleKey_TabAliasesSpace(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.HandleKey("tab")
	if _, ok := m.selected["U1"]; !ok {
		t.Error("expected U1 in selection after tab")
	}
}

func TestHandleKey_SpaceOnSelectedRemovesIt(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.HandleKey(" ")
	m.HandleKey(" ")
	if _, ok := m.selected["U1"]; ok {
		t.Error("expected U1 to be removed after second space")
	}
}

func TestHandleKey_SpaceCapsAtMaxRecipients(t *testing.T) {
	users := make([]User, 10)
	for i := range users {
		users[i] = User{
			ID:          fmt.Sprintf("U%d", i),
			DisplayName: fmt.Sprintf("User %d", i),
			Username:    fmt.Sprintf("u%d", i),
		}
	}
	m := New()
	m.SetUsers(users)
	m.Open()
	// Select 8 users by hitting space then down 8 times.
	for i := 0; i < 8; i++ {
		m.HandleKey(" ")
		m.HandleKey("down")
	}
	if len(m.selected) != 8 {
		t.Fatalf("expected 8 selections, got %d", len(m.selected))
	}
	// 9th selection attempt must be a no-op.
	m.HandleKey(" ")
	if len(m.selected) != 8 {
		t.Errorf("expected selection to be capped at 8, got %d", len(m.selected))
	}
}

func TestHandleKey_BackspaceAtEmptyQueryRemovesLastPill(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.HandleKey(" ")    // select U1
	m.HandleKey("down") // highlight U2
	m.HandleKey(" ")    // select U2
	// Two pills. Query is empty. Backspace should remove the LAST pill (U2).
	m.HandleKey("backspace")
	if _, ok := m.selected["U2"]; ok {
		t.Error("expected U2 removed by backspace at empty query")
	}
	if _, ok := m.selected["U1"]; !ok {
		t.Error("expected U1 still selected")
	}
}

func TestHandleKey_EnterWithPillsReturnsPillIDs(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.HandleKey(" ")    // select Alice (U1)
	m.HandleKey("down") // highlight Bob
	m.HandleKey(" ")    // select Bob (U2)

	res := m.HandleKey("enter")
	if res == nil {
		t.Fatal("expected non-nil result from Enter with pills")
	}
	if len(res.UserIDs) != 2 {
		t.Fatalf("expected 2 user IDs, got %d", len(res.UserIDs))
	}
	// Order within UserIDs is not guaranteed by the spec; check set membership.
	got := map[string]bool{}
	for _, id := range res.UserIDs {
		got[id] = true
	}
	if !got["U1"] || !got["U2"] {
		t.Errorf("expected {U1, U2}, got %v", res.UserIDs)
	}
}

func TestHandleKey_EnterWithoutPillsUsesHighlight(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.HandleKey("down") // highlight Bob (U2)

	res := m.HandleKey("enter")
	if res == nil {
		t.Fatal("expected non-nil result from Enter with highlight")
	}
	if len(res.UserIDs) != 1 || res.UserIDs[0] != "U2" {
		t.Errorf("expected [U2], got %v", res.UserIDs)
	}
}

func TestHandleKey_EnterWithNoHighlightAndNoPillsReturnsNil(t *testing.T) {
	m := New()
	m.SetUsers(nil) // no users -> filtered is empty
	m.Open()

	res := m.HandleKey("enter")
	if res != nil {
		t.Errorf("expected nil result when nothing to submit, got %+v", res)
	}
}

func TestHandleKey_EnterWithEmptyFilteredButPillsStillSubmits(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.HandleKey(" ")    // select Alice
	m.setQuery("xyzqq") // filter to nothing
	res := m.HandleKey("enter")
	if res == nil || len(res.UserIDs) != 1 || res.UserIDs[0] != "U1" {
		t.Errorf("expected pills to submit even with empty filter, got %+v", res)
	}
}

func TestHandleKey_EscClosesPickerAndReturnsNil(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	if !m.IsVisible() {
		t.Fatal("precondition: picker should be visible")
	}
	res := m.HandleKey("esc")
	if res != nil {
		t.Errorf("expected nil result from Esc, got %+v", res)
	}
	if m.IsVisible() {
		t.Error("expected picker to be hidden after Esc")
	}
}

func TestHandleKey_SpaceClearsQueryAfterAdd(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()

	// Type "ali" to filter down to Alice.
	m.HandleKey("a")
	m.HandleKey("l")
	m.HandleKey("i")
	if m.query != "ali" {
		t.Fatalf("precondition: expected query=ali, got %q", m.query)
	}

	// Space selects Alice. Query should clear so the user can type the
	// next name without backspacing.
	m.HandleKey(" ")
	if m.query != "" {
		t.Errorf("expected query cleared after add, got %q", m.query)
	}
	if _, ok := m.selected["U1"]; !ok {
		t.Error("expected U1 selected")
	}
	// Filter should now show all non-self users (5 minus none = 5).
	if len(m.filtered) != 5 {
		t.Errorf("expected filtered to be reset to all users, got %d", len(m.filtered))
	}
}

func TestHandleKey_SpaceDoesNotClearQueryOnRemove(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()

	// Select Alice first (with no query so we have a clean starting state).
	m.HandleKey(" ")
	if _, ok := m.selected["U1"]; !ok {
		t.Fatal("precondition: expected U1 selected after space")
	}

	// Now type a query that matches Alice.
	m.HandleKey("a")
	m.HandleKey("l")
	m.HandleKey("i")
	if m.query != "ali" {
		t.Fatalf("precondition: expected query=ali, got %q", m.query)
	}

	// Space on the highlighted (already-selected) Alice should REMOVE her,
	// and the query should be preserved (user is course-correcting).
	m.HandleKey(" ")
	if _, ok := m.selected["U1"]; ok {
		t.Error("expected U1 removed")
	}
	if m.query != "ali" {
		t.Errorf("expected query preserved after remove, got %q", m.query)
	}
}
