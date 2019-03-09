package buckettree

import (
	"bytes"
	"fmt"
	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/protos"
)

type syncProcess struct {
	*StateImpl
	targetStateHash []byte
	current         *protos.BucketTreeOffset
	syncLevels		[]int
	curLevelIndex	int
	metadataTreeDelta   *bucketTreeDelta // metadata
}


const partialStatusKeyPrefixByte = byte(16)

func checkSyncProcess(parent *StateImpl) *syncProcess {
	dbItr := parent.GetIterator(db.StateCF)
	defer dbItr.Close()

	for ; dbItr.Valid() && dbItr.ValidForPrefix([]byte{partialStatusKeyPrefixByte}); dbItr.Next() {

		targetStateHash := statemgmt.Copy(dbItr.Key().Data())[1:]
		data := &protos.SyncOffset{Data: statemgmt.Copy(dbItr.Value().Data())}
		offset, err := data.Unmarshal2BucketTree()

		if err == nil {

			//TODO: should verify the partial data ...
			logger.Info("Restore sync task to target [%x]", targetStateHash)
			return &syncProcess{
				StateImpl:       parent,
				targetStateHash: targetStateHash,
				current:         offset,
			}
		}

		logger.Infof("Recorded sync state [%x] is invalid: %s", targetStateHash, err)
		parent.DeleteKey(db.StateCF, dbItr.Key().Data())
	}

	return nil
}

func newSyncProcess(parent *StateImpl, stateHash []byte) *syncProcess {

	sp := &syncProcess{
		StateImpl:       parent,
		targetStateHash: stateHash,
	}

	calcSyncLevels(sp)
	syncLevel := sp.syncLevels[sp.curLevelIndex]
	sp.current = &protos.BucketTreeOffset{
		Level:     uint64(syncLevel),
		BucketNum: 1,
		Delta:     sp.currentConfig.getDelta(syncLevel),
	}

	logger.Infof("newSyncProcess: curLevelIndex[%d], syncLevels[%+v]", sp.curLevelIndex, sp.syncLevels)
	return sp
}

// compute the levels by which metadata will be sent for sanity check
func calcSyncLevels(proc *syncProcess) {

	proc.syncLevels = make([]int, 0)
	lowestLevel := proc.StateImpl.currentConfig.lowestLevel

	proc.syncLevels = append(proc.syncLevels, proc.StateImpl.currentConfig.lowestLevel)

	enableMetadataVerify := true
	//enableMetadataVerify = false

	if enableMetadataVerify {
		diff := 2

		sqrtFunc := func() {
			res := int(sqrt(float64(lowestLevel + 1)))
			for res >= diff {

				proc.syncLevels = append(proc.syncLevels, res)
				res = int(sqrt(float64(res)))
			}
		}

		decreamentFunc := func() {
			res := lowestLevel - diff
			for res >= diff {
				proc.syncLevels = append(proc.syncLevels, res)
				res -= diff
			}
		}

		_ = sqrtFunc
		_ = decreamentFunc

		decreamentFunc()
		//sqrtFunc()
	}
	proc.curLevelIndex = len(proc.syncLevels) - 1
}

//implement for syncinprogress interface
func (proc *syncProcess) IsSyncInProgress() {}
func (proc *syncProcess) Error() string     { return "buckettree: state syncing is in progress" }

func (proc *syncProcess) PersistProgress(writeBatch *db.DBWriteBatch) error {

	key := append([]byte{partialStatusKeyPrefixByte}, proc.targetStateHash...)
	if value, err := proc.current.Byte(); err == nil {
		logger.Infof("Persisting current sync process = %+v", proc.current)
		writeBatch.PutCF(writeBatch.GetDBHandle().StateCF, key, value)
	} else {
		return err
	}

	return nil
}

func (proc *syncProcess) RequiredParts() ([]*protos.SyncOffset, error) {

	if proc.current == nil {
		return nil, nil
	}

	if data, err := proc.current.Byte(); err != nil {
		return nil, err
	} else {
		return []*protos.SyncOffset{&protos.SyncOffset{Data: data}}, err
	}

}

func (proc *syncProcess) CompletePart(part *protos.BucketTreeOffset) error {

	if proc.current == nil {
		return fmt.Errorf("No task left")
	} else if proc.current.Level != part.GetLevel() || proc.current.BucketNum != part.GetBucketNum() {
		return fmt.Errorf("Not current task (expect <%v> but has <%v>", proc.current, part)
	}

	conf := proc.currentConfig

	err := proc.verifyMetadata()
	if err != nil {
		return err
	}

	maxNum := uint64(conf.getNumBuckets(int(proc.current.Level)))
	nextNum := proc.current.BucketNum + proc.current.Delta

	if maxNum <= nextNum-1 {

		if proc.curLevelIndex > 0 {
			lastLevel := proc.current.Level

			proc.curLevelIndex--
			syncLevel := proc.syncLevels[proc.curLevelIndex]

			currentDelta := int(proc.currentConfig.syncDelta) *
				pow(proc.currentConfig.getMaxGroupingAtEachLevel(),
					syncLevel - int(lastLevel))

			logger.Infof("Compute Next level[%d], delta<%d>", syncLevel, currentDelta)

			proc.current = &protos.BucketTreeOffset{
				Level:     uint64(syncLevel),
				BucketNum: 1,
				Delta:     min(uint64(currentDelta),
					uint64(proc.currentConfig.getNumBuckets(syncLevel))),
			}

			maxNum = uint64(conf.getNumBuckets(int(proc.current.Level)))
			nextNum = proc.current.BucketNum + proc.current.Delta

			logger.Infof("Go to Next level. state offset <%+v>", proc.current)

		} else {
			logger.Infof("Finally hit maxBucketNum<%d>, target Level<%d> BucketNum<%d>",
				maxNum, proc.current.Level,	nextNum)
			proc.current = nil
		}
		return nil
	}
	delta := min(uint64(proc.current.Delta), maxNum-nextNum+1)

	proc.current.BucketNum = nextNum
	proc.current.Delta = delta

	logger.Infof("Next state offset <%+v>", proc.current)
	return nil
}

func (underSync *syncProcess) verifyMetadata() error {

	curLevelIndex := underSync.curLevelIndex
	tempTreeDelta := underSync.StateImpl.bucketTreeDelta
	if underSync.metadataTreeDelta != nil {
		tempTreeDelta = underSync.metadataTreeDelta
	}

	var err error
	if curLevelIndex < len(underSync.syncLevels) - 1 {

		lastSyncLevel := underSync.syncLevels[curLevelIndex + 1]
		bucketNodes := tempTreeDelta.getBucketNodesAt(lastSyncLevel)

		var localBucketNode *bucketNode
		for _, bkNode := range bucketNodes {

			localBucketNode, err = fetchBucketNodeFromDB(underSync.StateImpl.OpenchainDB,
				bkNode.bucketKey.getBucketKey(underSync.currentConfig))

			if err == nil {
				if bytes.Equal(localBucketNode.computeCryptoHash(), bkNode.computeCryptoHash()) {
					logger.Infof("Pass: verify metadata: bucketKey[%+v] cryptoHash[%x]",
						bkNode.bucketKey,
						bkNode.computeCryptoHash())
				} else {
					err = fmt.Errorf("Failed to verify metadata: error: mismatch, " +
						"bucketKey[%+v] cryptoHash[%x] localCryptoHash[%x]",
						bkNode.bucketKey,
						bkNode.computeCryptoHash(),
						localBucketNode.computeCryptoHash())
					break
				}
			} else {
				err = fmt.Errorf("Failed to verify metadata: error: %s, bucketKey[%+v] cryptoHash[%x]",
					err,
					bkNode.bucketKey,
					bkNode.computeCryptoHash())
				break
			}

		}
	}

	underSync.metadataTreeDelta = nil
	return err
}

func sqrt(x float64) float64 {
	z := 1.0

	if x < 0 {
		return 0
	} else if x == 0 {
		return 0
	} else {

		getabs := func(x float64) float64 {
			if x < 0 {
				return -x
			}
			if x == 0 {
				return 0
			}
			return x
		}

		for getabs(z*z-x) > 1e-6 {
			z = (z + x/z) / 2
		}
		return z
	}
}
