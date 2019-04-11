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

// for state syncing, proving the state hash and block number which can be requested
// for syncing in a async process. That is, a syncing request for this state will
// be raised sometime later and ledger ensure that it will be still available in
// high possibility (i.e. not flushed by the updating of current state or other reasons)
func (ledger *Ledger) GetStableSyncingState() (shash []byte, height uint64) {
	sn, height := ledger.snapshots.GetStableSnapshot()
	//0 indicate that ledger still has not any stable state for syncing
	//(may because it just startted up )
	if sn == nil {
		return nil, 0
	}

	return sn.stateHash, height
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

func (s *indexedSnapshot) index() string {
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
	stableHeight     uint64 //the latest number added in sns
	current          *indexedSnapshot
	sns              []*indexedSnapshot
	stateIndex       map[string]*indexedSnapshot
}

const defaultSnapshotTotal = 8
const defaultSnapshotInterval = 16

func initNewLedgerSnapshotManager(odb *db.OpenchainDB, shash []byte, blkheight uint64, config *ledgerConfig) *ledgerHistory {
	lsm := new(ledgerHistory)
	//TODO: read config
	lsm.snapshotInterval = defaultSnapshotInterval
	lsm.sns = make([]*indexedSnapshot, defaultSnapshotTotal)
	lsm.stateIndex = make(map[string]*indexedSnapshot)

	if blkheight > 0 {
		lsm.beginIntervalNum = (blkheight - 1) / uint64(lsm.snapshotInterval)
		lsm.currentHeight = blkheight
		lsm.current = &indexedSnapshot{odb.GetSnapshot(), shash}
	}

	lsm.db = odb

	return lsm
}

func (lh *ledgerHistory) Release() {
	for _, sn := range lh.sns {
		sn.Release()
	}
}

func (lh *ledgerHistory) GetCurrentSnapshot() *indexedSnapshot {
	lh.RLock()
	defer lh.RUnlock()

	return lh.current
}

func (lh *ledgerHistory) GetStableSnapshot() (*indexedSnapshot, uint64) {
	lh.RLock()
	defer lh.RUnlock()
	if lh.stableHeight == 0 {
		return nil, 0
	}
	sni, blkn := lh.historyIndex(lh.stableHeight)
	if sni == -1 || blkn != lh.stableHeight {
		ledgerLogger.Errorf("Can not found corresponding stable snapshot, indicate %d but get [%d, %d]", lh.stableHeight, sni, blkn)
		return nil, 0
	} else {
		return lh.sns[sni], lh.stableHeight
	}
}

func (lh *ledgerHistory) GetSnapshot(blknum uint64) (*indexedSnapshot, uint64) {
	lh.RLock()
	defer lh.RUnlock()

	sni, blkn := lh.historyIndex(blknum)
	if sni == -1 {
		return lh.current, blkn
	} else {
		return lh.sns[sni], blkn
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

	if currentNum := lh.currentHeight - 1; currentNum%uint64(lh.snapshotInterval) == 0 {
		//can keep this snapshot
		//but check index not occupied first, thought rare ...
		if _, ok := lh.stateIndex[lh.current.index()]; !ok {
			sec := currentNum / uint64(lh.snapshotInterval)
			indx := int(sec % uint64(len(lh.sns)))

			if s := lh.sns[indx]; s != nil {
				delete(lh.stateIndex, s.index())
				s.Release()
			}
			lh.sns[indx] = lh.current
			lh.stateIndex[lh.current.index()] = lh.current
			lh.stableHeight = currentNum

			ledgerLogger.Debugf("Cache snapshot of %d at %d", lh.currentHeight-1, indx)
			if lh.beginIntervalNum+uint64(len(lh.sns)) <= sec && lh.currentHeight > 0 {
				lh.beginIntervalNum = sec - uint64(len(lh.sns)-1)
				ledgerLogger.Debugf("Move update begin to %d", lh.beginIntervalNum)
			}
		} else {
			lh.current.Release()
			ledgerLogger.Warningf("gave up caching snapshot on blk %d because of (extremely rare) index conflicting", currentNum)
		}

	} else {
		lh.current.Release()
	}
	lh.currentHeight = blknum + 1
	lh.current = &indexedSnapshot{lh.db.GetSnapshot(), shash}
}
