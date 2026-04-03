# Query Results TTL Cleanup

## Overview

Fleet stores scheduled query results in the `query_results` MySQL table. In an openframe deployment, Debezium CDC captures every INSERT/UPDATE/DELETE on this table and streams changes to Kafka. Over time the table grows unbounded, causing database bloat and performance degradation.

This feature adds a **standalone cron schedule** that periodically deletes rows older than a configurable TTL. It is designed to work alongside the CDC pipeline — rows are kept long enough for Debezium to capture them, then purged.

## Motivation

- The `query_results` table in dev/staging environments grew to millions of rows, degrading MySQL performance.
- We cannot use `FLEET_SERVER_QUERY_REPORTS_DISABLED` because that stops Fleet from writing to `query_results` entirely, which breaks the Debezium CDC pipeline (no writes → no binlog events → no Kafka messages).
- Fleet's built-in `query_results_cleanup` job only handles rows for queries with `discard_data = true` — it does not enforce a time-based retention policy.

## Configuration

Two new server config values control the feature:

| Env var | YAML key | Default | Description |
|---------|----------|---------|-------------|
| `FLEET_SERVER_QUERY_RESULTS_TTL` | `server.query_results_ttl` | `1440h` (60 days) | Rows with `last_fetched` older than this are deleted. Set to `0` to disable. |
| `FLEET_SERVER_QUERY_RESULTS_CLEANUP_INTERVAL` | `server.query_results_cleanup_interval` | `1h` | How often the cleanup schedule runs. Set to `0` to disable. |

Both are `time.Duration` values read at startup. **Changing them requires a pod restart.**

Examples:

```
FLEET_SERVER_QUERY_RESULTS_TTL=720h          # 30 days
FLEET_SERVER_QUERY_RESULTS_TTL=2160h         # 90 days
FLEET_SERVER_QUERY_RESULTS_TTL=0             # disabled

FLEET_SERVER_QUERY_RESULTS_CLEANUP_INTERVAL=30m   # every 30 minutes
FLEET_SERVER_QUERY_RESULTS_CLEANUP_INTERVAL=6h    # every 6 hours
```

## Feature gate

The schedule only starts when `QueryResultsTTL > 0` (checked in `serve.go`). This is **not** gated behind `FLEET_OPENFRAME_MODE` because TTL-based cleanup is a general-purpose optimization that any Fleet deployment could benefit from. The env vars themselves act as the feature flag — deployments that don't set them get the defaults; deployments that set TTL to `0` disable the feature entirely.

## Architecture

The cleanup runs as an **independent cron schedule**, separate from the existing `cleanups_then_aggregation` schedule:

```
┌──────────────────────────────────────────────┐
│     cleanups_then_aggregation (1h)           │
│                                               │
│  • query_results_cleanup (discard_data only) │
│  • other cleanup + aggregation jobs          │
│                                               │
│  (unchanged — existing Fleet behavior)       │
└──────────────────────────────────────────────┘

┌──────────────────────────────────────────────┐
│     query_results_ttl_cleanup (configurable) │  ← NEW
│                                               │
│  • cleanup_expired_query_results             │
│  • own cron lock, own goroutine              │
│  • interval: QueryResultsCleanupInterval     │
│                                               │
└──────────────────────────────────────────────┘
```

Benefits of a separate schedule:
- **Independent failure** — if it fails, it does not block the existing cleanup/aggregation jobs.
- **Independent frequency** — can run more or less often than the hourly aggregation cycle.
- **Clear monitoring** — has its own name in `cron_stats` table and logs.

## Deletion algorithm

The `CleanupExpiredQueryResults` function in `server/datastore/mysql/query_results.go` uses a **rate-limited batch deletion** strategy to avoid long-running locks:

### Constants

| Constant | Value | Purpose |
|----------|-------|---------|
| `queryResultsTTLBatchSize` | 1,000 | Rows per SELECT+DELETE batch |
| `queryResultsTTLMinPerRun` | 3,000 | Minimum rows to delete per schedule run |
| `queryResultsTTLPercent` | 0.10 (10%) | Fraction of eligible rows to delete per run |
| `queryResultsTTLBatchSleep` | 100ms | Pause between batches |

### Flow

1. **Count eligible rows**: `SELECT COUNT(*) FROM query_results WHERE last_fetched < ?`
2. **Calculate max to delete this run**: `max(totalEligible * 10%, 3000)`, capped at `totalEligible`
3. **Batch loop** (repeat until done or cap reached):
   - `SELECT id FROM query_results WHERE last_fetched < ? ORDER BY id LIMIT 1000`
   - `DELETE FROM query_results WHERE id IN (...)`
   - Sleep 100ms between batches
   - Break if fewer IDs returned than batch size (no more rows)
   - Break if `totalDeleted >= maxToDelete`

### Example: 10,000 expired rows

```
Run 1 (hour 0):
  eligible = 10,000
  10% = 1,000 → min = 3,000 → maxToDelete = 3,000
  3 batches × 1,000 rows = 3,000 deleted
  remaining: 7,000

Run 2 (hour 1):
  eligible = 7,000
  10% = 700 → min = 3,000 → maxToDelete = 3,000
  3 batches × 1,000 rows = 3,000 deleted
  remaining: 4,000

Run 3 (hour 2):
  eligible = 4,000
  10% = 400 → min = 3,000 → maxToDelete = 3,000
  3 batches × 1,000 rows = 3,000 deleted
  remaining: 1,000

Run 4 (hour 3):
  eligible = 1,000
  10% = 100 → min = 3,000 → maxToDelete = 3,000
  cap at totalEligible → maxToDelete = 1,000
  1 batch × 1,000 rows = 1,000 deleted
  remaining: 0 ✅
```

Backlog of 10,000 rows drains in ~4 hourly runs without ever holding a lock on more than 1,000 rows.

## Observability

### Logs

When rows are deleted, the job emits:
```
msg="cleaned up expired query results" deleted=3000 ttl=1440h0m0s
```

When no expired rows exist, the job exits silently (no log spam).

### MySQL — cron_stats

```sql
SELECT * FROM cron_stats
WHERE name = 'query_results_ttl_cleanup'
ORDER BY created_at DESC LIMIT 5;
```

### MySQL — verify retention

```sql
-- Should return 0 after the job catches up
SELECT COUNT(*) FROM query_results
WHERE last_fetched < NOW() - INTERVAL 60 DAY;
```

## Files changed

| File | Purpose |
|------|---------|
| `server/config/config.go` | `QueryResultsTTL` and `QueryResultsCleanupInterval` config fields + registration |
| `server/fleet/datastore.go` | `CleanupExpiredQueryResults` method on `Datastore` interface |
| `server/fleet/cron_schedules.go` | `CronQueryResultsTTLCleanup` schedule name constant |
| `server/datastore/mysql/query_results.go` | MySQL implementation of `CleanupExpiredQueryResults` with batch deletion |
| `cmd/fleet/cron.go` | `newQueryResultsTTLCleanupSchedule` constructor |
| `cmd/fleet/serve.go` | Conditional registration of the schedule (when TTL > 0) |
| `server/mock/datastore_mock.go` | Mock implementation for testing |

## Upstream merge notes

This feature touches shared files that upstream Fleet also modifies:

- **`server/config/config.go`**: two new fields added to `ServerConfig` struct and two `addConfigDuration` / `getConfigDuration` calls. On conflict, re-add the fields and config lines.
- **`server/fleet/datastore.go`**: one new method added to `Datastore` interface. On conflict, re-add the method signature.
- **`server/fleet/cron_schedules.go`**: one new `CronScheduleName` constant. On conflict, re-add the constant.
- **`cmd/fleet/serve.go`**: one new `StartCronSchedule` block. On conflict, re-add the block after the `frequent_cleanups` registration.
- **`cmd/fleet/cron.go`**: one new function `newQueryResultsTTLCleanupSchedule` appended after `newFrequentCleanupsSchedule`. Also a minor variable rename (`config` → `appConfig`) inside `query_results_cleanup` job to avoid shadowing the outer `config` parameter. On conflict, re-add the function and apply the rename.
- **`server/mock/datastore_mock.go`**: auto-generated mock entries. Regenerate or manually add.
- **`server/datastore/mysql/query_results.go`**: new function appended to end of file. Low conflict risk.
