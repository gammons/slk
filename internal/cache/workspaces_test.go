package cache

import (
	"testing"
)

func TestUpsertAndGetWorkspace(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ws := Workspace{
		ID:     "T123",
		Name:   "Acme Corp",
		Domain: "acme",
	}

	if err := db.UpsertWorkspace(ws); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetWorkspace("T123")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Acme Corp" {
		t.Errorf("expected name 'Acme Corp', got %q", got.Name)
	}
	if got.Domain != "acme" {
		t.Errorf("expected domain 'acme', got %q", got.Domain)
	}
}

func TestListWorkspaces(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.UpsertWorkspace(Workspace{ID: "T1", Name: "Team 1", Domain: "t1"})
	db.UpsertWorkspace(Workspace{ID: "T2", Name: "Team 2", Domain: "t2"})

	workspaces, err := db.ListWorkspaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaces) != 2 {
		t.Errorf("expected 2 workspaces, got %d", len(workspaces))
	}
}
