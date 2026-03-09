# Architecture: Host-Level Policy & Query Targeting

## Overview

Fleet natively supports targeting policies and queries by **labels** (dynamic groups of hosts).
Openframe extends this with **direct host targeting** — the ability to assign individual hosts to a policy or query via a many-to-many relationship.

This feature is gated behind the `FLEET_OPENFRAME_MODE=1` environment variable so it has zero impact on standard Fleet deployments.

## Motivation

In an openframe SaaS tenant, operators need to:

- Run a policy check against a specific set of devices (e.g. 5 hosts out of 10,000).
- Schedule a live query on explicitly selected machines.
- Manage assignments without label overhead when the target set is ad-hoc and short-lived.

Label-based targeting requires creating/deleting labels for every ad-hoc selection, which is cumbersome and creates label sprawl. Direct host targeting provides a simpler model for this use case.

## Feature gate

```
┌──────────────────────────────────────────────┐
│              FLEET_OPENFRAME_MODE             │
│                                               │
│  "1"  → host assignment endpoints enabled     │
│  ""   → endpoints return 400 Bad Request      │
└──────────────────────────────────────────────┘
```

Implementation: `server/fleet/openframe.go` — `IsOpenframeMode()` reads the env var at call time.

Every service method checks this gate before proceeding. No new tables are queried, no join logic is executed, and no extra SQL is generated when the mode is off.

## Database schema

Two join tables were added via openframe-specific migrations (see [migrations.md](migrations.md) for the separate migration pipeline):

```
migrations/openframe/00001_AddPolicyHostsJoinTable.go
migrations/openframe/00002_AddQueryHostsJoinTable.go
```

```sql
CREATE TABLE policy_hosts (
  id          INT UNSIGNED NOT NULL AUTO_INCREMENT,
  policy_id   INT UNSIGNED NOT NULL,
  host_id     INT UNSIGNED NOT NULL,
  created_at  TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY  idx_policy_hosts_policy_host (policy_id, host_id),
  FOREIGN KEY (policy_id) REFERENCES policies (id) ON DELETE CASCADE,
  FOREIGN KEY (host_id)   REFERENCES hosts (id)    ON DELETE CASCADE
);

CREATE TABLE query_hosts (
  id          INT UNSIGNED NOT NULL AUTO_INCREMENT,
  query_id    INT UNSIGNED NOT NULL,
  host_id     INT UNSIGNED NOT NULL,
  created_at  TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY  idx_query_hosts_query_host (query_id, host_id),
  FOREIGN KEY (query_id) REFERENCES queries (id) ON DELETE CASCADE,
  FOREIGN KEY (host_id)  REFERENCES hosts (id)   ON DELETE CASCADE
);
```

Key design decisions:

- **`ON DELETE CASCADE`** on both FKs — deleting a policy/query/host automatically cleans up assignments.
- **Composite unique key** `(policy_id, host_id)` / `(query_id, host_id)` prevents duplicate assignments and enables `INSERT IGNORE`.
- **Surrogate `id` PK** provides a stable row identity for potential future audit/history needs.

## Layered architecture

```
┌──────────────────────────────────────────────┐
│                 HTTP Handler                  │
│  POST / DELETE / PUT / GET  .../{id}/hosts   │
│  (server/service/handler.go)                 │
└────────────────────┬─────────────────────────┘
                     │
┌────────────────────▼─────────────────────────┐
│              Service Layer                    │
│  AddPolicyHosts / RemovePolicyHosts /        │
│  ReplacePolicyHosts / ListPolicyHosts        │
│  (same for queries)                          │
│                                               │
│  • Checks IsOpenframeMode()                  │
│  • Loads the parent policy/query             │
│  • Authorizes (fleet.ActionWrite/Read)       │
│  • Delegates to datastore                    │
│                                               │
│  (server/service/global_policies.go)         │
│  (server/service/queries.go)                 │
└────────────────────┬─────────────────────────┘
                     │
┌────────────────────▼─────────────────────────┐
│             Datastore Layer                   │
│  MySQL implementations                       │
│                                               │
│  • Add:     INSERT IGNORE (idempotent)       │
│  • Remove:  DELETE ... WHERE IN (idempotent) │
│  • Replace: DELETE ALL + INSERT (in tx)      │
│  • List:    SELECT + pagination              │
│                                               │
│  (server/datastore/mysql/policies.go)        │
│  (server/datastore/mysql/queries.go)         │
└────────────────────┬─────────────────────────┘
                     │
┌────────────────────▼─────────────────────────┐
│               MySQL                          │
│  policy_hosts / query_hosts tables           │
└──────────────────────────────────────────────┘
```

## API design

Four operations are exposed for each entity type (policies and queries):

| Operation | HTTP method | Path | Concurrency-safe | Use case |
|-----------|-------------|------|-------------------|----------|
| **Add** | `POST` | `.../{id}/hosts` | Yes | UI, incremental updates |
| **Remove** | `DELETE` | `.../{id}/hosts` | Yes | UI, incremental updates |
| **Replace** | `PUT` | `.../{id}/hosts` | No (full swap) | GitOps, bulk sync |
| **List** | `GET` | `.../{id}/hosts` | Yes (read-only) | Display, export |

### Why separate add/remove instead of PATCH with full list?

A PATCH-style "send the entire list every time" approach suffers from **lost updates** under concurrent access:

```
Time    User A                     User B
─────────────────────────────────────────────────
t1      GET → [1, 2, 3]
t2                                 GET → [1, 2, 3]
t3      PATCH [1, 2, 3, 4]        
t4                                 PATCH [1, 2, 5]  ← overwrites User A's addition of 4
```

With atomic add/remove, both operations succeed independently:

```
Time    User A                     User B
─────────────────────────────────────────────────
t3      POST {host_ids: [4]}       
t4                                 POST {host_ids: [5]}
                                   DELETE {host_ids: [3]}
Result: [1, 2, 4, 5]              ← both changes preserved
```

### Why keep PUT (replace)?

The `PUT` endpoint is intentionally retained for **single-writer** scenarios:

- **GitOps**: a CI/CD pipeline declares the canonical host list in a YAML file and pushes the full state.
- **Bulk import/migration**: an operator replaces the entire list from a CSV or external system.
- **Clear all**: `PUT` with an empty array is the cleanest way to remove all assignments.

## Read path integration

When openframe mode is on, existing `GET` endpoints for policies and queries include a `hosts_include_any` read-only field in their responses:

```
GET /api/v1/fleet/policies/{id}
→ { ..., "hosts_include_any": [{"id": 1, "hostname": "..."}] }

GET /api/v1/fleet/queries/{id}
→ { ..., "hosts_include_any": [{"id": 1, "hostname": "..."}] }
```

This is loaded via `loadHostsForPolicies` / `loadHostsForQueries` helper functions in the MySQL datastore layer, which join against the `policy_hosts` / `query_hosts` tables.

## Agent query filtering

When a host checks in and requests its scheduled queries or policies, Fleet's query planner filters results through the join tables:

- **Policies**: `PolicyQueriesForHost` in `server/datastore/mysql/policies.go` adds a conditional `EXISTS (SELECT 1 FROM policy_hosts ...)` clause when openframe mode is enabled.
- **Queries**: `ListScheduledQueriesForAgents` in `server/datastore/mysql/queries.go` adds a similar conditional filter.

If `policy_hosts` rows exist for a policy, only hosts in that list receive the policy. If no rows exist, the policy is delivered to all hosts (standard Fleet behavior). This preserves backward compatibility — existing policies without host assignments continue to work as before.

## Domain model

The `HostIdent` struct carries the minimal host identity needed for assignment responses:

```go
type HostIdent struct {
    HostID   uint   `json:"id" db:"id"`
    Hostname string `json:"hostname" db:"hostname"`
}
```

The `PolicyData.HostsIncludeAny` and `Query.HostsIncludeAny` fields (type `[]HostIdent`) are **read-only** — they are populated from the database when loading the parent object but cannot be set via the create/modify endpoints. All mutations go through the dedicated host assignment endpoints.

## Deployment

The feature is controlled by a single environment variable:

| Environment | Configuration |
|-------------|---------------|
| **SaaS tenant (Helm)** | `fleet.environments.FLEET_OPENFRAME_MODE: "1"` in `manifests/integrated-tools/fleet/values.yaml` |
| **Docker Compose** | `FLEET_OPENFRAME_MODE=1` in `docs/solutions/docker-compose/.env` |
| **Local dev** | `FLEET_OPENFRAME_MODE=1 ./build/fleet serve ...` |

When the variable is absent or set to anything other than `"1"`, all openframe endpoints return `400 Bad Request` and no join-table queries are executed.

## Files changed

| File | Purpose |
|------|---------|
| `server/fleet/openframe.go` | Feature gate function |
| `server/fleet/policies.go` | `HostIdent` type, `HostsIncludeAny` on `PolicyData` |
| `server/fleet/queries.go` | `HostsIncludeAny` on `Query` |
| `server/fleet/datastore.go` | Datastore interface (8 new methods + `MigrateOpenframe`) |
| `server/fleet/service.go` | Service interface (8 new methods) |
| `server/datastore/mysql/migrations/openframe/migration.go` | Separate goose client (`migration_status_openframe`) |
| `server/datastore/mysql/migrations/openframe/00001_*` | `policy_hosts` table |
| `server/datastore/mysql/migrations/openframe/00002_*` | `query_hosts` table |
| `server/datastore/mysql/mysql.go` | `MigrateOpenframe()` method |
| `cmd/fleet/prepare.go` | Calls `MigrateOpenframe` after upstream migrations |
| `server/datastore/mysql/policies.go` | MySQL implementation for policy hosts |
| `server/datastore/mysql/queries.go` | MySQL implementation for query hosts |
| `server/datastore/mysql/hosts.go` | Policy-for-host query filter |
| `server/service/global_policies.go` | Policy host assignment endpoints |
| `server/service/queries.go` | Query host assignment endpoints |
| `server/service/handler.go` | Route registration |
| `server/service/labels_util.go` | Host ID validation helper |
| `server/mock/datastore_mock.go` | Mock datastore |
| `server/mock/service/service_mock.go` | Mock service |
