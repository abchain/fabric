package txnetwork

import (
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
	"testing"
	"time"
)

type dummyH struct {
	tx        []*PendingTransaction
	callTimes int
	testing.TB
}

func (h *dummyH) Release() {}

func (h *dummyH) HandleTxs(tx []*PendingTransaction) error {

	h.Logf("Add %d txs", len(tx))
	h.tx = append(h.tx, tx...)
	h.callTimes++

	return nil

}

type dummyEndorser int

func (dummyEndorser) EndorseTransaction(tx *pb.Transaction) (*pb.Transaction, error) { return tx, nil }
func (dummyEndorser) Release()                                                       {}

func TestTxEntrance(t *testing.T) {

	wctx, endf := context.WithCancel(context.Background())

	entry := newTxNetworkEntry()

	for i := 0; i < 10; i++ {

		tx, _ := buildTestTx(t)
		entry.BroadCastTransaction(tx, nil)
	}

	h := &dummyH{nil, 0, t}

	entry.Start(wctx, h)

	time.Sleep(time.Second)

	if len(h.tx) != 10 {
		t.Fatal("Handler never receive enough txs:", h.tx)
	}

	if h.callTimes > 1 {
		t.Errorf("Call mutiple times (%d)", h.callTimes)
	}

	for i := 0; i < 5; i++ {

		tx, _ := buildTestTx(t)
		entry.BroadCastTransaction(tx, dummyEndorser(i))
	}

	time.Sleep(time.Second)

	if len(h.tx) != 15 {
		t.Fatal("Handler never receive enough txs:", h.tx)
	}

	if h.tx[7].endorser != nil {
		t.Fatal("ghost endorser", h.tx)
	}

	if int(h.tx[13].endorser.(dummyEndorser)) != 3 {
		t.Fatal("Wrong endorser name", h.tx)
	}

	if h.callTimes > 6 {
		t.Errorf("Too many calling times (%d)", h.callTimes)
	}

	endf()

	time.Sleep(time.Second)

	ntx, _ := buildTestTx(t)
	entry.BroadCastTransaction(ntx, dummyEndorser(100))

	time.Sleep(time.Second)

	if len(h.tx) != 15 {
		t.Fatal("Handler accept unexpected tx")
	}

}
