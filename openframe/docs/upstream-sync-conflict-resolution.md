# Upstream Sync — Conflict-Resolution Runbook

This is the procedure for merging upstream `fleetdm/fleet` into this fork and
resolving the conflicts. It is written to be followed by a human **or by Claude
Code**. Read it before touching a conflicted merge.

## Mechanics & orientation

- The fork tracks Fleet **~v4.90.1** (synced from `upstream/main` `b139336f8f`) and drifts
  thousands of commits behind
  `upstream/main`. It does **not** clean-merge — historically it squash-rebases.
- The CI workflow `.github/workflows/sync-upstream.yml` runs `git merge --no-ff
  upstream/main` weekly and **aborts on any conflict** (it skips and retries next
  week). So real resolution happens **locally**:

  ```bash
  git remote get-url upstream || git remote add upstream https://github.com/fleetdm/fleet.git
  git fetch upstream
  git checkout -b sync/upstream-main
  git merge --no-ff upstream/main      # resolve conflicts, then continue
  ```

- **Orientation:** in this merge, **ours = the fork** (`HEAD` / `sync/upstream-main`),
  **theirs = upstream**. In a conflict hunk, `<<<<<<< HEAD` is fork code and
  `>>>>>>> upstream/main` is upstream code.

## Cardinal rule

**Never drop fork logic to make a conflict go away.** The default resolution is
to **keep both sides**: take upstream's change *and* re-apply the fork's edit on
top. Fork edits inside shared files are wrapped in `// OPENFRAME(<slug>)` markers
(see below) — if a conflict removes one, you have lost fork behavior.

## How to find what the fork changed (and why)

1. **In-code markers.** Every fork edit in a shared upstream file is wrapped:

   ```go
   // >>> OPENFRAME(redis-key-prefix): namespace keys per tenant — openframe/docs/redis-key-prefix.md
   ...fork lines...
   // <<< OPENFRAME(redis-key-prefix)
   ```

   List them: `grep -rn "OPENFRAME(" --include='*.go' --include='*.yaml' --include='*.tpl' .`
   Slugs, largest first — `mysql-multitenancy` (~205 markers, the biggest fork
   feature by a wide margin), `host-assignments`, `helm`, `agent-openframe-mode`,
   `redis-key-prefix`, `hardening`, `vuln-persistence`, `query-results-ttl`,
   `waf-inventory-shape`, `agent-json-content-type`, `cloudsql-v2`,
   `redis-seed-nodes`, `migration-race`, `osquery-host-id`, plus
   `migrations-idempotency` (the upstream migrations carry `// Idempotent migration.`).

   > Keep this list and the `for slug in …` loop in
   > [verify.sh](../scripts/verify.sh) in sync with the tree. Both were stale for
   > several releases and the presence detector — whose only job is catching fork
   > code a merge dropped — was blind to `mysql-multitenancy` entirely. Get the
   > authoritative list with:
   >
   > ```bash
   > grep -rho 'OPENFRAME([a-z0-9-]*)' --include='*.go' --include='*.yaml' --include='*.tpl' . | sort | uniq -c | sort -rn
   > ```

2. **The manifest.** [fork-file-manifest.md](fork-file-manifest.md) lists every
   created/modified/deleted path.

3. **The feature docs.** Each has a *Rebase notes* / *Upstream merge notes* section:
   [architecture-host-assignments.md](architecture-host-assignments.md),
   [redis-key-prefix.md](redis-key-prefix.md),
   [query-results-ttl-cleanup.md](query-results-ttl-cleanup.md),
   [agent-openframe-mode.md](agent-openframe-mode.md),
   [migrations.md](migrations.md), [api-expose-osquery-host-id.md](api-expose-osquery-host-id.md),
   [helm-chart.md](helm-chart.md).

## Per-hotspot resolution recipes

| File(s) | Fork content (keep it) | On conflict |
|---------|------------------------|-------------|
| `cmd/fleet/serve.go` | `mysql-multitenancy` `ValidateOpenframeMultitenancy()` boot check + the `EnsureOpenframeTeamID`/`SetOpenframeTeamID` pinned-mode startup | Keep both; they are independent of upstream's additions around them |
| `cmd/fleet/cron_registration.go` | `query-results-ttl` registration, gated on `IsOpenframeMode() && QueryResultsTTL > 0` | Re-place the `deps.register("failed to register query_results_ttl_cleanup schedule", …)` block among upstream's other registrations |
| `cmd/fleet/redis.go` | `redis-key-prefix` `KeyPrefix: cfg.KeyPrefix` in the pool config literal | Keep the field in the literal. **Not** in `serve.go` — earlier revisions of this runbook said so |
| `server/config/config.go` | `KeyPrefix` (RedisConfig) + `QueryResultsTTL`/`QueryResultsCleanupInterval` (ServerConfig) fields, their `addConfig*`/`getConfig*` calls | Re-add the fields and the matching register/read lines if upstream restructures the structs |
| `server/datastore/mysql/policies.go`, `queries.go` | `if fleet.IsOpenframeMode()` hooks inside upstream query builders + the fork `Add/Remove/Replace/List*Hosts` funcs + `loadHostsFor*` | Keep the fork funcs verbatim; re-insert each `IsOpenframeMode()` hook into the (possibly rewritten) upstream query builder |
| `server/datastore/mysql/hosts.go` | `policy_hosts` EXISTS filter under `IsOpenframeMode()` | Re-insert the `AND EXISTS (SELECT 1 FROM policy_hosts …)` clause into the host-policies query |
| `server/datastore/mysql/mysql.go`, `cmd/fleet/prepare.go` | `MigrateOpenframe` method + its call (and the removed early-return in `prepare.go`) | Keep `MigrateOpenframe` running after `MigrateTables`/`MigrateData`; ensure no early `return` skips it |
| `server/fleet/datastore.go`, `service.go` | 8 host-assignment methods + `MigrateOpenframe` + `CleanupExpiredQueryResults` on the interfaces | Re-add the method signatures; then **regenerate mocks** (below) |
| `server/fleet/hosts.go` | `OsqueryHostID` json tag is `json:"osquery_host_id"` (upstream = `json:"-"`) | Keep the fork tag |
| `server/service/handler.go` | routes `POST/DELETE/PUT/GET …/{id}/hosts` | Re-register the 4+4 routes |
| `server/datastore/redis/redis.go` | pool `keyPrefix` fields, `KeyPrefix()` accessors, `normalizeKeyPrefix`, `newPrefixedConn`/`unwrapConn` around `redisc` | **Critical** — see watchlist; verify every pool `Get()` returns `newPrefixedConn(...)` |
| `orbit/cmd/orbit/orbit.go` | 4 `openframe-*` flags, custom osqueryd path, token-refresher startup, `uuid` cmd, osquery flag passthrough, `NewOrbitClient(..., openFrameMode, authManager)` args | Keep all; re-thread the two extra `NewOrbitClient` args at both call sites |
| `client/orbit_client.go` (not `server/service/` — that path does not exist) | `openFrameMode`/`authManager` fields, bearer-header block, `/tools/agent/fleetmdm-server` url prefix, `NewOrbitClient` signature | Keep the two trailing constructor params and the header/prefix logic |
| `charts/fleet/*` | externalized config, `FLEET_OPENFRAME_MODE`, `FLEET_REDIS_KEY_PREFIX`, migration job, waitForMysql, probe split, additionalCAs | The chart is fork-owned — prefer ours, cherry-pick upstream chart improvements deliberately |
| `server/datastore/mysql/{hosts,labels,targets,teams,campaigns,app_configs,query_results}.go`, `server/service/{osquery,orbit,global_policies,team_policies,endpoint_middleware,endpoint_campaigns}.go`, `server/activity/internal/mysql/new_activity.go`, `server/datastore/cached_mysql/cached_mysql.go` | `mysql-multitenancy` tenant fences (`OpenframeTeamID(ctx)` pins, `openframeForeignTeam` early-returns) | The single largest fork surface — 205 markers over ~41 files, and it was absent from this table for several releases. Re-apply each fence onto upstream's new signature; a fence that silently disappears is a cross-tenant leak |
| `server/service/osquery_utils/queries.go` | `waf-inventory-shape`: the `certificates_darwin`/`certificates_windows` detail queries `hex()` their DN columns, decoded by `decodeCertificateDNColumns` | Hex-encode whatever DN columns upstream's ingest now reads — v4.90 switched Windows from `subject`/`issuer` to `subject2`/`issuer2`, so the aliases became `subject2_hex`/`issuer2_hex` and the decode takes a per-platform column list. `queries_openframe_sql_test.go` pins the SQL |
| `go.mod` / `go.sum` | fork adds `github.com/robfig/cron/v3` (token refresher) | Keep the require line on conflict; run `go mod tidy` after |

## Semantic-conflict watchlist (no git conflict — the dangerous ones)

These break fork behavior **without** producing a merge conflict. Check each after every sync:

| Risk | Why it's invisible | Detection |
|------|--------------------|-----------|
| **New upstream migration not idempotent** | A brand-new file in `migrations/tables|data/` is not a conflict, but the fork's invariant is that all migrations are idempotent. v4.90.1 brought 45 of them, none idempotent. | Now caught automatically by `TestOpenframeMigrationsAreIdempotent` (`idempotency_openframe_test.go`, no MySQL/Docker needed) for the three textual rules. An `ALTER … ADD COLUMN` still needs a hand-written `columnExists`/`indexExistsTx` guard, which no regex can judge — so list the new files and read them:<br>`git diff --name-only --diff-filter=A HEAD...upstream/main -- 'server/datastore/mysql/migrations/tables/*' 'server/datastore/mysql/migrations/data/*'`<br>Mind the direction: `HEAD...upstream/main` is upstream-added. The reverse (`upstream/main...HEAD`, as earlier revisions of this table had it) lists *fork*-added files and finds nothing. |
| **Upstream re-timestamps a migration the fork already made idempotent** | Upstream renames e.g. `20260611202649_Add…` → `20260702013055_Add…`. Git reports it as a content conflict on the new path, so it is visible — but the consequence is not: goose keys on the version number, so a migration already applied on every tenant is seen as new and **runs again**. Idempotency is the only thing that makes that safe. v4.90.1 re-timestamped 5. | Take upstream's new file and function names, re-apply the fork's guards and marker. Never resolve one of these by keeping the fork's old timestamp — the file would diverge from upstream forever. Confirm the guard actually covers a second run. |
| **Upstream signature change lands only in test code** | `go build` on non-test packages compiles fine, so a clean build hides it. v4.90.1 changed `ListGlobalPolicies`/`CountPolicies`/`ListTeamPolicies`(+2) and `NewOrbitClient`; three test files (two of them fork-only) stopped compiling. | `go vet ./server/... ./cmd/... ./client/... ./ee/...` — verify.sh's vet step now covers `server/service` and `client` too. |
| **`schema.sql` lost the OpenFrame tables** | `make dump-test-schema` regenerates `schema.sql` from upstream migrations only — it never contains `policy_hosts`/`query_hosts`/`migration_status_openframe`. This is expected. | Don't "fix" it. Fork datastore tests must call `ds.MigrateOpenframe(ctx)` first (see `migrations_openframe_test.go`). |
| **Redis pool refactor un-wires the prefix** | If upstream rewrites pool construction, `newPrefixedConn` may stop wrapping `Get()` — keys silently go unprefixed → **cross-tenant data leak**. The key-prefix unit test still passes (it tests the wrapper, not the wiring). | After any `redis.go` conflict: confirm `standalonePool.Get()` and `clusterPool.Get()` both return `newPrefixedConn(...)`, and `ConfigureDoer`/`EachNode` unwrap/re-wrap. |
| **Query-planner change bypasses the host filter** | If upstream rewrites the policy/query SQL builders, the `IsOpenframeMode()` EXISTS filter may end up on a dead code path. | Re-read `PolicyQueriesForHost` / `ListScheduledQueriesForAgents` and confirm the `policy_hosts`/`query_hosts` filter is still applied. |
| **Orbit/osquery invocation refactor** | Upstream may change how osquery is launched, dropping the `--openframe-*` flags or the custom binary path. | Re-read the `osquery.NewRunner` options and the `openframe-mode` osqueryd-path branch in `orbit.go`. |
| **Upstream ungates an agent feature OpenFrame relies on being off** | Upstream moves a feature out of a flag guard so it runs unconditionally (e.g. the Fleet Desktop device-token check `trw.StartRotation()` was moved out of `if --fleet-desktop` to "keep the identifier valid for refetch-host/device-auth"). OpenFrame agents run **without** those flags, so the feature silently turns on and hits paths that aren't gatewayed → a fleet-wide 401 storm on `/api/.../fleet/device/{token}/ping`. No git conflict; the fork never edited that line. | After an `orbit.go` sync, confirm the device-token check + `trw.StartRotation()` are still wrapped in `if !c.Bool("openframe-mode")` (`OPENFRAME(agent-openframe-mode)`). More generally, watch for upstream removing flag guards around device-token / MDM / Fleet Desktop features. |
| **Upstream adds a team-level cache in front of host-scoped data** | The fork's host targeting (`policy_hosts`/`query_hosts`) makes per-host data that upstream assumes is uniform per team. A cache keyed by team then serves one host's data to another — a cross-host leak inside a tenant. No git conflict: the caching is new upstream code the fork never touched, and the host filter it bypasses still looks correct in place. v4.90 did exactly this with the `getPackConfig` pack-config cache (`svc.packConfigCache`), which is safe upstream only because label targeting is the sole per-host input. | After a sync, grep `server/service/` for new caches keyed by `teamID` and ask whether the cached value depends on `IsOpenframeMode()` host targeting. Gate the cache on `!fleet.IsOpenframeMode()` if so — see `pack_config_cache_openframe_test.go`. |
| **Mocks out of sync** | `datastore_mock.go` / `service_mock.go` are generated; an interface change makes them stale (compile error if lucky, wrong behavior if not). | `make generate-mock` after any change to `server/fleet/datastore.go` or `service.go`. |

## Mandatory post-merge steps

1. **Idempotency sweep** — make every newly added/changed upstream migration idempotent (watchlist row 1).
   `go test ./server/datastore/mysql/migrations/tables/ -run TestOpenframeMigrationsAreIdempotent`
   fails if the textual rules were missed; the Go-guard cases still need reading.
2. **Regenerate mocks** if `server/fleet/datastore.go` or `service.go` changed: `make generate-mock`.
   It is authoritative — if a hand-resolved `datastore_mock.go` is correct, this is a no-op diff,
   which is a useful confirmation that the interface conflict was resolved properly.
3. **`go mod tidy`** if `go.mod` conflicted. Afterwards `diff <(git show upstream/main:go.mod) go.mod`
   should show only the fork's own requires (today: `github.com/robfig/cron/v3`).
4. **Verify**:
   ```bash
   make openframe-verify                 # build + vet + markers + key-prefix tests (no Docker)
   MYSQL_TEST=1 make openframe-verify     # + MySQL-backed fork tests (needs Docker)
   ```
   See [openframe/scripts/verify.sh](../scripts/verify.sh). The fast tier is expected to be
   **all green** — treat any FAIL as a real finding. It was not always able to reach green: the
   build step used to fail on every source checkout (the `full` tag excludes all of
   `server/bindata` unless `make generate` has run), which made a FAIL summary look normal.
   Also confirm the marker counts per slug are unchanged from before the merge, which is a
   stronger signal than mere presence:
   ```bash
   grep -rho 'OPENFRAME([a-z0-9-]*)' --include='*.go' --include='*.yaml' --include='*.tpl' . | sort | uniq -c | sort -rn
   ```
5. (Optional, deepest) run the live E2E against a dev server:
   `FLEET_URL=http://localhost:8080 bash openframe/scripts/test_host_assignments.sh`
   (see [test_host_assignments.md](test_host_assignments.md) and [local-setup.md](local-setup.md)).

## Notes

- **`git blame` is NOT reliable for fork-vs-upstream here.** The squash re-sync
  commit (`01a6bb8a63`, "CI to build Fleet") re-imported whole upstream trees, so
  blame attributes thousands of *upstream* lines to a fork committer. To decide
  whether code is fork or upstream, compare content:
  `git show upstream/main:<file>` (or `git show v4.81.2:<file>`) — if the symbol
  exists there, it's upstream and must **not** be marked. The `OPENFRAME` markers +
  `make openframe-verify`'s coverage check are the source of truth, not blame.
- `.coderabbit.yaml` enforces a review rule that host-scoped SQL keeps its `WHERE`
  filter — directly relevant when re-applying the `IsOpenframeMode()` host filters.
- Optional: `git config rerere.enabled true` locally so repeated resolutions are
  remembered across sync attempts (modest help given the squash-rebase history).
- Test coverage gaps worth filling over time (not blockers): datastore CRUD tests
  for `policy_hosts`/`query_hosts` and a `CleanupExpiredQueryResults` test — both
  require `MYSQL_TEST=1` + Docker and a `ds.MigrateOpenframe(ctx)` call first.
