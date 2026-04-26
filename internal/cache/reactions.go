package cache

import (
	"encoding/json"
	"fmt"
)

type ReactionRow struct {
	Emoji   string
	UserIDs []string
	Count   int
}

func (db *DB) UpsertReaction(messageTS, channelID, emoji string, userIDs []string, count int) error {
	userIDsJSON, err := json.Marshal(userIDs)
	if err != nil {
		return fmt.Errorf("marshal user_ids: %w", err)
	}

	_, err = db.conn.Exec(`
		INSERT INTO reactions (message_ts, channel_id, emoji, user_ids, count)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(message_ts, channel_id, emoji)
		DO UPDATE SET user_ids = excluded.user_ids, count = excluded.count`,
		messageTS, channelID, emoji, string(userIDsJSON), count,
	)
	return err
}

func (db *DB) GetReactions(messageTS, channelID string) ([]ReactionRow, error) {
	rows, err := db.conn.Query(`
		SELECT emoji, user_ids, count
		FROM reactions
		WHERE message_ts = ? AND channel_id = ?`,
		messageTS, channelID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ReactionRow
	for rows.Next() {
		var r ReactionRow
		var userIDsJSON string
		if err := rows.Scan(&r.Emoji, &userIDsJSON, &r.Count); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(userIDsJSON), &r.UserIDs); err != nil {
			return nil, fmt.Errorf("unmarshal user_ids: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func (db *DB) DeleteReaction(messageTS, channelID, emoji string) error {
	_, err := db.conn.Exec(`
		DELETE FROM reactions
		WHERE message_ts = ? AND channel_id = ? AND emoji = ?`,
		messageTS, channelID, emoji,
	)
	return err
}
