package protos

import "github.com/golang/protobuf/ptypes/timestamp"
import "time"

func CreateUtcTimestamp() *timestamp.Timestamp {
	return ConvertToTimestamp(time.Now().UTC())
}

func ConvertToTimestamp(t time.Time) *timestamp.Timestamp {
	secs := t.Unix()
	nanos := int32(t.UnixNano() - (secs * 1000000000))
	return &(timestamp.Timestamp{Seconds: secs, Nanos: nanos})
}

func GetUnixTime(ts *timestamp.Timestamp) time.Time {

	if ts == nil {
		panic("Have nil timestamp")
	}

	return time.Unix(ts.GetSeconds(), int64(ts.GetNanos()))

}
