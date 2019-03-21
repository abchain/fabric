package statesync

import (
	"fmt"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/util"
	pb "github.com/abchain/fabric/protos"
	"github.com/spf13/viper"
	"golang.org/x/net/context"
	"io/ioutil"
	"os"
	"testing"
)

func TestMain(m *testing.M) {

	fmt.Printf("func TestMain\n")
	setupTestConfig()
	os.Exit(m.Run())
}

func setupTestConfig() {
	tempDir, err := ioutil.TempDir("", "fabric-db-test")
	if err != nil {
		panic(err)
	}
	viper.Set("peer.fileSystemPath", tempDir)
	deleteTestDBPath()
}


func newTestServer(l *ledger.Ledger, parent ISyncHandler) (*stateServer) {

	s := &stateServer{
		parent: parent,
	}
	s.ledger = l.CreateSnapshot()

	stateDelta, _ := s.ledger.GetStateDelta(2)

	_ = stateDelta

	return s

}

func newTestClient(l *ledger.Ledger, parent ISyncHandler) (*syncer) {

	sts := &syncer{positionResp: make(chan *pb.SyncStateResp),
		ledger: l,
		parent: parent,
		Context: context.TODO(),
	}
	return sts
}


func formTestData(ledger *ledger.Ledger) {
	transaction, _ := buildTestTx()
	//add gensis block
	ledger.BeginTxBatch(0)
	ledger.CommitTxBatch(0, nil, nil, nil)
	for ib := 0; ib < 5; ib++ {
		ledger.BeginTxBatch(1)
		ledger.TxBegin("txUuid")
		ledger.SetState("chaincode1", "keybase", []byte{byte(ib)})
		ledger.SetState("chaincode2", "keybase", []byte{byte(ib)})
		ledger.SetState("chaincode3", "keybase", []byte{byte(ib)})
		ledger.TxFinished("txUuid", true)
		ledger.CommitTxBatch(1, []*pb.Transaction{transaction}, nil, []byte("proof1"))
	}

}



func buildTestTx() (*pb.Transaction, string) {
	uuid := util.GenerateUUID()
	tx, _ := pb.NewTransaction(pb.ChaincodeID{Path: "testUrl"}, uuid, "anyfunction", []string{"param1, param2"})
	return tx, uuid
}


func TestBlockSync(t *testing.T) {

	tl := ledger.InitTestLedger(t)

	clientMockSyncHandler := newMockSyncHandler(&pb.PeerID{Name: "server"}, tl)
	client := newTestClient(tl, clientMockSyncHandler)
	clientMockSyncHandler.client = client

	formTestData(tl)


	serverMockSyncHandler := newMockSyncHandler(&pb.PeerID{Name: "client"}, tl)
	server := newTestServer(tl, serverMockSyncHandler)
	serverMockSyncHandler.server = server


	clientMockSyncHandler.peer = serverMockSyncHandler
	serverMockSyncHandler.peer = clientMockSyncHandler

	go clientMockSyncHandler.handleServerChat()
	go serverMockSyncHandler.handleClientChat()

	//clientMockSyncHandler.fsmHandler.Event(enterSyncBegin)
	clientMockSyncHandler.fsmHandler.Event(enterGetDelta2)
	serverMockSyncHandler.fsmHandler.Event(enterServe)

	msgHandler := newBlockMessageHandler(2, 5, client)
	client.syncMessageHandler = msgHandler
	err := client.executeSync() // sync block cf
	if err != nil {
		logger.Errorf("[%s]: Failed to sync state detals. err: %s", err)
	}
}


func assertTrue(t *testing.T, v bool) {
	if v {
		return
	}

	t.Fatalf("Get unexpected FALSE result")
}

func assertIntEqual(t *testing.T, v int, exp int) {
	if v == exp {
		return
	}

	t.Fatalf("Value is not equal: get %d, expected %d", v, exp)
}

func assertByteEqual(t *testing.T, v []byte, expected string) {
	if expected == "" && v == nil {
		return
	} else if string(v) == expected {
		return
	}

	t.Fatalf("Value is not equal: get %s, expected %s", string(v), expected)
}


func deleteTestDBPath() {
	dbPath := viper.GetString("peer.fileSystemPath")
	os.RemoveAll(dbPath)
}
