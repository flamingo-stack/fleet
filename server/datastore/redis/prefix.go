package redis

import (
	"fmt"
	"strings"

	"github.com/fleetdm/fleet/v4/server/fleet"
)

// KeyBuilder is a small value type that owns a single string prefix and
// produces fully-qualified Redis keys/channels for a Fleet subsystem.
//
// It is intentionally decoupled from fleet.RedisPool: a KeyBuilder only
// needs the prefix string, not the connection pool. Production code in
// cmd/fleet/serve.go builds one from config.Redis.KeyPrefix and threads
// it into every subsystem constructor so all subsystems share the same
// namespace; tests pass NewKeyBuilder("") for upstream-identical keys.
//
// All methods are safe for concurrent use (the value is immutable after
// construction). An empty prefix produces upstream-Fleet-identical keys —
// callers do not need to special-case the "no prefix" deployment.
type KeyBuilder struct {
	prefix string
}

// NewKeyBuilder returns a KeyBuilder that prepends prefix to every key it
// produces. Pass "" to disable prefixing entirely (matches upstream Fleet
// byte-for-byte). The canonical call site is in cmd/fleet/serve.go:
//
//	kb := redis.NewKeyBuilder(config.Redis.KeyPrefix)
func NewKeyBuilder(prefix string) KeyBuilder {
	return KeyBuilder{prefix: prefix}
}

// Prefix returns the configured prefix string. Useful for code that needs
// to inspect or pass the prefix elsewhere (e.g. test cleanup scans). Most
// callers should not need this — use Key/HashTag/Sprintf/Scan instead.
func (b KeyBuilder) Prefix() string {
	return b.prefix
}

// Key prepends the builder's prefix to s and returns the result. For keys
// containing Redis Cluster hash tags use HashTag instead so the prefix
// stays outside the {...} braces.
func (b KeyBuilder) Key(s string) string {
	return b.prefix + s
}

// HashTag builds a Redis key of the form
//
//	<prefix><before>{<tagged>}<after>
//
// guaranteeing that the configured prefix lands BEFORE the hash-tag
// braces. Hash tags determine which slot a key maps to in Redis Cluster,
// so two keys built with the same `tagged` value (and the same builder)
// always co-locate on the same slot regardless of the prefix.
func (b KeyBuilder) HashTag(before, tagged, after string) string {
	return b.prefix + before + "{" + tagged + "}" + after
}

// Sprintf is the printf-style sibling of Key: it formats `format` with
// `args` and prepends the prefix to the result. Preferred for keys whose
// body contains a hash tag built via fmt.Sprintf — e.g.
// "policy_pass:{%d}", hostID — so all subsystems use a single, uniform
// construction style. Same hash-tag positioning rules as HashTag apply:
// the prefix lands before any "{...}" segment in `format`.
func (b KeyBuilder) Sprintf(format string, args ...any) string {
	return b.prefix + fmt.Sprintf(format, args...)
}

// Strip removes the builder's prefix from the front of s, if present.
// Use it for callers that read raw keys back out of Redis (for example
// via SCAN) and need to recover the original subsystem-level key name.
// If the prefix is empty or s does not start with it, s is returned
// unchanged.
func (b KeyBuilder) Strip(s string) string {
	if b.prefix == "" {
		return s
	}
	return strings.TrimPrefix(s, b.prefix)
}

// Scan is like the package-level ScanKeys but automatically prepends the
// builder's prefix to the supplied pattern, scoping the scan to this
// builder's namespace. Pass the redis pool the builder was created from
// (the pool is needed to actually run the SCAN — the builder only owns
// the prefix string).
func (b KeyBuilder) Scan(pool fleet.RedisPool, pattern string, count int) ([]string, error) {
	return ScanKeys(pool, b.prefix+pattern, count)
}
