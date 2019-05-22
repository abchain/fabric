package ledger

import (
	"github.com/abchain/fabric/protos"
)

//state syncing commiter
type PartialSync struct {
	ledger *Ledger
}

func AssignSyncAgent(ledger *Ledger) (PartialSync, error) {
	if !ledger.state.isSyncing() {
		return PartialSync{}, newLedgerError(ErrorTypeInvalidArgument, "state is not syncing")
	}

	return PartialSync{ledger}, nil
}

func NewSyncAgent(ledger *Ledger, blkn uint64, stateHash []byte) (PartialSync, error) {

	err := ledger.state.startSyncState(blkn, stateHash)
	if err != nil {
		return PartialSync{}, err
	}

	return AssignSyncAgent(ledger)
}

func (syncer PartialSync) GetTarget() []byte {
	return syncer.ledger.state.buildingState.statehash
}

func (syncer PartialSync) CleanTask() {
	syncer.ledger.state.buildingState.syncing = nil
}

func (syncer PartialSync) FinishTask() error {
	ledger := syncer.ledger
	if !ledger.state.isSyncing() {
		return newLedgerError(ErrorTypeInvalidArgument, "state is not syncing")
	} else if !ledger.state.buildingState.syncing.IsCompleted() {
		return newLedgerError(ErrorTypeInvalidArgument, "state is not complete")
	}

	ledger.state.buildingState.syncing = nil
	return nil
}

func (syncer PartialSync) AssignTasks() ([]*protos.SyncOffset, error) {
	return syncer.ledger.state.getSyncing().RequiredParts()
}

func (syncer PartialSync) ApplySyncData(data *protos.SyncStateChunk) error {

	ledger := syncer.ledger

	err := ledger.state.preapreForSyncData(data)
	if err != nil {
		return err
	}

	writeBatch := ledger.state.NewWriteBatch()
	defer writeBatch.Destroy()

	ledger.state.persistentSync(writeBatch)
	if err := writeBatch.BatchCommit(); err != nil {
		return err
	}

	ledger.readCache.Lock()
	defer ledger.readCache.Unlock()

	ledger.state.persistentSyncDone()
	return nil
}
