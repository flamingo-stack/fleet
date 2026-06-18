package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20260217200906, Down_20260217200906)
}

func Up_20260217200906(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "host_device_auth", "previous_token") {
		if _, err := tx.Exec(`ALTER TABLE host_device_auth ADD COLUMN previous_token VARCHAR(255) COLLATE utf8mb4_unicode_ci DEFAULT NULL`); err != nil {
			return fmt.Errorf("adding previous_token to host_device_auth: %w", err)
		}
	}
	if !indexExistsTx(tx, "host_device_auth", "idx_host_device_auth_previous_token") {
		if _, err := tx.Exec(`ALTER TABLE host_device_auth ADD INDEX idx_host_device_auth_previous_token (previous_token)`); err != nil {
			return fmt.Errorf("adding idx_host_device_auth_previous_token to host_device_auth: %w", err)
		}
	}
	return nil
}

func Down_20260217200906(tx *sql.Tx) error {
	return nil
}
