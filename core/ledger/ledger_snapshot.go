package ledger

import (
	"fmt"
	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/core/ledger/statemgmt/state"
	"github.com/abchain/fabric/core/util"
	"github.com/abchain/fabric/protos"
	"sync"
)

type LedgerSnapshot struct {
	l *Ledger
	*db.DBSnapshot
}

func (sledger *LedgerSnapshot) GetParitalRangeIterator(offset *protos.SyncOffset) (statemgmt.PartialRangeIterator, error) {

	partialInf := sledger.l.state.GetDividableState()
	if partialInf == nil {
		return nil, fmt.Errorf("State not support")
	}

	return partialInf.GetPartialRangeIterator(sledger.DBSnapshot)
}

func (sledger *LedgerSnapshot) GetBlockByNumber(blockNumber uint64) (*protos.Block, error) {
	size, err := sledger.GetBlockchainSize()

	if err != nil {
		return nil, err
	}

	if blockNumber >= size {
		return nil, ErrOutOfBounds
	}

	blockBytes, err := sledger.GetFromBlockchainCFSnapshot(encodeBlockNumberDBKey(blockNumber))
	if err != nil {
		return nil, err
	}

	blk, err := bytesToBlock(blockBytes)

	if err != nil {
		return nil, err
	}

	blk = finishFetchedBlock(blk)

	return blk, nil
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
	return sledger.l.state.GetSnapshot(blockHeight-1, sledger.DBSnapshot)
}

func (ledger *Ledger) CreateSyncingSnapshot(shash []byte) (*LedgerSnapshot, error) {

	if blkn, err := ledger.GetBlockNumberByState(shash); err != nil {
		return nil, err
	} else {
		sn, snblkn := ledger.snapshots.GetSnapshot(blkn)
		if snblkn != blkn {
			return nil, fmt.Errorf("request state [%X] is not stable (found blocknumber is %d but not cached)", shash, blkn)
		}

		return &LedgerSnapshot{ledger, sn}, nil
	}
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
	return ledger.snapshots.GetStableIndex()
}

// create a snapshot wrapper for current db
func (ledger *Ledger) CreateSnapshot() *LedgerSnapshot {

	return &LedgerSnapshot{ledger, ledger.snapshots.GetCurrentSnapshot()}
}

// ----- will be deprecated -----
// GetStateSnapshot returns a point-in-time view of the global state for the current block. This
// should be used when transferring the state from one peer to another peer. You must call
// stateSnapshot.Release() once you are done with the snapshot to free up resources.
func (ledger *Ledger) GetStateSnapshot() (*state.StateSnapshot, error) {
	snapshotL := ledger.CreateSnapshot()
	blockHeight, err := snapshotL.GetBlockchainSize()
	if err != nil {
		snapshotL.Release()
		return nil, err
	}
	if 0 == blockHeight {
		snapshotL.Release()
		return nil, fmt.Errorf("Blockchain has no blocks, cannot determine block number")
	}
	return ledger.state.GetSnapshot(blockHeight-1, snapshotL.DBSnapshot)
}

type indexedSnapshot struct {
	*db.DBSnapshot
	stateHash []byte
}

func (s indexedSnapshot) index() string {

	if l := len(s.stateHash); l == 0 {
		return "NIL"
	} else if l < 8 {
		return fmt.Sprintf("%X", s.stateHash)
	} else {
		return fmt.Sprintf("%X", s.stateHash[:8])
	}
}

//maintain a series of snapshot for state querying
type ledgerHistory struct {
	sync.RWMutex
	db               *db.OpenchainDB
	snapshotInterval int
	beginIntervalNum uint64
	currentHeight    uint64
	current          indexedSnapshot
	sns              []*indexedSnapshot
	stableStatus     *protos.StateFilter
}

//we should not keep too many snapshot or scaliablity problem will raise in rocksdb,
//see the doc for detail: https://github.com/facebook/rocksdb/wiki/Snapshot
const defaultSnapshotTotal = 8
const defaultSnapshotInterval = 16

func initNewLedgerSnapshotManager(odb *db.OpenchainDB, shash []byte, blkheight uint64, config *ledgerConfig) *ledgerHistory {
	lsm := new(ledgerHistory)
	//TODO: read config
	lsm.snapshotInterval = defaultSnapshotInterval
	lsm.sns = make([]*indexedSnapshot, defaultSnapshotTotal)
	lsm.stableStatus = new(protos.StateFilter)

	filterEff := lsm.stableStatus.InitAuto(uint(len(lsm.sns)))
	if fr := filterEff * float64(100); fr > float64(1.0) {
		ledgerLogger.Warningf("The false positive ratio of filter is larger than 1% (%f)", fr)
	}

	if blkheight > 0 {
		lsm.beginIntervalNum = (blkheight - 1) / uint64(lsm.snapshotInterval)
		lsm.currentHeight = blkheight
		lsm.current = indexedSnapshot{odb.GetSnapshot(), shash}
	}

	lsm.db = odb

	return lsm
}

func (lh *ledgerHistory) Release() {
	for _, sn := range lh.sns {
		sn.Release()
	}
}

func (lh *ledgerHistory) GetCurrentSnapshot() *db.DBSnapshot {
	lh.RLock()
	defer lh.RUnlock()

	return lh.current.DBSnapshot.Clone()
}

func (lh *ledgerHistory) GetStableIndex() *protos.StateFilter {
	lh.RLock()
	defer lh.RUnlock()

	ret := new(protos.StateFilter)
	ret.Filter = lh.stableStatus.Filter
	ret.HashCounts = lh.stableStatus.HashCounts

	return ret
}

func (lh *ledgerHistory) GetSnapshot(blknum uint64) (*db.DBSnapshot, uint64) {
	lh.RLock()
	defer lh.RUnlock()

	sni, blkn := lh.historyIndex(blknum)
	if sni == -1 {
		return lh.current.DBSnapshot.Clone(), blkn
	} else {
		return lh.sns[sni].DBSnapshot.Clone(), blkn
	}
}

func (lh *ledgerHistory) historyIndex(blknum uint64) (int, uint64) {

	sec := blknum / uint64(lh.snapshotInterval)
	if sec < lh.beginIntervalNum {
		return int(lh.beginIntervalNum % uint64(len(lh.sns))), lh.beginIntervalNum * uint64(lh.snapshotInterval)
	} else {
		//ceiling ....
		if blknum%uint64(lh.snapshotInterval) != 0 {
			sec++
		}

		corblk := sec * uint64(lh.snapshotInterval)
		if corblk >= lh.currentHeight {
			return -1, lh.currentHeight - 1
		}

		return int(sec % uint64(len(lh.sns))), corblk
	}
}

//force update current state
func (lh *ledgerHistory) ForceUpdate() {
	lh.Lock()
	defer lh.Unlock()

	lh.current.Release()
	lh.current.DBSnapshot = lh.db.GetSnapshot()
}

func (lh *ledgerHistory) Update(shash []byte, blknum uint64) {
	lh.Lock()
	defer lh.Unlock()

	if blknum < lh.currentHeight {
		ledgerLogger.Errorf("Try update a less blocknumber %d (current %d), not accept", blknum, lh.currentHeight-1)
		return
	}

	if currentNum := lh.currentHeight - 1; lh.currentHeight > 0 && currentNum%uint64(lh.snapshotInterval) == 0 {
		//can keep this snapshot
		sec := currentNum / uint64(lh.snapshotInterval)
		indx := int(sec % uint64(len(lh.sns)))

		if s := lh.sns[indx]; s != nil {
			s.Release()
		}
		newsnapshot := &indexedSnapshot{lh.current.DBSnapshot, lh.current.stateHash}
		lh.sns[indx] = newsnapshot

		ledgerLogger.Debugf("Cache snapshot of %d at %d", lh.currentHeight-1, indx)
		if lh.beginIntervalNum+uint64(len(lh.sns)) <= sec && lh.currentHeight > 0 {
			lh.beginIntervalNum = sec - uint64(len(lh.sns)-1)
			ledgerLogger.Debugf("Move update begin to %d", lh.beginIntervalNum)
		}

		//reset bloom filter
		lh.stableStatus.ResetFilter()
		for _, sn := range lh.sns {
			if sn != nil && len(sn.stateHash) != 0 {
				lh.stableStatus.Add(sn.stateHash)
			}
		}

	} else {
		lh.current.Release()
	}
	lh.currentHeight = blknum + 1
	lh.current = indexedSnapshot{lh.db.GetSnapshot(), shash}
}
