
package db

import (
	"context"
	"database/sql"
)

func (d *DB) GetLastValues(ctx context.Context, chatID int64, itemIDs []string) (map[string]float64, error) {
	out := map[string]float64{}
	if len(itemIDs) == 0 {
		return out, nil
	}

	// Build a simple IN query with placeholders
	q := `SELECT item_id,last_value FROM chat_last_values WHERE chat_id=? AND item_id IN (` + placeholders(len(itemIDs)) + `)`
	args := []any{chatID}
	for _, id := range itemIDs {
		args = append(args, id)
	}
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var v float64
		if err := rows.Scan(&id, &v); err != nil {
			return nil, err
		}
		out[id] = v
	}
	return out, nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	s := "?"
	for i := 1; i < n; i++ {
		s += ",?"
	}
	return s
}

func (d *DB) GetLastPostMessageID(ctx context.Context, chatID int64) (sql.NullInt64, error) {
	var mid sql.NullInt64
	err := d.sql.QueryRowContext(ctx, `SELECT last_post_message_id FROM chat_settings WHERE chat_id=?`, chatID).Scan(&mid)
	return mid, err
}
