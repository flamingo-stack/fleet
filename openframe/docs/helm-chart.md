# Fleet Helm Chart (OpenFrame fork)

## Overview

The fork maintains its own build of the Fleet Helm chart under
[`charts/fleet/`](../../charts/fleet/) and publishes it as an OCI artifact from the
fork's release pipeline (see [ci-cd-release-pipeline.md](ci-cd-release-pipeline.md)).
The chart is adapted for OpenFrame's multi-tenant, GitOps-driven deployment model:
configuration is externalized into ConfigMaps/Secrets, the migration job is
restructured, OpenFrame mode and the per-tenant Redis prefix are wired in.

- Chart name: `fleet`, version `v6.8.4`, appVersion `v4.81.2`
  ([Chart.yaml](../../charts/fleet/Chart.yaml)).
- Subcharts: Bitnami MySQL `9.12.5` (`mysql.enabled`), Bitnami Redis `18.1.6`
  (`redis.enabled`).

> Source commits: `Migrate helm chart to our own Build` (1387fa6f), `Update
> charts` (9ce084f6), `Refactor Fleet Helm chart` (b318417d), `Fix mysql.enabled
> at Pre Sync` (8223125e), `Enable helm hooks for fleet-migration job` (f3e636d5),
> `Remove Chart Pre Hooks` (ba488747), `Remove chart ttlSecondsAfterFinished`
> (7178785e), `per-tenant Redis key prefix ... change in configmap` (719eab41).

## OpenFrame mode flag

[`values.yaml`](../../charts/fleet/values.yaml) exposes `fleet.setup.openframeMode`
(default `"0"`), rendered into the deployment as the server-side feature flag
([deployment.yaml](../../charts/fleet/templates/deployment.yaml)):

```yaml
- name: FLEET_OPENFRAME_MODE
  value: {{ .Values.fleet.setup.openframeMode | quote }}
```

Set it to `"1"` for tenant deployments. This is the switch behind host
assignments and the query-results TTL cleanup
(see [architecture-host-assignments.md](architecture-host-assignments.md),
[query-results-ttl-cleanup.md](query-results-ttl-cleanup.md)).

## MySQL-multitenancy feature envs (`mysql-multitenancy`)

`values.yaml` exposes a `fleet.openframe.multiTenancy` block — the chart-side wiring of the
platform property `openframe.fleet.multi-tenancy.enabled` (see
[process-team-pin.md](process-team-pin.md) for the flag/mode semantics):

```yaml
fleet:
  openframe:
    multiTenancy:
      enabled: false          # → FLEET_OPENFRAME_MULTI_TENANCY_ENABLED (deployment + migration Job)
      tenantUuid: ""          # pinned mode: static FLEET_OPENFRAME_TENANT_UUID
      existingConfigMap: ""   # pinned mode: read the UUID from a ConfigMap instead (wins over tenantUuid)
      tenantUuidKey: ""       # key within existingConfigMap (default FLEET_OPENFRAME_TENANT_UUID)
      teamId: ""              # escape hatch: direct FLEET_OPENFRAME_TEAM_ID pin (prefer tenantUuid)
```

Rendered env vars ([deployment.yaml](../../charts/fleet/templates/deployment.yaml)):
`FLEET_OPENFRAME_MULTI_TENANCY_ENABLED` is always emitted (`"false"` by default — pre-feature
fork behavior). `FLEET_OPENFRAME_TENANT_UUID` is **always read via `configMapKeyRef`** — there is
no inline value or branching in the Deployment (same pattern as the DB/cache config) — from one of:
- `existingConfigMap` set → the operator's own ConfigMap;
- `existingConfigMap` unset → the chart-managed **`fleet-openframe-tenant`** ConfigMap that
  [configmap.yaml](../../charts/fleet/templates/configmap.yaml) creates, holding `tenantUuid`.

In shared per-request mode (`enabled: true`, no `tenantUuid`) and flag-off, the value is empty and
Fleet treats `""` as unset — so no pin. The **flag is also emitted into
[job-migration.yaml](../../charts/fleet/templates/job-migration.yaml)** so `fleet prepare db`
takes the `GET_LOCK` serialization on a shared MySQL.

Downstream (openframe-saas-tenant) pinned-mode wiring can reuse the existing per-namespace
`tenant` ConfigMap, whose `TENANT_ID` key already holds the tenant UUID (it is the same key the
Redis prefix reads):

```yaml
fleetmdm:
  fleet:
    openframe:
      multiTenancy:
        enabled: true
        existingConfigMap: "tenant"
        tenantUuidKey: "TENANT_ID"
```

## Externalized configuration (ConfigMaps + Secrets)

Where upstream takes Redis/MySQL connection details as plain Helm values, the fork
adds [`templates/configmap.yaml`](../../charts/fleet/templates/configmap.yaml) and
[`templates/secret.yaml`](../../charts/fleet/templates/secret.yaml) and lets an
operator point the chart at **externally managed** ConfigMaps/Secrets. This is the
GitOps-friendly pattern: the tenant's control plane owns the ConfigMaps, the chart
just references them.

| Concern | values.yaml block | `existingConfigMap` / `existingSecret` override | Generated object (when no override) |
|---------|-------------------|--------------------------------------------------|-------------------------------------|
| Database | `database.*` | `database.existingConfigMap`, `database.existingSecret` | `fleet-database` ConfigMap (host/port/db/user) + Secret (password) |

`database.existingSecret` covers **both** MySQL secrets: the password (`database.passwordKey`) is read
from it as an env var, and — when `database.tls.enabled` — the same Secret is mounted at
`/secrets/mysql`, where `database.tls.caCertKey` names the server CA file. It therefore takes
precedence over the legacy `database.secretName`, which still applies when `existingSecret` is unset.
The mount is whole-Secret (no `items:` projection), so every key in it surfaces as a file in the Fleet
container; keep unrelated material out of that Secret if that matters to you.
| Cache (Redis) | `cache.*` | `cache.existingConfigMap` | `fleet-cache` ConfigMap (address, key prefix) |
| Tenant UUID (multi-tenancy) | `fleet.openframe.multiTenancy.*` | `fleet.openframe.multiTenancy.existingConfigMap` | `fleet-openframe-tenant` ConfigMap (`FLEET_OPENFRAME_TENANT_UUID` = `tenantUuid`, empty in shared mode) |
| Admin setup | `fleet.setup.*` | `fleet.setup.adminPassword.existingSecret` | `fleet-setup` Secret (`FLEET_SETUP_ADMIN_PASSWORD`) |

Keys within the referenced ConfigMap are themselves configurable
(`database.hostKey`, `database.portKey`, `cache.addressKey`, …), so the chart can
adapt to whatever key names the tenant control plane already uses.

## Per-tenant Redis key prefix

`cache.keyPrefixKey` wires the [Redis key prefix](redis-key-prefix.md) into both
the main deployment and the vuln-processing cron. When non-empty, the chart
renders:

```yaml
{{- if .Values.cache.keyPrefixKey }}
- name: FLEET_REDIS_KEY_PREFIX
  valueFrom:
    configMapKeyRef:
      name: {{ default "fleet-cache" .Values.cache.existingConfigMap }}
      key: {{ .Values.cache.keyPrefixKey }}
{{- end }}
```

It reads from the cache ConfigMap, so the prefix (typically the tenant ID) is
managed alongside the Redis address. See
[redis-key-prefix.md](redis-key-prefix.md) for what the prefix does in the server.

## Migration job

[`templates/job-migration.yaml`](../../charts/fleet/templates/job-migration.yaml)
runs `/usr/bin/fleet prepare db --no-prompt` (which executes all three migration
pipelines, including the OpenFrame one — see [migrations.md](migrations.md)).

Current behavior:

- **Gated by `fleet.autoApplySQLMigrations`** (default `true`). When false, no
  migration Job is rendered (the control plane applies migrations itself).
- **`waitForMysql` init container** (`fleet.waitForMysql.enabled`, image
  `fleet.waitForMysql.image`) — a `nc`-based wait loop that blocks until the MySQL
  host/port (read from the database ConfigMap) is reachable, so the job does not
  race the database.
- **No Helm hooks** and **no `ttlSecondsAfterFinished`** — the job runs as an
  ordinary Job and idempotent migrations make re-runs safe.

### Why the migration job evolved (commit history)

The hook strategy was deliberately walked back:

1. `Enable helm hooks for fleet-migration job` (f3e636d5) added
   `pre-install,pre-upgrade` hooks (with `hook-weight` / `hook-delete-policy`),
   gated on external MySQL (`not .Values.mysql.enabled`).
2. `Fix mysql.enabled at Pre Sync` (8223125e) corrected that gating.
3. `Remove Chart Pre Hooks` (ba488747) **removed the hooks entirely** in favor of
   the `waitForMysql` init container — hooks ran outside the normal release
   ordering and were harder to reason about than an explicit readiness wait.
4. `Remove chart ttlSecondsAfterFinished` (7178785e) dropped the Job's
   `ttlSecondsAfterFinished: 100` so cluster-level TTL/GC policy governs cleanup.

This walk-back depends on the [migration idempotency](migrations.md) work: because
re-running `prepare db` is a no-op, the job no longer needs hook-managed
exactly-once semantics.

## Other fork additions

| Feature | values.yaml | Notes |
|---------|-------------|-------|
| Deployment annotations | `deploymentAnnotations` | Arbitrary annotations merged onto the Fleet Deployment metadata (e.g. for ArgoCD / reloader). |
| Additional CA certs | `fleet.additionalCAs.*` | Init-container injection of CA bundles from named ConfigMaps/Secrets, for private PKI. |
| Dedicated vuln processing | `vulnProcessing.dedicated`, `vulnProcessing.schedule` | When `true`, runs vulnerability processing as a separate CronJob ([vulnprocessing/cronjob.yaml](../../charts/fleet/templates/vulnprocessing/cronjob.yaml)) and disables it in the main deployment. |
| Vuln feed persistence | `vulnProcessing.persistence.*`, `vulnProcessing.staggerSchedule` | See [below](#vulnerability-feed-persistence-vuln-persistence). |
| Probe split | `fleet.probes.*` | See [below](#probes). |

## Probes

Upstream puts both liveness and readiness on `/healthz`. That endpoint checks MySQL
and Redis (`healthCheckers` in [cmd/fleet/serve.go](../../cmd/fleet/serve.go)), and
that breaks in two ways:

- **Slow start.** Fleet waits for the database on its own, about 105s: 15 attempts
  sleeping 0,1,2,…,14 seconds ([common.go](../../server/platform/mysql/common.go),
  count from `defaultMaxAttempts` in
  [config.go](../../server/datastore/mysql/config.go)). While it waits it isn't
  listening on `listenPort` yet, so the default liveness kills it after 30s and it
  never gets to finish waiting. Happens every time MySQL and Fleet come up together
- **Redis blip.** One Redis outage makes `/healthz` return 500 in every Fleet pod at
  once. Restarting them doesn't bring Redis back, it just piles a restart storm on
  top of the outage

So only readiness looks at the dependencies:

| Probe | Path | Why |
|-------|------|-----|
| `startupProbe` | `/version` | An answer on `/version` already means the boot finished. Fleet reaches `srv.ListenAndServe` long after `initDatastore` and `evalMigrationStatus`, so nothing serves before MySQL is in |
| `livenessProbe` | `/version` | Restart only if the listener stops answering. [version.go](../../server/version/version.go) returns a static struct, no auth, no MySQL, no Redis |
| `readinessProbe` | `/healthz` | A pod that can't reach its deps drops out of the Service instead of dying |

That liveness is deliberately narrow. A Fleet that still answers on `/version` but is
wedged on an exhausted connection pool will not be restarted, because a restart is not
what fixes that. Readiness is what takes it out of rotation.

### What moving liveness off `/healthz` gives up

The MySQL checker is not a ping. `Datastore.HealthCheck`
([mysql.go](../../server/datastore/mysql/mysql.go)) runs `SELECT @@read_only` and
returns an error when the answer is 1, with the comment saying so outright: fail the
endpoint so the orchestrator restarts Fleet with fresh DB connections. Upstream added
it for AWS Aurora, where a failover demotes the old writer to a reader behind the same
endpoint. So upstream's liveness on `/healthz` was not only an oversight, it was also a
self-heal after a database failover, and `connMaxLifetime: 0` means the pool never
recycles those connections on its own.

We give that up knowingly, because neither database we run gets into the state it
repairs.

Tenant Fleet talks to a single MySQL StatefulSet in its own namespace,
`fleetmdm-mysql-0.fleetmdm-mysql.<namespace>.svc.cluster.local` out of the
`fleetmdm-mysql` ConfigMap. No replica, no reader endpoint, nothing that promotes or
demotes. When that MySQL goes away the connections break with socket errors and
`database/sql` opens new ones. `@@read_only` only turns 1 if somebody sets it by hand.

The chart can also be pointed at Cloud SQL, which is what the platform-level Fleet app
does. A Cloud SQL HA failover doesn't leave a demoted writer behind a stable endpoint
either: the standby serves on the same shared static IP, the old primary is destroyed
and recreated as the new standby, and open connections are closed rather than turned
read-only ([Cloud SQL HA](https://cloud.google.com/sql/docs/mysql/high-availability)).

That failover takes about 60 seconds, and 60 seconds fits inside the 2.5 min readiness
budget. So a Cloud SQL failover doesn't even push the pod out of the Service, and
liveness on `/version` leaves it alone while `database/sql` reconnects.

If Fleet ever moves onto a database that can demote a writer behind a stable endpoint,
put this back. `health.Handler` ([health.go](../../server/health/health.go)) supports a
`?check=<name>` filter, so a narrow `/healthz?check=mysql` liveness is available without
dragging Redis back into the restart decision.

The startup probe is close to a duplicate of liveness, both on `/version`, and liveness
alone already covers the 105s. It earns its place with two things: a wider window for a
stuck boot, 6 min against 2.5, and keeping readiness quiet until the process is up.

Putting `/healthz` on startup would have kept the same bug at boot: with Redis down,
a pod that gets rescheduled mid outage never starts, gets killed and lands in
CrashLoopBackOff. Tenants run one replica, so that is full downtime that outlives the
Redis outage by the backoff. On `/version` the pod comes up, stays out of the Service
and joins it the moment Redis is back, with no restart.

Numbers live under `fleet.probes.{startup,liveness,readiness}`. The first probe runs at
t=0, so the Nth failure lands at `(N - 1) × periodSeconds`: startup gives up at 6 min,
liveness and readiness at 2.5 min.

The startup number has a floor and no real ceiling. The floor is the ~105s Fleet spends
retrying MySQL with the port still closed: go under it and the probe cuts a legitimate
wait short, which is the original bug with liveness playing that role at 30s. Above the
floor it only catches a hang, because a MySQL that never answers ends the process
anyway: `initFatal` inside `initDatastore`, or `evalMigrationStatus` reporting an
unapplied migration and `serve` exiting on it (both in
[cmd/fleet/datastore.go](../../cmd/fleet/datastore.go), called from
[cmd/fleet/serve.go](../../cmd/fleet/serve.go)).
So 6 min reads as "how long we tolerate a stuck boot", not "how long we wait for the
database". That wait is Fleet's own and a probe can only cut it short, never extend it.

Readiness holds a broken pod in the Service for 2.5 min, which is deliberate. On one
replica dropping it earlier changes nothing, the tenant is down regardless. Lower it
if you ever run Fleet with more than one replica per tenant.

`timeoutSeconds` is 10s on all three. The point is getting away from the 1s default:
readiness needs it because `/healthz` does real MySQL and Redis round trips, and even
`/version` can miss a 1s deadline on a node still busy starting everything else. One
number everywhere keeps it readable. It caps a single attempt and sits inside the
period, it isn't added on top, so keep it under `periodSeconds` or the timeout becomes
the real cadence

## Vulnerability feed persistence (`vuln-persistence`)

Fleet downloads ~800MB of vulnerability feeds (NVD, CPE, OSV, OVAL, MSRC, …) into
`FLEET_VULNERABILITIES_DATABASES_PATH` (`/tmp/vuln`). Upstream mounts `/tmp` as an
`emptyDir`, so every pod replacement re-downloads the full set, even though Fleet's
sync code is delta-aware (per-file mtime / SHA-256 / `.meta` comparisons that skip
unchanged artifacts when the directory survives a restart).

The fork adds first-class persistence for the **dedicated cron** mode:

```yaml
vulnProcessing:
  dedicated: true        # required — persistence only wires into the CronJob
  staggerSchedule: true  # hourly at a minute derived from the release namespace
  persistence:
    enabled: true
    size: 5Gi
    # storageClassName: ""   # cluster default
    # existingClaim: ""      # reuse a pre-created PVC
```

What this renders:

- [vulnprocessing/pvc.yaml](../../charts/fleet/templates/vulnprocessing/pvc.yaml)
  — a `ReadWriteOnce` PVC `fleet-vulnprocessing` (skipped when `existingClaim` is
  set). RWO is safe because the CronJob runs with `concurrencyPolicy: Forbid`, so
  at most one pod attaches it at a time.
- [vulnprocessing/bind-job.yaml](../../charts/fleet/templates/vulnprocessing/bind-job.yaml)
  — a one-shot Job (`persistence.bindJob.enabled`, default on) that mounts the
  claim at install, using `bindJob.image` (default `busybox:1.36`, like
  `fleet.waitForMysql` — any image that can exit 0 works). With a
  `WaitForFirstConsumer` storage class the claim
  otherwise stays Pending until the first cron tick — up to an hour during which
  an Argo CD sync operation sits in `Running` on
  "waiting for healthy state of /PersistentVolumeClaim/fleet-vulnprocessing"
  (`ignore-healthcheck` only fixes aggregated app health, not operation gating —
  argo-cd issue #22940). Argo CD users should also set
  `bindJob.annotations: {argocd.argoproj.io/sync-options: Replace=true,Force=true}`
  so later image-tag changes don't hit the Job-spec-immutable apply error. The
  Job must stay a plain Sync-phase resource — PostSync would deadlock on the very
  PVC health it unblocks, PreSync runs before the claim exists.
- The CronJob mounts the PVC at `/tmp/vuln` (nested inside the `/tmp` emptyDir)
  and gets a pod-level `securityContext` from `fleet.podSecurityContext`, whose
  `fsGroup` (+ `fsGroupChangePolicy: OnRootMismatch`) is what lets it write —
  without it uid 3333 cannot write a fresh root-owned PVC.
- With `staggerSchedule: true`, `vulnProcessing.schedule` is ignored and the cron
  fires hourly at `adler32sum(namespace) mod 60`, so tenants sharing a cluster
  don't all hit the feed mirrors at `:00`.

Combined effect: server pods never download feeds (redeploys are free), and each
hourly job only fetches deltas (EPSS/CISA and, when Amazon Linux hosts exist,
goval-dictionary sqlite files are always re-fetched — they are the small part).
Do **not** enable `dedicated` without `persistence` on a busy schedule: each job
pod would start with an empty `/tmp` and re-download the full set every run.

Caveat: `cpe.sqlite` is versionless on disk (schema lives in the `cpe_2` table
name) and the sync skips it when its mtime is "today", so after a Fleet upgrade a
persisted copy can lag by up to a day. Deleting the PVC (the data is a disposable
cache) forces a full re-sync.

Server-side guard (`server/vulnerabilities/nvd/cpe.go`): when a feed sync fails
but the scan continues, the CPE translation phase opens the missing
`cpe.sqlite` with the sqlite driver, which creates an **empty 0-byte file**. On
an emptyDir that artifact dies with the pod, but on a persisted volume its
fresh mtime satisfies both freshness gates in `DownloadCPEDBFromGithub` and the
real download is never retried — vuln processing wedges until the next
`fleetdm/nvd` release (~24h) while the Job reports Success. The fork adds a
`stat.Size() == 0 → treat as absent` case ahead of the mtime gate, so the next
run after a transient failure (rate limit, GitHub outage) re-downloads
immediately. The write path is temp-file + atomic rename, so replacing the
empty file is safe. A non-empty corrupt file would still pass, but that cannot
be produced by this code path — only by disk corruption.

## Operator quick reference

Tenant-style install (external DB + shared Redis + OpenFrame mode):

```bash
helm upgrade --install fleet oci://ghcr.io/flamingo-stack/fleetmdm/helm-charts/fleet \
  --set fleet.setup.openframeMode="1" \
  --set database.existingConfigMap=fleet-db-config \
  --set database.existingSecret=fleet-db-secret \
  --set cache.existingConfigMap=fleet-cache \
  --set cache.keyPrefixKey=FLEET_REDIS_KEY_PREFIX \
  --set fleet.autoApplySQLMigrations=true \
  --set fleet.waitForMysql.enabled=true
```

> The OCI chart coordinates above match the release pipeline in
> [ci-cd-release-pipeline.md](ci-cd-release-pipeline.md); confirm the exact
> registry path for your environment.

## Files changed

| File | Purpose |
|------|---------|
| `charts/fleet/values.yaml` | OpenFrame mode, externalized DB/cache/setup config, `cache.keyPrefixKey`, `waitForMysql`, `probes`, `additionalCAs`, `vulnProcessing`, `deploymentAnnotations` |
| `charts/fleet/templates/configmap.yaml` | **New** — generated DB/cache ConfigMaps |
| `charts/fleet/templates/secret.yaml` | **New** — generated DB password / admin-setup Secrets |
| `charts/fleet/templates/deployment.yaml` | `FLEET_OPENFRAME_MODE`, `FLEET_OPENFRAME_MULTI_TENANCY_ENABLED` / `FLEET_OPENFRAME_TENANT_UUID` / `FLEET_OPENFRAME_TEAM_ID`, `FLEET_REDIS_KEY_PREFIX`, ConfigMap/Secret refs, annotations, CA init container, probe split |
| `charts/fleet/templates/job-migration.yaml` | `waitForMysql` init container, hook removal, TTL removal |
| `charts/fleet/templates/vulnprocessing/cronjob.yaml` | Dedicated vuln-processing cron + `FLEET_REDIS_KEY_PREFIX`, feed-cache PVC mount, fsGroup, schedule stagger (moved from `templates/cron-vulnprocessing.yaml`) |
| `charts/fleet/templates/vulnprocessing/pvc.yaml` | **New** — PVC persisting the vulnerability feed cache across cron runs |
| `charts/fleet/templates/vulnprocessing/bind-job.yaml` | **New** — one-shot Job binding the WFFC feed PVC at install |
| `charts/fleet/Chart.yaml` | Fork chart version, MySQL/Redis subchart pins |

## Rebase notes

The chart is largely fork-owned, so upstream chart changes rarely apply cleanly —
treat the fork chart as the source of truth and cherry-pick upstream improvements
deliberately. The `charts/fleet/README.md` is still upstream-oriented and does not
yet describe the externalized-config / multi-tenant patterns above.
