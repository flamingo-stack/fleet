# Per-Tenant Redis Key Prefix

## Overview

Upstream Fleet assumes it owns its Redis instance — keys and pub/sub channels are
written with bare names. In an OpenFrame multi-tenant deployment several Fleet
servers (one per tenant) may share a single Redis cluster, so their keyspaces
must not collide.

This fork adds a **key prefix** that is transparently prepended to every Redis
key and pub/sub channel. Each tenant configures a unique prefix (typically the
tenant ID), giving each tenant an isolated slice of a shared Redis.

> Source commit: `per-tenant Redis key prefix in Fleet configuration and change
> in configmap` (719eab41fc).

## Configuration

| Env var | YAML key | Default | Description |
|---------|----------|---------|-------------|
| `FLEET_REDIS_KEY_PREFIX` | `redis.key_prefix` | `""` (disabled) | String prepended to every Redis key/channel. Set to the tenant ID when sharing one Redis across tenants. |

The field is registered in
[`server/config/config.go`](../../server/config/config.go) as
`RedisConfig.KeyPrefix` and threaded into the Redis pool configuration in
[`cmd/fleet/serve.go`](../../cmd/fleet/serve.go). When empty, the wrapper is never
installed and behavior is byte-for-byte identical to upstream Fleet.

### Normalization

`normalizeKeyPrefix` in
[`server/datastore/redis/redis.go`](../../server/datastore/redis/redis.go):

- **Appends a `:` separator** if the configured prefix does not already end in
  one — so `tenant1` becomes `tenant1:` and keys look like `tenant1:hosts:123`.
- **Rejects `{` and `}`** characters. Those are Redis Cluster hash-tag delimiters;
  allowing them in a prefix would let the prefix change which hash slot a key
  lands in and break cluster routing. An invalid prefix fails startup.

## How it works

The mechanism is a thin connection decorator,
[`server/datastore/redis/keyprefix.go`](../../server/datastore/redis/keyprefix.go),
that wraps each pooled connection:

```
pool.Get()  ──▶  newPrefixedConn(rawConn, prefix)  ──▶  prefixedConn
                                                          │
   Do(cmd, args...) / Send(cmd, args...)                  │
        └── prefixArgs(cmd, args, prefix) rewrites key args before forwarding ──┘
```

- Both pool implementations (`standalonePool`, `clusterPool`) carry the prefix and
  return a `prefixedConn` from `Get()`. A `KeyPrefix()` accessor lets helper code
  read it back via `keyPrefixOf(pool)`.
- `prefixedConn` implements the full `redigo` connection surface used by Fleet —
  `Do`, `Send`, `DoWithTimeout`, `ReceiveWithTimeout`, plus `Bind` and `ReadOnly`
  for cluster support — so it is a drop-in replacement.
- `redisc.RetryConn` rejects unknown connection wrappers, so the code
  `unwrapConn`s before handing a connection to redisc and re-wraps afterward.

### Command policy (fail-closed)

`prefixArgs` decides which arguments of a command are keys/channels. The policy is
**fail-closed**: any command not explicitly listed has its **first argument**
prefixed, which is correct for the vast majority of Redis commands and safe for
unknown/future ones.

| Class | Behavior | Examples |
|-------|----------|----------|
| `noPrefixCmds` | No argument is a key — prefix nothing. | `PING`, `AUTH`, `HELLO`, `SELECT`, `INFO`, `CLIENT`, `CLUSTER`, `CONFIG`, `SCRIPT`, `MULTI`, `EXEC`, … |
| `specialCmds` | Keys are not at `arg[0]` — apply a custom rule (below). | `DEL`, `MGET`, `RENAME`, `EVAL`, `SCAN`, `SUBSCRIBE`, `BITOP`, `OBJECT`, `SORT`, `BLPOP`, … |
| default | `arg[0]` is the key. | `GET`, `SET`, `HSET`, `EXPIRE`, `INCR`, … and any command not otherwise listed |

The `specialCmds` rules in `keyprefix.go`:

| Rule | Applies to | What it prefixes |
|------|-----------|------------------|
| `allArgs` | `DEL`, `UNLINK`, `MGET`, `EXISTS`, `TOUCH`, `WATCH`, `*STORE` set/zset ops, `PFCOUNT`/`PFMERGE`, and all pub/sub channel commands | every argument |
| `twoKeys` | `RENAME`, `RENAMENX`, `SMOVE`, `COPY`, `LMOVE`, `RPOPLPUSH`, `LCS`, `ZRANGESTORE`, `GEOSEARCHSTORE`, … | `arg[0]` and `arg[1]` |
| `bitopArgs` | `BITOP` | `arg[1..]` (dest + sources; `arg[0]` is the operation) |
| `objectArgs` | `OBJECT` | the key at `arg[1]` for `ENCODING`/`IDLETIME`/`FREQ`/`REFCOUNT` |
| `evalArgs` | `EVAL`, `EVALSHA`, `FCALL`, `FCALL_RO` | the `numKeys` keys starting at `arg[2]` |
| `scanArgs` | `SCAN` | the `MATCH` pattern (see caveat) |
| `sortArgs` | `SORT` | the source key and the `STORE` destination |
| `pubsubArgs` | `PUBSUB` | channels for `NUMSUB`/`CHANNELS`/`SHARD*` |
| `blockingKeysArgs` | `BLPOP`, `BRPOP`, `BZPOPMIN`, `BZPOPMAX` | all keys before the trailing timeout |

Because pub/sub channels are prefixed alongside keys, tenants sharing a Redis are
also isolated at the pub/sub layer (live-query results, etc.).

## Caveats

- **`SCAN` requires a `MATCH` pattern.** `scanArgs` only prefixes the `MATCH`
  argument; a `SCAN` with no `MATCH` would iterate **every tenant's** keys.
  Fleet's own `ScanKeys` always supplies `MATCH`, so internal usage is safe — but
  any new `SCAN` call site must pass `MATCH`.
- **The prefix must be stable for the life of a tenant.** Changing it orphans all
  previously written keys (locks, caches, live-query state) under the old prefix.
- **`BY`/`GET` patterns of `SORT` are left unprefixed** — Fleet does not use them.
  Revisit `sortArgs` if that changes.
- The prefix applies to **everything** Fleet stores in Redis: distributed
  scheduler locks, the live-query store, caches, and pub/sub — so isolation is
  complete, not partial.

## Helm / deployment wiring

The chart surfaces the prefix through the cache config
(see [helm-chart.md](helm-chart.md)):

| values.yaml | Effect |
|-------------|--------|
| `cache.keyPrefixKey: "<configmap-key>"` | When non-empty, the chart renders a `FLEET_REDIS_KEY_PREFIX` env var on the Fleet deployment **and** the vuln-processing cron, sourced from `cache.existingConfigMap[keyPrefixKey]`. |

Both the main deployment
([deployment.yaml](../../charts/fleet/templates/deployment.yaml)) and the
dedicated vuln-processing cron
([cron-vulnprocessing.yaml](../../charts/fleet/templates/cron-vulnprocessing.yaml))
receive the same prefix, so background jobs share the tenant's keyspace.

## Cluster seed nodes — `OPENFRAME(redis-seed-nodes)`

Upstream initializes the `redisc.Cluster` with a single startup node
(`StartupNodes: []string{conf.Server}`) and relies on `CLUSTER SLOTS` discovery.
The fork's multi-tenant deploys instead pass an explicit, comma-separated node
list in `FLEET_REDIS_ADDRESS` (e.g. `redis-0:6379,redis-1:6379,redis-2:6379`).
`splitSeedNodes()` in [`redis.go`](../../server/datastore/redis/redis.go) parses
that list (trimming whitespace and a `redis://` scheme per entry) into
`StartupNodes`.

**Without it, startup fails** with `redisc: all nodes failed … dial tcp: address
<list>: too many colons in address`, because the whole comma-joined string is
treated as one host:port. This edit was dropped once during an upstream sync
(it had no marker then); it is now marked and covered by `TestSplitSeedNodes` in
[`seednodes_test.go`](../../server/datastore/redis/seednodes_test.go) and the
`redis-seed-nodes` slug in `openframe/scripts/verify.sh`.

## Files changed

| File | Purpose |
|------|---------|
| `server/datastore/redis/keyprefix.go` | New `prefixedConn` wrapper + per-command key-prefix policy |
| `server/datastore/redis/redis.go` | Pool wiring, `normalizeKeyPrefix`, `KeyPrefix()` accessors, redisc unwrap/rewrap |
| `server/config/config.go` | `RedisConfig.KeyPrefix` field + `redis.key_prefix` registration |
| `cmd/fleet/serve.go` | Passes the configured prefix into the Redis pool |
| `charts/fleet/values.yaml` | `cache.keyPrefixKey` knob |
| `charts/fleet/templates/deployment.yaml` | `FLEET_REDIS_KEY_PREFIX` env var |
| `charts/fleet/templates/cron-vulnprocessing.yaml` | `FLEET_REDIS_KEY_PREFIX` env var |

## Rebase notes

- `keyprefix.go` is net-new; conflicts are unlikely.
- `redis.go` adds the prefix to pool construction and `Get()`. If upstream
  refactors the pool types, re-thread `keyPrefix` through the new constructors and
  keep the `unwrapConn`/`newPrefixedConn` calls around the `redisc` paths.
- `config.go` adds one field and one `addConfigString` call — re-add on conflict.
