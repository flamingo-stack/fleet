package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20260126150840, Down_20260126150840)
}

func Up_20260126150840(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "mdm_windows_enrollments", "credentials_hash") {
		if _, err := tx.Exec(`ALTER TABLE mdm_windows_enrollments ADD COLUMN credentials_hash BINARY(16)`); err != nil {
			return fmt.Errorf("adding credentials_hash to mdm_windows_enrollments: %w", err)
		}
	}
	if !columnExists(tx, "mdm_windows_enrollments", "credentials_acknowledged") {
		if _, err := tx.Exec(`ALTER TABLE mdm_windows_enrollments ADD COLUMN credentials_acknowledged BOOLEAN NOT NULL DEFAULT FALSE`); err != nil {
			return fmt.Errorf("adding credentials_acknowledged to mdm_windows_enrollments: %w", err)
		}
	}
	return nil
}

func Down_20260126150840(tx *sql.Tx) error {
	return nil
}
