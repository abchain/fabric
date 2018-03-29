package embedded_chaincode

import (
	"fmt"

	"github.com/abchain/fabric/core/chaincode"
	"github.com/abchain/fabric/core/embedded_chaincode/api"
	"github.com/abchain/fabric/protos"
	"github.com/op/go-logging"
	"golang.org/x/net/context"
)

var sysccLogger = logging.MustGetLogger("sysccapi")

// deployLocal deploys the supplied chaincode image to the local peer (pass through devOps interface)
func deploySysCC(syscc *api.SystemChaincode) error {

	err := api.RegisterSysCC(syscc)
	if err != nil {
		sysccLogger.Error(fmt.Sprintf("deploy register fail (%s,%v): %s", syscc.Path, syscc, err))
		return err
	}

	ctx := context.Background()
	// First build and get the deployment spec
	chaincodeID := &protos.ChaincodeID{Path: syscc.Path, Name: syscc.Name}
	spec := protos.ChaincodeSpec{Type: protos.ChaincodeSpec_Type(protos.ChaincodeSpec_Type_value["GOLANG"]), ChaincodeID: chaincodeID, CtorMsg: &protos.ChaincodeInput{Args: syscc.InitArgs}}

	chaincodeDeploymentSpec, err := api.BuildEmbeddedCC(ctx, &spec)

	if err != nil {
		sysccLogger.Error(fmt.Sprintf("Error deploying chaincode spec: %v\n\n error: %s", spec, err))
		return err
	}

	transaction, err := protos.NewChaincodeDeployTransaction(chaincodeDeploymentSpec, chaincodeDeploymentSpec.ChaincodeSpec.ChaincodeID.Name)
	if err != nil {
		return fmt.Errorf("Error deploying chaincode: %s ", err)
	}

	_, _, err = chaincode.Execute(ctx, chaincode.GetChain(chaincode.DefaultChain), transaction)

	return err
}