package openframe

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_00002, Down_00002)
}

func Up_00002(tx *sql.Tx) error {
	// Idempotent migration.
	_, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS query_hosts (
  id int unsigned NOT NULL AUTO_INCREMENT,
  query_id int unsigned NOT NULL,
  host_id int unsigned NOT NULL,
  created_at timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  CONSTRAINT query_hosts_query_id FOREIGN KEY (query_id) REFERENCES queries (id) ON DELETE CASCADE,
  CONSTRAINT query_hosts_host_id FOREIGN KEY (host_id) REFERENCES hosts (id) ON DELETE CASCADE,
  UNIQUE KEY idx_query_hosts_query_host (query_id, host_id)
)
`)
	if err != nil {
		return fmt.Errorf("creating query_hosts table: %w", err)
	}
	return nil
}

func Down_00002(tx *sql.Tx) error {
	return nil
}
