package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20250513162912, Down_20250513162912)
}

func Up_20250513162912(tx *sql.Tx) error {
	// Idempotent migration.
	// Add an index to the token field. This is needed for finding identical declarations.
	if !indexExistsTx(tx, "host_mdm_apple_declarations", "idx_token") {
		if _, err := tx.Exec(`ALTER TABLE host_mdm_apple_declarations ADD INDEX idx_token (token)`); err != nil {
			return fmt.Errorf("failed to add index to token field in host_mdm_apple_declarations table: %w", err)
		}
	}

	// Add a resync column to force a DeclarativeManagement resync in some corner cases.
	if !columnExists(tx, "host_mdm_apple_declarations", "resync") {
		if _, err := tx.Exec(`ALTER TABLE host_mdm_apple_declarations ADD COLUMN resync TINYINT(1) NOT NULL DEFAULT '0'`); err != nil {
			return fmt.Errorf("failed to add resync column to host_mdm_apple_declarations table: %w", err)
		}
	}

	// Delete any rows with "remove" operations whose status is either "verifying" or "verified".
	// Remove operations can only have nil and "pending" status.
	// We want to clean up any profiles that got into a bad state due to https://github.com/fleetdm/fleet/issues/27979
	if _, err := tx.Exec(`DELETE FROM host_mdm_apple_declarations
		WHERE operation_type = 'remove' AND status IN ('verifying', 'verified');`); err != nil {
		return fmt.Errorf("failed to delete remove operations with verifying or verified status: %w", err)
	}

	return nil
}

func Down_20250513162912(_ *sql.Tx) error {
	return nil
}
