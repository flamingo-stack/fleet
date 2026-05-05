package fleet

import "github.com/gomodule/redigo/redis"

// RedisPool is the common interface for redigo's Pool for standalone Redis
// and redisc's Cluster for Redis Cluster.
type RedisPool interface {
	// Get returns a redis connection. It must always be closed after use.
	Get() redis.Conn

	// Close closes the redis connection.
	Close() error

	// Stats returns a map of redis pool statistics for each server address.
	Stats() map[string]redis.PoolStats

	// Mode returns the mode in which Redis is running.
	Mode() RedisMode

	// KeyPrefix returns the configured prefix that should be prepended to
	// every Redis key and pub/sub channel before issuing commands. Returns
	// the empty string when no prefix is configured (the default), in which
	// case callers should write/read keys exactly as before. Subsystems
	// should use the helpers in server/datastore/redis (PrefixKey,
	// PrefixHashTagKey, StripPrefix, ScanPrefixedKeys) rather than calling
	// this directly so that hash-tag positioning and scan patterns stay
	// consistent.
	KeyPrefix() string
}

// RedisMode indicates the mode in which Redis is running.
type RedisMode byte

// List of supported Redis modes.
const (
	RedisStandalone RedisMode = iota
	RedisCluster
)

// String returns the string representation of the Redis mode.
func (m RedisMode) String() string {
	switch m {
	case RedisStandalone:
		return "standalone"
	case RedisCluster:
		return "cluster"
	default:
		return "unknown"
	}
}
