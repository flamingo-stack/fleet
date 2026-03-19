package tables

import (
	"database/sql"

	"github.com/pkg/errors"
)

func init() {
	MigrationClient.AddMigration(Up_20220915165116, Down_20220915165116)
}

func Up_20220915165116(tx *sql.Tx) error {
	// Idempotent migration.
	if indexExistsTx(tx, "hosts", "hosts_search") {
		if _, err := tx.Exec(`ALTER TABLE hosts DROP INDEX hosts_search`); err != nil {
			return errors.Wrapf(err, "upHostDisplayName: delete index")
		}
	}

	if !indexExistsTx(tx, "hosts", "hosts_search") {
		if _, err := tx.Exec(`CREATE FULLTEXT INDEX hosts_search ON hosts(hostname, uuid, computer_name)`); err != nil {
			return errors.Wrapf(err, "upHostDisplayName: create index")
		}
	}

	if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS host_display_names (
			    host_id int(10) unsigned NOT NULL,
			    display_name varchar(255) NOT NULL,
			    PRIMARY KEY (host_id),
			    KEY (display_name)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
		`); err != nil {
		return errors.Wrapf(err, "upHostDisplayName: new table")
	}

	if _, err := tx.Exec(`
			INSERT IGNORE INTO host_display_names (
				SELECT id host_id, IF(computer_name='', hostname, computer_name) display_name FROM hosts
			)
		`); err != nil {
		return errors.Wrapf(err, "upHostDisplayName: migrate data")
	}

	return nil
}

func Down_20220915165116(tx *sql.Tx) error {
	return nil
}
