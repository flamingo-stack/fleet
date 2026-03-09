package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20240703154849, Down_20240703154849)
}

func Up_20240703154849(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "mdm_configuration_profile_labels", "exclude") {
		if _, err := tx.Exec(`ALTER TABLE mdm_configuration_profile_labels ADD COLUMN exclude TINYINT(1) NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("failed to add exclude boolean to mdm_configuration_profile_labels: %w", err)
		}
	}

	if !columnExists(tx, "mdm_declaration_labels", "exclude") {
		if _, err := tx.Exec(`ALTER TABLE mdm_declaration_labels ADD COLUMN exclude TINYINT(1) NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("failed to add exclude boolean to mdm_declaration_labels: %w", err)
		}
	}
	return nil
}

func Down_20240703154849(tx *sql.Tx) error {
	return nil
}
