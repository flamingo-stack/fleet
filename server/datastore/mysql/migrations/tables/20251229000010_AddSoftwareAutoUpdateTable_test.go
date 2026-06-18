package tables

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUp_20251229000010 guards the fork's migration-idempotency convention for
// this migration (openframe/docs/migrations.md). Upstream's version runs an
// unconditional `ALTER TABLE hosts ADD COLUMN timezone`, which fails with
// "Error 1060: Duplicate column name 'timezone'" against a partially-migrated /
// divergent database where the column already exists (as our dev databases do).
// The fork guards it with columnExists; this test pins that behavior so a future
// upstream sync can't silently reintroduce the gap.
func TestUp_20251229000010(t *testing.T) {
	db := newDBConnForTests(t)

	// Inline the applyUpToPrev loop to bypass its >60-day age skip: this
	// migration carries an upstream timestamp far in the past, but the fork
	// modified it and must keep exercising it on every run.
	const version = 20251229000010
	for {
		current, err := MigrationClient.GetDBVersion(db.DB)
		require.NoError(t, err)
		next, err := MigrationClient.Migrations.Next(current)
		require.NoError(t, err)
		if next.Version == version {
			break
		}
		applyNext(t, db)
	}

	// Simulate the divergent database that exposed the bug: hosts.timezone was
	// already added out-of-band. Without the columnExists guard, applyNext below
	// reproduces the duplicate-column failure.
	_, err := db.Exec(`ALTER TABLE hosts ADD COLUMN timezone VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci`)
	require.NoError(t, err)

	// Applying the migration must succeed despite the pre-existing column.
	applyNext(t, db)

	// Double-apply assertion: running the Up body again must be a no-op (goose
	// won't re-run a recorded migration, so invoke it directly).
	tx, err := db.Begin()
	require.NoError(t, err)
	require.NoError(t, Up_20251229000010(tx))
	require.NoError(t, tx.Commit())

	// The new table must exist and the timezone column must still be present.
	var n int
	require.NoError(t, db.Get(&n,
		`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'software_update_schedules'`))
	require.Equal(t, 1, n, "software_update_schedules table should exist")

	require.NoError(t, db.Get(&n,
		`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'hosts' AND column_name = 'timezone'`))
	require.Equal(t, 1, n, "hosts.timezone column should exist exactly once")
}
