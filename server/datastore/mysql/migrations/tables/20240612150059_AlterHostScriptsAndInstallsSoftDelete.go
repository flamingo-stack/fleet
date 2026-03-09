package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20240612150059, Down_20240612150059)
}

func Up_20240612150059(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "host_script_results", "host_deleted_at") {
		if _, err := tx.Exec(`ALTER TABLE host_script_results ADD COLUMN host_deleted_at TIMESTAMP NULL`); err != nil {
			return fmt.Errorf("failed to add host_deleted_at timestamp to host_script_results: %w", err)
		}
	}
	return nil
}

func Down_20240612150059(tx *sql.Tx) error {
	return nil
}
