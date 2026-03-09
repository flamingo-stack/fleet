package tables

import (
	"database/sql"

	"github.com/pkg/errors"
)

func init() {
	MigrationClient.AddMigration(Up_20210903132338, Down_20210903132338)
}

func Up_20210903132338(tx *sql.Tx) error {
	// Idempotent migration.
	// Check if the primary key already includes username (target state)
	var count int
	err := tx.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'host_users' AND INDEX_NAME = 'PRIMARY' AND COLUMN_NAME = 'username'`).Scan(&count)
	if err == nil && count > 0 {
		return nil // already migrated
	}

	if columnExists(tx, "host_users", "id") {
		_, err := tx.Exec(`alter table host_users drop column id, drop primary key, add primary key(host_id, uid, username);`)
		if err != nil {
			return errors.Wrap(err, "dropping id from host_users")
		}
	}
	return nil
}

func Down_20210903132338(tx *sql.Tx) error {
	return nil
}
