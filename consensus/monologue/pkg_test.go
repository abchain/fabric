package monologue_test

import (
	"github.com/abchain/fabric/consensus/framework"
	"github.com/abchain/fabric/consensus/monologue"
	cspb "github.com/abchain/fabric/consensus/protos"
	"github.com/abchain/fabric/core/chaincode"
	"github.com/abchain/fabric/core/config"
	"github.com/abchain/fabric/core/embedded_chaincode/api"
	"github.com/abchain/fabric/core/ledger/testutil"
	"github.com/abchain/fabric/node"
	pb "github.com/abchain/fabric/protos"
	"github.com/spf13/viper"
	"golang.org/x/net/context"
	"io/ioutil"
	"os"
	"testing"
	"time"
)

const testNode = "test"

func TestMain(m *testing.M) {

	cf := config.SetupTestConf{"FABRIC", "conf_test", ""}
	cf.Setup()

	thechain := chaincode.NewSystemChaincodeSupport(testNode, chaincode.SystemChain)
	chaincode.SetChaincodeSupport(chaincode.DefaultChain, thechain)
	os.Exit(m.Run())
}

func setTmpPath() {
	tempDir, err := ioutil.TempDir("", "monologue-test")
	if err != nil {
		panic("tempfile fail")
	}

	viper.Set("peer.fileSystemPath", tempDir)
	config.CacheViper()
}

var syscc = &api.SystemChaincode{
	Enabled: true,
	Name:    "testCC",
}

func TestMining(t *testing.T) {

	setTmpPath()

	ne := new(node.NodeEngine)
	ne.Name = testNode
	ne.Options.MakeGenesisForLedger = true

	ne.PreInit()
	cbase := framework.NewConsensusBaseNake()
	cbase.MakeScheme(ne, syscc.Name)

	defer ne.FinalRelease()
	if err := ne.ExecInit(); err != nil {
		t.Fatal(err)
	}

	newstateNotify := make(chan uint64, 1)
	ne.DefaultLedger().SubScribeNewState(func(statepos uint64, _ []byte) { newstateNotify <- statepos })

	blockNotify := make(chan *cspb.PurposeBlock, 1)
	syscc.Chaincode = monologue.NewChaincode(monologue.NewCore(10),
		func(blk *cspb.PurposeBlock) { blockNotify <- blk })

	err := api.RegisterAndLaunchSysCC(context.Background(), syscc, ne.DefaultLedger())
	testutil.AssertNoError(t, err, "launch cc")

	defer chaincode.GetSystemChain().Stop(context.Background(), ne.DefaultLedger().Tag(), syscc.Deployed)

	err = ne.RunPeers()
	testutil.AssertNoError(t, err, "run peer")

	cslearner := framework.NewBaseLedgerLearner(ne.DefaultLedger(),
		ne.DefaultPeer(), framework.NewConfig(nil))

	miner := monologue.NewPurposer(true, monologue.DefaultWrapper(syscc.Name, "ANY"))
	confirmer := monologue.BuildConfirm(syscc.Name, cbase.PurposeEntry(), cslearner)

	txdeliver, err := framework.GenDeliverFromNode(ne, "")
	testutil.AssertNoError(t, err, "gen deliver")

	mining := cbase.BuildBasePurposerRoutine(miner, txdeliver, 5, ne.TxTopic[""].NewClient())

	//build 2 tx indicate unexisted cc
	unexistedCC := &pb.ChaincodeID{Name: "Not existed"}
	tx1, err := pb.NewChaincodeTransaction(pb.Transaction_CHAINCODE_INVOKE,
		unexistedCC, "", nil)

	testutil.AssertNoError(t, err, "make tx1")

	tx2, err := pb.NewChaincodeTransaction(pb.Transaction_CHAINCODE_INVOKE,
		unexistedCC, "another", nil)

	testutil.AssertNoError(t, err, "make tx2")

	resp := ne.DefaultPeer().TxNetwork().ExecuteTransaction(context.Background(), tx1, nil)
	testutil.AssertEquals(t, resp.Status, pb.Response_SUCCESS)

	resp = ne.DefaultPeer().TxNetwork().ExecuteTransaction(context.Background(), tx2, nil)
	testutil.AssertEquals(t, resp.Status, pb.Response_SUCCESS)

	csctx, endcsf := context.WithCancel(context.Background())
	mainroutineEnd := make(chan interface{})
	defer func() {
		<-mainroutineEnd
	}()
	defer endcsf()

	go func() {
		defer close(mainroutineEnd)
		cbase.MainRoutine(csctx, cslearner)
	}()

	time.Sleep(300 * time.Millisecond)
	wres := mining()
	err = wres(context.Background())
	testutil.AssertNoError(t, err, "mining res 1")
	var addblk *cspb.PurposeBlock
	select {
	case addblk = <-blockNotify:
	case <-time.NewTimer(time.Second).C:
		t.Fatal("Unexpected no-call on notify")
	}

	addbh, err := addblk.GetB().GetHash()
	testutil.AssertNoError(t, err, "blk hash")

	confirmer(addblk.GetN(), addbh)

	<-newstateNotify

	blk, err := ne.DefaultLedger().GetBlockByNumber(1)
	testutil.AssertNoError(t, err, "get block")
	t.Log(blk)
	testutil.AssertEquals(t, len(blk.GetTransactions()), 2)

	wres = mining()
	err = wres(context.Background())
	testutil.AssertNoError(t, err, "mining res 2")
	select {
	case addblk = <-blockNotify:
	case <-time.NewTimer(time.Second).C:
		t.Fatal("Unexpected no-call on notify")
	}
}
