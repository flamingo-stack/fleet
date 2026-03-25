package tables

import "database/sql"

func init() {
	MigrationClient.AddMigration(Up_20170119234632, Down_20170119234632)
}

func Up_20170119234632(tx *sql.Tx) error {
	// Idempotent migration.
	if columnExists(tx, "queries", "description") {
		if _, err := tx.Exec(
			"ALTER TABLE `queries` MODIFY `description` TEXT NOT NULL;",
		); err != nil {
			return err
		}
	}
	return nil
}

func Down_20170119234632(tx *sql.Tx) error {
	_, err := tx.Exec(
		"ALTER TABLE `queries` MODIFY `description` VARCHAR(255) NOT NULL;",
	)
	return err
}
