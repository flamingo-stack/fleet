package tables

import (
	"database/sql"
)

func init() {
	MigrationClient.AddMigration(Up20191220130734, Down20191220130734)
}

func Up20191220130734(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "app_configs", "live_query_disabled") {
		if _, err := tx.Exec(
			"ALTER TABLE `app_configs` " +
				"ADD COLUMN `live_query_disabled` TINYINT(1) NOT NULL DEFAULT FALSE;",
		); err != nil {
			return err
		}
	}
	return nil
}

func Down20191220130734(tx *sql.Tx) error {
	_, err := tx.Exec(
		"ALTER TABLE `app_configs` " +
			"DROP COLUMN `live_query_disabled`;",
	)
	return err
}
