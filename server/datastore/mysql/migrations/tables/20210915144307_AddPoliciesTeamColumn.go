package tables

import (
	"database/sql"

	"github.com/pkg/errors"
)

func init() {
	MigrationClient.AddMigration(Up_20210915144307, Down_20210915144307)
}

func Up_20210915144307(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "policies", "team_id") {
		if _, err := tx.Exec(`ALTER TABLE policies
		ADD COLUMN team_id INT UNSIGNED
	`); err != nil {
			return errors.Wrap(err, "add column team_id")
		}
	}

	if !fkExists(tx, "policies", "fk_policies_team_id") {
		if _, err := tx.Exec(`ALTER TABLE policies
		ADD FOREIGN KEY fk_policies_team_id (team_id) REFERENCES teams (id) ON DELETE CASCADE ON UPDATE CASCADE
	`); err != nil {
			return errors.Wrap(err, "add fk_policies_team_id")
		}
	}
	return nil
}

func Down_20210915144307(tx *sql.Tx) error {
	return nil
}
