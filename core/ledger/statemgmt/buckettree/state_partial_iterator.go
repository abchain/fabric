package buckettree

import (
	"fmt"
	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/proto"
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

	if partialItr.curLevel != 0 {
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

func (partialItr *PartialSnapshotIterator) NextBucketNode() bool {
	partialItr.keyCache = nil
	partialItr.valueCache = nil

	if !partialItr.StateSnapshotIterator.Next() {
		return false
	}

	keyBytes := statemgmt.Copy(partialItr.dbItr.Key().Data())
	valueBytes := statemgmt.Copy(partialItr.dbItr.Value().Data())

	bucketKey := decodeBucketKey(partialItr.config, keyBytes)

	if bucketKey.bucketNumber > partialItr.lastBucketNum {
		return false
	}
	if bucketKey.level > partialItr.curLevel {
		return false
	}

	bucketNode := unmarshalBucketNode(bucketKey, valueBytes)

	partialItr.keyCache = bucketKey.getEncodedBytes()
	partialItr.valueCache = bucketNode.marshal()

	logger.Debugf("Produce metadata: bucketNode[%+v], computeCryptoHash[%x]",
		bucketNode.bucketKey,
		bucketNode.computeCryptoHash())

	return true
}

func (partialItr *PartialSnapshotIterator) innerSeek() error {

	if partialItr.curLevel == 0 {
		partialItr.dbItr.Seek(minimumPossibleDataKeyBytesFor(newBucketKeyAtLowestLevel(partialItr.config, partialItr.currentBucketNum)))
	} else {
		partialItr.dbItr.Seek(newBucketKey(partialItr.config, partialItr.curLevel, partialItr.currentBucketNum).getEncodedBytes())
	}
	partialItr.dbItr.Prev()
	return nil
}

func (partialItr *PartialSnapshotIterator) Seek(offset *protos.SyncOffset) error {

	bucketTreeOffset, err := offset.Unmarshal2BucketTree()
	if err != nil {
		return err
	}

	level := int(bucketTreeOffset.Level)
	if level > partialItr.getLowestLevel() {
		return fmt.Errorf("level %d outbound: [%d]", level, partialItr.getLowestLevel())
	}
	partialItr.curLevel = level
	if level == 0 { //it was datanode, so set level to lowest
		level = partialItr.getLowestLevel()
	}

	startNum := int(bucketTreeOffset.BucketNum)
	if startNum > partialItr.getNumBuckets(level) {
		return fmt.Errorf("Start numbucket %d outbound: [%d]", startNum, partialItr.getNumBuckets(level))
	}

	endNum := int(bucketTreeOffset.Delta + bucketTreeOffset.BucketNum - 1)
	if endNum > partialItr.getNumBuckets(level) {
		endNum = partialItr.getNumBuckets(level)
	}

	logger.Infof("Required bucketTreeOffset: [%+v] start-end [%d-%d]",
		bucketTreeOffset, startNum, endNum)

	partialItr.currentBucketNum = startNum
	partialItr.lastBucketNum = endNum

	return partialItr.innerSeek()

}

func (partialItr *PartialSnapshotIterator) GetMetaData() []byte {

	if partialItr.curLevel > partialItr.getLowestLevel() || partialItr.curLevel == 0 {
		return nil
	}

	md := &protos.SyncMetadata{}
	seeked := true

	//bucketkey is saved in protobuf's variat number format and notice:
	//1. number in any expected range is ordered (i.e. larger number is always laid after smaller)
	//2. number is not always ordered in an iterator (i.e. a possible sequence may be :256, 512, 257 ...)

	for {
		for partialItr.currentBucketNum <= partialItr.lastBucketNum {
			if !partialItr.StateSnapshotIterator.Next() {
				//we can stop iterating, according to rule 1 before
				break
			}
			bucketKey := decodeBucketKey(partialItr.config, partialItr.dbItr.Key().Data())

			if bucketKey.level > partialItr.curLevel {
				//can also stop iterating, (still rule 1)
				return false
			} else if bucketKey.bucketNumber != partialItr.partialItr.currentBucketNum {
				//try another seek (according to rule 2)
				if err := partialItr.innerSeek() err != nil{
					logger.Errorf("seek fail on [%v]", partialItr)
					break
				}
				continue
			}

			partialItr.currentBucketNum++

		}
		//try by another seek, or end
		if !seeked && partialItr.innerSeek() == nil {
			seeked = true
		} else {
			break
		}

	}

	for partialItr.StateSnapshotIterator.Next() {

	}

	for partialItr.NextBucketNode() {
		_, v := partialItr.GetRawKeyValue()
		md.BucketNodeHashList = append(md.BucketNodeHashList, v)
	}
	metadata, err := proto.Marshal(md)
	if err != nil {
		return nil
	}
	return metadata
}

func (partialItr *PartialSnapshotIterator) GetRawKeyValue() ([]byte, []byte) {
	//sanity check
	if partialItr.keyCache == nil {
		panic("Called after Next return false")
	}
	return partialItr.keyCache, partialItr.valueCache
}
