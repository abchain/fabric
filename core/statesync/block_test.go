package statesync

import (
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


//func TestMain(m *testing.M) {
//	setupTestConfig()
//	os.Exit(m.Run())
//}


func TestMain(m *testing.M) {
	testutil.SetupTestConfig()
	os.Exit(m.Run())
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

	clientMockSyncHandler := newMockSyncHandler(&pb.PeerID{Name: "server"}, testLedger)
	client := newTestClient(testLedger, clientMockSyncHandler)
	clientMockSyncHandler.client = client

	height := 2019
	formTestData(t, testLedger, height)

	serverMockSyncHandler := newMockSyncHandler(&pb.PeerID{Name: "client"}, testLedger)

	clientMockSyncHandler.peer = serverMockSyncHandler
	serverMockSyncHandler.peer = clientMockSyncHandler

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
