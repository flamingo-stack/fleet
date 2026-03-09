package tables

import (
	"database/sql"
)

func init() {
	MigrationClient.AddMigration(Up_20251215163721, Down_20251215163721)
}

func Up_20251215163721(tx *sql.Tx) error {
	// Idempotent migration.
	if !indexExistsTx(tx, "hosts", "idx_hosts_hostname") {
		if _, err := tx.Exec(`
	ALTER TABLE hosts ADD INDEX idx_hosts_hostname (hostname)
	`); err != nil {
			return err
		}
	}
	return nil
}

func Down_20251215163721(tx *sql.Tx) error {
	return nil
}
