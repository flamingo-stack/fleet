package tables

import (
	"database/sql"
)

func init() {
	MigrationClient.AddMigration(Up_20231009094543, Down_20231009094543)
}

func Up_20231009094543(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "queries", "discard_data") {
		if _, err := tx.Exec(`ALTER TABLE queries ADD COLUMN discard_data TINYINT(1) NOT NULL DEFAULT TRUE;`); err != nil {
			return err
		}
	}
	return nil
}

func Down_20231009094543(tx *sql.Tx) error {
	return nil
}
