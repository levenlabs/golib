package timeutil

import (
	"strconv"
	"time"

	"gopkg.in/mgo.v2/bson"
)

func timeToFloat(t time.Time) float64 {
	return float64(t.UnixNano()) / 1e9
}

// Timestamp is a wrapper around time.Time which adds methods to marshal and
// unmarshal the value as a unix timestamp instead of a formatted string
type Timestamp struct {
	time.Time
}

// String returns the string representation of the Timestamp, in the form of a
// floating point form of the time as a unix timestamp
func (t Timestamp) String() string {
	ts := timeToFloat(t.Time)
	return strconv.FormatFloat(ts, 'f', -1, 64)
}

// MarshalJSON returns the JSON representation of the Timestamp as an integer.
// It never returns an error
func (t Timestamp) MarshalJSON() ([]byte, error) {
	ts := timeToFloat(t.Time)
	stamp := strconv.FormatFloat(ts, 'f', -1, 64)

	return []byte(stamp), nil
}

// UnmarshalJSON takes a JSON integer and converts it into a Timestamp, or
// returns an error if this can't be done
func (t *Timestamp) UnmarshalJSON(b []byte) error {
	ts, err := strconv.ParseFloat(string(b), 64)
	if err != nil {
		return err
	}

	*t = TimestampFromFloat64(ts)
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

// Float64 returns the float representation of the timestamp in seconds.
func (t Timestamp) Float64() float64 {
	return timeToFloat(t.Time)
}

// TimestampNow is simply a wrapper around time.Now which returns a Timestamp.
func TimestampNow() Timestamp {
	return TimestampFromFloat64(timeToFloat(time.Now()))
}

// TimestampFromInt64 returns a Timestamp equal to the given int64, assuming it
// too is a unix timestamp
func TimestampFromInt64(ts int64) Timestamp {
	return Timestamp{time.Unix(ts, 0)}
}

// TimestampFromFloat64 returns a Timestamp equal to the given float64, assuming
// it too is a unix timestamp. The float64 is interpreted as number of seconds,
// with everything after the decimal indicating milliseconds, microseconds, and
// nanoseconds
func TimestampFromFloat64(ts float64) Timestamp {
	secs := int64(ts)
	nsecs := int64((ts - float64(secs)) * 1e9)
	return Timestamp{time.Unix(secs, nsecs)}
}
