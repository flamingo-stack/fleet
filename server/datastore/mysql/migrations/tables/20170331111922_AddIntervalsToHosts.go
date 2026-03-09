package tables

import "database/sql"

func init() {
	MigrationClient.AddMigration(Up_20170331111922, Down_20170331111922)
}

func Up_20170331111922(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnsExists(tx, "hosts", "distributed_interval", "logger_tls_period", "config_tls_refresh") {
		if _, err := tx.Exec(
			"ALTER TABLE `hosts` " +
				"ADD COLUMN `distributed_interval` int DEFAULT 0, " +
				"ADD COLUMN `logger_tls_period` int DEFAULT 0, " +
				"ADD COLUMN `config_tls_refresh` int DEFAULT 0;",
		); err != nil {
			return err
		}
	}
	return nil
}

func Down_20170331111922(tx *sql.Tx) error {
	_, err := tx.Exec(
		"ALTER TABLE `hosts` " +
			"DROP COLUMN `distributed_interval`, " +
			"DROP COLUMN `logger_tls_period`, " +
			"DROP COLUMN `config_tls_refresh`;",
	)
	return err
}
