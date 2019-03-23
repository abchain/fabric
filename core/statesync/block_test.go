package statesync

import (
	"fmt"
	_ "github.com/abchain/fabric/core/config"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/ledger/testutil"
	"github.com/abchain/fabric/core/util"
	"github.com/abchain/fabric/flogging"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
	"os"
	"testing"
)

type itest interface {
	call()
}

type base struct {
	aa int
}

func (b *base) call()  {
	fmt.Printf("base call\n")
}

type child struct {
	*base
}

//func (b *child) call()  {
//	fmt.Printf("child call\n")
//}



//func TestMain(m *testing.M) {
//	setupTestConfig()
//	os.Exit(m.Run())
//}


func TestMain(m *testing.M) {
	testutil.SetupTestConfig()
	os.Exit(m.Run())
}



func TestBaseChild(t *testing.T) {
	var obj itest
	obj = &child{}

	obj.call()
}

func formTestData(t *testing.T, ledger *ledger.Ledger, height int) {

	uuid := util.GenerateUUID()
	transaction, _ := pb.NewTransaction(pb.ChaincodeID{Path: "testUrl"}, uuid, "anyfunction", []string{"param1, param2"})

	//add gensis block
	ledger.BeginTxBatch(0)
	ledger.CommitTxBatch(0, nil, nil, nil)
	for ib := 1; ib < height; ib++ {
		ledger.BeginTxBatch(ib)
		ledger.TxBegin("txUuid")
		ledger.SetState("chaincode1", "keybase", []byte{byte(ib)})
		ledger.SetState("chaincode2", "keybase", []byte{byte(ib)})
		ledger.SetState("chaincode3", "keybase", []byte{byte(ib)})
		ledger.TxFinished("txUuid", true)
		ledger.CommitTxBatch(ib, []*pb.Transaction{transaction}, nil, []byte("proof1"))
		size := ledger.GetBlockchainSize()
		stateDelta, _ := ledger.GetStateDelta(size - 1)
		if(stateDelta == nil) {
			t.Fatalf("Get unexpected GetStateDelta result")
		}
	}

	statehash, err := ledger.GetCurrentStateHash()
	if(err != nil) {
		t.Fatalf("Get unexpected GetStateDelta result")
	}

	t.Logf("Target statehash: %x", statehash)
}



func TestBlockSync(t *testing.T) {

	testLedger := ledger.InitTestLedger(t)

	clientMockSyncHandler := newMockSyncHandler2(&pb.PeerID{Name: "server"}, testLedger)
	client := newTestClient(testLedger, clientMockSyncHandler)
	clientMockSyncHandler.client = client

	height := 2019
	formTestData(t, testLedger, height)

	serverMockSyncHandler := newMockSyncHandler2(&pb.PeerID{Name: "client"}, testLedger)
	server := newTestServer(testLedger, serverMockSyncHandler)
	serverMockSyncHandler.server = server

	for i := 1; i < height; i++ {

		stateDelta, err := server.ledger.GetStateDelta(uint64(i))
		if err != nil {
			t.Fatalf("[%s]: Failed to get state detals. err: %s", flogging.GoRDef, err)
		}
		if stateDelta == nil {
			t.Fatalf("[%s]: Invalid state detals. height: %d", flogging.GoRDef, i)
		}
	}

	clientMockSyncHandler.peer = serverMockSyncHandler
	serverMockSyncHandler.peer = clientMockSyncHandler


	//mk := server.parent.(mockSyncHandler2)


	go clientMockSyncHandler.handleServerChat()
	go serverMockSyncHandler.handleClientChat()

	_, endBlockNumber, err := client.getSyncTargetBlockNumber()

	if err != nil {
		t.Fatalf("[%s]: Failed to sync state detals. err: %s", flogging.GoRDef, err)
	}

	clientMockSyncHandler.fsmHandler.Event(enterGetDelta)
	msgHandler := newBlockMessageHandler(2, endBlockNumber, client)
	client.syncMessageHandler = msgHandler
	err = client.executeSync() // sync block cf
	if err != nil {
		t.Fatalf("[%s]: Failed to sync state detals. err: %s", flogging.GoRDef, err)
	}
}


func newTestServer(l *ledger.Ledger, parent ISyncHandler) (*stateServer) {
	s := &stateServer{
		parent: parent,
	}
	s.ledger = l.CreateSnapshot()

	s.parent.sendSyncMsg(nil,9,nil)
	return s

}

func newTestClient(l *ledger.Ledger, parent ISyncHandler) (*syncer) {

	sts := &syncer{positionResp: make(chan *pb.SyncStateResp),
		ledger: l,
		parent: parent,
		Context: context.TODO(),
	}

	sts.syncMessageChan = make(chan *pb.SyncMessage)
	sts.positionResp = make(chan *pb.SyncStateResp)
	sts.startResponseChan = make(chan *pb.SyncStartResponse)
	return sts
}
