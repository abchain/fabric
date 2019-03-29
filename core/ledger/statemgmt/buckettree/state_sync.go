package buckettree

import (
	"fmt"
	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/protos"
)

type syncProcess struct {
	*StateImpl
	targetStateHash []byte
	current         *protos.BucketTreeOffset
	metaDelta       int
	syncLevels      []int
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

			sp := &syncProcess{
				StateImpl:       parent,
				targetStateHash: targetStateHash,
			}
			sp.calcSyncLevels(parent.currentConfig)

			if int(offset.GetLevel()) == parent.currentConfig.getLowestLevel() {
				sp.syncLevels = []int{}
				sp.current = offset
			} else {
				//we check current offset against the calculated sync plan ...
				for i, lvl := range sp.syncLevels {
					if lvl <= int(offset.Level) {
						sp.syncLevels = sp.syncLevels[:i+1]

						if lvl == int(offset.Level) {
							sp.current = offset
						} else if lvl < int(offset.Level) {
							logger.Infof("We restored different level (%d, nearest is %d)", offset.Level, lvl)
							sp.resetCurrentOffset()
						}
						break
					}
				}
			}

			logger.Info("Restore sync task to target [%x] at offset %v", targetStateHash, sp.current)
			return sp
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

	sp.calcSyncLevels(parent.currentConfig)
	sp.resetCurrentOffset()

	logger.Infof("newSyncProcess: sync start with offset %v", sp.current)
	return sp
}

func (proc *syncProcess) resetCurrentOffset() {
	cfg := proc.StateImpl.currentConfig
	if l := len(proc.syncLevels); l == 0 {
		proc.current = &protos.BucketTreeOffset{
			Level:     uint64(cfg.getLowestLevel() + 1),
			BucketNum: 1,
			Delta:     uint64(cfg.syncDelta),
		}

		if proc.current.BucketNum+proc.current.Delta > uint64(cfg.getNumBucketsAtLowestLevel()) {
			proc.current.Delta = uint64(cfg.getNumBucketsAtLowestLevel()) - proc.current.BucketNum
		}

	} else {
		proc.current = &protos.BucketTreeOffset{
			Level:     uint64(proc.syncLevels[l-1]),
			BucketNum: 1,
			Delta:     uint64(proc.metaDelta),
		}

		maxBuckets := uint64(cfg.getNumBuckets(int(proc.current.Level)))
		if proc.current.BucketNum+proc.current.Delta > maxBuckets {
			proc.current.Delta = maxBuckets - proc.current.BucketNum
		}
	}

}

// compute the levels by which metadata will be sent for sanity check
func (proc *syncProcess) calcSyncLevels(conf *config) {

	syncLevels := []int{}
	//estimate a suitable delta for metadata: one bucketnode is 32-bytes hash
	//and we expect it was 1/3 size of the datanode (no providence yet)
	//so we decide it as 3*syncdelta in config and then align it to
	//an exponent of maxgroup
	metaDelta := conf.getMaxGroupingAtEachLevel()
	metaReferenceDelta := conf.syncDelta * 3
	lvldistance := 1
	for ; metaDelta < metaReferenceDelta; metaDelta = metaDelta * conf.getMaxGroupingAtEachLevel() {
		lvldistance++
	}
	//let's check which is larger, to avoid a unusual maxgrouping leads to too great metaDelta ...
	if lvldistance > 1 && metaReferenceDelta-(lvldistance-1)*conf.getMaxGroupingAtEachLevel() < metaDelta-metaReferenceDelta {
		//in this case, we select a less distance ...
		lvldistance--
	}

	//test syncdelta to see which bucket level we can stopped at: the level
	//must aligned on maxgroup level
	testSyncDelta := conf.syncDelta
	curlvl := conf.getLowestLevel()
	grpNum := conf.getMaxGroupingAtEachLevel()
	for ; curlvl >= 0; curlvl-- {
		if testSyncDelta%grpNum == 0 && testSyncDelta >= grpNum {
			testSyncDelta = testSyncDelta / grpNum
		} else if testSyncDelta > conf.getNumBuckets(curlvl) {
			//a rare case, but we still consider
			testSyncDelta = testSyncDelta / grpNum
		} else {
			break
		}
	}

	//sanity check, because we have always adjust the syncdelta
	if curlvl == conf.getLowestLevel() {
		//end at lowest level means we have to transfer too many hashes
		logger.Warningf("We have metadata sync level end at lowest")
	}

	for ; curlvl > 0; curlvl = curlvl - lvldistance {
		//twe shrink the lvls by metaDelta
		syncLevels = append(syncLevels, curlvl)
	}

	proc.metaDelta = metaDelta
	proc.syncLevels = syncLevels
	logger.Infof("Calculate sync plan as: %v", syncLevels)
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

	// err := proc.verifyMetadata()
	// if err != nil {
	// 	return err
	// }

	maxNum := uint64(conf.getNumBuckets(int(proc.current.Level)))
	nextNum := proc.current.BucketNum + proc.current.Delta

	if maxNum <= nextNum-1 {
		//current level is done
		if l := len(proc.syncLevels); l > 0 {
			proc.syncLevels = proc.syncLevels[:l-1]
		} else {
			logger.Infof("Finally hit maxBucketNum<%d> @ target Level<%d>",
				maxNum, proc.current.Level)
			proc.current = nil
			return nil
		}

		proc.resetCurrentOffset()
		logger.Infof("Compute Next level[%d], delta<%d>", proc.current.Level, proc.current.Delta)
	} else {
		delta := min(uint64(proc.current.Delta), maxNum-nextNum+1)

		proc.current.BucketNum = nextNum
		proc.current.Delta = delta
		logger.Infof("Next state offset <%+v>", proc.current)
	}

	return nil
}
