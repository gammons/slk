package sidebar

import "testing"

// presenceOf returns the Presence of the DM item with the given DMUserID.
func presenceOf(m *Model, userID string) string {
	for _, it := range m.Items() {
		if it.DMUserID == userID {
			return it.Presence
		}
	}
	return "<not found>"
}

// A live presence update must survive a subsequent SetItems rebuild.
// SectionsRefreshedMsg / WorkspaceReadyMsg push freshly-built items whose
// Presence is the stale "away" default; without authoritative presence
// state that rebuild wipes the live dots (the "everyone offline" bug).
func TestSetItemsPreservesLivePresence(t *testing.T) {
	m := New([]ChannelItem{{ID: "D1", Type: "dm", DMUserID: "U1", Presence: "away"}})

	m.UpdatePresenceByUser("U1", "active")
	if got := presenceOf(&m, "U1"); got != "active" {
		t.Fatalf("after UpdatePresenceByUser: presence = %q, want active", got)
	}

	// Simulate a sidebar rebuild (e.g. sections refresh) carrying stale presence.
	m.SetItems([]ChannelItem{{ID: "D1", Type: "dm", DMUserID: "U1", Presence: "away"}})
	if got := presenceOf(&m, "U1"); got != "active" {
		t.Errorf("after SetItems rebuild: presence = %q, want active (live presence must survive)", got)
	}
}

// A presence event that arrives before the DM item exists must be applied
// once the item is set (startup race: the presence burst lands right after
// subscribe, potentially before WorkspaceReadyMsg populates the sidebar).
func TestUpdatePresenceBeforeItemsIsAppliedOnSetItems(t *testing.T) {
	m := New(nil)

	m.UpdatePresenceByUser("U1", "active") // no items yet

	m.SetItems([]ChannelItem{{ID: "D1", Type: "dm", DMUserID: "U1", Presence: "away"}})
	if got := presenceOf(&m, "U1"); got != "active" {
		t.Errorf("presence = %q, want active (early event must be remembered)", got)
	}
}

// ResetPresence drops remembered presence so a workspace switch starts from
// the new workspace's cache-seeded item presence rather than stale peers.
func TestResetPresenceClearsRememberedState(t *testing.T) {
	m := New(nil)
	m.UpdatePresenceByUser("U1", "active")
	m.ResetPresence()

	m.SetItems([]ChannelItem{{ID: "D1", Type: "dm", DMUserID: "U1", Presence: "away"}})
	if got := presenceOf(&m, "U1"); got != "away" {
		t.Errorf("presence = %q, want away (reset must forget prior presence)", got)
	}
}

// TestSidebar_PresenceByUser pins PresenceByUser's contract: an unknown
// user reports ("", false); a user recorded via UpdatePresenceByUser
// reports (value, true) -- the "presence shown only when known" rule
// the user-profile dialog depends on to decide whether to render the
// presence indicator at all.
func TestSidebar_PresenceByUser(t *testing.T) {
	m := New(nil)

	if p, ok := m.PresenceByUser("U1"); ok || p != "" {
		t.Errorf("unknown user: PresenceByUser = (%q, %v), want (\"\", false)", p, ok)
	}

	m.UpdatePresenceByUser("U1", "away")
	if p, ok := m.PresenceByUser("U1"); !ok || p != "away" {
		t.Errorf("after UpdatePresenceByUser: PresenceByUser = (%q, %v), want (\"away\", true)", p, ok)
	}
}
