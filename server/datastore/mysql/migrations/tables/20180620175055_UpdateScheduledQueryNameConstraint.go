package tables

import (
	"database/sql"

	"github.com/pkg/errors"
)

func init() {
	MigrationClient.AddMigration(Up20180620175055, Down20180620175055)
}

func Up20180620175055(tx *sql.Tx) error {
	// Idempotent migration.
	// Update constraint by removing the old constraint and replacing it
	if fkExists(tx, "scheduled_queries", "scheduled_queries_query_name") {
		sql := `ALTER TABLE scheduled_queries DROP FOREIGN KEY scheduled_queries_query_name`
		if _, err := tx.Exec(sql); err != nil {
			return errors.Wrap(err, "drop old constraint")
		}
	}

	if !fkExists(tx, "scheduled_queries", "scheduled_queries_query_name") {
		sql := `
		ALTER TABLE scheduled_queries
		ADD CONSTRAINT scheduled_queries_query_name
		FOREIGN KEY (query_name) REFERENCES queries (name)
		ON DELETE CASCADE ON UPDATE CASCADE
	`
		if _, err := tx.Exec(sql); err != nil {
			return errors.Wrap(err, "add new constraint")
		}
	}

	return nil
}

func Down20180620175055(tx *sql.Tx) error {
	return nil
}
