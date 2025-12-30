
package db

import (
	"context"
	"fmt"
	"strings"
)

// BackupTo creates a consistent SQLite snapshot at dstPath using VACUUM INTO.
// This works even when WAL mode is enabled.
func (d *DB) BackupTo(ctx context.Context, dstPath string) error {
	// Escape single quotes for SQLite string literal
	escaped := strings.ReplaceAll(dstPath, "'", "''")
	_, err := d.sql.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s';", escaped))
	return err
}
