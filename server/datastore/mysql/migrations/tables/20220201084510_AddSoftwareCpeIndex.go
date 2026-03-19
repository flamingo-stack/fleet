package tables

import (
	"database/sql"

	"github.com/pkg/errors"
)

func init() {
	MigrationClient.AddMigration(Up_20220201084510, Down_20220201084510)
}

func Up_20220201084510(tx *sql.Tx) error {
	// Idempotent migration.
	if !indexExistsTx(tx, "software_cpe", "software_cpe_cpe_idx") {
		if _, err := tx.Exec(`CREATE INDEX software_cpe_cpe_idx ON software_cpe(cpe);`); err != nil {
			return errors.Wrap(err, "creating software_cpe index")
		}
	}
	return nil
}

func Down_20220201084510(tx *sql.Tx) error {
	return nil
}
