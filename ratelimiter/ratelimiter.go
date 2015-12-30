// Package ratelimiter implements a generic ratelimiter using a token bucket
// algorithm and a backing redis instance (or cluster)
package ratelimiter

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/mediocregopher/radix.v2/cluster"
	"github.com/mediocregopher/radix.v2/pool"
	"github.com/mediocregopher/radix.v2/util"
)

// Errors which may be returned
var (
	ErrRateLimited = errors.New("rate limited")
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

	// Number of tokens which are added to any given bucket every second.
	// Defaults to 1
	FillTokensPerSecond float64

	// Maximum number of tokens which are allowed to be in a bucket. Defaults
	// to 10
	MaxTokens float64

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

	if o.FillTokensPerSecond == 0 {
		o.FillTokensPerSecond = 1
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

func (r *RateLimiter) bucketKey(key string) string {
	return fmt.Sprintf("%s-{%s}-bucket", r.opts.Prefix, key)
}

func (r *RateLimiter) tsKey(key string) string {
	return fmt.Sprintf("%s-{%s}-ts", r.opts.Prefix, key)
}

const takeScript = `
	local bucketKey = KEYS[1]
	local tsKey = KEYS[2]

	local toTake = tonumber(ARGV[1])
	local curTS = tonumber(ARGV[2])
	local perSec = tonumber(ARGV[3])
	local maxTokens = tonumber(ARGV[4])

	local tokens
	local vals = redis.call("MGET", bucketKey, tsKey)
	if not vals[1] or not vals[2] then
		tokens = maxTokens
	else
		local diffTS = curTS - tonumber(vals[2])
		tokens = tonumber(vals[1]) + (diffTS*perSec)
	end

	tokens = tokens - toTake
	if tokens < 0 then
		return "0"
	end

	redis.call("MSET", bucketKey, tokens, tsKey, curTS)
	local expire = math.ceil(maxTokens / perSec)
	redis.call("EXPIRE", bucketKey, expire)
	redis.call("EXPIRE", tsKey, expire)
	return tostring(tokens)
`

// Take will attempt to take the given number of tokens out of the bucket
// identified by key, and returns the number of tokens left in the bucket. If
// the bucket does not have enough tokens in it then none are taken out, and
// ErrRateLimited is returned
func (r *RateLimiter) Take(key string, tokens float64) (float64, error) {
	curTS := float64(time.Now().UnixNano()) / 1.0e9
	left, err := util.LuaEval(
		r.rc,
		takeScript,
		2,
		r.bucketKey(key),
		r.tsKey(key),
		tokens,
		curTS,
		r.opts.FillTokensPerSecond,
		r.opts.MaxTokens,
	).Str()
	if err != nil {
		return 0, err
	} else if left == "0" {
		return 0, ErrRateLimited
	}

	return strconv.ParseFloat(left, 64)
}
