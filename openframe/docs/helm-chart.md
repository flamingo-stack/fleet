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
| Cache (Redis) | `cache.*` | `cache.existingConfigMap` | `fleet-cache` ConfigMap (address, key prefix) |
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
  and gets a pod-level `securityContext` with `fsGroup` =
  `fleet.securityContext.runAsGroup` (+ `fsGroupChangePolicy: OnRootMismatch`) —
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
| `charts/fleet/values.yaml` | OpenFrame mode, externalized DB/cache/setup config, `cache.keyPrefixKey`, `waitForMysql`, `additionalCAs`, `vulnProcessing`, `deploymentAnnotations` |
| `charts/fleet/templates/configmap.yaml` | **New** — generated DB/cache ConfigMaps |
| `charts/fleet/templates/secret.yaml` | **New** — generated DB password / admin-setup Secrets |
| `charts/fleet/templates/deployment.yaml` | `FLEET_OPENFRAME_MODE`, `FLEET_REDIS_KEY_PREFIX`, ConfigMap/Secret refs, annotations, CA init container |
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
