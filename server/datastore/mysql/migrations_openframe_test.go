// OPENFRAME(host-assignments): verifies the separate OpenFrame migration pipeline —
// openframe/docs/migrations.md
//
// IMPORTANT: the datastore test harness loads server/datastore/mysql/schema.sql,
// which is dumped from the UPSTREAM tables/ migrations only — it does NOT contain
// policy_hosts / query_hosts / migration_status_openframe. So any test that needs
// the OpenFrame tables must call ds.MigrateOpenframe(ctx) first (as this test does).
// This also guards the sync hazard documented in
// openframe/docs/upstream-sync-conflict-resolution.md: `make dump-test-schema`
// regenerates schema.sql without the OpenFrame tables.
package mysql

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigrateOpenframeIdempotent(t *testing.T) {
	ds := CreateMySQLDS(t)
	ctx := context.Background()

	// schema.sql does not include the OpenFrame tables; the first call creates
	// policy_hosts / query_hosts via the separate goose client.
	require.NoError(t, ds.MigrateOpenframe(ctx))

	// Running again must be a no-op: the openframe migrations are idempotent
	// (CREATE TABLE IF NOT EXISTS) and tracked in migration_status_openframe.
	require.NoError(t, ds.MigrateOpenframe(ctx))

	// The join tables must now exist.
	for _, table := range []string{"policy_hosts", "query_hosts"} {
		var n int
		require.NoError(t,
			ds.writer(ctx).GetContext(ctx, &n,
				"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?",
				table),
			"checking table %s exists", table)
		require.Equal(t, 1, n, "expected table %s to exist after MigrateOpenframe", table)
	}
}
