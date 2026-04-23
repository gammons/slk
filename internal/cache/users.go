package cache

import "fmt"

type User struct {
	ID          string
	WorkspaceID string
	Name        string
	DisplayName string
	AvatarURL   string
	Presence    string
	UpdatedAt   int64
}

func (db *DB) UpsertUser(u User) error {
	_, err := db.conn.Exec(`
		INSERT INTO users (id, workspace_id, name, display_name, avatar_url, presence, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
			display_name=excluded.display_name,
			avatar_url=excluded.avatar_url,
			presence=excluded.presence,
			updated_at=excluded.updated_at
	`, u.ID, u.WorkspaceID, u.Name, u.DisplayName, u.AvatarURL, u.Presence, u.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upserting user: %w", err)
	}
	return nil
}

func (db *DB) GetUser(id string) (User, error) {
	var u User
	err := db.conn.QueryRow(`
		SELECT id, workspace_id, name, display_name, avatar_url, presence, updated_at
		FROM users WHERE id = ?
	`, id).Scan(&u.ID, &u.WorkspaceID, &u.Name, &u.DisplayName, &u.AvatarURL, &u.Presence, &u.UpdatedAt)
	if err != nil {
		return u, fmt.Errorf("getting user: %w", err)
	}
	return u, nil
}

func (db *DB) ListUsers(workspaceID string) ([]User, error) {
	rows, err := db.conn.Query(`
		SELECT id, workspace_id, name, display_name, avatar_url, presence, updated_at
		FROM users WHERE workspace_id = ? ORDER BY display_name, name
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.WorkspaceID, &u.Name, &u.DisplayName, &u.AvatarURL, &u.Presence, &u.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning user: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (db *DB) UpdatePresence(userID, presence string) error {
	_, err := db.conn.Exec(`UPDATE users SET presence = ? WHERE id = ?`, presence, userID)
	if err != nil {
		return fmt.Errorf("updating presence: %w", err)
	}
	return nil
}
