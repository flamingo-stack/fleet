package tables

import "database/sql"

func init() {
	MigrationClient.AddMigration(Up_20170110202752, Down_20170110202752)
}

func Up_20170110202752(tx *sql.Tx) error {
	// Idempotent migration.
	if columnExists(tx, "app_configs", "smtp_enabled") {
		if _, err := tx.Exec(
			"ALTER TABLE `app_configs` " +
				"DROP COLUMN `smtp_enabled`;",
		); err != nil {
			return err
		}
	}
	return nil
}

func Down_20170110202752(tx *sql.Tx) error {
	_, err := tx.Exec(
		"ALTER TABLE `app_configs` " +
			"ADD COLUMN `smtp_enabled` TINYINT(1) NOT NULL DEFAULT FALSE;",
	)
	return err
}
