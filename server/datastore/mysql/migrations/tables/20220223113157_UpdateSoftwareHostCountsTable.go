package tables

import (
	"database/sql"

	"github.com/pkg/errors"
)

func init() {
	MigrationClient.AddMigration(Up_20220223113157, Down_20220223113157)
}

func Up_20220223113157(tx *sql.Tx) error {
	// Idempotent migration.
	// Check if the primary key already includes team_id (target state)
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'software_host_counts' AND INDEX_NAME = 'PRIMARY' AND COLUMN_NAME = 'team_id'`).Scan(&count); err == nil && count > 0 {
		// already migrated, skip DROP/ADD PRIMARY KEY
	} else if !columnExists(tx, "software_host_counts", "team_id") {
		alterStmt := `ALTER TABLE software_host_counts
    ADD COLUMN team_id INT(10) UNSIGNED NOT NULL DEFAULT 0,
    DROP PRIMARY KEY,
    ADD PRIMARY KEY (software_id, team_id),
    ADD INDEX idx_software_host_counts_team_id_hosts_count_software_id (team_id,hosts_count,software_id),
    DROP INDEX idx_software_host_counts_host_count_software_id`
		if _, err := tx.Exec(alterStmt); err != nil {
			return errors.Wrap(err, "alter software_host_counts table")
		}
	} else {
		if !indexExistsTx(tx, "software_host_counts", "idx_software_host_counts_team_id_hosts_count_software_id") {
			if _, err := tx.Exec(`ALTER TABLE software_host_counts ADD INDEX idx_software_host_counts_team_id_hosts_count_software_id (team_id,hosts_count,software_id)`); err != nil {
				return errors.Wrap(err, "add index idx_software_host_counts_team_id_hosts_count_software_id")
			}
		}
		if indexExistsTx(tx, "software_host_counts", "idx_software_host_counts_host_count_software_id") {
			if _, err := tx.Exec(`ALTER TABLE software_host_counts DROP INDEX idx_software_host_counts_host_count_software_id`); err != nil {
				return errors.Wrap(err, "drop index idx_software_host_counts_host_count_software_id")
			}
		}
	}
	return nil
}

func Down_20220223113157(tx *sql.Tx) error {
	return nil
}
