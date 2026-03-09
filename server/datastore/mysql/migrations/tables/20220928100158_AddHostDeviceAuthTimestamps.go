package tables

import (
	"database/sql"
)

func init() {
	MigrationClient.AddMigration(Up_20220928100158, Down_20220928100158)
}

func Up_20220928100158(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnsExists(tx, "host_device_auth", "created_at", "updated_at") {
		logger.Info.Println("Adding timestamps to 'host_device_auth'...")
		_, err := tx.Exec(`
		ALTER TABLE host_device_auth
			ADD COLUMN created_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
			ADD COLUMN updated_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
`)
		if err != nil {
			return err
		}
		logger.Info.Println("Done adding timestamps to 'host_device_auth'...")
	}
	return nil
}

func Down_20220928100158(tx *sql.Tx) error {
	return nil
}
