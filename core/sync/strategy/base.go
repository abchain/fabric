package syncstrategy

import (
	"github.com/abchain/fabric/core/sync"
	"github.com/op/go-logging"
	"github.com/spf13/viper"
)

var logger = logging.MustGetLogger("syncstrategy")

type SyncEntry struct {
	stub *sync.SyncStub
}

func GetSyncStrategyEntry(stub *sync.SyncStub) *SyncEntry {
	return &SyncEntry{stub: stub}
}

func (se *SyncEntry) Configure(vp *viper.Viper) error {
	return nil
}
