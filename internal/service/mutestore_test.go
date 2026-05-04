package service

import (
	"context"
	"errors"
	"sort"
	"testing"
)

type fakeMutedClient struct {
	ids []string
	err error
}

func (f *fakeMutedClient) GetMutedChannels(_ context.Context) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.ids, nil
}

func TestMuteStore_NotReadyByDefault(t *testing.T) {
	s := NewMuteStore()
	if s.Ready() {
		t.Errorf("Ready=true on fresh store, want false")
	}
	if s.IsMuted("C1") {
		t.Errorf("IsMuted=true on not-ready store, want false (conservative default)")
	}
}

func TestMuteStore_Bootstrap_PopulatesSet(t *testing.T) {
	s := NewMuteStore()
	c := &fakeMutedClient{ids: []string{"C1", "C2", "D7"}}
	if err := s.Bootstrap(context.Background(), c); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if !s.Ready() {
		t.Errorf("Ready=false after bootstrap")
	}
	for _, id := range []string{"C1", "C2", "D7"} {
		if !s.IsMuted(id) {
			t.Errorf("IsMuted(%q)=false, want true", id)
		}
	}
	if s.IsMuted("C99") {
		t.Errorf("IsMuted(C99)=true, want false")
	}
	if s.IsMuted("") {
		t.Errorf("IsMuted(\"\")=true, want false")
	}
}

func TestMuteStore_Bootstrap_Empty(t *testing.T) {
	s := NewMuteStore()
	if err := s.Bootstrap(context.Background(), &fakeMutedClient{}); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if !s.Ready() {
		t.Errorf("Ready=false after empty bootstrap, want true")
	}
	if s.IsMuted("C1") {
		t.Errorf("IsMuted(C1)=true on empty bootstrap, want false")
	}
}

func TestMuteStore_Bootstrap_ErrorLeavesStateUntouched(t *testing.T) {
	s := NewMuteStore()
	// First successful bootstrap.
	if err := s.Bootstrap(context.Background(), &fakeMutedClient{ids: []string{"C1"}}); err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	// Second bootstrap fails — store must keep its prior state.
	err := s.Bootstrap(context.Background(), &fakeMutedClient{err: errors.New("boom")})
	if err == nil {
		t.Fatal("expected error")
	}
	if !s.Ready() {
		t.Errorf("Ready=false after failed re-bootstrap")
	}
	if !s.IsMuted("C1") {
		t.Errorf("IsMuted(C1)=false after failed re-bootstrap; prior state lost")
	}
}

func TestMuteStore_ApplyPrefChange_OnlyHandlesMuteRelatedPrefs(t *testing.T) {
	s := NewMuteStore()
	if changed := s.ApplyPrefChange("highlight_words", "alert,oncall"); changed {
		t.Errorf("ApplyPrefChange returned changed=true for unrelated pref")
	}
	if s.Ready() {
		t.Errorf("Ready=true after unrelated pref_change, want false")
	}
}

func TestMuteStore_ApplyPrefChange_AllNotificationsPrefs(t *testing.T) {
	// Live Slack: mute state arrives as a JSON string under
	// all_notifications_prefs.channels[id].muted=true.
	s := NewMuteStore()
	value := `{"channels":{"C1":{"muted":true},"C2":{"muted":false},"C3":{"muted":true}},"global":{}}`
	if changed := s.ApplyPrefChange("all_notifications_prefs", value); !changed {
		t.Errorf("first ApplyPrefChange returned changed=false, want true")
	}
	if !s.Ready() {
		t.Errorf("Ready=false after pref_change, want true")
	}
	if !s.IsMuted("C1") || !s.IsMuted("C3") {
		t.Errorf("expected C1+C3 muted, got %v", s.MutedChannels())
	}
	if s.IsMuted("C2") {
		t.Errorf("C2 should not be muted (muted=false in pref)")
	}

	// Unmute C1: the next event drops C1 from the muted set.
	value2 := `{"channels":{"C1":{"muted":false},"C2":{"muted":false},"C3":{"muted":true}}}`
	if changed := s.ApplyPrefChange("all_notifications_prefs", value2); !changed {
		t.Errorf("second ApplyPrefChange returned changed=false, want true")
	}
	if s.IsMuted("C1") {
		t.Errorf("C1 still muted after being unmuted in pref event")
	}
	if !s.IsMuted("C3") {
		t.Errorf("C3 unexpectedly unmuted")
	}

	// Idempotent: same payload => changed=false.
	if changed := s.ApplyPrefChange("all_notifications_prefs", value2); changed {
		t.Errorf("idempotent ApplyPrefChange returned changed=true")
	}
}

func TestMuteStore_ApplyPrefChange_AllNotificationsPrefs_BadJSONClearsSet(t *testing.T) {
	s := NewMuteStore()
	// First populate the store.
	s.ApplyPrefChange("all_notifications_prefs", `{"channels":{"C1":{"muted":true}}}`)
	if !s.IsMuted("C1") {
		t.Fatalf("precondition: C1 should be muted")
	}
	// Garbage JSON: parser returns no IDs, so the set becomes empty.
	// We treat this as "Slack told us nothing's muted" rather than
	// keeping stale state — wholesale-replace semantics.
	if changed := s.ApplyPrefChange("all_notifications_prefs", "garbage"); !changed {
		t.Errorf("bad JSON should still mark a change (set cleared)")
	}
	if s.IsMuted("C1") {
		t.Errorf("C1 still muted after bad JSON cleared the set")
	}
}

func TestMuteStore_ApplyPrefChange_ReplacesSet(t *testing.T) {
	s := NewMuteStore()
	if changed := s.ApplyPrefChange("muted_channels", "C1,C2"); !changed {
		t.Errorf("first ApplyPrefChange returned changed=false, want true")
	}
	if !s.Ready() {
		t.Errorf("Ready=false after first pref_change, want true (event is authoritative)")
	}
	if !s.IsMuted("C1") || !s.IsMuted("C2") {
		t.Errorf("expected C1+C2 muted")
	}

	// Slack ships the full list on every change. Removing C1 should
	// unmute it; adding C3 should mute it.
	if changed := s.ApplyPrefChange("muted_channels", "C2,C3"); !changed {
		t.Errorf("second ApplyPrefChange returned changed=false, want true")
	}
	if s.IsMuted("C1") {
		t.Errorf("C1 still muted after being dropped from pref value")
	}
	if !s.IsMuted("C2") || !s.IsMuted("C3") {
		t.Errorf("C2/C3 not muted after pref update")
	}

	// Idempotent: same value again => changed=false.
	if changed := s.ApplyPrefChange("muted_channels", "C2,C3"); changed {
		t.Errorf("idempotent ApplyPrefChange returned changed=true")
	}

	// Order-insensitive: same set, different order.
	if changed := s.ApplyPrefChange("muted_channels", "C3,C2"); changed {
		t.Errorf("order-only change returned changed=true")
	}

	// Empty value clears the set.
	if changed := s.ApplyPrefChange("muted_channels", ""); !changed {
		t.Errorf("clearing the pref returned changed=false")
	}
	if s.IsMuted("C2") || s.IsMuted("C3") {
		t.Errorf("set not cleared by empty pref value")
	}
}

func TestMuteStore_ApplyPrefChange_TolerantOfWhitespaceAndEmpties(t *testing.T) {
	s := NewMuteStore()
	s.ApplyPrefChange("muted_channels", " C1 , ,C2,")
	if !s.IsMuted("C1") || !s.IsMuted("C2") {
		t.Errorf("whitespace/empty entries not handled cleanly")
	}
	if s.IsMuted("") {
		t.Errorf("empty entry treated as muted")
	}
}

func TestMuteStore_MutedChannelsSnapshot(t *testing.T) {
	s := NewMuteStore()
	if got := s.MutedChannels(); got != nil {
		t.Errorf("MutedChannels on not-ready store = %v, want nil", got)
	}
	if err := s.Bootstrap(context.Background(), &fakeMutedClient{ids: []string{"C2", "C1"}}); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	got := s.MutedChannels()
	sort.Strings(got)
	want := []string{"C1", "C2"}
	if len(got) != len(want) {
		t.Fatalf("MutedChannels len=%d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("MutedChannels[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}
