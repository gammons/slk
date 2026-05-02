package service

import (
	"context"
	"testing"

	slk "github.com/gammons/slk/internal/slack"
)

// fakeSectionsClient implements the subset of slk.Client SectionStore needs.
type fakeSectionsClient struct {
	sections []slk.SidebarSection
	getErr   error
}

func (f *fakeSectionsClient) GetChannelSections(ctx context.Context) ([]slk.SidebarSection, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.sections, nil
}

func TestSectionStore_Bootstrap_Empty(t *testing.T) {
	store := NewSectionStore()
	c := &fakeSectionsClient{}
	if err := store.Bootstrap(context.Background(), c); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if !store.Ready() {
		t.Errorf("Ready=false after empty bootstrap")
	}
	if got := store.OrderedSections(); len(got) != 0 {
		t.Errorf("OrderedSections len = %d, want 0", len(got))
	}
}

func TestSectionStore_Bootstrap_BuildsLinkedListOrder(t *testing.T) {
	// Build: head=A → B → C → tail
	sections := []slk.SidebarSection{
		{ID: "B", Name: "Books", Type: "standard", Next: "C", LastUpdate: 100, ChannelIDs: []string{"C2"}, ChannelsCount: 1},
		{ID: "A", Name: "Alerts", Type: "standard", Next: "B", LastUpdate: 100, ChannelIDs: []string{"C1"}, ChannelsCount: 1},
		{ID: "C", Name: "Channels", Type: "channels", Next: "", LastUpdate: 100},
	}
	c := &fakeSectionsClient{sections: sections}
	store := NewSectionStore()
	if err := store.Bootstrap(context.Background(), c); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	got := store.OrderedSections()
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (got: %+v)", len(got), got)
	}
	wantOrder := []string{"A", "B", "C"}
	for i, w := range wantOrder {
		if got[i].ID != w {
			t.Errorf("[%d] ID = %q, want %q", i, got[i].ID, w)
		}
	}
}

func TestSectionStore_Bootstrap_TruncatedSection_LogsAndContinues(t *testing.T) {
	// Section "A" reports count=5 but only first 3 channels were returned
	// in channel_ids_page. v1 trusts the first-page data and lets the
	// remaining 2 stay in the catch-all "Channels" bucket until WS
	// deltas migrate them. Bootstrap must NOT fail in this case.
	sections := []slk.SidebarSection{
		{ID: "A", Type: "standard", Next: "", LastUpdate: 100,
			ChannelIDs:     []string{"C1", "C2", "C3"},
			ChannelsCount:  5,
			ChannelsCursor: "C3"},
	}
	c := &fakeSectionsClient{sections: sections}
	store := NewSectionStore()
	if err := store.Bootstrap(context.Background(), c); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if !store.Ready() {
		t.Errorf("Ready=false after truncated bootstrap")
	}
	// First-page channels are mapped.
	if id, ok := store.SectionForChannel("C1"); !ok || id != "A" {
		t.Errorf("SectionForChannel(C1) = (%q, %v), want (A, true)", id, ok)
	}
	// Channels beyond the first page are NOT mapped.
	if _, ok := store.SectionForChannel("C5"); ok {
		t.Errorf("SectionForChannel(C5) ok=true, want false (channel beyond first page must stay unmapped in v1)")
	}
}

func TestSectionStore_OrderedSections_FiltersSystemTypes(t *testing.T) {
	sections := []slk.SidebarSection{
		{ID: "S", Type: "salesforce_records", Next: "G", LastUpdate: 1},
		{ID: "G", Type: "agents", Next: "T", LastUpdate: 1},
		{ID: "T", Type: "stars", Next: "K", LastUpdate: 1},
		{ID: "K", Type: "slack_connect", Next: "U", LastUpdate: 1},
		{ID: "U", Type: "standard", Name: "Mine", Next: "", LastUpdate: 1, ChannelIDs: []string{"C1"}, ChannelsCount: 1},
	}
	c := &fakeSectionsClient{sections: sections}
	store := NewSectionStore()
	_ = store.Bootstrap(context.Background(), c)
	got := store.OrderedSections()
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (only standard)", len(got))
	}
	if got[0].ID != "U" {
		t.Errorf("got %q, want U", got[0].ID)
	}
}

func TestSectionStore_BootstrapFailure_NotReady(t *testing.T) {
	c := &fakeSectionsClient{getErr: context.DeadlineExceeded}
	store := NewSectionStore()
	if err := store.Bootstrap(context.Background(), c); err == nil {
		t.Errorf("expected error")
	}
	if store.Ready() {
		t.Errorf("Ready=true after failure; should remain false")
	}
}

func TestSectionStore_NotReady_SectionForChannelFalse(t *testing.T) {
	store := NewSectionStore()
	if _, ok := store.SectionForChannel("C1"); ok {
		t.Errorf("ok=true on never-bootstrapped store")
	}
}

func TestApplyUpsert_NewSection(t *testing.T) {
	store := NewSectionStore()
	c := &fakeSectionsClient{sections: []slk.SidebarSection{
		{ID: "A", Type: "standard", Name: "A", LastUpdate: 100},
	}}
	_ = store.Bootstrap(context.Background(), c)

	store.ApplyUpsert(slk.ChannelSectionUpserted{
		ID: "B", Name: "Brand New", Type: "standard", Next: "", LastUpdate: 200,
	})
	got := store.OrderedSections()
	// Both A and B exist now; the head is whichever isn't pointed at.
	// A.Next="" (set in fixture), B.Next="" too — multiple heads.
	// Our heuristic picks the highest LastUpdate.
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (multi-head heuristic picks newest)", len(got))
	}
	if got[0].ID != "B" {
		t.Errorf("head = %q, want B (newer LastUpdate wins)", got[0].ID)
	}
}

func TestApplyUpsert_RenameExistingByID(t *testing.T) {
	store := NewSectionStore()
	c := &fakeSectionsClient{sections: []slk.SidebarSection{
		{ID: "A", Type: "standard", Name: "Old", Next: "", LastUpdate: 100},
	}}
	_ = store.Bootstrap(context.Background(), c)
	store.ApplyUpsert(slk.ChannelSectionUpserted{
		ID: "A", Name: "New", Type: "standard", Next: "", LastUpdate: 200,
	})
	got := store.OrderedSections()
	if len(got) != 1 || got[0].Name != "New" {
		t.Errorf("got %+v, want one section named New", got)
	}
}

func TestApplyUpsert_StaleEventIgnored(t *testing.T) {
	store := NewSectionStore()
	c := &fakeSectionsClient{sections: []slk.SidebarSection{
		{ID: "A", Type: "standard", Name: "Latest", Next: "", LastUpdate: 200},
	}}
	_ = store.Bootstrap(context.Background(), c)
	// Older event arrives.
	store.ApplyUpsert(slk.ChannelSectionUpserted{
		ID: "A", Name: "Stale", Type: "standard", LastUpdate: 100,
	})
	got := store.OrderedSections()
	if got[0].Name != "Latest" {
		t.Errorf("name = %q, want Latest (stale event must be dropped)", got[0].Name)
	}
}

func TestApplyDelete_RemovesSectionAndChannels(t *testing.T) {
	store := NewSectionStore()
	c := &fakeSectionsClient{sections: []slk.SidebarSection{
		{ID: "A", Type: "standard", Name: "A", Next: "", LastUpdate: 100, ChannelIDs: []string{"C1"}, ChannelsCount: 1},
	}}
	_ = store.Bootstrap(context.Background(), c)
	store.ApplyDelete("A")
	if _, ok := store.SectionForChannel("C1"); ok {
		t.Errorf("channel still mapped after section delete")
	}
	if got := store.OrderedSections(); len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestApplyChannelsAdded_UpdatesIndex(t *testing.T) {
	store := NewSectionStore()
	c := &fakeSectionsClient{sections: []slk.SidebarSection{
		{ID: "A", Type: "standard", Next: "", LastUpdate: 100},
	}}
	_ = store.Bootstrap(context.Background(), c)
	store.ApplyChannelsAdded("A", []string{"C1", "C2"})
	if id, ok := store.SectionForChannel("C1"); !ok || id != "A" {
		t.Errorf("C1 → (%q,%v), want (A,true)", id, ok)
	}
	if id, ok := store.SectionForChannel("C2"); !ok || id != "A" {
		t.Errorf("C2 → (%q,%v), want (A,true)", id, ok)
	}
}

func TestApplyChannelsAdded_OverwritesPreviousSection(t *testing.T) {
	// Channel moves from A to B via remove-then-add (Slack's pattern):
	// upsert into B should replace its membership in A.
	store := NewSectionStore()
	c := &fakeSectionsClient{sections: []slk.SidebarSection{
		{ID: "A", Type: "standard", Next: "B", LastUpdate: 100, ChannelIDs: []string{"C1"}, ChannelsCount: 1},
		{ID: "B", Type: "standard", Next: "", LastUpdate: 100},
	}}
	_ = store.Bootstrap(context.Background(), c)
	store.ApplyChannelsAdded("B", []string{"C1"})
	if id, _ := store.SectionForChannel("C1"); id != "B" {
		t.Errorf("C1 in %q, want B (add must overwrite)", id)
	}
}

func TestApplyChannelsRemoved_DropsIndex(t *testing.T) {
	store := NewSectionStore()
	c := &fakeSectionsClient{sections: []slk.SidebarSection{
		{ID: "A", Type: "standard", Next: "", LastUpdate: 100, ChannelIDs: []string{"C1"}, ChannelsCount: 1},
	}}
	_ = store.Bootstrap(context.Background(), c)
	store.ApplyChannelsRemoved("A", []string{"C1"})
	if _, ok := store.SectionForChannel("C1"); ok {
		t.Errorf("C1 still mapped after removal")
	}
}

func TestMaybeRebootstrap_DebouncedWithin30s(t *testing.T) {
	store := NewSectionStore()
	c := &fakeSectionsClient{sections: []slk.SidebarSection{
		{ID: "A", Type: "standard", Next: "", LastUpdate: 100},
	}}
	if err := store.Bootstrap(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	// First call: too soon, skipped.
	calledAgain := false
	c2 := &fakeSectionsClient{sections: []slk.SidebarSection{
		{ID: "B", Type: "standard", Next: "", LastUpdate: 200},
	}}
	wrap := &countingClient{inner: c2, onCall: func() { calledAgain = true }}
	store.MaybeRebootstrap(context.Background(), wrap)
	if calledAgain {
		t.Errorf("MaybeRebootstrap should be debounced within 30s")
	}
}

type countingClient struct {
	inner  SectionsClient
	onCall func()
}

func (cc *countingClient) GetChannelSections(ctx context.Context) ([]slk.SidebarSection, error) {
	cc.onCall()
	return cc.inner.GetChannelSections(ctx)
}
