package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20250923120000, Down_20250923120000)
}

func Up_20250923120000(tx *sql.Tx) error {
	// Idempotent migration.
	if columnExists(tx, "host_mdm_managed_certificates", "type") {
		if _, err := tx.Exec(`
ALTER TABLE host_mdm_managed_certificates
MODIFY COLUMN type ENUM('digicert','custom_scep_proxy','ndes','smallstep') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT 'ndes';`); err != nil {
			return fmt.Errorf("failed to modify host_mdm_managed_certificates table: %w", err)
		}
	}

	if columnExists(tx, "certificate_authorities", "type") {
		if _, err := tx.Exec(`
ALTER TABLE certificate_authorities
MODIFY COLUMN type ENUM('digicert', 'ndes_scep_proxy', 'custom_scep_proxy', 'hydrant', 'smallstep') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL`); err != nil {
			return fmt.Errorf("failed to modify certificate_authorities type column: %w", err)
		}
	}

	if !columnExists(tx, "certificate_authorities", "challenge_url") {
		// Smallstep fields
		// Note Smallstep also shares username and password fields with NDES
		if _, err := tx.Exec(`
ALTER TABLE certificate_authorities
ADD COLUMN challenge_url TEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NULL AFTER password_encrypted`); err != nil {
			return fmt.Errorf("failed to add challenge_url column to certificate_authorities table: %w", err)
		}
	}
	return nil
}

func Down_20250923120000(tx *sql.Tx) error {
	return nil
}
