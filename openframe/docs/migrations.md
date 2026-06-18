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

## Idempotency of upstream migrations

Separate from the openframe migration pipeline, the fork also **rewrote the
upstream `tables/` and `data/` migrations in place to be idempotent** (commit
`Idemponent migrations`, 5bc52398cd — ~475 files). Each modified migration carries
an `// Idempotent migration.` marker comment. The transformations are:

| Upstream pattern | Fork pattern | Count (approx.) |
|------------------|--------------|-----------------|
| `CREATE TABLE \`x\`` | `CREATE TABLE IF NOT EXISTS \`x\`` | ~146 |
| `INSERT INTO x ...` | `INSERT IGNORE INTO x ...` | ~49 |
| `DROP TABLE \`x\`` | `DROP TABLE IF EXISTS \`x\`` | ~10 |

### Why

OpenFrame tenants are provisioned and re-provisioned against databases that may be
in a partially-migrated state (restored snapshots, re-run jobs, idempotent Helm
migration jobs — see [helm-chart.md](helm-chart.md)). Making the DDL idempotent
means a migration that is re-applied against an object that already exists is a
no-op instead of a hard failure (`table already exists`, duplicate-key, etc.).
This pairs with the Helm migration job, which may run `fleet prepare db` more than
once across install/upgrade cycles.

### Trade-off and rebase cost

This is the single most invasive category of fork change by file count, and it is
a **standing merge cost**: every upstream release that adds or edits a migration
will conflict or arrive non-idempotent, and the new/changed migrations must be
re-patched to match. Two options when rebasing:

1. **Targeted re-patch** — for migrations that conflict or are newly added,
   re-apply the `IF NOT EXISTS` / `INSERT IGNORE` / `IF EXISTS` transformation and
   the `// Idempotent migration.` marker.
2. **Accept upstream as-is** for new migrations and only patch the ones that
   actually fail in practice. Lower effort, but loses the "always idempotent"
   guarantee for those files.

> The openframe migrations in `migrations/openframe/` should follow the same
> idempotency convention (see *Guidelines* above), but the bulk rewrite here is a
> distinct, upstream-facing change.

## Rebase workflow

When rebasing onto a newer upstream release:

1. **No openframe-pipeline changes needed.** Our migrations live in
   `migrations/openframe/` and use independent numbering — upstream changes to
   `migrations/tables/` don't affect the separate goose client.
2. **Re-apply idempotency** to any newly added or conflicting upstream migrations
   (see *Idempotency of upstream migrations* above).
3. Resolve any conflicts in Go source files (datastore methods, service handlers, etc.) as usual.
4. Run `fleet prepare db` on a test database to verify all three migration pipelines complete successfully.

## Known issue: `prepare db` early return (fixed)

Previously, `cmd/fleet/prepare.go` had an early `return` when `MigrationStatus()` reported `AllMigrationsCompleted`. Since `MigrationStatus()` only checks `tables` and `data` migrations, this caused `MigrateOpenframe()` to be skipped on databases where upstream migrations were already applied. The fix removes the early return so that openframe migrations always execute.
