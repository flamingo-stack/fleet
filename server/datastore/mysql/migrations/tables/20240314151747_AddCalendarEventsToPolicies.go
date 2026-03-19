package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20240314151747, Down_20240314151747)
}

func Up_20240314151747(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "policies", "calendar_events_enabled") {
		if _, err := tx.Exec(`ALTER TABLE policies ADD COLUMN calendar_events_enabled TINYINT(1) UNSIGNED NOT NULL DEFAULT '0'`); err != nil {
			return fmt.Errorf("failed to add calendar_events_enabled to policies: %w", err)
		}
	}
	return nil
}

func Down_20240314151747(_ *sql.Tx) error {
	return nil
}
