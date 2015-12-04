package timeutil

import (
	"encoding/json"
	"strconv"
	. "testing"

	"gopkg.in/mgo.v2/bson"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimestamp(t *T) {
	ts := TimestampNow()

	tsJ, err := json.Marshal(&ts)
	require.Nil(t, err)

	// tsJ should basically be an integer
	tsI, err := strconv.ParseInt(string(tsJ), 10, 64)
	require.Nil(t, err)
	assert.True(t, tsI > 0)

	ts2 := TimestampFromInt64(tsI)
	assert.Equal(t, ts, ts2)

	var ts3 Timestamp
	err = json.Unmarshal(tsJ, &ts3)
	require.Nil(t, err)
	assert.Equal(t, ts, ts3)
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

func TestTimestampBSON(t *T) {
	now := TimestampNow()
	in := Foo{now}
	b, err := bson.Marshal(in)
	require.Nil(t, err)
	assert.NotEmpty(t, b)

	var out Foo
	err = bson.Unmarshal(b, &out)
	require.Nil(t, err)
	assert.Equal(t, in, out)
}
