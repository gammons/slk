package cache

import (
	"fmt"
	"time"
)

func (db *DB) RecordEmojiUse(emoji string) error {
	now := time.Now().Unix()
	_, err := db.conn.Exec(`
		INSERT INTO frecent_emoji (emoji, use_count, last_used)
		VALUES (?, 1, ?)
		ON CONFLICT(emoji)
		DO UPDATE SET use_count = use_count + 1, last_used = excluded.last_used`,
		emoji, now,
	)
	if err != nil {
		return fmt.Errorf("recording emoji use: %w", err)
	}
	return nil
}

func (db *DB) GetFrecentEmoji(limit int) ([]string, error) {
	now := time.Now().Unix()
	rows, err := db.conn.Query(`
		SELECT emoji
		FROM frecent_emoji
		ORDER BY CAST(use_count AS REAL) / (1.0 + (? - last_used) / 86400.0) DESC
		LIMIT ?`,
		now, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("querying frecent emoji: %w", err)
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var emoji string
		if err := rows.Scan(&emoji); err != nil {
			return nil, fmt.Errorf("scanning frecent emoji: %w", err)
		}
		result = append(result, emoji)
	}
	return result, rows.Err()
}
