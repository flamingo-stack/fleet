package redis

import (
	"fmt"
	"strings"

	"github.com/gomodule/redigo/redis"
)

// prefixedConn wraps a redigo.Conn and transparently prepends keyPrefix to
// every key and pub/sub channel argument. It is the single point that makes
// Fleet's per-tenant key namespace work when multiple Fleet servers share
// one Redis (cluster).
//
// Policy is fail-CLOSED for tenant isolation: by default arg[0] is treated
// as a key and prefixed. This is correct for the vast majority of Redis
// commands (GET/SET/HSET/ZADD/LPUSH/EXPIRE/TTL/TYPE/INCR/...) — including
// commands we have never heard of and commands future upstream commits will
// introduce. Two explicit tables override the default:
//
//   - noPrefixCmds: commands whose args carry no keys/channels (admin,
//     handshake, transactions, server info — PING/AUTH/CLUSTER/...).
//   - specialCmds:  commands with keys in non-arg[0] positions (multi-key
//     ops, EVAL, SCAN, pub/sub channel commands).
//
// If a new command appears that is not key-based and is missing from
// noPrefixCmds, arg[0] gets prefixed and Redis usually rejects it with a
// loud error (wrong type / syntax error). That is preferable to the
// allowlist alternative, where a missed entry silently leaks keys between
// tenants.
//
// Bind/ReadOnly are implemented so redisc.BindConn / ReadOnlyConn keep
// working through interface assertion. redisc.RetryConn does a direct
// *redisc.Conn type assertion and rejects the wrapper — callers must unwrap
// before passing to RetryConn (see ConfigureDoer in redis.go).
type prefixedConn struct {
	inner  redis.Conn
	prefix string
}

func newPrefixedConn(inner redis.Conn, prefix string) redis.Conn {
	if prefix == "" || inner == nil {
		return inner
	}
	if _, already := inner.(*prefixedConn); already {
		return inner
	}
	return &prefixedConn{inner: inner, prefix: prefix}
}

func unwrapConn(c redis.Conn) (redis.Conn, string) {
	if pc, ok := c.(*prefixedConn); ok {
		return pc.inner, pc.prefix
	}
	return c, ""
}

func (p *prefixedConn) Close() error                  { return p.inner.Close() }
func (p *prefixedConn) Err() error                    { return p.inner.Err() }
func (p *prefixedConn) Flush() error                  { return p.inner.Flush() }
func (p *prefixedConn) Receive() (interface{}, error) { return p.inner.Receive() }

func (p *prefixedConn) Do(cmd string, args ...interface{}) (interface{}, error) {
	return p.inner.Do(cmd, prefixArgs(cmd, args, p.prefix)...)
}

func (p *prefixedConn) Send(cmd string, args ...interface{}) error {
	return p.inner.Send(cmd, prefixArgs(cmd, args, p.prefix)...)
}

// Bind makes redisc.BindConn(p, keys...) work — it asserts the
// interface{ Bind(...string) error } on the conn. We prefix the keys and
// forward to the inner conn's Bind, which is the real cluster-routing call.
func (p *prefixedConn) Bind(keys ...string) error {
	prefixed := make([]string, len(keys))
	for i, k := range keys {
		prefixed[i] = p.prefix + k
	}
	cc, ok := p.inner.(interface {
		Bind(...string) error
	})
	if !ok {
		return fmt.Errorf("redis: inner conn does not implement Bind")
	}
	return cc.Bind(prefixed...)
}

// ReadOnly makes redisc.ReadOnlyConn(p) work.
func (p *prefixedConn) ReadOnly() error {
	cc, ok := p.inner.(interface {
		ReadOnly() error
	})
	if !ok {
		return fmt.Errorf("redis: inner conn does not implement ReadOnly")
	}
	return cc.ReadOnly()
}

// prefixArgs rewrites args so that any element that represents a key or
// pub/sub channel gets keyPrefix prepended. See the type-level comment on
// prefixedConn for the policy.
func prefixArgs(cmd string, args []interface{}, prefix string) []interface{} {
	if prefix == "" {
		return args
	}
	up := strings.ToUpper(cmd)
	if noPrefixCmds[up] {
		return args
	}
	out := make([]interface{}, len(args))
	copy(out, args)
	if rule, ok := specialCmds[up]; ok {
		rule(out, prefix)
		return out
	}
	// Default: arg[0] is the key. Correct for the vast majority of Redis
	// commands; safe-by-default for unknown / future commands.
	if len(out) > 0 {
		prefixOne(out, 0, prefix)
	}
	return out
}

func prefixOne(args []interface{}, idx int, prefix string) {
	if idx < 0 || idx >= len(args) {
		return
	}
	args[idx] = prefix + toString(args[idx])
}

func toString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return fmt.Sprintf("%v", v)
	}
}

type prefixRule func(args []interface{}, prefix string)

// allArgs: every arg is a key/channel. Used by DEL/UNLINK/MGET/EXISTS/WATCH
// and by SUBSCRIBE/UNSUBSCRIBE/PSUBSCRIBE-family channel commands.
func allArgs(args []interface{}, prefix string) {
	for i := range args {
		prefixOne(args, i, prefix)
	}
}

// twoKeys: args[0] and args[1] are both keys. Used by RENAME/SMOVE/COPY/
// LMOVE/RPOPLPUSH/LCS/ZRANGESTORE.
func twoKeys(args []interface{}, prefix string) {
	prefixOne(args, 0, prefix)
	prefixOne(args, 1, prefix)
}

// evalArgs: EVAL/EVALSHA/FCALL — args = [script, numKeys, k1, ..., kN, arg1, ...].
func evalArgs(args []interface{}, prefix string) {
	if len(args) < 2 {
		return
	}
	n := toInt(args[1])
	if n <= 0 {
		return
	}
	for i := 0; i < n; i++ {
		prefixOne(args, 2+i, prefix)
	}
}

// scanArgs: SCAN cursor [MATCH pattern] [COUNT n] [TYPE t]. Find MATCH and
// prefix the pattern. SCAN without MATCH would see every tenant's keys —
// callers should always pass MATCH (ScanKeys in redis.go does).
func scanArgs(args []interface{}, prefix string) {
	for i := 0; i < len(args)-1; i++ {
		if strings.EqualFold(toString(args[i]), "MATCH") {
			prefixOne(args, i+1, prefix)
			return
		}
	}
}

// bitopArgs: BITOP op dst src [src ...]. args[0] is the operation (AND/OR/
// XOR/NOT), args[1..] are keys.
func bitopArgs(args []interface{}, prefix string) {
	for i := 1; i < len(args); i++ {
		prefixOne(args, i, prefix)
	}
}

// objectArgs: OBJECT <subcommand> [key]. Subcommands ENCODING/IDLETIME/FREQ/
// REFCOUNT take a key at args[1]; HELP takes no key.
func objectArgs(args []interface{}, prefix string) {
	if len(args) < 2 {
		return
	}
	sub := strings.ToUpper(toString(args[0]))
	switch sub {
	case "ENCODING", "IDLETIME", "FREQ", "REFCOUNT":
		prefixOne(args, 1, prefix)
	}
}

// pubsubArgs: PUBSUB <subcommand> [args...]. Only NUMSUB/CHANNELS/SHARD*
// take channel/pattern args (at index 1..).
func pubsubArgs(args []interface{}, prefix string) {
	if len(args) < 1 {
		return
	}
	sub := strings.ToUpper(toString(args[0]))
	switch sub {
	case "NUMSUB", "CHANNELS", "SHARDCHANNELS", "SHARDNUMSUB":
		for i := 1; i < len(args); i++ {
			prefixOne(args, i, prefix)
		}
	}
}

// blockingKeysArgs: BLPOP/BRPOP/BZPOPMIN/BZPOPMAX key [key ...] timeout.
// All args except the trailing timeout are keys.
func blockingKeysArgs(args []interface{}, prefix string) {
	if len(args) < 2 {
		return
	}
	for i := 0; i < len(args)-1; i++ {
		prefixOne(args, i, prefix)
	}
}

// sortArgs: SORT key [BY pat] [LIMIT off cnt] [GET pat...] [ASC|DESC]
// [ALPHA] [STORE dst]. Always prefix the source key; if STORE is present,
// prefix the destination too. BY/GET patterns reference external keys but
// Fleet doesn't use them — left unprefixed deliberately.
func sortArgs(args []interface{}, prefix string) {
	if len(args) == 0 {
		return
	}
	prefixOne(args, 0, prefix)
	for i := 1; i < len(args)-1; i++ {
		if strings.EqualFold(toString(args[i]), "STORE") {
			prefixOne(args, i+1, prefix)
			return
		}
	}
}

func toInt(v interface{}) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case int32:
		return int(x)
	case uint:
		return int(x)
	case uint64:
		return int(x)
	case string:
		var n int
		_, _ = fmt.Sscanf(x, "%d", &n)
		return n
	default:
		return 0
	}
}

// noPrefixCmds enumerates commands that contain NO keys or channels. These
// pass through verbatim. Membership here is the closed set of Redis admin/
// connection/info/transaction commands — it changes very rarely between
// Redis versions and is safe to maintain by hand.
//
// NOTE: commands not listed here AND not in specialCmds get arg[0] prefixed
// by default. This is intentional fail-closed behavior for tenant isolation.
var noPrefixCmds = map[string]bool{
	// connection / handshake
	"PING":      true,
	"AUTH":      true,
	"HELLO":     true,
	"SELECT":    true,
	"ECHO":      true,
	"QUIT":      true,
	"RESET":     true,
	"READONLY":  true,
	"READWRITE": true,

	// server / introspection
	"INFO":         true,
	"DBSIZE":       true,
	"TIME":         true,
	"ROLE":         true,
	"LASTSAVE":     true,
	"SHUTDOWN":     true,
	"BGSAVE":       true,
	"BGREWRITEAOF": true,
	"FLUSHDB":      true,
	"FLUSHALL":     true,
	"COMMAND":      true,
	"WAIT":         true,
	"FAILOVER":     true,
	"REPLICAOF":    true,
	"SLAVEOF":      true,

	// subcommand namespaces — none of their args are bare keys
	"CLIENT":   true,
	"CLUSTER":  true,
	"CONFIG":   true,
	"DEBUG":    true,
	"LATENCY":  true,
	"SLOWLOG":  true,
	"MEMORY":   true,
	"ACL":      true,
	"SCRIPT":   true,
	"FUNCTION": true,

	// transactions (WATCH is multi-key and handled in specialCmds)
	"MULTI":   true,
	"EXEC":    true,
	"DISCARD": true,
	"UNWATCH": true,
}

// specialCmds enumerates commands whose key/channel arguments are NOT at
// arg[0]. Everything else falls through to the default "prefix arg[0]" rule.
var specialCmds = map[string]prefixRule{
	// every arg is a key
	"DEL":    allArgs,
	"UNLINK": allArgs,
	"MGET":   allArgs,
	"EXISTS": allArgs,
	"TOUCH":  allArgs,
	"WATCH":  allArgs,

	// dst + src(s) — every arg is a key
	"SINTERSTORE": allArgs,
	"SUNIONSTORE": allArgs,
	"SDIFFSTORE":  allArgs,
	"ZINTERSTORE": allArgs,
	"ZUNIONSTORE": allArgs,
	"ZDIFFSTORE":  allArgs,
	"PFCOUNT":     allArgs,
	"PFMERGE":     allArgs,

	// args[0] and args[1] are both keys
	"RENAME":         twoKeys,
	"RENAMENX":       twoKeys,
	"SMOVE":          twoKeys,
	"COPY":           twoKeys,
	"LMOVE":          twoKeys,
	"BLMOVE":         twoKeys,
	"RPOPLPUSH":      twoKeys,
	"BRPOPLPUSH":     twoKeys,
	"LCS":            twoKeys,
	"ZRANGESTORE":    twoKeys,
	"GEOSEARCHSTORE": twoKeys,

	// BITOP op dst src [src...] — args[1..] are keys
	"BITOP": bitopArgs,

	// OBJECT <sub> key — key at args[1] for ENCODING/IDLETIME/FREQ/REFCOUNT
	"OBJECT": objectArgs,

	// scripting — args[1] is numKeys
	"EVAL":     evalArgs,
	"EVALSHA":  evalArgs,
	"FCALL":    evalArgs,
	"FCALL_RO": evalArgs,

	// SCAN — MATCH pattern
	"SCAN": scanArgs,

	// SORT key ... [STORE dst]
	"SORT": sortArgs,

	// pub/sub channel commands — every arg is a channel
	"SUBSCRIBE":    allArgs,
	"UNSUBSCRIBE":  allArgs,
	"PSUBSCRIBE":   allArgs,
	"PUNSUBSCRIBE": allArgs,
	"SSUBSCRIBE":   allArgs,
	"SUNSUBSCRIBE": allArgs,
	"PUBSUB":       pubsubArgs,

	// blocking pop — BLPOP key [key ...] timeout
	"BLPOP":    blockingKeysArgs,
	"BRPOP":    blockingKeysArgs,
	"BZPOPMIN": blockingKeysArgs,
	"BZPOPMAX": blockingKeysArgs,
}
