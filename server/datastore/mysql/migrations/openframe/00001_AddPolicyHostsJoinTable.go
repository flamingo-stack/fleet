package openframe

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_00001, Down_00001)
}

func Up_00001(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS policy_hosts (
  id int unsigned NOT NULL AUTO_INCREMENT,
  policy_id int unsigned NOT NULL,
  host_id int unsigned NOT NULL,
  created_at timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  CONSTRAINT policy_hosts_policy_id FOREIGN KEY (policy_id) REFERENCES policies (id) ON DELETE CASCADE,
  CONSTRAINT policy_hosts_host_id FOREIGN KEY (host_id) REFERENCES hosts (id) ON DELETE CASCADE,
  UNIQUE KEY idx_policy_hosts_policy_host (policy_id, host_id)
)
`)
	if err != nil {
		return fmt.Errorf("creating policy_hosts table: %w", err)
	}
	return nil
}

func Down_00001(tx *sql.Tx) error {
	return nil
}
