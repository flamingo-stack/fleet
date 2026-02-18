package tables

import (
	"database/sql"

	"github.com/pkg/errors"
)

func init() {
	MigrationClient.AddMigration(Up_20260217000000, Down_20260217000000)
}

func Up_20260217000000(tx *sql.Tx) error {
	if !columnExists(tx, "labels", "hidden") {
		if _, err := tx.Exec(`ALTER TABLE labels ADD COLUMN hidden TINYINT(1) NOT NULL DEFAULT 0`); err != nil {
			return errors.Wrap(err, "add hidden column to labels")
		}
	}

	if !columnExists(tx, "policies", "auto_host_ids_label_id") {
		if _, err := tx.Exec(`ALTER TABLE policies ADD COLUMN auto_host_ids_label_id INT UNSIGNED NULL`); err != nil {
			return errors.Wrap(err, "add auto_host_ids_label_id column to policies")
		}
	}

	if !fkExists(tx, "policies", "fk_policies_auto_label") {
		if _, err := tx.Exec(`ALTER TABLE policies ADD CONSTRAINT fk_policies_auto_label FOREIGN KEY (auto_host_ids_label_id) REFERENCES labels(id) ON DELETE SET NULL`); err != nil {
			return errors.Wrap(err, "add fk_policies_auto_label foreign key")
		}
	}

	if !columnExists(tx, "queries", "auto_host_ids_label_id") {
		if _, err := tx.Exec(`ALTER TABLE queries ADD COLUMN auto_host_ids_label_id INT UNSIGNED NULL`); err != nil {
			return errors.Wrap(err, "add auto_host_ids_label_id column to queries")
		}
	}

	if !fkExists(tx, "queries", "fk_queries_auto_label") {
		if _, err := tx.Exec(`ALTER TABLE queries ADD CONSTRAINT fk_queries_auto_label FOREIGN KEY (auto_host_ids_label_id) REFERENCES labels(id) ON DELETE SET NULL`); err != nil {
			return errors.Wrap(err, "add fk_queries_auto_label foreign key")
		}
	}

	return nil
}

func Down_20260217000000(_ *sql.Tx) error {
	return nil
}
