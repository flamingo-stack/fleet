package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20231122101320, Down_20231122101320)
}

func Up_20231122101320(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnsExists(tx, "software", "browser", "extension_id") {
		stmt := `
		ALTER TABLE software
		ADD COLUMN browser varchar(255) NOT NULL DEFAULT '',
		ADD COLUMN extension_id varchar(255) NOT NULL DEFAULT '';
	`
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("add browser and extension_id to software: %w", err)
		}
	}

	// We cannot add `extension_id` to the unq_name constraint because it is already at its 3072 byte limit, so we will drop it.
	// There appears to be no critical reason for this constraint except to help development.
	if indexExistsTx(tx, "software", "unq_name") {
		stmt := `
		ALTER TABLE software
		DROP INDEX unq_name;
	`
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("drop unq_name key from software: %w", err)
		}
	}

	return nil
}

func Down_20231122101320(tx *sql.Tx) error {
	/*
		ALTER TABLE software DROP COLUMN extension_id;
		ALTER TABLE software DROP COLUMN browser;
		ALTER TABLE software ADD UNIQUE KEY `unq_name` (`name`,`version`,`source`,`release`,`vendor`,`arch`);
	*/
	return nil
}
