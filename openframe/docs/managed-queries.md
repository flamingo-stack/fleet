# OpenFrame-Managed Queries

## Overview

An **OpenFrame-managed query** is a normal Fleet query (a "report" in current product terminology)
carrying `queries.openframe_managed = 1`. It is omitted from the query **listing** and from the
count that listing returns, while it keeps running on hosts and keeps recording results exactly like
any other query.

The use case is platform-owned telemetry: OpenFrame needs its own scheduled queries on every tenant
without them cluttering the operator's Reports page.

This is the queries twin of [managed-policies.md](managed-policies.md) — same column name, same
semantics, same rationale for the name (`openframe_managed` cannot collide with a field upstream
Fleet may add later). Read the policies doc for the shared reasoning; this one covers what differs.

Like the policies side, it is **not** gated on `FLEET_OPENFRAME_MODE`: `prepare db` runs
`MigrateOpenframe` unconditionally, so the column exists in every deployment and the filter is
always on — inert until something sets the flag.

## What the flag does — and what it deliberately does not

| Surface | Managed query |
|---|---|
| `GET /queries` (incl. `merge_inherited`) | **omitted** |
| the `count` / `inherited_count` that listing returns | **not counted** |
| `GET /queries/{id}` | returned in full |
| `PATCH /queries/{id}` | accepted — including `openframe_managed: false` |
| `DELETE /queries/{id}`, `POST /queries/delete` | accepted |
| `POST /spec/queries` with a matching name | not verified — see below |
| Scheduled execution on hosts, query reports, live results | unchanged |

**This flag is decluttering, not access control** — same as for policies. The listing is filtered;
by-id reads and every write path ignore the flag, so an operator holding admin or maintainer can
read, unmanage, rewrite, or delete a managed query if they know its id (ids are sequential) or its
name.

Two differences worth stating explicitly rather than assuming:

- The five attack paths in [managed-policies.md](managed-policies.md) were **verified against a
  running server for policies only**. For queries the code shape is the same, so the same conclusions
  are very likely — but the `POST /spec/queries` overwrite-by-name path in particular has not been
  exercised here. Treat it as presumed, not proven.
- The GitOps deletion pass was likewise reasoned about for policies. The query side of `fleetctl
  gitops` has not been checked.

## API

Create:

```json
POST /api/latest/fleet/queries
{ "name": "openframe: disk inventory", "query": "SELECT 1 ...", "openframe_managed": true }
```

`PATCH /api/latest/fleet/queries/{id}` takes `"openframe_managed": true|false`; omitting the field
leaves the flag as-is. Every query payload returns `"openframe_managed"`.

There is no `include_managed` parameter, for the same fail-closed reason as on the policies side.

## Implementation

### Schema

`server/datastore/mysql/migrations/openframe/20260818000002_AddQueriesOpenframeManagedColumn.go`
adds `queries.openframe_managed TINYINT(1) NOT NULL DEFAULT 0`, and `schema.sql` carries the same
column so test databases match production. It ALTERs an upstream table from the OpenFrame pipeline
and is on the [semantic-conflict watchlist](upstream-sync-conflict-resolution.md) — including the
fact that `make dump-test-schema` would drop the `schema.sql` line.

### One insertion point instead of five

This is the notable difference from the policies side. `ListQueries` builds a `whereClauses` string
and then derives its count statement by wrapping the very same statement:

```go
getQueriesCountStmt = fmt.Sprintf("SELECT COUNT(DISTINCT id) ... FROM (%s) AS s", getQueriesStmt)
```

So appending the exclusion to `whereClauses` filters the listing **and** its count in one place —
where policies needed the fragment repeated across five separate queries.

The constant lives in `server/datastore/mysql/queries.go` under `OPENFRAME(managed-queries)` markers:

```go
const openframeManagedQueryExclusion = ` AND q.openframe_managed = 0`
```

The flag is read back through the two SELECT column lists (`Query` by id and `ListQueries`) and
written by the same `INSERT`/`UPDATE` as every other query field (`NewQuery`, `SaveQuery`).
`ApplyQueries` does not name the column, so a spec apply leaves it untouched.

> Loading the flag in `Query(ctx, id)` is not optional: `modifyQuery` reads the query, applies the
> payload, and hands the whole struct to `SaveQuery`, which now writes `openframe_managed`. If the
> read did not populate it, every `PATCH` that omitted the field would silently reset it to `0`.

### Touched files

| File | Change |
|------|--------|
| `migrations/openframe/20260818000002_AddQueriesOpenframeManagedColumn.go` | the column |
| `server/datastore/mysql/schema.sql` | the column, so test databases match production |
| `server/fleet/queries.go` | `OpenframeManaged` on `Query` and on `QueryPayload` |
| `server/service/queries.go` | maps the flag on query create and on modify |
| `server/datastore/mysql/queries.go` | the `INSERT`, the `UPDATE`, both SELECT lists, and the listing exclusion |
| `server/datastore/mysql/queries_openframe_managed_test.go` | MySQL coverage |

## Host assignment interaction (open item)

In OpenFrame mode queries are scoped to hosts through `query_hosts` the same way policies are
through `policy_hosts` (see [architecture-host-assignments.md](architecture-host-assignments.md)).
A managed query without assignments therefore reaches no host, and hosts enrolling later are not
backfilled. Making the flag bypass that requirement is deliberately out of scope here — it changes
what agents execute and wants its own review.
