package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20230518114155, Down_20230518114155)
}

func Up_20230518114155(tx *sql.Tx) error {
	// Idempotent migration.
	if columnExists(tx, "mdm_idp_accounts", "salt") {
		if _, err := tx.Exec(`ALTER TABLE mdm_idp_accounts DROP COLUMN salt`); err != nil {
			return fmt.Errorf("alter mdm_idp_accounts table: drop salt: %w", err)
		}
	}
	if columnExists(tx, "mdm_idp_accounts", "entropy") {
		if _, err := tx.Exec(`ALTER TABLE mdm_idp_accounts DROP COLUMN entropy`); err != nil {
			return fmt.Errorf("alter mdm_idp_accounts table: drop entropy: %w", err)
		}
	}
	if columnExists(tx, "mdm_idp_accounts", "iterations") {
		if _, err := tx.Exec(`ALTER TABLE mdm_idp_accounts DROP COLUMN iterations`); err != nil {
			return fmt.Errorf("alter mdm_idp_accounts table: drop iterations: %w", err)
		}
	}
	if !columnExists(tx, "mdm_idp_accounts", "fullname") {
		if _, err := tx.Exec(`ALTER TABLE mdm_idp_accounts ADD COLUMN fullname varchar(256) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("alter mdm_idp_accounts table: add fullname: %w", err)
		}
	}
	return nil
}

func Down_20230518114155(tx *sql.Tx) error {
	return nil
}
