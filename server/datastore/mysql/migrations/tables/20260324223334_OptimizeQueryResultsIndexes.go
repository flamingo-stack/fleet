package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20260324223334, Down_20260324223334)
}

func Up_20260324223334(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "query_results", "has_data") {
		if _, err := tx.Exec(`ALTER TABLE query_results ADD COLUMN has_data TINYINT(1) GENERATED ALWAYS AS (data IS NOT NULL) VIRTUAL`); err != nil {
			return fmt.Errorf("adding has_data virtual column to query_results: %w", err)
		}
	}
	if !indexExistsTx(tx, "query_results", "idx_query_id_has_data_host_id_last_fetched") {
		if _, err := tx.Exec(`ALTER TABLE query_results ADD INDEX idx_query_id_has_data_host_id_last_fetched (query_id, has_data, host_id, last_fetched)`); err != nil {
			return fmt.Errorf("adding idx_query_id_has_data_host_id_last_fetched index to query_results: %w", err)
		}
	}
	return nil
}

func Down_20260324223334(tx *sql.Tx) error {
	return nil
}
