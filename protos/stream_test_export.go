package protos

import (
	"fmt"
	_ "github.com/golang/protobuf/proto"
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

//integrate the bi-direction comm into one func, but may lead to unexpected dead lock
func HandleDummyBiComm(ctx context.Context, h1 *StreamHandler, h2 *StreamHandler) error {

	var err error

	select {
	case <-ctx.Done():
		return ctx.Err()
	case m := <-h1.writeQueue:
		err = h1.handler.BeforeSendMessage(m)
		if err == nil {
			err = h2.handler.HandleMessage(h2, m)
		}
	case m := <-h2.writeQueue:
		err = h2.handler.BeforeSendMessage(m)
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

//s1 is act as client and s2 as service, create bi-direction comm between two handlers
func (s1 *SimuPeerStub) ConnectTo(ctx context.Context, s2 *SimuPeerStub) (err error, traffic func() error) {

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

	traffic = func() error { return HandleDummyBiComm(ctx, s1h, s2h) }

	return
}

func NewSimuPeerStub(id string, ss *StreamStub) *SimuPeerStub {

	return &SimuPeerStub{
		id:         &PeerID{id},
		StreamStub: ss,
	}

}
