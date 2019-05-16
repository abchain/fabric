package sync

import (
	"fmt"
	"github.com/abchain/fabric/core/ledger/testutil"
	pb "github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/proto"
	"testing"
)

//simulate a marshal/unmarshal process
func transferSimulate(msg *pb.SyncMsg) *pb.SyncMsg {

	b, err := proto.Marshal(msg)
	if err != nil {
		panic(err.Error())
	}

	cmsg := new(pb.SyncMsg)

	err = proto.Unmarshal(b, cmsg)
	if err != nil {
		panic(err.Error())
	}

	return cmsg

}

//all process is in the same thread so it is more easy for debugging,
//but it may lead to possible deadlock
type asDebugReceiver struct {
	base *syncCore
	to   *syncCore
}

func (s asDebugReceiver) SendMessage(m proto.Message) error {
	s.base.HandleMessage(transferSimulate(m.(*pb.SyncMsg)), asDebugReceiver{s.to, s.base})
	return nil
}

type asReceiver struct {
	base *syncCore
	to   *syncCore
	msgC chan proto.Message
}

func NewReceiver(base, to *syncCore, another *asReceiver) (*asReceiver, func()) {
	ret := &asReceiver{
		base, to, make(chan proto.Message, 1),
	}

	defer func() {
		go func() {
			for m := range ret.msgC {
				base.HandleMessage(transferSimulate(m.(*pb.SyncMsg)), another)
			}
		}()
	}()

	var endF func()
	if another == nil {
		another, endF = NewReceiver(to, base, ret)
		return ret, func() {
			endF()
			close(ret.msgC)
		}
	} else {
		endF = func() {
			close(ret.msgC)
		}
	}

	return ret, endF
}

func (s asReceiver) SendMessage(m proto.Message) error {
	s.msgC <- m
	return nil
}

func TestCoreBasic(t *testing.T) {
	srv := make(chan *pb.SyncMsg_Response, 10)

	core1, core2 := newFsmHandler(), newFsmHandler()

	core1.Init(srv, nil)

	s := &pb.LedgerState{
		Height: 100,
	}

	//test push
	core2.Push(asDebugReceiver{core1, core2}, &pb.SimpleResp{Resp: &pb.SimpleResp_State{State: s}})

	select {
	case msg := <-srv:
		data := msg.GetSimple()
		testutil.AssertNotNil(t, data)
		testutil.AssertEquals(t, data.GetState().GetHeight(), uint64(100))

	default:
		t.Fatal("No message is received")
	}

	testutil.AssertEquals(t, len(core2.waitForResp), 0)
	testutil.AssertEquals(t, len(core1.client), 1)
}

func TestCoreSimpleQuery(t *testing.T) {
	core1, core2 := newFsmHandler(), newFsmHandler()
	srv := make(chan *pb.SyncMsg, 10)

	core1.Init(nil, srv)

	r := &pb.SimpleReq_Tx{Tx: &pb.TxQuery{Txid: []string{"aa", "bb", "cc"}}}

	resp, err := core2.Request(asDebugReceiver{core1, core2}, &pb.SimpleReq{Req: r})
	testutil.AssertNoError(t, err, "request")
	testutil.AssertEquals(t, core2.IsIdle(), false)

	select {
	case msg := <-srv:
		data := msg.GetRequest().GetSimple()
		testutil.AssertNotNil(t, data)
		testutil.AssertEquals(t, len(data.GetTx().GetTxid()), 3)
		testutil.AssertEquals(t, data.GetTx().GetTxid()[0], "aa")

		testutil.AssertEquals(t, msg.GetCorrelationId(), uint64(1))
		testutil.AssertEquals(t, len(core1.waitForResp), 1)
		testutil.AssertEquals(t, len(core2.client), 1)
		testutil.AssertEquals(t, core2.msgId, uint64(2))

		core1.Response(msg, &pb.SimpleResp{Resp: &pb.SimpleResp_Tx{Tx: &pb.TransactionBlock{}}})
		testutil.AssertEquals(t, core2.IsIdle(), true)

	default:
		t.Fatal("No request is received")
	}

	select {
	case msg := <-resp:
		data := msg.GetSimple()
		testutil.AssertNotNil(t, data)
		testutil.AssertNotNil(t, data.GetTx())

	default:
		t.Fatal("No reponse is received")
	}

	testutil.AssertEquals(t, len(core1.waitForResp), 0)
	testutil.AssertEquals(t, len(core2.client), 0)

	core2.sessionOpt.maxPending = 2
	resp, err = core2.Request(asDebugReceiver{core1, core2}, &pb.SimpleReq{Req: r})
	testutil.AssertNoError(t, err, "request again")
	testutil.AssertEquals(t, core2.IsIdle(), true)

	select {
	case msg := <-srv:
		testutil.AssertEquals(t, msg.GetCorrelationId(), uint64(2))
		testutil.AssertEquals(t, core2.msgId, uint64(3))

		core1.ResponseFailure(msg, fmt.Errorf("just fail"))

	default:
		t.Fatal("No request is received")
	}

	select {
	case msg := <-resp:
		data := msg.GetErr()
		testutil.AssertNotNil(t, data)

	default:
		t.Fatal("No reponse is received")
	}

	testutil.AssertEquals(t, len(core1.waitForResp), 0)
	testutil.AssertEquals(t, len(core2.client), 0)

}

func extractMsg(t *testing.T, srv chan *pb.SyncMsg) *pb.SyncMsg {
	select {
	case msg := <-srv:
		return msg
	default:
		t.Fatal("No request is received")
	}
	return nil
}

func extractResp(t *testing.T, r <-chan *pb.SyncMsg_Response) *pb.SyncMsg_Response {
	select {
	case msg := <-r:
		return msg
	default:
		t.Fatal("No response is received")
	}
	return nil
}

func TestCoreOutOfOrderSimpleQuery(t *testing.T) {
	core1, core2 := newFsmHandler(), newFsmHandler()
	srv := make(chan *pb.SyncMsg, 10)

	core1.Init(nil, srv)

	r := &pb.SimpleReq_Tx{Tx: &pb.TxQuery{Txid: []string{"aa"}}}

	resp1, err := core2.Request(asDebugReceiver{core1, core2}, &pb.SimpleReq{Req: r})
	testutil.AssertNoError(t, err, "request 1")

	r = &pb.SimpleReq_Tx{Tx: &pb.TxQuery{Txid: []string{"aa", "bb", "cc"}}}
	resp2, err := core2.Request(asDebugReceiver{core1, core2}, &pb.SimpleReq{Req: r})
	testutil.AssertNoError(t, err, "request 2")

	msg1 := extractMsg(t, srv)
	msg2 := extractMsg(t, srv)

	testutil.AssertNotNil(t, msg1.GetRequest().GetSimple())
	testutil.AssertNotNil(t, msg2.GetRequest().GetSimple())
	testutil.AssertEquals(t, len(msg2.GetRequest().GetSimple().GetTx().GetTxid()), 3)

	testutil.AssertEquals(t, core2.msgId, uint64(3))
	testutil.AssertEquals(t, len(core1.waitForResp), 2)

	core1.Response(msg2, &pb.SimpleResp{Resp: &pb.SimpleResp_Tx{Tx: &pb.TransactionBlock{}}})
	resp := extractResp(t, resp2)
	testutil.AssertNotNil(t, resp.GetSimple().GetTx())

	select {
	case <-resp1:
		t.Fatal("unexpected resp")
	default:
	}

	core1.ResponseFailure(msg1, fmt.Errorf("just fail"))
	resp = extractResp(t, resp1)
	testutil.AssertNil(t, resp.GetSimple().GetTx())
	testutil.AssertNotNil(t, resp.GetErr())

	testutil.AssertEquals(t, len(core1.waitForResp), 0)
	testutil.AssertEquals(t, len(core2.client), 0)

}

func TestCoreSessionHandshake(t *testing.T) {

	core1, core2 := newFsmHandler(), newFsmHandler()
	srv := make(chan *pb.SyncMsg, 10)

	core1.Init(nil, srv)
	core2.Init(nil, srv)

	r := &pb.OpenSession{For: &pb.OpenSession_BlocksOrDelta{BlocksOrDelta: 10}}

	//conn1
	respc, err := core2.OpenSession(asDebugReceiver{core1, core2}, r)
	testutil.AssertNil(t, core2.session)
	testutil.AssertNil(t, core1.session)

	msg := extractMsg(t, srv)
	hs := msg.GetRequest().GetHandshake()
	testutil.AssertNotNil(t, hs)

	err = core1.AcceptSession(msg, &pb.AcceptSession{})
	testutil.AssertNoError(t, err, "handshake")

	testutil.AssertNotNil(t, core2.session)
	testutil.AssertNotNil(t, core1.session)
	testutil.AssertEquals(t, core1.Current(), fsm_state_session)
	testutil.AssertEquals(t, core2.Current(), fsm_state_session)

	resp := extractResp(t, respc)

	testutil.AssertNotNil(t, resp.GetHandshake())
	testutil.AssertEquals(t, len(core1.waitForResp), 0)
	testutil.AssertEquals(t, len(core2.client), 1)

	err = core2.SessionClose()
	testutil.AssertNoError(t, err, "session close")
	testutil.AssertNil(t, core2.session)
	testutil.AssertNil(t, core1.session)
	testutil.AssertEquals(t, core1.Current(), fsm_state_idle)
	testutil.AssertEquals(t, core2.Current(), fsm_state_idle)

	msg = extractMsg(t, srv)
	testutil.AssertEquals(t, msg.GetType(), pb.SyncMsg_CLIENT_SESSION_CLOSE)

	//conn2
	connTest := func(core1 *syncCore, core2 *syncCore, title string) {
		r.Transfer = nil
		respc, err := core2.OpenSession(asDebugReceiver{core1, core2}, r)
		msg := extractMsg(t, srv)
		err = core1.AcceptSession(msg, &pb.AcceptSession{})
		testutil.AssertNoError(t, err, "handshake"+title)

		testutil.AssertNotNil(t, core2.session)
		testutil.AssertNotNil(t, core1.session)
		testutil.AssertEquals(t, core1.Current(), fsm_state_session)
		testutil.AssertEquals(t, core2.Current(), fsm_state_session)

		extractResp(t, respc)
		testutil.AssertEquals(t, len(core1.waitForResp), 0)
		testutil.AssertEquals(t, len(core2.client), 1)

		err = core2.SessionClose()
		testutil.AssertNoError(t, err, "session close"+title)
		testutil.AssertNil(t, core2.session)
		testutil.AssertNil(t, core1.session)
		testutil.AssertEquals(t, core1.Current(), fsm_state_idle)
		testutil.AssertEquals(t, core2.Current(), fsm_state_idle)

		msg = extractMsg(t, srv)
		testutil.AssertEquals(t, msg.GetType(), pb.SyncMsg_CLIENT_SESSION_CLOSE)
	}
	connTest(core1, core2, " 2")

	//conn3 (fail it)
	r.Transfer = nil
	respc, err = core2.OpenSession(asDebugReceiver{core1, core2}, r)
	msg = extractMsg(t, srv)
	err = core1.ResponseFailure(msg, fmt.Errorf("rejected"))
	testutil.AssertNoError(t, err, "handshake 3")

	testutil.AssertNil(t, core2.session)
	testutil.AssertNil(t, core1.session)

	extractResp(t, respc)
	//conn4 (again)
	connTest(core1, core2, " 4")

	//conn5 (another side)
	connTest(core2, core1, " 5")
}

func TestCoreSessionTransport(t *testing.T) {

	core1, core2 := newFsmHandler(), newFsmHandler()
	srv := make(chan *pb.SyncMsg, 10)

	core1.Init(nil, srv)
	r := &pb.OpenSession{For: &pb.OpenSession_BlocksOrDelta{BlocksOrDelta: 10}}

	//conn 1
	respc, err := core2.OpenSession(asDebugReceiver{core1, core2}, r)
	msg := extractMsg(t, srv)
	err = core1.AcceptSession(msg, &pb.AcceptSession{})
	testutil.AssertNoError(t, err, "handshake")

	extractResp(t, respc)

	writeTest := func(core *syncCore) {
		select {
		case <-core.SessionCanWrite():
		default:
			t.Fatalf("Can not write")
		}
	}

	blockTest := func(core *syncCore) {
		select {
		case <-core.SessionCanWrite():
			t.Fatalf("unexpected can write")
		default:
		}
	}

	sreq := &pb.TransferRequest{Req: &pb.TransferRequest_Block{Block: &pb.SyncBlockRange{Start: 1, End: 20}}}
	err = core2.SessionReq(sreq)
	testutil.AssertNoError(t, err, "request")

	reqrecv := extractMsg(t, srv)
	testutil.AssertEquals(t, reqrecv.GetRequest().GetSession().GetBlock().GetEnd(), uint64(20))

	writeTest(core1)
	pack := &pb.TransferResponse{Block: &pb.SyncBlock{Height: 1}}

	err = core1.SessionSend(pack)
	testutil.AssertNoError(t, err, "package 1")

	blockTest(core1)
	//ensure method can re-entry
	testutil.AssertEquals(t, core1.SessionCanWrite(), core1.SessionCanWrite())

	packrecv := extractResp(t, respc)
	testutil.AssertEquals(t, packrecv.GetSession().GetBlock().GetHeight(), uint64(1))
	err = core2.SessionAck(packrecv.GetSession(), nil)
	testutil.AssertNoError(t, err, "ack package 1")

	writeTest(core1)

	err = core2.SessionClose()
	testutil.AssertNoError(t, err, "close 1")

	testutil.AssertNil(t, core2.session)
	testutil.AssertNil(t, core1.session)
	testutil.AssertEquals(t, len(core1.waitForResp), 0)
	testutil.AssertEquals(t, len(core2.client), 0)

	//contain both ack and close
	extractMsg(t, srv)
	extractMsg(t, srv)

	//conn again
	core2.sessionOpt.maxWindow = 4
	core1.sessionOpt.maxWindow = 2

	r.Transfer = nil
	respc, err = core2.OpenSession(asDebugReceiver{core1, core2}, r)
	msg = extractMsg(t, srv)
	//	t.Log(msg)
	testutil.AssertEquals(t, msg.GetRequest().GetHandshake().GetTransfer().GetMaxWindowSize(), uint32(4))
	err = core1.AcceptSession(msg, &pb.AcceptSession{})
	testutil.AssertNoError(t, err, "handshake 2")

	accpack := extractResp(t, respc)
	testutil.AssertEquals(t, accpack.GetHandshake().GetTransfer().GetMaxWindowSize(), uint32(2))
	testutil.AssertEquals(t, core1.session.maxWindow, uint32(2))
	testutil.AssertEquals(t, core2.session.maxWindow, uint32(2))

	pack1, pack2, pack3 := &pb.TransferResponse{Block: &pb.SyncBlock{Height: 1}},
		&pb.TransferResponse{Block: &pb.SyncBlock{Height: 2}},
		&pb.TransferResponse{Block: &pb.SyncBlock{Height: 3}}

	writeTest(core1)
	err = core1.SessionSend(pack1)
	testutil.AssertNoError(t, err, "package 2-1")

	err = core1.SessionSend(pack2)
	testutil.AssertNoError(t, err, "package 2-2")

	writeTest(core1)
	err = core1.SessionSend(pack3)
	testutil.AssertNoError(t, err, "package 2-3")

	blockTest(core1)

	//extract pack1
	packrecv = extractResp(t, respc)
	testutil.AssertEquals(t, packrecv.GetSession().GetBlock().GetHeight(), uint64(1))
	err = core2.SessionAck(packrecv.GetSession(), nil)
	testutil.AssertNoError(t, err, "ack package 2-1")

	//extract ack
	extractMsg(t, srv)
	writeTest(core1)

	//sent pack3 again
	err = core1.SessionSend(pack3)
	testutil.AssertNoError(t, err, "package 2-4")
	blockTest(core1)

	//extract (and skip) pack2
	extractResp(t, respc)
	packrecv = extractResp(t, respc)

	testutil.AssertEquals(t, packrecv.GetSession().GetBlock().GetHeight(), uint64(3))
	err = core2.SessionAck(packrecv.GetSession(), nil)
	testutil.AssertNoError(t, err, "ack package 2-3")
	writeTest(core1)

	err = core1.SessionFailure("timeout")
	testutil.AssertNoError(t, err, "closed")

	testutil.AssertNil(t, core2.session)
	testutil.AssertNil(t, core1.session)
	testutil.AssertEquals(t, len(core1.waitForResp), 0)
	testutil.AssertEquals(t, len(core2.client), 0)

	packrecv = extractResp(t, respc)
	testutil.AssertEquals(t, packrecv.GetSession().GetBlock().GetHeight(), uint64(3))
	packrecv = extractResp(t, respc)

	testutil.AssertNotNil(t, packrecv.GetErr())
}
