package syncstrategy

import (
	_ "github.com/abchain/fabric/core/sync"
	pb "github.com/abchain/fabric/protos"
	"github.com/op/go-logging"
	"github.com/spf13/viper"
	"golang.org/x/net/context"
)

var logger = logging.MustGetLogger("syncstrategy")

type SyncEntry struct {
	sstub *pb.StreamStub
}

func GetSyncStrategyEntry(sstub *pb.StreamStub) *SyncEntry {
	return &SyncEntry{sstub: sstub}
}

func (se *SyncEntry) Configure(vp *viper.Viper) error {
	return nil
}

//a base implement, should have no alter way...
func (se *SyncEntry) SyncTransactions(ctx context.Context) error {

	return nil
}
