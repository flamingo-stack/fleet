package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20240702123921, Down_20240702123921)
}

func Up_20240702123921(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "nano_enrollments", "enrolled_from_migration") {
		if _, err := tx.Exec(`ALTER TABLE nano_enrollments ADD COLUMN enrolled_from_migration TINYINT(1) UNSIGNED NOT NULL DEFAULT '0'`); err != nil {
			return fmt.Errorf("failed to add enrolled_from_migration to nano_enrollments: %w", err)
		}
	}
	return nil
}

func Down_20240702123921(tx *sql.Tx) error {
	return nil
}
