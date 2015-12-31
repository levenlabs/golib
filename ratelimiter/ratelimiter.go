// Package ratelimiter implements generic rate-limiting algorithms
package ratelimiter

import (
	"time"

	"github.com/mediocregopher/radix.v2/util"
)

// key limit intervalMS nowMS [amount]
const tokenBucketTakeScript = `
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

// TokenBucketTake implements a token bucket rate-limiting system. Each bucket
// (identified by key) starts with some amount of tokens (maxTokens) in it.
// Whenever some action is performed tokens are removed from the bucket
// (toTake). If there are not enough tokens in the bucket then the action is
// rate-limited.
//
// Tokens which were taken out of the bucket more than the given Duration
// previously are automatically returned to the bucket. If a bucket has never
// been used (i.e. key doesn't exist in redis) it is considered to be full.
//
// Returns the number of tokens left in the bucket after toTake is removed, or a
// negative value if none are available to be taken. An error is only returned
// if communication with the datastore fails.
func TokenBucketTake(rc util.Cmder, key string, interval time.Duration, maxTokens, toTake int64) (int64, error) {
	nowMS := time.Now().UnixNano() / 1.0e6
	intervalMS := int64(interval.Seconds() * 1.0e3)
	return util.LuaEval(
		rc,
		tokenBucketTakeScript,
		1,
		key,
		maxTokens,
		intervalMS,
		nowMS,
		toTake,
	).Int64()
}
