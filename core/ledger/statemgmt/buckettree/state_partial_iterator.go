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

//overwrite the original GetRawKeyValue and Next
func (partialItr *PartialSnapshotIterator) Next() bool {

	if partialItr.curLevel != partialItr.getLowestLevel() {
		return false
	}

	partialItr.keyCache = nil
	partialItr.valueCache = nil

	//notice the order of datakey is preserved, so we just need one seeking
	if !partialItr.StateSnapshotIterator.Next() {
		return false
	}

	keyBytes := statemgmt.Copy(partialItr.dbItr.Key().Data())
	valueBytes := statemgmt.Copy(partialItr.dbItr.Value().Data())
	dataNode := unmarshalDataNodeFromBytes(keyBytes, valueBytes)

	partialItr.currentBucketNum = dataNode.dataKey.bucketNumber
	if dataNode.dataKey.bucketNumber > partialItr.lastBucketNum {
		return false
	}

	partialItr.keyCache = dataNode.getCompositeKey()
	partialItr.valueCache = dataNode.getValue()

	return true
}

func (partialItr *PartialSnapshotIterator) innerSeek() error {

	if partialItr.curLevel == partialItr.getLowestLevel() {
		partialItr.dbItr.Seek(minimumPossibleDataKeyBytesFor(newBucketKeyAtLowestLevel(partialItr.config, partialItr.currentBucketNum)))
	} else {
		partialItr.dbItr.Seek(newBucketKey(partialItr.config, partialItr.curLevel, partialItr.currentBucketNum).getEncodedBytes())
	}
	partialItr.dbItr.Prev()
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

	endNum := int(bucketTreeOffset.Delta + bucketTreeOffset.BucketNum)
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
	//1. numbers in any expected range are ordered (i.e. larger number is always laid after smaller)
	//2. numbers are not always ordered when picked from iterator (i.e. a possible sequence may be :256, 512, 257 ...)

	for partialItr.currentBucketNum <= partialItr.lastBucketNum && partialItr.StateSnapshotIterator.Next() {

		kBytes := partialItr.dbItr.Key().Data()
		if len(kBytes) == 0 || kBytes[0] != 0 /*bucket key prefix*/ {
			//we can stop iterating, according to rule 1 before
			break
		}

		bucketKey := decodeBucketKey(partialItr.config, kBytes)
		if bucketKey.level > partialItr.curLevel {
			//still rule 1 exit (level is come before bucketnumber)
			break
		}

		if bucketKey.bucketNumber > partialItr.currentBucketNum {
			//here is a trick for the variat number in protobuf: the out-of-order
			//sequence is always occur with numbers with a least interval in 128
			//and a number A larger than expected by no more than 128 will be the
			//"true" successor of the expected one (that is, there will not be any
			//number less than A when iteration continues)
			if bucketKey.bucketNumber >= partialItr.currentBucketNum+128 {
				//try another seek (according to rule 2)
				partialItr.currentBucketNum++
				if err := partialItr.innerSeek(); err != nil {
					logger.Errorf("seek fail on [%v]: %s", partialItr, err)
					break
				}
				continue
			} else {
				partialItr.currentBucketNum = bucketKey.bucketNumber
				//notice out-of-bound may occur
				if partialItr.currentBucketNum > partialItr.lastBucketNum {
					break
				}
			}
		}

		//good, we have a new bucketnode
		node := &protos.BucketNode{uint64(bucketKey.level),
			uint64(bucketKey.bucketNumber),
			statemgmt.Copy(partialItr.dbItr.Value().Data())}
		md.Nodes = append(md.Nodes, node)
		partialItr.currentBucketNum++
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
