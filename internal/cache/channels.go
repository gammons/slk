package cache

import "fmt"

type Channel struct {
	ID          string
	WorkspaceID string
	Name        string
	Type        string // channel, dm, group_dm, private
	Topic       string
	IsMember    bool
	IsStarred   bool
	LastReadTS  string
	UnreadCount int
	UpdatedAt   int64
}

func (db *DB) UpsertChannel(ch Channel) error {
	_, err := db.conn.Exec(`
		INSERT INTO channels (id, workspace_id, name, type, topic, is_member, is_starred, last_read_ts, unread_count, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
			type=excluded.type,
			topic=excluded.topic,
			is_member=excluded.is_member,
			is_starred=excluded.is_starred,
			last_read_ts=excluded.last_read_ts,
			unread_count=excluded.unread_count,
			updated_at=excluded.updated_at
	`, ch.ID, ch.WorkspaceID, ch.Name, ch.Type, ch.Topic,
		boolToInt(ch.IsMember), boolToInt(ch.IsStarred),
		ch.LastReadTS, ch.UnreadCount, ch.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upserting channel: %w", err)
	}
	return nil
}

func (db *DB) GetChannel(id string) (Channel, error) {
	var ch Channel
	var isMember, isStarred int
	err := db.conn.QueryRow(`
		SELECT id, workspace_id, name, type, topic, is_member, is_starred, last_read_ts, unread_count, updated_at
		FROM channels WHERE id = ?
	`, id).Scan(&ch.ID, &ch.WorkspaceID, &ch.Name, &ch.Type, &ch.Topic,
		&isMember, &isStarred, &ch.LastReadTS, &ch.UnreadCount, &ch.UpdatedAt)
	if err != nil {
		return ch, fmt.Errorf("getting channel: %w", err)
	}
	ch.IsMember = isMember == 1
	ch.IsStarred = isStarred == 1
	return ch, nil
}

func (db *DB) ListChannels(workspaceID string, membersOnly bool) ([]Channel, error) {
	query := `
		SELECT id, workspace_id, name, type, topic, is_member, is_starred, last_read_ts, unread_count, updated_at
		FROM channels WHERE workspace_id = ?`
	args := []any{workspaceID}

	if membersOnly {
		query += " AND is_member = 1"
	}
	query += " ORDER BY name"

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing channels: %w", err)
	}
	defer rows.Close()

	var channels []Channel
	for rows.Next() {
		var ch Channel
		var isMember, isStarred int
		if err := rows.Scan(&ch.ID, &ch.WorkspaceID, &ch.Name, &ch.Type, &ch.Topic,
			&isMember, &isStarred, &ch.LastReadTS, &ch.UnreadCount, &ch.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning channel: %w", err)
		}
		ch.IsMember = isMember == 1
		ch.IsStarred = isStarred == 1
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

func (db *DB) UpdateUnreadCount(channelID string, count int) error {
	_, err := db.conn.Exec(`UPDATE channels SET unread_count = ? WHERE id = ?`, count, channelID)
	if err != nil {
		return fmt.Errorf("updating unread count: %w", err)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
