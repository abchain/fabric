package ledger

import (
	"fmt"
	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/core/ledger/statemgmt/state"
	"github.com/abchain/fabric/core/util"
	"github.com/abchain/fabric/protos"
)

type LedgerSnapshot struct {
	stateM statemgmt.SnapshotState
	*db.DBSnapshot
}

func (sledger *LedgerSnapshot) Clone() *LedgerSnapshot {
	return &LedgerSnapshot{sledger.stateM, sledger.DBSnapshot.Clone()}
}

func (sledger *LedgerSnapshot) GetParitalRangeIterator() (statemgmt.PartialRangeIterator, error) {

	return sledger.stateM.GetPartialRangeIterator(sledger.DBSnapshot)
}

//notice the indexs in snapshot is not ensured to be latest (e.g. in the asyncindexers)
func (sledger *LedgerSnapshot) GetBlockNumberByState(hash []byte) (uint64, error) {
	return fetchBlockNumberByStateHashFromSnapshot(sledger.DBSnapshot, hash)
}

func (sledger *LedgerSnapshot) GetRawBlockByNumber(blockNumber uint64) (*protos.Block, error) {
	blockBytes, err := sledger.GetFromBlockchainCFSnapshot(encodeBlockNumberDBKey(blockNumber))
	if err != nil {
		return nil, err
	}

	blk, err := bytesToBlock(blockBytes)

	if err != nil {
		return nil, err
	}
	return blk, nil
}

func (sledger *LedgerSnapshot) GetBlockByNumber(blockNumber uint64) (*protos.Block, error) {
	size, err := sledger.GetBlockchainSize()

	if err != nil {
		return nil, err
	}

	if blockNumber >= size {
		return nil, newLedgerError(ErrorTypeOutOfBounds, fmt.Sprintf("block number %d too large (%d)", blockNumber, size))
	}

	blk, err := sledger.GetRawBlockByNumber(blockNumber)

	if err != nil {
		return nil, err
	}

	blk = finishFetchedBlock(blk)

	return blk, nil
}

// TODO: make the snapshot version for testBlockExistedRangeSafe
// func (sledger *LedgerSnapshot) TestExistedBlockRange(blockNumber uint64) uint64 {

// 	return 0
// }

//test how height the blocks we have obtained continuously
func (sledger *LedgerSnapshot) TestContinuouslBlockRange() (uint64, error) {

	bytes, err := sledger.GetFromBlockchainCFSnapshot(continuousblockCountKey)
	if err != nil {
		return 0, err
	}
	return bytesToBlockNumber(bytes), nil
}

func (sledger *LedgerSnapshot) GetBlockchainSize() (uint64, error) {

	bytes, err := sledger.GetFromBlockchainCFSnapshot(blockCountKey)
	if err != nil {
		return 0, err
	}
	return bytesToBlockNumber(bytes), nil
}

func (sledger *LedgerSnapshot) GetStateDelta(blockNumber uint64) (*statemgmt.StateDelta, error) {

	size, err := sledger.GetBlockchainSize()

	if err != nil {
		return nil, err
	}

	if blockNumber >= size {
		return nil, ErrOutOfBounds
	}

	stateDeltaBytes, err := sledger.GetFromStateDeltaCFSnapshot(util.EncodeUint64(blockNumber))
	if err != nil {
		return nil, err
	}
	if len(stateDeltaBytes) == 0 {
		return nil, nil
	}
	stateDelta := statemgmt.NewStateDelta()
	err = stateDelta.Unmarshal(stateDeltaBytes)
	return stateDelta, err
}

func (sledger *LedgerSnapshot) GetStateSnapshot() (*state.StateSnapshot, error) {

	blockHeight, err := sledger.GetBlockchainSize()
	if err != nil {
		return nil, err
	}
	if 0 == blockHeight {
		return nil, fmt.Errorf("Blockchain has no blocks, cannot determine block number")
	}

	itr, err := sledger.stateM.GetStateSnapshotIterator(sledger.DBSnapshot)
	if err != nil {
		return nil, err
	}
	return state.NewStateSnapshot(blockHeight-1, itr, sledger.DBSnapshot), nil
}

func (ledger *Ledger) GetLedgerStatus() *protos.LedgerState {

	ledger.readCache.RLock()
	defer ledger.readCache.RUnlock()
	ret := new(protos.LedgerState)
	ret.States = ledger.snapshots.GetStableIndex()
	ret.Height = ledger.blockchain.getContinuousBlockHeight() + 1
	if ret.Height > 0 {
		blk, _ := ledger.blockchain.getRawBlock(ret.Height - 1)
		ret.HeadBlock, _ = blk.GetHash()
	}

	return ret
}

func (ledger *Ledger) CreateSyncingSnapshot(shash []byte) (*LedgerSnapshot, error) {

	ledger.readCache.RLock()
	defer ledger.readCache.RUnlock()
	blkn, err := ledger.index.fetchBlockNumberByStateHash(shash)
	if err != nil {
		return nil, err
	}

	//check current first
	if blkn+1 == ledger.snapshots.currentHeight {
		return &LedgerSnapshot{ledger.state.GetSnapshotManager(), ledger.snapshots.GetSnapshotCurrent()}, nil
	}

	if sn, snblkn := ledger.snapshots.GetSnapshot(blkn); sn != nil {
		if snblkn != blkn {
			sn.Release()
			return nil, fmt.Errorf("request state [%X] is not stable (found blocknumber is %d but not cached [closed set is %d])", shash, blkn, snblkn)
		}
		return &LedgerSnapshot{ledger.state.GetSnapshotManager(), sn}, nil
	}
	return nil, fmt.Errorf("request state [%X] is not avaliable", shash)
}

// for state syncing, proving the state hash and block number which can be requested
// for syncing in a async process. That is, a syncing request for this state will
// be raised sometime later and ledger ensure that it will be still available in
// high possibility (i.e. not flushed by the updating of current state or other reasons)

// TODO: use snapshot manager for syncing is not complete correct beacuse syncer
// may lost their available state because all nodes have trunced the target state,
// (consider a node is syncing some state A from another node B and B is off-line
// in the process, and time has passed so no other nodes will keep the state which A
// wish to sync) later we should use checkpoint for state syncing and a long hold node
// should be provided for alleviating this problem
func (ledger *Ledger) GetStableSyncingState() *protos.StateFilter {
	ledger.readCache.RLock()
	defer ledger.readCache.RUnlock()
	return ledger.snapshots.GetStableIndex()
}

// create a snapshot wrapper for current db, the corresponding blocknumber is also returned
// for verifing
// this snapshot is not enusred to sync with state and should be only used for block syncing
// and indexing should be avoiding used
func (ledger *Ledger) CreateSnapshot() (*LedgerSnapshot, uint64) {

	ledger.readCache.RLock()
	defer ledger.readCache.RUnlock()

	if ledger.blockchain.getSize() == 0 {
		//no snapshot because we have no block yet
		return nil, 0
	}

	return &LedgerSnapshot{ledger.state.GetSnapshotManager(), ledger.snapshots.db.GetSnapshot()},
		ledger.blockchain.getSize() - 1
}

// ----- will be deprecated -----
// GetStateSnapshot returns a point-in-time view of the global state for the current block. This
// should be used when transferring the state from one peer to another peer. You must call
// stateSnapshot.Release() once you are done with the snapshot to free up resources.
func (ledger *Ledger) GetStateSnapshot() (*state.StateSnapshot, error) {
	snapshotL, blockHeight := ledger.CreateSnapshot()
	if snapshotL == nil {
		return nil, fmt.Errorf("Blockchain has no blocks")
	}
	return ledger.state.GetSnapshot(blockHeight, snapshotL.DBSnapshot)
}

func indexState(stateHash []byte) string {

	if l := len(stateHash); l == 0 {
		return "#NIL"
	} else if l < 20 { //the first 160bit is used
		return fmt.Sprintf("#%X", stateHash)
	} else {
		return fmt.Sprintf("#%X", stateHash[:20])
	}
}

//maintain a series of snapshot for state querying
type ledgerHistory struct {
	db               *db.OpenchainDB
	snapshotInterval int
	beginIntervalNum uint64
	currentHeight    uint64
	currentState     []byte
	snsIndexed       [][]byte
	stableStatus     *protos.StateFilter
}

//we should not keep too many snapshot or scaliablity problem will raise in rocksdb,
//see the doc for detail: https://github.com/facebook/rocksdb/wiki/Snapshot
const defaultSnapshotTotal = 8
const defaultSnapshotInterval = 16
const currentSNTag = "#current"

func initNewLedgerSnapshotManager(odb *db.OpenchainDB, s *stateWrapper, config *ledgerConfig) *ledgerHistory {
	lsm := new(ledgerHistory)
	//TODO: read config
	lsm.snapshotInterval = defaultSnapshotInterval
	lsm.snsIndexed = make([][]byte, defaultSnapshotTotal)
	lsm.stableStatus = new(protos.StateFilter)

	filterEff := lsm.stableStatus.InitAuto(uint(len(lsm.snsIndexed)))
	if fr := filterEff * float64(100); fr > float64(1.0) {
		ledgerLogger.Warningf("The false positive ratio of filter is larger than 1% (%f)", fr)
	}

	lsm.db = odb
	if s.cache.refHeight > 0 {
		lsm.currentHeight = s.cache.refHeight
		curBlk := lsm.currentHeight - 1
		lsm.beginIntervalNum = curBlk / uint64(lsm.snapshotInterval)
		shash := s.cache.refHash
		if int(curBlk)%lsm.snapshotInterval == 0 {
			ledgerLogger.Debugf("cached state at the beginning [%X]@%d", shash, curBlk)
			lsm.currentState = shash
			lsm.snapshotCurrent(shash)
		}

		if oldSN := odb.ManageSnapshot(currentSNTag, odb.GetSnapshot()); oldSN != nil {
			oldSN.Release()
		}
	}

	return lsm
}

func (lh *ledgerHistory) GetStableIndex() *protos.StateFilter {

	ret := new(protos.StateFilter)
	ret.Filter = lh.stableStatus.Filter
	ret.HashCounts = lh.stableStatus.HashCounts

	return ret
}

func (lh *ledgerHistory) GetSnapshotCurrent() *db.DBSnapshot {
	return lh.db.GetManagedSnapshot(currentSNTag)
}

func (lh *ledgerHistory) GetSnapshot(blknum uint64) (sn *db.DBSnapshot, blkn uint64) {
	sni, blkn := lh.historyIndex(blknum)
	if sni == -1 {
		return nil, blkn
	} else if pickS := lh.snsIndexed[sni]; len(pickS) > 0 {
		return lh.db.GetManagedSnapshot(indexState(pickS)), blkn
	} else {
		return nil, blkn
	}
}

func (lh *ledgerHistory) historyIndex(blknum uint64) (int, uint64) {

	if lh.currentHeight <= 1 {
		//we have no history yet
		return -1, blknum
	} else if blknum+1 >= lh.currentHeight {
		blknum = lh.currentHeight - 2
	}

	sec := blknum / uint64(lh.snapshotInterval)
	if sec < lh.beginIntervalNum {
		return -1, blknum
	} else {
		corblk := sec * uint64(lh.snapshotInterval)
		return int(sec % uint64(len(lh.snsIndexed))), corblk
	}

}

func (lh *ledgerHistory) snapshotCurrent(shash []byte) {
	duplicatedSn := lh.db.ManageSnapshot(indexState(shash), lh.db.GetSnapshot())
	if duplicatedSn != nil {
		ledgerLogger.Warningf("We have duplidated snapshot in state %X and release it", shash)
		duplicatedSn.Release()
	}
	//also add current state into bloom filter
	lh.stableStatus.Add(shash)
}

func (lh *ledgerHistory) UpdateR(blknum uint64, shash []byte) {
	lh.Update(shash, blknum)
}

func (lh *ledgerHistory) Update(shash []byte, blknum uint64) {

	//out-of-order updating is allowed only if blknum is not exceed the current bound
	sec := blknum / uint64(lh.snapshotInterval)
	if sec < lh.beginIntervalNum {
		ledgerLogger.Errorf("Try update too small blocknumber %d (current least bound is %d), not accept", blknum, lh.beginIntervalNum*uint64(lh.snapshotInterval))
		return
	} else if blknum+1 == lh.currentHeight {
		ledgerLogger.Errorf("duplicated blocknumber %d, not accept", blknum)
		return
	}

	ledgerLogger.Debugf("updating state [%X]@%d (now height %d)", shash, blknum, lh.currentHeight)

	if blknum < lh.currentHeight {
		//to see if we can cached it, and exit
		//notice: it is ok when the modulo is expected to be 0, but not other integer
		//(go get minus result for a negitave integer being modulo)
		if int(blknum)%lh.snapshotInterval == 0 {
			indx := int(sec % uint64(len(lh.snsIndexed)))
			if s := lh.snsIndexed[indx]; len(s) == 0 {
				lh.snapshotCurrent(shash)
				lh.snsIndexed[indx] = shash
				ledgerLogger.Debugf("kept out-of-order state [%X]@%d", shash, blknum)
			} //or it should has been cached, we just duplicated, TODO: sanity check?
		}
		return
	}

	//so we forwarded
	cacheCurrent := int(blknum)%lh.snapshotInterval == 0
	//if current blknum has "push" the history range forward, we still have some works
	//to do
	if lh.beginIntervalNum+uint64(len(lh.snsIndexed)) <= sec {
		oldBegin := lh.beginIntervalNum
		if !cacheCurrent {
			lh.beginIntervalNum = sec - uint64(len(lh.snsIndexed)) + 1
		} else {
			//NOTICE: this is a trick: we cache the "edge" block at currentState
			//and not put it into history now, so we will not "waste" a slot
			//which will point to current height
			lh.beginIntervalNum = sec - uint64(len(lh.snsIndexed))
		}
		if oldBegin < lh.beginIntervalNum {
			ledgerLogger.Debugf("begin range has forward to [%d] (%d in section)", lh.beginIntervalNum*uint64(lh.snapshotInterval), lh.beginIntervalNum)
			for i := oldBegin; i < lh.beginIntervalNum; i++ {
				//now we must clean the current snapshot we kept in the old history
				dropS := lh.snsIndexed[int(i)%len(lh.snsIndexed)]
				lh.snsIndexed[int(i)%len(lh.snsIndexed)] = nil
				lh.db.UnManageSnapshot(indexState(dropS))
				ledgerLogger.Debugf("history snapshot [%X]@<%d> has been dropped", dropS, i)
			}
			//rebuild bloom filter
			lh.stableStatus.ResetFilter()
			for _, h := range lh.snsIndexed {
				if len(h) > 0 {
					lh.stableStatus.Add(h)
				}
			}
		}
	}
	//now, keep the cache of previous "current" if we have one
	if len(lh.currentState) > 0 {
		//keep last "current" snapshot we have cached first
		sec := (lh.currentHeight - 1) / uint64(lh.snapshotInterval)
		//CAUTION: we may forward too fast so the cached has to be drop ...
		if sec < lh.beginIntervalNum {
			ledgerLogger.Infof("we have dropped a cached snapshot [%X]", lh.currentState)
			lh.db.UnManageSnapshot(indexState(lh.currentState))
		} else {
			indx := int(sec % uint64(len(lh.snsIndexed)))
			//sanity check
			if len(lh.snsIndexed[indx]) > 0 {
				panic("wrong code: forwarding do not clean current slot yet")
			}
			lh.snsIndexed[indx] = lh.currentState
			ledgerLogger.Debugf("we have kept a new snapshot [%X]", lh.currentState)
		}
		lh.currentState = nil
	}

	if cacheCurrent {
		lh.snapshotCurrent(shash)
		lh.currentState = shash
		ledgerLogger.Debugf("cache current snapshot [%X]@%d", lh.currentState, blknum)
	}
	//also cache another copy of current db
	oldSN := lh.db.ManageSnapshot(currentSNTag, lh.db.GetSnapshot())
	if oldSN != nil {
		oldSN.Release()
	}
	lh.currentHeight = blknum + 1

}
