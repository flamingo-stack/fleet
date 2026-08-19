package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20260803135530, Down_20260803135530)
}

func Up_20260803135530(tx *sql.Tx) error {
	// Singleton config row holding the default team (fleet) that new user-driven Windows MDM enrollments are assigned to.
	// default_team_id is NULL when no default is configured, and is nulled by the FK when the referenced team is deleted.
	_, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS mdm_windows_enrollment_config (
			id INT UNSIGNED NOT NULL PRIMARY KEY,
			default_team_id INT UNSIGNED DEFAULT NULL,
			created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
			updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
			CONSTRAINT ck_mdm_windows_enrollment_config_singleton CHECK (id = 1),
			CONSTRAINT fk_mdm_windows_enrollment_config_default_team_id
				FOREIGN KEY (default_team_id) REFERENCES teams (id) ON DELETE SET NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return fmt.Errorf("create mdm_windows_enrollment_config: %w", err)
	}

	// Seed the singleton row here so the setter is a plain UPDATE rather than an upsert. The timestamps are explicit for schema.sql
	_, err = tx.Exec(`
		INSERT IGNORE INTO mdm_windows_enrollment_config (id, default_team_id, created_at, updated_at)
		VALUES (1, NULL, '2026-08-03 00:00:00', '2026-08-03 00:00:00')
	`)
	if err != nil {
		return fmt.Errorf("seed mdm_windows_enrollment_config: %w", err)
	}

	// hardware_serial is the SMBIOS serial the device reports over OMA-DM (DevDetail). It is persisted while the enrollment is still
	// unlinked (host_uuid = "") so the orbit enrollment path can reverse-link the enrollment to the host it just created.
	// Idempotent migration. The CREATE TABLE / INSERT above are already IF NOT EXISTS / IGNORE;
	// the column add and the index swap are split so a partially-applied run can finish.
	if !columnExists(tx, "mdm_windows_enrollments", "hardware_serial") {
		if _, err = tx.Exec(`
		ALTER TABLE mdm_windows_enrollments
		ADD COLUMN hardware_serial VARCHAR(255) COLLATE utf8mb4_unicode_ci DEFAULT NULL
	`); err != nil {
			return fmt.Errorf("add hardware_serial to mdm_windows_enrollments: %w", err)
		}
	}
	if !indexExistsTx(tx, "mdm_windows_enrollments", "idx_mdm_windows_enrollments_host_uuid_hardware_serial") {
		if _, err = tx.Exec(`
		ALTER TABLE mdm_windows_enrollments
		ADD INDEX idx_mdm_windows_enrollments_host_uuid_hardware_serial (host_uuid, hardware_serial)
	`); err != nil {
			return fmt.Errorf("add (host_uuid, hardware_serial) index on mdm_windows_enrollments: %w", err)
		}
	}
	if indexExistsTx(tx, "mdm_windows_enrollments", "idx_mdm_windows_enrollments_host_uuid") {
		if _, err = tx.Exec(`
		ALTER TABLE mdm_windows_enrollments
		DROP INDEX idx_mdm_windows_enrollments_host_uuid
	`); err != nil {
			return fmt.Errorf("drop superseded host_uuid index on mdm_windows_enrollments: %w", err)
		}
	}

	return nil
}

func Down_20260803135530(tx *sql.Tx) error {
	return nil
}
