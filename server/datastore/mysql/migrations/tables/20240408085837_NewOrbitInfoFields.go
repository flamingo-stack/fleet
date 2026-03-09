package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20240408085837, Down_20240408085837)
}

func Up_20240408085837(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnsExists(tx, "host_orbit_info", "desktop_version", "scripts_enabled") {
		if _, err := tx.Exec(
			`ALTER TABLE host_orbit_info ADD COLUMN (
		desktop_version VARCHAR(50) DEFAULT NULL,
		scripts_enabled TINYINT(1) DEFAULT NULL
	)`,
		); err != nil {
			return fmt.Errorf("failed to add desktop_version and scripts_enabled to host_orbit_info: %w", err)
		}
	}
	return nil
}

func Down_20240408085837(*sql.Tx) error {
	return nil
}
