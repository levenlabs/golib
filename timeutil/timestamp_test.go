package timeutil

import (
	"encoding/json"
	"strconv"
	. "testing"
	"time"

	"gopkg.in/mgo.v2/bson"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimestamp(t *T) {
	ts := TimestampNow()

	tsJ, err := json.Marshal(&ts)
	require.Nil(t, err)

	// tsJ should basically be an integer
	tsF, err := strconv.ParseFloat(string(tsJ), 64)
	require.Nil(t, err)
	assert.True(t, tsF > 0)

	ts2 := TimestampFromFloat64(tsF)
	assert.Equal(t, ts, ts2)

	var ts3 Timestamp
	err = json.Unmarshal(tsJ, &ts3)
	require.Nil(t, err)
	assert.Equal(t, ts, ts3)
}

// Make sure that we can take in a non-float from json
func TestTimestampMarshalInt(t *T) {
	now := time.Now()
	tsJ := []byte(strconv.FormatInt(now.Unix(), 10))
	var ts Timestamp
	err := json.Unmarshal(tsJ, &ts)
	require.Nil(t, err)
	assert.Equal(t, ts.Float64(), float64(now.Unix()))
}

type Foo struct {
	T Timestamp `json:"timestamp" bson:"t"`
}

func TestTimestampJSON(t *T) {
	now := TimestampNow()
	in := Foo{now}
	b, err := json.Marshal(in)
	require.Nil(t, err)
	assert.NotEmpty(t, b)

	var out Foo
	err = json.Unmarshal(b, &out)
	require.Nil(t, err)
	assert.Equal(t, in, out)
}

func TestTSJSONNull(t *T) {
	// TODO test that marshaling empty foo marshals into timestamp:null
	{
		var foo Foo
		require.Nil(t, json.Unmarshal([]byte(`{"timestamp":null}`), &foo))
		assert.True(t, foo.T.IsZero())
		assert.False(t, foo.T.IsUnixZero())
	}

	// TODO test that marshaling foo with unix 0 marshals into timestamp:0
	{
		var foo Foo
		require.Nil(t, json.Unmarshal([]byte(`{"timestamp":0}`), &foo))
		assert.False(t, foo.T.IsZero())
		assert.True(t, foo.T.IsUnixZero())
	}
}

func TestTimestampZero(t *T) {
	var ts Timestamp
	assert.True(t, ts.IsZero())
	assert.False(t, ts.IsUnixZero())
	tsf := timeToFloat(ts.Time)
	assert.Zero(t, tsf)

	ts = TimestampFromFloat64(0)
	assert.False(t, ts.IsZero())
	assert.True(t, ts.IsUnixZero())
	tsf = timeToFloat(ts.Time)
	assert.Zero(t, tsf)
}

func TestTimestampBSON(t *T) {
	// BSON only supports up to millisecond precision, but even if we keep that
	// many it kinda gets messed up due to rounding errors. So we just give it
	// one with second precision
	now := TimestampFromInt64(time.Now().Unix())

	in := Foo{now}
	b, err := bson.Marshal(in)
	require.Nil(t, err)
	assert.NotEmpty(t, b)

	var out Foo
	err = bson.Unmarshal(b, &out)
	require.Nil(t, err)
	assert.Equal(t, in, out)
}
