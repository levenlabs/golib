// Package ratelimiter implements a generic ratelimiter using a token bucket
// algorithm and a backing redis instance (or cluster).
//
// Conceptually, each arbitray key has a bucket of tokens. Tokens are taken from
// the bucket whenever some action is being performed, and if the bucket is
// empty then the action is considered rate limited. The bucket for each key is
// refilled at a given interval, up to a maximum amount of tokens. A key never
// used before automatically has a full bucket.
package ratelimiter

import (
	"time"

	"github.com/mediocregopher/radix.v2/cluster"
	"github.com/mediocregopher/radix.v2/pool"
	"github.com/mediocregopher/radix.v2/util"
)

// RateLimiter describes a token bucket system which can be used for
// rate-limiting.
//
// RateLimiter uses a backing redis instance or cluster to store data. If a
// cluster is used it must already be initialized and ready-to-go when
// RateLimiter is started.
//
// All methods on RateLimiter are thread-safe.
type RateLimiter struct {
	opts Opts
	rc   util.Cmder
}

// Opts describes a set of options which can change RateLimiter's behavior. Each
// option has a default value which will be used if the field is not set.
type Opts struct {

	// Duration of time before a token is returned to a key's bucket. Only
	// precision up to the millisecond is actually used. Defaults to 1 second.
	Interval time.Duration

	// Maximum number of tokens which are allowed to be in a bucket. Defaults
	// to 10
	MaxTokens int64

	// The string which will be prefixed to all keys stored in redis. Defaults
	// to "ratelimiter"
	Prefix string

	// PoolSize will be used as the number of connections to each backing redis
	// instance. Defaults to 10
	PoolSize int
}

// New returns an initialized RateLimiter backed by the given redis instance. If
// useCluster is true then the redis addr will be used to discover the rest of
// the redis cluster and that will be used to back the RateLimiter.
//
// If opts is nil then one with all default values will be used. If opts is not
// nil but any of its fields are not set their default values will be filled in.
func New(redisAddr string, useCluster bool, opts *Opts) (*RateLimiter, error) {
	o := fillOpts(opts)
	var rc util.Cmder
	var err error
	if useCluster {
		rc, err = cluster.NewWithOpts(cluster.Opts{
			Addr:     redisAddr,
			PoolSize: opts.PoolSize,
		})
	} else {
		rc, err = pool.New("tcp", redisAddr, o.PoolSize)
	}
	if err != nil {
		return nil, err
	}
	return &RateLimiter{
		opts: o,
		rc:   rc,
	}, nil
}

func fillOpts(o *Opts) Opts {
	if o == nil {
		o = &Opts{}
	}

	if o.Interval == 0 {
		o.Interval = 1 * time.Second
	}
	if o.MaxTokens == 0 {
		o.MaxTokens = 10
	}
	if o.Prefix == "" {
		o.Prefix = "ratelimiter"
	}
	if o.PoolSize == 0 {
		o.PoolSize = 10
	}
	return *o
}

// key limit intervalMS nowMS [amount]
const takeScript = `
	local key = KEYS[1]
	local limit = tonumber(ARGV[1])
	local intervalMS = tonumber(ARGV[2])
	local nowMS = tonumber(ARGV[3])
	local amount = 1
	if ARGV[4] then
	    amount = math.max(tonumber(ARGV[4]), 0)
	end

	redis.call('ZREMRANGEBYSCORE', key, '-inf', nowMS - intervalMS)
	local num = redis.call('ZCARD', key)

	local left = limit - num - amount
	if left < 0 then
		return left
	end

	local args = {'ZADD', key}
	for i = 1, amount do
	    args[(i * 2) + 1] = nowMS
	    args[(i * 2) + 2] = string.format("%x%x%x", nowMS, num, i)
	end
	redis.call(unpack(args))
	redis.call('PEXPIRE', key, intervalMS)
	return left
`

// Take will attempt to take the given number of tokens out of the bucket
// identified by key, and returns the number of tokens left in the bucket. If
// the bucket does not have enough tokens in it then none are taken out and a
// negative value is returned. An error is only returned in the case of not
// being able to communicate with the datastore.
func (r *RateLimiter) Take(key string, tokens int64) (int64, error) {
	nowMS := time.Now().UnixNano() / 1.0e6
	intervalMS := int64(r.opts.Interval.Seconds() * 1.0e3)
	return util.LuaEval(
		r.rc,
		takeScript,
		1,
		r.opts.Prefix+"-"+key,
		r.opts.MaxTokens,
		intervalMS,
		nowMS,
		tokens,
	).Int64()
}
