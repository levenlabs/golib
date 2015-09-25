package timeutil

import (
	"encoding/json"
	"strconv"
	. "testing"

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
