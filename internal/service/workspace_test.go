package service

import (
	"testing"

	"github.com/gammons/slk/internal/cache"
)

func TestWorkspaceManagerAddWorkspace(t *testing.T) {
	db, _ := cache.New(":memory:")
	defer db.Close()

	mgr := NewWorkspaceManager(db)

	mgr.AddWorkspace("T1", "Acme Corp", "acme")

	workspaces := mgr.Workspaces()
	if len(workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(workspaces))
	}
	if workspaces[0].Name != "Acme Corp" {
		t.Errorf("expected 'Acme Corp', got %q", workspaces[0].Name)
	}
}

func TestWorkspaceManagerActiveWorkspace(t *testing.T) {
	db, _ := cache.New(":memory:")
	defer db.Close()

	mgr := NewWorkspaceManager(db)
	mgr.AddWorkspace("T1", "Acme", "acme")
	mgr.AddWorkspace("T2", "Beta", "beta")

	if mgr.ActiveWorkspaceID() != "T1" {
		t.Error("expected first workspace to be active")
	}

	mgr.SetActiveWorkspace("T2")
	if mgr.ActiveWorkspaceID() != "T2" {
		t.Error("expected T2 to be active")
	}
}
