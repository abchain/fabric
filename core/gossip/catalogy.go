package gossip

import (
	"fmt"
	model "github.com/abchain/fabric/core/gossip/model"
	"github.com/abchain/fabric/core/gossip/stub"
	pb "github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"
	"sync"
	"time"
)

type CatalogHandler interface {
	Name() string
	//nil can be passed to just notify handler the status it manager is updated
	SelfUpdate(model.Update)
	HandleUpdate(*pb.PeerID, *pb.Gossip_Update, CatalogPeerPolicies)
	HandleDigest(*pb.PeerID, *pb.Gossip_Digest, CatalogPeerPolicies)
}

type CatalogPeerPolicies interface {
	GetId() string
	AllowRecvUpdate() bool
	PushUpdateQuota() int
	PushUpdate(int)
	RecordViolation(error)
	ScoringPeer(s int, weight uint)
}

type CatalogHelper interface {
	Name() string
	GetPolicies() CatalogPolicies //caller do not need to check the interface

	TransDigestToPb(model.Digest) *pb.Gossip_Digest
	TransPbToDigest(*pb.Gossip_Digest) model.Digest

	UpdateMessage() proto.Message
	EncodeUpdate(CatalogPeerPolicies, model.Update, proto.Message) proto.Message
	DecodeUpdate(CatalogPeerPolicies, proto.Message) (model.Update, error)
}

type puller struct {
	model.PullerHelper
	*model.Puller
	hf func(*model.Puller)
}

func (p *puller) init(ph model.PullerHelper, hf func(*model.Puller)) {
	p.PullerHelper = ph
	p.hf = hf
}

func (p *puller) Handle(puller *model.Puller) {
	p.Puller = puller
	if p.hf == nil {
		logger.Criticalf("Your code never call init method before providing it in the CanPull callback")
	}

	//we use a new thread for handing puller task
	go p.hf(puller)
}

type pullWorks struct {
	sync.Mutex
	m map[CatalogPeerPolicies]*puller
}

func (h *pullWorks) popPuller(cpo CatalogPeerPolicies) *puller {

	h.Lock()
	defer h.Unlock()
	p := h.m[cpo]
	delete(h.m, cpo)
	return p
}

func (h *pullWorks) newPuller(cpo CatalogPeerPolicies) *puller {
	h.Lock()
	defer h.Unlock()

	if _, ok := h.m[cpo]; ok {
		return nil
	}

	p := &puller{}
	h.m[cpo] = p

	return p
}

type scheduleWorks struct {
	cancelSchedule context.CancelFunc
	pushingCnt     int
	plannedCnt     int
	sync.Mutex
}

func (s *scheduleWorks) pushedCnt() int {
	s.Lock()
	defer s.Unlock()
	return s.pushingCnt
}

func (s *scheduleWorks) pushDone() {

	s.Lock()
	defer s.Unlock()
	s.pushingCnt++
}

func (s *scheduleWorks) endSchedule() {
	s.Lock()
	defer s.Unlock()

	if s.cancelSchedule != nil {
		//cancel last schedule and use a new one
		s.cancelSchedule()
		s.cancelSchedule = nil
	}
}

func (s *scheduleWorks) planPushingCount(newCnt int) {
	s.Lock()
	defer s.Unlock()

	if s.plannedCnt < newCnt {
		s.plannedCnt = newCnt
	}
}

//prepare a context and pop the planned pushing count
func (s *scheduleWorks) newSchedule(ctx context.Context) (context.Context, int) {

	s.Lock()
	defer s.Unlock()

	//check plannedCnt is important: that is, NEVER start a schedule
	//with 0 planned count
	if s.cancelSchedule != nil || s.plannedCnt == 0 {
		return nil, 0
	}

	wctx, cancelF := context.WithCancel(ctx)
	s.cancelSchedule = cancelF
	s.pushingCnt = 0

	plannedCnt := s.plannedCnt
	s.plannedCnt = 0

	return wctx, plannedCnt
}

type catalogHandler struct {
	CatalogHelper
	model    *model.Model
	pulls    pullWorks
	schedule scheduleWorks
	sstub    *pb.StreamStub
}

func NewCatalogHandlerImpl(stub *pb.StreamStub, helper CatalogHelper, model *model.Model) (ret *catalogHandler) {

	return &catalogHandler{
		CatalogHelper: helper,
		sstub:         stub,
		model:         model,
		pulls:         pullWorks{m: make(map[CatalogPeerPolicies]*puller)},
	}
}

type sessionHandler struct {
	*catalogHandler
	cpo CatalogPeerPolicies
}

//implement of pushhelper and pullerhelper
func (h *sessionHandler) EncodeDigest(d model.Digest) proto.Message {

	msg := &pb.Gossip{
		Seq:     getGlobalSeq(),
		Catalog: h.Name(),
		M:       &pb.Gossip_Dig{h.TransDigestToPb(d)},
	}

	h.cpo.PushUpdate(msg.EstimateSize())

	return msg
}

func (h *sessionHandler) EncodeUpdate(u model.Update) proto.Message {

	udsent := &pb.Gossip_Update{}

	if u != nil {
		payloadByte, err := proto.Marshal(
			h.CatalogHelper.EncodeUpdate(h.cpo, u,
				h.CatalogHelper.UpdateMessage()))
		if err == nil {
			udsent.Payload = payloadByte
			h.cpo.PushUpdate(len(payloadByte))
		} else {
			logger.Error("Encode update failure:", err)
		}
	}

	return &pb.Gossip{
		Seq:     getGlobalSeq(),
		Catalog: h.Name(),
		M:       &pb.Gossip_Ud{udsent},
	}
}

func (h *sessionHandler) runPullTask(cpo CatalogPeerPolicies, puller *model.Puller) (ret error) {

	pctx, _ := context.WithTimeout(context.Background(),
		time.Duration(h.GetPolicies().PullTimeout())*time.Second)
	ret = puller.Process(pctx)
	return
}

func (h *sessionHandler) CanPull() model.PullerHandler {

	if !h.cpo.AllowRecvUpdate() {
		return nil
	}

	if pos := h.pulls.newPuller(h.cpo); pos == nil {
		return nil
	} else {
		pos.init(h, func(p *model.Puller) {

			err := h.runPullTask(h.cpo, p)

			//when we succefully accept an update, we also schedule a new push process
			if err == nil {
				h.schedule.planPushingCount(h.GetPolicies().PushCount())
				h.executePush(map[string]bool{h.cpo.GetId(): true})
			} else {
				h.cpo.RecordViolation(fmt.Errorf("Passive pulling fail: %s", err))
			}
		})
		return pos
	}
}

func (h *catalogHandler) SelfUpdate(u model.Update) {

	if u != nil {
		h.model.RecvUpdate(u)
	}

	h.schedule.planPushingCount(h.GetPolicies().PushCount())
	go func() {
		//complete for the schedule resource until it failed
		for h.executePush(map[string]bool{}) == nil {
		}
	}()
}

func (h *catalogHandler) HandleDigest(peer *pb.PeerID, msg *pb.Gossip_Digest, cpo CatalogPeerPolicies) {

	strm := h.sstub.PickHandler(peer)
	if strm == nil {
		logger.Errorf("No stream found for %s", peer.Name)
		return
	}

	err := model.AcceptPush(&sessionHandler{h, cpo}, strm, h.model, h.TransPbToDigest(msg))
	if err != nil {
		logger.Error("Sending push message fail", err)
	} else {
		h.schedule.pushDone()
	}
}

func (h *catalogHandler) HandleUpdate(peer *pb.PeerID, msg *pb.Gossip_Update, cpo CatalogPeerPolicies) {

	puller := h.pulls.popPuller(cpo)
	if puller == nil {
		//something wrong! if no corresponding puller, we never handle this message
		logger.Errorf("No puller in catalog %s avaiable for peer %s", h.Name(), peer.Name)
		return
	}

	umsg := h.CatalogHelper.UpdateMessage()
	err := proto.Unmarshal(msg.Payload, umsg)
	if err != nil {
		cpo.RecordViolation(fmt.Errorf("Unmarshal message for update in catalog %s fail: %s", h.Name(), err))
		return
	}

	ud, err := h.DecodeUpdate(cpo, umsg)
	if err != nil {
		cpo.RecordViolation(fmt.Errorf("Decode update for catalog %s fail: %s", h.Name(), err))
		return
	}

	//the final update is executed in another thread
	puller.NotifyUpdate(ud)

}

func (h *catalogHandler) executePush(excluded map[string]bool) error {

	logger.Debug("try execute a pushing process")
	wctx, pushCnt := h.schedule.newSchedule(context.Background())
	if wctx == nil {
		logger.Debug("Another pushing process is running or no plan any more")
		return fmt.Errorf("Resource is occupied")
	}
	defer h.schedule.endSchedule()

	for strm := range h.sstub.OverAllHandlers(wctx) {

		logger.Debugf("finish (%d/%d) pulls, try execute a pushing on stream to %s",
			h.schedule.pushedCnt(), pushCnt, strm.GetName())

		if h.schedule.pushedCnt() >= pushCnt {
			break
		}

		ph, ok := stub.ObtainHandler(strm.StreamHandler).(*handlerImpl)
		if !ok {
			panic("type error, not handlerImpl")
		}
		cpo := ph.GetPeerPolicy()

		//check excluded first
		_, ok = excluded[cpo.GetId()]
		if ok {
			logger.Debugf("Skip exclueded peer [%s]", cpo.GetId())
			continue
		}

		if pos := h.pulls.newPuller(cpo); pos != nil {
			logger.Debugf("start pulling on stream to %s", cpo.GetId())
			pos.Puller = model.NewPuller(&sessionHandler{h, cpo}, strm.StreamHandler, h.model)
			pctx, _ := context.WithTimeout(wctx, time.Duration(h.GetPolicies().PullTimeout())*time.Second)
			err := pos.Puller.Process(pctx)

			logger.Debugf("Scheduled pulling from peer [%s] finish: %v", cpo.GetId(), err)
			if err == context.DeadlineExceeded {
				cpo.RecordViolation(fmt.Errorf("Aggressive pulling fail: %s", err))
			}

		} else {
			logger.Debugf("stream %s has a working puller, try next one", cpo.GetId())
		}
	}

	logger.Infof("Finished a push process, plan %d and %d finished", pushCnt, h.schedule.pushedCnt())

	return nil
}
