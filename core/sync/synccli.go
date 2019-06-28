package sync

import (
	"fmt"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
	"sync"
	"time"
)

type ClientFactory interface {
	Tag() string
	//opts just called at the beginning of ExecuteSyncTask so returned
	//object can be created just after being called
	Opts() *clientOpts
	PreFilter(rledger *pb.LedgerState) bool
	//notice this should provide a handling function and factory is
	//responsed to schedule tasks between its mutiple handling
	//functions
	//handling function MUST return nil to incidate current task
	//is finished and non-nil to indicate scheduler should retry
	//another peer
	AssignHandling() func(context.Context, *pb.StreamHandler, *syncCore) error
}

type syncTaskScheduler struct {
	sync.Mutex
	activedTasks     int
	conCurrentLimit  int
	excludeFatalOnly bool
	taskEndNotify    *sync.Cond
	sstub            *pb.StreamStub

	//the peer which has been handled and finished, so we never
	//retry on them when retry another traversing
	excluedePeers map[pb.PeerID]bool
}

type NormalEnd struct{}

func (NormalEnd) Error() string { return "Normal finish" }

type FatalEnd struct {
	error
}

var retryIntervalLimit = time.Second * 10

//this help us to access the stub object from streamstub
var AccessStubHelper func(*pb.StreamStub, func(*SyncStub)) = func(*pb.StreamStub, func(*SyncStub)) {}

func ExecuteSyncTask(ctx context.Context, cf ClientFactory, sstub *pb.StreamStub) error {

	opts := cf.Opts()

	rt := &syncTaskScheduler{
		conCurrentLimit:  opts.ConcurrentLimit,
		excluedePeers:    make(map[pb.PeerID]bool),
		excludeFatalOnly: opts.RetryFail,
	}

	//checking for correction
	if rt.conCurrentLimit == 0 {
		clilogger.Warningf("Concurrent limit is not set, suppose to be 1")
		rt.conCurrentLimit = 1
	}

	rt.taskEndNotify = sync.NewCond(rt)
	retryTime := time.Duration(opts.RetryInterval) * time.Second
	var retryCnt int

	AccessStubHelper(sstub, func(s *SyncStub) { s.depressStatusNotify = true })
	defer AccessStubHelper(sstub, func(s *SyncStub) { s.depressStatusNotify = false })

	for {
		clilogger.Infof("start sync task [%s] (%d times)", cf.Tag(), retryCnt)

		rt.innerSpawn(ctx, sstub, cf)
		if err := rt.waitAll(ctx); err != nil {
			return err
		}

		if rt.conCurrentLimit == 0 {
			//all thread has return normally
			//when we done, broadcast new ledger status to neighbours
			AccessStubHelper(sstub, func(s *SyncStub) { s.BroadcastLedgerStatus(sstub) })
			return nil
		}

		if opts.RetryCount >= 0 && retryCnt >= opts.RetryCount {
			return fmt.Errorf("Task [%s] fail: retry count [%d] exceed", cf.Tag(), retryCnt)
		} else {
			retryCnt++
		}

		if retryTime < retryIntervalLimit && retryCnt >= 3 {
			retryTime = retryIntervalLimit
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.NewTimer(retryTime).C:
			//do retry
		}
	}

}

func (s *syncTaskScheduler) activeTaskExit(peer *pb.PeerID, err error) {

	s.Lock()
	defer s.Unlock()

	//sanity check
	if s.conCurrentLimit == 0 || s.activedTasks == 0 {
		panic("Wrong code: error counting")
	}

	s.activedTasks--
	if _, ok := err.(NormalEnd); ok {
		err = nil
	}

	if err == nil {
		clilogger.Debugf("A task on peer [%s] has normally exited", peer.GetName())
		s.conCurrentLimit--
		delete(s.excluedePeers, *peer)
	} else if _, ok := err.(FatalEnd); ok {
		//exclude this peer
		clilogger.Infof("task on peer [%s] is FATAL fail: %s, exclude this peer", peer.GetName(), err)
		s.excluedePeers[*peer] = false
	} else if !s.excludeFatalOnly {
		clilogger.Infof("task on peer [%s] is fail: %s, exclude this peer", peer.GetName(), err)
		s.excluedePeers[*peer] = false
	}

	s.taskEndNotify.Signal()
}

func (s *syncTaskScheduler) waitAll(ctx context.Context) error {

	s.Lock()
	defer s.Unlock()
	for s.activedTasks > 0 {
		s.taskEndNotify.Wait()
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	return nil

}

func (s *syncTaskScheduler) waitForNext(ctx context.Context) error {

	s.Lock()
	defer s.Unlock()
	for s.conCurrentLimit > 0 && s.activedTasks >= s.conCurrentLimit {
		s.taskEndNotify.Wait()
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	if s.conCurrentLimit == 0 {
		return NormalEnd{}
	}

	return nil
}

func (s *syncTaskScheduler) preFilter(peer *pb.PeerID) bool {
	s.Lock()
	defer s.Unlock()

	_, existed := s.excluedePeers[*peer]
	if existed {
		return false
	}
	return true
}

func (s *syncTaskScheduler) innerSpawn(ctx context.Context, sstub *pb.StreamStub, cf ClientFactory) {

	wctx, endF := context.WithCancel(ctx)
	defer endF()

	chs := sstub.OverAllHandlers(wctx)
	for strm := range chs {
		castedh, ok := strm.Impl().(*syncHandler)
		if !ok {
			panic("Wrong implement, implement is not syncHandler")
		}

		if !castedh.core.IsIdle() || !s.preFilter(strm.Id) || !cf.PreFilter(castedh.GetRemoteLedgerState()) {
			clilogger.Debugf("prefiltering peer %s fail, try next one", strm.Id)
			continue
		}

		s.Lock()
		//do prepare works before spawning task
		s.activedTasks++
		s.excluedePeers[*strm.Id] = true
		s.Unlock()

		clilogger.Debugf("start syncing task on peer %s", strm.Id)
		//spawn handling routine
		go func(f func(context.Context, *pb.StreamHandler, *syncCore) error, strm *pb.PickedStreamHandler, core *syncCore) {
			err := f(ctx, strm.StreamHandler, core)
			s.activeTaskExit(strm.Id, err)

		}(cf.AssignHandling(), strm, castedh.core.syncCore)

		if err := s.waitForNext(ctx); err != nil {
			clilogger.Debugf("current schedule for task [%s] exit for [%s]", cf.Tag(), err)
			return
		}
	}

}

type ForceAck struct{}

func (ForceAck) Error() string { return "Forced acking" }

type SessionClientImpl interface {
	PreFilter(rledger *pb.LedgerState) bool
	//assign onconnect and next an id to distinguish the
	//different tasks, next can gen a user-custom object
	//and it was passed to OnData/Fail
	OnConnected(int, *pb.AcceptSession) error
	Next(int) (*pb.TransferRequest, interface{})
	//can return ForceAck or NormalEnd
	//if normalend is returned, Next() will
	//be call and the whole session is ended
	//if Next() return nil
	//Notice 1: if OnData return any error execpt
	//for ForceAck, session will end without
	//OnFail being called
	//Notice 2: we always ack when normal end
	OnData(interface{}, *pb.TransferResponse) error
	OnFail(interface{}, error)
}

type sessionCliAdapter struct {
	tag       string
	conn      *pb.OpenSession
	options   *clientOpts
	taskIdCnt int
	SessionClientImpl
}

func newSessionClient(tag string, impl SessionClientImpl) *sessionCliAdapter {
	ret := new(sessionCliAdapter)
	ret.tag = tag
	ret.options = DefaultClientOption()
	ret.SessionClientImpl = impl
	return ret
}

func (sa *sessionCliAdapter) setConnectMessage(msg *pb.OpenSession) {
	sa.conn = msg
}

func (sa *sessionCliAdapter) waitResp(ctx context.Context, c <-chan *pb.SyncMsg_Response) (*pb.SyncMsg_Response, error) {

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg := <-c:
		return msg, nil
	}

}

func (sa *sessionCliAdapter) handlingFunc(ctx context.Context, tskId int, strm msgSender, c *syncCore) (err error) {

	//a task is assigned first so we can fast end handling if no task
	req, taskCustom := sa.Next(tskId)
	defer func() {
		if failobj := recover(); failobj != nil {
			err = FatalEnd{fmt.Errorf("Fatal data: %v", failobj)}
		}

		if _, ok := err.(NormalEnd); err != nil && !ok {
			clilogger.Debugf("handling exit for session error: %s", err)
			sa.OnFail(taskCustom, err)
		}
	}()

	if req == nil {
		return NormalEnd{}
	}

	respc, err := c.OpenSession(strm, sa.conn)
	//opensession fail
	if err != nil {
		return err
	}

	accept, err := sa.waitResp(ctx, respc)
	if err != nil {
		c.CancelRequest(respc)
		return err
	} else if serr := accept.GetErr(); serr != nil {
		return fmt.Errorf("open session fail <%d>: %s", serr.GetErrorCode(), serr.GetErrorDetail())
	}
	defer c.SessionClose()

	if err = sa.OnConnected(tskId, accept.GetHandshake()); err != nil {
		return err
	} else if err = c.SessionReq(req); err != nil {
		return err
	}

	var ackCounter, ackWindow int
	if hs := accept.GetHandshake(); hs != nil {
		ackWindow = int(hs.GetTransfer().GetMaxWindowSize() / 2)
	}

	for {
		var msg *pb.SyncMsg_Response
		msg, err = sa.waitResp(ctx, respc)
		if err != nil {
			return err
		}

		if serr := msg.GetErr(); serr != nil {
			err = fmt.Errorf("resp fail <%d>: %s", serr.GetErrorCode(), serr.GetErrorDetail())
			return err
		} else if pack := msg.GetSession(); pack == nil {
			err = fmt.Errorf("mal-formed resp: no package")
			return err
		} else if err = sa.OnData(taskCustom, pack); err != nil {

			switch err.(type) {
			case ForceAck:
				err = c.SessionAck(pack, nil)
			case NormalEnd:
				req, taskCustom = sa.Next(tskId)
				if req == nil {
					clilogger.Debug("handling normal exit")
					return err
				}
				err = c.SessionAck(pack, req)
			default:
				clilogger.Debugf("handling exit for impl return error: %s", err)
				return err
			}
			ackCounter = 0

		} else {
			ackCounter++
			if ackCounter > ackWindow {
				ackCounter = 0
				err = c.SessionAck(pack, nil)
			}
		}

		if err != nil {
			return err
		}
	}

	return fmt.Errorf("Should not here")

}

func (sa *sessionCliAdapter) Tag() string       { return sa.tag }
func (sa *sessionCliAdapter) Opts() *clientOpts { return sa.options }
func (sa *sessionCliAdapter) AssignHandling() func(context.Context, *pb.StreamHandler, *syncCore) error {

	if sa.conn == nil {
		panic("Connect message is not set")
	}

	tskId := sa.taskIdCnt
	sa.taskIdCnt++

	return func(ctx context.Context, strm *pb.StreamHandler, c *syncCore) error {
		return sa.handlingFunc(ctx, tskId, strm, c)
	}

}
