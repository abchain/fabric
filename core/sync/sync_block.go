package sync

import (
	"bytes"
	"fmt"
	"github.com/abchain/fabric/core/ledger"
	pb "github.com/abchain/fabric/protos"
	"sync"
)

type blockSyncHandler struct {
	*sessionRT
	ledgerSN *ledger.LedgerSnapshot
}

func newBlockSyncHandler(requiredH uint64, root *syncHandler) (*blockSyncHandler, error) {

	if requiredH >= root.localLedger.GetBlockchainSize() {
		return nil, fmt.Errorf("required block height [%d] exceeded current", requiredH)
	}

	sn, blkH := root.localLedger.CreateSnapshot()
	//a double check like the singleton syntax...
	if requiredH > blkH {
		sn.Release()
		return nil, fmt.Errorf("required block height [%d] exceeded current", requiredH)
	}

	return &blockSyncHandler{
		sessionRT: newSessionRT(root, sn.Release),
		ledgerSN:  sn,
	}, nil
}

func (h *blockSyncHandler) onSessionRequest(req *pb.TransferRequest) error {

	if reqb := req.GetBlock(); reqb != nil {
		if err := h.testRange(reqb); err == nil {
			go h.handleBlock(reqb, h.RequestNew())
		} else {
			return err
		}

	} else if reqs := req.GetDelta(); reqs != nil {
		if err := h.testRange(reqs); err == nil {
			go h.handleDelta(reqs, h.RequestNew())
		} else {
			return err
		}
	} else {
		return fmt.Errorf("Invalid block/statedelta-syncing request")
	}

	return nil
}

func (h *blockSyncHandler) testRange(rge *pb.SyncBlockRange) error {

	if rge == nil {
		return fmt.Errorf("No range")
	}
	if len(rge.FirstHash) == 0 {
		return nil
	} else if blk, err := h.ledgerSN.GetBlockByNumber(rge.Start); err != nil {
		return err
	} else if blkhash, err := blk.GetHash(); err != nil {
		return err
	} else if bytes.Compare(rge.FirstHash, blkhash) != 0 {
		return fmt.Errorf("Blockhash is not matched")
	}

	return nil
}

func (h *blockSyncHandler) handleBlock(rge *pb.SyncBlockRange, waitF func() error) {

	nextOp := rge.NextNumOp()

	for i, next := rge.Start, true; next; next, i = i != rge.End, nextOp(i) {
		block, err := h.ledgerSN.GetBlockByNumber(i)
		if err != nil {
			logger.Errorf("Error querying block for blockNum %d: %s", i, err)
			h.core.SessionFailure(err)
			return
		}

		//we compress some unecessary size ...
		//clilogger.Debugf("block is %v before prune", block)
		block.Prune()

		pack := &pb.SyncBlock{Height: i, Block: block}
		if err = h.core.SessionSend(&pb.TransferResponse{Block: pack}); err != nil {
			logger.Errorf("send package fail: %s", err)
			return
		}

		if err = waitF(); err != nil {
			logger.Infof("request interrupted block syncing: %s", err)
			return
		}
	}
}

func (h *blockSyncHandler) handleDelta(rge *pb.SyncBlockRange, waitF func() error) {

	nextOp := rge.NextNumOp()

	for i, next := rge.Start, true; next; next, i = i != rge.End, nextOp(i) {
		stateDelta, err := h.ledgerSN.GetStateDelta(i)
		if err != nil {
			logger.Errorf("Error querying stateDelta for blockNum %d: %s", i, err)
			h.core.SessionFailure(err)
			return
		}

		pack := &pb.SyncStateDeltas{Height: i, Deltas: stateDelta.ChaincodeStateDeltas}
		if err = h.core.SessionSend(&pb.TransferResponse{Delta: pack}); err != nil {
			logger.Errorf("send package fail: %s", err)
			return
		}

		if err = waitF(); err != nil {
			logger.Infof("request interrupted statedelta syncing: %s", err)
			return
		}
	}
}

//pull the chain of blocks, from one height larger than current, to the curernt height,
//a series of checkpoints (height:hash) should be known first and the final syncing progress
//will be up to the largest height of these checkpoints.
//with mutiple checkpoints, client can use concurrent tasks to accelerate syncing
type blockSyncClient struct {
	//use a putblock function instead of an interface
	putBlock func(uint64, *pb.Block) error

	sync.Mutex
	startHeight uint64
	tasks       []*pb.SyncBlockRange
}

//build new plans from an a bunch of checkpoints, we turn n checkpoints into corresponding
//array of n tasks adjactent to each other, the last one of the tasks has end at 0
//NOTICE: the input map of checkpoints will be clear after execution
func CheckpointToSyncPlan(checkpoints map[uint64][]byte) (ret []*pb.SyncBlockRange) {
	clilogger.Debugf("generate block sync plan from checkpoints %v", checkpoints)

	for len(checkpoints) > 0 {
		req := &pb.SyncBlockRange{}
		//always search the largest height which is not assigned as start
		//and the secondary one as end
		for k, h := range checkpoints {
			if k > req.Start {
				req.Start = k
				req.FirstHash = h
			} else if k > req.End {
				req.End = k
			}
		}
		delete(checkpoints, req.Start)
		ret = append(ret, req)
	}

	return
}

//pruen a plan by the data of ledgers to avoiding overhead traffic (duplicated
//transfer for existed blocks). this function suppose we may replay
//the same syncing task (ofen generated by the same group of checkpoints) mutiple times
//Notice tasks is updated in palace but unnecessary task is removed
func PruneSyncPlan(l *ledger.Ledger, tasks []*pb.SyncBlockRange) []*pb.SyncBlockRange {
	//this is the solid button of till
	tillHeight := l.TestContinuouslBlockRange() + 1
	finalPos := 0

	deduceNewStart := func(h uint64, ref []byte) (uint64, []byte) {
		retH := l.TestExistedBlockRange(h)
		if retH == h {
			return h, ref
		} else if blk, err := l.GetRawBlockByNumber(retH + 1); err != nil {
			clilogger.Errorf("deduce checkpoint failure, indicate we have block at %d but could not acquire: %s", retH-1, err)
			return h, ref
		} else if blk == nil {
			clilogger.Errorf("deduce checkpoint failure: indicate we have block at %d but could not acquire", retH-1)
			return h, ref
		} else {
			return retH, blk.GetPreviousBlockHash()
		}
	}

	for _, t := range tasks {
		if t.Start < tillHeight {
			continue
		} else if t.End < tillHeight {
			t.End = tillHeight
		}

		t.Start, t.FirstHash = deduceNewStart(t.Start, t.FirstHash)
		if t.Start < t.End {
			continue
		}
		tasks[finalPos] = t
		finalPos++
	}
	return tasks[:finalPos]

}

//simple mode for the syncclient: from top, gen only one task
func BlocSyncSimplePlan(l *ledger.Ledger, targetHeight uint64, targetBlock []byte) []*pb.SyncBlockRange {
	return PruneSyncPlan(l, CheckpointToSyncPlan(map[uint64][]byte{targetHeight: targetBlock}))

}

func NewBlockSyncClient(pf func(uint64, *pb.Block) error, tsk []*pb.SyncBlockRange) *sessionCliAdapter {

	cliCore := &blockSyncClient{
		putBlock: pf,
		tasks:    tsk,
	}

	//scan tasks for highest task (startHeight)
	for _, t := range tsk {
		if t.Start > cliCore.startHeight {
			cliCore.startHeight = t.Start
		}
	}

	clilogger.Infof("Start new blocksync client: %v", cliCore)
	retAdapter := newSessionClient("blocksyncer", cliCore)

	retAdapter.setConnectMessage(&pb.OpenSession{
		For: &pb.OpenSession_BlocksOrDelta{BlocksOrDelta: cliCore.startHeight},
	})

	return retAdapter
}

func (cli *blockSyncClient) PreFilter(rledger *pb.LedgerState) bool {

	return rledger.GetHeight() >= cli.startHeight
}

//no more detail will be expected
func (cli *blockSyncClient) OnConnected(int, *pb.AcceptSession) error { return nil }

type taskCustom struct {
	id  int
	cur *pb.SyncBlockRange
}

func (cli *blockSyncClient) OnData(custom interface{}, pack *pb.TransferResponse) error {
	blk := pack.GetBlock()
	if blk == nil {
		return fmt.Errorf("Wrong package: no block")
	}

	task := custom.(taskCustom)
	blk.GetBlock().Normalize()
	//clilogger.Debugf("block is %v after normalized", blk.GetBlock())
	if blkhash, err := blk.GetBlock().GetHash(); err != nil {
		panic(fmt.Errorf("Wrong block package: %s", err))
	} else if bytes.Compare(blkhash, task.cur.FirstHash) != 0 {
		panic(fmt.Errorf("Unexpected block hash [%X] (expect %X)", blkhash, task.cur.FirstHash))
	} else {
		clilogger.Debugf("Task %d Obtain block [%X]@%d", task.id, blkhash, blk.Height)
	}

	if err := cli.putBlock(blk.GetHeight(), blk.GetBlock()); err != nil {
		return err
	}

	//update task datas
	if blk.GetHeight() <= task.cur.GetEnd() {
		//this task has done (touch another checkpoint or current height)
		clilogger.Debugf("Task %d finish", task.id)
		return NormalEnd{}
	} else {
		task.cur.Start = blk.GetHeight() - 1
		task.cur.FirstHash = blk.GetBlock().GetPreviousBlockHash()
	}

	return nil
}

func (cli *blockSyncClient) OnFail(custom interface{}, err error) {
	task := custom.(taskCustom)
	clilogger.Debugf("Task %d Fail", task.id)

	//we must return back current task
	cli.Lock()
	defer cli.Unlock()
	cli.tasks = append(cli.tasks, task.cur)
}

//we use Next to assign syncing task base on following surpose:
//the checkpoints we can obtained from external source is immutable
//except for the largest one. so for each interval we assigned to
//a task, it is always partialy filled from beginning, and then empty
//until next interval begins
//so we can just check the progress before assign each task and work
//until we hit another interval. for the worst case, this surpose
//do not lead to incorrect syncing but just cause overhead traffic
//(duplicated syncing for one block)
func (cli *blockSyncClient) Next(id int) (*pb.TransferRequest, interface{}) {
	cli.Lock()
	defer cli.Unlock()

	if psz := len(cli.tasks); psz > 0 {
		var req *pb.SyncBlockRange
		psz--
		req, cli.tasks = cli.tasks[psz], cli.tasks[:psz]
		clilogger.Infof("Assign block syncing (head-first mode) task [%v] to task %d", req, id)
		return &pb.TransferRequest{
			Req: &pb.TransferRequest_Block{Block: req},
		}, taskCustom{id, req}
	} else {
		return nil, nil
	}
}

//simply poll the chain of blocks in a specified range, we must be able to verify
//each block we obtained. client can divide it into mutiple concurrent tasks
type blockPollClient struct {
	ledger      *ledger.Ledger
	startHeight uint64
	tillHeight  uint64

	//the function which can verify blocks for each specified height
	blockVerifier func(uint64) []byte
}

func (cli *blockPollClient) PreFilter(rledger *pb.LedgerState) bool         { return false }
func (cli *blockPollClient) OnConnected(int, *pb.AcceptSession) error       { return nil }
func (cli *blockPollClient) Next(int) (*pb.TransferRequest, interface{})    { return nil, nil }
func (cli *blockPollClient) OnData(interface{}, *pb.TransferResponse) error { return NormalEnd{} }
func (cli *blockPollClient) OnFail(interface{}, error)                      {}

// type BlockMessageHandler struct {
// 	client                  *syncer
// 	statehash               []byte
// 	startBlockNumber        uint64
// 	endBlockNumber          uint64
// 	currentStateBlockNumber uint64
// 	delta                   uint64
// }

// func newBlockMessageHandler(startBlockNumber, endBlockNumber uint64, client *syncer) *BlockMessageHandler {
// 	handler := &BlockMessageHandler{}
// 	handler.client = client
// 	handler.delta = 5
// 	handler.startBlockNumber = startBlockNumber
// 	handler.endBlockNumber = endBlockNumber
// 	handler.currentStateBlockNumber = startBlockNumber - 1
// 	return handler
// }

// func (h *BlockMessageHandler) feedPayload(syncMessage *pb.SyncMessage) error {
// 	syncMessage.PayloadType = pb.SyncType_SYNC_BLOCK
// 	return nil
// }

// func (h *BlockMessageHandler) getInitialOffset() (*pb.SyncOffset, error) {

// 	end := h.startBlockNumber + h.delta - 1
// 	end = util.Min(end, h.endBlockNumber)

// 	blockOffset := &pb.BlockOffset{h.startBlockNumber,
// 		end}

// 	logger.Infof("Initial offset: <%v>", blockOffset)

// 	return &pb.SyncOffset{Data: &pb.SyncOffset_Block{Block: blockOffset}}, nil
// }

// func (h *BlockMessageHandler) produceSyncStartRequest() *pb.SyncStartRequest {
// 	return nil
// }

// func (h *BlockMessageHandler) processBlockState(deltaMessage *pb.SyncBlockState) (uint64, error) {

// 	sts := h.client
// 	endBlockNumber := deltaMessage.Range.End
// 	h.currentStateBlockNumber++

// 	if deltaMessage.Range.Start != h.currentStateBlockNumber ||
// 		deltaMessage.Range.End < deltaMessage.Range.Start ||
// 		deltaMessage.Range.End > endBlockNumber {
// 		err := fmt.Errorf(
// 			"Received a state delta either in the wrong order (backwards) or "+
// 				"not next in sequence, aborting, start=%d, end=%d",
// 			deltaMessage.Range.Start, deltaMessage.Range.End)
// 		return h.currentStateBlockNumber, err
// 	}

// 	localBlock, err := sts.ledger.GetBlockByNumber(deltaMessage.Range.Start - 1)
// 	if err != nil {
// 		return deltaMessage.Range.Start, err
// 	}
// 	h.statehash = localBlock.StateHash

// 	logger.Debugf("deltaMessage syncdata len<%d>, block chunk: <%s>", len(deltaMessage.Syncdata),
// 		deltaMessage.Range)

// 	for _, syncData := range deltaMessage.Syncdata {

// 		deltaByte := syncData.StateDelta

// 		block := syncData.Block
// 		umDelta := statemgmt.NewStateDelta()
// 		if err = umDelta.Unmarshal(deltaByte); nil != err {
// 			err = fmt.Errorf("Received a corrupt state delta from %s : %s",
// 				sts.parent.remotePeerIdName(), err)
// 			break
// 		}
// 		logger.Debugf("Current Block Number<%d>, umDelta len<%d>, deltaMessage.Syncdata <%x>",
// 			h.currentStateBlockNumber, len(umDelta.ChaincodeStateDeltas), deltaByte)

// 		sts.ledger.ApplyStateDelta(deltaMessage, umDelta)

// 		if block != nil {
// 			h.statehash, err = sts.sanityCheckBlock(block, h.statehash, h.currentStateBlockNumber, deltaMessage)
// 			if err != nil {
// 				break
// 			}
// 		}

// 		if err = sts.ledger.CommitAndIndexStateDelta(deltaMessage, h.currentStateBlockNumber); err != nil {
// 			sts.stateValid = false
// 			err = fmt.Errorf("Played state forward according to %s, "+
// 				"hashes matched, but failed to commit, invalidated state", sts.parent.remotePeerIdName())
// 			break
// 		}

// 		//we can still forward even if we can't persist the block
// 		if errPutBlock := sts.ledger.PutBlock(h.currentStateBlockNumber, block); errPutBlock != nil {
// 			logger.Warningf("err <Put block fail: %s>", errPutBlock)
// 		}

// 		logger.Infof("Successfully moved state to height %d",
// 			h.currentStateBlockNumber+1)

// 		if h.currentStateBlockNumber == endBlockNumber {
// 			break
// 		}

// 		h.currentStateBlockNumber++
// 	}

// 	return h.currentStateBlockNumber, err
// }

// func (h *BlockMessageHandler) processResponse(syncMessage *pb.SyncMessage) (*pb.SyncOffset, error) {

// 	syncBlockStateResp := &pb.SyncBlockState{}
// 	err := proto.Unmarshal(syncMessage.Payload, syncBlockStateResp)
// 	if err != nil {
// 		return nil, err
// 	}

// 	if len(syncMessage.FailedReason) > 0 {
// 		err = fmt.Errorf("Sync state failed! Reason: %s", syncMessage.FailedReason)
// 		return nil, err
// 	}

// 	_, err = h.processBlockState(syncBlockStateResp)
// 	if err != nil {
// 		return nil, err
// 	}

// 	var nextOffset *pb.SyncOffset
// 	if h.endBlockNumber > syncBlockStateResp.Range.End {
// 		start := syncBlockStateResp.Range.Start + h.delta
// 		end := syncBlockStateResp.Range.End + h.delta

// 		end = util.Min(end, h.endBlockNumber)

// 		nextOffset = pb.NewBlockOffset(start, end)
// 	}

// 	if nextOffset == nil {
// 		logger.Infof("Caught up to block %d, and state is now valid at hash <%x>", h.currentStateBlockNumber, h.statehash)
// 	}

// 	return nextOffset, err
// }
