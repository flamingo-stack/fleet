package redis

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/fleetdm/fleet/v4/server/config"
	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/gomodule/redigo/redis"
	"github.com/mna/redisc"
)

// this is an adapter type to implement the same Stats method as for
// redisc.Cluster, so both can satisfy the same interface.
type standalonePool struct {
	*redis.Pool
	addr            string
	connWaitTimeout time.Duration
	keyPrefix       string
}

func (p *standalonePool) Get() redis.Conn {
	var conn redis.Conn
	if p.connWaitTimeout <= 0 {
		conn = p.Pool.Get()
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), p.connWaitTimeout)
		defer cancel()

		// GetContext always returns an "errorConn" as valid connection when there is
		// an error, so there's no need to care about the second return value (as for
		// the no-wait case, the errorConn will fail on first use with the actual
		// error).
		conn, _ = p.Pool.GetContext(ctx)
	}
	return newPrefixedConn(conn, p.keyPrefix)
}

func (p *standalonePool) Stats() map[string]redis.PoolStats {
	return map[string]redis.PoolStats{
		p.addr: p.Pool.Stats(),
	}
}

func (p *standalonePool) Mode() fleet.RedisMode {
	return fleet.RedisStandalone
}

func (p *standalonePool) KeyPrefix() string { return p.keyPrefix }

type clusterPool struct {
	*redisc.Cluster
	followRedirs bool
	readReplica  bool
	keyPrefix    string
}

func (p *clusterPool) Get() redis.Conn {
	return newPrefixedConn(p.Cluster.Get(), p.keyPrefix)
}

func (p *clusterPool) Mode() fleet.RedisMode {
	return fleet.RedisCluster
}

func (p *clusterPool) KeyPrefix() string { return p.keyPrefix }

// keyPrefixOf returns the per-tenant key prefix configured on the pool, or
// "" if the pool is untyped (e.g. a test pool).
func keyPrefixOf(pool fleet.RedisPool) string {
	if kp, ok := pool.(interface{ KeyPrefix() string }); ok {
		return kp.KeyPrefix()
	}
	return ""
}

// PoolConfig holds the redis pool configuration options.
type PoolConfig struct {
	Server                    string
	CacheName                 string // for ElastiCache IAM auth
	Region                    string // for ElastiCache IAM auth
	Username                  string
	Password                  string
	Database                  int
	// KeyPrefix, if non-empty, is prepended to every Redis key and pub/sub
	// channel written or read by this pool. Used for multi-tenant Fleet
	// deployments that share one Redis (cluster). A trailing ":" is added
	// automatically if missing so generated keys remain readable.
	KeyPrefix string
	UseTLS                    bool
	StsAssumeRoleArn          string
	StsExternalID             string
	ConnTimeout               time.Duration
	KeepAlive                 time.Duration
	ConnectRetryAttempts      int
	ClusterFollowRedirections bool
	ClusterReadFromReplica    bool
	TLSCert                   string
	TLSKey                    string
	TLSCA                     string
	TLSServerName             string
	TLSHandshakeTimeout       time.Duration
	MaxIdleConns              int
	MaxOpenConns              int
	ConnMaxLifetime           time.Duration
	IdleTimeout               time.Duration
	ConnWaitTimeout           time.Duration
	TLSSkipVerify             bool
	WriteTimeout              time.Duration
	ReadTimeout               time.Duration

	// allows for testing dial retries and other dial-related scenarios
	testRedisDialFunc func(net, addr string, opts ...redis.DialOption) (redis.Conn, error)
}

// NewPool creates a Redis connection pool using the provided server
// address, username, password and database.
func NewPool(config PoolConfig) (fleet.RedisPool, error) {
	prefix := normalizeKeyPrefix(config.KeyPrefix)
	cluster, err := newCluster(config)
	if err != nil {
		return nil, err
	}
	if err := cluster.Refresh(); err != nil {
		if isClusterDisabled(err) || isClusterCommandUnknown(err) {
			// not a Redis Cluster setup, use a standalone Redis pool. When
			// multiple seeds are configured, pick the first non-empty one.
			seeds := splitSeedNodes(config.Server)
			var addr string
			if len(seeds) > 0 {
				addr = seeds[0]
			}
			pool, _ := cluster.CreatePool(addr, cluster.DialOptions...)
			cluster.Close()
			return &standalonePool{pool, addr, config.ConnWaitTimeout, prefix}, nil
		}
		return nil, fmt.Errorf("refresh cluster: %w", err)
	}

	return &clusterPool{
		cluster,
		config.ClusterFollowRedirections,
		config.ClusterReadFromReplica,
		prefix,
	}, nil
}

// splitSeedNodes parses FLEET_REDIS_ADDRESS into a list of host:port seed
// nodes for redisc.Cluster.StartupNodes. The input may be:
//   - a single host:port              ("redis-0:6379")
//   - a redis:// URL                  ("redis://host:6379", e.g. Render free tier)
//   - a comma-separated list of seeds ("a:6379,redis://b:6379,c:6379")
//
// Whitespace and the redis:// scheme are stripped from each entry; empty
// entries are dropped. With multiple seeds, redisc tries them in turn until
// cluster discovery succeeds — useful when a single seed pod is unhealthy
// at boot.
func splitSeedNodes(server string) []string {
	if server == "" {
		return nil
	}
	parts := strings.Split(server, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.TrimPrefix(p, "redis://")
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// normalizeKeyPrefix ensures the prefix ends with ":" so generated keys read
// as "<tenant>:<original-key>". An empty prefix stays empty (no-op wrapper).
func normalizeKeyPrefix(p string) string {
	if p == "" {
		return ""
	}
	if !strings.HasSuffix(p, ":") {
		return p + ":"
	}
	return p
}

// ReadOnlyConn turns conn into a connection that will try to connect to a
// replica instead of a primary. Note that this is not guaranteed that it will
// do so (there may not be any replica, or due to redirections it may end up on
// a primary, etc.), and it will only try to do so if pool is a Redis Cluster
// pool. The returned connection should only be used to run read-only
// commands.
func ReadOnlyConn(pool fleet.RedisPool, conn redis.Conn) redis.Conn {
	if p, isCluster := pool.(*clusterPool); isCluster && p.readReplica {
		// it only fails if the connection is not a redisc connection or the
		// connection is already bound, in which case we just return the connection
		// as-is. Our prefixedConn implements ReadOnly() and forwards to the
		// inner redisc.Conn, so the interface assertion in redisc.ReadOnlyConn
		// keeps working through the wrapper.
		_ = redisc.ReadOnlyConn(conn)
	}
	return conn
}

// ConfigureDoer configures conn to follow redirections if the redis
// configuration requested it and the pool is a Redis Cluster pool. If the conn
// is already in error, or if it is not a redisc cluster connection, it is
// returned unaltered.
func ConfigureDoer(pool fleet.RedisPool, conn redis.Conn) redis.Conn {
	if p, isCluster := pool.(*clusterPool); isCluster {
		if err := conn.Err(); err == nil && p.followRedirs {
			// redisc.RetryConn does a direct *redisc.Conn type assertion and
			// rejects our prefixedConn wrapper. Unwrap, wrap the retrying conn,
			// then re-wrap with the same prefix.
			inner, prefix := unwrapConn(conn)
			rc, err := redisc.RetryConn(inner, 3, 300*time.Millisecond)
			if err == nil {
				return newPrefixedConn(rc, prefix)
			}
		}
	}
	return conn
}

// SplitKeysBySlot takes a list of redis keys and groups them by hash slot
// so that keys in a given group are guaranteed to hash to the same slot, making
// them safe to run e.g. in a pipeline on the same connection or as part of a
// multi-key command in a Redis Cluster setup. When using standalone Redis, it
// simply returns all keys in the same group (i.e. the top-level slice has a
// length of 1).
//
// Slot is computed from the on-wire (prefixed) key so the groupings match
// what Redis Cluster will actually route. The returned keys are the original
// (unprefixed) ones — they get re-prefixed automatically when sent through a
// connection from this pool.
func SplitKeysBySlot(pool fleet.RedisPool, keys ...string) [][]string {
	if _, isCluster := pool.(*clusterPool); !isCluster {
		return [][]string{keys}
	}
	prefix := keyPrefixOf(pool)
	if prefix == "" {
		return redisc.SplitBySlot(keys...)
	}
	// Group raw keys by the slot of their prefixed form so cluster routing
	// stays correct even for keys without an explicit {hash-tag}.
	bySlot := make(map[int][]string)
	order := make([]int, 0)
	for _, k := range keys {
		slot := redisc.Slot(prefix + k)
		if _, seen := bySlot[slot]; !seen {
			order = append(order, slot)
		}
		bySlot[slot] = append(bySlot[slot], k)
	}
	out := make([][]string, 0, len(order))
	for _, s := range order {
		out = append(out, bySlot[s])
	}
	return out
}

// EachNode calls fn for each node in the redis cluster, with a connection
// to that node, until all nodes have been visited. The connection is
// automatically closed after the call. If fn returns an error, the iteration
// of nodes stops and EachNode returns that error. For standalone redis,
// fn is called only once.
//
// If replicas is true, it will visit each replica node instead, otherwise the
// primary nodes are visited. Keep in mind that if replicas is true, it will
// visit all known replicas - which is great e.g. to run diagnostics on each
// node, but can be surprising if the goal is e.g. to collect all keys, as it
// is possible that more than one node is acting as replica for the same
// primary, meaning that the same keys could be seen multiple times - you
// should be prepared to handle this scenario. The connection provided to fn is
// not a ReadOnly connection (conn.ReadOnly hasn't been called on it), it is up
// to fn to execute the READONLY redis command if required.
func EachNode(pool fleet.RedisPool, replicas bool, fn func(conn redis.Conn) error) error {
	prefix := keyPrefixOf(pool)
	if cluster, isCluster := pool.(*clusterPool); isCluster {
		return cluster.EachNode(replicas, func(_ string, conn redis.Conn) error {
			// per-node conns come from redisc directly — wrap so callers see
			// the same key/channel prefixing as conns from pool.Get().
			return fn(newPrefixedConn(conn, prefix))
		})
	}

	conn := pool.Get()
	defer conn.Close()
	return fn(conn)
}

// BindConn binds the connection to the redis node that serves those keys.
// In a Redis Cluster setup, all keys must hash to the same slot, otherwise
// an error is returned. In a Redis Standalone setup, it is a no-op and never
// fails. On successful return, the connection is ready to be used with those
// keys.
func BindConn(pool fleet.RedisPool, conn redis.Conn, keys ...string) error {
	if _, isCluster := pool.(*clusterPool); isCluster {
		return redisc.BindConn(conn, keys...)
	}
	return nil
}

// PublishHasListeners is like the PUBLISH redis command, but it also returns a
// boolean indicating if channel still has subscribed listeners. It is required
// because the redis command only returns the count of subscribers active on
// the same node as the one that is used to publish, which may not always be
// the case in Redis Cluster (especially with the read from replica option
// set).
//
// In Standalone mode, it is the same as PUBLISH (with the count of subscribers
// turned into a boolean), and in Cluster mode, if the count returned by
// PUBLISH is 0, it gets the number of subscribers on each node in the cluster
// to get the accurate count.
func PublishHasListeners(pool fleet.RedisPool, conn redis.Conn, channel, message string) (bool, error) {
	n, err := redis.Int(conn.Do("PUBLISH", channel, message))
	if n > 0 || err != nil {
		return n > 0, err
	}

	// otherwise n == 0, check the actual number of subscribers if this is a
	// redis cluster.
	if _, isCluster := pool.(*clusterPool); !isCluster {
		return false, nil
	}

	errDone := errors.New("done")
	var count int

	// subscribers can be subscribed on replicas, so we need to iterate on both
	// primaries and replicas.
	for _, replicas := range []bool{true, false} {
		err = EachNode(pool, replicas, func(conn redis.Conn) error {
			res, err := redis.Values(conn.Do("PUBSUB", "NUMSUB", channel))
			if err != nil {
				return err
			}
			var (
				name string
				n    int
			)
			_, err = redis.Scan(res, &name, &n)
			if err != nil {
				return err
			}
			count += n
			if count > 0 {
				// end early if we know it has subscribers
				return errDone
			}
			return nil
		})

		if err == errDone {
			break
		}
	}

	// if it completed successfully
	if err == nil || err == errDone {
		return count > 0, nil
	}
	return false, fmt.Errorf("checking for active subscribers: %w", err)
}

func newCluster(conf PoolConfig) (*redisc.Cluster, error) {
	// Initialize AWS IAM token generator if needed
	var awsIAMTokenGen *awsIAMAuthTokenGenerator

	opts := []redis.DialOption{
		redis.DialDatabase(conf.Database),
		redis.DialUseTLS(conf.UseTLS),
		redis.DialConnectTimeout(conf.ConnTimeout),
		redis.DialKeepAlive(conf.KeepAlive),
		redis.DialUsername(conf.Username),
		redis.DialWriteTimeout(conf.WriteTimeout),
		redis.DialReadTimeout(conf.ReadTimeout),
	}

	// Auto-detect ElastiCache and use IAM auth if no password is provided
	useIAMAuth := false
	if conf.Password == "" && conf.Region != "" && conf.CacheName != "" {
		useIAMAuth = true
		var err error
		region := conf.Region
		cacheName := conf.CacheName
		awsIAMTokenGen, err = newAWSIAMAuthTokenGenerator(cacheName, conf.Username, region, conf.StsAssumeRoleArn, conf.StsExternalID)
		if err != nil {
			return nil, fmt.Errorf("failed to create AWS IAM token generator: %w", err)
		}
	} else if conf.Password != "" {
		opts = append(opts, redis.DialPassword(conf.Password))
	}

	if conf.UseTLS {
		var err error
		tlsCfg := config.TLS{
			TLSCA:         conf.TLSCA,
			TLSCert:       conf.TLSCert,
			TLSKey:        conf.TLSKey,
			TLSServerName: conf.TLSServerName,
		}
		cfg, err := tlsCfg.ToTLSConfig()
		if err != nil {
			return nil, err
		}
		cfg.InsecureSkipVerify = conf.TLSSkipVerify

		opts = append(opts,
			redis.DialTLSConfig(cfg),
			redis.DialUseTLS(true),
			redis.DialTLSHandshakeTimeout(conf.TLSHandshakeTimeout))
	}

	dialFn := redis.Dial
	if conf.testRedisDialFunc != nil {
		dialFn = conf.testRedisDialFunc
	}

	return &redisc.Cluster{
		StartupNodes: splitSeedNodes(conf.Server),
		PoolWaitTime: conf.ConnWaitTimeout,
		DialOptions:  opts,
		CreatePool: func(server string, opts ...redis.DialOption) (*redis.Pool, error) {
			return &redis.Pool{
				MaxIdle:         conf.MaxIdleConns,
				MaxActive:       conf.MaxOpenConns,
				IdleTimeout:     conf.IdleTimeout,
				MaxConnLifetime: conf.ConnMaxLifetime,
				Wait:            conf.ConnWaitTimeout > 0,

				Dial: func() (redis.Conn, error) {
					var conn redis.Conn
					op := func() error {
						dialOpts := opts
						if useIAMAuth {
							token, err := awsIAMTokenGen.generateAuthToken(context.Background())
							if err != nil {
								return fmt.Errorf("failed to generate IAM auth token: %w", err)
							}
							dialOpts = append(dialOpts, redis.DialPassword(token))
						}

						c, err := dialFn("tcp", server, dialOpts...)

						var netErr net.Error
						if errors.As(err, &netErr) {
							if netErr.Temporary() || netErr.Timeout() {
								// retryable error
								return err
							}
						}
						if err != nil {
							// at this point, this is a non-retryable error
							return backoff.Permanent(err)
						}

						// success, store the connection to use
						conn = c
						return nil
					}

					if conf.ConnectRetryAttempts > 0 {
						boff := backoff.WithMaxRetries(backoff.NewExponentialBackOff(),
							uint64(conf.ConnectRetryAttempts)) //nolint:gosec // G115 false positive
						if err := backoff.Retry(op, boff); err != nil {
							return nil, err
						}
					} else if err := op(); err != nil {
						return nil, err
					}
					return conn, nil
				},

				TestOnBorrow: func(c redis.Conn, t time.Time) error {
					if time.Since(t) < time.Minute {
						return nil
					}
					_, err := c.Do("PING")
					return err
				},
			}, nil
		},
	}, nil
}

func isClusterDisabled(err error) bool {
	return strings.Contains(err.Error(), "ERR This instance has cluster support disabled") ||
		strings.Contains(err.Error(), "NOPERM this user has no permissions to run the 'cluster' command")
}

// On GCP Memorystore the CLUSTER command is entirely unavailable and fails with
// this error. See
// https://cloud.google.com/memorystore/docs/redis/product-constraints#blocked_redis_commands
//
// At some point it seems like the error message changed from wrapping the
// command name with backticks to single quotes.
//
// On RedisLabs, user reports indicate that the CLUSTER command fails with "ERR
// command is not allowed" when cluster mode is disabled.
func isClusterCommandUnknown(err error) bool {
	return strings.Contains(err.Error(), "ERR unknown command `CLUSTER`") ||
		strings.Contains(err.Error(), "ERR unknown command 'CLUSTER'") ||
		strings.Contains(err.Error(), "ERR unknown command CLUSTER") ||
		strings.Contains(err.Error(), `ERR unknown command "CLUSTER"`) ||
		strings.Contains(err.Error(), `ERR command is not allowed`)
}

func ScanKeys(pool fleet.RedisPool, pattern string, count int) ([]string, error) {
	var keys []string
	prefix := keyPrefixOf(pool)

	err := EachNode(pool, false, func(conn redis.Conn) error {
		cursor := 0
		for {
			// conn.Do prefixes the MATCH pattern automatically; the returned
			// key names come back from Redis in their on-wire form (prefixed),
			// so strip the prefix before handing keys to callers.
			res, err := redis.Values(conn.Do("SCAN", cursor, "MATCH", pattern, "COUNT", count))
			if err != nil {
				return fmt.Errorf("scan keys: %w", err)
			}
			var curKeys []string
			_, err = redis.Scan(res, &cursor, &curKeys)
			if err != nil {
				return fmt.Errorf("convert scan results: %w", err)
			}
			if prefix != "" {
				for i, k := range curKeys {
					curKeys[i] = strings.TrimPrefix(k, prefix)
				}
			}
			keys = append(keys, curKeys...)
			if cursor == 0 {
				return nil
			}
		}
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}
