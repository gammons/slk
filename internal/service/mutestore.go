package service

import (
	"context"
	"fmt"
	"strings"
	"sync"

	slk "github.com/gammons/slk/internal/slack"
)

// MutedChannelsClient is the subset of slk.Client MuteStore needs.
// Defined as an interface so tests can pass fakes.
type MutedChannelsClient interface {
	// GetMutedChannels returns the IDs of channels the authenticated
	// user has muted. The data lives in the user's Slack prefs blob
	// (the comma-separated "muted_channels" pref); it is not exposed
	// per-channel via conversations.list.
	GetMutedChannels(ctx context.Context) ([]string, error)
}

// MuteStore is the per-workspace authoritative cache of which channels
// the authenticated user has muted. Populated on bootstrap from the
// users.prefs.get REST call and kept fresh by pref_change WebSocket
// events (see ApplyPrefChange).
//
// All public methods are safe for concurrent use.
//
// Mirrors SectionStore in shape — same Bootstrap / Ready / Apply*
// lifecycle, same nil-safety expectations from callers.
type MuteStore struct {
	mu    sync.RWMutex
	ready bool
	muted map[string]bool
}

// NewMuteStore returns an empty store. Reports Ready()==false until
// Bootstrap completes successfully.
func NewMuteStore() *MuteStore {
	return &MuteStore{muted: map[string]bool{}}
}

// Ready reports whether the store has successfully bootstrapped at
// least once. Callers should treat !Ready as "assume nothing is muted"
// (the conservative default — better to show an unread dot we should
// have suppressed than to hide one the user wanted to see).
func (s *MuteStore) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ready
}

// Bootstrap fetches the muted_channels pref and replaces any prior
// state atomically. Returns an error without mutating state if the
// fetch fails.
func (s *MuteStore) Bootstrap(ctx context.Context, client MutedChannelsClient) error {
	ids, err := client.GetMutedChannels(ctx)
	if err != nil {
		return fmt.Errorf("fetching muted channels: %w", err)
	}
	next := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		next[id] = true
	}
	s.mu.Lock()
	s.muted = next
	s.ready = true
	s.mu.Unlock()
	return nil
}

// IsMuted reports whether the given channel is currently muted by the
// authenticated user. Returns false when the store is not ready (the
// conservative default: don't claim a channel is muted unless we know).
func (s *MuteStore) IsMuted(channelID string) bool {
	if channelID == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.ready {
		return false
	}
	return s.muted[channelID]
}

// MutedChannels returns a snapshot of every channel ID currently
// recorded as muted. Empty when the store is not ready. The returned
// slice is a copy and safe to mutate.
func (s *MuteStore) MutedChannels() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.ready || len(s.muted) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.muted))
	for id := range s.muted {
		out = append(out, id)
	}
	return out
}

// ApplyPrefChange applies a pref_change WebSocket event. Two prefs are
// acted on:
//
//   - all_notifications_prefs: Slack's current (per-channel) home for
//     mute state. value is a JSON-encoded string with channels[id].muted
//     keys; ParseMutedFromAllNotificationsPrefs decodes it.
//   - muted_channels: legacy flat list, comma-separated. Kept for
//     back-compat in case Slack still ships it on some workspaces.
//
// Slack ships the full updated payload on every change for both prefs,
// so this is a wholesale replace, not an incremental delta.
//
// Returns true when the muted set actually changed, so callers can
// decide whether to trigger a sidebar refresh.
func (s *MuteStore) ApplyPrefChange(name, value string) bool {
	var next map[string]bool
	switch name {
	case "muted_channels":
		next = parseMutedChannelsPref(value)
	case "all_notifications_prefs":
		next = map[string]bool{}
		for _, id := range slk.ParseMutedFromAllNotificationsPrefs(value) {
			next[id] = true
		}
	default:
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Mark ready even on the first pref_change we see — the event
	// carries the full authoritative list, so it's a valid bootstrap
	// on its own.
	changed := !s.ready || !sameMuteSet(s.muted, next)
	s.muted = next
	s.ready = true
	return changed
}

// parseMutedChannelsPref splits Slack's comma-separated muted_channels
// pref into a set, trimming whitespace and dropping empty entries.
func parseMutedChannelsPref(raw string) map[string]bool {
	out := map[string]bool{}
	if raw == "" {
		return out
	}
	for _, part := range strings.Split(raw, ",") {
		id := strings.TrimSpace(part)
		if id == "" {
			continue
		}
		out[id] = true
	}
	return out
}

// sameMuteSet reports whether a and b contain the same channel IDs.
func sameMuteSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for id := range a {
		if !b[id] {
			return false
		}
	}
	return true
}
