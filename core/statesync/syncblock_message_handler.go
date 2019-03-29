package statesync

import (
	_ "github.com/abchain/fabric/core/ledger/statemgmt"
	pb "github.com/abchain/fabric/protos"

	"fmt"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/core/util"
	"github.com/golang/protobuf/proto"
)

type BlockMessageHandler struct {
	client                  *syncer
	statehash               []byte
	startBlockNumber        uint64
	endBlockNumber          uint64
	currentStateBlockNumber uint64
	delta                   uint64
}

func newBlockMessageHandler(startBlockNumber, endBlockNumber uint64, client *syncer) *BlockMessageHandler {
	handler := &BlockMessageHandler{}
	handler.client = client
	handler.delta = 5
	handler.startBlockNumber = startBlockNumber
	handler.endBlockNumber = endBlockNumber
	handler.currentStateBlockNumber = startBlockNumber - 1
	return handler
}

func (h *BlockMessageHandler) feedPayload(syncMessage *pb.SyncMessage) error {
	syncMessage.PayloadType = pb.SyncType_SYNC_BLOCK
	return nil
}

func (h *BlockMessageHandler) getInitialOffset() (*pb.SyncOffset, error) {

	offset := &pb.SyncOffset{}

	end := h.startBlockNumber + h.delta - 1
	end = util.Min(end, h.endBlockNumber)

	blockOffset := &pb.BlockOffset{h.startBlockNumber,
		end}

	logger.Infof("Initial offset: <%v>", blockOffset)

	data, err := blockOffset.Byte()
	if err == nil {
		offset.Data = data
	}

	return offset, err
}

func (h *BlockMessageHandler) produceSyncStartRequest() *pb.SyncStartRequest {
	return nil
}

func (h *BlockMessageHandler) processBlockState(deltaMessage *pb.SyncBlockState) (uint64, error) {

	sts := h.client
	endBlockNumber := deltaMessage.Range.End
	h.currentStateBlockNumber++

	if deltaMessage.Range.Start != h.currentStateBlockNumber ||
		deltaMessage.Range.End < deltaMessage.Range.Start ||
		deltaMessage.Range.End > endBlockNumber {
		err := fmt.Errorf(
			"Received a state delta either in the wrong order (backwards) or "+
				"not next in sequence, aborting, start=%d, end=%d",
			deltaMessage.Range.Start, deltaMessage.Range.End)
		return h.currentStateBlockNumber, err
	}

	localBlock, err := sts.ledger.GetBlockByNumber(deltaMessage.Range.Start - 1)
	if err != nil {
		return deltaMessage.Range.Start, err
	}
	h.statehash = localBlock.StateHash

	logger.Debugf("deltaMessage syncdata len<%d>, block chunk: <%s>", len(deltaMessage.Syncdata),
		deltaMessage.Range)

	for _, syncData := range deltaMessage.Syncdata {

		deltaByte := syncData.StateDelta

		block := syncData.Block
		umDelta := statemgmt.NewStateDelta()
		if err = umDelta.Unmarshal(deltaByte); nil != err {
			err = fmt.Errorf("Received a corrupt state delta from %s : %s",
				sts.parent.remotePeerIdName(), err)
			break
		}
		logger.Debugf("Current Block Number<%d>, umDelta len<%d>, deltaMessage.Syncdata <%x>",
			h.currentStateBlockNumber, len(umDelta.ChaincodeStateDeltas), deltaByte)

		sts.ledger.ApplyStateDelta(deltaMessage, umDelta)

		if block != nil {
			h.statehash, err = sts.sanityCheckBlock(block, h.statehash, h.currentStateBlockNumber, deltaMessage)
			if err != nil {
				break
			}
		}

		if err = sts.ledger.CommitAndIndexStateDelta(deltaMessage, h.currentStateBlockNumber); err != nil {
			sts.stateValid = false
			err = fmt.Errorf("Played state forward according to %s, "+
				"hashes matched, but failed to commit, invalidated state", sts.parent.remotePeerIdName())
			break
		}

		//we can still forward even if we can't persist the block
		if errPutBlock := sts.ledger.PutBlock(h.currentStateBlockNumber, block); errPutBlock != nil {
			logger.Warningf("err <Put block fail: %s>", errPutBlock)
		}

		logger.Infof("Successfully moved state to height %d",
			h.currentStateBlockNumber + 1)

		if h.currentStateBlockNumber == endBlockNumber {
			break
		}

		h.currentStateBlockNumber++
	}

	return h.currentStateBlockNumber, err
}

func (h *BlockMessageHandler) processResponse(syncMessage *pb.SyncMessage) (*pb.SyncOffset, error) {

	syncBlockStateResp := &pb.SyncBlockState{}
	err := proto.Unmarshal(syncMessage.Payload, syncBlockStateResp)
	if err != nil {
		return nil, err
	}

	if len(syncMessage.FailedReason) > 0 {
		err = fmt.Errorf("Sync state failed! Reason: %s", syncMessage.FailedReason)
		return nil, err
	}

	_, err = h.processBlockState(syncBlockStateResp)
	if err != nil {
		return nil, err
	}

	var nextOffset *pb.SyncOffset
	if h.endBlockNumber > syncBlockStateResp.Range.End {
		start := syncBlockStateResp.Range.Start + h.delta
		end := syncBlockStateResp.Range.End + h.delta

		end = util.Min(end, h.endBlockNumber)

		nextOffset = pb.NewBlockOffset(start, end)
	}

	if nextOffset == nil {
		logger.Infof("Caught up to block %d, and state is now valid at hash <%x>", h.currentStateBlockNumber, h.statehash)
	}

	return nextOffset, err
}
