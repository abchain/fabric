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

//we devided embedded chaincode into system and embedded

// SystemChaincode defines the metadata needed to initialize system chaincode
// when the fabric comes up. SystemChaincodes are installed by adding an
// entry in importsysccs.go
type SystemChaincode struct {
	// Enabled a convenient switch to enable/disable system chaincode without
	// having to remove entry
	Enabled bool

	// if syscc is enforced, register process throw error when this code is not
	// allowed on whitelist
	Enforced bool

	//Unique name of the system chaincode
	Name string

	//Path to the system chaincode; currently not used
	Path string

	//InitArgs initialization arguments to startup the system chaincode,
	//notice this should include the function name as the first arg
	InitArgs [][]byte

	// Chaincode is the actual chaincode object
	Chaincode shim.Chaincode
}

// RegisterSysCC registers the given system chaincode with the peer
func RegisterAndLaunchSysCC(ctx context.Context, syscc *SystemChaincode, ledger ...*ledger.Ledger) error {

	syschain := chaincode.SystemChain
	chainplatform := chaincode.GetChain(syschain)
	if chainplatform == nil {
		return fmt.Errorf("system chain platform is not ready")
	}

	regpath, err := embedded.RegisterSysCC(syscc.Name, syscc.Chaincode, syschain)
	if err != nil {
		return err
	}

	deployspec := &protos.ChaincodeSpec{
		Type:        protos.ChaincodeSpec_Type(protos.ChaincodeSpec_Type_value["GOLANG"]),
		ChaincodeID: &protos.ChaincodeID{Path: regpath, Name: syscc.Name},
		CtorMsg:     &protos.ChaincodeInput{Args: syscc.InitArgs},
	}

	for _, l := range ledger {
		//system chaincode should not change state in deploy entry
		//so SuccessWithOutput is consider as error
		if err := embedded.DeployEcc(ctx, l, chainplatform,
			&protos.ChaincodeDeploymentSpec{
				ExecEnv:       protos.ChaincodeDeploymentSpec_SYSTEM,
				ChaincodeSpec: deployspec,
			}); err != nil {
			return err
		}
	}

	return nil

}
