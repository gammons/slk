package service

import (
	"context"
	"reflect"
	"testing"

	slk "github.com/gammons/slk/internal/slack"
)

func TestStarredConversationIDs_MembershipAndCopy(t *testing.T) {
	for _, ids := range [][]string{{"C1", "D1"}, {"D1"}, nil} {
		store := NewSectionStore()
		if got := store.StarredConversationIDs(); got != nil {
			t.Fatalf("unready IDs = %v", got)
		}
		client := &fakeSectionsClient{sections: []slk.SidebarSection{{ID: "ST", Type: "stars"}}, starIDs: ids}
		if err := store.Bootstrap(context.Background(), client); err != nil {
			t.Fatal(err)
		}
		got := store.StarredConversationIDs()
		if !reflect.DeepEqual(got, ids) {
			t.Fatalf("IDs = %v, want %v", got, ids)
		}
		for _, id := range ids {
			if section, ok := store.SectionForChannel(id); !ok || section != "ST" {
				t.Fatalf("%s membership = %q, %v", id, section, ok)
			}
		}
		if len(ids) > 0 {
			if sections := store.OrderedSections(); len(sections) != 1 || sections[0].ID != "ST" {
				t.Fatalf("renderable sections = %+v", sections)
			}
			got[0] = "mutated"
			if !reflect.DeepEqual(store.StarredConversationIDs(), ids) {
				t.Fatal("accessor exposed mutable store state")
			}
		}
		client.starIDs = nil
		// A real HTTP response is fresh: channelSections.list does not carry
		// the stars membership the store populated on the previous pass.
		client.sections = []slk.SidebarSection{{ID: "ST", Type: "stars"}}
		if err := store.Bootstrap(context.Background(), client); err != nil {
			t.Fatal(err)
		}
		if got := store.StarredConversationIDs(); got != nil {
			t.Fatalf("obsolete stars retained: %v", got)
		}
		for _, id := range ids {
			if section, ok := store.SectionForChannel(id); ok {
				t.Fatalf("obsolete membership %s = %s", id, section)
			}
		}
	}
	store := NewSectionStore()
	if err := store.Bootstrap(context.Background(), &fakeSectionsClient{starIDs: []string{"D1"}}); err != nil {
		t.Fatal(err)
	}
	if got := store.StarredConversationIDs(); got != nil {
		t.Fatalf("IDs without stars section = %v", got)
	}
}
