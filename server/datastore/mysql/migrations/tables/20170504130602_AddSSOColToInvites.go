package tables

import (
	"database/sql"
)

func init() {
	MigrationClient.AddMigration(Up_20170504130602, Down_20170504130602)
}

func Up_20170504130602(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "invites", "sso_enabled") {
		if _, err := tx.Exec("ALTER TABLE `invites` ADD COLUMN `sso_enabled` TINYINT(1) NOT NULL DEFAULT FALSE AFTER `token`;"); err != nil {
			return err
		}
	}
	return nil
}

func Down_20170504130602(tx *sql.Tx) error {
	_, err := tx.Exec("ALTER TABLE `invites` DROP COLUMN `sso_enabled`;")
	return err
}
