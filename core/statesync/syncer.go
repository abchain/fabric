package statesync

import (
	"fmt"
	"github.com/abchain/fabric/core/ledger"
	_ "github.com/abchain/fabric/core/ledger/statemgmt"
	pb "github.com/abchain/fabric/protos"
	"github.com/looplab/fsm"
	"golang.org/x/net/context"
	"time"
	"github.com/spf13/viper"
	"bytes"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/core/util"
	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/flogging"
)

type syncer struct {
	context.Context
	*pb.StreamHandler
	parent *stateSyncHandler
	ledger *ledger.Ledger
	positionResp chan *pb.SyncStateResp
	blocksResp chan *pb.SyncBlocks
	deltaResp  chan *pb.SyncStateDeltas
	correlationId uint64

	maxStateDeltas     int    // The maximum number of state deltas to attempt to retrieve before giving up and performing a full state snapshot retrieval
	maxBlockRange      uint64 // The maximum number blocks to attempt to retrieve at once, to prevent from overflowing the peer's buffer
	maxStateDeltaRange uint64 // The maximum number of state deltas to attempt to retrieve at once, to prevent from overflowing the peer's buffer
	RecoverDamage        bool          // Whether state transfer should ever modify or delete existing blocks if they are determined to be corrupted

	DiscoveryThrottleTime time.Duration // The amount of time to wait after discovering there are no connected peers
	stateValid bool // Are we currently operating under the assumption that the state is valid?
	inProgress bool // Set when state transfer is in progress so that the state may not be consistent
	blockVerifyChunkSize uint64        // The max block length to attempt to sync at once, this prevents state transfer from being delayed while the blockchain is validated

	BlockRequestTimeout         time.Duration // How long to wait for a peer to respond to a block request
	StateDeltaRequestTimeout    time.Duration // How long to wait for a peer to respond to a state delta request
	StateSnapshotRequestTimeout time.Duration // How long to wait for a peer to respond to a state snapshot request
	branchNode2CheckpointMap map[string][][]byte
}

func newSyncer2() (sts *syncer) {
	l, _ := ledger.GetLedger()

	sts = &syncer { positionResp: make(chan *pb.SyncStateResp),
		ledger:        l,
	}

	return sts
}


func newSyncer(ctx context.Context, h *stateSyncHandler, streamHandler *pb.StreamHandler) (sts *syncer) {

	l, _ := ledger.GetLedger()

	sts = &syncer{positionResp: make(chan *pb.SyncStateResp),
		ledger:        l,
		StreamHandler: streamHandler,
		Context:       ctx,
		parent:        h,
	}

	var err error
	sts.blocksResp = make(chan *pb.SyncBlocks)
	sts.deltaResp = make(chan *pb.SyncStateDeltas)

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

func (sts *syncer) InitiateSync(targetState []byte) error {

	defer logger.Infof("[%s]: Exit InitiateSync to <%s>", flogging.GoRDef, sts.parent.remotePeerIdName())
	defer sts.fini()

	logger.Infof("[%s]: Enter InitiateSync to <%s>", flogging.GoRDef, sts.parent.remotePeerIdName())
	//---------------------------------------------------------------------------
	// 1. query
	//---------------------------------------------------------------------------
	mostRecentIdenticalHistoryPosition, endBlockNumber, err := sts.getSyncTargetBlockNumber()
	if err != nil {
		logger.Errorf("[%s]: InitiateSync, getSyncTargetBlockNumber err: %s", flogging.GoRDef, err)
		return err
	}
	logger.Infof("[%s]: InitiateSync, query done. mostRecentIdenticalHistoryPosition:%d",
		flogging.GoRDef, mostRecentIdenticalHistoryPosition)

	startBlockNumber := mostRecentIdenticalHistoryPosition

	//---------------------------------------------------------------------------
	// 2. switch to the right checkpoint
	//---------------------------------------------------------------------------
	//checkpointPosition, err := sts.switchToBestCheckpoint(mostRecentIdenticalHistoryPosition)
	//if err != nil {
	//	logger.Errorf("[%s]: InitiateSync, switchToBestCheckpoint err: %s", flogging.GoRDef, err)
	//
	//	return err
	//}
	//startBlockNumber = checkpointPosition + 1
	//logger.Infof("[%s]: InitiateSync, switch done, startBlockNumber<%d>, endBlockNumber<%d>",
	//	flogging.GoRDef, startBlockNumber, endBlockNumber)


	//---------------------------------------------------------------------------
	// 3. sync blocks
	//---------------------------------------------------------------------------
	// explicitly go to syncblocks state
	sts.parent.fsmHandler.Event(enterGetBlock)
	logger.Infof("[%s]: explicitly go to syncblocks state, startBlockNumber<%d>, endBlockNumber<%d>",
		flogging.GoRDef, startBlockNumber, endBlockNumber)

	err = sts.syncBlocks(startBlockNumber, endBlockNumber)
	if err != nil {
		logger.Errorf("[%s]: InitiateSync, sync blocks err: %s", flogging.GoRDef, err)
		return err
	}
	logger.Infof("[%s]: InitiateSync, sync blocks done", flogging.GoRDef)

	//---------------------------------------------------------------------------
	// 4. sync detals
	//---------------------------------------------------------------------------
	// explicitly go to syncdelta state
	sts.parent.fsmHandler.Event(enterGetDelta)
	_, err = sts.syncDeltas(startBlockNumber, endBlockNumber)

	if err != nil {
		logger.Errorf("[%s]: InitiateSync, syncDeltas err: %s", flogging.GoRDef, err)
		return err
	}
	logger.Infof("[%s]: InitiateSync, sync detals done", flogging.GoRDef)

	//---------------------------------------------------------------------------
	// 5. sync snapshot
	//---------------------------------------------------------------------------

	//---------------------------------------------------------------------------
	// 6. the end
	//---------------------------------------------------------------------------
	return err
}


//---------------------------------------------------------------------------
// 1. receive start confirmed
//---------------------------------------------------------------------------
func (sts *syncer) afterSyncStartResponse(e *fsm.Event) {
	// implicitly go into synclocating state
	payloadMsg := &pb.SyncStateResp{}
	msg := sts.LoadSyncMsg(e, payloadMsg)
	if msg == nil {
		return
	}
	sts.positionResp <- payloadMsg
}

//---------------------------------------------------------------------------
// 2. receive query response
//---------------------------------------------------------------------------
func (sts *syncer) afterQueryResponse(e *fsm.Event) {
	payloadMsg := &pb.SyncStateResp{}
	msg := sts.LoadSyncMsg(e, payloadMsg)
	if msg == nil {
		return
	}
	sts.positionResp <- payloadMsg
}

//---------------------------------------------------------------------------
// 3. receive block response
//---------------------------------------------------------------------------
func (sts *syncer) afterSyncBlocks(e *fsm.Event) {

	payloadMsg := &pb.SyncBlocks{}
	msg := sts.LoadSyncMsg(e, payloadMsg)
	if msg == nil {
		return
	}
	sts.blocksResp <- payloadMsg
}

//---------------------------------------------------------------------------
// 4. receive delta response
//---------------------------------------------------------------------------
func (sts *syncer) afterSyncStateDeltas(e *fsm.Event) {

	payloadMsg := &pb.SyncStateDeltas{}
	msg := sts.LoadSyncMsg(e, payloadMsg)
	if msg == nil {
		return
	}
	sts.deltaResp <- payloadMsg
}

//---------------------------------------------------------------------------
// 5. receive snapshot response
//---------------------------------------------------------------------------
func (sts *syncer) afterSyncStateSnapshot(e *fsm.Event) {

}


func (sts *syncer) getSyncTargetBlockNumber() (uint64, uint64, error) {

	targetBlockNumber := uint64(0)
	endBlockNumber := uint64(0)

	sts.parent.fsmHandler.Event(enterSyncBegin)
	// todo create a fake fsm.Event
	err := sts.SendSyncMsg(nil, pb.SyncMsg_SYNC_SESSION_START, nil)

	if err != nil {
		return 0, 0, err
	}

	select {
	case response, ok := <-sts.positionResp:
		if !ok {
			return 0, 0, fmt.Errorf("had block channel close : %s", err)
		}
		logger.Infof("Remote peer Blockchain Height <%d>", response.BlockHeight)

		endBlockNumber = response.BlockHeight - 1

		// if response.BlockHeight < targetHeight {
		// 	targetHeight = response.BlockHeight
		// }
	//case <-sts.Done():
	//	return 0, 0, fmt.Errorf("Timed out during getSyncTargetBlockNumber")
	}

	var start uint64 = 0
	end := endBlockNumber
	for {

		if targetBlockNumber == (start + end) / 2 {
			break
		}

		targetBlockNumber = (start + end) / 2

		err = sts.SendSyncMsg(nil, pb.SyncMsg_SYNC_SESSION_QUERY,
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
				if targetBlockNumber == 1 {
					return 0, 0, fmt.Errorf("Has no identical state hash")
				}
			}

			logger.Infof("start<%d>, end<%d>, targetBlockNumber<%d>, endBlockNumber<%d>",
				start, end, targetBlockNumber, endBlockNumber)
		//case <-sts.Done():
		//	return 0, 0, fmt.Errorf("Timed out during get SyncTargetBlockNumber")
		}
	}


	logger.Infof("return: start<%d>, end<%d>, targetBlockNumber<%d>, endBlockNumber<%d>",
		start, end, targetBlockNumber, endBlockNumber)
	return targetBlockNumber, endBlockNumber, nil
}


func (sts *syncer) syncDeltas(startBlockNumber, endBlockNumber uint64) (uint64, error) {
	logger.Debugf("Attempting to play state forward from %v to block %d",
		sts.parent.remotePeerIdName(),
		endBlockNumber)
	var stateHash []byte

	intermediateBlock := endBlockNumber
	currentStateBlockNumber := startBlockNumber

	block, err := sts.ledger.GetBlockByNumber(startBlockNumber - 1)
	if err != nil {
		return startBlockNumber, err
	}
	lastStateHash := block.StateHash

	logger.Debugf("Requesting state delta range from %d to %d",
		startBlockNumber, intermediateBlock)

	syncBlockRange := &pb.SyncBlockRange {
		sts.correlationId,
		currentStateBlockNumber,
		intermediateBlock,
	}
	payloadMsg := &pb.SyncStateDeltasRequest{Range: syncBlockRange}
	err = sts.SendSyncMsg(nil, pb.SyncMsg_SYNC_SESSION_GET_DELTAS, payloadMsg)

	if err != nil {
		return startBlockNumber, fmt.Errorf("Received an error while trying to get " +
			"the state deltas for blocks %d through %d from %s",
			currentStateBlockNumber, intermediateBlock, sts.parent.remotePeerIdName())
	}

	for currentStateBlockNumber <= intermediateBlock {
		select {
		case deltaMessage, ok := <-sts.deltaResp:
			if !ok {
				err = fmt.Errorf("Was only able to recover to block number %d when desired to recover to %d",
					currentStateBlockNumber, endBlockNumber)
				break
			}

			if deltaMessage.Range.Start != currentStateBlockNumber ||
				deltaMessage.Range.End < deltaMessage.Range.Start ||
				deltaMessage.Range.End > endBlockNumber {
				err = fmt.Errorf(
					"Received a state delta either in the wrong order (backwards) or " +
						"not next in sequence, aborting, start=%d, end=%d",
					deltaMessage.Range.Start, deltaMessage.Range.End)
				break
			}

			for _, delta := range deltaMessage.Deltas {

				umDelta := &statemgmt.StateDelta{}
				if err := umDelta.Unmarshal(delta); nil != err {
					err = fmt.Errorf("Received a corrupt state delta from %s : %s",
						sts.parent.remotePeerIdName(), err)
					break
				}

				logger.Debugf("Range.Start<%d>, Range.End<%d>. sts.ledger.ApplyStateDelta: sts.currentStateBlockNumber<%d>",
					deltaMessage.Range.Start,
					deltaMessage.Range.End,
					currentStateBlockNumber)

				sts.ledger.ApplyStateDelta(deltaMessage, umDelta)

				success := false

				testBlock, err := sts.ledger.GetBlockByNumber(currentStateBlockNumber)

				if err != nil {
					logger.Warningf("Could not retrieve block %d, though it should be present",
						deltaMessage.Range.End)
				} else {

					stateHash, err = sts.ledger.GetCurrentStateHash()
					if err != nil {
						logger.Warningf("Could not compute state hash for some reason: %s", err)
					}
					logger.Debugf("Played state forward from %s to block %d with StateHash (%x), " +
						"block has StateHash (%x)",
						sts.parent.remotePeerIdName(),
						deltaMessage.Range.End, stateHash,
						testBlock.StateHash)

					if bytes.Equal(testBlock.StateHash, stateHash) {
						success = true
						//add new statehash, and we omit errors
						sts.ledger.AddGlobalState(lastStateHash, stateHash)
						lastStateHash = stateHash
					}
				}

				if !success {
					if sts.ledger.RollbackStateDelta(deltaMessage) != nil {
						sts.stateValid = false
						err = fmt.Errorf(
							"played state forward according to %s, but the state hash did not match, " +
								"failed to roll back, invalidated state",
							sts.parent.remotePeerIdName())
						break
					}
					err = fmt.Errorf("Played state forward according to %s, " +
						"but the state hash did not match, rolled back", sts.parent.remotePeerIdName())
					break

				}

				logger.Debugf("sts.ledger.CommitStateDelta: sts.currentStateBlockNumber<%d>",
					currentStateBlockNumber)

				if sts.ledger.CommitStateDelta(deltaMessage) != nil {
					sts.stateValid = false
					err = fmt.Errorf("Played state forward according to %s, " +
						"hashes matched, but failed to commit, invalidated state", sts.parent.remotePeerIdName())
					break
				}

				logger.Debugf("Moved state to %d, endBlockNumber: %d", currentStateBlockNumber, endBlockNumber)

				if currentStateBlockNumber == endBlockNumber {
					logger.Infof("Caught up to block %d", currentStateBlockNumber)

					logger.Infof("State is now valid at block %d and hash %x", currentStateBlockNumber, stateHash)
					return currentStateBlockNumber, err
				}

				currentStateBlockNumber++
			}
			if err != nil || currentStateBlockNumber == endBlockNumber {
				break
			}
		case <-time.After(sts.StateDeltaRequestTimeout):
			logger.Warningf("Timed out during state delta recovery from %s", sts.parent.remotePeerIdName())
			err = fmt.Errorf("timed out during state delta recovery from %s", sts.parent.remotePeerIdName())
			break
		}
	}

	logger.Infof("State is now valid at block %d and hash %x", currentStateBlockNumber, stateHash)
	return currentStateBlockNumber, err
}


func (sts *syncer) syncBlocks(startBlock, endBlock uint64) error {

	logger.Debugf("Syncing blocks from %d to %d", startBlock, endBlock)
	blockCursor := startBlock
	var block *pb.Block
	var err error

	intermediateBlock := blockCursor + 1

	if intermediateBlock == blockCursor+1 {

		if sts.maxBlockRange > blockCursor {
			// Don't underflow
			intermediateBlock = 0
		} else {
			intermediateBlock = blockCursor - sts.maxBlockRange
		}

		if intermediateBlock < endBlock {
			intermediateBlock = endBlock
		}
		logger.Debugf("sts.correlationId<%d>, Requesting block range from %d to %d",
			sts.correlationId,
			blockCursor, intermediateBlock)

		err = sts.SendSyncMsg(nil, pb.SyncMsg_SYNC_SESSION_GET_BLOCKS,
			&pb.SyncBlockRange{
				sts.correlationId,
				blockCursor,
				intermediateBlock,})
		if err != nil {
			return err
		}
	}

	for {

		if nil != err {
			logger.Warningf("Failed to get blocks from %d to %d from %v: %s",
				blockCursor, endBlock, sts.parent.remotePeerIdName(), err)
			return err
		}

		select {
		case syncBlockMessage, ok := <-sts.blocksResp:

			if !ok {
				return fmt.Errorf("Channel closed before we could finish reading")
			}

			if syncBlockMessage.Range.Start < syncBlockMessage.Range.End {
				// If the message is not replying with blocks backwards, we did not ask for it
				return fmt.Errorf("Received a block with wrong (increasing) order from %v, aborting",
					sts.parent.remotePeerIdName())
			}

			var i int
			for i, block = range syncBlockMessage.Blocks {
				// It no longer correct to get duplication or out of range blocks, so we treat this as an error
				if syncBlockMessage.Range.Start-uint64(i) != blockCursor {
					return fmt.Errorf("Received a block out of order, indicating a buffer " +
						"overflow or other corruption: start=%d, end=%d, wanted %d",
						syncBlockMessage.Range.Start, syncBlockMessage.Range.End, blockCursor)
				}

				logger.Debugf("Putting block %d to with PreviousBlockHash %x and StateHash %x",
					blockCursor, block.PreviousBlockHash, block.StateHash)

				sts.ledger.PutBlock(blockCursor, block)

				if blockCursor == endBlock {
					logger.Infof("Successfully synced from block %d to block %d", startBlock, endBlock)
					return nil
				}
				blockCursor++

			}
		case <-time.After(sts.BlockRequestTimeout):
			return fmt.Errorf("Had block sync request to %s time out", sts.parent.remotePeerIdName())
		}
	}

	if nil != block {
		logger.Debugf("Returned from sync with block %d, state hash %x", blockCursor, block.StateHash)
	} else {
		logger.Debugf("Returned from sync with no new blocks")
	}

	return err
}


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
//func (sts *syncer) enterIdle(e *fsm.Event) {
//
//	stateUpdate := "enterIdle"
//	sts.dumpStateUpdate(stateUpdate)
//}


func (sts *syncer) dumpStateUpdate(stateUpdate string) {
	logger.Debugf("Syncer Syncing state update: %s. correlationId<%d>, remotePeerId<%s>",
		stateUpdate, sts.correlationId, sts.parent.remotePeerIdName())
}

func (sts *syncer) fini() {

	err := sts.SendSyncMsg(nil, pb.SyncMsg_SYNC_SESSION_END, nil)

	if err != nil {
		logger.Errorf("[%s]: SendSyncMsg SyncMsg_SYNC_SESSION_END err: %s", flogging.GoRDef, err)
	}

	sts.parent.fsmHandler.Event(enterSyncFinish)

	select {
	case <-sts.positionResp:
	default:
		logger.Debugf("close positionResp channel")
		close(sts.positionResp)
	}

	select {
	case <-sts.blocksResp:
	default:
		logger.Debugf("close blocksResp channel")
		close(sts.blocksResp)
	}

	select {
	case <-sts.deltaResp:
	default:
		logger.Debugf("close deltaResp channel")
		close(sts.deltaResp)
	}
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
//               [ ]                               [B2]


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

	sts.branchNode2CheckpointMap = traverseGlobalStateGraph(genesis.StateHash, checkpointsMap)

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


func traverseGlobalStateGraph(genesisStateHash []byte, checkpointsMap map[string]bool) map[string][][]byte {

	var stateHashList [][] byte
	branchNode2CheckpointMap := make(map[string][][]byte)

	stateHashList = append(stateHashList, genesisStateHash)

	for i := 0; i < len(stateHashList) ; i++ {

		stateHash := stateHashList[i]
		nextBranchNodeStateHash, checkpointList := traverseTillBranch(stateHash, checkpointsMap)

		if nextBranchNodeStateHash != nil {

			stringStateHash := util.EncodeStatehash(nextBranchNodeStateHash)
			//stringStateHash := string(nextBranchNodeStateHash)
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

func traverseTillBranch(startStateHash []byte, checkpointMap map[string]bool) ([]byte, [][] byte) {
	var checkpointList [][] byte
	curcorStateHash := startStateHash

	for {
		_, ok := checkpointMap[util.EncodeStatehash(curcorStateHash)]
		//_, ok := checkpointMap[string(curcorStateHash)]
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

