package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20231204155427, Down_20231204155427)
}

func Up_20231204155427(tx *sql.Tx) error {
	// Idempotent migration.
	// update the windows profiles tables to use a 37-char uuid column for
	// the 'w' prefix.
	if columnExists(tx, "host_mdm_windows_profiles", "profile_uuid") {
		if _, err := tx.Exec(`
ALTER TABLE host_mdm_windows_profiles
	CHANGE COLUMN profile_uuid profile_uuid VARCHAR(37) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT ''
`); err != nil {
			return fmt.Errorf("failed to alter host_mdm_windows_profiles table: %w", err)
		}
	}
	if columnExists(tx, "mdm_windows_configuration_profiles", "profile_uuid") {
		if _, err := tx.Exec(`
ALTER TABLE mdm_windows_configuration_profiles
	CHANGE COLUMN profile_uuid profile_uuid VARCHAR(37) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT ''
`); err != nil {
			return fmt.Errorf("failed to alter mdm_windows_configuration_profiles table: %w", err)
		}
	}

	// update the apple profiles table to add the profile_uuid column.
	if !columnExists(tx, "mdm_apple_configuration_profiles", "profile_uuid") {
		if _, err := tx.Exec(`
ALTER TABLE mdm_apple_configuration_profiles
	-- 37 and not 36 because the UUID will be prefixed with 'a' to indicate
	-- that it's an Apple profile.
	ADD COLUMN profile_uuid VARCHAR(37) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT ''
`); err != nil {
			return fmt.Errorf("failed to alter mdm_apple_configuration_profiles table: %w", err)
		}

		// generate the uuids for the apple profiles table
		if _, err := tx.Exec(`
UPDATE
	mdm_apple_configuration_profiles macp
SET
	-- see https://stackoverflow.com/a/51393124/1094941
	profile_uuid = CONCAT('a', CONVERT(uuid() USING utf8mb4)),
	updated_at = macp.updated_at
`); err != nil {
			return fmt.Errorf("failed to update mdm_apple_configuration_profiles table: %w", err)
		}
	}

	// set the profile uuid as the new primary key
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'mdm_apple_configuration_profiles' AND INDEX_NAME = 'PRIMARY' AND COLUMN_NAME = 'profile_uuid'`).Scan(&count); err == nil && count > 0 {
		// already migrated, skip DROP/ADD PRIMARY KEY
	} else if !indexExistsTx(tx, "mdm_apple_configuration_profiles", "idx_mdm_apple_config_prof_id") {
		if _, err := tx.Exec(`
ALTER TABLE mdm_apple_configuration_profiles
	-- auto-increment column must have an index, so we create one before
	-- dropping the primary key.
	ADD UNIQUE KEY idx_mdm_apple_config_prof_id (profile_id),
	DROP PRIMARY KEY,
	ADD PRIMARY KEY (profile_uuid)`); err != nil {
			return fmt.Errorf("failed to set primary key of mdm_apple_configuration_profiles table: %w", err)
		}
	}

	// add the profile_uuid column to the host apple profiles table, keeping the
	// old id for now.
	if !columnExists(tx, "host_mdm_apple_profiles", "profile_uuid") {
		if _, err := tx.Exec(`
ALTER TABLE host_mdm_apple_profiles
	ADD COLUMN profile_uuid VARCHAR(37) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT ''
`); err != nil {
			return fmt.Errorf("failed to alter host_mdm_apple_profiles table: %w", err)
		}

		// update the apple host profiles table's profile_uuid based on its profile_id
		if _, err := tx.Exec(`
UPDATE
	host_mdm_apple_profiles
SET
	profile_uuid = COALESCE((
		SELECT
			macp.profile_uuid
		FROM
			mdm_apple_configuration_profiles macp
		WHERE
			host_mdm_apple_profiles.profile_id = macp.profile_id
	-- see https://stackoverflow.com/a/51393124/1094941
	), CONCAT('a', CONVERT(uuid() USING utf8mb4)))
`); err != nil {
			return fmt.Errorf("failed to update host_mdm_apple_profiles table: %w", err)
		}
	}

	// drop the now unused profile_id column from the host apple profiles table
	var count2 int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'host_mdm_apple_profiles' AND INDEX_NAME = 'PRIMARY' AND COLUMN_NAME = 'profile_uuid'`).Scan(&count2); err == nil && count2 > 0 {
		// already migrated, skip DROP/ADD PRIMARY KEY
	} else if columnExists(tx, "host_mdm_apple_profiles", "profile_id") {
		if _, err := tx.Exec(`ALTER TABLE host_mdm_apple_profiles
		DROP PRIMARY KEY,
		ADD PRIMARY KEY (host_uuid, profile_uuid),
		DROP COLUMN profile_id`); err != nil {
			return fmt.Errorf("failed to drop column from host_mdm_apple_profiles table: %w", err)
		}
	}

	return nil
}

func Down_20231204155427(tx *sql.Tx) error {
	return nil
}
