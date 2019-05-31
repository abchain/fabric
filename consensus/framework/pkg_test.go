package framework

import (
	"github.com/abchain/fabric/consensus/framework/example"
	"github.com/abchain/fabric/core/chaincode"
	"github.com/abchain/fabric/core/embedded_chaincode/api"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/ledger/genesis"
	"github.com/abchain/fabric/core/ledger/testutil"
	"github.com/abchain/fabric/examples/chaincode/embedded/simple_chaincode"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
	"os"
	"strconv"
	"testing"
)

var defTestChaincodeSup = chaincode.DefaultChain
var defCCName = "example02"

var theChain *chaincode.ChaincodeSupport

func TestMain(m *testing.M) {

	testutil.SetupTestConfig()
	theChain = chaincode.NewSystemChaincodeSupport("test", defTestChaincodeSup)
	chaincode.SetChaincodeSupport(chaincode.SystemChain, theChain)
	//register cscc and contract cc
	err := api.RegisterECC(&api.EmbeddedChaincode{defCCName, new(simple_chaincode.SimpleChaincode)})
	if err != nil {
		panic(err)
	}
	err = api.RegisterECC(&api.EmbeddedChaincode{framework_example.DefaultCCName,
		framework_example.SimpleConsensusChaincode{}})
	if err != nil {
		panic(err)
	}

	os.Exit(m.Run())
}

func prepareChaincodeSupport(tb testing.TB, ledgers ...*ledger.Ledger) func() {

	err, cds1, outs := api.LaunchEmbeddedCCFull(context.Background(), defCCName, string(defTestChaincodeSup),
		[][]byte{[]byte("init"), []byte("a"), []byte("1000"), []byte("b"), []byte("1000")}, ledgers...)

	testutil.AssertNoError(tb, err, "launch contract")

	err, cds2, _ := api.LaunchEmbeddedCCFull(context.Background(), framework_example.DefaultCCName, string(defTestChaincodeSup),
		[][]byte{}, ledgers...)
	testutil.AssertNoError(tb, err, "launch cscc")

	for i, l := range ledgers {
		err = genesis.MakeGenesisForLedgerDirect(l, outs[i])
		testutil.AssertNoError(tb, err, "do genesis")
	}

	return func() {
		for _, l := range ledgers {
			err := theChain.Stop(context.Background(), l.Tag(), cds1)
			testutil.AssertNoError(tb, err, "stop contract")

			err = theChain.Stop(context.Background(), l.Tag(), cds2)
			testutil.AssertNoError(tb, err, "stop cscc")
		}

	}
}

func prepareContractTxe(count int) *pb.TransactionHandlingContext {

	args := [][]byte{[]byte("any")}
	if count > 0 {
		args = append(args, []byte("a"), []byte("b"), []byte(strconv.Itoa(count)))
	} else {
		args = append(args, []byte("b"), []byte("a"), []byte(strconv.Itoa(count*(-1))))
	}

	tx, err := pb.NewChaincodeExecute(&pb.ChaincodeInvocationSpec{
		ChaincodeSpec: &pb.ChaincodeSpec{
			Type:        pb.ChaincodeSpec_GOLANG,
			ChaincodeID: &pb.ChaincodeID{Name: defCCName},
			CtorMsg:     &pb.ChaincodeInput{Args: args},
		},
	}, "", pb.Transaction_CHAINCODE_INVOKE)

	dig, err := tx.Digest()
	if err != nil {
		panic(err)
	}
	tx.Txid = pb.TxidFromDigest(dig)

	if err != nil {
		panic(err)
	}

	txe, _ := pb.DefaultTxHandler.Handle(pb.NewTransactionHandlingContext(tx))
	return txe

}
