package redis

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKeyBuilder verifies the struct-style builder. It carries the prefix
// itself (no fleet.RedisPool dependency) and exposes the helpers Fleet
// subsystems use to build per-tenant Redis keys. Subsystems are expected
// to embed a KeyBuilder via NewKeyBuilder(prefix) at construction time
// and route every key build through it.
func TestKeyBuilder(t *testing.T) {
	t.Run("empty prefix is identity (backward-compat with upstream Fleet)", func(t *testing.T) {
		b := NewKeyBuilder("")
		assert.Equal(t, "livequery:42", b.Key("livequery:42"))
		assert.Equal(t, "livequery:{42}", b.HashTag("livequery:", "42", ""))
		assert.Equal(t, "policy_pass:{42}", b.Sprintf("policy_pass:{%d}", 42))
		assert.Equal(t, "k", b.Strip("k"))
		assert.Equal(t, "", b.Prefix())
	})

	t.Run("with prefix", func(t *testing.T) {
		b := NewKeyBuilder("fleet:t1:")
		assert.Equal(t, "fleet:t1:livequery:42", b.Key("livequery:42"))
		assert.Equal(t, "fleet:t1:livequery:{42}", b.HashTag("livequery:", "42", ""))
		assert.Equal(t, "fleet:t1:policy_pass:{42}", b.Sprintf("policy_pass:{%d}", 42))
		assert.Equal(t, "livequery:42", b.Strip("fleet:t1:livequery:42"))
		assert.Equal(t, "fleet:t1:", b.Prefix())
	})

	// HashTag MUST keep the per-tenant prefix OUTSIDE the {…} braces. Two
	// keys built with the same `tagged` value must always hash to the same
	// Redis Cluster slot regardless of the tenant prefix — that's the
	// invariant Fleet's live_query, errorstore, async ip_banner subsystems
	// rely on for multi-key Lua scripts and pipelines.
	t.Run("hash-tag stays inside braces under HashTag", func(t *testing.T) {
		b := NewKeyBuilder("fleet:t1:")
		bitfield := b.HashTag("livequery:", "42", "")
		sql := b.HashTag("sql:livequery:", "42", "")
		// The substring inside {...} must be identical for cluster slot co-location.
		assert.Equal(t, hashTag(bitfield), hashTag(sql))

		// errorstore shape: error:{HASH}:json + error:{HASH}:count
		jsonKey := b.HashTag("error:", "abc", ":json")
		countKey := b.HashTag("error:", "abc", ":count")
		assert.Equal(t, hashTag(jsonKey), hashTag(countKey))

		// Empty prefix preserves upstream-Fleet shape exactly.
		empty := NewKeyBuilder("")
		assert.Equal(t, "livequery:{42}", empty.HashTag("livequery:", "42", ""))
		assert.Equal(t, "error:{abc}:json", empty.HashTag("error:", "abc", ":json"))
	})

	t.Run("Strip + Key round-trip", func(t *testing.T) {
		for _, prefix := range []string{"", "fleet:t1:", "x:", "very:long:prefix:with:colons:"} {
			b := NewKeyBuilder(prefix)
			for _, key := range []string{"livequery:{42}", "error:{abc}:count", "key_value_foo", "k", ""} {
				require.Equal(t, key, b.Strip(b.Key(key)),
					"round-trip failed: prefix=%q key=%q", prefix, key)
			}
		}
	})

	t.Run("zero value is usable (no prefix, no panic)", func(t *testing.T) {
		// A KeyBuilder field on a struct that wasn't initialised yet
		// should still produce upstream-identical keys.
		var b KeyBuilder
		assert.Equal(t, "x", b.Key("x"))
		assert.Equal(t, "{42}", b.HashTag("", "42", ""))
		assert.Equal(t, "", b.Prefix())
	})
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
// KeyBuilder.HashTag.
func TestHashTagHelper(t *testing.T) {
	assert.Equal(t, "42", hashTag("livequery:{42}"))
	assert.Equal(t, "42", hashTag("fleet:t1:livequery:{42}"))
	assert.Equal(t, "abc", hashTag("fleet:t1:error:{abc}:json"))
	assert.Equal(t, "no-tag", hashTag("no-tag")) // no braces => entire key is the tag
}
