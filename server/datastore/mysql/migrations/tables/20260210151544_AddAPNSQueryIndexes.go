package tables

import (
	"database/sql"
)

func init() {
	MigrationClient.AddMigration(Up_20260210151544, Down_20260210151544)
}

func Up_20260210151544(tx *sql.Tx) error {
	// Idempotent migration.
	return withSteps([]migrationStep{
		func(tx *sql.Tx) error {
			if indexExistsTx(tx, "nano_enrollment_queue", "idx_neq_filter") {
				return nil
			}
			return basicMigrationStep(`CREATE INDEX idx_neq_filter ON nano_enrollment_queue (
    active,
    priority,
    created_at
);`, "creating index idx_neq_filter on nano_enrollment_queue")(tx)
		},
		func(tx *sql.Tx) error {
			if indexExistsTx(tx, "nano_command_results", "idx_ncr_lookup") {
				return nil
			}
			return basicMigrationStep(`CREATE INDEX idx_ncr_lookup ON nano_command_results (id, command_uuid, status);`, "creating index idx_ncr_lookup on nano_command_results")(tx)
		},
	}, tx)
}

func Down_20260210151544(tx *sql.Tx) error {
	return nil
}
