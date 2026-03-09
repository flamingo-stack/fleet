package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20240726100517, Down_20240726100517)
}

func Up_20240726100517(tx *sql.Tx) error {
	// Idempotent migration.
	if columnExists(tx, "nano_commands", "created_at") {
		if _, err := tx.Exec(`ALTER TABLE nano_commands
		MODIFY COLUMN created_at TIMESTAMP(6) NULL DEFAULT CURRENT_TIMESTAMP(6),
		MODIFY COLUMN updated_at TIMESTAMP(6) NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)`); err != nil {
			return fmt.Errorf("failed to modify columns created_at, updated_at in nano_commands table: %w", err)
		}
	}
	if columnExists(tx, "nano_enrollment_queue", "created_at") {
		if _, err := tx.Exec(`ALTER TABLE nano_enrollment_queue
		MODIFY COLUMN created_at TIMESTAMP(6) NULL DEFAULT CURRENT_TIMESTAMP(6),
		MODIFY COLUMN updated_at TIMESTAMP(6) NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)`); err != nil {
			return fmt.Errorf("failed to modify columns created_at, updated_at in nano_enrollment_queue table: %w", err)
		}
	}
	return nil
}

func Down_20240726100517(_ *sql.Tx) error {
	return nil
}
