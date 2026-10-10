package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/gammons/slk/internal/bootstrap"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/service"
	slk "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slack/boot"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/slack-go/slack"
)

func TestMissingStarredConversations_BootClosedIM(t *testing.T) {
	res := &bootstrap.Result{IMs: []boot.IM{
		{ID: "D1", UserID: "U1", IsOrgShared: true},
		{ID: "D2", UserID: "U2"}, // unstarred, closed
		{ID: "D3", UserID: "U3", IsOpen: true},
	}}
	loaded := bootConversations(res)
	if len(loaded) != 1 || loaded[0].ID != "D3" {
		t.Fatalf("ordinary fallback = %+v", loaded)
	}
	got := missingStarredConversations(context.Background(), []string{"D3"}, []string{"D1", "D3", "D1"}, res.IMs,
		func(context.Context, string) (*slack.Channel, error) {
			t.Fatal("boot IM should not need a lookup")
			return nil, nil
		})
	if len(got) != 1 || got[0].ID != "D1" || got[0].User != "U1" || !got[0].IsIM || !got[0].IsMember || !got[0].IsOrgShared {
		t.Fatalf("missing starred = %+v", got)
	}
}

func TestMissingStarredConversations_ReadOnlyLookup(t *testing.T) {
	var calls []string
	got := missingStarredConversations(context.Background(), []string{"D0"}, []string{"D0", "D1", "D1", "D2"}, nil,
		func(_ context.Context, id string) (*slack.Channel, error) {
			calls = append(calls, id)
			return &slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: id, IsIM: true, User: "U" + id}}}, nil
		})
	if !reflect.DeepEqual(calls, []string{"D1", "D2"}) || len(got) != 2 {
		t.Fatalf("calls = %v, added = %+v", calls, got)
	}
}

func TestMissingStarredConversations_InvalidOrUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name    string
		channel *slack.Channel
		err     error
	}{
		{"error", nil, errors.New("not_in_channel")},
		{"nil", nil, nil},
		{"wrong ID", &slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "D2", IsIM: true, User: "U1"}}}, nil},
		{"no peer", &slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "D1", IsIM: true}}}, nil},
		{"archived", &slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "D1", IsIM: true, User: "U1"}, IsArchived: true}}, nil},
		{"closed mpim", &slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "D1", IsMpIM: true}, Name: "mpdm-a--b-1"}, IsMember: true}, nil},
		{"not joined", &slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "D1"}, Name: "not joined"}, IsChannel: true}, nil},
		{"malformed", &slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "D1"}}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := missingStarredConversations(context.Background(), nil, []string{"D1"}, nil,
				func(context.Context, string) (*slack.Channel, error) { return tc.channel, tc.err })
			if len(got) != 0 {
				t.Fatalf("added invalid conversation: %+v", got)
			}
		})
	}
	// An archived boot IM is already known to be ineligible, not a reason
	// to spend a request trying to rediscover it.
	got := missingStarredConversations(context.Background(), nil, []string{"D1"}, []boot.IM{{ID: "D1", UserID: "U1", IsArchived: true}},
		func(context.Context, string) (*slack.Channel, error) {
			t.Fatal("lookup of archived boot IM")
			return nil, nil
		})
	if len(got) != 0 {
		t.Fatalf("archived boot IM = %+v", got)
	}
}

func TestMissingStarredConversations_Cancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := missingStarredConversations(ctx, nil, []string{"D1"}, []boot.IM{{ID: "D1", UserID: "U1"}},
		func(context.Context, string) (*slack.Channel, error) {
			t.Fatal("lookup after cancellation")
			return nil, nil
		})
	if len(got) != 0 {
		t.Fatalf("added after cancellation: %+v", got)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	got = missingStarredConversations(ctx, nil, []string{"D1", "D2"}, nil,
		func(ctx context.Context, _ string) (*slack.Channel, error) { calls++; cancel(); return nil, ctx.Err() })
	if len(got) != 0 || calls != 1 {
		t.Fatalf("added = %+v, calls = %d", got, calls)
	}
}

func TestMissingStarredConversations_ContinuesAfterFailure(t *testing.T) {
	var calls []string
	got := missingStarredConversations(context.Background(), nil, []string{"missing", "C1", "D1"}, []boot.IM{{ID: "D1"}},
		func(_ context.Context, id string) (*slack.Channel, error) {
			calls = append(calls, id)
			if id == "missing" {
				return nil, errors.New("channel_not_found")
			}
			if id == "C1" {
				return &slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: id, IsPrivate: true}, Name: "private"}, IsMember: true}, nil
			}
			// A boot IM without its peer ID needs authoritative metadata;
			// do not publish a fabricated user-only row.
			return &slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: id, IsIM: true, User: "U1"}}}, nil
		})
	if !reflect.DeepEqual(calls, []string{"missing", "C1", "D1"}) || len(got) != 2 || got[0].ID != "C1" || got[1].User != "U1" {
		t.Fatalf("calls=%v, added=%+v", calls, got)
	}
}

func TestBuildChannelItem_StarredDM(t *testing.T) {
	store := service.NewSectionStore()
	if err := store.Bootstrap(context.Background(), &fakeSectionsClient{sections: []slk.SidebarSection{{ID: "ST", Type: "stars", ChannelIDs: []string{"D1"}}}}); err != nil {
		t.Fatal(err)
	}
	wctx := &WorkspaceContext{SectionStore: store, UserNames: newUserNameStore(map[string]string{"U1": "Alice"})}
	ch := slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "D1", IsIM: true, User: "U1"}}}
	item, finder := buildChannelItem(ch, wctx, config.Config{}, "T1")
	if item.Section != "ST" || item.Type != "dm" || item.DMUserID != "U1" || item.Name != "Alice" {
		t.Fatalf("item = %+v", item)
	}
	if finder.ID != "D1" || finder.Name != "Alice" || finder.Type != "dm" || !finder.Joined {
		t.Fatalf("finder = %+v", finder)
	}
}

func TestReconcileStarredConversations_ReconnectInactive(t *testing.T) {
	store := service.NewSectionStore()
	if err := store.Bootstrap(context.Background(), &fakeSectionsClient{sections: []slk.SidebarSection{{ID: "ST", Type: "stars", ChannelIDs: []string{"D1"}}}}); err != nil {
		t.Fatal(err)
	}
	wctx := &WorkspaceContext{SectionStore: store, UserNames: newUserNameStore(map[string]string{"U1": "Alice"}), Channels: []sidebar.ChannelItem{{ID: "C1"}}}
	sender := &captureSender{}
	h := &rtmEventHandler{wsCtx: wctx, workspaceID: "T1", program: sender, isActive: func() bool { return false }, channelNames: map[string]string{}, channelTypes: map[string]string{}}
	calls := 0
	h.resolveConversation = func(context.Context, string) (*slack.Channel, error) {
		calls++
		return &slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "D1", IsIM: true, User: "U1"}}}, nil
	}
	h.reconcileStarredConversations(context.Background())
	h.reconcileStarredConversations(context.Background())
	h.refreshSectionsForActive()
	if calls != 1 || len(wctx.Channels) != 2 || len(wctx.FinderItems) != 1 {
		t.Fatalf("calls=%d channels=%+v finder=%+v", calls, wctx.Channels, wctx.FinderItems)
	}
	if wctx.Channels[1].Section != "ST" || wctx.Channels[1].Name != "Alice" || h.channelTypes["D1"] != "dm" {
		t.Fatalf("DM = %+v", wctx.Channels[1])
	}
	if len(sender.sent) != 0 {
		t.Fatalf("inactive workspace sent UI messages: %+v", sender.sent)
	}
}
