package buckettree

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"github.com/abchain/fabric/protos"
)

type syncProcess struct {
	*config
	targetStateHash []byte
	current         *protos.BucketTreeOffset
	syncDelta       int //the basic syncDelta (overide the member in config)
	metaDelta       int
	verifyLevel     int
	syncLevels      []int
}

var partialStatusKey = []byte{17, 0}

func newSyncProcess(parent *StateImpl, stateHash []byte) *syncProcess {

	sp := &syncProcess{
		config:          parent.currentConfig,
		targetStateHash: stateHash,
		syncDelta:       parent.currentConfig.syncDelta,
		verifyLevel:     -1,
	}

	sp.calcSyncLevels()
	sp.resetCurrentOffset()

	logger.Infof("newSyncProcess: sync start with offset %v", sp.current)
	return sp
}

type procPersisting struct {
	Target        []byte
	BaseDelta     int
	CurrentOffset []byte
}

func loadSyncProcess(parent *StateImpl, bts []byte) (*syncProcess, error) {
	dec := gob.NewDecoder(bytes.NewReader(bts))
	load := new(procPersisting)

	if err := dec.Decode(load); err != nil {
		return nil, err
	}

	sp := &syncProcess{
		config:          parent.currentConfig,
		targetStateHash: load.Target,
		syncDelta:       load.BaseDelta,
		verifyLevel:     -1,
	}
	sp.calcSyncLevels()

	if offset, err := protos.UnmarshalBucketTree(load.CurrentOffset); err != nil {
		return nil, err
	} else {
		//restore current offset and set syncLevels
		if int(offset.GetLevel()) == parent.currentConfig.getLowestLevel() {
			sp.syncLevels = []int{}
			sp.current = offset
		} else {
			//we check current offset against the calculated sync plan ...
			for i, lvl := range sp.syncLevels {
				if lvl <= int(offset.Level) {
					if i+1 < len(sp.syncLevels) {
						sp.verifyLevel = sp.syncLevels[i+1]
					}
					sp.syncLevels = sp.syncLevels[:i+1]
					if lvl == int(offset.Level) {
						sp.current = offset
					} else if lvl < int(offset.Level) {
						//this should not happen, but we still try recover from it
						logger.Warningf("We restored different level (%d, nearest is %d)", offset.Level, lvl)
						sp.resetCurrentOffset()
					}

					break
				}
			}
		}

	}

	logger.Infof("loadSyncProcess: sync start with offset %v", sp.current)
	return sp, nil

}

func (proc *syncProcess) PersistProgress() ([]byte, error) {

	logger.Infof("Persisting current sync process = %+v", proc.current)

	save := &procPersisting{
		Target:    proc.targetStateHash,
		BaseDelta: proc.syncDelta,
	}

	if value, err := proc.current.Byte(); err == nil {
		save.CurrentOffset = value
	} else {
		return nil, err
	}

	buf := new(bytes.Buffer)
	if err := gob.NewEncoder(buf).Encode(save); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (proc *syncProcess) resetCurrentOffset() {

	if l := len(proc.syncLevels); l == 0 {
		proc.current = &protos.BucketTreeOffset{
			Level:     uint64(proc.getLowestLevel()),
			BucketNum: 1,
			Delta:     uint64(proc.syncDelta),
		}

		if proc.current.BucketNum+proc.current.Delta > uint64(proc.getNumBucketsAtLowestLevel())+1 {
			proc.current.Delta = uint64(proc.getNumBucketsAtLowestLevel()) - proc.current.BucketNum + 1
		}

	} else {
		proc.current = &protos.BucketTreeOffset{
			Level:     uint64(proc.syncLevels[l-1]),
			BucketNum: 1,
			Delta:     uint64(proc.metaDelta),
		}

		maxBuckets := uint64(proc.getNumBuckets(int(proc.current.Level)))
		if proc.current.BucketNum+proc.current.Delta > maxBuckets+1 {
			proc.current.Delta = maxBuckets - proc.current.BucketNum + 1
		}
	}

}

var bucketDataRatio = 5

// compute the levels by which metadata will be sent for sanity check
func (proc *syncProcess) calcSyncLevels() {

	syncLevels := []int{}
	//estimate a suitable delta for metadata: one bucketnode is 32-bytes hash
	//and we expect it was 1/5 size of the datanode (set by bucketDataRatio, no providence yet)
	//so we decide it as 5*syncdelta in config and then align it to
	//an exponent of maxgroup
	metaDelta := proc.getMaxGroupingAtEachLevel()
	metaReferenceDelta := proc.syncDelta * bucketDataRatio
	lvldistance := 0
	//too small, assign it as large as maxgrouping
	if metaDelta > metaReferenceDelta {
		metaReferenceDelta = metaDelta
	}

	for ; metaDelta <= metaReferenceDelta; metaDelta = metaDelta * proc.getMaxGroupingAtEachLevel() {

		//for non-align case, we allow an adjusting within 10% range, but for the first level we
		//always do adjust
		r := metaReferenceDelta % metaDelta
		if lvldistance != 0 && r != 0 && metaReferenceDelta < metaDelta*10 {
			break
		} else if r != 0 {
			metaReferenceDelta = metaReferenceDelta + metaDelta - r
		}
		lvldistance++
	}

	logger.Debugf("Calculate sync on metadata can skip by %d levels", lvldistance)

	//test syncdelta to see which bucket level we can stopped at: the level
	//must aligned on maxgroup level
	testSyncDelta := proc.syncDelta
	curlvl := proc.getLowestLevel() - 1
	grpNum := proc.getMaxGroupingAtEachLevel()
	for ; curlvl >= 0; curlvl-- {
		if testSyncDelta%grpNum == 0 && testSyncDelta >= grpNum {
			testSyncDelta = testSyncDelta / grpNum
		} else if testSyncDelta >= proc.getNumBuckets(curlvl) {
			//a rare case, but we still consider
			testSyncDelta = testSyncDelta / grpNum
		} else {
			break
		}
	}

	logger.Debugf("Calculate starting sync on metadata should be level %d", curlvl)

	//notice: delta indicate how many bucketnodes we should pass and one bucketnode has <maxgrouping> hashes
	proc.metaDelta = metaReferenceDelta / proc.getMaxGroupingAtEachLevel()
	for ; curlvl >= 0; curlvl = curlvl - lvldistance {
		//we shrink the lvls by metaDelta
		syncLevels = append(syncLevels, curlvl)
		//notice lvldistance do not indicate the size but the "ability" for metasync can "jump"
		//among layers (only if the delta is aligned to group number), it was possible in one
		//level we can transfer it once, so we do not need to transfer more layers
		if proc.getNumBuckets(curlvl) <= proc.metaDelta {
			break
		}
	}

	proc.syncLevels = syncLevels
	logger.Infof("Calculate sync plan as: %v (delta %d)", syncLevels, proc.metaDelta)
}

//implement for syncinprogress interface
func (proc *syncProcess) IsSyncInProgress()  {}
func (proc *syncProcess) SyncTarget() []byte { return proc.targetStateHash }
func (proc *syncProcess) Error() string      { return "buckettree: state syncing is in progress" }

//return a range, which is in the verifylevel and the minimun cover of current sync range
//and a "remainder" index for the last index in last node we must check
func (proc *syncProcess) verifiedRange() [2]int {

	if proc.current == nil {
		return [2]int{0, 0}
	}

	grpnum := proc.getMaxGroupingAtEachLevel()
	//use the 0-start indexed, closed interval
	ret := [2]int{int(proc.current.GetBucketNum()) - 1,
		int(proc.current.GetBucketNum()+proc.current.GetDelta()) - 2}

	for lvl := int(proc.current.GetLevel()); lvl > proc.verifyLevel+1; lvl-- {
		ret[0] = ret[0] / grpnum
		ret[1] = ret[1] / grpnum
	}

	ret = [2]int{ret[0] + 1, ret[1] + 1}
	logger.Debugf("Calc verify range for current proc [%v]: %v", proc.current, ret)

	return ret
}

func (proc *syncProcess) RequiredParts() ([]*protos.SyncOffset, error) {

	if proc.current == nil {
		return nil, nil
	}

	theOffset := &protos.SyncOffset{Data: &protos.SyncOffset_Buckettree{Buckettree: proc.current}}
	return []*protos.SyncOffset{theOffset}, nil

}

func (proc *syncProcess) CompletePart(part *protos.BucketTreeOffset) error {

	if proc.current == nil {
		return fmt.Errorf("No task left")
	} else if proc.current.Level != part.GetLevel() || proc.current.BucketNum != part.GetBucketNum() {
		return fmt.Errorf("Not current task (expect <%v> but has <%v>", proc.current, part)
	}

	maxNum := uint64(proc.getNumBuckets(int(proc.current.Level)))
	nextNum := proc.current.BucketNum + proc.current.Delta

	if maxNum < nextNum {
		//current level is done
		if l := len(proc.syncLevels); l > 0 {
			proc.verifyLevel = proc.syncLevels[l-1]
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
