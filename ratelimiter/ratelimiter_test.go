package ratelimiter

import (
	. "testing"
	"time"

	"github.com/levenlabs/golib/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testrl *RateLimiter

func init() {
	opts := Opts{
		FillTokensPerSecond: 1,
		MaxTokens:           10,
		Prefix:              "ratelimiter-test",
		PoolSize:            1,
	}
	var err error
	if testrl, err = New("localhost:6379", false, &opts); err != nil {
		panic(err)
	}
}

func assertEqualIsh(t *T, expected, actual float64) {
	assert.True(t, expected-1 <= actual)
	assert.True(t, expected+1 >= actual)
}

func TestTake(t *T) {
	k := testutil.RandStr()

	left, err := testrl.Take(k, 9)
	require.Nil(t, err)
	assert.Equal(t, float64(1), left)

	_, err = testrl.Take(k, 2)
	assert.Equal(t, ErrRateLimited, err)

	time.Sleep(500 * time.Millisecond)
	_, err = testrl.Take(k, 2)
	assert.Equal(t, ErrRateLimited, err)

	time.Sleep(500 * time.Millisecond)
	left, err = testrl.Take(k, 2)
	require.Nil(t, err)
	assertEqualIsh(t, 0, left)

	ttl, err := testrl.rc.Cmd("TTL", testrl.bucketKey(k)).Int()
	require.Nil(t, err)
	assertEqualIsh(t, 10, float64(ttl))
}
