package embedded_chaincode

import (
	"fmt"
	"github.com/abchain/fabric/core/chaincode"
	"github.com/abchain/fabric/core/chaincode/container/inproccontroller"
	"github.com/abchain/fabric/core/chaincode/shim"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/protos"
	"github.com/op/go-logging"
	"golang.org/x/net/context"
)

const (
	Embedded_Dummy_Path = "embedded/"
)

var ecclogger = logging.MustGetLogger("embedded_chaincode")

func RegisterEcc(name string, cc shim.Chaincode) (string, error) {

	regPath := Embedded_Dummy_Path + name

	return regPath, inproccontroller.Register(regPath, cc)
}

func DeployEcc(ctxt context.Context, l *ledger.Ledger, chain *chaincode.ChaincodeSupport, chaincodeDeploymentSpec *protos.ChaincodeDeploymentSpec) error {

	spec := chaincodeDeploymentSpec.GetChaincodeSpec()
	chaincode := spec.GetChaincodeID().GetName()
	ecclogger.Debugf("launching embedded chaincode [%s] on chain [%s] for ledger <%p>", chaincode, chain.Name(), l)

	if spec == nil {
		return fmt.Errorf("chaincode spec is nil")
	}

	err, chrte := chain.Launch(ctxt, l, spec.GetChaincodeID(), chaincodeDeploymentSpec)
	if err != nil {
		return fmt.Errorf("Failed to launch chaincode spec (%s): %s", chaincode, err)
	}

	defer func() {
		if err != nil {
			ecclogger.Debugf("stop contianer for %s because of fail exec", chaincode)
			chain.Stop(ctxt, l.Tag(), chaincodeDeploymentSpec)
		}
	}()

	dummyout := ledger.TxExecStates{}
	dummyout.InitForInvoking(l)
	var resp *protos.ChaincodeMessage
	//here we never mark ledger into tx status, so init in syscc NEVER write state
	resp, err = chain.ExecuteLite(ctxt, chrte, protos.Transaction_CHAINCODE_DEPLOY, spec.GetCtorMsg(), dummyout)
	if err != nil {
		return fmt.Errorf("Failed to init chaincode spec(%s): %s", chaincode, err)
	} else if resp.Type == protos.ChaincodeMessage_ERROR {
		err = fmt.Errorf("Exec fail: %s", resp.GetPayload())
		return err
	}

	ecclogger.Debugf("launch exec get result %v", resp)

	if !dummyout.IsEmpty() {
		ecclogger.Warningf("embedded chaincode [%s] set states [%v] in init, which will be just discarded", chaincode, dummyout.DeRef())
	}

	ecclogger.Infof("embedded chaincode [%s] is launched for ledger <%p>", chaincode, l)
	return nil
}
