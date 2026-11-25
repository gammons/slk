package compose

import (
	"testing"

	"github.com/gammons/slk/internal/ui/mentionpicker"
)

// Usergroup mentions: the per-workspace usergroup map feeds both the
// send-time translation (@handle -> <!subteam^SID|@handle>) and the
// mention picker's group entries.

func TestTranslateUsergroupMentionForSend(t *testing.T) {
	m := New("general")
	m.SetUserGroups(map[string]string{"S0TESTGRP01": "platform-team"})
	m.SetUsers([]mentionpicker.User{
		{ID: "U1234", DisplayName: "Alice", Username: "alice"},
	})

	tests := []struct {
		input    string
		expected string
	}{
		{"hey @platform-team look", "hey <!subteam^S0TESTGRP01|@platform-team> look"},
		{"@platform-team and @Alice", "<!subteam^S0TESTGRP01|@platform-team> and <@U1234>"},
		// Word boundary: a longer non-group word must not match.
		{"@platform-teammates hi", "@platform-teammates hi"},
	}
	for _, tt := range tests {
		result := m.TranslateMentionsForSend(tt.input)
		if result != tt.expected {
			t.Errorf("TranslateMentionsForSend(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// A user and a usergroup can share a name. The entry sort must resolve
// the collision the same way on every call -- sort.Slice is unstable and
// the entries come out of map iteration, so an unbroken tie would pick a
// different wire form run to run.
func TestTranslateMentionsForSendResolvesNameCollisionDeterministically(t *testing.T) {
	m := New("general")
	m.SetUserGroups(map[string]string{"S0TESTGRP01": "design"})
	m.SetUsers([]mentionpicker.User{
		{ID: "U1234", DisplayName: "design", Username: "design"},
	})

	// The person wins: a name that resolves to a human should not ping
	// a whole group.
	const want = "ping <@U1234>"
	for i := 0; i < 50; i++ {
		if got := m.TranslateMentionsForSend("ping @design"); got != want {
			t.Fatalf("iteration %d: TranslateMentionsForSend = %q, want %q", i, got, want)
		}
	}
}

func TestUsergroupsAreScopedPerComposeModel(t *testing.T) {
	work := New("general")
	work.SetUserGroups(map[string]string{"S0TESTGRP01": "work-team"})
	side := New("general")
	side.SetUserGroups(map[string]string{"S0TESTGRP01": "side-team"})

	workOut := work.TranslateMentionsForSend("ping @work-team")
	sideOut := side.TranslateMentionsForSend("ping @side-team")

	if workOut != "ping <!subteam^S0TESTGRP01|@work-team>" {
		t.Errorf("work TranslateMentionsForSend = %q", workOut)
	}
	if sideOut != "ping <!subteam^S0TESTGRP01|@side-team>" {
		t.Errorf("side TranslateMentionsForSend = %q", sideOut)
	}
	if got := work.TranslateMentionsForSend("ping @side-team"); got != "ping @side-team" {
		t.Errorf("work model translated side workspace group: %q", got)
	}
}

func TestMentionPickerIncludesUsergroups(t *testing.T) {
	m := New("general")
	m.SetUserGroups(map[string]string{"S0AB12CD3": "backend"})
	m.SetUsers([]mentionpicker.User{
		{ID: "U1234", DisplayName: "Alice", Username: "alice"},
	})

	found := false
	for _, u := range m.MentionUsers() {
		if u.ID == "usergroup:S0AB12CD3" {
			found = true
			if u.DisplayName != "backend" {
				t.Errorf("group DisplayName = %q, want %q", u.DisplayName, "backend")
			}
			if !u.InChannel {
				t.Error("group entry should have InChannel=true")
			}
		}
	}
	if !found {
		t.Errorf("mention picker missing usergroup entry; got %+v", m.MentionUsers())
	}
}

// Groups must survive channel-membership filtering: they are not
// members of any channel, but pinging them is channel-agnostic.
func TestMentionPickerKeepsGroupsWithMembershipLoaded(t *testing.T) {
	m := New("general")
	m.SetUserGroups(map[string]string{"S0AB12CD3": "backend"})
	m.SetActiveChannel("C1")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1234", DisplayName: "Alice", Username: "alice"},
	})
	m.SetChannelMembership("C1", []string{"U1234"})

	for _, u := range m.MentionUsers() {
		if u.ID == "usergroup:S0AB12CD3" {
			if !u.InChannel {
				t.Error("group entry should stay InChannel=true under membership filtering")
			}
			return
		}
	}
	t.Errorf("usergroup entry dropped by membership filtering; got %+v", m.MentionUsers())
}

// Two Grid member workspaces can each have a group with the same
// handle (#214). Sending must emit one valid token, not nest the second
// group's token inside the first one's "@handle" label.
func TestTranslateMentionsForSendDuplicateGroupHandle(t *testing.T) {
	m := New("general")
	m.SetUserGroups(map[string]string{"S2": "design", "S1": "design"})
	const want = "ping <!subteam^S1|@design> now"
	for i := 0; i < 50; i++ {
		if got := m.TranslateMentionsForSend("ping @design now"); got != want {
			t.Fatalf("iteration %d: TranslateMentionsForSend = %q, want %q", i, got, want)
		}
	}
}
