package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/gammons/slk/internal/slack/edge"
)

type fakeChannelSearch struct {
	query       string
	topChannels []string
	channels    []edge.Channel
	members     []string
	err         error
	calls       int
}

func (f *fakeChannelSearch) ChannelsSearch(_ context.Context, query string, topChannels []string) ([]edge.Channel, []string, error) {
	f.calls++
	f.query = query
	f.topChannels = topChannels
	return f.channels, f.members, f.err
}

func TestSearchChannelsRemote_MapsTypesTheFinderUnderstands(t *testing.T) {
	// edge results carry no type string — the finder's four types have
	// to be derived from the flags, and getting it wrong shows up as a
	// channel with the wrong sigil rather than as an error.
	fake := &fakeChannelSearch{channels: []edge.Channel{
		{ID: "C1", Name: "public", IsChannel: true},
		{ID: "C2", Name: "secret", IsChannel: true, IsPrivate: true},
		{ID: "C3", Name: "dm", IsIM: true},
		{ID: "C4", Name: "group", IsMPIM: true},
	}}

	got := searchChannelsRemote(context.Background(), fake, nil, "x")
	if len(got) != 4 {
		t.Fatalf("got %d items; want 4 (%+v)", len(got), got)
	}
	want := map[string]string{"C1": "channel", "C2": "private", "C3": "dm", "C4": "group_dm"}
	for _, it := range got {
		if it.Type != want[it.ID] {
			t.Errorf("%s type = %q; want %q", it.ID, it.Type, want[it.ID])
		}
	}
}

func TestSearchChannelsRemote_SkipsArchivedChannels(t *testing.T) {
	// The conversations.list walk this replaced passed
	// ExcludeArchived; an archived channel in the finder is a dead end
	// the user cannot post to.
	fake := &fakeChannelSearch{channels: []edge.Channel{
		{ID: "C1", Name: "live", IsChannel: true},
		{ID: "C2", Name: "dead", IsChannel: true, IsArchived: true},
	}}
	got := searchChannelsRemote(context.Background(), fake, nil, "x")
	if len(got) != 1 || got[0].ID != "C1" {
		t.Errorf("got %+v; want only the live channel", got)
	}
}

func TestSearchChannelsRemote_CarriesLastVisitedAndTheFrecencyHint(t *testing.T) {
	// top_channels is the server's ranking hint, and LastVisited is
	// what the finder sorts its own list by; both come from the same
	// visit record, which is the only frecency signal slk keeps.
	visits := map[string]int64{"C_OLD": 100, "C_NEW": 300, "C_MID": 200}
	fake := &fakeChannelSearch{channels: []edge.Channel{{ID: "C_MID", Name: "mid", IsChannel: true}}}

	got := searchChannelsRemote(context.Background(), fake, visits, "x")
	if len(got) != 1 || got[0].LastVisited != 200 {
		t.Errorf("got %+v; want LastVisited 200 carried through", got)
	}
	if want := []string{"C_NEW", "C_MID", "C_OLD"}; !reflect.DeepEqual(fake.topChannels, want) {
		t.Errorf("top_channels = %v; want %v — most recently visited first, which is the order the hint is read in", fake.topChannels, want)
	}
}

func TestSearchChannelsRemote_EmptyQueryMakesNoRequest(t *testing.T) {
	fake := &fakeChannelSearch{}
	if got := searchChannelsRemote(context.Background(), fake, nil, ""); got != nil {
		t.Errorf("got %+v; want nil", got)
	}
	if fake.calls != 0 {
		t.Errorf("empty query made %d requests; want 0", fake.calls)
	}
}

func TestSearchChannelsRemote_ErrorLeavesTheFinderOnLocalMatches(t *testing.T) {
	// Returning nil means SetBrowseable is never called, so what the
	// user already sees stays on screen. Anything else would blank the
	// list because a search failed.
	fake := &fakeChannelSearch{err: errors.New("ratelimited")}
	if got := searchChannelsRemote(context.Background(), fake, nil, "x"); got != nil {
		t.Errorf("got %+v; want nil on error", got)
	}
}

func TestSearchChannelsRemote_NilSearcherIsSafe(t *testing.T) {
	// A workspace whose edge client failed to construct must not panic
	// the finder on the first keystroke.
	if got := searchChannelsRemote(context.Background(), nil, nil, "x"); got != nil {
		t.Errorf("got %+v; want nil", got)
	}
}
