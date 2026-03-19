package tables

import "database/sql"

func init() {
	MigrationClient.AddMigration(Up_20170111133013, Down_20170111133013)
}

func Up_20170111133013(tx *sql.Tx) error {
	// Idempotent migration.
	if !constraintExists(tx, "queries", "constraint_query_name_unique") {
		if _, err := tx.Exec(
			"ALTER TABLE `queries` " +
				"ADD CONSTRAINT `constraint_query_name_unique` " +
				"UNIQUE (`name`);",
		); err != nil {
			return err
		}
	}
	return nil
}

func Down_20170111133013(tx *sql.Tx) error {
	_, err := tx.Exec(
		"ALTER TABLE `queries` " +
			"DROP INDEX `constraint_query_name_unique`;",
	)
	return err
}
