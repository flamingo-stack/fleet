package pubsub

import (
	"testing"

	"github.com/fleetdm/fleet/v4/server/datastore/redis"
	"github.com/fleetdm/fleet/v4/server/datastore/redis/redistest"
	"github.com/go-kit/log"
)

func SetupRedisForTest(t *testing.T, cluster, readReplica bool) *redisQueryResults {
	const dupResults = false
	pool := redistest.SetupRedis(t, "zz", cluster, false, readReplica)
	// Tests use an empty key prefix (upstream-Fleet-identical key shape).
	kb := redis.NewKeyBuilder("")
	return NewRedisQueryResults(pool, kb, dupResults, log.NewNopLogger())
}
