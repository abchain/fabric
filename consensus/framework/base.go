package framework

import (
	"fmt"
	cspb "github.com/abchain/fabric/consensus/protos"
	"github.com/abchain/fabric/core/chaincode"
	"github.com/abchain/fabric/events/litekfk"
	"github.com/abchain/fabric/node"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
	"time"
)

type ConsensusState []byte
type ConsensusHistory []byte

//wrap the interface of miner
type ConsensusProposal interface {
	//input a group of transaction (and their handling context) to create
	//the real consensus transaction, currently the puprposer CAN NOT rejected part of
	//txs in input. It must accept or rejected them at all
	//DO NOT CALL methods of the input ledgerlearnerinfo outside of propose (for example, run
	//them in an spawned goroutine) or we may got race-condition on the target ledger
	//which binding to the learner
	Propose(cspb.PendingTransactions, LedgerLearnerInfo) <-chan *cspb.ConsensusPurpose
	//interrupt the proposal by an external call
	Cancel()
}

//wrap the interface for delivering generated transactions, often it can be just wrap of the client service module
//when tx is delivered, it will be treated as other consensus tx received from txnetwork and handled by learner
type ConsensusTxDeliver interface {
	Send(context.Context, []*pb.Transaction) error
}

type ProposalTask func(context.Context, LedgerLearnerInfo)

type ConsensusBase struct {
	immediateH chan *pb.TransactionHandlingContext
	proposal   chan ProposalTask

	cstxTopic   litekfk.Topic
	triggerTime time.Duration
}

func NewConsensusBase(topic litekfk.Topic) *ConsensusBase {
	cb := NewConsensusBaseNake(NewConfig(nil))
	cb.cstxTopic = topic
	return cb
}

func NewConsensusBaseNake(cfg FrameworkConfig) *ConsensusBase {
	ret := &ConsensusBase{
		immediateH:  make(chan *pb.TransactionHandlingContext, 1),
		proposal:    make(chan ProposalTask, 1),
		triggerTime: 5 * time.Second,
	}

	conf := cfg.SubConfig("base")

	if v := conf.GetInt("triggertime"); v != 0 {
		ret.triggerTime = time.Duration(v) * time.Millisecond
	}

	logger.Infof("Start a base [%.3f]", ret.triggerTime.Seconds())

	return ret
}

func (cb *ConsensusBase) ProposeEntry() chan<- ProposalTask {
	return cb.proposal
}

func (cb *ConsensusBase) MakeScheme(ne *node.NodeEngine, ccname string) {
	ne.AddTxTopic(ccname)
	cb.cstxTopic = ne.TxTopic[ccname]

	//a common "bypass" mode for handling tx in more efficient way: tx first
	//try to send to handler and if it success, the following handling chain
	//is interrupted
	ne.CustomFilters = append(ne.CustomFilters, pb.FuncAsTxPreHandler(
		func(txe *pb.TransactionHandlingContext) (*pb.TransactionHandlingContext, error) {

			if txe.ChaincodeName != ccname {
				return txe, nil
			}

			select {
			case cb.immediateH <- txe:
				return nil, pb.ValidateInterrupt
			default:
				return txe, nil
			}
		}))
}

//BuildBaseProposalRoutine return this type for trigger a new proposal progress
//the function can be called mutiple times and after each calling user MUST
//wait the returned wait function for another call, or undefined behaviour
//will be raised.
//use can passed a done context to wait function, which will try to stop
//current running proposal progress as soon as possible
type ProposalFunc func() func(context.Context) error

//an default handling for purposing (mining) which can be handled in idle time of
//mainroutine, it was a very base one which do nothing extra (for example: prepare
//for the newstate)
//the reason that mining only run when main routine is idle is node is difficult to
//made reasonable block when it was just busy for catching the latest state
//this method generate
func (cb *ConsensusBase) BuildBaseProposalRoutine(miner ConsensusProposal,
	deliver ConsensusTxDeliver, batchLimit int, sourceCli ...*litekfk.Client) ProposalFunc {

	cpCli := make([]*litekfk.Client, len(sourceCli))
	copy(cpCli, sourceCli)

	//runtime in closure
	var readers []litekfk.Reader
	for i, cli := range cpCli {
		if rd, err := cli.Read(litekfk.ReadPos_Default); err == nil {
			cpCli[len(readers)] = cli
			readers = append(readers, rd)
		} else {
			panic(fmt.Sprintf("read topic [%d] fail when preparing proposal: %s", i, err))
		}
	}

	proposalRes := make(chan error)

	waitF := func(ctx context.Context) (err error) {

		defer func() {
			for i, rd := range readers {
				var rdErr error
				if err == nil {
					rdErr = rd.Commit()
				} else {
					rdErr = rd.Rollback()
				}
				if rdErr != nil {
					logger.Errorf("topic reader encounter fail: %s, retry it", rdErr)
					if rd, rdErr = cpCli[i].Read(litekfk.ReadPos_ResumeOrDefault); rdErr != nil {
						//we can ensure that reader normally should not return error
						panic(fmt.Sprintf("Resume tx reader fail: %s", rdErr))
					} else {
						readers[i] = rd
					}
				}
				rd.Reset()
			}
		}()

		select {
		case err = <-proposalRes:
		case <-ctx.Done():
			miner.Cancel()
			err = <-proposalRes
		}
		return
	}

	coreTask := func(ctx context.Context, learner LedgerLearnerInfo) {

		//collect tx pool, notice empty block is allowed
		l := learner.Ledger()
		workReaders := make([]litekfk.Reader, len(readers))
		copy(workReaders, readers)

		var outputTxe []*pb.TransactionHandlingContext
		for len(workReaders) > 0 && len(outputTxe) < batchLimit {

			var lastq int
			for _, rtx := range workReaders {

				if r := rtx.TransactionReadOne(); r != nil {
					txe := r.(*pb.TransactionHandlingContext)
					if blk, _, err := l.GetBlockNumberByTxid(txe.GetTxid()); err != nil || blk == 0 {
						outputTxe = append(outputTxe, txe)
						if len(outputTxe) >= batchLimit {
							break
						}
					}
					workReaders[lastq] = rtx
					lastq++
				}
			}
			workReaders = workReaders[:lastq]
		}

		startH := time.Now()
		go func(ret <-chan *cspb.ConsensusPurpose) (ferr error) {
			defer func() {
				logger.Infof("-------- Proposal End after %.3f sec: %v",
					time.Since(startH).Seconds(), ferr)
				proposalRes <- ferr
			}()

			logger.Infof("------ Do proposal wtih %d Transactions, prepare in %.3f sec--------",
				len(outputTxe), time.Since(startH).Seconds())
			select {
			case csoutput := <-ret:
				switch r := csoutput.GetOut().(type) {
				case *cspb.ConsensusPurpose_Txs:
					return deliver.Send(ctx, r.Txs.GetTransactions())
				case *cspb.ConsensusPurpose_Nothing:
					logger.Debugf("Miner output nothing")
					return nil
				case *cspb.ConsensusPurpose_Error:
					logger.Errorf("Purposer encounter error: %s", r.Error)
					return fmt.Errorf("%s", r.Error)
				default:
					return fmt.Errorf("Unexpected output: %v", r)
				}
			case <-ctx.Done():
				return ctx.Err()
			}

		}(miner.Propose(outputTxe, learner))

	}

	return func() func(context.Context) error {
		cb.ProposeEntry() <- coreTask
		return waitF
	}
}

//main routine act as an handler for each consensus tx, drive a learner to make ledger and state
//go forward
func (cb *ConsensusBase) MainRoutine(ctx context.Context, learner LedgerLearner) {

	if chaincode.GetSystemChain() == nil {
		panic("system chain platform is not avaliable")
	}

	cli := cb.cstxTopic.NewClient()
	rd, rderr := cli.Read(litekfk.ReadPos_Default)
	pullTxe := func() *pb.TransactionHandlingContext {

		if rderr != nil {
			rd, rderr = cli.Read(litekfk.ReadPos_ResumeOrDefault)
			if rderr != nil {
				logger.Errorf("Encounter error for reading topic attempt: %s", rderr)
				return nil
			}
		}

		r, err := rd.ReadOne()
		if err == nil {
			if txe, ok := r.(*pb.TransactionHandlingContext); !ok {
				panic(fmt.Sprintf("read unexpected value %v", r))
			} else {
				return txe
			}
		} else if err != litekfk.ErrEOF {
			logger.Errorf("read topic fail: %s", err)
			rderr = err
		}
		return nil
	}

	triggerTimer := time.NewTimer(cb.triggerTime)
	plainWait := func() (*pb.TransactionHandlingContext, error) {
		for {
			select {
			case mining := <-cb.proposal:
				mining(ctx, learner)
			case <-triggerTimer.C:
				logger.Debugf("Idle time out")
				triggerTimer.Reset(cb.triggerTime)
				if learner.Trigger(ctx) {
					return nil, nil
				}
			case txe := <-cb.immediateH:
				return txe, learner.Put(ctx, txe)
			case <-ctx.Done():
				return nil, ctx.Err()
			}

		}
	}

	//handle the income (pulled) txe or into idle mode
	deal := func() (*pb.TransactionHandlingContext, error) {
		select {
		case txe := <-cb.immediateH:
			return txe, learner.Put(ctx, txe)
		default:
			if txe := pullTxe(); txe != nil {
				return txe, learner.Put(ctx, txe)
			}
			return plainWait()
		}
	}

	logger.Infof("Consensus main start")

	for ctx.Err() == nil {
		txe, err := deal()
		if err == nil {
			if txe != nil {
				logger.Debugf("put tx [%s] to ledger done", txe.GetTxid())
			} else {
				logger.Debugf("deal do not handle any tx")
			}

		} else if _, ok := err.(ErrorWriteBack); ok {

			if err := cb.cstxTopic.Write(txe); err != nil {
				logger.Errorf("Can not writeback tx [%s]: %s", txe.GetTxid(), err)
			}

			beginTxe := txe
			cycleCounter := 0
			for cycleCounter == 0 || txe != beginTxe {
				txe, err = deal()
				cycleCounter++
				if _, ok = err.(ErrorWriteBack); !ok {
					//when there is not writeback, we can handle error outside
					break
				} else {
					if err := cb.cstxTopic.Write(txe); err != nil {
						logger.Errorf("Can not writeback tx [%s]: %s", txe.GetTxid(), err)
						break
					}
				}
			}
			if txe == beginTxe {
				logger.Debugf("we have entered a writeback cycle with %d txs, do a waiting deliberatily", cycleCounter)
				txe, err = plainWait()
			}
		}

		if txe != nil && err != nil {
			//we have a huge output
			logger.Errorf("------------------ Consensus Encounter An Error ------------------------")
			logger.Errorf("* Transaction is [%s] on network <%s>@<%s>", txe.GetTxid(), txe.NetworkID, txe.PeerID)
			logger.Errorf("* chaincode spec: %v", txe.ChaincodeSpec)
			logger.Errorf("* Error: %s", err)
			logger.Errorf("* Error may block the node updating its state for a long time or lead to")
			logger.Errorf("* permanent failure which require a manually recover")
			logger.Errorf("------------------------------------------------------------------------")
		} else if err != nil {
			logger.Errorf("Encounter handling error: %s", err)
		}

	}

}
