# API Change: Expose `osquery_host_id` in Host JSON Response

## Change

```go
// server/fleet/hosts.go — Host struct

// Before (upstream Fleet):
OsqueryHostID *string `json:"-" db:"osquery_host_id" csv:"-"`

// After (openframe fork):
OsqueryHostID *string `json:"osquery_host_id" db:"osquery_host_id" csv:"osquery_host_id"`
```

All endpoints returning a `Host` object (`/api/latest/fleet/hosts`, `/api/latest/fleet/hosts/:id`, etc.) now include `osquery_host_id` in the JSON response.

## Problem

The `FleetMdmAgentIdTransformer` in `openframe-client-core` identifies which Fleet host record corresponds to an openframe agent by matching `agentToolId` (the device UUID) against `host.getOsqueryHostId()`.

Upstream Fleet excludes `osquery_host_id` from the JSON response via `json:"-"`. This means the Java model always deserializes `osqueryHostId` as `null`, and the primary matching logic always fails.

The fallback logic (`findFirst()` on hosts with a non-blank `osVersion`) cannot distinguish between duplicate host records (old offline + new online) for the same physical device, leading to incorrect host selection.

## Why upstream hides it

Fleet marks `osquery_host_id` with `json:"-"` as a **minimalism decision**, not a security one. The field contains either a hardware UUID or a hostname — both already available in the API response as `uuid` and `hostname`. The actual secrets (`node_key`, `enroll_secret`) remain hidden.

## Why we expose it

1. **Correct host matching** — `osquery_host_id` is the canonical identifier Fleet uses internally for host enrollment matching (`matchHostDuringEnrollment`). Exposing it lets `FleetMdmAgentIdTransformer` use the same key for host identification.

2. **Duplicate host disambiguation** — when Fleet has two records for the same physical device (common after osquery re-enrollment), `osquery_host_id` distinguishes the active record (UUID-based) from the stale one (hostname-based).

3. **No security impact** — the field duplicates information already present in `uuid`/`hostname`. The sensitive fields (`node_key`, `orbit_node_key`) remain excluded from API responses.

## Affected consumers

- `FleetMdmAgentIdTransformer` (`openframe-client-core`) — primary consumer, uses `osqueryHostId` for host matching during agent registration.

## Rebase notes

On rebase with upstream Fleet, this is a single-line change in `server/fleet/hosts.go`. If upstream modifies the `Host` struct tags, resolve by keeping our `json:"osquery_host_id"` tag.
