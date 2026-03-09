package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20240905140514, Down_20240905140514)
}

func Up_20240905140514(tx *sql.Tx) error {
	// Idempotent migration.
	// The new 'url' column will only be set for software uploaded in batch via GitOps.
	if !columnExists(tx, "software_installers", "url") {
		if _, err := tx.Exec(`
		ALTER TABLE software_installers
		ADD COLUMN url VARCHAR(255) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '';
	`); err != nil {
			return fmt.Errorf("failed to add url to software_installers: %w", err)
		}
	}
	return nil
}

func Down_20240905140514(tx *sql.Tx) error {
	return nil
}
