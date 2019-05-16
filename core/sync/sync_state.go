package sync

import (
	"fmt"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
	"sync"
)

type stateSyncHandler struct {
	*sessionRT
	iterator statemgmt.PartialRangeIterator
	snapshot *ledger.LedgerSnapshot
}

func newStateSyncHandler(target []byte, root *syncHandler) (*stateSyncHandler, error) {

	sn, err := root.localLedger.CreateSyncingSnapshot(target)
	if err != nil {
		return nil, err
	}

	itr, err := sn.GetParitalRangeIterator()
	if err != nil {
		sn.Release()
		return nil, err
	}

	ret := &stateSyncHandler{
		sessionRT: newSessionRT(root, func() {
			itr.Close()
			sn.Release()
		}),
		iterator: itr,
		snapshot: sn,
	}

	return ret, nil
}

func (h *stateSyncHandler) onSessionRequest(req *pb.TransferRequest) error {

	sreq := req.GetState()
	if sreq == nil {
		return fmt.Errorf("Invalid state-syncing request")
	} else {
		//TODO: for small batch, we simply do transferming in local thread,
		//saving the cost of creating go routines
		// if tasks := sreq.GetOffset(); len(tasks) < h.core.TransportWindow() {
		h.handleSyncing(sreq, nil)
		// } else {
		// go h.handleSyncing(sreq, h.RequestNew())
		//}
	}

	return nil
}

func (h *stateSyncHandler) FillAcceptMsg(accept *pb.AcceptSession) {
	accept.Detail = &pb.AcceptSession_State{
		State: &pb.AcceptSession_StateStatus{
			EstimatedSize: 1,
		},
	}
}

//TODO: stateChunk can be divided into several pieces and client will gather them
func (h *stateSyncHandler) handleSyncing(offset *pb.SyncOffset, waitF func() error) {

	stateChunk, err := statemgmt.GetRequiredParts(h.iterator, offset)
	if err != nil {
		logger.Errorf("get state chunk fail: %s, session closed", err)
		h.core.SessionFailure(err)
		return
	}
	if err = h.core.SessionSend(&pb.TransferResponse{State: stateChunk}); err != nil {
		logger.Errorf("send package fail: %s", err)
		return
	}
	if waitF != nil && waitF() != nil {
		logger.Infof("request interrupted state syncing")
		return
	}

}

type stateSyncer interface {
	GetTarget() []byte
	ApplySyncData(data *pb.SyncStateChunk) error
	AssignTasks() ([]*pb.SyncOffset, error)
}

type stateSyncClient struct {
	syncer stateSyncer

	taskAssigned <-chan *pb.SyncOffset
	taskCounter  sync.WaitGroup
}

//an additional cancel func is provided and should be called before the sync task is
//stopped
func NewStateSyncClient(ctx context.Context, syncer stateSyncer) (*sessionCliAdapter, func()) {

	target := syncer.GetTarget()
	ret := &stateSyncClient{syncer: syncer}

	wctx, cf := context.WithCancel(ctx)
	retAdapter := newSessionClient(wctx, "statesyncer", ret)
	retAdapter.setConnectMessage(&pb.OpenSession{
		For: &pb.OpenSession_Fullstate{Fullstate: target},
	})

	ret.init(wctx)
	clilogger.Infof("create new state sync client for target %X", target)

	return retAdapter, func() {
		cf()
		for {
			_, finished := <-ret.taskAssigned
			//keep polling until channel is closed (so assigned thread quit)
			if !finished {
				clilogger.Infof("task assigned thread for target %X exit", target)
				return
			}
		}
	}
}

func (cli *stateSyncClient) init(ctx context.Context) {

	taskC := make(chan *pb.SyncOffset)
	cli.taskAssigned = taskC

	go func() {

		clilogger.Infof("Task assigned thread for syncing to [%x] start", cli.syncer.GetTarget())
		defer close(taskC)

		for {
			tsks, err := cli.syncer.AssignTasks()
			if err != nil {
				clilogger.Errorf("Task assigned encounter error [%s] and quit!", err)
				return
			} else if len(tsks) == 0 {
				clilogger.Infof("Task assigned thread normal finished")
				return
			}

			clilogger.Debugf("Task assigned thread get %d synctasks and assigned them", len(tsks))

			for _, tsk := range tsks {
				cli.taskCounter.Add(1)
				select {
				case taskC <- tsk:
					select {
					case <-ctx.Done():
						clilogger.Infof("Task assigned thread is stopped: %s", ctx.Err())
						return
					default:
					}
				case <-ctx.Done():
					clilogger.Infof("Task assigned thread is stopped: %s", ctx.Err())
					return
				}
			}

			cli.taskCounter.Wait()
		}

	}()

}

func (cli *stateSyncClient) PreFilter(rledger *pb.LedgerState) bool {
	return rledger.GetStates().Match(cli.syncer.GetTarget())
}
func (cli *stateSyncClient) OnConnected(_ int, hs *pb.AcceptSession) error {
	if ss := hs.GetState(); ss == nil {
		clilogger.Errorf("Handshake do not contain state detail, we use default settings")
	} else {
		clilogger.Infof("Targe state has %d bytes", ss.GetEstimatedSize())
		//TODO: we can use target state's size to decide the syncing delta size, etc
	}
	return nil
}

func (cli *stateSyncClient) Next(int) (*pb.TransferRequest, interface{}) {

	//we may block here, wait until new task is avaliable
	task, ok := <-cli.taskAssigned
	if !ok {
		//no tasks any more, we just exit
		return nil, nil
	}
	clilogger.Debugf("Get assigned sync task (%v)", task)
	req := &pb.TransferRequest_State{State: task}

	return &pb.TransferRequest{Req: req}, nil
}

func (cli *stateSyncClient) OnData(_ interface{}, pack *pb.TransferResponse) (err error) {

	defer func() {

		if err != nil {
			cli.taskCounter.Done()
		}

	}()

	schunk := pack.GetState()
	if schunk == nil {
		err = fmt.Errorf("Wrong package: no state chunk")
		return
	}

	if err = cli.syncer.ApplySyncData(schunk); err != nil {
		return
	}

	err = NormalEnd{}
	return
}

func (cli *stateSyncClient) OnFail(interface{}, error) { cli.taskCounter.Done() }

type stateSyncDetector struct {
	Candidate struct {
		State  []byte
		Height uint64
	}

	ledger       *ledger.Ledger
	targetStates [][]byte
	result       map[int]int
}

func NewStateSyncDetector(ctx context.Context, l *ledger.Ledger, stateRange int) *stateSyncDetector {

	if l.GetBlockchainSize() == 0 {
		clilogger.Errorf("Create sync detector fail: empty blockchain")
		return nil
	}

	var stateCol [][]byte

	for i := l.GetBlockchainSize() - 1; i > 0 && len(stateCol) < stateRange; i-- {
		blk, err := l.GetBlockByNumber(i)
		if err != nil {
			clilogger.Errorf("get block %d fail for state detection: %s", i, err)
		} else if blk == nil {
			clilogger.Errorf("get block %d fail for state detection: no corresponding block", i)
		} else if shash := blk.GetStateHash(); len(shash) == 0 {
			clilogger.Errorf("get state on [%d] fail: wrong field in block", i)
		} else {
			stateCol = append(stateCol, shash)
		}
	}

	clilogger.Infof("Create sync detector with %d candidate states", len(stateCol))
	return &stateSyncDetector{
		ledger:       l,
		targetStates: stateCol,
		result:       make(map[int]int),
	}
}

func (d *stateSyncDetector) detectLedger(rlstat *pb.LedgerState) {
	if rlstat == nil || rlstat.States == nil {
		return
	}

	for k, s := range d.targetStates {
		if rlstat.States.Match(s) {
			clilogger.Debugf("Found state [%X] match target's ledger", s)
			d.result[k] = d.result[k] + 1
			return
		}
	}

}

func (d *stateSyncDetector) DoDetection(sstub *pb.StreamStub) error {

	chs := sstub.OverAllHandlers(context.Background())
	for strm := range chs {
		castedh, ok := strm.StreamHandler.Impl().(*syncHandler)
		if !ok {
			return fmt.Errorf("Wrong implement, implement is not syncHandler")
		} else {
			d.detectLedger(castedh.remoteLedgerState)
		}
	}

	ret := -1
	cnt := 0
	for k, v := range d.result {
		if v > cnt {
			ret = k
			cnt = v
		}
	}

	if ret < 0 {
		return fmt.Errorf("No matching result is found")
	} else {

		retState := d.targetStates[ret]
		retH, err := d.ledger.GetBlockNumberByState(retState)
		if err != nil {
			clilogger.Errorf("Acquire height fail for candidate state [%X]: %s", retState, err)
			return err
		}

		d.Candidate.Height = retH
		d.Candidate.State = retState
		return nil
	}

}
