package main

import (
	"sync"

	"github.com/gammons/slk/internal/config"
)

// workspaceConfigStore holds cfg.Workspaces. SaveTheme and
// SaveSidebarWidth are the in-memory half of the theme/sidebar-width
// savers wired in run(), mutating the map in place on the Update
// goroutine. Snapshot is what every connect goroutine does with cfg
// before reading it further (WorkspaceByTeamID, MatchSectionAndOrder,
// SectionOrder, ResolveTheme, ResolveWidth).
//
// mu guards cfg.Workspaces: a value copy of config.Config does not
// copy Workspaces independently -- Workspaces is a map, so a naive
// Snapshot's copy would alias the same underlying map SaveTheme and
// SaveSidebarWidth mutate from another goroutine. Snapshot instead
// takes its own independent copy under mu, and the savers hold mu for
// their read-modify-write. See TestWorkspaceConfigStore_ConcurrentSaveAndSnapshot.
type workspaceConfigStore struct {
	mu  sync.RWMutex
	cfg config.Config
}

func newWorkspaceConfigStore(cfg config.Config) *workspaceConfigStore {
	return &workspaceConfigStore{cfg: cfg}
}

// SaveTheme finds or creates teamID's TOML block and sets its Theme,
// returning the TOML key for the caller to persist to disk.
func (s *workspaceConfigStore) SaveTheme(teamID, name string) (tomlKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tomlKey = s.tomlKeyFor(teamID)
	ws := s.cfg.Workspaces[tomlKey]
	ws.TeamID = teamID
	ws.Theme = name
	s.cfg.Workspaces[tomlKey] = ws
	return tomlKey
}

// SaveSidebarWidth is SaveTheme's sibling for sidebar width.
func (s *workspaceConfigStore) SaveSidebarWidth(teamID string, width int) (tomlKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tomlKey = s.tomlKeyFor(teamID)
	ws := s.cfg.Workspaces[tomlKey]
	ws.TeamID = teamID
	ws.SidebarWidth = width
	s.cfg.Workspaces[tomlKey] = ws
	return tomlKey
}

// tomlKeyFor finds the existing TOML key for teamID, if any -- if no
// block exists yet it falls back to the team ID itself (legacy
// default; a future --add-workspace may have already written a
// slug-keyed block) -- and ensures Workspaces is non-nil. Callers must
// hold mu.
func (s *workspaceConfigStore) tomlKeyFor(teamID string) string {
	if s.cfg.Workspaces == nil {
		s.cfg.Workspaces = make(map[string]config.Workspace)
	}
	for k, w := range s.cfg.Workspaces {
		if w.TeamID == teamID {
			return k
		}
	}
	return teamID
}

// Snapshot returns cfg with its own independent copy of Workspaces, so
// the caller's later reads (via WorkspaceByTeamID, MatchSectionAndOrder,
// SectionOrder, ResolveTheme, ResolveWidth) never alias the map
// SaveTheme/SaveSidebarWidth mutate. Call once per workspace connect.
func (s *workspaceConfigStore) Snapshot() config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg := s.cfg
	clone := make(map[string]config.Workspace, len(cfg.Workspaces))
	for k, v := range cfg.Workspaces {
		clone[k] = v
	}
	cfg.Workspaces = clone
	return cfg
}
