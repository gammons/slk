package service

import (
	"sync"

	"github.com/gammons/slack-tui/internal/cache"
)

type WorkspaceInfo struct {
	ID     string
	Name   string
	Domain string
}

type WorkspaceManager struct {
	mu         sync.RWMutex
	db         *cache.DB
	workspaces []WorkspaceInfo
	activeIdx  int
}

func NewWorkspaceManager(db *cache.DB) *WorkspaceManager {
	return &WorkspaceManager{
		db:        db,
		activeIdx: 0,
	}
}

func (m *WorkspaceManager) AddWorkspace(id, name, domain string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.workspaces = append(m.workspaces, WorkspaceInfo{
		ID:     id,
		Name:   name,
		Domain: domain,
	})

	m.db.UpsertWorkspace(cache.Workspace{
		ID:     id,
		Name:   name,
		Domain: domain,
	})
}

func (m *WorkspaceManager) Workspaces() []WorkspaceInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]WorkspaceInfo, len(m.workspaces))
	copy(result, m.workspaces)
	return result
}

func (m *WorkspaceManager) ActiveWorkspaceID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.workspaces) == 0 {
		return ""
	}
	return m.workspaces[m.activeIdx].ID
}

func (m *WorkspaceManager) ActiveWorkspace() (WorkspaceInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.workspaces) == 0 {
		return WorkspaceInfo{}, false
	}
	return m.workspaces[m.activeIdx], true
}

func (m *WorkspaceManager) SetActiveWorkspace(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, ws := range m.workspaces {
		if ws.ID == id {
			m.activeIdx = i
			return
		}
	}
}

func (m *WorkspaceManager) SetActiveByIndex(idx int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if idx < 0 || idx >= len(m.workspaces) {
		return false
	}
	m.activeIdx = idx
	return true
}
