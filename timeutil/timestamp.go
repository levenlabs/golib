package timeutil

import (
	"fmt"
	"strconv"
	"time"

	"gopkg.in/mgo.v2/bson"
)

// Credit to Gal Ben-Heim, from this post:
// https://medium.com/coding-and-deploying-in-the-cloud/time-stamps-in-golang-abcaf581b72f
// (although the tests were written by us, and the Timestamp type is slightly
// modified)

// Timestamp is a wrapper around time.Time which adds methods to marshal and
// unmarshal the value as a unix timestamp instead of a formatted string
type Timestamp struct {
	time.Time
}

// MarshalJSON returns the JSON representation of the Timestamp as an integer.
// It never returns an error
func (t *Timestamp) MarshalJSON() ([]byte, error) {
	ts := t.Unix()
	stamp := fmt.Sprint(ts)

	return []byte(stamp), nil
}

// UnmarshalJSON takes a JSON integer and converts it into a Timestamp, or
// returns an error if this can't be done
func (t *Timestamp) UnmarshalJSON(b []byte) error {
	ts, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return err
	}

	*t = Timestamp{time.Unix(ts, 0)}

	return nil
}

// GetBSON implements the bson.Getter interface
func (t Timestamp) GetBSON() (interface{}, error) {
	return t.Time, nil
}

// SetBSON implements the bson.Setter interface
func (t *Timestamp) SetBSON(raw bson.Raw) error {
	return raw.Unmarshal(&t.Time)
}

// TimestampNow is simply a wrapper around time.Now which returns a Timestamp.
// The underlying time.Time will not have any resolution under a second, to make
// using Timestamp in tests easier
func TimestampNow() Timestamp {
	return TimestampFromInt64(time.Now().Unix())
}

// TimestampFromInt64 returns a Timestamp equal to the given int64, assuming it
// too is a unix timestamp
func TimestampFromInt64(ts int64) Timestamp {
	return Timestamp{time.Unix(ts, 0)}
}
