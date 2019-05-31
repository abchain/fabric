package syncstrategy

import (
	"github.com/abchain/fabric/core/sync"
	pb "github.com/abchain/fabric/protos"
	"github.com/op/go-logging"
	"github.com/spf13/viper"
	"golang.org/x/net/context"
	"time"
)

var logger = logging.MustGetLogger("syncstrategy")

//any strategy applier has two simple entrances: one for blocks only and another for blocks and the full
//world-state, by anymeans except for executing txs in blocks (e.g. sync state directly or statedelta)
//each method will just block until the process finish
//strategy may left partial syncing results after encounter any error
type Applier interface {
	Blocks(context.Context) error
	Full(context.Context) error
}

type SyncEntry struct {
	sstub *pb.StreamStub

	options struct {
		//timeout is only used for simple task like synctransactions, there is no
		//timeout (unless user set it manually in context) for a sync strategy
		//but it will be also a reference in sync strategy (for example, the default
		//interval process should wait between two retries)
		defaultTimeout int
	}
}

func GetSyncStrategyEntry(sstub *pb.StreamStub) *SyncEntry {
	const defaultTimeout = 30
	ret := &SyncEntry{sstub: sstub}
	ret.options.defaultTimeout = defaultTimeout
	return ret
}

func (se *SyncEntry) Configure(vp *viper.Viper) error {
	return nil
}

//a base implement, should have no alter way...
func (se *SyncEntry) SyncTransactions(ctx context.Context, txids []string) ([]*pb.Transaction, []string) {

	cli := sync.NewTxSyncClient(sync.DefaultClientOption(), txids)
	wctx, endf := context.WithTimeout(ctx, time.Duration(se.options.defaultTimeout)*time.Second)
	defer endf()

	err := sync.ExecuteSyncTask(wctx, cli, se.sstub)

	outtx, outreside := cli.Result()

	if err != nil {
		logger.Errorf("sync transaction fail: %s (%d done in %d)", err, len(txids), len(outtx))
	}

	logger.Info("sync transaction finished for %d txs", len(outtx))

	return outtx, outreside
}
