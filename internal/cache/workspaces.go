package cache

import "fmt"

type Workspace struct {
	ID           string
	Name         string
	Domain       string
	IconURL      string
	LastSyncedAt int64
}

func (db *DB) UpsertWorkspace(ws Workspace) error {
	_, err := db.conn.Exec(`
		INSERT INTO workspaces (id, name, domain, icon_url, last_synced_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
			domain=excluded.domain,
			icon_url=excluded.icon_url,
			last_synced_at=excluded.last_synced_at
	`, ws.ID, ws.Name, ws.Domain, ws.IconURL, ws.LastSyncedAt)
	if err != nil {
		return fmt.Errorf("upserting workspace: %w", err)
	}
	return nil
}

func (db *DB) GetWorkspace(id string) (Workspace, error) {
	var ws Workspace
	err := db.conn.QueryRow(`
		SELECT id, name, domain, icon_url, last_synced_at
		FROM workspaces WHERE id = ?
	`, id).Scan(&ws.ID, &ws.Name, &ws.Domain, &ws.IconURL, &ws.LastSyncedAt)
	if err != nil {
		return ws, fmt.Errorf("getting workspace: %w", err)
	}
	return ws, nil
}

func (db *DB) ListWorkspaces() ([]Workspace, error) {
	rows, err := db.conn.Query(`
		SELECT id, name, domain, icon_url, last_synced_at
		FROM workspaces ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("listing workspaces: %w", err)
	}
	defer rows.Close()

	var workspaces []Workspace
	for rows.Next() {
		var ws Workspace
		if err := rows.Scan(&ws.ID, &ws.Name, &ws.Domain, &ws.IconURL, &ws.LastSyncedAt); err != nil {
			return nil, fmt.Errorf("scanning workspace: %w", err)
		}
		workspaces = append(workspaces, ws)
	}
	return workspaces, rows.Err()
}
