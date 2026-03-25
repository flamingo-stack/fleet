package tables

import (
	"database/sql"

	"github.com/pkg/errors"
)

func init() {
	MigrationClient.AddMigration(Up_20221130114928, Down_20221130114928)
}

func Up_20221130114928(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "policies", "critical") {
		if _, err := tx.Exec(`
		ALTER TABLE policies ADD COLUMN critical TINYINT(1) NOT NULL DEFAULT FALSE;
	`); err != nil {
			return errors.Wrapf(err, "adding column critical")
		}
	}

	return nil
}

func Down_20221130114928(*sql.Tx) error {
	return nil
}
