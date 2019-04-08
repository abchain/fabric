package statemgmt

import (
	_ "fmt"
	"github.com/abchain/fabric/protos"
)

func GetRequiredParts(itr PartialRangeIterator, offset *protos.SyncOffset) (*protos.SyncStateChunk, error) {
	err := itr.Seek(offset)
	if err != nil {
		return nil, err
	}

	stateDelta := NewStateDelta()
	stateChunk := &protos.SyncStateChunk{Offset: offset}
	stateChunk.MetaData = itr.GetMetaData()

	for itr.Next() {

		rawkey, value := itr.GetRawKeyValue()
		ccdId, key := DecodeCompositeKey(rawkey)
		stateDelta.Set(ccdId, key, value, nil)
	}
	stateChunk.ChaincodeStateDeltas = stateDelta.ChaincodeStateDeltas

	return stateChunk, err
}
