package tables

import (
	"database/sql"

	"github.com/pkg/errors"
)

func init() {
	MigrationClient.AddMigration(Up_20230503101418, Down_20230503101418)
}

func Up_20230503101418(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "jobs", "not_before") {
		if _, err := tx.Exec(`
ALTER TABLE jobs ADD COLUMN not_before TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP;
`); err != nil {
			return errors.Wrap(err, "add not_before")
		}
	}
	return nil
}

func Down_20230503101418(tx *sql.Tx) error {
	return nil
}
