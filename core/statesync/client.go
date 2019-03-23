package statesync

import (
	"bytes"
	"fmt"
	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/core/util"
	"github.com/abchain/fabric/flogging"
	pb "github.com/abchain/fabric/protos"
	"github.com/looplab/fsm"
	"github.com/spf13/viper"
	"golang.org/x/net/context"
	"time"
)

type syncer struct {
	context.Context
	//parent     *stateSyncHandler
	ledger     *ledger.Ledger
	parent 		ISyncHandler

	positionResp chan *pb.SyncStateResp
	//deltaResp    chan *pb.SyncBlockState
	syncMessageChan   chan *pb.SyncMessage
	startResponseChan chan *pb.SyncStartResponse

	correlationId uint64

	maxStateDeltas     int    // The maximum number of state deltas to attempt to retrieve before giving up and performing a full state snapshot retrieval
	maxBlockRange      uint64 // The maximum number blocks to attempt to retrieve at once, to prevent from overflowing the peer's buffer
	maxStateDeltaRange uint64 // The maximum number of state deltas to attempt to retrieve at once, to prevent from overflowing the peer's buffer
	RecoverDamage      bool   // Whether state transfer should ever modify or delete existing blocks if they are determined to be corrupted

	DiscoveryThrottleTime time.Duration // The amount of time to wait after discovering there are no connected peers
	stateValid            bool          // Are we currently operating under the assumption that the state is valid?
	inProgress            bool          // Set when state transfer is in progress so that the state may not be consistent
	blockVerifyChunkSize  uint64        // The max block length to attempt to sync at once, this prevents state transfer from being delayed while the blockchain is validated

	BlockRequestTimeout         time.Duration // How long to wait for a peer to respond to a block request
	StateDeltaRequestTimeout    time.Duration // How long to wait for a peer to respond to a state delta request
	StateSnapshotRequestTimeout time.Duration // How long to wait for a peer to respond to a state snapshot request
	branchNode2CheckpointMap    map[string][][]byte

	syncMessageHandler SyncMessageHandler
}


func newSyncer(ctx context.Context, h *stateSyncHandler) (sts *syncer) {

	l, _ := ledger.GetLedger()

	sts = &syncer{
		ledger:  l,
		Context: ctx,
		parent:  h.this,
	}

	var err error
	sts.positionResp = make(chan *pb.SyncStateResp)
	//sts.deltaResp = make(chan *pb.SyncBlockState)
	sts.syncMessageChan = make(chan *pb.SyncMessage)
	sts.startResponseChan = make(chan *pb.SyncStartResponse)


	sts.RecoverDamage = viper.GetBool("statetransfer.recoverdamage")

	sts.blockVerifyChunkSize = uint64(viper.GetInt("statetransfer.blocksperrequest"))
	if sts.blockVerifyChunkSize == 0 {
		panic(fmt.Errorf("Must set statetransfer.blocksperrequest to be nonzero"))
	}

	sts.DiscoveryThrottleTime = 1 * time.Second

	sts.BlockRequestTimeout, err = time.ParseDuration(viper.GetString("statetransfer.timeout.singleblock"))
	if err != nil {
		panic(fmt.Errorf("Cannot parse statetransfer.timeout.singleblock timeout: %s", err))
	}
	sts.StateDeltaRequestTimeout, err = time.ParseDuration(viper.GetString("statetransfer.timeout.singlestatedelta"))
	if err != nil {
		panic(fmt.Errorf("Cannot parse statetransfer.timeout.singlestatedelta timeout: %s", err))
	}
	sts.StateSnapshotRequestTimeout, err = time.ParseDuration(viper.GetString("statetransfer.timeout.fullstate"))
	if err != nil {
		panic(fmt.Errorf("Cannot parse statetransfer.timeout.fullstate timeout: %s", err))
	}

	sts.maxStateDeltas = viper.GetInt("statetransfer.maxdeltas")
	if sts.maxStateDeltas <= 0 {
		panic(fmt.Errorf("sts.maxdeltas must be greater than 0"))
	}

	tmp := viper.GetInt("peer.sync.blocks.channelSize")
	if tmp <= 0 {
		panic(fmt.Errorf("peer.sync.blocks.channelSize must be greater than 0"))
	}
	sts.maxBlockRange = uint64(tmp)

	tmp = viper.GetInt("peer.sync.state.deltas.channelSize")
	if tmp <= 0 {
		panic(fmt.Errorf("peer.sync.state.deltas.channelSize must be greater than 0"))
	}
	sts.maxStateDeltaRange = uint64(tmp)

	return
}

//---------------------------------------------------------------------------
// 1. receive start confirmed
//---------------------------------------------------------------------------
func (sts *syncer) afterSyncStartResponse(e *fsm.Event) {
	// implicitly go into synclocating state
	resp := &pb.SyncStartResponse{}
	msg := sts.parent.onRecvSyncMsg(e, resp)

	if msg == nil {
		e.Cancel(fmt.Errorf("unexpected sync message"))
		return
	}
	sts.startResponseChan <- resp
}

//---------------------------------------------------------------------------
// 2. receive query response
//---------------------------------------------------------------------------
func (sts *syncer) afterQueryResponse(e *fsm.Event) {
	payloadMsg := &pb.SyncStateResp{}
	msg := sts.parent.onRecvSyncMsg(e, payloadMsg)
	if msg == nil {
		return
	}
	sts.positionResp <- payloadMsg
}

//---------------------------------------------------------------------------
// 3. receive block response
//---------------------------------------------------------------------------
//func (sts *syncer) afterSyncBlocks(e *fsm.Event) {
//
//	payloadMsg := &pb.SyncBlocks{}
//	msg := sts.parent.onRecvSyncMsg(e, payloadMsg)
//	if msg == nil {
//		return
//	}
//	sts.blocksResp <- payloadMsg
//}

//---------------------------------------------------------------------------
// 4. receive delta response
//---------------------------------------------------------------------------
//func (sts *syncer) afterSyncStateDeltas(e *fsm.Event) {
//
//	payloadMsg := &pb.SyncBlockState{}
//	msg := sts.parent.onRecvSyncMsg(e, payloadMsg)
//	if msg == nil {
//		return
//	}
//	sts.deltaResp <- payloadMsg
//}

func (sts *syncer) getSyncTargetBlockNumber() (uint64, uint64, error) {

	targetBlockNumber := uint64(0)

	req := &pb.SyncStartRequest{0, pb.SyncType_SYNC_BLOCK, nil}
	endBlockNumber, err := sts.issueSyncRequest(req)

	if err != nil {
		return 0, 0, err
	}

	var start uint64 = 0
	end := endBlockNumber
	for {

		if targetBlockNumber == (start+end)/2 {
			break
		}

		targetBlockNumber = (start + end) / 2

		err = sts.parent.sendSyncMsg(nil, pb.SyncMsg_SYNC_SESSION_QUERY,
			&pb.SyncStateQuery{uint32(sts.correlationId), targetBlockNumber})

		if err != nil {
			return 0, 0, err
		}

		select {
		case response, ok := <-sts.positionResp:
			if !ok {
				return 0, 0, fmt.Errorf("had block channel close : %s", err)
			}

			if sts.ledger.GetGlobalState(response.Statehash) != nil {
				start = targetBlockNumber
			} else {
				end = targetBlockNumber
			}

			logger.Debugf("start<%d>, end<%d>, targetBlockNumber<%d>, endBlockNumber<%d>",
				start, end, targetBlockNumber, endBlockNumber)

			case <-sts.Done():
				return 0, 0, fmt.Errorf("Timed out during get SyncTargetBlockNumber")
		}
	}

	logger.Infof("return: start<%d>, end<%d>, targetBlockNumber<%d>, endBlockNumber<%d>",
		start, end, targetBlockNumber, endBlockNumber)
	return targetBlockNumber, endBlockNumber, nil
}

func (sts *syncer) sanityCheckBlock(block *pb.Block, stateHash []byte, blockNum uint64,
	deltaMessage *pb.SyncBlockState) ([]byte, error) {

	success := false
	lastStateHash := stateHash
	stateHash, err := sts.ledger.GetCurrentStateHash()
	if err != nil {
		logger.Warningf("Could not compute state hash for some reason: %s", err)
	}

	logger.Debugf("Played state forward from %s to block %d with StateHash (%x), "+
		"block has StateHash (%x)",
		sts.parent.remotePeerIdName(),
		blockNum, stateHash,
		block.StateHash)

	if bytes.Equal(block.StateHash, stateHash) {
		success = true
		//add new statehash, and we omit errors
		sts.ledger.AddGlobalState(lastStateHash, stateHash)
		lastStateHash = stateHash
	}

	if !success {
		if sts.ledger.RollbackStateDelta(deltaMessage) != nil {
			sts.stateValid = false
			err = fmt.Errorf(
				"played state forward according to %s, but the state hash did not match, "+
					"failed to roll back, invalidated state",
				sts.parent.remotePeerIdName())
			return nil, err
		}
		err = fmt.Errorf("Played state forward according to %s, "+
			"but the state hash did not match, rolled back", sts.parent.remotePeerIdName())
		return nil, err
	}

	return lastStateHash, nil
}


//func (sts *syncer) syncDeltas(startBlockNumber, endBlockNumber uint64) (uint64, error) {
//	logger.Debugf("Attempting to play state forward from %v to block %d",
//		sts.parent.remotePeerIdName(),
//		endBlockNumber)
//	var stateHash []byte
//
//	intermediateBlock := endBlockNumber
//	currentStateBlockNumber := startBlockNumber
//
//	localBlock, err := sts.ledger.GetBlockByNumber(startBlockNumber - 1)
//	if err != nil {
//		return startBlockNumber, err
//	}
//	lastStateHash := localBlock.StateHash
//
//	logger.Debugf("Requesting state delta range from %d to %d",
//		startBlockNumber, intermediateBlock)
//
//	syncBlockRange := &pb.SyncBlockRange{
//		sts.correlationId,
//		currentStateBlockNumber,
//		intermediateBlock,
//	}
//	payloadMsg := &pb.SyncStateDeltasRequest{Range: syncBlockRange}
//	err = sts.parent.sendSyncMsg(nil, pb.SyncMsg_SYNC_SESSION_DELTAS, payloadMsg)
//
//	if err != nil {
//		return startBlockNumber, fmt.Errorf("Received an error while trying to get "+
//			"the state deltas for blocks %d through %d from %s",
//			currentStateBlockNumber, intermediateBlock, sts.parent.remotePeerIdName())
//	}
//
//	for currentStateBlockNumber <= intermediateBlock {
//		select {
//		case deltaMessage, ok := <-sts.deltaResp:
//			if !ok {
//				err = fmt.Errorf("Was only able to recover to block number %d when desired to recover to %d",
//					currentStateBlockNumber, endBlockNumber)
//				break
//			}
//
//			if deltaMessage.Range.Start != currentStateBlockNumber ||
//				deltaMessage.Range.End < deltaMessage.Range.Start ||
//				deltaMessage.Range.End > endBlockNumber {
//				err = fmt.Errorf(
//					"Received a state delta either in the wrong order (backwards) or "+
//						"not next in sequence, aborting, start=%d, end=%d",
//					deltaMessage.Range.Start, deltaMessage.Range.End)
//				break
//			}
//
//			for _, syncData := range deltaMessage.Syncdata {
//
//				logger.Debugf("deltaMessage.Syncdata len<%d>, block num: <%s>", len(deltaMessage.Syncdata),
//					deltaMessage.Range)
//
//				deltaByte := syncData.StateDelta
//
//				logger.Debugf("deltaMessage.Syncdata <%x>", deltaByte)
//				block := syncData.Block
//				umDelta := statemgmt.NewStateDelta()
//				if err := umDelta.Unmarshal(deltaByte); nil != err {
//					err = fmt.Errorf("Received a corrupt state delta from %s : %s",
//						sts.parent.remotePeerIdName(), err)
//					break
//				}
//				logger.Debugf("Recv block [<%d>, <%d>]. currentStateBlockNumber<%d>, umDelta len<%d>",
//					deltaMessage.Range.Start,
//					deltaMessage.Range.End,
//					currentStateBlockNumber, len(umDelta.ChaincodeStateDeltas))
//				sts.ledger.ApplyStateDelta(deltaMessage, umDelta)
//
//				if block != nil {
//					lastStateHash, err = sts.sanityCheckBlock(block, lastStateHash, currentStateBlockNumber, deltaMessage)
//					if err != nil {
//						logger.Errorf("sanityCheckBlock err: %s", err)
//						break
//					}
//				}
//
//				logger.Debugf("sts.ledger.CommitStateDelta: sts.currentStateBlockNumber<%d>",
//					currentStateBlockNumber)
//
//				if err := sts.ledger.CommitAndIndexStateDelta(deltaMessage, currentStateBlockNumber); err != nil {
//					sts.stateValid = false
//					err = fmt.Errorf("Played state forward according to %s, "+
//						"hashes matched, but failed to commit, invalidated state", sts.parent.remotePeerIdName())
//					logger.Errorf("%s", err)
//					break
//				}
//
//				//we can still forward even if we can't persist the block
//				if err := sts.ledger.PutBlock(currentStateBlockNumber, block); err != nil {
//					logger.Warningf("err <Put block fail: %s>", err)
//				}
//
//				logger.Debugf("Moved state to %d, endBlockNumber: %d", currentStateBlockNumber, endBlockNumber)
//
//				if currentStateBlockNumber == endBlockNumber {
//					logger.Infof("Caught up to block %d", currentStateBlockNumber)
//
//					logger.Infof("State is now valid at block %d and hash %x", currentStateBlockNumber, stateHash)
//					return currentStateBlockNumber, err
//				}
//
//				currentStateBlockNumber++
//			}
//			if err != nil || currentStateBlockNumber == endBlockNumber {
//				break
//			}
//		case <-time.After(sts.StateDeltaRequestTimeout):
//			logger.Warningf("Timed out during state delta recovery from %s", sts.parent.remotePeerIdName())
//			err = fmt.Errorf("timed out during state delta recovery from %s", sts.parent.remotePeerIdName())
//			break
//		}
//
//		if err != nil {
//			break
//		}
//	}
//
//	if err == nil {
//		logger.Infof("State is now valid at block %d and hash %x", currentStateBlockNumber, stateHash)
//	}
//	return currentStateBlockNumber, err
//}


func (sts *syncer) leaveSyncLocating(e *fsm.Event) {

	stateUpdate := "leaveSyncLocating"
	sts.dumpStateUpdate(stateUpdate)
}

func (sts *syncer) leaveSyncBlocks(e *fsm.Event) {

	stateUpdate := "leaveSyncBlocks"
	sts.dumpStateUpdate(stateUpdate)
}

func (sts *syncer) leaveSyncStateSnapshot(e *fsm.Event) {

	stateUpdate := "leaveSyncStateSnapshot"
	sts.dumpStateUpdate(stateUpdate)
}

func (sts *syncer) leaveSyncStateDeltas(e *fsm.Event) {

	stateUpdate := "leaveSyncStateDeltas"
	sts.dumpStateUpdate(stateUpdate)
}

//func (sts *syncer) leaveIdle(e *fsm.Event) {
//
//	stateUpdate := "leaveIdle"
//	sts.dumpStateUpdate(stateUpdate)
//}

//========================================================
func (sts *syncer) enterSyncLocating(e *fsm.Event) {

	stateUpdate := "enterSyncLocating"
	sts.dumpStateUpdate(stateUpdate)
}

func (sts *syncer) enterSyncBlocks(e *fsm.Event) {

	stateUpdate := "enterSyncBlocks"
	sts.dumpStateUpdate(stateUpdate)
}

func (sts *syncer) enterSyncStateSnapshot(e *fsm.Event) {

	stateUpdate := "enterSyncStateSnapshot"
	sts.dumpStateUpdate(stateUpdate)
}

func (sts *syncer) enterSyncStateDeltas(e *fsm.Event) {

	stateUpdate := "enterSyncStateDeltas"
	sts.dumpStateUpdate(stateUpdate)
}

func (sts *syncer) dumpStateUpdate(stateUpdate string) {
	logger.Debugf("%s: Syncer Syncing state update: %s. correlationId<%d>, remotePeerId<%s>", flogging.GoRDef,
		stateUpdate, sts.correlationId, sts.parent.remotePeerIdName())
}

//------------------------------------------------------------------
// The Global State Graph
// [G]: genesis
// [A]: last branch node of [B]
// [B]: last branch node of [startBlockNumber]
// [C1], [C2], [C3]: checkpoints
//
// [G]->[]->[C1]->[A]->[]->[C2]->[]->[C3]-[][][][]->[B]->[]->[startBlockNumber]->[][][]
//                |                                 |
//                V                                 V
//               [ ]                               [B1]

// host 1
// [G]->[]->[C1]->[A]->[]->[C2]->[]->[C3]-[][][][]->[B]->[]->[startBlockNumber]->[][][]

// host 2
// [G]->[]->[C1]->[A]->[]->[C2]->[]->[C3]-[][][][]->[B]
//                                                  |
//                                                  V
//                                                 []
// host 3
// [G]->[]->[C1]->[A]
//                |
//                V
//               [ ]

// Target: Switch to B2 from startBlockNumber
// Prerequisite: go to the best checkpoint(preference: C3->C2->C1) from [startBlockNumber]:
// 1. Go to [B] from [startBlockNumber]
// 2. Go and try to switch to [C3], then return if success
// 3. Go and try to switch to [C2] if #2 fails, then return if success
// 4. GO to [A] if #3 fails
// 5. Go and try to switch to [C1], then return if success
// 6. Go and try to switch to [G] if #5 fails

//
//------------------------------------------------------------------

func (sts *syncer) switchToBestCheckpoint(startBlockNumber uint64) (uint64, error) {

	genesis, _ := sts.ledger.GetBlockByNumber(0)

	checkpointsMap := make(map[string]bool)
	checkpointList := db.GetGlobalDBHandle().ListCheckpoints()

	for _, cp := range checkpointList {
		checkpointsMap[util.EncodeStatehash(cp)] = true
	}

	sts.branchNode2CheckpointMap = traverseGlobalStateGraph(genesis.StateHash, checkpointsMap, true)

	var checkpointPosition uint64
	var checkpointStatehash []byte

	statehash, err := sts.GetStateHash(startBlockNumber)
	if err != nil {
		return checkpointPosition, err
	}

	for {
		gs := db.GetGlobalDBHandle().GetGlobalState(statehash)

		if gs == nil {
			err = fmt.Errorf("failed to fetch GlobalState <%x>", statehash)
			break
		}

		lastBranchNodeStateHash := gs.LastBranchNodeStateHash

		if lastBranchNodeStateHash == nil {
			err = fmt.Errorf(
				"the GlobalState <%x> does not have a previous branch node", statehash)
			break
		}

		checkpointPosition, checkpointStatehash, err = sts.switchToCheckpointByBranchNode(lastBranchNodeStateHash)
		if err != nil {
			// visiting the previous lastBranchNodeStateHash
			statehash = lastBranchNodeStateHash
		} else {

			logger.Infof("[%s]: Switch to checkpoint: <%x>, block num: <%d>",
				flogging.GoRDef, checkpointStatehash, checkpointPosition)

			break
		}
	}

	if err != nil {
		// try to switch to genesis
		var genesisStateHash []byte
		genesisStateHash, err = sts.GetStateHash(0)

		if err == nil {
			checkpointPosition, err = sts.stateSwitch(genesisStateHash)
		}
	}

	return checkpointPosition, err
}

func (sts *syncer) switchToCheckpointByBranchNode(branchNodeStateHash []byte) (uint64, []byte, error) {

	if branchNodeStateHash == nil {
		panic(fmt.Errorf("the branchNodeStateHash is nil"))
	}

	checkpointList, ok := sts.branchNode2CheckpointMap[util.EncodeStatehash(branchNodeStateHash)]

	var err error
	var checkpointPosition uint64
	var checkpointStateHash []byte
	len := len(checkpointList)
	if ok && len > 0 {
		// start from the end of checkpointList
		for i := int(len - 1); i >= 0; i-- {
			checkpointPosition, err = sts.stateSwitch(checkpointList[i])

			if err == nil {
				checkpointStateHash = checkpointList[i]
				break
			}
		}
	} else {
		err = fmt.Errorf("failed to switch to any of checkpoints of the branch node: <%x>",
			branchNodeStateHash)
	}
	return checkpointPosition, checkpointStateHash, err
}

func traverseGlobalStateGraph(genesisStateHash []byte, checkpointsMap map[string]bool, hexEncoded bool) map[string][][]byte {

	var stateHashList [][]byte
	branchNode2CheckpointMap := make(map[string][][]byte)

	stateHashList = append(stateHashList, genesisStateHash)

	for i := 0; i < len(stateHashList); i++ {

		stateHash := stateHashList[i]
		nextBranchNodeStateHash, checkpointList := traverseTillBranch(stateHash, checkpointsMap, hexEncoded)

		if nextBranchNodeStateHash != nil {

			var stringStateHash string
			if hexEncoded {
				stringStateHash = util.EncodeStatehash(nextBranchNodeStateHash)
			} else {
				stringStateHash = string(nextBranchNodeStateHash)
			}
			_, ok := branchNode2CheckpointMap[stringStateHash]
			if !ok {
				branchNode2CheckpointMap[stringStateHash] = checkpointList
			} else {
				panic(fmt.Errorf("Duplicated state hash: %s", stringStateHash))
			}

			branchNodeGs := db.GetGlobalDBHandle().GetGlobalState(nextBranchNodeStateHash)
			stateHashList = append(stateHashList, branchNodeGs.NextNodeStateHash...)
		}
	}

	return branchNode2CheckpointMap
}

func traverseTillBranch(startStateHash []byte, checkpointMap map[string]bool, hexEncoded bool) ([]byte, [][]byte) {
	var checkpointList [][]byte
	curcorStateHash := startStateHash

	for {

		var stringStateHash string
		if hexEncoded {
			stringStateHash = util.EncodeStatehash(curcorStateHash)
		} else {
			stringStateHash = string(curcorStateHash)
		}

		_, ok := checkpointMap[stringStateHash]
		if ok {
			checkpointList = append(checkpointList, curcorStateHash)
		}

		curcor := db.GetGlobalDBHandle().GetGlobalState(curcorStateHash)
		childNodeNumber := len(curcor.NextNodeStateHash)

		if childNodeNumber == 1 {
			curcorStateHash = curcor.NextNodeStateHash[0]
		} else if childNodeNumber == 0 {
			// hit the end, return
			curcorStateHash = nil
			checkpointList = nil
			break
		} else if childNodeNumber > 1 {
			// return on hitting a branch node
			break
		}
	}

	return curcorStateHash, checkpointList
}

func (sts *syncer) GetStateHash(targetBlockNumber uint64) ([]byte, error) {

	block, err := sts.ledger.GetBlockByNumber(targetBlockNumber)
	if err != nil {
		return nil, fmt.Errorf("Error fetching block %d.", targetBlockNumber)
	}

	return block.StateHash, nil
}

func (sts *syncer) stateSwitch(statehash []byte) (uint64, error) {

	gs := db.GetGlobalDBHandle().GetGlobalState(statehash)

	if gs == nil {
		return 0, fmt.Errorf(
			"StateSwitch: Failed to find the GlobalState by the state hash <%x>",
			statehash)
	}

	err := db.GetDBHandle().StateSwitch(statehash)
	if err != nil {
		return 0, err
	}

	return gs.Count, nil
}


func (sts *syncer) fini() {

	//select {
	//case <-sts.deltaResp:
	//default:
	//	logger.Debugf("close deltaResp channel")
	//	close(sts.deltaResp)
	//}

	select {
	case <-sts.positionResp:
	default:
		logger.Debugf("close positionResp channel")
		close(sts.positionResp)
	}

	select {
	case <-sts.startResponseChan:
	default:
		logger.Debugf("close startResponseChan channel")
		close(sts.startResponseChan)
	}

	defer func() {
		if x := recover(); x != nil {
			logger.Errorf("Error sending syncMessageChan to channel: %v",x)
		}
	}()

	select {
	case <-sts.syncMessageChan:
	default:
		logger.Debugf("close syncMessageChan channel")
		close(sts.syncMessageChan)
	}
}



//func (sts *syncer) syncBlocks(startBlock, endBlock uint64) error {
//
//	logger.Debugf("Syncing blocks from %d to %d", startBlock, endBlock)
//	blockCursor := startBlock
//	var block *pb.Block
//	var err error
//
//	intermediateBlock := blockCursor + 1
//
//	if intermediateBlock == blockCursor+1 {
//
//		if sts.maxBlockRange > blockCursor {
//			// Don't underflow
//			intermediateBlock = 0
//		} else {
//			intermediateBlock = blockCursor - sts.maxBlockRange
//		}
//
//		if intermediateBlock < endBlock {
//			intermediateBlock = endBlock
//		}
//		logger.Debugf("sts.correlationId<%d>, Requesting block range from %d to %d",
//			sts.correlationId,
//			blockCursor, intermediateBlock)
//
//		err = sts.parent.sendSyncMsg(nil, pb.SyncMsg_SYNC_SESSION_GET_BLOCKS,
//			&pb.SyncBlockRange{
//				sts.correlationId,
//				blockCursor,
//				intermediateBlock})
//		if err != nil {
//			return err
//		}
//	}
//
//	for {
//
//		if nil != err {
//			logger.Warningf("Failed to get blocks from %d to %d from %v: %s",
//				blockCursor, endBlock, sts.parent.remotePeerIdName(), err)
//			return err
//		}
//
//		select {
//		case syncBlockMessage, ok := <-sts.blocksResp:
//
//			if !ok {
//				return fmt.Errorf("Channel closed before we could finish reading")
//			}
//
//			if syncBlockMessage.Range.Start < syncBlockMessage.Range.End {
//				// If the message is not replying with blocks backwards, we did not ask for it
//				return fmt.Errorf("Received a block with wrong (increasing) order from %v, aborting",
//					sts.parent.remotePeerIdName())
//			}
//
//			var i int
//			for i, block = range syncBlockMessage.Blocks {
//				// It no longer correct to get duplication or out of range blocks, so we treat this as an error
//				if syncBlockMessage.Range.Start-uint64(i) != blockCursor {
//					return fmt.Errorf("Received a block out of order, indicating a buffer "+
//						"overflow or other corruption: start=%d, end=%d, wanted %d",
//						syncBlockMessage.Range.Start, syncBlockMessage.Range.End, blockCursor)
//				}
//
//				logger.Debugf("Putting block %d to with PreviousBlockHash %x and StateHash %x",
//					blockCursor, block.PreviousBlockHash, block.StateHash)
//
//				sts.ledger.PutBlock(blockCursor, block)
//
//				if blockCursor == endBlock {
//					logger.Infof("Successfully synced from block %d to block %d", startBlock, endBlock)
//					return nil
//				}
//				blockCursor++
//
//			}
//		case <-time.After(sts.BlockRequestTimeout):
//			return fmt.Errorf("Had block sync request to %s time out", sts.parent.remotePeerIdName())
//		}
//	}
//
//	if nil != block {
//		logger.Debugf("Returned from sync with block %d, state hash %x", blockCursor, block.StateHash)
//	} else {
//		logger.Debugf("Returned from sync with no new blocks")
//	}
//
//	return err
//}


//////////////////////////////////////////////////////////////////////////////
/////////  client
//////////////////////////////////////////////////////////////////////////////
func (sts *syncer) commitStateChunk(stateChunkArray *pb.SyncStateChunk, committedOffset *pb.SyncOffset) error {
	var err error

	umDelta := statemgmt.NewStateDelta()
	umDelta.ChaincodeStateDeltas = stateChunkArray.ChaincodeStateDeltas

	err = sts.ledger.ApplyStateDelta(stateChunkArray, umDelta)
	if err != nil {
		return err

	}

	err = sts.ledger.CommitAndIndexStateDelta(stateChunkArray, 0)
	if err != nil {
		return err
	}

	//err = sts.ledger.SaveStateOffset(committedOffset)
	//if err != nil {
	//	return err
	//}

	return err
}

func (sts *syncer) executeSync() error {

	nextOffset, err := sts.syncMessageHandler.getInitialOffset()

	if err != nil {
		return err
	}

	for {
		syncMessage := &pb.SyncMessage{}
		syncMessage.Offset = nextOffset

		// 1. feed payload
		err = sts.syncMessageHandler.feedPayload(syncMessage)
		if err != nil {
			break
		}

		err = sts.parent.sendSyncMsg(nil, pb.SyncMsg_SYNC_SESSION_SYNC_MESSAGE, syncMessage)
		if err != nil {
			break
		}

		// 2. process response
		var syncMessageResp *pb.SyncMessage
		var ok bool

		select {
		case syncMessageResp, ok = <- sts.syncMessageChan:
			if !ok {
				err = fmt.Errorf("sync Message channel close")
				break
			}

		case <-sts.Done():
			err = fmt.Errorf("Timed out during wait for sync message Response")
			break
		}

		if !ok {
			err = fmt.Errorf("sts.syncMessageChan error")
			break
		}

		nextOffset, err = sts.syncMessageHandler.processResponse(syncMessageResp)
		if err != nil {
			break
		}

		// 3. no more data to sync
		if nextOffset == nil {
			break
		}
	}

	return err
}


func (sts *syncer) afterSyncMessage(e *fsm.Event) {

	payloadMsg := &pb.SyncMessage{}
	msg := sts.parent.onRecvSyncMsg(e, payloadMsg)
	if msg == nil {
		return
	}

	defer func() {
		if x := recover(); x != nil {
			logger.Errorf("Error sending syncMessageChan to channel: %v, <%v>", x, payloadMsg)
		}
	}()

	sts.syncMessageChan <- payloadMsg
}



func (sts *syncer) issueSyncRequest(request *pb.SyncStartRequest) (uint64, error) {

	sts.parent.getFsm().Event(enterSyncBegin)
	err := sts.parent.sendSyncMsg(nil, pb.SyncMsg_SYNC_SESSION_START, request)
	endBlockNumber := uint64(0)

	if err != nil {
		return 0, err
	}

	select {
	case response, ok := <-sts.startResponseChan:
		if !ok {
			return 0, fmt.Errorf("startResponse channel close : %s", err)
		}
		logger.Infof("Sync request RejectedReason<%s>, Remote peer Blockchain Height <%d>",
			response.RejectedReason, response.BlockHeight)

		if len(response.RejectedReason) > 0 {
			err = fmt.Errorf("Sync request rejected! Reason: %s", response.RejectedReason)
		} else {
			endBlockNumber = response.BlockHeight - 1
		}

	case <-sts.Done():
		return 0, fmt.Errorf("Timed out during wait for start Response")
	}

	return endBlockNumber, err
}