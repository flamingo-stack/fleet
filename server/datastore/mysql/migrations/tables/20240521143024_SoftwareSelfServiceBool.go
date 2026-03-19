package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20240521143024, Down_20240521143024)
}

func Up_20240521143024(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "software_installers", "self_service") {
		if _, err := tx.Exec(`ALTER TABLE software_installers ADD COLUMN self_service bool NOT NULL DEFAULT false`); err != nil {
			return fmt.Errorf("failed to add self_service to software_installers: %w", err)
		}
	}

	if !columnExists(tx, "host_software_installs", "self_service") {
		if _, err := tx.Exec(`ALTER TABLE host_software_installs ADD COLUMN self_service bool NOT NULL DEFAULT false`); err != nil {
			return fmt.Errorf("failed to add self_service bool to host_software_installs: %w", err)
		}
	}

	return nil
}

func Down_20240521143024(tx *sql.Tx) error {
	return nil
}
