package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20241004005000, Down_20241004005000)
}

func Up_20241004005000(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "host_script_results", "policy_id") {
		if _, err := tx.Exec(`
		ALTER TABLE host_script_results
		ADD COLUMN policy_id INT UNSIGNED DEFAULT NULL
	`); err != nil {
			return fmt.Errorf("failed to add policy_id to host script results: %w", err)
		}
	}

	if !fkExists(tx, "host_script_results", "fk_script_result_policy_id") {
		if _, err := tx.Exec(`
		ALTER TABLE host_script_results
		ADD FOREIGN KEY fk_script_result_policy_id (policy_id) REFERENCES policies (id) ON DELETE SET NULL
	`); err != nil {
			return fmt.Errorf("failed to add FK fk_script_result_policy_id: %w", err)
		}
	}

	return nil
}

func Down_20241004005000(tx *sql.Tx) error {
	return nil
}
