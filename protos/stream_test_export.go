package protos

import (
	"fmt"
	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"
)

func HandleDummyWrite(ctx context.Context, h *StreamHandler) error {

	select {
	case <-ctx.Done():
		return ctx.Err()
	case m := <-h.writeQueue:
		//"swallow" the message silently
		h.handler.BeforeSendMessage(m)
	}

	return nil
}

func HandleDummyComm(ctx context.Context, hfrom *StreamHandler, hto *StreamHandler) error {

	select {
	case <-ctx.Done():
		return ctx.Err()
	case m := <-hfrom.writeQueue:
		if err := hfrom.handler.BeforeSendMessage(m); err == nil {
			err = hto.handler.HandleMessage(hto, m)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	return nil
}

func HandleDummyBiComm(ctx context.Context, h1 *StreamHandler, h2 *StreamHandler) error {
	return HandleDummyBiComm2(ctx, h1, h2, nil)
}

//integrate the bi-direction comm into one func, but may lead to unexpected dead lock
func HandleDummyBiComm2(ctx context.Context, h1 *StreamHandler, h2 *StreamHandler, copyHelper func(proto.Message) proto.Message) error {

	var err error

	select {
	case <-ctx.Done():
		return ctx.Err()
	case m := <-h1.writeQueue:
		err = h1.handler.BeforeSendMessage(m)
		if copyHelper != nil {
			m = copyHelper(m)
		}
		if err == nil {
			err = h2.handler.HandleMessage(h2, m)
		}
	case m := <-h2.writeQueue:
		err = h2.handler.BeforeSendMessage(m)
		if copyHelper != nil {
			m = copyHelper(m)
		}
		if err == nil {
			err = h1.handler.HandleMessage(h1, m)
		}
	}

	return err
}

type SimuPeerStub struct {
	id *PeerID
	*StreamStub
}

func (s1 *SimuPeerStub) ConnectTo(ctx context.Context, s2 *SimuPeerStub) (err error, traffic func() error) {
	return s1.ConnectTo2(ctx, s2, nil)
}

func dummycphelper(m proto.Message) proto.Message { return m }

//s1 is act as client and s2 as service, create bi-direction comm between two handlers
func (s1 *SimuPeerStub) ConnectTo2(ctx context.Context, s2 *SimuPeerStub, msgHelper func() proto.Message) (err error, traffic func() error) {

	var hi StreamHandlerImpl
	hi, _ = s1.NewStreamHandlerImpl(s2.id, s1.StreamStub, true)
	s1h := newStreamHandler(hi)
	err = s1.registerHandler(s1h, s2.id)
	if err != nil {
		err = fmt.Errorf("reg s1 fail: %s", err)
		return
	}

	hi, _ = s2.NewStreamHandlerImpl(s1.id, s2.StreamStub, false)
	s2h := newStreamHandler(hi)
	err = s2.registerHandler(s2h, s1.id)
	if err != nil {
		err = fmt.Errorf("reg s2 fail: %s", err)
		return
	}

	var cphelper func(proto.Message) proto.Message
	if msgHelper == nil {
		cphelper = dummycphelper
	} else {
		cphelper = func(m proto.Message) proto.Message {
			bytes, err := proto.Marshal(m)
			if err != nil {
				panic(err)
			}

			newM := msgHelper()
			err = proto.Unmarshal(bytes, newM)
			if err != nil {
				panic(err)
			}
			return newM
		}
	}

	traffic = func() error { return HandleDummyBiComm2(ctx, s1h, s2h, cphelper) }

	return
}

func (s1 *SimuPeerStub) AddDummyPeer(id string, asCli bool) error {
	farId := &PeerID{Name: id}
	var hi StreamHandlerImpl
	hi, _ = s1.NewStreamHandlerImpl(farId, s1.StreamStub, asCli)
	sh := newStreamHandler(hi)
	return s1.registerHandler(sh, farId)
}

func NewSimuPeerStub(id string, ss *StreamStub) *SimuPeerStub {

	return &SimuPeerStub{
		id:         &PeerID{Name: id},
		StreamStub: ss,
	}

}

func NewSimuPeerStub2(ss *StreamStub) *SimuPeerStub {

	return &SimuPeerStub{
		id:         ss.localID,
		StreamStub: ss,
	}

}
