package tables

import (
	"database/sql"
)

func init() {
	MigrationClient.AddMigration(Up_20250815130115, Down_20250815130115)
}

func Up_20250815130115(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnsExists(tx, "host_dep_assignments", "mdm_migration_deadline", "mdm_migration_completed") {
		stmt := `ALTER TABLE host_dep_assignments
	ADD COLUMN mdm_migration_deadline TIMESTAMP(6) DEFAULT NULL,
	ADD COLUMN mdm_migration_completed TIMESTAMP(6) DEFAULT NULL`
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func Down_20250815130115(tx *sql.Tx) error {
	return nil
}
