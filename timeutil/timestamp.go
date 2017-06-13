package timeutil

import (
	"bytes"
	"strconv"
	"time"

	"gopkg.in/mgo.v2/bson"
)

var unixZero = time.Unix(0, 0)

func timeToFloat(t time.Time) float64 {
	// If time.Time is the empty value, UnixNano will return the farthest back
	// timestamp a float can represent, which is some large negative value. We
	// compromise and call it zero
	if t.IsZero() {
		return 0
	}
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

var jsonNull = []byte("null")

// MarshalJSON returns the JSON representation of the Timestamp as an integer.
// It never returns an error
func (t Timestamp) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return jsonNull, nil
	}

	ts := timeToFloat(t.Time)
	stamp := strconv.FormatFloat(ts, 'f', -1, 64)
	return []byte(stamp), nil
}

// UnmarshalJSON takes a JSON integer and converts it into a Timestamp, or
// returns an error if this can't be done
func (t *Timestamp) UnmarshalJSON(b []byte) error {
	// since 0 is a valid timestamp we can't use that to mean "unset", so we
	// take null to mean unset instead
	if bytes.Equal(b, jsonNull) {
		t.Time = time.Time{}
		return nil
	}

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

// IsUnixZero returns true if the timestamp is equal to the unix zero timestamp,
// representing 1/1/1970. This is different than checking if the timestamp is
// the empty value (which should be done with IsZero)
func (t Timestamp) IsUnixZero() bool {
	return t.Equal(unixZero)
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

// TimestampFromString attempts to parse the string as a float64, and then
// passes that into TimestampFromFloat64, returning the result
func TimestampFromString(ts string) (Timestamp, error) {
	f, err := strconv.ParseFloat(ts, 64)
	if err != nil {
		return Timestamp{}, err
	}
	return TimestampFromFloat64(f), nil
}
