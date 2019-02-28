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
	dataNode  *dataNode
	lastBucketNum        int
}

func newPartialSnapshotIterator(snapshot *db.DBSnapshot, cfg *config) (*PartialSnapshotIterator, error) {
	iit, err := newStateSnapshotIterator(snapshot)
	if err != nil {
		return nil, err
	}

	return &PartialSnapshotIterator{
		StateSnapshotIterator: *iit,
		config:                cfg,
		lastBucketNum:         cfg.getNumBucketsAtLowestLevel(),
	}, nil
}

//overwrite the original GetRawKeyValue and Next
func (partialItr *PartialSnapshotIterator) Next() bool {

	partialItr.keyCache = nil
	partialItr.valueCache = nil

	if !partialItr.StateSnapshotIterator.Next() {
		return false
	}

	keyBytes := statemgmt.Copy(partialItr.dbItr.Key().Data())
	valueBytes := statemgmt.Copy(partialItr.dbItr.Value().Data())
	dataNode := unmarshalDataNodeFromBytes(keyBytes, valueBytes)

	if dataNode.dataKey.bucketNumber > partialItr.lastBucketNum {
		return false
	}

	partialItr.keyCache = dataNode.getCompositeKey()
	partialItr.valueCache = dataNode.getValue()
	partialItr.dataNode = dataNode

	return true
}

func (partialItr *PartialSnapshotIterator) GetRawKeyValue() ([]byte, []byte) {

	//sanity check
	panic(partialItr.keyCache == nil)
	return partialItr.keyCache, partialItr.valueCache
}

func (partialItr *PartialSnapshotIterator) Seek(offset *protos.SyncOffset) error {

	bucketTreeOffset, err := offset.Unmarshal2BucketTree()
	if err != nil {
		return err
	}

	logger.Infof("Required bucketTreeOffset: [%+v]", bucketTreeOffset)

	level := int(bucketTreeOffset.Level)
	if level > partialItr.getLowestLevel() {
		return fmt.Errorf("level %d outbound: [%d]", level, partialItr.getLowestLevel())
	}

	startNum := int(bucketTreeOffset.BucketNum)
	if startNum >= partialItr.getNumBuckets(level) {
		return fmt.Errorf("Start numbucket %d outbound: [%d]", startNum, partialItr.getNumBuckets(level))
	}

	endNum := int(bucketTreeOffset.Delta + bucketTreeOffset.BucketNum - 1)

	if level == partialItr.getLowestLevel() {
		//transfer datanode
		//seek to target datanode
		partialItr.dbItr.Seek(minimumPossibleDataKeyBytesFor(newBucketKey(partialItr.config, level, startNum)))
		partialItr.dbItr.Prev()
		partialItr.lastBucketNum = endNum
	} else {
		//TODO: transfer bucketnode in metadata
		return fmt.Errorf("No implement")
	}

	return nil
}

func (partialItr *PartialSnapshotIterator) GetRequiredParts(offset *protos.SyncOffset) (*protos.SyncStateChunk, error) {
	err := partialItr.Seek(offset)
	if err != nil {
		return nil, err
	}

	i := 0
	stateDelta := statemgmt.NewStateDelta()
	for partialItr.Next() {
		i++
		dataNode := partialItr.dataNode

		ccdId, key := dataNode.getKeyElements()

		logger.Infof("Index[%d] bucketNumber[%d] compositeKey<%s> ccdId[%s] key[%s] value[%s]",
			i,
			dataNode.dataKey.bucketNumber,
			dataNode.dataKey.compositeKey,
			ccdId, key, dataNode.getValue())

		stateDelta.Set(ccdId, key, dataNode.getValue(), nil)
	}

	stateChunk := &protos.SyncStateChunk{}
	stateChunk.ChaincodeStateDeltas = stateDelta.ChaincodeStateDeltas

	return stateChunk, err
}


func (partialItr *PartialSnapshotIterator) GetMetaData() []byte {
	//TODO: return a series of bucketNode for the first offset
	return nil
}
