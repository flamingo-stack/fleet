package tables

import (
	"database/sql"
)

func init() {
	MigrationClient.AddMigration(Up20210119174155, Down20210119174155)
}

func Up20210119174155(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "scheduled_queries", "denylist") {
		if _, err := tx.Exec(`
		ALTER TABLE scheduled_queries
		ADD COLUMN denylist TINYINT(1) DEFAULT NULL
	`); err != nil {
			return err
		}
	}
	return nil
}

func Down20210119174155(tx *sql.Tx) error {
	_, err := tx.Exec(`
		ALTER TABLE scheduled_queries
		DROP COLUMN denylist
	`)
	return err
}
