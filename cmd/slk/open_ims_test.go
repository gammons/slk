package main

import (
	"slices"
	"testing"

	"github.com/gammons/slk/internal/bootstrap"
	"github.com/gammons/slk/internal/slack/boot"
)

func TestMissingOpenIMs(t *testing.T) {
	res := &bootstrap.Result{
		IMs: []boot.IM{
			{ID: "D1", UserID: "U1", IsOpen: true},                   // listed already
			{ID: "D2", UserID: "U2", IsOpen: true},                   // omitted by the list
			{ID: "D3", UserID: "U3"},                                 // closed
			{ID: "D4", UserID: "U4"},                                 // open through is_open only
			{ID: "D5", UserID: "U5", IsOpen: true, IsArchived: true}, // archived
			{ID: "D6", IsOpen: true},                                 // no peer
			{ID: "D2", UserID: "U2", IsOpen: true},                   // repeated
		},
		IsOpen: []string{"D4"},
	}
	got := missingOpenIMs([]string{"D1", "C1"}, res)
	var ids []string
	for _, ch := range got {
		ids = append(ids, ch.ID)
		if !ch.IsIM || ch.User == "" || !ch.IsMember {
			t.Errorf("%s is not a DM with its peer: %+v", ch.ID, ch)
		}
	}
	if want := []string{"D2", "D4"}; !slices.Equal(ids, want) {
		t.Fatalf("recovered %v, want %v", ids, want)
	}
	if got := missingOpenIMs(nil, nil); got != nil {
		t.Fatalf("nil result recovered %v", got)
	}
}

// The fallback and the recovery agree on which userBoot DMs are open.
// They differ on one input on purpose: the recovery skips a DM with no
// peer ID (D5), which the fallback keeps.
func TestMissingOpenIMsMatchesTheFallback(t *testing.T) {
	res := &bootstrap.Result{
		IMs: []boot.IM{
			{ID: "D1", UserID: "U1", IsOpen: true},
			{ID: "D2", UserID: "U2"},
			{ID: "D3", UserID: "U3"},
			{ID: "D4", UserID: "U4", IsOpen: true, IsArchived: true},
			{ID: "D5", IsOpen: true},
		},
		IsOpen: []string{"D3"},
	}
	var fallback, recovered []string
	for _, ch := range bootConversations(res) {
		if ch.User != "" {
			fallback = append(fallback, ch.ID)
		}
	}
	for _, ch := range missingOpenIMs(nil, res) {
		recovered = append(recovered, ch.ID)
	}
	if !slices.Equal(fallback, recovered) {
		t.Fatalf("fallback %v, recovery %v", fallback, recovered)
	}
}
