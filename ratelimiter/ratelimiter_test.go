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
		Interval:  1 * time.Second,
		MaxTokens: 10,
		Prefix:    "ratelimiter-test",
		PoolSize:  1,
	}
	var err error
	if testrl, err = New("localhost:6379", false, &opts); err != nil {
		panic(err)
	}
}

func TestTake(t *T) {
	k := testutil.RandStr()

	left, err := testrl.Take(k, 1)
	require.Nil(t, err)
	assert.Equal(t, int64(9), left)

	time.Sleep(500 * time.Millisecond)

	left, err = testrl.Take(k, 8)
	require.Nil(t, err)
	assert.Equal(t, int64(1), left)

	left, err = testrl.Take(k, 2)
	require.Nil(t, err)
	assert.Equal(t, int64(-1), left)

	time.Sleep(500 * time.Millisecond)
	left, err = testrl.Take(k, 2)
	require.Nil(t, err)
	assert.Equal(t, int64(0), left)

	time.Sleep(500 * time.Millisecond)
	left, err = testrl.Take(k, 2)
	require.Nil(t, err)
	assert.Equal(t, int64(6), left)

	pttl, err := testrl.rc.Cmd("PTTL", testrl.opts.Prefix+"-"+k).Int()
	require.Nil(t, err)
	expected := testrl.opts.Interval.Seconds() * 1.0e3
	assert.True(t, float64(pttl) < 1.1*expected)
	assert.True(t, float64(pttl) > 0.9*expected)
}
