package main

// OnStarChanged applies conversation stars only: a message or file
// star arrives through the dispatcher like any other (the saved-items
// list it belongs to has no view yet) but must not touch the sidebar's
// section state.
import (
	"testing"

	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui/sidebar"
)

func TestOnStarChanged_OnlyConversationStarsResection(t *testing.T) {
	starInStore := func(wctx *WorkspaceContext, channelID string) bool {
		id, ok := wctx.SectionStore.SectionForChannel(channelID)
		return ok && id == "stars"
	}

	t.Run("conversation star claims the row", func(t *testing.T) {
		wctx := &WorkspaceContext{TeamID: "T1", SectionStore: bootstrappedStore(t, nil)}
		wctx.Channels = []sidebar.ChannelItem{{ID: "C1", Name: "one", Type: "channel"}}
		h := &rtmEventHandler{wsCtx: wctx, workspaceID: "T1"}

		h.OnStarChanged(slackclient.StarChange{ItemType: "channel", Channel: "C1"}, true)
		if !starInStore(wctx, "C1") || !wctx.Channels[0].IsStarred {
			t.Fatal("a channel star did not claim the row")
		}

		h.OnStarChanged(slackclient.StarChange{ItemType: "im", Channel: "C1"}, false)
		if starInStore(wctx, "C1") || wctx.Channels[0].IsStarred {
			t.Fatal("an im un-star did not release the row")
		}
	})

	for _, itemType := range []string{"message", "file"} {
		t.Run(itemType+" star is ignored", func(t *testing.T) {
			wctx := &WorkspaceContext{TeamID: "T1", SectionStore: bootstrappedStore(t, nil)}
			wctx.Channels = []sidebar.ChannelItem{{ID: "C1", Name: "one", Type: "channel"}}
			h := &rtmEventHandler{wsCtx: wctx, workspaceID: "T1"}

			// A message star names the channel its message lives in;
			// that must not read as starring the conversation.
			h.OnStarChanged(slackclient.StarChange{ItemType: itemType, Channel: "C1"}, true)
			if starInStore(wctx, "C1") || wctx.Channels[0].IsStarred {
				t.Fatalf("a %s star claimed the row; only conversations re-section", itemType)
			}
		})
	}
}

// queueStarApply hands a successful star change to the event owner:
// the rows update when OnPendingEvents drains, not before; an apply
// whose row a newer star event already moved off the pre-request flag
// is skipped (a delayed HTTP reply must not overwrite a newer echo);
// and a handler without the wake channel (tests, unwired) drops the
// apply rather than panicking.
func TestQueueStarApply(t *testing.T) {
	wctx := &WorkspaceContext{TeamID: "T1"}
	wctx.Channels = []sidebar.ChannelItem{{ID: "C1", Name: "one", Type: "channel"}}
	h := &rtmEventHandler{wsCtx: wctx, workspaceID: "T1", starWake: make(chan struct{}, 1)}

	// prevKnown false: the apply runs unconditionally (no store to
	// have read the pre-state from).
	h.queueStarApply("C1", true, false, false)
	if wctx.Channels[0].IsStarred {
		t.Fatal("the row updated before the event owner drained the queue")
	}
	if len(h.starWake) != 1 {
		t.Fatalf("wake signals = %d; want 1", len(h.starWake))
	}

	h.OnPendingEvents()
	if !wctx.Channels[0].IsStarred {
		t.Fatal("the row did not update when the event owner drained the queue")
	}
	h.starMu.Lock()
	queued := len(h.starApplies)
	h.starMu.Unlock()
	if queued != 0 {
		t.Fatalf("queue kept %d applies after the drain; want none", queued)
	}

	// A delayed reply whose pre-state no longer matches: the row was
	// starred (prev true) when the request ran, a newer event already
	// unstarred it, so the apply's star=true is skipped.
	h.queueStarApply("C1", true, true, true)
	wctx.Channels[0].IsStarred = false // the newer star_removed echo
	h.OnPendingEvents()
	if wctx.Channels[0].IsStarred {
		t.Fatal("a superseded apply overwrote the newer event's state")
	}

	// A matching pre-state: the apply writes.
	h.queueStarApply("C1", true, false, true)
	h.OnPendingEvents()
	if !wctx.Channels[0].IsStarred {
		t.Fatal("an apply with a matching pre-state did not write")
	}

	// Unwired handler: queueStarApply is a drop, not a panic.
	h2 := &rtmEventHandler{wsCtx: wctx, workspaceID: "T1"}
	h2.queueStarApply("C1", false, true, true)
	h2.OnPendingEvents()
	if !wctx.Channels[0].IsStarred {
		t.Fatal("an unwired handler's dropped apply still changed the row")
	}
}
