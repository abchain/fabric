package statesync

import (
	"fmt"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/flogging"
	pb "github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/proto"
	"github.com/looplab/fsm"
)

type stateServer struct {
	parent        ISyncHandler
	ledger        *ledger.LedgerSnapshot
	correlationId uint64
	pit           statemgmt.PartialRangeIterator
}

func newStateServer(h *stateSyncHandler) (s *stateServer) {

	s = &stateServer{
		parent: h.this,
	}
	l, _ := ledger.GetLedger()
	s.ledger = l.CreateSnapshot()

	return

}

//---------------------------------------------------------------------------
// 2. acknowledge query request
//---------------------------------------------------------------------------
func (server *stateServer) beforeQuery(e *fsm.Event) {

	payloadMsg := &pb.SyncStateQuery{}
	syncMsg := server.parent.onRecvSyncMsg(e, payloadMsg)
	if syncMsg == nil || server.correlationId != syncMsg.CorrelationId {
		return
	}

	block, err := server.ledger.GetBlockByNumber(payloadMsg.BlockHeight)
	if err != nil {
		server.ledger.Release()
		e.Cancel(err)
		return
	}

	resp := &pb.SyncStateResp{}
	resp.BlockHeight = payloadMsg.BlockHeight
	resp.Statehash = block.StateHash

	err = server.parent.sendSyncMsg(e, pb.SyncMsg_SYNC_SESSION_QUERY_ACK, resp)
	if err != nil {
		server.ledger.Release()
	}
}

//---------------------------------------------------------------------------
// 3. acknowledge sync block request
//---------------------------------------------------------------------------
//func (server *stateServer) beforeGetBlocks(e *fsm.Event) {
//	payloadMsg := &pb.SyncBlockRange{}
//	syncMsg := server.parent.onRecvSyncMsg(e, payloadMsg)
//	if syncMsg == nil || server.correlationId != syncMsg.CorrelationId {
//		return
//	}
//
//	go server.sendBlocks(e, payloadMsg)
//}

//---------------------------------------------------------------------------
// 4. acknowledge sync detal request
//---------------------------------------------------------------------------
func (server *stateServer) beforeGetDeltas(e *fsm.Event) {
	payloadMsg := &pb.SyncStateDeltasRequest{}
	syncMsg := server.parent.onRecvSyncMsg(e, payloadMsg)
	if syncMsg == nil || server.correlationId != syncMsg.CorrelationId {
		return
	}

	go server.sendStateDeltas(e, payloadMsg)
}

func (server *stateServer) enterServe(e *fsm.Event) {
	stateUpdate := "enterServe"
	server.dumpStateUpdate(stateUpdate)
}

func (server *stateServer) leaveServe(e *fsm.Event) {
	stateUpdate := "leaveServe"
	server.dumpStateUpdate(stateUpdate)
}

func (sts *stateServer) dumpStateUpdate(stateUpdate string) {
	logger.Debugf("%s: StateServer Syncing state update: %s. correlationId<%d>, remotePeerId<%s>", flogging.GoRDef,
		stateUpdate, sts.correlationId, sts.parent.remotePeerIdName())
}

//---------------------------------------------------------------------------
// 5. acknowledge sync end
//---------------------------------------------------------------------------
func (server *stateServer) beforeSyncEnd(e *fsm.Event) {
	syncMsg := server.parent.onRecvSyncMsg(e, nil)
	if syncMsg == nil || server.correlationId != syncMsg.CorrelationId {
		return
	}

	server.ledger.Release()
}

func (d *stateServer) sendStateDeltas(e *fsm.Event, syncStateDeltasRequest *pb.SyncStateDeltasRequest) {
	logger.Debugf("Sending state deltas for block range %d-%d", syncStateDeltasRequest.Range.Start,
		syncStateDeltasRequest.Range.End)
	var blockNums []uint64
	syncBlockRange := syncStateDeltasRequest.Range
	if syncBlockRange.Start > syncBlockRange.End {
		// Send in reverse order
		for i := syncBlockRange.Start; i >= syncBlockRange.End; i-- {
			blockNums = append(blockNums, i)
		}
	} else {
		//
		for i := syncBlockRange.Start; i <= syncBlockRange.End; i++ {
			logger.Debugf("%s: Appending to blockNums: %d", flogging.GoRDef, i)
			blockNums = append(blockNums, i)
		}
	}

	for _, currBlockNum := range blockNums {

		block, err := d.ledger.GetBlockByNumber(currBlockNum)
		if err != nil {
			logger.Errorf("Error sending blockNum %d: %s", currBlockNum, err)
			break
		}

		// Get the state deltas for Block from coordinator
		stateDelta, err := d.ledger.GetStateDelta(currBlockNum)
		if err != nil {
			logger.Errorf("Error sending stateDelta for blockNum %d: %s", currBlockNum, err)
			break
		}
		if stateDelta == nil {
			logger.Warningf("Requested to send a stateDelta for blockNum %d which has been discarded",
				currBlockNum)
			break
		}

		stateDeltaBytes := stateDelta.Marshal()

		blockState := &pb.BlockState{StateDelta: stateDeltaBytes, Block: block}
		syncStateDeltas := &pb.SyncBlockState{
			Range:    &pb.SyncBlockRange{Start: currBlockNum, End: currBlockNum, CorrelationId: syncBlockRange.CorrelationId},
			Syncdata: []*pb.BlockState{blockState}}

		if err := d.parent.sendSyncMsg(e, pb.SyncMsg_SYNC_SESSION_DELTAS_ACK, syncStateDeltas); err != nil {
			logger.Errorf("Error sending stateDeltas for blockNum %d: %s", currBlockNum, err)
			break
		}
		logger.Debugf("Successfully sent stateDeltas for blockNum %d", currBlockNum)
	}
}

//////////////////////////////////////////////////////////////////////////////
/////////  server
//////////////////////////////////////////////////////////////////////////////
func (server *stateServer) beforeSyncMessage(e *fsm.Event) {
	payloadMsg := &pb.SyncMessage{}
	syncMsg := server.parent.onRecvSyncMsg(e, payloadMsg)
	if syncMsg == nil || server.correlationId != syncMsg.CorrelationId {
		return
	}

	go server.sendSyncMessageResponse(e, payloadMsg)
}

func (d *stateServer) sendSyncMessageResponse(e *fsm.Event, message *pb.SyncMessage) {
	if message.PayloadType == pb.SyncType_SYNC_STATE {
		d.sendStateChuck(e, message.Offset)
	} else if message.PayloadType == pb.SyncType_SYNC_BLOCK {
		d.sendStateDeltasArray(e, message.Offset, message.CorrelationId)
	}
}

func (d *stateServer) sendStateDeltasArray(e *fsm.Event, offset *pb.SyncOffset, correlationId uint64) {

	blockOffset, err := offset.Unmarshal2BlockOffset()
	if err != nil {
		logger.Errorf("Error Unmarshal2BlockOffset: %s", err)
		panic("todo")
	}

	logger.Debugf("Sending state deltas for block range <%d-%d>", blockOffset.StartNum, blockOffset.EndNum)

	var blockNums []uint64
	syncBlockRange := &pb.SyncBlockRange{}

	syncBlockRange.Start = blockOffset.StartNum
	syncBlockRange.End = blockOffset.EndNum

	if syncBlockRange.Start > syncBlockRange.End {
		// Send in reverse order
		for i := syncBlockRange.Start; i >= syncBlockRange.End; i-- {
			blockNums = append(blockNums, i)
		}
	} else {
		for i := syncBlockRange.Start; i <= syncBlockRange.End; i++ {
			logger.Debugf("%s: Appending to blockNums: %d", flogging.GoRDef, i)
			blockNums = append(blockNums, i)
		}
	}

	blockStateArray := make([]*pb.BlockState, 0)

	failedReason := ""
	var currBlockNum uint64
	for _, currBlockNum = range blockNums {

		var block *pb.Block
		block, err = d.ledger.GetBlockByNumber(currBlockNum)
		if err != nil {
			break
		}

		var stateDelta *statemgmt.StateDelta
		// Get the state deltas for Block from coordinator
		stateDelta, err = d.ledger.GetStateDelta(currBlockNum)
		if err != nil {
			break
		}
		if stateDelta == nil {
			logger.Warningf("Requested to send a stateDelta for blockNum %d which has been discarded",
				currBlockNum)
			break
		}
		stateDeltaBytes := stateDelta.Marshal()

		logger.Debugf("currBlockNum<%d>, stateDelta: <%s>", currBlockNum, stateDelta)

		blockState := &pb.BlockState{StateDelta: stateDeltaBytes, Block: block}
		blockStateArray = append(blockStateArray, blockState)
	}

	if err != nil {
		failedReason += fmt.Sprintf("%s; ", err)
	}

	blockOffset.EndNum = currBlockNum
	syncStateDeltas := &pb.SyncBlockState{
		Range:    &pb.SyncBlockRange{Start: blockOffset.StartNum, End: currBlockNum, CorrelationId: correlationId},
		Syncdata: blockStateArray}

	var data []byte
	data, err = blockOffset.Byte()
	offset.Data = data

	if err != nil {
		failedReason += fmt.Sprintf("%s; ", err)
	}

	var syncMessage *pb.SyncMessage
	syncMessage, err = feedSyncMessage(offset, syncStateDeltas, pb.SyncType_SYNC_BLOCK_ACK)

	if err != nil {
		failedReason += fmt.Sprintf("%s; ", err)
	}
	syncMessage.FailedReason = failedReason

	err = d.parent.sendSyncMsg(e, pb.SyncMsg_SYNC_SESSION_SYNC_MESSAGE_ACK, syncMessage)
	if err != nil {
		logger.Errorf("Error sending SYNC_SESSION_SYNC_MESSAGE_ACK: %s", err)
	}

	logger.Debugf("Successfully sent stateDeltas for blockNum %d-%d", blockOffset.StartNum, currBlockNum)
}

func (server *stateServer) initStateSync(req *pb.SyncStartRequest, resp *pb.SyncStartResponse) error {

	syncState := &pb.SyncState{}
	err := proto.Unmarshal(req.Payload, syncState)
	if err != nil {
		return err
	}

	pit, err := server.ledger.GetParitalRangeIterator(nil)
	if err != nil {
		return err
	}
	server.pit = pit

	// TODO: send metaData and root hash
	//       get state hash by snapshot
	resp.Statehash = nil
	resp.BlockHeight, err = server.ledger.GetBlockchainSize()
	if err == nil {
		l, err := ledger.GetLedger()
		if err == nil {
			localStatehash, err := l.GetCurrentStateHash()
			if err == nil {
				logger.Infof("localStatehash<%x>, remoteHash: <%x>", localStatehash, syncState.Statehash)
			}
		}
	}

	return err
}

func (server *stateServer) sendStateChuck(e *fsm.Event, offset *pb.SyncOffset) {

	var failedReason string = ""
	var err error
	var stateChunkArray *pb.SyncStateChunk

	stateChunkArray, err = statemgmt.GetRequiredParts(server.pit, offset)

	if err != nil {
		failedReason = fmt.Sprintf("%s; ", err)
	} else {
		logger.Debugf("send <%s> stateDelta: <%s>", server.parent.remotePeerIdName(), stateChunkArray)
	}

	var syncMessage *pb.SyncMessage

	stateChunkArray.Offset = offset
	syncMessage, err = feedSyncMessage(offset, stateChunkArray, pb.SyncType_SYNC_STATE_ACK)
	if err != nil {
		failedReason += fmt.Sprintf("%s", err)
	}

	syncMessage.FailedReason = failedReason
	err = server.parent.sendSyncMsg(e, pb.SyncMsg_SYNC_SESSION_SYNC_MESSAGE_ACK, syncMessage)
	if err != nil {
		logger.Errorf("Error sending SYNC_SESSION_SYNC_MESSAGE_ACK: %s", err)
	}
}

func feedSyncMessage(offset *pb.SyncOffset, payload proto.Message, syncType pb.SyncType) (*pb.SyncMessage, error) {

	syncMessage := &pb.SyncMessage{}
	syncMessage.Offset = offset
	syncMessage.PayloadType = syncType
	var err error
	syncMessage.Payload, err = proto.Marshal(payload)
	return syncMessage, err
}
