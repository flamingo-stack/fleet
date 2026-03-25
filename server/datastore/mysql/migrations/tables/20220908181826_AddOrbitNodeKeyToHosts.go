package tables

import (
	"database/sql"
)

func init() {
	MigrationClient.AddMigration(Up_20220908181826, Down_20220908181826)
}

func Up_20220908181826(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "hosts", "orbit_node_key") {
		if _, err := tx.Exec(`ALTER TABLE hosts ADD COLUMN orbit_node_key VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin DEFAULT NULL;`); err != nil {
			return err
		}
	}
	if !indexExistsTx(tx, "hosts", "idx_host_unique_orbitnodekey") {
		if _, err := tx.Exec(`ALTER TABLE hosts ADD UNIQUE KEY idx_host_unique_orbitnodekey (orbit_node_key);`); err != nil {
			return err
		}
	}
	return nil
}

func Down_20220908181826(tx *sql.Tx) error {
	return nil
}
