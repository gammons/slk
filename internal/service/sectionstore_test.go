package service

import (
	"context"
	"testing"

	slk "github.com/gammons/slk/internal/slack"
)

// fakeSectionsClient implements the subset of slk.Client SectionStore needs.
type fakeSectionsClient struct {
	sections []slk.SidebarSection
	starIDs  []string
	starErr  error
	getErr   error
}

func (f *fakeSectionsClient) GetChannelSections(ctx context.Context) ([]slk.SidebarSection, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.sections, nil
}

func (f *fakeSectionsClient) GetStarredChannels(ctx context.Context) ([]string, error) {
	if f.starErr != nil {
		return nil, f.starErr
	}
	return f.starIDs, nil
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
		{ID: "G", Type: "agents", Next: "K", LastUpdate: 1},
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

// stars is a Slack-native section type (the "Starred" feature). A
// non-empty stars section must render in the sidebar, matching how the
// official Slack client surfaces starred channels.
func TestSectionStore_OrderedSections_StarsRenderWhenNonEmpty(t *testing.T) {
	sections := []slk.SidebarSection{
		{ID: "ST", Type: "stars", Name: "", Next: "U", LastUpdate: 1,
			ChannelIDs: []string{"C1"}, ChannelsCount: 1},
		{ID: "U", Type: "standard", Name: "Mine", Next: "", LastUpdate: 1},
	}
	c := &fakeSectionsClient{sections: sections}
	store := NewSectionStore()
	_ = store.Bootstrap(context.Background(), c)
	got := store.OrderedSections()
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (stars + standard); got %+v", len(got), got)
	}
	if got[0].ID != "ST" {
		t.Errorf("got[0].ID = %q, want ST (stars comes first in linked list)", got[0].ID)
	}
	if got[1].ID != "U" {
		t.Errorf("got[1].ID = %q, want U", got[1].ID)
	}
}

// An empty stars section must stay hidden so users who haven't starred
// anything don't see an empty header — mirrors recent_apps semantics.
func TestSectionStore_OrderedSections_StarsHiddenWhenEmpty(t *testing.T) {
	sections := []slk.SidebarSection{
		{ID: "ST", Type: "stars", Name: "", Next: "U", LastUpdate: 1},
		{ID: "U", Type: "standard", Name: "Mine", Next: "", LastUpdate: 1,
			ChannelIDs: []string{"C1"}, ChannelsCount: 1},
	}
	c := &fakeSectionsClient{sections: sections}
	store := NewSectionStore()
	_ = store.Bootstrap(context.Background(), c)
	got := store.OrderedSections()
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (empty stars hidden); got %+v", len(got), got)
	}
	if got[0].ID != "U" {
		t.Errorf("got[0].ID = %q, want U", got[0].ID)
	}
}

// SectionForChannel must claim channels that Slack has placed in a
// stars section, so they render under the Starred header rather than
// falling through to the type-default bucket.
func TestSectionStore_SectionForChannel_StarsClaimed(t *testing.T) {
	sections := []slk.SidebarSection{
		{ID: "ST", Type: "stars", Name: "", Next: "U", LastUpdate: 1,
			ChannelIDs: []string{"C9"}, ChannelsCount: 1},
		{ID: "U", Type: "standard", Name: "Mine", Next: "", LastUpdate: 1},
	}
	c := &fakeSectionsClient{sections: sections}
	store := NewSectionStore()
	_ = store.Bootstrap(context.Background(), c)
	id, ok := store.SectionForChannel("C9")
	if !ok {
		t.Fatalf("SectionForChannel(C9) ok=false, want true (stars section should claim its channels)")
	}
	if id != "ST" {
		t.Errorf("SectionForChannel(C9) = %q, want ST", id)
	}
}

// Slack's users.channelSections.list returns the stars section with an
// empty channel_ids array. PopulateStars fills it from stars.list, the
// authoritative source for starred channels. Without this call, a
// bootstrapped stars section stays empty and includeInSidebar hides it.
func TestSectionStore_PopulateStars_FillsEmptyStarsSection(t *testing.T) {
	sections := []slk.SidebarSection{
		{ID: "ST", Type: "stars", Name: "", Next: "U", LastUpdate: 1},
		{ID: "U", Type: "standard", Name: "Mine", Next: "", LastUpdate: 1},
	}
	c := &fakeSectionsClient{sections: sections}
	store := NewSectionStore()
	_ = store.Bootstrap(context.Background(), c)

	// Before: stars section is empty → hidden by includeInSidebar.
	// The standard section U renders regardless (standard always shows).
	got := store.OrderedSections()
	if len(got) != 1 || got[0].ID != "U" {
		t.Fatalf("before PopulateStars: OrderedSections = %+v, want just [U] (empty stars hidden)", got)
	}

	store.PopulateStars([]string{"C1", "C2"})

	// After: stars section renders, channels claimed.
	got = store.OrderedSections()
	if len(got) != 2 {
		t.Fatalf("after PopulateStars: OrderedSections len = %d, want 2 (stars + standard); got %+v", len(got), got)
	}
	if got[0].ID != "ST" {
		t.Errorf("got[0].ID = %q, want ST (stars is earlier in linked list)", got[0].ID)
	}
	for _, cid := range []string{"C1", "C2"} {
		id, ok := store.SectionForChannel(cid)
		if !ok || id != "ST" {
			t.Errorf("SectionForChannel(%s) = (%q,%v), want (ST,true)", cid, id, ok)
		}
	}
}

// PopulateStars is a no-op when there is no stars section (workspace
// doesn't have one, or it was deleted). It must not synthesize one.
func TestSectionStore_PopulateStars_NoOpWithoutStarsSection(t *testing.T) {
	sections := []slk.SidebarSection{
		{ID: "U", Type: "standard", Name: "Mine", Next: "", LastUpdate: 1},
	}
	c := &fakeSectionsClient{sections: sections}
	store := NewSectionStore()
	_ = store.Bootstrap(context.Background(), c)
	store.PopulateStars([]string{"C1", "C2"})
	// Channel C1 is not in any real section.
	if _, ok := store.SectionForChannel("C1"); ok {
		t.Errorf("SectionForChannel(C1) ok=true, want false (no stars section to claim it)")
	}
}

// PopulateStars replaces the stars section's channel list on re-call so
// star/unstar events stay consistent. Re-starring the same channel and
// un-starring another should reflect in the new state.
func TestSectionStore_PopulateStars_ReplacesPreviousStarList(t *testing.T) {
	sections := []slk.SidebarSection{
		{ID: "ST", Type: "stars", Name: "", Next: "U", LastUpdate: 1},
		{ID: "U", Type: "standard", Name: "Mine", Next: "", LastUpdate: 1},
	}
	c := &fakeSectionsClient{sections: sections}
	store := NewSectionStore()
	_ = store.Bootstrap(context.Background(), c)
	store.PopulateStars([]string{"C1", "C2"})
	store.PopulateStars([]string{"C2", "C3"})
	// C1 un-starred, C3 newly starred.
	if _, ok := store.SectionForChannel("C1"); ok {
		t.Errorf("C1 should no longer be claimed after re-populate without it")
	}
	for _, cid := range []string{"C2", "C3"} {
		if id, ok := store.SectionForChannel(cid); !ok || id != "ST" {
			t.Errorf("SectionForChannel(%s) = (%q,%v), want (ST,true)", cid, id, ok)
		}
	}
}

// TestBootstrap_PopulatesStars regresses a bug where the Starred header
// disappeared after a WebSocket reconnect. Bootstrap atomically replaces
// store state; channelSections.list returns the stars section with an
// empty channel_ids array (built-in types aren't populated). Without
// Bootstrap also fetching stars.list, a reconnect-triggered re-bootstrap
// wiped the star list that PopulateStars had filled at startup and
// includeInSidebar hid the header. Bootstrap now fetches stars.list
// itself, so a single Bootstrap leaves stars populated — and any future
// Bootstrap call site is automatically covered.
func TestBootstrap_PopulatesStars(t *testing.T) {
	c := &fakeSectionsClient{
		sections: []slk.SidebarSection{
			{ID: "ST", Type: "stars", Next: "U", LastUpdate: 1},
			{ID: "U", Type: "standard", Name: "Mine", Next: "", LastUpdate: 1},
		},
		starIDs: []string{"C1", "C2"},
	}
	store := NewSectionStore()
	if err := store.Bootstrap(context.Background(), c); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	// Stars section now renders (non-empty ChannelIDs).
	got := store.OrderedSections()
	if len(got) != 2 || got[0].ID != "ST" {
		t.Fatalf("OrderedSections = %+v, want [ST, U] (stars populated by Bootstrap)", got)
	}
	for _, cid := range []string{"C1", "C2"} {
		id, ok := store.SectionForChannel(cid)
		if !ok || id != "ST" {
			t.Errorf("SectionForChannel(%s) = (%q,%v), want (ST,true)", cid, id, ok)
		}
	}
}

// TestBootstrap_StarsFetchErrorIsBestEffort verifies that a stars.list
// failure does not fail Bootstrap itself — sections still load and the
// store becomes Ready; the stars section just stays hidden until the
// next successful bootstrap.
func TestBootstrap_StarsFetchErrorIsBestEffort(t *testing.T) {
	c := &fakeSectionsClient{
		sections: []slk.SidebarSection{
			{ID: "U", Type: "standard", Name: "Mine", Next: "", LastUpdate: 1},
		},
		starErr: context.DeadlineExceeded,
	}
	store := NewSectionStore()
	if err := store.Bootstrap(context.Background(), c); err != nil {
		t.Fatalf("Bootstrap should succeed despite stars.list error: %v", err)
	}
	if !store.Ready() {
		t.Fatalf("Ready=false; sections should still be loaded")
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

func (cc *countingClient) GetStarredChannels(ctx context.Context) ([]string, error) {
	return cc.inner.GetStarredChannels(ctx)
}

// TestSectionForChannel_HidesNonRenderableSections regresses a sidebar
// crash where a channel mapped to a non-renderable section (stars,
// slack_connect, salesforce_records, agents) ended up with a
// Section ID the sidebar's modelOrderedSections never emitted, causing
// a nil-pointer dereference in buildCache. SectionForChannel now
// returns ok=false for such channels so the resolver falls through to
// type-default bucketing.
func TestSectionForChannel_HidesNonRenderableSections(t *testing.T) {
	store := NewSectionStore()
	c := &fakeSectionsClient{sections: []slk.SidebarSection{
		// A channel in a slack_connect section: real, indexed, but the
		// section type is hidden by the renderability filter.
		{ID: "L_SC", Type: "slack_connect", Next: "L_STD", LastUpdate: 100,
			ChannelIDs: []string{"C_EXTERNAL"}, ChannelsCount: 1},
		// A regular standard section, fully renderable.
		{ID: "L_STD", Type: "standard", Name: "Mine", Next: "", LastUpdate: 100,
			ChannelIDs: []string{"C_STD"}, ChannelsCount: 1},
	}}
	if err := store.Bootstrap(context.Background(), c); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// Channel in the renderable section — returns the ID.
	if id, ok := store.SectionForChannel("C_STD"); !ok || id != "L_STD" {
		t.Errorf("C_STD → (%q, %v), want (L_STD, true)", id, ok)
	}
	// Channel in the non-renderable (slack_connect) section — returns ("", false)
	// even though the channelToSection index has it. This prevents the
	// sidebar from receiving a Section ID it can't bucket against.
	if id, ok := store.SectionForChannel("C_EXTERNAL"); ok {
		t.Errorf("C_EXTERNAL → (%q, %v), want ('', false) for non-renderable section", id, ok)
	}
}

// TestSectionForChannel_HidesRedactedSections is the parallel guard for
// is_redacted=true sections: even if the type would otherwise render,
// a redacted section is hidden from the sidebar, and channels in it
// must not leak their Section ID upward.
func TestSectionForChannel_HidesRedactedSections(t *testing.T) {
	store := NewSectionStore()
	c := &fakeSectionsClient{sections: []slk.SidebarSection{
		{ID: "L_R", Type: "standard", Name: "Hidden", Next: "", LastUpdate: 100,
			IsRedacted: true,
			ChannelIDs: []string{"C_REDACTED"}, ChannelsCount: 1},
	}}
	if err := store.Bootstrap(context.Background(), c); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if id, ok := store.SectionForChannel("C_REDACTED"); ok {
		t.Errorf("C_REDACTED → (%q, %v), want ('', false) for redacted section", id, ok)
	}
}
