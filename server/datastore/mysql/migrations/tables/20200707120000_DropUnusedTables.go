package tables

import (
	"database/sql"

	"github.com/pkg/errors"
)

func init() {
	MigrationClient.AddMigration(Up_20200707120000, Down_20200707120000)
}

func Up_20200707120000(tx *sql.Tx) error {
	// Idempotent migration.
	_, err := tx.Exec("DROP TABLE IF EXISTS `decorators`")
	if err != nil {
		return errors.Wrap(err, "drop decorators table")
	}

	_, err = tx.Exec("DROP TABLE IF EXISTS `yara_file_paths`")
	if err != nil {
		return errors.Wrap(err, "drop yara_file_paths table")
	}

	_, err = tx.Exec("DROP TABLE IF EXISTS `yara_signature_paths`")
	if err != nil {
		return errors.Wrap(err, "drop yara_signature_paths table")
	}

	_, err = tx.Exec("DROP TABLE IF EXISTS `yara_signatures`")
	if err != nil {
		return errors.Wrap(err, "drop yara_signatures table")
	}

	_, err = tx.Exec("DROP TABLE IF EXISTS `file_integrity_monitoring_files`")
	if err != nil {
		return errors.Wrap(err, "drop file_integrity_monitoring_files table")
	}

	_, err = tx.Exec("DROP TABLE IF EXISTS `file_integrity_monitorings`")
	if err != nil {
		return errors.Wrap(err, "drop file_integrity_monitorings table")
	}

	_, err = tx.Exec("DROP TABLE IF EXISTS `options`")
	if err != nil {
		return errors.Wrap(err, "drop file_integrity_monitorings table")
	}

	return nil
}

func Down_20200707120000(tx *sql.Tx) error {
	return nil
}
