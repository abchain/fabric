package api

import (
	"fmt"
	"github.com/abchain/fabric/core/chaincode"
	"github.com/abchain/fabric/core/chaincode/shim"
	embedded "github.com/abchain/fabric/core/embedded_chaincode"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
	"strings"
)

type EmbeddedChaincode struct {

	//Unique name of the embedded chaincode
	Name string

	// Chaincode is the actual chaincode object
	Chaincode shim.Chaincode
}

//general embedded only need to register before deploy ...
var ccReg map[string]string

func init() {
	ccReg = make(map[string]string)
}

func RegisterECC(ecc *EmbeddedChaincode) error {

	name := ecc.Name

	_, ok := ccReg[name]
	if ok {
		return fmt.Errorf("register dupliated embedded chaincode", name)
	}

	regPath, err := embedded.RegisterEcc(name, ecc.Chaincode)
	if err != nil {
		return fmt.Errorf("could not register embedded chaincode", name, err)
	}

	ccReg[name] = regPath

	return nil
}

// "build" a given chaincode code (the origin buildSysCC)
// no chaincodeid.path is specified in the input ChaincodeSpec, API help to search
// the corresponding registry and build correspoinding path
func BuildEmbeddedCC(spec *protos.ChaincodeSpec) (*protos.ChaincodeDeploymentSpec, error) {

	tname := spec.ChaincodeID.Name
	if tname == "" {
		return nil, fmt.Errorf("chaincode name is empty")
	}
	snames := strings.Split(tname, ":")
	tname = snames[0]
	if tname == "" {
		return nil, fmt.Errorf("chaincode name (%s) is invalid, lead to empty template", spec.ChaincodeID.Name)
	}

	regPath, ok := ccReg[tname]
	if !ok {
		return nil, fmt.Errorf("Embedded chaincode not found", spec.ChaincodeID.Name)
	}
	if len(snames) > 1 {
		tname = snames[1]
	}

	dspec := *spec
	dspec.ChaincodeID = &protos.ChaincodeID{Path: regPath, Name: tname}

	chaincodeDeploymentSpec := &protos.ChaincodeDeploymentSpec{ExecEnv: protos.ChaincodeDeploymentSpec_SYSTEM, ChaincodeSpec: &dspec}
	return chaincodeDeploymentSpec, nil
}

func LaunchEmbeddedCC(ctx context.Context, name, chain string, args [][]byte, ledger ...*ledger.Ledger) error {

	var chainplatform *chaincode.ChaincodeSupport
	if chain == "" {
		chainplatform = chaincode.GetDefaultChain()
	} else {
		chainplatform = chaincode.GetChain(chaincode.ChainName(chain))
	}

	if chainplatform == nil {
		return fmt.Errorf("chain platform %s is not ready", chain)
	}

	deployspec, err := BuildEmbeddedCC(&protos.ChaincodeSpec{
		Type:        protos.ChaincodeSpec_Type(protos.ChaincodeSpec_Type_value["GOLANG"]),
		ChaincodeID: &protos.ChaincodeID{Name: name},
		CtorMsg:     &protos.ChaincodeInput{Args: args},
	})

	if err != nil {
		return err
	}

	for _, l := range ledger {
		if err := embedded.DeployEcc(ctx, l, chainplatform, deployspec); err != nil {
			return err
		}
	}

	return nil

}
