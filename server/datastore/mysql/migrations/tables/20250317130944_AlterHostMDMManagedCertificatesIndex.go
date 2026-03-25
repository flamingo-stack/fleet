package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20250317130944, Down_20250317130944)
}

func Up_20250317130944(tx *sql.Tx) error {
	// Idempotent migration.
	// Check if the primary key already includes ca_name (i.e., migration already applied).
	if columnExists(tx, "host_mdm_managed_certificates", "ca_name") {
		var count int
		err := tx.QueryRow(`
			SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS
			WHERE TABLE_SCHEMA = DATABASE()
			AND TABLE_NAME = 'host_mdm_managed_certificates'
			AND INDEX_NAME = 'PRIMARY'
			AND COLUMN_NAME = 'ca_name'
		`).Scan(&count)
		if err == nil && count > 0 {
			// Primary key already includes ca_name, skip.
			return nil
		}
	}

	_, err := tx.Exec(`
	ALTER TABLE host_mdm_managed_certificates
	DROP PRIMARY KEY,
	ADD PRIMARY KEY (host_uuid, profile_uuid, ca_name)
	`)
	if err != nil {
		return fmt.Errorf("failed to update primary key in host_mdm_managed_certificates table: %s", err)
	}
	return nil
}

func Down_20250317130944(_ *sql.Tx) error {
	return nil
}
