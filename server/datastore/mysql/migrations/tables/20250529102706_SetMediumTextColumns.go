package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20250529102706, Down_20250529102706)
}

func Up_20250529102706(tx *sql.Tx) error {
	// Idempotent migration.
	if columnsExists(tx, "queries", "query", "description") {
		if _, err := tx.Exec(
			`ALTER TABLE queries
		MODIFY query MEDIUMTEXT NOT NULL,
		MODIFY description MEDIUMTEXT NOT NULL;
`,
		); err != nil {
			return fmt.Errorf("failed to set queries.query and queries.description to mediumtext: %w", err)
		}
	}
	if columnExists(tx, "labels", "query") {
		if _, err := tx.Exec(
			`ALTER TABLE labels MODIFY query MEDIUMTEXT NOT NULL;`,
		); err != nil {
			return fmt.Errorf("failed to set labels.query to mediumtext: %w", err)
		}
	}
	return nil
}

func Down_20250529102706(tx *sql.Tx) error {
	return nil
}
