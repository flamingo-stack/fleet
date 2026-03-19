package tables

import (
	"database/sql"
	"github.com/pkg/errors"
)

func init() {
	MigrationClient.AddMigration(Up_20221216115820, Down_20221216115820)
}

func Up_20221216115820(tx *sql.Tx) error {
	// Idempotent migration.
	if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS policy_automation_iterations (
				policy_id INT UNSIGNED NOT NULL PRIMARY KEY,
				iteration INT NOT NULL,
				FOREIGN KEY (policy_id) REFERENCES policies(id) ON DELETE CASCADE
			);
		`); err != nil {
		return errors.Wrap(err, "create table")
	}

	if !columnExists(tx, "policy_membership", "automation_iteration") {
		if _, err := tx.Exec(`
			ALTER TABLE policy_membership ADD COLUMN automation_iteration INT NULL;
		`); err != nil {
			return errors.Wrap(err, "alter table")
		}
	}
	return nil
}

func Down_20221216115820(tx *sql.Tx) error {
	return nil
}
