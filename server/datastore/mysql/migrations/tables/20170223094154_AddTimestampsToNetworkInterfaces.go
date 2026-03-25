package tables

import "database/sql"

func init() {
	MigrationClient.AddMigration(Up_20170223094154, Down_20170223094154)
}

func Up_20170223094154(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnsExists(tx, "network_interfaces", "created_at", "updated_at") {
		if _, err := tx.Exec(
			"ALTER TABLE `network_interfaces` " +
				"ADD COLUMN `created_at` timestamp DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP, " +
				"ADD COLUMN `updated_at` timestamp DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP",
		); err != nil {
			return err
		}
	}
	return nil
}

func Down_20170223094154(tx *sql.Tx) error {
	_, err := tx.Exec(
		"ALTER TABLE `network_interfaces` " +
			"DROP COLUMN `created_at`, " +
			"DROP COLUMN `updated_at`",
	)
	return err
}
