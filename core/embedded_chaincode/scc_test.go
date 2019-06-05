package embedded_chaincode_test

import (
	"github.com/abchain/fabric/core/chaincode"
	"github.com/abchain/fabric/core/config"
	"github.com/abchain/fabric/core/embedded_chaincode"
	"github.com/abchain/fabric/core/embedded_chaincode/api"
	cc "github.com/abchain/fabric/examples/chaincode/embedded/simple_chaincode"
	"github.com/abchain/fabric/node"
	pb "github.com/abchain/fabric/protos"
	"github.com/spf13/viper"
	"golang.org/x/net/context"
	"io/ioutil"
	"testing"
)

func TestRunningScc(t *testing.T) {

	cf := config.SetupTestConf{"FABRIC", "conf_test", ""}
	cf.Setup()

	tempDir, err := ioutil.TempDir("", "fabric-db-test")
	if err != nil {
		t.Fatal("tempfile fail", err)
	}
	viper.Set("node.fileSystemPath", tempDir)
	config.CacheViper()

	ne := new(node.NodeEngine)
	ne.Name = "test"
	defer ne.FinalRelease()
	if err := ne.Init(); err != nil {
		t.Fatal(err)
	}

	chaincode.NewSystemChaincodeSupport(ne.Name)

	ccConf := &api.SystemChaincode{
		Enabled:   true,
		Name:      "examplecc",
		Chaincode: new(cc.SimpleChaincode),
		InitArgs:  [][]byte{[]byte("INIT"), []byte("a"), []byte("100"), []byte("b"), []byte("50")},
	}

	if err := api.RegisterAndLaunchSysCC(context.Background(), ccConf, ne.LedgersList()...); err != nil {
		if _, ok := err.(embedded_chaincode.SuccessWithOutput); !ok {
			t.Fatal(err)
		}

	}

	defpf := chaincode.NewChaincodeSupport(chaincode.DefaultChain, ne.Name,
		ne.DefaultPeer().GetServerPoint().Spec(), false)

	deployspec := &pb.ChaincodeDeploymentSpec{
		ChaincodeSpec: &pb.ChaincodeSpec{
			Type:        pb.ChaincodeSpec_Type(pb.ChaincodeSpec_Type_value["GOLANG"]),
			ChaincodeID: &pb.ChaincodeID{Name: "shouldnotrun", Path: "embedded/" + ccConf.Name},
			CtorMsg:     &pb.ChaincodeInput{Args: ccConf.InitArgs},
		},
		ExecEnv: pb.ChaincodeDeploymentSpec_SYSTEM,
	}

	if _, err := defpf.Launch(context.Background(), ne.DefaultLedger(),
		"shouldnotrun", deployspec); err == nil {
		t.Fatal("unexpected success in running syscc (security is passed by)")
	}
}
