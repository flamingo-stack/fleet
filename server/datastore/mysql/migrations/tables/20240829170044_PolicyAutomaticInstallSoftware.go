package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20240829170044, Down_20240829170044)
}

func Up_20240829170044(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "policies", "software_installer_id") {
		if _, err := tx.Exec(`
		ALTER TABLE policies
		ADD COLUMN software_installer_id INT UNSIGNED DEFAULT NULL
	`); err != nil {
			return fmt.Errorf("failed to add software_installer_id to policies: %w", err)
		}
	}

	if !fkExists(tx, "policies", "fk_policies_software_installer_id") {
		if _, err := tx.Exec(`
		ALTER TABLE policies
		ADD FOREIGN KEY fk_policies_software_installer_id (software_installer_id) REFERENCES software_installers (id)
	`); err != nil {
			return fmt.Errorf("failed to add FK fk_policies_software_installer_id: %w", err)
		}
	}

	// We store `user_name` and `user_email` in case the user is deleted from Fleet (`user_id` set to NULL).
	if !columnsExists(tx, "software_installers", "user_id", "user_name", "user_email") {
		if _, err := tx.Exec(`
		ALTER TABLE software_installers
		ADD COLUMN user_id INT(10) UNSIGNED DEFAULT NULL,
		ADD COLUMN user_name VARCHAR(255) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
		ADD COLUMN user_email VARCHAR(255) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT ''
	`); err != nil {
			return fmt.Errorf("failed to add user_id to software_installers: %w", err)
		}
	}

	if !constraintExists(tx, "software_installers", "fk_software_installers_user_id") {
		if _, err := tx.Exec(`
		ALTER TABLE software_installers
		ADD CONSTRAINT fk_software_installers_user_id FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE SET NULL
	`); err != nil {
			return fmt.Errorf("failed to add FK fk_software_installers_user_id: %w", err)
		}
	}

	return nil
}

func Down_20240829170044(tx *sql.Tx) error {
	return nil
}
