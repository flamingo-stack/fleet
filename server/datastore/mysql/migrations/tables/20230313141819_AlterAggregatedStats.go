package tables

import (
	"database/sql"

	"github.com/pkg/errors"
)

func init() {
	MigrationClient.AddMigration(Up_20230313141819, Down_20230313141819)
}

func Up_20230313141819(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "aggregated_stats", "global_stats") {
		if _, err := tx.Exec(
			"ALTER TABLE aggregated_stats ADD COLUMN global_stats tinyint(1) NOT NULL DEFAULT 0"); err != nil {
			return errors.Wrap(err, "add global_stats column")
		}
	}

	// Check if global_stats is already part of the primary key
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'aggregated_stats' AND INDEX_NAME = 'PRIMARY' AND COLUMN_NAME = 'global_stats'`).Scan(&count); err == nil && count > 0 {
		// already migrated, skip DROP/ADD PRIMARY KEY
	} else {
		if _, err := tx.Exec(
			"ALTER TABLE aggregated_stats DROP PRIMARY KEY, ADD PRIMARY KEY(`id`, `type`, `global_stats`)"); err != nil {
			return errors.Wrap(err, "update primary key for aggregated_stats")
		}
	}

	// pre-existing rows with id=0 are global stats, and from now on when id=0
	// and global_stats=0 it will mean "hosts that are part of no team" instead
	// of "all teams/global"
	if _, err := tx.Exec("UPDATE aggregated_stats SET global_stats=1 WHERE id=0"); err != nil {
		return errors.Wrap(err, "update global_stats flag")
	}

	return nil
}

func Down_20230313141819(tx *sql.Tx) error {
	return nil
}
