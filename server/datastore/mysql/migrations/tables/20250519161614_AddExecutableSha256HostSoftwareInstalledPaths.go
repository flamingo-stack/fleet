package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20250519161614, Down_20250519161614)
}

func Up_20250519161614(tx *sql.Tx) error {
	// Idempotent migration.
	// Using char over varchar because sha256 is always a fixed length of 64 characters
	if !columnExists(tx, "host_software_installed_paths", "executable_sha256") {
		if _, err := tx.Exec(`
		ALTER TABLE host_software_installed_paths
		ADD COLUMN executable_sha256 CHAR(64) COLLATE utf8mb4_unicode_ci NULL
		`); err != nil {
			return fmt.Errorf("failed to add column 'executable_sha256' to 'host_software_installed_paths': %w", err)
		}
	}
	return nil
}

func Down_20250519161614(tx *sql.Tx) error {
	return nil
}
