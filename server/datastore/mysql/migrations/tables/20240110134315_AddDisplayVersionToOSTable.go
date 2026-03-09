package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20240110134315, Down_20240110134315)
}

func Up_20240110134315(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "operating_systems", "display_version") {
		addColumnStmt := `
		ALTER TABLE operating_systems
		ADD COLUMN display_version VARCHAR(10) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '';
	`
		if _, err := tx.Exec(addColumnStmt); err != nil {
			return fmt.Errorf("adding operating_systems column: %w", err)
		}

		if indexExistsTx(tx, "operating_systems", "idx_unique_os") {
			if _, err := tx.Exec(`ALTER TABLE operating_systems DROP INDEX idx_unique_os`); err != nil {
				return fmt.Errorf("dropping operating_systems index: %w", err)
			}
		}

		if _, err := tx.Exec(`
		ALTER TABLE operating_systems
		ADD UNIQUE INDEX idx_unique_os (name, version, arch, kernel_version, platform, display_version)`); err != nil {
			return fmt.Errorf("adding operating_systems index: %w", err)
		}
	}

	return nil
}

func Down_20240110134315(tx *sql.Tx) error {
	return nil
}
