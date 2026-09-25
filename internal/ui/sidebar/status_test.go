package sidebar

import (
	"testing"
	"time"

	"github.com/gammons/slk/internal/ui/peerstatus"
)

// statusOf returns the Status of the DM item with the given DMUserID.
func statusOf(t *testing.T, m *Model, userID string) peerstatus.Status {
	t.Helper()
	for _, it := range m.Items() {
		if it.DMUserID == userID {
			return it.Status
		}
	}
	t.Fatalf("no DM item for %s", userID)
	return peerstatus.Status{}
}

// Status follows presence's contract: an update that arrives before the
// DM row exists is applied when it appears, and a rebuild carrying the
// default empty status does not wipe it.
func TestStatusByUser_AppliedLateAndSurvivesRebuild(t *testing.T) {
	m := New(nil)
	exp := time.Now().Add(time.Hour)
	m.UpdateStatusByUser("U1", ":calendar:", "In a meeting", exp)
	m.UpdateDNDByUser("U1", true, time.Time{})

	m.SetItems([]ChannelItem{{ID: "D1", Type: "dm", DMUserID: "U1", Name: "alice"}})
	st := statusOf(t, &m, "U1")
	if st.Emoji != ":calendar:" || st.Text != "In a meeting" || !st.Expires.Equal(exp) || !st.DND {
		t.Fatalf("early status not applied: %+v", st)
	}

	m.SetItems([]ChannelItem{{ID: "D1", Type: "dm", DMUserID: "U1", Name: "alice"}})
	if st := statusOf(t, &m, "U1"); st.Emoji != ":calendar:" || !st.DND {
		t.Errorf("rebuild wiped live status: %+v", st)
	}
}

// Custom status and DND arrive on different events; each update must
// leave the other half alone.
func TestStatusAndDNDUpdatesAreIndependent(t *testing.T) {
	m := New([]ChannelItem{{ID: "D1", Type: "dm", DMUserID: "U1"}})
	m.UpdateDNDByUser("U1", true, time.Time{})
	m.UpdateStatusByUser("U1", ":palm_tree:", "Vacation", time.Time{})
	if st := statusOf(t, &m, "U1"); !st.DND || st.Emoji != ":palm_tree:" {
		t.Fatalf("status update dropped DND or vice versa: %+v", st)
	}
	m.UpdateDNDByUser("U1", false, time.Time{})
	if st := statusOf(t, &m, "U1"); st.DND || st.Emoji != ":palm_tree:" {
		t.Errorf("DND off dropped the custom status: %+v", st)
	}
}

// A cache-seeded status on the row must not be discarded by the first
// DND update for that user.
func TestDNDUpdateKeepsCacheSeededStatus(t *testing.T) {
	seeded := peerstatus.Status{Emoji: ":house:", Text: "Remote"}
	m := New([]ChannelItem{{ID: "D1", Type: "dm", DMUserID: "U1", Status: seeded}})
	m.UpdateDNDByUser("U1", true, time.Time{})
	if st := statusOf(t, &m, "U1"); st.Emoji != ":house:" || !st.DND {
		t.Errorf("status = %+v; want the seeded status plus DND", st)
	}
}

func TestUpsertItemSeedsKnownStatus(t *testing.T) {
	m := New(nil)
	m.UpdateStatusByUser("U1", ":calendar:", "", time.Time{})
	m.UpsertItem(ChannelItem{ID: "D1", Type: "dm", DMUserID: "U1"})
	if st := statusOf(t, &m, "U1"); st.Emoji != ":calendar:" {
		t.Errorf("UpsertItem status = %+v; want the remembered status", st)
	}
}

func TestResetPresenceAlsoForgetsStatus(t *testing.T) {
	m := New(nil)
	m.UpdateStatusByUser("U1", ":calendar:", "In a meeting", time.Time{})
	m.ResetPresence()
	m.SetItems([]ChannelItem{{ID: "D1", Type: "dm", DMUserID: "U1"}})
	if st := statusOf(t, &m, "U1"); st.Emoji != "" {
		t.Errorf("status = %+v; want none after reset", st)
	}
}

func TestExpireStatuses_ClearsOnlyPassedDeadlines(t *testing.T) {
	now := time.Now()
	m := New([]ChannelItem{
		{ID: "D1", Type: "dm", DMUserID: "U1"},
		{ID: "D2", Type: "dm", DMUserID: "U2"},
	})
	m.UpdateStatusByUser("U1", ":calendar:", "In a meeting", now.Add(-time.Minute))
	m.UpdateStatusByUser("U2", ":palm_tree:", "Vacation", time.Time{})

	if !m.ExpireStatuses(now) {
		t.Fatal("ExpireStatuses reported no change with an expired status")
	}
	if st := statusOf(t, &m, "U1"); st.Emoji != "" {
		t.Errorf("expired status kept: %+v", st)
	}
	if st := statusOf(t, &m, "U2"); st.Emoji != ":palm_tree:" {
		t.Errorf("unexpiring status dropped: %+v", st)
	}
	if m.ExpireStatuses(now) {
		t.Error("a second pass with nothing expired reported a change")
	}
	// The remembered value was cleared too, so a rebuild does not
	// resurrect the expired status.
	m.SetItems([]ChannelItem{{ID: "D1", Type: "dm", DMUserID: "U1"}})
	if st := statusOf(t, &m, "U1"); st.Emoji != "" {
		t.Errorf("rebuild resurrected an expired status: %+v", st)
	}
}
