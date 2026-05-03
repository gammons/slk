package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	slk "github.com/gammons/slk/internal/slack"
)

// SectionsClient is the subset of slk.Client SectionStore needs.
// Defined as an interface so tests can pass fakes.
type SectionsClient interface {
	GetChannelSections(ctx context.Context) ([]slk.SidebarSection, error)
}

// SectionStore is the per-workspace authoritative cache of the user's
// Slack-side sidebar sections. Populated on bootstrap from the REST
// endpoint and kept fresh by WS event handlers (Apply* methods).
//
// All public methods are safe for concurrent use.
type SectionStore struct {
	mu               sync.RWMutex
	ready            bool
	sectionsByID     map[string]*slk.SidebarSection
	channelToSection map[string]string
	lastBootstrap    time.Time
}

// NewSectionStore returns an empty store. It reports Ready()==false until
// Bootstrap completes successfully.
func NewSectionStore() *SectionStore {
	return &SectionStore{
		sectionsByID:     map[string]*slk.SidebarSection{},
		channelToSection: map[string]string{},
	}
}

// Ready reports whether the store has successfully bootstrapped at least
// once. Callers should treat !Ready as "fall through to config-glob".
func (s *SectionStore) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ready
}

// Bootstrap fetches the section list and replaces any prior state
// atomically. Returns an error without mutating state if the fetch fails.
//
// v1 limitation (Task 3 deferred): when ChannelsCount exceeds
// len(ChannelIDs), the section is partially populated. We trust what we
// have; remaining channels stay in the catch-all bucket until either a
// WS channel_sections_channels_upserted event migrates them or a
// reconnect-triggered re-bootstrap fetches fresher data.
func (s *SectionStore) Bootstrap(ctx context.Context, client SectionsClient) error {
	sections, err := client.GetChannelSections(ctx)
	if err != nil {
		return fmt.Errorf("fetching sections: %w", err)
	}

	for i := range sections {
		sec := &sections[i]
		if sec.ChannelsCount > len(sec.ChannelIDs) {
			log.Printf("section store: section %q (%s) reports %d channels but server returned %d on first page; remaining channels will fall through to default bucket",
				sec.Name, sec.ID, sec.ChannelsCount, len(sec.ChannelIDs))
		}
	}

	// Build new maps.
	byID := make(map[string]*slk.SidebarSection, len(sections))
	c2s := map[string]string{}
	for i := range sections {
		sec := &sections[i]
		byID[sec.ID] = sec
		for _, ch := range sec.ChannelIDs {
			c2s[ch] = sec.ID
		}
	}

	s.mu.Lock()
	s.sectionsByID = byID
	s.channelToSection = c2s
	s.ready = true
	s.lastBootstrap = time.Now()
	s.mu.Unlock()
	return nil
}

// SectionForChannel returns the section ID a channel belongs to. Returns
// ok=false when the store isn't ready or the channel isn't in any section.
// SectionForChannel returns the renderable section ID a channel belongs
// to. Returns ok=false when the store isn't ready, the channel isn't
// indexed, OR the indexed section is not renderable in the v1 sidebar
// (e.g. stars, slack_connect, salesforce_records, agents). Hiding
// non-renderable sections at this boundary prevents the sidebar from
// trying to bucket items into headers it never created — see
// includeInSidebar for the renderability rule.
func (s *SectionStore) SectionForChannel(channelID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.ready {
		return "", false
	}
	id, ok := s.channelToSection[channelID]
	if !ok {
		return "", false
	}
	sec, secOK := s.sectionsByID[id]
	if !secOK || !includeInSidebar(sec) {
		// Section was deleted, or has a non-renderable type (stars /
		// slack_connect / etc.). Treat the channel as unclaimed so it
		// falls into the appropriate type-default bucket.
		return "", false
	}
	return id, true
}

// OrderedSections walks the linked-list (head-first) and returns the
// sections that should render in the sidebar, filtered to the v1
// type whitelist. Cycle protection: stops if a section is revisited.
//
// Head detection: a section is the head if no other section's Next
// points at it. When multiple candidate heads exist (orphans), the
// one with the highest LastUpdate wins as a heuristic; this is a
// best-effort recovery for malformed state.
func (s *SectionStore) OrderedSections() []*slk.SidebarSection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.ready {
		return nil
	}

	pointedAt := map[string]bool{}
	for _, sec := range s.sectionsByID {
		if sec.Next != "" {
			pointedAt[sec.Next] = true
		}
	}
	var head *slk.SidebarSection
	for id, sec := range s.sectionsByID {
		if !pointedAt[id] {
			if head == nil || sec.LastUpdate > head.LastUpdate {
				head = sec
			}
		}
	}
	if head == nil {
		// Cycle or empty.
		return nil
	}

	out := make([]*slk.SidebarSection, 0, len(s.sectionsByID))
	visited := map[string]bool{}
	cur := head
	for cur != nil && !visited[cur.ID] {
		visited[cur.ID] = true
		if includeInSidebar(cur) {
			out = append(out, cur)
		}
		if cur.Next == "" {
			break
		}
		cur = s.sectionsByID[cur.Next]
	}
	return out
}

// includeInSidebar applies the v1 filter rules. Renderable types:
// standard (always, even when empty — user intent), channels (default
// catch-all), direct_messages (default DM bucket). recent_apps is only
// rendered when non-empty (slk has its own Apps logic for the empty
// case). Everything else is hidden in v1.
func includeInSidebar(sec *slk.SidebarSection) bool {
	if sec.IsRedacted {
		return false
	}
	switch sec.Type {
	case "standard", "channels", "direct_messages":
		return true
	case "recent_apps":
		return len(sec.ChannelIDs) > 0
	default:
		// stars, slack_connect, salesforce_records, agents, anything new.
		return false
	}
}

// ApplyUpsert applies a channel_section_upserted WS event (also used
// for create / rename / reorder / emoji change). Last-write-wins by
// LastUpdate: stale events are dropped.
func (s *SectionStore) ApplyUpsert(ev slk.ChannelSectionUpserted) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		return
	}
	if existing, ok := s.sectionsByID[ev.ID]; ok && ev.LastUpdate < existing.LastUpdate {
		return
	}
	prev := s.sectionsByID[ev.ID]
	sec := &slk.SidebarSection{
		ID:         ev.ID,
		Name:       ev.Name,
		Type:       ev.Type,
		Emoji:      ev.Emoji,
		Next:       ev.Next,
		LastUpdate: ev.LastUpdate,
		IsRedacted: ev.IsRedacted,
	}
	if prev != nil {
		// Preserve channel membership; upsert events don't carry it.
		sec.ChannelIDs = prev.ChannelIDs
		sec.ChannelsCount = prev.ChannelsCount
	}
	s.sectionsByID[ev.ID] = sec
}

// ApplyDelete applies a channel_section_deleted WS event.
func (s *SectionStore) ApplyDelete(sectionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		return
	}
	delete(s.sectionsByID, sectionID)
	for ch, sec := range s.channelToSection {
		if sec == sectionID {
			delete(s.channelToSection, ch)
		}
	}
}

// ApplyChannelsAdded applies a channel_sections_channels_upserted WS event.
// A channel can only belong to one section, so adding to section X
// implicitly removes it from any prior section in our index.
func (s *SectionStore) ApplyChannelsAdded(sectionID string, channelIDs []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		return
	}
	sec, ok := s.sectionsByID[sectionID]
	if !ok {
		// Section we don't know about yet; skip — bootstrap or upsert
		// will reconcile.
		return
	}
	added := map[string]bool{}
	for _, ch := range sec.ChannelIDs {
		added[ch] = true
	}
	for _, ch := range channelIDs {
		if !added[ch] {
			sec.ChannelIDs = append(sec.ChannelIDs, ch)
			added[ch] = true
		}
		// Remove from any other section's ChannelIDs.
		if prevSec, prev := s.channelToSection[ch]; prev && prevSec != sectionID {
			if old, ok := s.sectionsByID[prevSec]; ok {
				filtered := old.ChannelIDs[:0]
				for _, x := range old.ChannelIDs {
					if x != ch {
						filtered = append(filtered, x)
					}
				}
				old.ChannelIDs = filtered
			}
		}
		s.channelToSection[ch] = sectionID
	}
}

// ApplyChannelsRemoved applies a channel_sections_channels_removed WS event.
func (s *SectionStore) ApplyChannelsRemoved(sectionID string, channelIDs []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		return
	}
	sec, ok := s.sectionsByID[sectionID]
	if !ok {
		return
	}
	dropped := map[string]bool{}
	for _, ch := range channelIDs {
		dropped[ch] = true
		if cur, ok := s.channelToSection[ch]; ok && cur == sectionID {
			delete(s.channelToSection, ch)
		}
	}
	filtered := sec.ChannelIDs[:0]
	for _, ch := range sec.ChannelIDs {
		if !dropped[ch] {
			filtered = append(filtered, ch)
		}
	}
	sec.ChannelIDs = filtered
}

// MaybeRebootstrap re-runs Bootstrap when the previous successful one was
// more than 30 seconds ago. Cheap insurance against missed events during
// disconnects without thundering during a flapping connection.
func (s *SectionStore) MaybeRebootstrap(ctx context.Context, client SectionsClient) error {
	s.mu.RLock()
	last := s.lastBootstrap
	s.mu.RUnlock()
	if !last.IsZero() && time.Since(last) < 30*time.Second {
		return nil
	}
	return s.Bootstrap(ctx, client)
}
