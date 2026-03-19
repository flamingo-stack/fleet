package tables

import (
	"database/sql"
)

func init() {
	MigrationClient.AddMigration(Up_20241205122800, Down_20241205122800)
}

func Up_20241205122800(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnsExists(tx, "host_mdm_apple_profiles", "created_at", "updated_at") {
		if _, err := tx.Exec(
			"ALTER TABLE host_mdm_apple_profiles " +
				"ADD COLUMN created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), " +
				"ADD COLUMN updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)",
		); err != nil {
			return err
		}
	}
	return nil
}

func Down_20241205122800(_ *sql.Tx) error {
	return nil
}
