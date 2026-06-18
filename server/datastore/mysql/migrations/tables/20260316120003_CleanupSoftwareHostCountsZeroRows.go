package tables

import (
	"database/sql"
)

func init() {
	MigrationClient.AddMigration(Up_20260316120003, Down_20260316120003)
}

func Up_20260316120003(tx *sql.Tx) error {
	// Idempotent migration.
	// Delete any accumulated zero-count rows from software_host_counts and software_titles_host_counts.
	// After this migration, the sync process uses an atomic swap table pattern that never produces zero-count rows.
	// Add CHECK constraints to prevent zero-count rows from being inserted in the future.
	// Constraints are unnamed because the sync process uses CREATE TABLE ... LIKE to create swap tables,
	// which copies CHECK constraints with auto-generated names. Named constraints would drift after each swap.
	// MySQL auto-names the first unnamed CHECK on each table <table>_chk_1, which we use as the idempotency guard.

	steps := []migrationStep{
		basicMigrationStep(
			`DELETE FROM software_host_counts WHERE hosts_count = 0`,
			"deleting zero-count rows from software_host_counts",
		),
	}
	if !constraintExists(tx, "software_host_counts", "software_host_counts_chk_1") {
		steps = append(steps, basicMigrationStep(
			`ALTER TABLE software_host_counts ADD CHECK (hosts_count > 0)`,
			"adding CHECK constraint to software_host_counts",
		))
	}
	steps = append(steps, basicMigrationStep(
		`DELETE FROM software_titles_host_counts WHERE hosts_count = 0`,
		"deleting zero-count rows from software_titles_host_counts",
	))
	if !constraintExists(tx, "software_titles_host_counts", "software_titles_host_counts_chk_1") {
		steps = append(steps, basicMigrationStep(
			`ALTER TABLE software_titles_host_counts ADD CHECK (hosts_count > 0)`,
			"adding CHECK constraint to software_titles_host_counts",
		))
	}

	return withSteps(steps, tx)
}

func Down_20260316120003(tx *sql.Tx) error {
	return nil
}
