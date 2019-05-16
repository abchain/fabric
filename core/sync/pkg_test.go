package sync

import (
	"github.com/abchain/fabric/core/ledger/testutil"
	pb "github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/proto"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	testutil.SetupTestConfig()
	os.Exit(m.Run())
}

func packageMsgHelper() proto.Message {
	return new(pb.SyncMsg)
}
