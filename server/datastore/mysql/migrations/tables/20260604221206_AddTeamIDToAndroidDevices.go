package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20260604221206, Down_20260604221206)
}

func Up_20260604221206(tx *sql.Tx) error {
	// Idempotent migration.
	// Add team_id to android_devices so the last-known team assignment survives
	// host record deletion.
	if !columnExists(tx, "android_devices", "team_id") {
		if _, err := tx.Exec(`ALTER TABLE android_devices
		ADD COLUMN team_id INT UNSIGNED DEFAULT NULL,
		ADD CONSTRAINT fk_android_devices_team_id FOREIGN KEY (team_id) REFERENCES teams (id) ON DELETE SET NULL`); err != nil {
			return fmt.Errorf("add team_id to android_devices: %w", err)
		}
	} else if !constraintExists(tx, "android_devices", "fk_android_devices_team_id") {
		if _, err := tx.Exec(`ALTER TABLE android_devices
		ADD CONSTRAINT fk_android_devices_team_id FOREIGN KEY (team_id) REFERENCES teams (id) ON DELETE SET NULL`); err != nil {
			return fmt.Errorf("add fk_android_devices_team_id to android_devices: %w", err)
		}
	}

	if _, err := tx.Exec(`UPDATE android_devices ad
		JOIN hosts h ON ad.host_id = h.id
		SET ad.team_id = h.team_id`); err != nil {
		return fmt.Errorf("backfill android_devices.team_id: %w", err)
	}

	return nil
}

func Down_20260604221206(tx *sql.Tx) error {
	return nil
}
