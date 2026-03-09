package tables

import (
	"database/sql"

	"github.com/pkg/errors"
)

func init() {
	MigrationClient.AddMigration(Up_20221223174807, Down_20221223174807)
}

func Up_20221223174807(tx *sql.Tx) error {
	// Idempotent migration.
	if columnExists(tx, "hosts", "osquery_host_id") {
		if _, err := tx.Exec(`
		ALTER TABLE hosts MODIFY osquery_host_id VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NULL
	`); err != nil {
			return errors.Wrapf(err, "altering hosts table: modify osquery_host_id")
		}
	}

	if !indexExistsTx(tx, "hosts", "idx_hosts_hardware_serial") {
		if _, err := tx.Exec(`
		ALTER TABLE hosts ADD INDEX idx_hosts_hardware_serial (hardware_serial)
	`); err != nil {
			return errors.Wrapf(err, "altering hosts table: add index")
		}
	}

	return nil
}

func Down_20221223174807(tx *sql.Tx) error {
	return nil
}
