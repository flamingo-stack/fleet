package tables

import (
	"database/sql"

	"github.com/pkg/errors"
)

func init() {
	MigrationClient.AddMigration(Up_20221205112142, Down_20221205112142)
}

func Up_20221205112142(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "carve_metadata", "error") {
		if _, err := tx.Exec("ALTER TABLE `carve_metadata` ADD COLUMN `error` TEXT"); err != nil {
			return errors.Wrap(err, "adding error column to carve_metadata")
		}
	}
	return nil
}

func Down_20221205112142(tx *sql.Tx) error {
	return nil
}
