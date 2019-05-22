package embedded_chaincode

import (
	"fmt"
	"github.com/abchain/fabric/core/chaincode"
	"github.com/abchain/fabric/core/chaincode/container/inproccontroller"
	"github.com/abchain/fabric/core/chaincode/shim"
	"github.com/spf13/viper"
)

var wlchaincodes map[string]string

func RegisterSysCC(name string, cc shim.Chaincode, chains ...chaincode.ChainName) (string, error) {

	if wlchaincodes == nil {
		wlchaincodes = viper.GetStringMapString("chaincode.system")
	}

	//notice if chaincode.system is not specified, it was consider as no restriction
	//on syscc
	if len(wlchaincodes) > 0 {
		if val, ok := wlchaincodes[name]; !ok || !(val == "enable" || val == "true" || val == "yes") {
			return "", fmt.Errorf("can not register system chaincode <%s>", name)
		}
	}

	regPath := Embedded_Dummy_Path + name
	var pfname []string
	for _, cn := range chains {
		pfname = append(pfname, string(cn))
	}

	return regPath, inproccontroller.RegisterOnPlatform(regPath, cc, pfname)
}

// func sysccInit(ctxt context.Context, l *ledger.Ledger, chain *chaincode.ChaincodeSupport, syscc *api.SystemChaincode) error {
// 	// First build and get the deployment spec
// 	chaincodeID := &protos.ChaincodeID{Path: syscc.Path, Name: syscc.Name}
// 	spec := protos.ChaincodeSpec{Type: protos.ChaincodeSpec_Type(protos.ChaincodeSpec_Type_value["GOLANG"]), ChaincodeID: chaincodeID, CtorMsg: &protos.ChaincodeInput{Args: syscc.InitArgs}}

// 	chaincodeDeploymentSpec, err := api.BuildEmbeddedCC(ctxt, &spec)

// 	if err != nil {
// 		return fmt.Errorf("Error deploying chaincode spec (%s): %s", syscc.Name, err)
// 	}

// 	err, chrte := chain.Launch(ctxt, l, chaincodeID, chaincodeDeploymentSpec)
// 	if err != nil {
// 		return fmt.Errorf("Failed to launch chaincode spec (%s): %s", syscc.Name, err)
// 	}

// 	dummyout := ledger.TxExecStates{}
// 	dummyout.InitForInvoking(l)
// 	//here we never mark ledger into tx status, so init in syscc NEVER write state
// 	_, err = chain.ExecuteLite(ctxt, chrte, protos.Transaction_CHAINCODE_DEPLOY, spec.CtorMsg, dummyout)
// 	if err != nil {
// 		return fmt.Errorf("Failed to init chaincode spec(%s): %s", syscc.Name, err)
// 	}

// 	if !dummyout.IsEmpty() {
// 		sysccLogger.Warning("system chaincode [%s] set states in init, which will be just discarded", syscc.Name)
// 	}

// 	sysccLogger.Infof("system chaincode [%s] is launched for ledger <%p>", syscc.Name, l)
// 	return nil
// }
