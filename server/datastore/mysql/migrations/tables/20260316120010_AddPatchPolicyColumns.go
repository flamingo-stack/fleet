package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20260316120010, Down_20260316120010)
}

func Up_20260316120010(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "policies", "type") {
		if _, err := tx.Exec(`ALTER TABLE policies ADD COLUMN type ENUM('dynamic', 'patch') NOT NULL DEFAULT 'dynamic'`); err != nil {
			return fmt.Errorf("adding type column to policies table: %w", err)
		}
	}
	if !columnExists(tx, "policies", "patch_software_title_id") {
		if _, err := tx.Exec(`ALTER TABLE policies ADD COLUMN patch_software_title_id INT UNSIGNED DEFAULT NULL`); err != nil {
			return fmt.Errorf("adding patch_software_title_id column to policies table: %w", err)
		}
	}
	if !constraintExists(tx, "policies", "fk_patch_software_title_id") {
		if _, err := tx.Exec(`ALTER TABLE policies ADD CONSTRAINT fk_patch_software_title_id
				FOREIGN KEY (patch_software_title_id) REFERENCES software_titles(id) ON DELETE CASCADE`); err != nil {
			return fmt.Errorf("adding patch_software_title_id foreign key to policies table: %w", err)
		}
	}
	if !indexExistsTx(tx, "policies", "idx_team_id_patch_software_title_id") {
		if _, err := tx.Exec(`ALTER TABLE policies ADD UNIQUE INDEX idx_team_id_patch_software_title_id (team_id, patch_software_title_id)`); err != nil {
			return fmt.Errorf("adding (team_id, patch_software_title_id) unique index to policies table: %w", err)
		}
	}
	return nil
}

func Down_20260316120010(tx *sql.Tx) error {
	return nil
}
