package tables

import (
	"database/sql"

	"github.com/pkg/errors"
)

func init() {
	MigrationClient.AddMigration(Up_20220316155700, Down_20220316155700)
}

func Up_20220316155700(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "hosts", "public_ip") {
		if _, err := tx.Exec(
			"ALTER TABLE `hosts` ADD COLUMN `public_ip` varchar(45) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT ''",
		); err != nil {
			return errors.Wrap(err, "add public_ip column")
		}
	}

	return nil
}

func Down_20220316155700(tx *sql.Tx) error {
	return nil
}
