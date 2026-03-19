package tables

import (
	"database/sql"

	"github.com/pkg/errors"
)

func init() {
	MigrationClient.AddMigration(Up_20210920155130, Down_20210920155130)
}

func Up_20210920155130(tx *sql.Tx) error {
	// Idempotent migration.
	if fkExists(tx, "label_membership", "fk_lm_host_id") {
		if _, err := tx.Exec(
			`ALTER TABLE label_membership DROP FOREIGN KEY fk_lm_host_id`); err != nil {
			return errors.Wrap(err, "dropping foreign key fk_lm_host_id for label_membership")
		}
	}
	if fkExists(tx, "label_membership", "fk_lm_label_id") {
		if _, err := tx.Exec(
			`ALTER TABLE label_membership DROP FOREIGN KEY fk_lm_label_id`); err != nil {
			return errors.Wrap(err, "dropping foreign key fk_lm_label_id for label_membership")
		}
	}
	return nil
}

func Down_20210920155130(tx *sql.Tx) error {
	return nil
}
