package framework

import (
	"bytes"
	"fmt"
	cspb "github.com/abchain/fabric/consensus/protos"
	"github.com/abchain/fabric/core/chaincode"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/sync/strategy"
	"github.com/abchain/fabric/node"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
	"time"
)

//writeback error indicate CURRENT handling txe do not being handled and
//should be put later (generally, after a Trigger call with true return)
type ErrorWriteBack struct{}

func (ErrorWriteBack) Error() string { return "writeback" }

type LedgerLearnerInfo interface {

	//help to built a preview block, which contain the essential data
	//(previousblock, transactions, statehashes, nonhashdata, etc)
	//learner can also cache build candidate so it is not need to do evaluation
	//again
	Preview(context.Context, uint64, []*pb.TransactionHandlingContext) (*pb.Block, error)
	//previewsimple only build the mininal fields of the block from input txes,
	//it do not evaluate any transaction nor prepare for statehash
	PreviewSimple([]*pb.TransactionHandlingContext) *pb.Block

	Ledger() *ledger.Ledger

	//just a fast entry for many miner, incidate the newest history's hash it has
	//learn so miner can mine new block on top of it
	//if there is no avaliable history, just return nil
	HistoryTop() (uint64, []byte)
}

//a ledger learner accept incoming consensus tx and update A ledger
type LedgerLearner interface {
	LedgerLearnerInfo

	//put is allowed to block for CERTAIN (limited) time
	//it may return three results for a tx:
	//* learner consume the incoming tx and success (return nil)
	//* learner consume incoming tx but fail (return error)
	//* writeback, learner return one (or more) tx and calle should postpone
	//  their delivery
	Put(context.Context, *pb.TransactionHandlingContext) error

	//put direct allow external calling directly input some result,
	//to drive the ledger/status wrapped inside learner go forward
	//forexample, ConsensusBase allow deliver any custom process
	//which is able to access the learner directly. Such a process can
	//make use of this entry
	PutDirect(context.Context, *cspb.ConsensusOutput) error

	//trigger make learner to resolve its pending task, even no new
	//consensus tx is input
	//it is also help caller to decide if writeback tx should be delivered
	//again: a trigger return false indicate there is still pending
	//tasks and no progress is made
	Trigger(context.Context) bool
}

type baseLearnerImpl struct {
	ledger *ledger.Ledger
	sync   *syncstrategy.SyncEntry

	txSyncDist        int
	blockSyncDist     int
	stateSyncDist     int
	fullSyncCriterion uint64
	txPrehandle       pb.TxPreHandler
	chainforTx        chaincode.ChainName
	commitCC          map[string]bool

	//we trace, both the highest block state of current chain (top)
	//and the lowest state we have known, but could not catch up
	//with yet. Because to sync a lower state with neighbours is
	//more possible than a higher one (many nodes may not climb up
	//to the top yet)
	refhistory struct {
		pos  uint64
		hash []byte

		syncRefpos  uint64
		syncRefhash []byte
	}

	cache struct {
		pendingTxs   []string
		pendingBlock map[uint64][]*pb.Block
	}

	lastBuilt struct {
		state  []byte
		output *ledger.TxEvaluateAndCommit
	}
}

func NewBaseLedgerLearner(l *ledger.Ledger, peer *node.PeerEngine, cfg FrameworkConfig) *baseLearnerImpl {

	ret := &baseLearnerImpl{
		sync:              peer.Syncer(),
		ledger:            l,
		txPrehandle:       pb.DefaultTxHandler,
		chainforTx:        chaincode.DefaultChain,
		txSyncDist:        5,
		blockSyncDist:     200,
		stateSyncDist:     2000000000, //2T blocks, which is large enough
		fullSyncCriterion: 2,          //only do fullsyncing when we are at the very beginning
	}
	ret.cache.pendingBlock = make(map[uint64][]*pb.Block)

	linfo, _ := l.GetLedgerInfo()
	if linfo == nil || linfo.GetHeight() == 0 {
		panic("Learner can not work on an uninited ledger (without a genesis block)")
	}
	ret.refhistory.pos = linfo.GetHeight() - 1
	ret.refhistory.hash = linfo.GetCurrentBlockHash()

	conf := cfg.SubConfig("learner")

	if v := conf.GetInt("txsyncdistance"); v != 0 {
		ret.txSyncDist = v
	}

	if v := conf.GetInt("blocksyncdistance"); v != 0 {
		ret.blockSyncDist = v
	}

	if v := conf.GetInt("statesyncdistance"); v != 0 {
		ret.stateSyncDist = v
	}

	//notice it can be zero
	if conf.IsSet("dofullsyncwhen") {
		ret.fullSyncCriterion = uint64(conf.GetInt64("dofullsyncwhen"))
	}

	if !linfo.States.Avaliable {
		if conf.GetBool("syncingresuming.disabled") {
			logger.Errorf("we have pending full-state syncing but not resume it")
			err := l.DeleteALLStateKeysAndValues()
			if err != nil {
				logger.Fatalf("clean state failure: %s", err)
			}
		}
	}

	logger.Infof("Start a learner [%d, %d, %d, %d]", ret.txSyncDist, ret.blockSyncDist, ret.stateSyncDist, ret.fullSyncCriterion)

	return ret
}

func (l *baseLearnerImpl) Ledger() *ledger.Ledger {
	return l.ledger
}

func (l *baseLearnerImpl) HistoryTop() (uint64, []byte) {
	return l.refhistory.pos, l.refhistory.hash
}

func (l *baseLearnerImpl) updateRefHistory(newBlkN uint64, newBlkH []byte) {

	if l.refhistory.syncRefpos == 0 || l.refhistory.syncRefpos > newBlkN {
		l.refhistory.syncRefpos = newBlkN
		l.refhistory.syncRefhash = newBlkH
	}

	if l.refhistory.pos < newBlkN {
		l.refhistory.pos = newBlkN
		l.refhistory.hash = newBlkH
	}

}

func (l *baseLearnerImpl) resolvePendingBlock(newBlkN uint64, refhash []byte) uint64 {

	blkAgent := ledger.NewBlockAgent(l.ledger)
	for newblks, ok := l.cache.pendingBlock[newBlkN]; ok; newblks, ok = l.cache.pendingBlock[newBlkN] {

		delete(l.cache.pendingBlock, newBlkN)
		for _, newblk := range newblks {
			//find block match to current branch
			if bytes.Compare(newblk.GetPreviousBlockHash(), refhash) == 0 {
				if err := blkAgent.SyncCommitBlock(newBlkN, newblk); err != nil {
					logger.Errorf("commit pending block %d fail: %s, give up", newBlkN, err)
					return newBlkN
				} else {
					logger.Debugf("commit new block %d", newBlkN)
					newBlkN, refhash = newBlkN+1, blkAgent.LastCommit()
				}
				break
			}
		}
	}

	return newBlkN

}

//any block should has been verified and just postpone the input block which may lead to a gap
//block will be postpone by return any error
//notice we do not handle state switch in baseLearner
func (l *baseLearnerImpl) putblock(linfo *ledger.LedgerInfo, newBlkN uint64, newblk *pb.Block) error {

	//legacy block (v0) can not be accept and method will panic
	if newblk.GetVersion() < 1 {
		panic("get legacy block, which is not acceptable, check your code")
	} else if newBlkN == 0 {
		panic("wrong height (0), system should not broadcast genesis block")
	}

	if linfo.Persisted.Blocks > newBlkN {
		//we have a more advanced data then current (may because syncing)
		//just silently swallow this block
		//(though we can also detect state branching here but we still have enough chance)
		logger.Debugf("new block %d is lower than current persisted (%d)",
			newBlkN, linfo.Persisted.Blocks)
		return nil
	} else if newBlkN < linfo.GetHeight() {
		blkAgent := ledger.NewBlockAgent(l.ledger)
		//we do not check blocks which fill gaps because
		//they are rare (if we just keep using learner for a ledger)...
		logger.Debugf("commit gapped block %d", newBlkN)
		return blkAgent.SyncCommitBlock(newBlkN, newblk)
	}

	logger.Debugf("put pending block %d", newBlkN)
	l.updateRefHistory(newBlkN-1, newblk.GetPreviousBlockHash())
	l.cache.pendingBlock[newBlkN] = append(l.cache.pendingBlock[newBlkN], newblk)
	return nil
}

var errNothing = fmt.Errorf("Do Nothing")

//block forward can be always triggered, ignoring pending tasks
func (l *baseLearnerImpl) blockForward(ctx context.Context, linfo *ledger.LedgerInfo) (err error) {
	//TODO: if we just do a state-switch, we should first query for a most advanced
	//checkpoint, and then start block/state syncing

	watermark := l.refhistory.pos + 1
	if linfo.GetHeight() > watermark {
		watermark = linfo.GetHeight()
	}

	//first we may trigger a syncing
	fallbehind := watermark - linfo.Persisted.Blocks
	if fallbehind < uint64(l.blockSyncDist) {
		return errNothing
	}

	startH := time.Now()

	defer func() {

		syncTime := time.Now().Sub(startH)
		if err == nil {
			logger.Infof("----- Sync done in %.1f seconds -----", syncTime.Seconds())
			//also clean pending tasks ...
			l.cache.pendingTxs = nil

		} else {
			logger.Infof("----- Sync finished in %.1f seconds but (partial) failure: %s",
				syncTime.Seconds(), err)
			//if persisted block has progress, we clear the error flag
			if l.ledger.TestContinuouslBlockRange() > linfo.Persisted.Blocks {
				err = nil
			}
		}

	}()

	//notice, we start with the refpos, not top pos (but we calc watermark by top pos)
	syncer := l.sync.FromTopStrategy(l.ledger, l.refhistory.syncRefpos, l.refhistory.syncRefhash)
	logger.Infof("----- Fall behind from newest (%d) for %d blocks, start syncing -----",
		l.refhistory.pos, fallbehind)
	if err := syncer.Block(ctx); err != nil {
		return err
	}

	if fallbehind > uint64(l.stateSyncDist) || linfo.Avaliable.States < l.fullSyncCriterion {
		logger.Infof("State fall behind exceed tolerance (%d) or just at the beginning (%d), do full syncing:",
			l.stateSyncDist, linfo.Avaliable.States)
		return syncer.State(ctx)
	}

	return nil

}

func (l *baseLearnerImpl) stateForward(ctx context.Context, linfo *ledger.LedgerInfo) (ferr error) {

	//TODO: resolve pending syncing first
	if !linfo.States.Avaliable {
		return errNothing
	}

	//then resolve pending tx
	if pl := len(l.cache.pendingTxs); pl > 0 {
		//do tx sync to resolve the missing tx
		logger.Infof("start sync %d pending tx", pl)
		syncedTx, restTx := l.sync.SyncTransactions(ctx, l.cache.pendingTxs)
		if err := l.ledger.PutTransactions(syncedTx); err != nil {
			return fmt.Errorf("Can't not persisted tx: %s", err)
		}

		logger.Debugf("has sync %d txs in %d pending", len(syncedTx), pl)
		l.cache.pendingTxs = restTx
		if len(l.cache.pendingTxs) > 0 {
			return errNothing
		}
	}

	//TODO: consider start a state only syncing if current state is still far behind (even we have many blocks)
	if linfo.Persisted.Blocks < linfo.Avaliable.States {
		//wired state ... we just do nothing
		logger.Warningf("state position [%d] is higher than working blocks [%d]", linfo.Avaliable.States, linfo.Persisted.Blocks)
		return errNothing
	} else if stateFallbehind := linfo.Persisted.Blocks - linfo.Avaliable.States; stateFallbehind > 0 {

		startH := linfo.Avaliable.States
		lastStateHash := linfo.States.AvaliableHash
		defer func() {

			if ferr != nil && startH > linfo.Avaliable.States {
				logger.Errorf("Forstate fail (%s) with some progress (to %d)", ferr, startH)
				ferr = nil
			}
		}()

		//start forward each state, current we do it one-by-one!
		for ; startH < linfo.Persisted.Blocks; startH++ {

			//collect txs
			refblk, err := l.ledger.GetRawBlockByNumber(startH)
			if err != nil {
				return fmt.Errorf("state forward fail on get reference block: %s", err)
			}

			//execute in a closure ...
			if err := func() (ferr error) {

				defer func(parentHash []byte) {
					if ferr == nil {
						if err := l.ledger.AddGlobalState(parentHash, lastStateHash); err != nil {
							logger.Warningf("can not add global state [%12X] on [%12X]:%s",
								lastStateHash, parentHash, err)

						}
					}

				}(lastStateHash)

				//try cache, currently we can only build a state just on top of current chain
				//so cache will be always cleared after that
				if cachedOut := l.lastBuilt.output; cachedOut != nil {
					l.lastBuilt.output = nil
					if bytes.Compare(l.lastBuilt.state, refblk.StateHash) == 0 {
						logger.Debugf("Apply last built block [%.16X]", l.lastBuilt.state)

						//also remember commit the txs
						if err := l.ledger.CommitTransactions(refblk.GetTxids(), startH); err != nil {
							logger.Warningf("Can not commit tx in block %d: %s", startH, err)
						}

						if err := cachedOut.StateCommitOne(startH, refblk); err != nil {
							logger.Errorf("state forward fail on use cached built data: %s", err)
						} else {
							lastStateHash = refblk.StateHash
							return nil
						}
					}
				}

				outTxs, pending := l.ledger.GetTxForExecution(refblk.GetTxids(), l.txPrehandle)
				if ptxl := len(pending); ptxl > 0 {
					if stateFallbehind > uint64(l.txSyncDist) {
						//put pending txid for syncing, or we just wait
						l.cache.pendingTxs = pending
						logger.Debugf("Put %d tx for syncing", ptxl)
					}
					logger.Debugf("Still %d tx is pending, try later", ptxl)
					return fmt.Errorf("finish forwarding state for transaction is not avaliable yet")
				}

				startT := time.Now()

				txagent, err := ledger.NewTxEvaluatingAgent(l.ledger)
				if err != nil {
					return err
				}
				//if there is no timestamp in block (some legacy code, use UNIX 0 instead)
				blockTs := time.Unix(0, 0)
				if blkts := refblk.GetTimestamp(); blkts != nil {
					blockTs = pb.GetUnixTime(blkts)
				}
				//done, we evaluate the txs
				if _, err := chaincode.ExecuteTransactions2(ctx, l.chainforTx,
					outTxs, blockTs, txagent); err != nil {
					logger.Errorf("Execute transactions on block %d encounter err, which is fatal: %s",
						startH, err)
					return err
				}

				//commit tx at the last, so in the previous GetTxForExecution call, we can make
				// use of the tx pool
				if err := l.ledger.CommitTransactions(refblk.GetTxids(), startH); err != nil {
					logger.Warningf("Can not commit tx in block %d: %s", startH, err)
				}

				if err := txagent.StateCommitOne(startH, refblk); err != nil {
					logger.Errorf("commit state to block %d encounter err %s", startH, err)
					return err
				}

				lastStateHash = txagent.LastCommitState()

				endT := time.Now().Sub(startT)
				//TODO: more detail information (like geth?)
				logger.Infof("Commit block %d: evaluate %d transactions in %.3f sec", startH,
					len(outTxs), endT.Seconds())

				return nil
			}(); err != nil {
				return err
			}
		}

		return nil
	}

	return errNothing
}

func (l *baseLearnerImpl) PreviewSimple(txes []*pb.TransactionHandlingContext) *pb.Block {

	blkTxs := make([]*pb.Transaction, 0, len(txes))
	for _, txe := range txes {
		blkTxs = append(blkTxs, txe.Transaction)
	}
	return ledger.BuildPreviewBlock([]byte("preview"), blkTxs)
}

func (l *baseLearnerImpl) Preview(ctx context.Context, pos uint64, txes []*pb.TransactionHandlingContext) (*pb.Block, error) {

	txagent, err := ledger.NewTxEvaluatingAgent(l.ledger)
	if err != nil {
		return nil, fmt.Errorf("bulid block fail on create agent: %s", err)
	}

	previewBlk := l.PreviewSimple(txes)

	//done, we evaluate the txs
	_, err = chaincode.ExecuteTransactions2(ctx, l.chainforTx,
		txes, pb.GetUnixTime(previewBlk.GetTimestamp()), txagent)
	if err != nil {
		logger.Errorf("Execute transactions on block %d encounter err, which is fatal: %s",
			pos, err)
		return nil, err
	}

	previewBlk, err = txagent.PreviewBlock(pos, previewBlk)
	if err != nil {
		return nil, err
	}

	l.lastBuilt.state, l.lastBuilt.output = previewBlk.GetStateHash(), txagent

	return previewBlk, err
}

func (l *baseLearnerImpl) Trigger(ctx context.Context) bool {

	linfo, err := l.ledger.GetLedgerInfo()
	if err != nil {
		logger.Errorf("can not get ledger info: %s, trigger is given up", err)
		return false
	}

	logger.Debugf("Start trigger, known top (%d:%d), our top %d, persisted top %d",
		l.refhistory.pos+1, l.refhistory.syncRefpos+1, linfo.GetHeight(), linfo.Avaliable.States)

	//TODO: resolve pending syncing first
	if !linfo.States.Avaliable {
		panic("TODO: resuming full-state syncing not implied")
	}

	if err := l.blockForward(ctx, linfo); err == nil {
		//block syncing done
		forwardTo := l.ledger.TestContinuouslBlockRange()
		//cleaning pending Block
		for h, _ := range l.cache.pendingBlock {
			if h < forwardTo {
				delete(l.cache.pendingBlock, h)
			}
		}
		l.resolvePendingBlock(l.refhistory.syncRefpos+1, l.refhistory.syncRefhash)
		//merge refpos
		if forwardTo >= l.refhistory.syncRefpos {
			l.refhistory.syncRefhash = l.refhistory.hash
			l.refhistory.syncRefpos = l.refhistory.pos
		}
	} else if err != errNothing {
		logger.Errorf("can not forward block for error: %s, give up", err)
		//STOP for an error
		return false
	} else if err = l.stateForward(ctx, linfo); err != nil {
		logger.Debugf("Trigger do not forward any state: %s", err)
		return false
	}

	linfo, err = l.ledger.GetLedgerInfo()
	if err != nil {
		logger.Warning("can not re-acquire ledger info: %s, state forward is given up", err)
		return true
	}

	if err := l.stateForward(ctx, linfo); err != nil {
		logger.Debugf("Trigger do not forward any state: %s", err)
	}

	return true
}

func (l *baseLearnerImpl) handleOutput(ctx context.Context, linfo *ledger.LedgerInfo, ro *cspb.ConsensusOutput) error {

	defer func() {
		if l.ledger.TestContinuouslBlockRange() >= linfo.Persisted.Blocks {
			if ulinfo, err := l.ledger.GetLedgerInfo(); err != nil {
				logger.Errorf("can not update ledger info: %s", err)
			} else {
				linfo = ulinfo
			}
		}

		if err := l.stateForward(ctx, linfo); err != nil && err != errNothing {
			logger.Errorf("state forward fail on put consensus transaction step: %s", err)
		}
	}()

	switch r := ro.GetOut().(type) {
	case *cspb.ConsensusOutput_More:
		panic("not implied")
	case *cspb.ConsensusOutput_Block:
		if err := l.putblock(linfo, ro.GetPosition(), r.Block); err != nil {
			return fmt.Errorf("block %d@[%8X] not commit: %s",
				ro.GetPosition(), r.Block.GetStateHash(), err)
		}
		if newBlk := ro.GetPosition(); newBlk == linfo.GetHeight() {
			l.resolvePendingBlock(newBlk, linfo.GetCurrentBlockHash())
		}
		return nil
	case *cspb.ConsensusOutput_Blocks:
		for _, pblk := range r.Blocks.GetBlks() {
			if err := l.putblock(linfo, pblk.GetN(), pblk.GetB()); err != nil {
				return fmt.Errorf("block %d@[%8X] not commit: %s",
					pblk.GetN(), pblk.GetB().GetStateHash(), err)
			} else if newBlk := pblk.GetN(); newBlk == linfo.GetHeight() {
				defer l.resolvePendingBlock(newBlk, linfo.GetCurrentBlockHash())
			}
		}
		return nil
	case *cspb.ConsensusOutput_Blockhash:
		if linfo.GetHeight() <= ro.GetPosition() {
			l.updateRefHistory(ro.GetPosition(), r.Blockhash)
		}
		return nil
	case *cspb.ConsensusOutput_Error:
		return fmt.Errorf("handle input consensus fail: %s", r.Error)
	case *cspb.ConsensusOutput_Nothing:
		return nil
	default:
		panic("Unexpected type")
	}

}

func (l *baseLearnerImpl) PutDirect(ctx context.Context, ro *cspb.ConsensusOutput) error {

	linfo, err := l.ledger.GetLedgerInfo()
	if err != nil {
		return err
	}

	return l.handleOutput(ctx, linfo, ro)
}

func (l *baseLearnerImpl) Put(ctx context.Context, cstxe *pb.TransactionHandlingContext) (ferr error) {

	linfo, err := l.ledger.GetLedgerInfo()
	if err != nil {
		logger.Errorf("can not get ledger info: %s", err)
		return ErrorWriteBack{}
	}

	var txForPosition uint64

	defer func() {

		if doCommit := l.commitCC[cstxe.ChaincodeName]; ferr == nil && doCommit {
			l.ledger.CommitTransactions([]string{cstxe.GetTxid()}, txForPosition)
		} else {
			l.ledger.PruneTransactions([]*pb.Transaction{cstxe.Transaction})
		}
	}()

	//evaluate the input tx on SYSTEM chaincode platform
	//consensus tx is a query one, but it in fact keep state inside itself
	consensusRet, err := chaincode.Execute2(ctx, l.ledger, chaincode.GetSystemChain(), cstxe, ledger.NewQueryExecState(l.ledger))
	if err != nil {
		return err
	}

	retout := cspb.ConsensusRet(consensusRet.Resp)
	if ro, err := retout.Out(); err != nil {
		panic(fmt.Sprintf("chaincode do not set valid output， fail for (%s)", err))
	} else {

		txForPosition = ro.GetPosition()

		return l.handleOutput(ctx, linfo, ro)
	}
}

//also provide an interface of deliver, such a deliver can send tx into learner directly
func (l *baseLearnerImpl) Send(ctx context.Context, txs []*pb.Transaction) error {
	var txes []*pb.TransactionHandlingContext
	for _, tx := range txs {
		txe, err := l.txPrehandle.Handle(pb.NewTransactionHandlingContext(tx))
		if err != nil {
			return err
		}
		txes = append(txes, txe)
	}

	for i, txe := range txes {
		err := l.Put(ctx, txe)
		if err != nil {
			return &deliverProgress{
				lastFailure: err,
				resumedTask: &pendingDelivery{
					txs:     txs[i:],
					deliver: l,
				},
			}
		}
	}
	return nil
}
