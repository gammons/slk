package cache

import "fmt"

type User struct {
	ID          string
	WorkspaceID string
	Name        string
	DisplayName string
	AvatarURL   string
	Presence    string
	// IsBot is true for Slack apps and classic bots (the union of
	// slack.User.IsBot and IsAppUser). Used to bucket their DMs into
	// the "Apps" sidebar section so they don't clutter the human DM
	// list.
	IsBot bool
	// IsExternal is true for users whose home team_id differs from
	// the workspace's TeamID (Slack Connect / shared-channel guests).
	// Set by the user-resolution path; persisted so we don't re-resolve.
	IsExternal bool
	// StatusEmoji, StatusText and StatusExpiration are the user's
	// custom status as Slack last reported it. StatusExpiration is a
	// Unix time; 0 means the status never expires. An expired status
	// is stored as sent, and renderers decide whether to show it.
	StatusEmoji      string
	StatusText       string
	StatusExpiration int64
	// HuddleState and HuddleExpiration are Slack's huddle_state and
	// huddle_state_expiration_ts, stored as sent like the status.
	// "default_unset" (or empty) means the user is not in a huddle.
	HuddleState      string
	HuddleExpiration int64
	UpdatedAt        int64
}

// UpsertUser writes the status and huddle columns on insert only.
// Several callers upsert a placeholder record built without a profile,
// so writing them on conflict would blank real values; changes go
// through UpdateUserStatus and the edge revalidation writes instead.
func (db *DB) UpsertUser(u User) error {
	_, err := db.conn.Exec(`
		INSERT INTO users (id, workspace_id, name, display_name, avatar_url, presence, is_bot, is_external, status_emoji, status_text, status_expiration, huddle_state, huddle_expiration, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
			display_name=excluded.display_name,
			avatar_url=excluded.avatar_url,
			presence=excluded.presence,
			is_bot=excluded.is_bot,
			is_external=excluded.is_external,
			updated_at=excluded.updated_at
	`, u.ID, u.WorkspaceID, u.Name, u.DisplayName, u.AvatarURL, u.Presence, u.IsBot, u.IsExternal,
		u.StatusEmoji, u.StatusText, u.StatusExpiration, u.HuddleState, u.HuddleExpiration, u.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upserting user: %w", err)
	}
	return nil
}

func (db *DB) GetUser(id string) (User, error) {
	var u User
	err := db.conn.QueryRow(`
		SELECT id, workspace_id, name, display_name, avatar_url, presence, is_bot, is_external, status_emoji, status_text, status_expiration, huddle_state, huddle_expiration, updated_at
		FROM users WHERE id = ?
	`, id).Scan(&u.ID, &u.WorkspaceID, &u.Name, &u.DisplayName, &u.AvatarURL, &u.Presence, &u.IsBot, &u.IsExternal,
		&u.StatusEmoji, &u.StatusText, &u.StatusExpiration, &u.HuddleState, &u.HuddleExpiration, &u.UpdatedAt)
	if err != nil {
		return u, fmt.Errorf("getting user: %w", err)
	}
	return u, nil
}

func (db *DB) ListUsers(workspaceID string) ([]User, error) {
	rows, err := db.conn.Query(`
		SELECT id, workspace_id, name, display_name, avatar_url, presence, is_bot, is_external, status_emoji, status_text, status_expiration, huddle_state, huddle_expiration, updated_at
		FROM users WHERE workspace_id = ? ORDER BY display_name, name
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.WorkspaceID, &u.Name, &u.DisplayName, &u.AvatarURL, &u.Presence, &u.IsBot, &u.IsExternal,
			&u.StatusEmoji, &u.StatusText, &u.StatusExpiration, &u.HuddleState, &u.HuddleExpiration, &u.UpdatedAt); err != nil {
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

// ListUserStatuses returns the users in workspaceID that have a custom
// status set or are in a huddle, with only ID and the status and huddle
// fields populated. Expired values are included as stored; renderers
// hide them.
func (db *DB) ListUserStatuses(workspaceID string) ([]User, error) {
	rows, err := db.conn.Query(`
		SELECT id, status_emoji, status_text, status_expiration, huddle_state, huddle_expiration
		FROM users
		WHERE workspace_id = ?
			AND (status_emoji != '' OR status_text != '' OR huddle_state = 'in_a_huddle')
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("listing user statuses: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		u := User{WorkspaceID: workspaceID}
		if err := rows.Scan(&u.ID, &u.StatusEmoji, &u.StatusText, &u.StatusExpiration, &u.HuddleState, &u.HuddleExpiration); err != nil {
			return nil, fmt.Errorf("scanning user status: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// UpdateUserStatus records a user's custom status and huddle state.
// Empty values with zero expirations clear them.
func (db *DB) UpdateUserStatus(userID, emoji, text string, expiration int64, huddleState string, huddleExpiration int64) error {
	_, err := db.conn.Exec(`
		UPDATE users
		SET status_emoji = ?, status_text = ?, status_expiration = ?, huddle_state = ?, huddle_expiration = ?
		WHERE id = ?`,
		emoji, text, expiration, huddleState, huddleExpiration, userID)
	if err != nil {
		return fmt.Errorf("updating user status: %w", err)
	}
	return nil
}
