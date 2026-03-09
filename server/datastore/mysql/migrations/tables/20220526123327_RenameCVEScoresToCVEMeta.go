package tables

import (
	"database/sql"

	"github.com/pkg/errors"
)

func init() {
	MigrationClient.AddMigration(Up_20220526123327, Down_20220526123327)
}

func Up_20220526123327(tx *sql.Tx) error {
	// Idempotent migration.
	if tableExists(tx, "cve_scores") {
		if _, err := tx.Exec(`
RENAME TABLE
    cve_scores TO cve_meta
`); err != nil {
			return errors.Wrapf(err, "rename table")
		}
	}

	if !columnExists(tx, "cve_meta", "published") {
		if _, err := tx.Exec(`
ALTER TABLE cve_meta
    ADD published TIMESTAMP NULL DEFAULT NULL
`); err != nil {
			return errors.Wrapf(err, "add column")
		}
	}

	return nil
}

func Down_20220526123327(tx *sql.Tx) error {
	return nil
}
