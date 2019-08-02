package protos

import (
	"github.com/golang/protobuf/ptypes/timestamp"
	"testing"
	"time"
)

func Test_TimeConvertion(t *testing.T) {

	valT := time.Now()
	valTs := ConvertToTimestamp(valT)
	valTts := GetUnixTime(valTs)

	if !valT.Equal(valTts) {
		t.Fatal("Not equal after convert: %s vs %s", valT, valTts)
	}

	yaTs := &timestamp.Timestamp{Seconds: 999999, Nanos: 99999}
	yaTts := GetUnixTime(yaTs)
	yaTtts := ConvertToTimestamp(yaTts)

	if yaTs.Seconds != yaTtts.Seconds || yaTs.Nanos != yaTtts.Nanos {
		t.Fatalf("Not equal after convert timestamp: %v vs %v", yaTs, yaTtts)
	}

}
