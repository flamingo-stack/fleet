package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20251107170854, Down_20251107170854)
}

func Up_20251107170854(tx *sql.Tx) error {
	// Idempotent migration.
	// CHAR(38) to account for 32 hex chars + 4 hyphens + open/close curly braces
	if !columnExists(tx, "software_titles", "upgrade_code") {
		if _, err := tx.Exec(`ALTER TABLE software_titles ADD COLUMN upgrade_code CHAR(38) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL`); err != nil {
			return fmt.Errorf("failed to add software_titles.upgrade_code column: %w", err)
		}
	}
	if _, err := tx.Exec(`UPDATE software_titles SET upgrade_code = '' WHERE source = 'programs'`); err != nil {
		return fmt.Errorf("failed to add default empty string value to software_titles.upgrade_code column for rows where source = 'programs': %w", err)
	}

	if !columnExists(tx, "software", "upgrade_code") {
		if _, err := tx.Exec(`ALTER TABLE software ADD COLUMN upgrade_code CHAR(38) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL`); err != nil {
			return fmt.Errorf("failed to add software.upgrade_code column: %w", err)
		}
	}

	if _, err := tx.Exec(`UPDATE software SET upgrade_code = '' WHERE source = 'programs'`); err != nil {
		return fmt.Errorf("failed to add default empty string value to software.upgrade_code column for rows where source = 'programs': %w", err)
	}

	// NULLIF(upgrade_code, "") prevents upgrade_code being used as the unique_identifier when it is
	// the empty string, which will be the case for "programs"-sourced software but is obviously not unique
	if columnExists(tx, "software_titles", "unique_identifier") {
		if _, err := tx.Exec(`ALTER TABLE software_titles MODIFY COLUMN unique_identifier VARCHAR(255) GENERATED ALWAYS AS (COALESCE(bundle_identifier, application_id, NULLIF(upgrade_code, ""), name)) VIRTUAL`); err != nil {
			return fmt.Errorf("failed to alter definition of software_titles.unique_identifier column to include upgrade_code in its COALESCE: %w", err)
		}
	}

	return nil
}

func Down_20251107170854(tx *sql.Tx) error {
	return nil
}
