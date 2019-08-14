package node

import (
	"bytes"
	"github.com/abchain/fabric/core/config"
	pb "github.com/abchain/fabric/protos"
	"github.com/spf13/viper"
	"golang.org/x/net/context"
	"io/ioutil"
	"testing"
	"time"
)

func buildLegacyNode(cfgname string, t *testing.T) *NodeEngine {

	cf := config.SetupTestConf{"FABRIC", cfgname, ""}
	cf.Setup()

	tempDir, err := ioutil.TempDir("", "fabric-running-test")
	if err != nil {
		t.Fatal("tempfile fail", err)
	}

	viper.Set("peer.fileSystemPath", tempDir)
	config.CacheViper()

	ne := new(NodeEngine)
	ne.Name = "test"
	if err := ne.Init(); err != nil {
		t.Fatal(err)
	}

	return ne

}

func compareTxAndTxe(t *testing.T, origin *pb.Transaction, delivered *pb.TransactionHandlingContext) {
	compareTx(t, origin, delivered.Transaction)
}

func compareTx(t *testing.T, origin, delivered *pb.Transaction) {

	if delivered == nil {
		t.Fatal("No tx is found to compare with", origin)
	}

	if bytes.Compare(origin.ChaincodeID, delivered.ChaincodeID) != 0 {
		t.Fatal("chaincode ID is different:", origin, delivered)
	}

	if bytes.Compare(origin.Payload, delivered.Payload) != 0 {
		t.Fatal("payload is different:", origin, delivered)
	}
}

func TestTxNetwork(t *testing.T) {
	thenode := buildLegacyNode("conf_legacy_test", t)
	defer thenode.FinalRelease()

	testTxNetwork(thenode, t)
}

func TestTxNetworkWithCred(t *testing.T) {

	thenode := buildLegacyNode("conf_running_withcred_test", t)
	defer thenode.FinalRelease()

	testTxNetwork(thenode, t)
}

func testTxNetwork(thenode *NodeEngine, t *testing.T) {

	thepeer := thenode.Peers[""]

	if err := thepeer.Run(); err != nil {
		t.Fatal("run peer fail", err)
	}

	defer thepeer.Stop()

	cli := thenode.TxTopic[""].NewClient()
	defer cli.UnReg()

	topicRead, err := cli.Read(1) //default pos
	if err != nil {
		t.Fatal("topic read err", err)
	}

	spec1 := &pb.ChaincodeInvocationSpec{
		ChaincodeSpec: &pb.ChaincodeSpec{
			ChaincodeID: &pb.ChaincodeID{Name: "mycc1"},
		},
	}

	tx1, err := pb.NewChaincodeExecute(spec1, "", pb.Transaction_CHAINCODE_INVOKE)
	if nil != err {
		t.Fatal("Error on make tx", err)
	}

	resp := thepeer.txn.ExecuteTransaction(context.Background(), tx1, nil)
	if resp.Status == pb.Response_FAILURE {
		t.Fatal("Error on deliver tx1")
	}

	txid1 := string(resp.Msg)
	t.Logf("Get tx id for tx1: %s", txid1)

	//need some time to fill the tx into network ...
	time.Sleep(time.Second)

	compareTx(t, tx1, thenode.DefaultLedger().MustGetTransactionByID(txid1))
	obj, err := topicRead.ReadOne()
	if err != nil {
		t.Fatal("read tx fail", err)
	}

	if topictx, ok := obj.(*pb.TransactionHandlingContext); !ok {
		t.Fatalf("write wrong object in topic: %T(%v)", obj, obj)
	} else {
		if topictx.ChaincodeSpec == nil {
			t.Fatalf("wrong exec context: %v", topictx)
		}
		compareTxAndTxe(t, tx1, topictx)
	}

	thepeer.Stop()

	thenode.TxTopic["mycc2"] = thenode.TxTopic[""]
	delete(thenode.TxTopic, "")

	spec2 := &pb.ChaincodeInvocationSpec{
		ChaincodeSpec: &pb.ChaincodeSpec{
			ChaincodeID: &pb.ChaincodeID{Name: "mycc2"},
			CtorMsg:     &pb.ChaincodeInput{Args: [][]byte{[]byte{42, 42, 42}}},
		},
	}

	tx2, err := pb.NewChaincodeExecute(spec2, "", pb.Transaction_CHAINCODE_INVOKE)
	if nil != err {
		t.Fatal("Error on make tx", err)
	}

	err = thepeer.txn.BroadCastTransaction(tx2, nil)
	if nil != err {
		t.Fatal("Error on broadcast tx2", err)
	}

	err = thepeer.Run()
	if nil != err {
		t.Fatal("re-run peer fail", err)
	}

	resp = thepeer.txn.ExecuteTransaction(context.Background(), tx1, nil)
	if resp.Status == pb.Response_FAILURE {
		t.Fatal("Error on deliver tx1 again")
	}

	txid1again := string(resp.Msg)
	t.Logf("Get tx id for anoghter tx1: %s", txid1again)
	if txid1again == txid1 {
		t.Fatal("Two txid is identify")
	}

	//need some time to fill the tx into network ...
	time.Sleep(time.Second)
	compareTx(t, tx1, thenode.DefaultLedger().MustGetTransactionByID(txid1again))

	obj, err = topicRead.ReadOne()
	if err != nil {
		t.Fatal("read tx2 fail", err)
	}

	if topictx, ok := obj.(*pb.TransactionHandlingContext); !ok {
		t.Fatalf("write wrong object in topic: %T(%v)", obj, obj)
	} else {
		if topictx.ChaincodeSpec == nil {
			t.Fatalf("wrong exec context for tx2: %v", topictx)
		}

		compareTxAndTxe(t, tx2, topictx)
		compareTx(t, tx2, thenode.DefaultLedger().MustGetTransactionByID(topictx.Transaction.GetTxid()))
	}

}
