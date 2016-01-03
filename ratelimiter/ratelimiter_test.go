package ratelimiter

import (
	. "testing"
	"time"

	"github.com/levenlabs/golib/radixutil"
	"github.com/levenlabs/golib/testutil"
	"github.com/mediocregopher/radix.v2/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testCmder util.Cmder
var testInterval = 1 * time.Second

func init() {
	var err error
	testCmder, err = radixutil.DialMaybeCluster("tcp", "localhost:6379", 10)
	if err != nil {
		panic(err)
	}
}

func take(key string, toTake int64) (int64, error) {
	return TokenBucketTake(testCmder, key, testInterval, 10, toTake)
}

func TestTake(t *T) {
	k := testutil.RandStr()

	left, err := take(k, 1)
	require.Nil(t, err)
	assert.Equal(t, int64(9), left)

	time.Sleep(500 * time.Millisecond)

	left, err = take(k, 8)
	require.Nil(t, err)
	assert.Equal(t, int64(1), left)

	left, err = take(k, 2)
	require.Nil(t, err)
	assert.Equal(t, int64(-1), left)

	time.Sleep(500 * time.Millisecond)
	left, err = take(k, 2)
	require.Nil(t, err)
	assert.Equal(t, int64(0), left)

	time.Sleep(500 * time.Millisecond)
	left, err = take(k, 2)
	require.Nil(t, err)
	assert.Equal(t, int64(6), left)

	pttl, err := testCmder.Cmd("PTTL", k).Int()
	require.Nil(t, err)
	expected := testInterval.Seconds() * 1.0e3
	assert.True(t, float64(pttl) < 1.1*expected)
	assert.True(t, float64(pttl) > 0.9*expected)
}
