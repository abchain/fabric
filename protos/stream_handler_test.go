package protos

import (
	"fmt"
	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"
	"testing"
	"time"
)

type dummyError struct {
	error
}

type dummyHandler struct {
}

func (*dummyHandler) Stop() {}

func (h *dummyHandler) Tag() string { return "Dummy" }

func (h *dummyHandler) EnableLoss() bool { return true }

func (h *dummyHandler) NewMessage() proto.Message { return nil }

func (h *dummyHandler) HandleMessage(_ *StreamHandler, m proto.Message) error {
	return &dummyError{fmt.Errorf("No implement")}
}

func (h *dummyHandler) BeforeSendMessage(proto.Message) error {
	return nil
}
func (h *dummyHandler) OnWriteError(e error) {}

type verifySet map[string]bool

func newVSet(name []string) verifySet {

	vset := make(map[string]bool)

	for _, n := range name {
		vset[n] = false
	}

	return verifySet(vset)
}

func (v verifySet) allPass(t *testing.T) {

	for n, ok := range v {
		if !ok {
			t.Fatal("Verify set have fail item", n)
		}
	}
}

func (v verifySet) excludeFail() (ret []string) {

	for n, ok := range v {
		if !ok {
			ret = append(ret, n)
		}
	}

	for _, n := range ret {
		delete(v, n)
	}

	return
}

func toPeerId(name []string) (ret []*PeerID) {
	for _, n := range name {
		ret = append(ret, &PeerID{Name: n})
	}

	return
}

func Test_StreamHub_Base(t *testing.T) {

	tstub := NewStreamStub(nil, &PeerID{Name: "peer0"})

	h1 := newStreamHandler(&dummyHandler{})

	tstub.registerHandler(h1, &PeerID{Name: "peer1"})

	if strm := tstub.PickHandler(&PeerID{Name: "peer1"}); strm == nil {
		t.Fatal("1. Can not pick registered handler")
	}

	if strm := tstub.PickHandler(&PeerID{Name: "peer2"}); strm != nil {
		t.Fatal("1. pick unregistered handler")
	}

	h2 := newStreamHandler(&dummyHandler{})
	tstub.registerHandler(h2, &PeerID{Name: "peer2"})

	if strm := tstub.PickHandler(&PeerID{Name: "peer1"}); strm == nil {
		t.Fatal("2. Can not pick registered handler peer1")
	}

	if strm := tstub.PickHandler(&PeerID{Name: "peer2"}); strm == nil {
		t.Fatal("2. Can not pick registered handler peer2")
	}

	if strms := tstub.PickHandlers([]*PeerID{&PeerID{Name: "peer1"}, &PeerID{Name: "peer3"}, &PeerID{Name: "peer4"}}); len(strms) != 1 {
		t.Fatal("2. Can not pick expected registered handlers", strms)
	} else {
		if strms[0] != h1 {
			t.Fatal("2. picked unexpected handler")
		}
	}

	tstub.unRegisterHandler(&PeerID{Name: "peer2"})

	if strm := tstub.PickHandler(&PeerID{Name: "peer1"}); strm == nil {
		t.Fatal("3. Can not pick registered handler")
	}

	if strm := tstub.PickHandler(&PeerID{Name: "peer2"}); strm != nil {
		t.Fatal("3. pick unregistered handler")
	}
}

func Test_StreamHub_OverHandler(t *testing.T) {

	tstub := NewStreamStub(nil, &PeerID{Name: "peer0"})

	peerNames := []string{"peer1", "peer2", "peer3", "peer4", "peer5", "peer6", "peer7", "peer8"}

	wctx, endworks := context.WithCancel(context.Background())
	defer endworks()

	//populate streamhub with dummy handler
	for _, n := range peerNames {
		h := newStreamHandler(&dummyHandler{})
		tstub.registerHandler(h, &PeerID{Name: n})
		go HandleDummyWrite(wctx, h)
	}

	testName1 := []string{"peer3", "peer4", "peer6", "peer8"}

	vset1 := newVSet(testName1)

	//work on all
	for p := range tstub.OverHandlers(wctx, toPeerId(testName1)) {

		vset1[p.Id.GetName()] = true
	}

	vset1.allPass(t)

	//cancel part
	counter := len(testName1) - 1

	ctx, cancel := context.WithCancel(wctx)
	vset2 := newVSet(testName1)

	for p := range tstub.OverHandlers(ctx, toPeerId(testName1)) {

		vset2[p.Id.GetName()] = true
		counter--
		if counter == 0 {
			break
		}
	}

	cancel()

	failSet2 := vset2.excludeFail()
	if len(failSet2) != 1 {
		t.Fatal("Obtain unexpected fail set", failSet2)
	}

	vset2.allPass(t)

	//wrong peer

	testName3 := []string{"peer3", "peer4", "peer6", "wrongPeer"}

	vset3 := newVSet(testName3)

	for p := range tstub.OverHandlers(wctx, toPeerId(testName3)) {

		vset3[p.Id.GetName()] = true
	}

	failSet3 := vset3.excludeFail()
	if len(failSet3) != 1 && failSet3[0] != "wrongPeer" {
		t.Fatal("Obtain unexpected fail set", failSet3)
	}

	vset3.allPass(t)

	//on all
	vset4 := newVSet(peerNames)
	for p := range tstub.OverAllHandlers(wctx) {

		vset4[p.Id.GetName()] = true
	}

	vset4.allPass(t)

	//unregister one on "for on all"
	vset5 := newVSet(peerNames)
	counter = len(peerNames) / 2
	for p := range tstub.OverAllHandlers(wctx) {

		vset5[p.Id.GetName()] = true
		counter--
		if counter == 0 {
			notTouch := vset5.excludeFail()
			tstub.unRegisterHandler(&PeerID{Name: notTouch[0]})
		}
	}

	vset5.allPass(t)
	if len(vset5)+1 != len(peerNames) {
		t.Fatal("Unexpected vset", vset5)
	}

}

func ensureOneDummyWrite(ctx context.Context, h *StreamHandler) error {

	if err := HandleDummyWrite(ctx, h); err != nil {
		return err
	}

	ensureOneDummyWrite(ctx, h)
	return nil
}

func Test_StreamHub_Broadcast(t *testing.T) {

	tstub := NewStreamStub(nil, &PeerID{Name: "peer0"})

	peerNames := []string{"peer1", "peer2", "peer3", "peer4", "peer5", "peer6", "peer7", "peer8"}

	wctx, endworks := context.WithCancel(context.Background())
	defer endworks()

	//populate streamhub with dummy handler
	for _, n := range peerNames {
		h := newStreamHandler(&dummyHandler{})
		tstub.registerHandler(h, &PeerID{Name: n})
		go func() {
			if err := ensureOneDummyWrite(wctx, h); err != nil {
				t.Fatal("Dummy write fail", err)
			}
		}()
	}

	//test one handler for negative sampe
	go func() {
		h := newStreamHandler(&dummyHandler{})
		if err := ensureOneDummyWrite(wctx, h); err == nil {
			t.Fatal("ensure dummy write func is error")
		}
	}()

	err, ret := tstub.Broadcast(wctx, &PeerID{})

	if err != nil {
		t.Fatal("Broadcast fail", err)
	}

	if len(ret) != len(peerNames) {
		t.Fatal("Unexpected result set", ret)
	}

	for _, r := range ret {
		if r.WorkError != nil {
			t.Fatal("Unexpected result set in working error", r.WorkError, ret)
		}
	}

	time.Sleep(time.Second * 2)
}
