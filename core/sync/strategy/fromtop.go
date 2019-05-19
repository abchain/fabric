package syncstrategy

import (
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/sync"
	"golang.org/x/net/context"
)

/*
this strategy sync ledger to a specified height of block and the most close world state,
it also persist current task to help accelerating a task restarted

the main process of this strategy has a unit-test in the sync module (see sync_state_test.go)

prerequisite:
  * a block height and its corresponding hash
  * the block must include statehash
*/

type syncStrategyFromTop struct {
	*SyncEntry
	ledger *ledger.Ledger

	blockCheckpoints map[uint64][]byte
}

func (se *SyncEntry) FromTopStrategy(l *ledger.Ledger, h uint64, blockh []byte) *syncStrategyFromTop {

	ret := &syncStrategyFromTop{
		SyncEntry:        se,
		ledger:           l,
		blockCheckpoints: map[uint64][]byte{h: blockh},
	}

	return ret
}

func (s *syncStrategyFromTop) Block(ctx context.Context) error {

	blkAgent := ledger.NewBlockAgent(s.ledger)

	//TODO: how to select other commit scheme?
	blockCli := sync.NewBlockSyncClient(blkAgent.SyncCommitBlock, sync.PruneSyncPlan(
		s.ledger, sync.CheckpointToSyncPlan(s.blockCheckpoints)))

	err := sync.ExecuteSyncTask(ctx, blockCli, s.sstub)
	if err != nil {
		logger.Errorf("from-to strategy: sync block fail %s", err)
	} else {
		logger.Infof("from-to strategy: sync block done, current ledger has height <%d>", s.ledger.GetBlockchainSize())
	}
	return err

}

func (s *syncStrategyFromTop) Full(ctx context.Context) error {

	if err := s.Block(ctx); err != nil {
		return err
	}

	sdetector := sync.NewStateSyncDetector(s.ledger, 64)
	err := sdetector.DoDetection(s.sstub)
	if err != nil {
		logger.Errorf("from-to strategy: can not obtain state-syncing target: %s", err)
		return err
	}

	syncer, err := ledger.NewSyncAgent(s.ledger, sdetector.Candidate.Height, sdetector.Candidate.State)
	if err != nil {
		logger.Errorf("from-to strategy: create state syncer fail: %s", err)
		return err
	}

	stateCli, endSyncF := sync.NewStateSyncClient(ctx, syncer)
	defer endSyncF()

	err = sync.ExecuteSyncTask(ctx, stateCli, s.sstub)
	if err != nil {
		logger.Errorf("from-to strategy: sync world-state fail: %s", err)
		return err
	}

	err = syncer.FinishTask()
	if err != nil {
		logger.Errorf("from-to strategy: sync world-state has no right result: %s", err)
		return err
	}

	logger.Infof("from-to strategy: sync full state done")
	return nil
}
