package tables

import (
	"database/sql"

	"github.com/pkg/errors"
)

func init() {
	MigrationClient.AddMigration(Up_20210617174723, Down_20210617174723)
}

func Up_20210617174723(tx *sql.Tx) error {
	// Idempotent migration.
	sql := `
		DELETE FROM pack_targets
		WHERE pack_id NOT IN (SELECT id FROM packs)
	`
	if _, err := tx.Exec(sql); err != nil {
		return errors.Wrap(err, "delete orphaned pack targets")
	}

	if !fkExists(tx, "pack_targets", "pack_targets_ibfk_1") {
		sql = `
		ALTER TABLE pack_targets
		ADD FOREIGN KEY (pack_id) REFERENCES packs (id) ON UPDATE CASCADE ON DELETE CASCADE
	`
		if _, err := tx.Exec(sql); err != nil {
			return errors.Wrap(err, "add foreign key on pack_targets pack_id")
		}
	}

	return nil
}

func Down_20210617174723(tx *sql.Tx) error {
	return nil
}
