package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20241219180042, Down_20241219180042)
}

func Up_20241219180042(tx *sql.Tx) error {
	// Idempotent migration.
	// The new 'url' column will only be set for software uploaded in batch via GitOps.
	if columnExists(tx, "software_installers", "url") {
		if _, err := tx.Exec(`
		ALTER TABLE software_installers
		CHANGE COLUMN url url VARCHAR(4095) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '';
	`); err != nil {
			return fmt.Errorf("failed to lengthen url in software_installers: %w", err)
		}
	}
	return nil
}

func Down_20241219180042(tx *sql.Tx) error {
	return nil
}
