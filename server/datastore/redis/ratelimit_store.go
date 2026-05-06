package redis

import (
	"strings"
	"time"

	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/gomodule/redigo/redis"
)

type ThrottledStore struct {
	Pool      fleet.RedisPool
	KeyPrefix string
	// Kb owns the per-tenant key prefix. Callers should set it from the
	// same KeyBuilder passed to other Fleet subsystems so that all keys
	// share the same namespace. Leaving it as the zero value keeps
	// upstream-Fleet behavior (no per-tenant prefix).
	Kb KeyBuilder
}

// NewThrottledStore is the conventional constructor for ThrottledStore.
// Prefer it over a struct literal so the ThrottledStore initialization
// signature matches the rest of Fleet's Redis-backed subsystems
// (NewSessionStore, NewProfileMatcher, NewLock, …): all of them take
// (pool, kb, ...subsystem-specific args). The struct fields are kept
// exported for upstream compatibility — the constructor is a thin
// convenience wrapper.
func NewThrottledStore(pool fleet.RedisPool, kb KeyBuilder, keyPrefix string) *ThrottledStore {
	return &ThrottledStore{
		Pool:      pool,
		Kb:        kb,
		KeyPrefix: keyPrefix,
	}
}

const (
	getWithTimeScript = `
local tbl = redis.call('TIME')
local val = redis.call('GET', KEYS[1])
table.insert(tbl, val)
return tbl
`

	compareAndSwapWithTTLScript = `
local v = redis.call('get', KEYS[1])
if v == false then
  return redis.error_reply("key does not exist")
end
if v ~= ARGV[1] then
  return 0
end
redis.call('SET', KEYS[1], ARGV[2], 'EX', ARGV[3])
return 1
`

	compareAndSwapNoKeyError = "key does not exist"
)

// k composes the fully-qualified Redis key for a throttled-store entry:
// per-tenant prefix (via Kb) followed by the in-store KeyPrefix (subsystem
// namespace, e.g. "ratelimit::") + caller key. Lua scripts below receive
// the fully-qualified key via KEYS[1], so script bodies don't need to
// know about prefixing.
func (s *ThrottledStore) k(key string) string {
	return s.Kb.Key(s.KeyPrefix + key)
}

func (s *ThrottledStore) GetWithTime(key string) (int64, time.Time, error) {
	var t time.Time

	key = s.k(key)

	conn := s.Pool.Get()
	defer conn.Close()
	if err := BindConn(s.Pool, conn, key); err != nil {
		return 0, t, err
	}
	// must come after BindConn due to redisc restrictions
	conn = ConfigureDoer(s.Pool, conn)

	script := redis.NewScript(1, getWithTimeScript)
	res, err := redis.Values(script.Do(conn, key))
	if err != nil {
		return 0, t, err
	}
	if len(res) < 3 {
		res = append(res, nil)
	}

	var secs, us, val int64
	val = -1 // initialize val to -1, will stay untouched if res[2] is nil
	if _, err := redis.Scan(res, &secs, &us, &val); err != nil {
		return 0, t, err
	}
	t = time.Unix(secs, us*int64(time.Microsecond))

	return val, t, nil
}

func (s *ThrottledStore) SetIfNotExistsWithTTL(key string, value int64, ttl time.Duration) (bool, error) {
	key = s.k(key)

	conn := ConfigureDoer(s.Pool, s.Pool.Get())
	defer conn.Close()

	ttlSeconds := int(ttl.Seconds())
	// An `EX 0` will fail, make sure that we set expiry for a minimum of one second
	if ttlSeconds < 1 {
		ttlSeconds = 1
	}

	_, err := redis.String(conn.Do("SET", key, value, "EX", ttlSeconds, "NX"))
	if err != nil {
		if err == redis.ErrNil {
			// not set due to NX condition not met
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func (s *ThrottledStore) CompareAndSwapWithTTL(key string, old, isNew int64, ttl time.Duration) (bool, error) {
	key = s.k(key)

	conn := s.Pool.Get()
	defer conn.Close()
	if err := BindConn(s.Pool, conn, key); err != nil {
		return false, err
	}
	// must come after BindConn due to redisc restrictions
	conn = ConfigureDoer(s.Pool, conn)

	ttlSeconds := int(ttl.Seconds())
	// An `EX 0` will fail, make sure that we set expiry for a minimum of one second
	if ttlSeconds < 1 {
		ttlSeconds = 1
	}

	script := redis.NewScript(1, compareAndSwapWithTTLScript)
	swapped, err := redis.Bool(script.Do(conn, key, old, isNew, ttlSeconds))
	if err != nil {
		if strings.Contains(err.Error(), compareAndSwapNoKeyError) {
			return false, nil
		}
		return false, err
	}

	return swapped, nil
}
