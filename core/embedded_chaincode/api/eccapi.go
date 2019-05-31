package api

import (
	"fmt"
	"github.com/abchain/fabric/core/chaincode"
	"github.com/abchain/fabric/core/chaincode/shim"
	embedded "github.com/abchain/fabric/core/embedded_chaincode"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
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

	ccfullname := spec.GetChaincodeID().GetName()
	if ccfullname == "" {
		return nil, fmt.Errorf("chaincode name is empty")
	}

	ccname, tmn, _ := protos.ParseYFCCName(ccfullname)
	if tmn == "" {
		tmn = ccname
	}

	regPath, ok := ccReg[tmn]
	if !ok {
		return nil, fmt.Errorf("Embedded chaincode not found (%s in %s)", tmn, ccfullname)
	}

	dspec := *spec
	dspec.ChaincodeID = &protos.ChaincodeID{Path: regPath, Name: ccname}

	chaincodeDeploymentSpec := &protos.ChaincodeDeploymentSpec{ExecEnv: protos.ChaincodeDeploymentSpec_SYSTEM, ChaincodeSpec: &dspec}
	return chaincodeDeploymentSpec, nil
}

func LaunchEmbeddedCCFull(ctx context.Context, name, chain string,
	args [][]byte, ledgers ...*ledger.Ledger) (error, *protos.ChaincodeDeploymentSpec, []ledger.TxExecStates) {

	var chainplatform *chaincode.ChaincodeSupport
	if chain == "" {
		chainplatform = chaincode.GetDefaultChain()
	} else {
		chainplatform = chaincode.GetChain(chaincode.ChainName(chain))
	}

	if chainplatform == nil {
		return fmt.Errorf("chain platform %s is not ready", chain), nil, nil
	}

	deployspec, err := BuildEmbeddedCC(&protos.ChaincodeSpec{
		Type:        protos.ChaincodeSpec_Type(protos.ChaincodeSpec_Type_value["GOLANG"]),
		ChaincodeID: &protos.ChaincodeID{Name: name},
		CtorMsg:     &protos.ChaincodeInput{Args: args},
	})

	if err != nil {
		return err, nil, nil
	}

	deployOut := []ledger.TxExecStates{}
	for _, l := range ledgers {
		if err := embedded.DeployEcc(ctx, l, chainplatform, deployspec); err != nil {
			if out, ok := err.(embedded.SuccessWithOutput); ok {
				deployOut = append(deployOut, out.TxExecStates)
			} else {
				return err, nil, nil
			}
		} else {
			deployOut = append(deployOut, ledger.TxExecStates{})
		}
	}

	return nil, deployspec, deployOut
}

func LaunchEmbeddedCC(ctx context.Context, name, chain string, args [][]byte, ledger ...*ledger.Ledger) error {

	ret, _, _ := LaunchEmbeddedCCFull(ctx, name, chain, args, ledger...)
	return ret

}
