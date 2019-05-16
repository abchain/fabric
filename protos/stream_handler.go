package protos

import (
	"fmt"
	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"io"
	"math/rand"
	"sync"
)

/*

  a streamhandler factory include two methods:

  1. Stream creator: generate a stream from given connection
  2. Handler creator: generate a handler which is binded to specified peer, with a bool parameter indicate
     it should act as client or server (may be omitted in a bi-directory session)

*/
type StreamHandlerFactory interface {
	NewClientStream(*grpc.ClientConn) (grpc.ClientStream, error)
	NewStreamHandlerImpl(*PeerID, *StreamStub, bool) (StreamHandlerImpl, error)
}

/*

  each streamhandler implement exposed following methods for working in a streaming, it supposed message with
  certain type is transmitted in the stream and each end handle this message by a streamhandler implement:

  Tag: providing a string tag for the implement
  EnableLoss: indicate the message transmitted in stream can be dropped for any reason (send buffer full, bad
			  linking, deliberately omitted by the other side ...)
  NewMessage: provided a prototype object of the transamitted message for receiving and handling later in a HandleMessage call
  HandleMessage: handling a message received from the other side. The object was allocated before in a NewMessage call and
			  the wire data was parsed and put into it
  BeforeSendMessage: when a message is ready to send to the other side (by calling SendMessage in a StreamHandler), this method
			  is called to give handler a last chance for dropping (by return a non-nil error) the message or do any statistics
			  jobs. Method MAY be called in different thread so you must protect your data from any racing
  OnWriteError: method is called if any error raised in sending message
  Stop: when the stream is broken this method is called and no more calling will be made on this handler

  *** All calling to the methods (except for BeforeSendMessage) are ensured being raised in the same thread ***

*/
type StreamHandlerImpl interface {
	Tag() string
	EnableLoss() bool
	NewMessage() proto.Message
	HandleMessage(*StreamHandler, proto.Message) error
	BeforeSendMessage(proto.Message) error
	OnWriteError(error)
	Stop()
}

type StreamHandler struct {
	sync.RWMutex
	handler     StreamHandlerImpl
	name        string
	enableLoss  bool
	writeQueue  chan proto.Message
	writeExited chan error
}

const (
	defaultWriteBuffer = 32
)

func newStreamHandler(impl StreamHandlerImpl) *StreamHandler {
	return &StreamHandler{
		handler:     impl,
		enableLoss:  impl.EnableLoss(),
		writeQueue:  make(chan proto.Message, defaultWriteBuffer),
		writeExited: make(chan error),
	}
}

func (h *StreamHandler) GetName() string {
	if h == nil {
		return ""
	}
	return h.name
}

func (h *StreamHandler) Impl() StreamHandlerImpl {
	if h == nil {
		return nil
	}
	return h.handler
}

//sending a message to far-end, this method is thread-safe
func (h *StreamHandler) SendMessage(m proto.Message) error {

	if h == nil {
		return fmt.Errorf("Stream is not exist")
	}

	h.RLock()
	defer h.RUnlock()
	if h.writeQueue == nil {
		return fmt.Errorf("Streamhandler %s has been killed", h.handler.Tag())
	}

	select {
	case h.writeQueue <- m:
		return nil
	default:
		if !h.enableLoss {
			return fmt.Errorf("Streamhandler %s's write channel full, rejecting", h.handler.Tag())
		}
	}

	return nil
}

func (h *StreamHandler) handleWrite(stream grpc.Stream) {

	var m proto.Message
	for m = range h.writeQueue {

		err := h.handler.BeforeSendMessage(m)
		if err == nil {
			err = stream.SendMsg(m)
			if err != nil {
				h.writeExited <- err
				return
			}
		}
	}

	h.writeExited <- nil

}

func (h *StreamHandler) endHandler() {
	h.Lock()
	defer h.Unlock()
	close(h.writeQueue)
	h.writeQueue = nil

}

func (h *StreamHandler) handleStream(stream grpc.Stream) error {

	//dispatch write goroutine
	go h.handleWrite(stream)

	defer h.handler.Stop()
	defer h.endHandler()

	for {
		in := h.handler.NewMessage()
		err := stream.RecvMsg(in)
		if err == io.EOF {
			return fmt.Errorf("received EOF")
		} else if err != nil {
			return err
		}

		err = h.handler.HandleMessage(h, in)
		if err != nil {
			//don't need to call onError again
			return err
		}

		select {
		case err := <-h.writeExited:
			//if the writting goroutine exit unexpectedly, we resume it ...
			h.handler.OnWriteError(err)
			go h.handleWrite(stream)
		default:
			//or everything is ok
		}
	}

	return nil
}

type shandlerMap struct {
	sync.Mutex
	m map[PeerID]*StreamHandler
}

//a default implement, use two goroutine for read and write simultaneously
type StreamStub struct {
	StreamHandlerFactory
	handlerMap *shandlerMap
	localID    *PeerID
}

func NewStreamStub(factory StreamHandlerFactory, peerid *PeerID) *StreamStub {

	return &StreamStub{
		StreamHandlerFactory: factory,
		handlerMap: &shandlerMap{
			m: make(map[PeerID]*StreamHandler),
		},
		localID: peerid,
	}
}

func (s *StreamStub) registerHandler(h *StreamHandler, peerid *PeerID) error {

	s.handlerMap.Lock()
	defer s.handlerMap.Unlock()

	if _, ok := s.handlerMap.m[*peerid]; ok {
		// Duplicate,
		return fmt.Errorf("Duplicate handler for peer %s", peerid)
	}
	h.name = peerid.Name
	s.handlerMap.m[*peerid] = h

	logger.Debugf("register handler for far-end peer %s", peerid.GetName())
	return nil
}

func (s *StreamStub) unRegisterHandler(peerid *PeerID) {
	s.handlerMap.Lock()
	defer s.handlerMap.Unlock()
	if _, ok := s.handlerMap.m[*peerid]; ok {
		logger.Debugf("unregister handler for far-end peer %s", peerid.GetName())
		delete(s.handlerMap.m, *peerid)
	}
}

func shuffle(in []*PeerID) []*PeerID {

	//ad-hoc random shuffle incoming peers
	l := len(in)
	if l < 2 {
		return in
	}

	var swapTo int
	for i, id := range in[:l-1] {
		swapTo = rand.Intn(l-i) + i
		if swapTo != i {
			in[i] = in[swapTo]
			in[swapTo] = id
		}
	}

	return in
}

type PickedStreamHandler struct {
	Id        *PeerID
	WorkError error
	*StreamHandler
}

func (s *StreamStub) deliverHandlers(ctx context.Context, peerids []*PeerID, out chan *PickedStreamHandler) {

	for _, id := range peerids {
		s.handlerMap.Lock()
		h, ok := s.handlerMap.m[*id]
		s.handlerMap.Unlock()
		if ok {
			select {
			case out <- &PickedStreamHandler{id, nil, h}:
			case <-ctx.Done():
				return
			}
		}
	}

	close(out)
	return
}

func (s *StreamStub) HandlerCount() int {
	s.handlerMap.Lock()
	defer s.handlerMap.Unlock()

	return len(s.handlerMap.m)
}

//pick handlers by random from given peerids, whether candidates to be choosed is decided
//at each time before it was delivered. This is oftenn used by a range statement
func (s *StreamStub) OverHandlers(ctx context.Context, peerids []*PeerID) chan *PickedStreamHandler {
	peerids = shuffle(peerids)
	outchan := make(chan *PickedStreamHandler)
	go s.deliverHandlers(ctx, peerids, outchan)
	return outchan
}

//like OverHandlers, but pick all handlers which streamsub owns in callingtime
func (s *StreamStub) OverAllHandlers(ctx context.Context) chan *PickedStreamHandler {
	s.handlerMap.Lock()

	ids := make([]*PeerID, 0, len(s.handlerMap.m))
	for k, _ := range s.handlerMap.m {
		//CAUTION: &k is point to same address
		kcopy := k
		ids = append(ids, &kcopy)
	}

	s.handlerMap.Unlock()

	return s.OverHandlers(ctx, ids)
}

//an entry mimic that in statetransfer package
func (s *StreamStub) TryOverHandlers(peerids []*PeerID,
	do func(*PickedStreamHandler) error) {

	ctx, cancel := context.WithCancel(context.Background())

	for p := range s.OverHandlers(ctx, peerids) {
		err := do(p)
		if err == nil {
			cancel()
			break
		} else {
			logger.Warningf("tryOverHandlers: loop error from %v : %s", p.Id, err)
		}
	}
}

func (s *StreamStub) PickHandler(peerid *PeerID) *StreamHandler {

	strms := s.PickHandlers([]*PeerID{peerid})
	if len(strms) < 1 {
		return nil
	}

	return strms[0]
}

func (s *StreamStub) PickHandlers(peerids []*PeerID) []*StreamHandler {

	s.handlerMap.Lock()
	defer s.handlerMap.Unlock()

	ret := make([]*StreamHandler, 0, len(peerids))

	for _, id := range peerids {
		h, ok := s.handlerMap.m[*id]
		if ok {
			ret = append(ret, h)
		}
	}

	return ret
}

func (s *StreamStub) Broadcast(ctx context.Context, m proto.Message) (err error,
	ret []*PickedStreamHandler) {

	var bcWG sync.WaitGroup
	retchan := make(chan *PickedStreamHandler)

	//we always exhault all handlers so just use background context
	for h := range s.OverAllHandlers(context.Background()) {
		bcWG.Add(1)
		go func(h *PickedStreamHandler, retchan chan *PickedStreamHandler) {
			defer bcWG.Done()
			h.WorkError = h.SendMessage(m)
			retchan <- h
		}(h, retchan)
	}

	wchan := make(chan error)
	go func(w chan error) {
		bcWG.Wait()
		w <- nil
	}(wchan)

	for {
		select {
		case r := <-retchan:
			ret = append(ret, r)
		case <-wchan: //all done
			return nil, ret
		case <-ctx.Done():
			return fmt.Errorf("Broadcasting is canceled"), ret
		}
	}

}

func (s *StreamStub) HandleClient(conn *grpc.ClientConn, remotePeerid *PeerID) (error, func()) {
	clistrm, err := s.NewClientStream(conn)

	if err != nil {
		return err, func() {}
	}

	errf := func() { clistrm.CloseSend() }

	// handshake: send my id to remote peer
	err = clistrm.SendMsg(s.localID)
	if err != nil {
		return err, errf
	}

	// himpl is stateSyncHandler or GossipHandlerImpl
	himpl, err := s.NewStreamHandlerImpl(remotePeerid, s, true)

	if err != nil {
		return err, errf
	}

	streamHandler := newStreamHandler(himpl)

	err = s.registerHandler(streamHandler, remotePeerid)
	if err != nil {
		return err, errf
	}

	return nil, func() {
		defer clistrm.CloseSend()
		defer s.unRegisterHandler(remotePeerid)
		streamHandler.handleStream(clistrm)
	}

}

func (s *StreamStub) HandleServer(stream grpc.ServerStream) error {

	remotePeerid := new(PeerID)

	// handshake: receive remote peer id
	err := stream.RecvMsg(remotePeerid)
	if err != nil {
		return err
	}

	himpl, err := s.NewStreamHandlerImpl(remotePeerid, s, false)

	if err != nil {
		return err
	}

	streamHandler := newStreamHandler(himpl)

	err = s.registerHandler(streamHandler, remotePeerid)
	if err != nil {
		return err
	}

	defer s.unRegisterHandler(remotePeerid)
	return streamHandler.handleStream(stream)
}
