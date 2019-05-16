package gossip

import (
	"fmt"
	model "github.com/abchain/fabric/core/gossip/model"
	pb "github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"
	"sync"
	"time"
)

type CatalogHandler interface {
	Name() string
	Model() *model.Model
	//just notify the model is updated
	SelfUpdate(...*pb.PeerID)
	HandleUpdate(*pb.GossipMsg_Update, CatalogPeerPolicies)
	HandleDigest(*pb.StreamHandler, *pb.GossipMsg_Digest, CatalogPeerPolicies)
}

type CatalogPeerPolicies interface {
	GetId() string //equal to peerId.GetName()
	AllowRecvUpdate() bool
	PushUpdateQuota() int
	PushUpdate(int)
	RecordViolation(error)
	ScoringPeer(s int, weight uint)
}

type CatalogHelper interface {
	Name() string
	GetPolicies() CatalogPolicies //caller do not need to check the interface

	TransDigestToPb(model.Digest) *pb.GossipMsg_Digest
	TransPbToDigest(*pb.GossipMsg_Digest) model.Digest

	TransUpdateToPb(CatalogPeerPolicies, model.Update) *pb.GossipMsg_Update
	TransPbToUpdate(CatalogPeerPolicies, *pb.GossipMsg_Update) (model.Update, error)
	//	EncodeUpdate(CatalogPeerPolicies, model.Update, proto.Message) proto.Message
	//	DecodeUpdate(CatalogPeerPolicies, proto.Message) (model.Update, error)
}

type pullWorks struct {
	sync.Mutex
	m map[CatalogPeerPolicies]*model.Puller
	//a home-make sync.condition which can work along with a context
	occFlag       int
	releaseNotify chan error
}

func (h *pullWorks) queryPuller(cpo CatalogPeerPolicies) *model.Puller {

	h.Lock()
	defer h.Unlock()
	return h.m[cpo]
}

func (h *pullWorks) popPuller(cpo CatalogPeerPolicies) *model.Puller {

	h.Lock()
	defer h.Unlock()
	p, ok := h.m[cpo]
	if ok {
		delete(h.m, cpo)

		if h.occFlag > 1 {
			select {
			case h.releaseNotify <- nil:
			default:
				logger.Errorf("Seems [%s] try to made notify while no peer is waiting any more", cpo.GetId())
			}
		} else if h.occFlag != 1 {
			panic("Impossible occFlag")
		}
		h.occFlag--

	}
	return p
}

//if we check-and-create puller under "responsed-pulling" condiction, no-op also indicate the existed
//puller a pushing is processed
func (h *pullWorks) newPuller(ctx context.Context, cpo CatalogPeerPolicies, m *model.Model) *model.Puller {
	h.Lock()
	defer h.Unlock()

	if _, ok := h.m[cpo]; ok {
		return nil
	}

	puller := model.NewPuller(m)
	h.m[cpo] = puller
	h.occFlag++

	if h.occFlag > 1 {

		//we must wait for another pulling is finish, so avoid a concurrent-polling which may
		//waste a lot of bandwidth and comp. cost
		//this routine work like a condiction
		h.Unlock()
		select {
		case <-h.releaseNotify:
		case <-ctx.Done():
			logger.Infof("Peer [%s] waiting for another pulling fail, give up this pulling", cpo.GetId())
			h.Lock()
			h.m[cpo] = nil
			h.occFlag--
			return nil
		}

		h.Lock()
	}
	return puller
}

type scheduleWorks struct {
	cancelSchedules   map[context.Context]context.CancelFunc
	totalPushingCnt   int64
	plannedCnt        int64
	maxConcurrentTask int
	sync.RWMutex
}

func (s *scheduleWorks) reachPlan(target int64) bool {
	s.Lock()
	defer s.Unlock()

	return target <= s.totalPushingCnt
}

func (s *scheduleWorks) pushDone() {

	s.Lock()
	defer s.Unlock()

	//it is not count for any "exceed the planning" pushing
	if s.totalPushingCnt < s.plannedCnt {
		s.totalPushingCnt++
	}

}

func (s *scheduleWorks) endSchedule(ctx context.Context) {

	s.Lock()
	defer s.Unlock()

	if cf, ok := s.cancelSchedules[ctx]; ok {
		cf()
		delete(s.cancelSchedules, ctx)
	}

	if len(s.cancelSchedules) == 0 && s.totalPushingCnt < s.plannedCnt {
		logger.Infof("all schedules end with planned count %d left, drop planning",
			s.plannedCnt-s.totalPushingCnt)
		s.plannedCnt = s.totalPushingCnt
	}
}

//schedule works as following:
//1. each request will be passed (return a context) if current "pending" count (plannedcnt - totalpushing)
//   is not larger than the wishCount
//2. passed request obtained a "target" pushing number and should check if currently totalpushing has reached the
//   target number
//3. caller with passed request can do everything possible to make the totalpushing count increase (it can even
//   do nothing and just wait there until there is enough incoming digests)
func (s *scheduleWorks) newSchedule(ctx context.Context, wishCount int) (context.Context, int64) {

	s.Lock()
	defer s.Unlock()

	curPlannedCnt := s.totalPushingCnt + int64(wishCount)

	//no need to schedule more
	if len(s.cancelSchedules) > s.maxConcurrentTask {
		logger.Infof("A schedule is rejected because the concurrent task exceed limit: %d", s.maxConcurrentTask)
		return nil, 0
	} else if curPlannedCnt <= s.plannedCnt {
		//no need to start new schedule
		return nil, 0
	}

	s.plannedCnt = curPlannedCnt

	wctx, cancelF := context.WithCancel(ctx)
	s.cancelSchedules[wctx] = cancelF

	return wctx, curPlannedCnt
}

type catalogHandler struct {
	CatalogHelper
	hctx     context.Context //hctx CAN NOT be used for waiting directly!
	model    *model.Model
	pulls    pullWorks
	schedule scheduleWorks
	sstub    *pb.StreamStub
}

func NewCatalogHandlerImpl(stub *pb.StreamStub, ctx context.Context, helper CatalogHelper, m *model.Model) (ret *catalogHandler) {

	return &catalogHandler{
		CatalogHelper: helper,
		sstub:         stub,
		hctx:          ctx,
		model:         m,
		pulls:         pullWorks{m: make(map[CatalogPeerPolicies]*model.Puller), releaseNotify: make(chan error, 1)},
		schedule:      scheduleWorks{cancelSchedules: make(map[context.Context]context.CancelFunc), maxConcurrentTask: 5},
	}
}

type sessionHandler struct {
	*catalogHandler
	cpo        CatalogPeerPolicies
	notPull    bool
	pullingCtx context.Context
	isRespond  bool
}

func genSessionHandler(h *catalogHandler, cpo CatalogPeerPolicies, resp bool) *sessionHandler {
	return &sessionHandler{h, cpo, false, nil, resp}
}

//implement of pushhelper and pullerhelper
func (h *sessionHandler) EncodeDigest(d model.Digest) proto.Message {

	msg := &pb.GossipMsg{
		Seq:     getGlobalSeq(),
		Catalog: h.Name(),
		M:       &pb.GossipMsg_Dig{Dig: h.TransDigestToPb(d)},
	}

	//a responding pull should never require more responding,
	//else, we respect the handler's option
	if h.isRespond {
		msg.GetDig().NoResp = true
	}

	h.cpo.PushUpdate(msg.EstimateSize())

	return msg
}

var uSizeWarning = 0

func (h *sessionHandler) EncodeUpdate(u model.Update) proto.Message {

	var udsent *pb.GossipMsg_Update

	if u != nil {

		udsent = h.catalogHandler.TransUpdateToPb(h.cpo, u)
		updateSize := proto.Size(udsent)
		if updateSize == 0 && uSizeWarning < 5 {
			logger.Warningf("can not estimate the size of update message [%v]", udsent)
		}

		h.cpo.PushUpdate(updateSize)
	}

	return &pb.GossipMsg{
		Seq:     getGlobalSeq(),
		Catalog: h.Name(),
		M:       &pb.GossipMsg_Ud{Ud: udsent},
	}
}

func (h *sessionHandler) Process(strm *pb.StreamHandler, d model.Digest) (err error) {

	cpo := h.cpo
	logger.Debugf("Start a pulling (responding %v) to peer [%s]", h.isRespond, cpo.GetId())

	pctx, pctxend := context.WithTimeout(h.hctx,
		time.Duration(h.GetPolicies().PullTimeout())*time.Second)
	defer pctxend()
	h.pullingCtx = pctx

	var puller *model.Puller
	if h.isRespond {
		puller, err = model.AcceptPulling(h, strm, h.Model(), d)
	} else {
		puller, err = model.StartPulling(h, strm)
	}

	if puller != nil {
		defer h.pulls.popPuller(cpo)
	}

	if err != nil {
		logger.Errorf("accepting pulling fail: %s", err)
		return
	} else if puller == nil {
		logger.Debugf("do not start pulling to peer [%s]", cpo.GetId())
		return
	}

	err = puller.Process(pctx)

	//when we succefully accept an update, we also trigger a new push process
	if err == nil {
		//notice we should exclude current stream
		//so the triggered push wouldn't make duplicated pulling
		if h.isRespond {
			go h.executePush(nil, strm)
		}
	} else if err == model.EmptyUpdate {
		logger.Debugf("pull nothing from peer [%s]", cpo.GetId())
	} else {
		cpo.RecordViolation(fmt.Errorf("Pulling (responding %v) fail: %s", h.isRespond, err))
	}

	return
}

func (h *sessionHandler) CanPull() *model.Puller {

	if h.pullingCtx == nil {
		panic("WRONG CODE: CanPull can be only called implicitly within the Process method")
	}

	if h.notPull || !h.GetPolicies().AllowRecvUpdate() || !h.cpo.AllowRecvUpdate() {
		logger.Debugf("Policy has rejected a pulling to peer [%s]", h.cpo.GetId())
		return nil
	}

	return h.pulls.newPuller(h.pullingCtx, h.cpo, h.Model())
}

func (h *catalogHandler) Model() *model.Model {

	return h.model
}

var emptyExcluded = make(map[*pb.StreamHandler]bool)

func (h *catalogHandler) SelfUpdate(to ...*pb.PeerID) {

	go h.executePush(to, nil)
}

func (h *catalogHandler) HandleDigest(strm *pb.StreamHandler, msg *pb.GossipMsg_Digest, cpo CatalogPeerPolicies) {

	sess := genSessionHandler(h, cpo, true)
	sess.notPull = msg.NoResp
	go sess.Process(strm, h.TransPbToDigest(msg))

	//everytime we accept a digest, it is counted as a push
	h.schedule.pushDone()
}

func (h *catalogHandler) HandleUpdate(msg *pb.GossipMsg_Update, cpo CatalogPeerPolicies) {

	puller := h.pulls.queryPuller(cpo)
	if puller == nil {
		//something wrong! if no corresponding puller, we never handle this message
		logger.Errorf("No puller in catalog %s avaiable for peer %s", h.Name(), cpo.GetId())
		return
	}

	ud, err := h.TransPbToUpdate(cpo, msg)
	if err != nil {
		cpo.RecordViolation(fmt.Errorf("Decode update for catalog %s fail: %s", h.Name(), err))
	}

	//the final update is executed in another thread, even nil update is accepted
	logger.Debugf("Accept update from peer [%s]", cpo.GetId())
	puller.NotifyUpdate(ud)

}

func (h *catalogHandler) executePush(to []*pb.PeerID, excluded *pb.StreamHandler) error {

	logger.Debug("try execute a pushing process for handler", h.Name())
	wctx, targetCnt := h.schedule.newSchedule(h.hctx, h.GetPolicies().PushCount())
	if wctx == nil {
		logger.Debug("Another pushing process is running or no plan any more")
		return fmt.Errorf("Resource is occupied")
	}
	defer h.schedule.endSchedule(wctx)

	var pushCnt int
	var overChn chan *pb.PickedStreamHandler
	if len(to) == 0 {
		overChn = h.sstub.OverAllHandlers(wctx)
	} else {
		overChn = h.sstub.OverHandlers(wctx, to)
	}
	for strm := range overChn {

		if h.schedule.reachPlan(targetCnt) {
			break
		} else if strm.StreamHandler == excluded {
			logger.Debugf("stream %s is excluded, try next one", strm.Id.GetName())
			continue
		}

		logger.Debugf("finish (%d) pulls, try execute a pushing on stream to %s",
			pushCnt, strm.Id.GetName())

		ph, ok := ObtainHandler(strm.StreamHandler).(*handlerImpl)
		if !ok {
			panic("type error, not handlerImpl")
		}
		cpo := ph.GetPeerPolicy()

		if err := genSessionHandler(h, cpo, false).Process(strm.StreamHandler, nil); err == model.EmptyDigest {
			//***GIVEN UP THE WHOLE PUSH PROCESS***
			logger.Infof("Catalogy model has forbidden a pulling process")
			break
		} else if err == nil {
			pushCnt++
			logger.Debugf("Scheduled pulling from peer [%s] finish", cpo.GetId())
		} else {
			logger.Debugf("Scheduled pulling from peer [%s] failed (%v)", cpo.GetId(), err)
		}

	}

	logger.Debugf("Finished a push process,  %d finished", pushCnt)

	return nil
}
