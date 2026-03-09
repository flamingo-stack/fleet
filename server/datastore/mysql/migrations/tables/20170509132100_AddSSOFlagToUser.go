package tables

import (
	"database/sql"
)

func init() {
	MigrationClient.AddMigration(Up_20170509132100, Down_20170509132100)
}

func Up_20170509132100(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "users", "sso_enabled") {
		if _, err := tx.Exec("ALTER TABLE `users` ADD COLUMN `sso_enabled` TINYINT NOT NULL DEFAULT FALSE AFTER `position`;"); err != nil {
			return err
		}
	}
	return nil
}

func Down_20170509132100(tx *sql.Tx) error {
	_, err := tx.Exec("ALTER TABLE `users` DROP COLUMN `sso_enabled` ;")
	return err
}
