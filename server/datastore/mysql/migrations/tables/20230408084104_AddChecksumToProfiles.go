package tables

import (
	"database/sql"

	"github.com/pkg/errors"
)

func init() {
	MigrationClient.AddMigration(Up_20230408084104, Down_20230408084104)
}

func Up_20230408084104(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "mdm_apple_configuration_profiles", "checksum") {
		if _, err := tx.Exec(
			`ALTER TABLE mdm_apple_configuration_profiles ADD COLUMN checksum BINARY(16) NOT NULL`); err != nil {
			return errors.Wrap(err, "add checksum column to mdm_apple_configuration_profiles")
		}
	}
	if !columnExists(tx, "host_mdm_apple_profiles", "checksum") {
		if _, err := tx.Exec(
			`ALTER TABLE host_mdm_apple_profiles ADD COLUMN checksum BINARY(16) NOT NULL`); err != nil {
			return errors.Wrap(err, "add checksum column to host_mdm_apple_profiles")
		}
	}
	if _, err := tx.Exec(
		`UPDATE mdm_apple_configuration_profiles SET checksum = UNHEX(MD5(mobileconfig))`); err != nil {
		return errors.Wrap(err, "update checksum in mdm_apple_configuration_profiles")
	}
	if _, err := tx.Exec(
		`UPDATE host_mdm_apple_profiles hmap SET checksum = (SELECT checksum FROM mdm_apple_configuration_profiles macp WHERE macp.profile_id = hmap.profile_id)`); err != nil {
		return errors.Wrap(err, "update checksum in host_mdm_apple_profiles")
	}
	return nil
}

func Down_20230408084104(tx *sql.Tx) error {
	return nil
}
