package ui

import (
	"testing"
	"time"
)

func TestUserStatusChangeMsg_CarriesHuddleToEverySurface(t *testing.T) {
	a := newPeerStatusTestApp(t)
	exp := time.Now().Add(10 * time.Minute)

	cmd, _ := a.presence.Handle(a, UserStatusChangeMsg{UserID: "U1", Emoji: ":calendar:", Huddle: "in_a_huddle", HuddleExpires: exp})
	if cmd == nil {
		t.Error("a huddle with an expiry must start the expiry tick")
	}
	if st := sidebarStatus(t, a, "D1"); st.Huddle != "in_a_huddle" || !st.HuddleExpires.Equal(exp) || st.Emoji != ":calendar:" {
		t.Errorf("sidebar status = %+v", st)
	}
	if st := finderStatus(t, a, "D1"); st.Huddle != "in_a_huddle" {
		t.Errorf("finder status = %+v", st)
	}
	if st := a.presence.peers["U1"]; st.Huddle != "in_a_huddle" {
		t.Errorf("peers status = %+v", st)
	}

	// A later refetch reporting the unset default ends the huddle.
	a.presence.Handle(a, UserStatusChangeMsg{UserID: "U1", Emoji: ":calendar:", Huddle: "default_unset"})
	if st := sidebarStatus(t, a, "D1"); st.InHuddle(time.Now()) {
		t.Errorf("sidebar still in a huddle: %+v", st)
	}
}
