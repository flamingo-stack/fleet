package redis

import (
	"testing"

	"github.com/fleetdm/fleet/v4/server/fleet"
	redigo "github.com/gomodule/redigo/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePool is a minimal fleet.RedisPool implementation that only carries a
// configurable KeyPrefix value — used to exercise the prefix helpers without
// spinning up a real Redis. It returns nil/empty for everything else; the
// helpers tested here never actually open a connection.
type fakePool struct {
	prefix string
}

func (f *fakePool) Get() redigo.Conn                     { return nil }
func (f *fakePool) Close() error                         { return nil }
func (f *fakePool) Stats() map[string]redigo.PoolStats   { return nil }
func (f *fakePool) Mode() fleet.RedisMode                { return fleet.RedisStandalone }
func (f *fakePool) KeyPrefix() string                    { return f.prefix }

// TestPrefixKey covers the basic concatenation contract and the "empty
// prefix is identity" backward-compat invariant.
func TestPrefixKey(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		key    string
		want   string
	}{
		{"empty prefix returns key unchanged (backward-compat)", "", "livequery:42", "livequery:42"},
		{"tenant prefix prepended", "fleet:t1:", "livequery:42", "fleet:t1:livequery:42"},
		{"prefix is opaque, can be anything", "x:y:z:", "k", "x:y:z:k"},
		{"empty key with prefix yields just the prefix", "fleet:t1:", "", "fleet:t1:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := &fakePool{prefix: tc.prefix}
			assert.Equal(t, tc.want, PrefixKey(pool, tc.key))
		})
	}
}

// TestPrefixHashTagKey is the most important test in this file: it verifies
// that the per-tenant prefix lands OUTSIDE the {…} hash tag braces. Two
// keys built with the same tag must always hash to the same Redis Cluster
// slot, regardless of which tenant they belong to. We can't run a real
// CLUSTER KEYSLOT here (no Redis dependency in this package's unit tests),
// but we assert the shape that guarantees that property.
func TestPrefixHashTagKey(t *testing.T) {
	pool := &fakePool{prefix: "fleet:t1:"}

	// Replicates the live_query shape: livequery:{42} + sql:livequery:{42}
	bitfield := PrefixHashTagKey(pool, "livequery:", "42", "")
	sql := PrefixHashTagKey(pool, "sql:livequery:", "42", "")

	assert.Equal(t, "fleet:t1:livequery:{42}", bitfield)
	assert.Equal(t, "fleet:t1:sql:livequery:{42}", sql)

	// The hash tag (the substring between the first { and first }) must be
	// identical across both — that is the property Redis Cluster uses to
	// co-locate them on the same slot.
	assert.Equal(t, hashTag(bitfield), hashTag(sql),
		"hash tags must match for cluster slot co-location")

	// Replicates the errorstore shape: error:{HASH}:json + error:{HASH}:count
	jsonKey := PrefixHashTagKey(pool, "error:", "abc123", ":json")
	countKey := PrefixHashTagKey(pool, "error:", "abc123", ":count")
	assert.Equal(t, "fleet:t1:error:{abc123}:json", jsonKey)
	assert.Equal(t, "fleet:t1:error:{abc123}:count", countKey)
	assert.Equal(t, hashTag(jsonKey), hashTag(countKey))

	// With an empty prefix the helper still produces the original
	// upstream-Fleet shape — no behavior change.
	emptyPool := &fakePool{prefix: ""}
	assert.Equal(t, "livequery:{42}", PrefixHashTagKey(emptyPool, "livequery:", "42", ""))
	assert.Equal(t, "error:{abc123}:json", PrefixHashTagKey(emptyPool, "error:", "abc123", ":json"))
}

// TestStripPrefix verifies that a key produced by PrefixKey round-trips
// cleanly back to the original. This is what live_query.extractTargetKeyName
// relies on to recover campaign IDs from raw Redis keys.
func TestStripPrefix(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		key    string
		want   string
	}{
		{"empty prefix is no-op", "", "livequery:{42}", "livequery:{42}"},
		{"prefix is stripped when present", "fleet:t1:", "fleet:t1:livequery:{42}", "livequery:{42}"},
		{"key without prefix is returned unchanged", "fleet:t1:", "livequery:{42}", "livequery:{42}"},
		{"only prefix yields empty string", "fleet:t1:", "fleet:t1:", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := &fakePool{prefix: tc.prefix}
			assert.Equal(t, tc.want, StripPrefix(pool, tc.key))
		})
	}
}

// TestPrefixRoundTrip ensures PrefixKey and StripPrefix are exact inverses.
// This is the property extractTargetKeyName depends on.
func TestPrefixRoundTrip(t *testing.T) {
	for _, prefix := range []string{"", "fleet:t1:", "x:", "very:long:prefix:with:colons:"} {
		for _, key := range []string{"livequery:{42}", "error:{abc}:count", "key_value_foo", ""} {
			pool := &fakePool{prefix: prefix}
			require.Equal(t, key, StripPrefix(pool, PrefixKey(pool, key)),
				"round-trip failed for prefix=%q key=%q", prefix, key)
		}
	}
}

// hashTag extracts the substring between the first '{' and the first '}'
// after it — the same algorithm Redis Cluster uses to compute the slot
// hash. Mirrors logic in github.com/mna/redisc and Redis itself; kept
// inline here so this test has no external Redis dependency.
func hashTag(key string) string {
	openIdx := -1
	for i := 0; i < len(key); i++ {
		if key[i] == '{' {
			openIdx = i
			break
		}
	}
	if openIdx < 0 {
		return key
	}
	for j := openIdx + 1; j < len(key); j++ {
		if key[j] == '}' {
			if j == openIdx+1 {
				// "{}" is treated as no hash tag
				return key
			}
			return key[openIdx+1 : j]
		}
	}
	return key
}

// TestHashTagHelper sanity-checks the hashTag helper itself so that a
// regression in the test infra does not silently mask a regression in
// PrefixHashTagKey.
func TestHashTagHelper(t *testing.T) {
	assert.Equal(t, "42", hashTag("livequery:{42}"))
	assert.Equal(t, "42", hashTag("fleet:t1:livequery:{42}"))
	assert.Equal(t, "abc", hashTag("fleet:t1:error:{abc}:json"))
	assert.Equal(t, "no-tag", hashTag("no-tag")) // no braces => entire key is the tag
}
