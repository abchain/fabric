package chaincode

import (
	"github.com/abchain/fabric/core/chaincode/container"
	"github.com/abchain/fabric/core/chaincode/container/ccintf"
	"github.com/abchain/fabric/core/chaincode/platforms"
	"github.com/abchain/fabric/core/config"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
	"io"
	"os"
	"testing"
)

func TestGeneratePackage(t *testing.T) {

	t.Skip("This is a temporary test only for deep inspecting purpose")

	url := "github.com/abchain/fabric/examples/chaincode/go/chaincode_example01"
	spec := &pb.ChaincodeSpec{Type: pb.ChaincodeSpec_GOLANG, ChaincodeID: &pb.ChaincodeID{Path: url}, CtorMsg: &pb.ChaincodeInput{Args: [][]byte{[]byte("init")}}}

	ccbytes, err := GetChaincodePackageBytes(spec)
	if err != nil {
		t.Fatal("get chaincode fail:", err)
	}

	cli := new(config.ClientSpec)
	err = cli.Init(config.SubViper("peer"))
	if err != nil || !cli.EnableTLS {
		t.Fatal("Load spec fail:", err)
	}

	rd, err := WriteRuntimePackage(&pb.ChaincodeDeploymentSpec{
		ChaincodeSpec: spec,
		CodePackage:   ccbytes,
	}, cli)
	if err != nil {
		t.Fatal("write runtime fail:", err)
	}

	fl, err := os.Create("dump.tar.gz")
	if err != nil {
		t.Fatal("create file fail:", err)
	}

	sz, err := io.Copy(fl, rd)
	if err != nil {
		t.Fatal("copy content fail:", err)
	} else {
		t.Logf("write %d bytes", sz)
	}

	err = rd.Finish()
	if err != nil {
		t.Fatal("finish have fail:", err)
	}

	rd, err = WriteRuntimePackage(&pb.ChaincodeDeploymentSpec{
		ChaincodeSpec: spec,
		CodePackage:   ccbytes,
	}, cli)
	if err != nil {
		t.Fatal("write runtime again fail:", err)
	}

	rd.Finish()
}

func TestChaincodeImage(t *testing.T) {

	url := "github.com/abchain/fabric/examples/chaincode/go/chaincode_example01"
	spec := &pb.ChaincodeSpec{
		Type:        pb.ChaincodeSpec_GOLANG,
		ChaincodeID: &pb.ChaincodeID{Name: "testcc", Path: url},
		CtorMsg:     &pb.ChaincodeInput{Args: [][]byte{[]byte("init")}}}

	cli := new(config.ClientSpec)
	err := cli.Init(config.SubViper("peer"))
	if err != nil || !cli.EnableTLS {
		t.Fatal("Load spec fail:", err)
	}

	args, envs, err := platforms.GetArgsAndEnv(spec, "testnet", cli)
	if err != nil {
		t.Fatal("args fail:", err)
	}

	ccbytes, err := GetChaincodePackageBytes(spec)
	if err != nil {
		t.Fatal("get chaincode fail:", err)
	}

	rd, err := WriteRuntimePackage(&pb.ChaincodeDeploymentSpec{
		ChaincodeSpec: spec,
		CodePackage:   ccbytes,
	}, cli)
	if err != nil {
		t.Fatal("write runtime fail:", err)
	}

	cir := &container.CreateImageReq{
		CCID: ccintf.CCID{
			ChaincodeSpec: spec,
			NetworkID:     "default",
			PeerID:        "test_node"},
		Args:   args,
		Reader: rd,
		Env:    envs}

	//create image and create container
	_, err = container.VMCProcess(context.Background(), container.DOCKER, cir)
	if err != nil {
		t.Fatal("container vm fail:", err)
	}

	err = rd.Finish()
	if err != nil {
		t.Fatal("finish have fail:", err)
	}

	dir := container.DestroyImageReq{CCID: ccintf.CCID{ChaincodeSpec: spec,
		NetworkID: "default",
		PeerID:    "test_node"}, Force: true, NoPrune: true}

	_, err = container.VMCProcess(context.Background(), container.DOCKER, dir)
	if err != nil {
		t.Fatal("container vm rmi fail:", err)
	}

}
