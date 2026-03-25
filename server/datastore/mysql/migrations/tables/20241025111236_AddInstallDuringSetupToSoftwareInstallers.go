package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20241025111236, Down_20241025111236)
}

func Up_20241025111236(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "software_installers", "install_during_setup") {
		_, err := tx.Exec(`ALTER TABLE software_installers ADD COLUMN install_during_setup BOOL NOT NULL DEFAULT false`)
		if err != nil {
			return fmt.Errorf("failed to add install_during_setup to software_installers: %w", err)
		}
	}

	if !columnExists(tx, "vpp_apps_teams", "install_during_setup") {
		_, err := tx.Exec(`ALTER TABLE vpp_apps_teams ADD COLUMN install_during_setup BOOL NOT NULL DEFAULT false`)
		if err != nil {
			return fmt.Errorf("failed to add install_during_setup to vpp_apps_teams: %w", err)
		}
	}

	return nil
}

func Down_20241025111236(tx *sql.Tx) error {
	return nil
}
