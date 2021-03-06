/*
Copyright IBM Corp. 2016 All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

		 http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package container

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"github.com/abchain/fabric/core/config"
	"io/ioutil"
	"os"
	"testing"

	"github.com/abchain/fabric/core/chaincode/platforms"
	cutil "github.com/abchain/fabric/core/chaincode/util"
	"github.com/abchain/fabric/core/util"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
)

func TestMain(m *testing.M) {
	flag.BoolVar(&runTests, "run-controller-tests", false, "run tests")
	flag.Parse()
	cfg := config.SetupTestConf{"CORE", "core", "./../../peer/"}
	cfg.Setup()
	os.Exit(m.Run())
}

// an all-in-one function helping to create a valid docker-package
func createChaincodePackageBytes(spec *pb.ChaincodeSpec) ([]byte, error) {
	if spec == nil || spec.ChaincodeID == nil {
		return nil, fmt.Errorf("invalid chaincode spec")
	}

	inputbuf := bytes.NewBuffer(nil)
	gw := gzip.NewWriter(inputbuf)
	tw := tar.NewWriter(gw)

	// platform, err := platforms.Find(spec.Type)
	// if err != nil {
	// 	return nil, err
	// }

	_, err := platforms.WritePackage(spec, tw)
	if err != nil {
		return nil, err
	}

	tw.Close()
	gw.Close()

	err = fmt.Errorf("Not implied yet")

	if err != nil {
		return nil, err
	}

	chaincodePkgBytes := inputbuf.Bytes()
	return chaincodePkgBytes, nil
}

func TestVM_ListImages(t *testing.T) {
	t.Skip("No need to invoke list images.")
	vm, err := NewVM()
	if err != nil {
		t.Fail()
		t.Logf("Error getting VM: %s", err)
	}
	err = vm.ListImages(context.TODO())
	if err != nil {
		t.Fail()
		t.Logf("Error listing images: %s", err)
	}
}

func TestVM_BuildImage_WritingGopathSource(t *testing.T) {
	t.Skip("This can be re-enabled if testing GOPATH writing to tar image.")
	inputbuf := bytes.NewBuffer(nil)
	tw := tar.NewWriter(inputbuf)

	err := cutil.WriteGopathSrc(tw, "")
	if err != nil {
		t.Fail()
		t.Logf("Error writing gopath src: %s", err)
	}
	ioutil.WriteFile("/tmp/chaincode_deployment.tar", inputbuf.Bytes(), 0644)

}

func TestVM_BuildImage_ChaincodeLocal(t *testing.T) {
	vm, err := NewVM()
	if err != nil {
		t.Fail()
		t.Logf("Error getting VM: %s", err)
		return
	}
	// Build the spec
	chaincodePath := "github.com/abchain/fabric/examples/chaincode/go/chaincode_example01"
	spec := &pb.ChaincodeSpec{Type: pb.ChaincodeSpec_GOLANG, ChaincodeID: &pb.ChaincodeID{Path: chaincodePath}, CtorMsg: &pb.ChaincodeInput{Args: util.ToChaincodeArgs("f")}}
	packbytes, err := createChaincodePackageBytes(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := vm.BuildChaincodeContainer(spec, packbytes); err != nil {
		t.Fail()
		t.Log(err)
	}
}

func TestVM_BuildImage_ChaincodeRemote(t *testing.T) {
	t.Skip("Works but needs user credentials. Not suitable for automated unit tests as is")
	vm, err := NewVM()
	if err != nil {
		t.Fail()
		t.Logf("Error getting VM: %s", err)
		return
	}
	// Build the spec
	chaincodePath := "https://github.com/prjayach/chaincode_examples/chaincode_example02"
	spec := &pb.ChaincodeSpec{Type: pb.ChaincodeSpec_GOLANG, ChaincodeID: &pb.ChaincodeID{Path: chaincodePath}, CtorMsg: &pb.ChaincodeInput{Args: util.ToChaincodeArgs("f")}}
	packbytes, err := createChaincodePackageBytes(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := vm.BuildChaincodeContainer(spec, packbytes); err != nil {
		t.Fail()
		t.Log(err)
	}
}

func TestVM_Chaincode_Compile(t *testing.T) {
	// vm, err := NewVM()
	// if err != nil {
	// 	t.Fail()
	// 	t.Logf("Error getting VM: %s", err)
	// 	return
	// }

	// if err := vm.BuildPeerContainer(); err != nil {
	// 	t.Fail()
	// 	t.Log(err)
	// }
	t.Skip("NOT IMPLEMENTED")
}
