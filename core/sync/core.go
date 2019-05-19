package sync

import (
	"fmt"
	pb "github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/proto"
	"github.com/looplab/fsm"
)

//take the interface from streamhandler (so it was convenient to mock in testing)
type msgSender interface {
	SendMessage(m proto.Message) error
}

func fail(id uint64, err *pb.RequestError) *pb.SyncMsg {
	return &pb.SyncMsg{
		Type:          pb.SyncMsg_SERVER_ERROR,
		CorrelationId: id,
		Response:      &pb.SyncMsg_Response{Err: err},
	}
}

func failStr(id uint64, err string) *pb.SyncMsg {
	return fail(id, &pb.RequestError{ErrorDetail: err})
}

func filterError(err error) error {
	if _, ok := err.(fsm.NoTransitionError); ok {
		return nil
	}
	return err
}

type sessionOptions struct {
	maxWindow  int
	maxPending int
}

type syncCore struct {
	*fsm.FSM
	msgId      uint64
	sessionOpt sessionOptions

	client map[uint64]chan *pb.SyncMsg_Response
	//we basically use this for tracing currently pending request
	//and also record a message farend for easily using
	waitForResp map[uint64]msgSender
	//we send out the whole message, not just the request part
	//for handling, because type and correlationId is also essential
	server  chan<- *pb.SyncMsg
	session *syncSession
}

type syncSession struct {
	farend      msgSender
	client      chan<- *pb.SyncMsg_Response
	id          uint64
	writeNotify chan struct{}
	acked       uint32
	lastData    uint32
	maxWindow   uint32
}

func (s *syncCore) Init(push chan *pb.SyncMsg_Response, srv chan<- *pb.SyncMsg) {
	s.client[0] = push
	s.server = srv
}

func (s *syncCore) TransportWindow() int {
	if s == nil {
		return 0
	}
	return s.sessionOpt.maxWindow
}

func (s *syncCore) HandleMessage(msg *pb.SyncMsg, sh msgSender) {
	logger.Debugf("recv and handle message %s[%d]", msg.GetType(), msg.GetCorrelationId())
	err := s.Event(msg.GetType().OnRecv(), msg, sh)
	if filterError(err) != nil {
		logger.Warningf("Handling incoming message [%v] fail: %s", msg, err)
	}
}

func (s *syncCore) SendMessage(msg *pb.SyncMsg, args ...interface{}) error {
	err := s.Event(msg.GetType().OnSend(), append([]interface{}{msg}, args...)...)
	if filterError(err) != nil {
		return err
	}
	return nil
}

func (s *syncCore) onSendMessage(e *fsm.Event) {

	//so we have three types of events, according to the numbers of args
	//1. resp message, it has an entry in waitForResp and we can take the
	//   message farend from that
	//2. pushing or session message, it just has far end in arg
	//3. request message, it has far end and a channel for resolving as args

	msg := e.Args[0].(*pb.SyncMsg)
	logger.Debugf("Sending message [%v] with %d params", msg, len(e.Args))
	switch len(e.Args) {
	case 1:
		if farend, ok := s.waitForResp[msg.GetCorrelationId()]; ok {
			farend.SendMessage(msg)
			delete(s.waitForResp, msg.GetCorrelationId())
		} else {
			logger.Errorf("message [%d]:[%v] has not a request for resolving", msg.GetCorrelationId(), msg)
		}
	case 2:
		//the session or pushing message
		farend := e.Args[1].(msgSender)
		farend.SendMessage(msg)
	case 3:
		farend, chn := e.Args[1].(msgSender), e.Args[2].(chan *pb.SyncMsg_Response)
		msg.CorrelationId = s.msgId
		s.client[msg.CorrelationId] = chn
		s.msgId++
		farend.SendMessage(msg)
	default:
		panic(fmt.Sprintf("Malform in args for a sending message event: [%v]", e.Args))
	}
}

func (s *syncCore) onBeforeRecvRespMessage(e *fsm.Event) {

	msg := e.Args[0].(*pb.SyncMsg)
	logger.Debugf("receive response message [%v]", msg)

	if resp := msg.GetResponse(); resp != nil {
		msgId := msg.GetCorrelationId()
		if target, ok := s.client[msgId]; ok {
			select {
			case target <- resp:
			default:
				logger.Errorf("incoming resp for [%d] could not be sent for being blocked", msgId)
			}
		} else {
			logger.Warningf("incoming resp for [%d]:[%v] has no corresponding entry", msgId, resp)
			e.Cancel(fmt.Errorf("msg no entry"))
		}
	}
}

func (s *syncCore) onBeforeRecvRequestMessage(e *fsm.Event) {

	//sanity check
	if s.session != nil {
		panic("session req should not come here, wrong FSM")
	}

	msg, farend := e.Args[0].(*pb.SyncMsg), e.Args[1].(msgSender)
	logger.Debugf("receive request message [%v]", msg)

	var err error
	select {
	case s.server <- msg:
		if _, ok := s.waitForResp[msg.GetCorrelationId()]; !ok {
			s.waitForResp[msg.GetCorrelationId()] = farend
		} else {
			logger.Errorf("incoming request [%v] is duplicated to another request", msg)
			err = fmt.Errorf("wrong request")
		}

	default:
		logger.Errorf("incoming request [%v] could not be sent for being blocked", msg)
		err = fmt.Errorf("server is busy")
	}

	if err == nil {
		return
	}

	//an error message is build and sent back for response, skip FSM handling
	e.Cancel(err)
	go farend.SendMessage(failStr(msg.GetCorrelationId(), err.Error()))

}

//do cleaning for resp message (any simple resp and error)
func (s *syncCore) onRecvRespMessage(e *fsm.Event) {

	msg := e.Args[0].(*pb.SyncMsg)
	//never clear the entry for pushing (id == 0)
	if msgId := msg.GetCorrelationId(); msgId != 0 {
		delete(s.client, msgId)
	}
}

//session control ...
func (s *syncCore) onBeforeRecvSessionMessage(e *fsm.Event) {

	//sanity check
	if s.session == nil {
		panic("no session in session state, wrong FSM")
	}

	msg := e.Args[0].(*pb.SyncMsg)
	logger.Debugf("receive session message [%v]", msg)

	if msg.GetCorrelationId() != s.session.id {
		logger.Errorf("incoming resp for unexpected session ([%d], current [%d]", msg.GetCorrelationId(), s.session.id)
		e.Cancel(fmt.Errorf("Wrong session"))
		return
	}

	if resp := msg.GetResponse(); resp != nil {
		select {
		case s.session.client <- resp:
		default:
			logger.Errorf("incoming resp for session [%d] could not be sent for being blocked", s.session.id)
		}
	} else {
		select {
		case s.server <- msg:
		default:
			logger.Errorf("incoming request for session [%d] could not be sent for being blocked", s.session.id)
		}
	}
}

func (s *syncCore) onBeforeSendSessionMessage(e *fsm.Event) {

	//sanity check
	if s.session == nil {
		panic("no session in session state, wrong FSM")
	}

	msg := e.Args[0].(*pb.SyncMsg)

	msg.CorrelationId = s.session.id

	e.Args = []interface{}{msg, s.session.farend}
}

func (s *syncCore) onBeforeSendTransport(e *fsm.Event) {

	//chain general handling first
	s.onBeforeSendSessionMessage(e)

	msg := e.Args[0].(*pb.SyncMsg)
	if pack := msg.GetResponse().GetSession(); pack != nil {
		//we need seq should start from 1
		s.session.lastData++
		pack.Seq = s.session.lastData
	} else {
		e.Cancel(fmt.Errorf("No payload in session transport msg"))
	}
}

func (ss *syncSession) canWrite() bool {
	return ss.acked+ss.maxWindow >= ss.lastData
}

func (s *syncCore) onRecvAcking(e *fsm.Event) {

	msg := e.Args[0].(*pb.SyncMsg)
	if ack := msg.GetRequest().GetAck(); ack != 0 {
		if ack > s.session.lastData {
			e.Cancel(fmt.Errorf("Wrong acking: out-of-bound seq %d, current %d", s.session.acked, s.session.lastData))
		} else if ack < s.session.acked {
			e.Cancel(fmt.Errorf("Wrong acking: out-of-bound seq %d, las recv %d", s.session.acked, s.session.acked))
		} else {
			s.session.acked = ack
		}

		if s.session.writeNotify != nil && s.session.canWrite() {
			close(s.session.writeNotify)
			s.session.writeNotify = nil
		}
	}
}

func (s *syncCore) onRecvTransport(e *fsm.Event) {

	msg := e.Args[0].(*pb.SyncMsg)
	if pack := msg.GetResponse().GetSession(); pack != nil {
		if pack.Seq > s.session.lastData {
			s.session.lastData = pack.Seq
		} else {
			logger.Warningf("receive out-of-order transport package (with seq %d but last recv is %d)", pack.Seq, s.session.lastData)
		}

	}

	//TODO: we do nothing now but we may enable some "automatic acking" policy later
}

var defaultClosedChan = func() <-chan struct{} {
	ret := make(chan struct{})
	close(ret)
	return ret
}()

const setWriteNotify = "settingWriteNotify"
const testIdle = "testingIdle"
const cancelRequest = "cancelRequest"

func (s *syncCore) onCancelRequest(e *fsm.Event) {
	reqC := e.Args[0].(chan *pb.SyncMsg_Response)

	for k, c := range s.client {
		if c == reqC {
			logger.Infof("Sync request [%d] has been cancelled", k)
			delete(s.client, k)
			return
		}
	}
	e.Cancel(fmt.Errorf("No client entry"))
}

func (s *syncCore) onSettingWriteNotify(e *fsm.Event) {

	logger.Debugf("detect session can write: [%d:%d win<%d>]", s.session.lastData, s.session.acked, s.session.maxWindow)
	out := e.Args[0].(*<-chan struct{})
	if !s.session.canWrite() {
		if s.session.writeNotify == nil {
			s.session.writeNotify = make(chan struct{})
		}
		*out = s.session.writeNotify
	} else {
		*out = defaultClosedChan
	}
}

func (s *syncCore) SessionCanWrite() <-chan struct{} {
	var ret <-chan struct{}

	if err := s.Event(setWriteNotify, &ret); filterError(err) != nil {
		logger.Errorf("test session write failure: %s", err)
		return defaultClosedChan
	}

	return ret
}

func (s *syncCore) onIdleTesting(e *fsm.Event) {
	if len(s.client) > e.Args[0].(int) {
		e.Cancel(fmt.Errorf("Too much pending requests"))
	}
}

func (s *syncCore) IsIdle() bool {
	if err := s.Event(testIdle, s.sessionOpt.maxPending); filterError(err) != nil {
		logger.Debugf("idle test fail: %s", err)
		return false
	}
	return true
}

func (s *syncCore) onStateSessionOpen(e *fsm.Event) {

	msg := e.Args[0].(*pb.SyncMsg)
	logger.Debugf("enter session open state: %v", msg)

	if s.session != nil {
		panic("Fail FSM: Duplicated session")
	}

	//message should be acceptsession
	acceptMsg := msg.GetResponse().GetHandshake()
	if acceptMsg == nil {
		panic(fmt.Sprintf("Fail FSM: trigger session state by other message [%v]", e))
	}

	var farend msgSender
	windowSize := acceptMsg.GetTransfer().GetMaxWindowSize()

	if len(e.Args) > 1 {
		farend = e.Args[1].(msgSender)
		logger.Infof("Session %d [window %d] is established as client", msg.GetCorrelationId(), windowSize)
	} else if farend = s.waitForResp[msg.GetCorrelationId()]; farend != nil {
		logger.Infof("Session %d [window %d] is established as server", msg.GetCorrelationId(), windowSize)
	} else {
		panic("Fail FSM: could not obtain farend in context")
	}

	s.session = &syncSession{
		farend:    farend,
		client:    s.client[msg.GetCorrelationId()],
		id:        msg.GetCorrelationId(),
		maxWindow: windowSize,
	}

}

func (s *syncCore) onStateSessionClosed(e *fsm.Event) {

	if s.session == nil {
		panic("Fail FSM: session not existed")
	}

	logger.Debugf("session %v is closed now", s.session)
	s.session = nil
}

func (s *syncCore) Push(sh msgSender, msg *pb.SimpleResp) error {
	return s.SendMessage(&pb.SyncMsg{
		Type:     pb.SyncMsg_SERVER_SIMPLE,
		Response: &pb.SyncMsg_Response{Simple: msg},
	}, sh)
}

func (s *syncCore) Request(sh msgSender, msg *pb.SimpleReq) (chan *pb.SyncMsg_Response, error) {

	chn := make(chan *pb.SyncMsg_Response, 1)

	return chn, s.SendMessage(&pb.SyncMsg{
		Type:    pb.SyncMsg_CLIENT_SIMPLE,
		Request: &pb.SyncMsg_Request{Simple: msg},
	}, sh, chn)
}

func (s *syncCore) OpenSession(sh msgSender, req *pb.OpenSession) (chan *pb.SyncMsg_Response, error) {

	chn := make(chan *pb.SyncMsg_Response, s.sessionOpt.maxWindow+1)

	//helping fill session detail
	if req.Transfer == nil {
		req.Transfer = &pb.TransferDetail{
			MaxWindowSize: uint32(s.sessionOpt.maxWindow),
		}
	}

	return chn, s.SendMessage(&pb.SyncMsg{
		Type:    pb.SyncMsg_CLIENT_SESSION_OPEN,
		Request: &pb.SyncMsg_Request{Handshake: req},
	}, sh, chn)
}

//the input channel will be closed, no matter success or not
func (s *syncCore) CancelRequest(rc chan *pb.SyncMsg_Response) error {
	defer close(rc)
	return filterError(s.Event(cancelRequest, rc))

}

func (s *syncCore) ResponseFailure(msgin *pb.SyncMsg, err error) error {
	return s.SendMessage(&pb.SyncMsg{
		Type:          pb.SyncMsg_SERVER_ERROR,
		CorrelationId: msgin.GetCorrelationId(),
		Response:      &pb.SyncMsg_Response{Err: &pb.RequestError{ErrorDetail: err.Error()}},
	})
}

func (s *syncCore) Response(msgin *pb.SyncMsg, resp *pb.SimpleResp) error {
	return s.SendMessage(&pb.SyncMsg{
		Type:          pb.SyncMsg_SERVER_SIMPLE,
		CorrelationId: msgin.GetCorrelationId(),
		Response:      &pb.SyncMsg_Response{Simple: resp},
	})
}

func (s *syncCore) AcceptSession(msgin *pb.SyncMsg, resp *pb.AcceptSession) error {

	logger.Debugf("Accept session request: %v", msgin)

	hs := msgin.GetRequest().GetHandshake()
	if hs == nil {
		return fmt.Errorf("handshake has no content")
	}

	if resp.Transfer == nil {
		//compare and set window size
		sessWin := uint32(s.sessionOpt.maxWindow)
		if sessWin > hs.Transfer.GetMaxWindowSize() {
			sessWin = hs.Transfer.GetMaxWindowSize()
		}

		resp.Transfer = &pb.TransferDetail{
			MaxWindowSize: sessWin,
		}
	}

	return s.SendMessage(&pb.SyncMsg{
		CorrelationId: msgin.GetCorrelationId(),
		Type:          pb.SyncMsg_SERVER_SESSION_ACCEPT,
		Response:      &pb.SyncMsg_Response{Handshake: resp},
	})
}

func (s *syncCore) SessionReq(msg *pb.TransferRequest) error {

	return s.SendMessage(&pb.SyncMsg{
		Type:    pb.SyncMsg_CLIENT_SESSION,
		Request: &pb.SyncMsg_Request{Session: msg},
	})
}

func (s *syncCore) SessionAck(ackedmsg *pb.TransferResponse, more *pb.TransferRequest) error {

	return s.SendMessage(&pb.SyncMsg{
		Type: pb.SyncMsg_CLIENT_SESSION_ACK,
		Request: &pb.SyncMsg_Request{
			Ack:     ackedmsg.GetSeq(),
			Session: more,
		},
	})
}

func (s *syncCore) SessionClose() error {

	return s.SendMessage(&pb.SyncMsg{Type: pb.SyncMsg_CLIENT_SESSION_CLOSE})
}

func (s *syncCore) SessionSend(msg *pb.TransferResponse) error {

	return s.SendMessage(&pb.SyncMsg{
		Type:     pb.SyncMsg_SERVER_SESSION,
		Response: &pb.SyncMsg_Response{Session: msg},
	})
}

func (s *syncCore) SessionFailure(reason string) error {

	return s.SendMessage(&pb.SyncMsg{
		Type: pb.SyncMsg_SERVER_SESSION_ERROR,
		Response: &pb.SyncMsg_Response{
			Err: &pb.RequestError{ErrorDetail: reason},
		},
	})
}

const fsm_state_idle = "idle"
const fsm_state_handshake = "handshake"
const fsm_state_session = "session"

func newFsmHandler() *syncCore {

	const idle = fsm_state_idle
	const handshake = fsm_state_handshake
	const session = fsm_state_session
	var allstates = []string{idle, handshake, session}
	core := &syncCore{
		client:      make(map[uint64]chan *pb.SyncMsg_Response),
		waitForResp: make(map[uint64]msgSender),
		msgId:       1,
	}

	evts := fsm.Events{
		{Name: pb.SyncMsg_CLIENT_SIMPLE.OnRecv(), Src: []string{idle}, Dst: idle},
		//response can be receive at any time so server is able to reply each request
		//out-of-order
		{Name: pb.SyncMsg_SERVER_SIMPLE.OnRecv(), Src: []string{idle}, Dst: idle},
		{Name: pb.SyncMsg_SERVER_SIMPLE.OnRecv(), Src: []string{handshake}, Dst: handshake},
		{Name: pb.SyncMsg_SERVER_SIMPLE.OnRecv(), Src: []string{session}, Dst: session},

		{Name: pb.SyncMsg_CLIENT_SIMPLE.OnSend(), Src: []string{idle}, Dst: idle},
		{Name: pb.SyncMsg_SERVER_SIMPLE.OnSend(), Src: []string{idle}, Dst: idle},
		{Name: pb.SyncMsg_SERVER_ERROR.OnSend(), Src: allstates, Dst: idle},
		{Name: pb.SyncMsg_SERVER_ERROR.OnRecv(), Src: allstates, Dst: idle},
		{Name: pb.SyncMsg_SERVER_SESSION_ERROR.OnSend(), Src: []string{session}, Dst: idle},
		{Name: pb.SyncMsg_SERVER_SESSION_ERROR.OnRecv(), Src: []string{session}, Dst: idle},

		//serving phase
		{Name: pb.SyncMsg_CLIENT_SESSION_OPEN.OnRecv(), Src: []string{idle}, Dst: handshake},
		{Name: pb.SyncMsg_CLIENT_SESSION_CLOSE.OnRecv(), Src: []string{session, idle}, Dst: idle},
		{Name: pb.SyncMsg_CLIENT_SESSION.OnRecv(), Src: []string{session}, Dst: session},
		{Name: pb.SyncMsg_CLIENT_SESSION_ACK.OnRecv(), Src: []string{session}, Dst: session},
		{Name: pb.SyncMsg_SERVER_SESSION_ACCEPT.OnSend(), Src: []string{handshake}, Dst: session},
		{Name: pb.SyncMsg_SERVER_SESSION.OnSend(), Src: []string{session}, Dst: session},

		//client phase
		{Name: pb.SyncMsg_CLIENT_SESSION_OPEN.OnSend(), Src: []string{idle}, Dst: handshake},
		{Name: pb.SyncMsg_CLIENT_SESSION.OnSend(), Src: []string{session}, Dst: session},
		{Name: pb.SyncMsg_CLIENT_SESSION_ACK.OnSend(), Src: []string{session}, Dst: session},
		{Name: pb.SyncMsg_CLIENT_SESSION_CLOSE.OnSend(), Src: []string{session}, Dst: idle},
		{Name: pb.SyncMsg_SERVER_SESSION_ACCEPT.OnRecv(), Src: []string{handshake}, Dst: session},
		{Name: pb.SyncMsg_SERVER_SESSION.OnRecv(), Src: []string{session}, Dst: session},

		//special: write notify, idle test, exit dead
		{Name: testIdle, Src: []string{idle}, Dst: idle},
		{Name: cancelRequest, Src: []string{idle}, Dst: idle},
		{Name: setWriteNotify, Src: []string{session}, Dst: session},
		{Name: "fatal", Src: allstates, Dst: "dead"},
	}

	callback := fsm.Callbacks{
		"enter_" + session: core.onStateSessionOpen,
		"leave_" + session: core.onStateSessionClosed,
		//before simple msg recv
		"before_" + pb.SyncMsg_SERVER_SIMPLE.OnRecv():         core.onBeforeRecvRespMessage,
		"before_" + pb.SyncMsg_SERVER_ERROR.OnRecv():          core.onBeforeRecvRespMessage,
		"before_" + pb.SyncMsg_SERVER_SESSION_ACCEPT.OnRecv(): core.onBeforeRecvRespMessage,
		"before_" + pb.SyncMsg_CLIENT_SESSION_OPEN.OnRecv():   core.onBeforeRecvRequestMessage,
		"before_" + pb.SyncMsg_CLIENT_SIMPLE.OnRecv():         core.onBeforeRecvRequestMessage,
		//session msg recv
		"before_" + pb.SyncMsg_SERVER_SESSION.OnRecv():       core.onBeforeRecvSessionMessage,
		"before_" + pb.SyncMsg_SERVER_SESSION_ERROR.OnRecv(): core.onBeforeRecvSessionMessage,
		"before_" + pb.SyncMsg_CLIENT_SESSION.OnRecv():       core.onBeforeRecvSessionMessage,
		"before_" + pb.SyncMsg_CLIENT_SESSION_ACK.OnRecv():   core.onBeforeRecvSessionMessage,
		"before_" + pb.SyncMsg_CLIENT_SESSION_CLOSE.OnRecv(): core.onBeforeRecvSessionMessage,
		//session msg sent
		"before_" + pb.SyncMsg_CLIENT_SESSION.OnSend():       core.onBeforeSendSessionMessage,
		"before_" + pb.SyncMsg_CLIENT_SESSION_ACK.OnSend():   core.onBeforeSendSessionMessage,
		"before_" + pb.SyncMsg_CLIENT_SESSION_CLOSE.OnSend(): core.onBeforeSendSessionMessage,
		"before_" + pb.SyncMsg_SERVER_SESSION_ERROR.OnSend(): core.onBeforeSendSessionMessage,
		"before_" + pb.SyncMsg_SERVER_SESSION.OnSend():       core.onBeforeSendTransport,
		//recv simple msg handling
		"after_" + pb.SyncMsg_SERVER_SESSION_ACCEPT.OnRecv(): core.onRecvRespMessage,
		"after_" + pb.SyncMsg_SERVER_SIMPLE.OnRecv():         core.onRecvRespMessage,
		"after_" + pb.SyncMsg_SERVER_ERROR.OnRecv():          core.onRecvRespMessage,
		//recv session handling
		"after_" + pb.SyncMsg_CLIENT_SESSION_ACK.OnRecv(): core.onRecvAcking,
		"after_" + pb.SyncMsg_SERVER_SESSION.OnRecv():     core.onRecvTransport,

		//inner methods
		"before_" + setWriteNotify: core.onSettingWriteNotify,
		"before_" + testIdle:       core.onIdleTesting,
		"before_" + cancelRequest:  core.onCancelRequest,
	}
	//also add handler for every send event
	for id, _ := range pb.SyncMsg_Type_name {
		callback["after_"+pb.SyncMsg_Type(id).OnSend()] = core.onSendMessage
	}

	core.FSM = fsm.NewFSM(idle, evts, callback)
	return core

}
