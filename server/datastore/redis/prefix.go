package redis

import (
	"fmt"
	"strings"

	"github.com/fleetdm/fleet/v4/server/fleet"
)

// PrefixKey returns key with the pool's configured key prefix prepended.
// If the pool has no prefix configured (the default), it returns key
// unchanged. This is the standard helper for any Redis key built by Fleet
// subsystems — it guarantees that a shared Redis can be safely namespaced
// per service or tenant via the redis.key_prefix config option.
//
// For keys that contain Redis Cluster hash tags (segments wrapped in
// curly braces, e.g. "livequery:{42}"), use PrefixHashTagKey instead so
// that the prefix is placed *outside* the hash-tag braces and the cluster
// slot routing for related keys is preserved.
func PrefixKey(pool fleet.RedisPool, key string) string {
	return pool.KeyPrefix() + key
}

// PrefixHashTagKey builds a Redis key of the form
//
//	<pool prefix><before>{<tagged>}<after>
//
// guaranteeing that the configured pool key-prefix lands *before* the
// hash-tag braces. Hash tags determine which slot a key maps to in Redis
// Cluster, so two keys that need to share a slot (and therefore live on
// the same node — required for multi-key commands, transactions, and Lua
// scripts) must share the substring inside the braces. Putting the
// per-tenant pool prefix outside the braces preserves that invariant: keys
// that were co-located before remain co-located afterwards.
//
// Example:
//
//	PrefixHashTagKey(pool, "livequery:", "42", "")    => "fleet:t1:livequery:{42}"
//	PrefixHashTagKey(pool, "sql:livequery:", "42", "") => "fleet:t1:sql:livequery:{42}"
//
// Both keys above hash to the same slot regardless of the pool prefix.
func PrefixHashTagKey(pool fleet.RedisPool, before, tagged, after string) string {
	return pool.KeyPrefix() + before + "{" + tagged + "}" + after
}

// StripPrefix removes the pool's configured key prefix from the front of
// key, if present. It is intended for callers that read raw keys back out
// of Redis (for example via SCAN) and need to recover the original
// subsystem-level key name. If the pool has no prefix configured or the
// key does not start with the prefix, key is returned unchanged.
func StripPrefix(pool fleet.RedisPool, key string) string {
	prefix := pool.KeyPrefix()
	if prefix == "" {
		return key
	}
	return strings.TrimPrefix(key, prefix)
}

// PrefixSprintf is the Sprintf-style sibling of PrefixKey: it formats
// `format` with `args` and prepends the pool's configured key prefix to
// the result. It is the preferred helper for keys whose body contains a
// hash tag built via fmt.Sprintf — e.g. "policy_pass:{%d}", hostID — so
// that all subsystems use a single, uniform construction style.
//
// The same hash-tag positioning rules as PrefixHashTagKey apply: the
// pool prefix lands BEFORE any "{...}" segment in `format`, preserving
// Redis Cluster slot co-location invariants.
//
// Example:
//
//	PrefixSprintf(pool, "policy_pass:{%d}", 42)
//	    => "fleet:t1:policy_pass:{42}"
func PrefixSprintf(pool fleet.RedisPool, format string, args ...any) string {
	return pool.KeyPrefix() + fmt.Sprintf(format, args...)
}

// ScanPrefixedKeys is like ScanKeys but automatically prepends the pool's
// configured key prefix to the supplied pattern. Use this for any
// administrative scan (cleanup, debug listings, error-store enumeration)
// so that the scan is automatically scoped to the current pool's
// namespace. When the pool has no prefix configured, this is equivalent
// to calling ScanKeys directly.
func ScanPrefixedKeys(pool fleet.RedisPool, pattern string, count int) ([]string, error) {
	return ScanKeys(pool, pool.KeyPrefix()+pattern, count)
}
