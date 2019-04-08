package buckettree

import (
	"fmt"
	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/protos"
)

type PartialSnapshotIterator struct {
	StateSnapshotIterator
	*config
	keyCache, valueCache []byte
	lastBucketNum        int
	currentBucketNum     int
	//we use 0 to indicate we should interate on datanode instead of bucketnode
	//(we should never iterate on level 0 for bucketnode)
	curLevel int
	//for respecting the StateSnapshotIterator, Next is called first in iteration
	//and StateSnapshotIterator use an extra Prev() after seek (that is, SeekForPrev)
	//but we notice that Prev + Next is not identify to no-op (because iterator may become invalid)
	//so we use additional flag
	justSeeked bool
}

func newPartialSnapshotIterator(snapshot *db.DBSnapshot, cfg *config) (*PartialSnapshotIterator, error) {
	iit, err := newStateSnapshotIterator(snapshot)
	if err != nil {
		return nil, err
	}

	return &PartialSnapshotIterator{
		StateSnapshotIterator: *iit,
		config:                cfg,
		lastBucketNum:         1,
	}, nil
}

func (partialItr *PartialSnapshotIterator) innerNext() bool {
	if partialItr.justSeeked {
		partialItr.justSeeked = false
		return true
	} else {
		return partialItr.StateSnapshotIterator.Next()
	}
}

//overwrite the original GetRawKeyValue and Next
func (partialItr *PartialSnapshotIterator) Next() bool {

	if partialItr.curLevel != partialItr.getLowestLevel() {
		return false
	}

	if !partialItr.innerNext() {
		return false
	}

	partialItr.keyCache = nil
	partialItr.valueCache = nil

	keyBytes := statemgmt.Copy(partialItr.dbItr.Key().Data())
	valueBytes := statemgmt.Copy(partialItr.dbItr.Value().Data())
	dataNode := unmarshalDataNodeFromBytes(keyBytes, valueBytes)

	//logger.Debugf("seeking data at bucket %d, get %v [key %X]", partialItr.currentBucketNum, dataNode, keyBytes)

	partialItr.currentBucketNum = dataNode.dataKey.bucketNumber
	if dataNode.dataKey.bucketNumber > partialItr.lastBucketNum {
		return false
	}

	partialItr.keyCache = dataNode.getCompositeKey()
	partialItr.valueCache = dataNode.getValue()

	return true
}

func (partialItr *PartialSnapshotIterator) innerSeek() error {

	var seekK []byte
	if partialItr.curLevel == partialItr.getLowestLevel() {
		seekK = minimumPossibleDataKeyBytesFor(newBucketKeyAtLowestLevel(partialItr.config, partialItr.currentBucketNum))
	} else {
		seekK = newBucketKey(partialItr.config, partialItr.curLevel, partialItr.currentBucketNum).getEncodedBytes()
	}
	partialItr.dbItr.Seek(seekK)
	//we may hit the end and iterator become invalid ...
	if partialItr.dbItr.Valid() {
		partialItr.justSeeked = true
	} else if err := partialItr.dbItr.Err(); err != nil {
		return fmt.Errorf("partial iterator error: %s", err)
	} else {
		//hit the end, so we set iterator on last and not set justseeked
		partialItr.dbItr.SeekToLast()
	}
	return nil
}

func (partialItr *PartialSnapshotIterator) Seek(offset *protos.SyncOffset) error {

	bucketTreeOffset := offset.GetBuckettree()
	if bucketTreeOffset == nil {
		return fmt.Errorf("offset [%v] has no content for buckettree", offset)
	}

	level := int(bucketTreeOffset.Level)
	if level > partialItr.getLowestLevel() {
		return fmt.Errorf("level %d outbound: [%d]", level, partialItr.getLowestLevel())
	}
	partialItr.curLevel = level

	startNum := int(bucketTreeOffset.BucketNum)
	if startNum > partialItr.getNumBuckets(level) {
		return fmt.Errorf("Start numbucket %d outbound: [%d]", startNum, partialItr.getNumBuckets(level))
	}

	endNum := int(bucketTreeOffset.Delta+bucketTreeOffset.BucketNum) - 1
	if endNum > partialItr.getNumBuckets(level) {
		endNum = partialItr.getNumBuckets(level)
	}

	logger.Infof("Required bucketTreeOffset: [%+v] start-end [%d-%d]",
		bucketTreeOffset, startNum, endNum)

	partialItr.currentBucketNum = startNum
	partialItr.lastBucketNum = endNum

	return partialItr.innerSeek()

}

func (partialItr *PartialSnapshotIterator) GetMetaData() *protos.SyncMetadata {

	if partialItr.curLevel >= partialItr.getLowestLevel() {
		return nil
	}

	md := &protos.BucketNodes{}

	//bucketkey is saved in protobuf's variat number format and notice:
	// numbers are not always ordered when picked from iterator (i.e. a possible sequence may be :256, 512, 257 ...)

	for justSeeked := partialItr.justSeeked; partialItr.currentBucketNum <= partialItr.lastBucketNum && partialItr.innerNext(); justSeeked = partialItr.justSeeked {

		kBytes := partialItr.dbItr.Key().Data()
		if len(kBytes) != 0 && kBytes[0] == 0 /*bucket key prefix*/ {
			bucketKey := decodeBucketKey(partialItr.config, kBytes)
			if bucketKey.level == partialItr.curLevel {
				//here is a trick for the variat number in protobuf: the out-of-order
				//sequence is always occur with numbers with a least interval in 128
				//and a number A larger than expected by no more than 128 will be the
				//"true" successor of the expected one (that is, there will not be any
				//number less than A when iteration continues)
				if bucketKey.bucketNumber >= partialItr.currentBucketNum && bucketKey.bucketNumber < partialItr.currentBucketNum+127 {
					if partialItr.currentBucketNum != bucketKey.bucketNumber {
						logger.Infof("reset bucketnum from %d to %d ", partialItr.currentBucketNum, bucketKey.bucketNumber)
						partialItr.currentBucketNum = bucketKey.bucketNumber

						//notice out-of-bound may occur for we have restted current number
						if partialItr.currentBucketNum > partialItr.lastBucketNum {
							break
						}

					} else {
						partialItr.currentBucketNum++
					}

					//good, we have a new bucketnode and can iterate to next
					node := &protos.BucketNode{uint64(bucketKey.level),
						uint64(bucketKey.bucketNumber),
						statemgmt.Copy(partialItr.dbItr.Value().Data())}
					md.Nodes = append(md.Nodes, node)

					continue
				}
			}
		}

		if justSeeked {
			partialItr.currentBucketNum++
			//notice out-of-bound
			if partialItr.currentBucketNum > partialItr.lastBucketNum {
				break
			}
		}

		logger.Infof("seek for %d (get %X) ", partialItr.currentBucketNum, kBytes)
		if err := partialItr.innerSeek(); err != nil {
			logger.Errorf("seek fail on [%v]: %s", partialItr, err)
			break
		}
	}

	return &protos.SyncMetadata{Data: &protos.SyncMetadata_Buckettree{Buckettree: md}}
}

func (partialItr *PartialSnapshotIterator) GetRawKeyValue() ([]byte, []byte) {
	//sanity check
	if partialItr.keyCache == nil {
		panic("Called after Next return false")
	}
	return partialItr.keyCache, partialItr.valueCache
}
