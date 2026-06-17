# Openframe Migration Strategy

## Problem

Fleet uses [goose](https://github.com/pressly/goose) for database migrations with a **high-water mark** tracking model. The migration runner (`Up()`) works as follows:

1. Read the current DB version — the highest applied migration number from `migration_status_tables`.
2. Find the next migration with `version > current`.
3. Apply it and repeat.

Migrations with a version number **below** the current DB version are permanently skipped.

### Why this matters for a fork

Openframe maintains a fork of Fleet and periodically rebases onto the upstream `main` branch. If we place our custom migrations in the same `tables/` directory as upstream, we face two failure modes:

**Timestamp collision**: upstream adds a migration with the same or nearby timestamp as ours → build error (duplicate function names) or undefined ordering.

**Skipped migrations on existing deployments**: if a tenant has already applied upstream migrations past our timestamp, our migrations will never run:

```
Upstream applied:
  20260223000000  CleanupSoftwareHostCountsZeroRows
  20260225143121  FixUnverifiedSuccessfulWindowsProfiles
  ...
  20260306120000  RenameActivitiesToActivityPast    ← current DB version

Our migrations (added after rebase):
  20260223000001  AddPolicyHostsJoinTable           ← SKIPPED (< 20260306120000)
  20260223000002  AddQueryHostsJoinTable            ← SKIPPED
```

## Solution: Separate goose client

Fleet already uses this pattern internally — `tables.MigrationClient` and `data.MigrationClient` are independent goose instances with separate tracking tables. We add a third:

```
server/datastore/mysql/migrations/
├── tables/      ← upstream Fleet     (tracking table: migration_status_tables)
├── data/        ← upstream Fleet     (tracking table: migration_status_data)
└── openframe/   ← our migrations    (tracking table: migration_status_openframe)
```

### How it works

```go
// server/datastore/mysql/migrations/openframe/migration.go
var MigrationClient = goose.New("migration_status_openframe", goose.MySqlDialect{})
```

Each openframe migration registers itself against `MigrationClient`:

```go
// server/datastore/mysql/migrations/openframe/20260301000001_AddPolicyHostsJoinTable.go
func init() {
    MigrationClient.AddMigration(Up_20260301000001, Down_20260301000001)
}
```

The datastore exposes a new method:

```go
// server/datastore/mysql/mysql.go
func (ds *Datastore) MigrateOpenframe(ctx context.Context) error {
    return openframemigrations.MigrationClient.Up(ds.writer(ctx).DB, "")
}
```

And `prepare db` calls all three in order:

```go
// cmd/fleet/prepare.go
ds.MigrateTables(...)    // 1. upstream schema (creates policies, queries, hosts, ...)
ds.MigrateData(...)      // 2. upstream built-in data
ds.MigrateOpenframe(...) // 3. openframe schema (creates policy_hosts, query_hosts, ...)
```

### Execution order guarantees

The three migration clients run **sequentially**. By the time `MigrateOpenframe` executes, all upstream tables (`policies`, `queries`, `hosts`) already exist. This satisfies the foreign key dependencies in our `CREATE TABLE` statements.

## Conventions for adding new migrations

### Naming

Use date-based timestamps matching the Fleet convention — `YYYYMMDDHHMMSS`:

```
20260301000001_AddPolicyHostsJoinTable.go
20260301000002_AddQueryHostsJoinTable.go
20260301000003_YourNextMigration.go
```

This is required because the goose `parseNameAndDate` function expects an 8+ digit date prefix. Function names inside the file must match:

```go
func init() {
    MigrationClient.AddMigration(Up_20260301000003, Down_20260301000003)
}
```

### Template

```go
package openframe

import (
    "database/sql"
    "fmt"
)

func init() {
    MigrationClient.AddMigration(Up_20260301000003, Down_20260301000003)
}

func Up_20260301000003(tx *sql.Tx) error {
    _, err := tx.Exec(`
        -- Your DDL here.
        -- Use CREATE TABLE IF NOT EXISTS / ADD COLUMN IF NOT EXISTS
        -- for idempotency where possible.
    `)
    if err != nil {
        return fmt.Errorf("description of migration: %w", err)
    }
    return nil
}

func Down_20260301000003(tx *sql.Tx) error {
    return nil
}
```

### Guidelines

1. **Always use `IF NOT EXISTS` / `IF EXISTS`** in DDL when possible. This makes migrations safe to re-run in edge cases.
2. **Foreign keys must reference upstream tables?** That's fine — upstream tables are guaranteed to exist because `MigrateTables` runs first.
3. **Never modify upstream tables** in openframe migrations. If you need to alter an upstream table (e.g. add a column to `policies`), do it in a way that won't conflict on rebase — or better, use a join table pattern instead.
4. **Keep migrations small and focused** — one logical change per file.
5. **Down migrations are optional.** Fleet convention is to return `nil` from `Down_*` functions.

## Database state

After running all migrations, the database will contain three tracking tables:

| Table | Owner | Tracks |
|-------|-------|--------|
| `migration_status_tables` | upstream Fleet | Schema migrations (700+ entries) |
| `migration_status_data` | upstream Fleet | Built-in data migrations |
| `migration_status_openframe` | openframe fork | Openframe-specific schema |

Each table has the standard goose schema:

```sql
SELECT * FROM migration_status_openframe;
+----+----------------+------------+---------------------+
| id | version_id     | is_applied | tstamp              |
+----+----------------+------------+---------------------+
|  1 |              0 |          1 | 2026-03-01 00:00:00 |  -- bootstrap
|  2 | 20260301000001 |          1 | 2026-03-01 00:00:01 |  -- policy_hosts
|  3 | 20260301000002 |          1 | 2026-03-01 00:00:02 |  -- query_hosts
+----+----------------+------------+---------------------+
```

## Rebase workflow

When rebasing onto a newer upstream release:

1. **Openframe migrations need no changes.** Our migrations live in `migrations/openframe/` and use independent numbering — upstream changes to `migrations/tables/` don't affect them.
2. Resolve any conflicts in Go source files (datastore methods, service handlers, etc.) as usual.
3. **Scan `migrations/tables/` for duplicate-named migrations** and make the new upstream copies idempotent — see [Duplicate-migration collisions](#duplicate-migration-collisions-after-an-upstream-sync) below. This is the one way an upstream sync *can* break migrations on existing tenants.
4. Run `fleet prepare db` against a **dump of a real tenant database** (not an empty one) to verify all three pipelines complete — an empty DB will not surface collisions with already-applied migrations.

## Duplicate-migration collisions after an upstream sync

The fork carries its **own copies of some upstream `tables/` migrations** under their *original* timestamps — they were captured during the squashed upstream re-import (commit `01a6bb8a63`) and made idempotent in a later pass. Upstream sometimes **re-timestamps or re-adds the same migration** under a *new* (often round) timestamp — e.g. upstream PR #37993 "Re-timestamp migrations due to 4.79 cherry pick" renumbered a batch to `20251229000010`, `20251229000020`, … . A merge keeps **both** files (different filenames → no textual conflict), so the same schema change is registered twice.

**Symptom:** on any DB that already applied the fork's copy (i.e. every existing tenant), `prepare db` aborts mid-run when it reaches upstream's re-timestamped copy:

```
FAIL 20251229000010_AddSoftwareAutoUpdateTable.go (Error 1060 (42S21): Duplicate column name 'timezone'), quitting migration.
```

An **empty** test DB never hits this — the fork copy and the upstream copy both apply once, in timestamp order. The collision only appears against a DB that is *already past* the fork copy's timestamp. Always test `prepare db` against a real tenant dump.

**Detect** duplicate-named migrations after every sync:

```bash
ls server/datastore/mysql/migrations/tables/*.go \
  | sed -E 's#.*/[0-9]+_##; s#\.go$##' | grep -v '_test$' \
  | sort | uniq -d
```

Any name printed is a migration the fork carries under two timestamps. For each, the **fork copy** (older timestamp, already applied on tenants) is already idempotent; the **upstream copy** (newer timestamp) usually is not.

**Fix:** make the upstream copy idempotent, mirroring the fork copy's guard. Use the helpers in `migrations/tables/migration.go` — `columnExists`, `tableExists`, `constraintExists`, `indexExistsTx`:

```go
// 20251229000010_AddSoftwareAutoUpdateTable.go (upstream copy)
if !columnExists(tx, "hosts", "timezone") {
    if _, err = tx.Exec(`ALTER TABLE hosts ADD COLUMN timezone ...`); err != nil {
        return err
    }
}
```

This no-ops on existing tenants (column/table already present) while still creating the schema on fresh DBs — the same idempotency pass the fork already applied to ~477 upstream migrations. Down migrations stay no-ops.

## Known issue: `prepare db` early return (fixed)

Previously, `cmd/fleet/prepare.go` had an early `return` when `MigrationStatus()` reported `AllMigrationsCompleted`. Since `MigrationStatus()` only checks `tables` and `data` migrations, this caused `MigrateOpenframe()` to be skipped on databases where upstream migrations were already applied. The fix removes the early return so that openframe migrations always execute.
