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

package crypto

import (
	"github.com/abchain/fabric/core/crypto/primitives"
	"github.com/abchain/fabric/core/crypto/utils"
	obc "github.com/abchain/fabric/protos"
)

func (client *clientImpl) createTransactionNonce() ([]byte, error) {
	nonce, err := primitives.GetRandomNonce()
	if err != nil {
		client.Errorf("Failed creating nonce [%s].", err.Error())
		return nil, err
	}

	return nonce, err
}

func (client *clientImpl) createDeployTx(chaincodeDeploymentSpec *obc.ChaincodeDeploymentSpec, uuid string, nonce []byte) (*obc.Transaction, error) {
	// Create a new transaction
	tx, err := obc.NewChaincodeDeployTransaction(chaincodeDeploymentSpec, uuid)
	if err != nil {
		client.Errorf("Failed creating new transaction [%s].", err.Error())
		return nil, err
	}

	return client.handleInnerCreation(chaincodeDeploymentSpec.GetChaincodeSpec(), nonce, tx)
}

func getMetadata(chaincodeSpec *obc.ChaincodeSpec, tCert tCert) ([]byte, error) {
	//TODO this code is being commented due temporarily is not enabled attributes encryption.
	/*
		isAttributesEnabled := viper.GetBool("security.attributes.enabled")
		if !isAttributesEnabled {
			return chaincodeSpec.Metadata, nil
		}

		if tCert == nil {
			return nil, errors.New("Invalid TCert.")
		}

		return attributes.CreateAttributesMetadata(tCert.GetCertificate().Raw, chaincodeSpec.Metadata, tCert.GetPreK0(), attrs)
	*/
	return chaincodeSpec.Metadata, nil
}

func (client *clientImpl) handleInnerCreation(chaincodespec *obc.ChaincodeSpec, nonce []byte, tx *obc.Transaction) (txr *obc.Transaction, err error) {

	// ** YA-fabric have abandoned attributes encryption
	// // Copy metadata from ChaincodeSpec
	// tx.Metadata, err = getMetadata(chaincodeInvocation.GetChaincodeSpec(), tCert, attrs...)
	// if err != nil {
	// 	client.Errorf("Failed creating new transaction [%s].", err.Error())
	// 	return nil, err
	// }
	txr = tx

	if nonce == nil {
		tx.Nonce, err = primitives.GetRandomNonce()
		if err != nil {
			client.Errorf("Failed creating nonce [%s].", err.Error())
			return
		}
	} else {
		tx.Nonce = nonce
	}

	// Handle confidentiality
	if chaincodespec.ConfidentialityLevel == obc.ConfidentialityLevel_CONFIDENTIAL {
		// 1. set confidentiality level and nonce
		tx.ConfidentialityLevel = obc.ConfidentialityLevel_CONFIDENTIAL

		// 2. set confidentiality protocol version
		tx.ConfidentialityProtocolVersion = client.conf.GetConfidentialityProtocolVersion()

		// 3. encrypt tx
		err = client.encryptTx(tx)
		if err != nil {
			client.Errorf("Failed encrypting payload [%s].", err.Error())
			return
		}
	}

	return
}

func (client *clientImpl) createExecuteTx(chaincodeInvocation *obc.ChaincodeInvocationSpec, uuid string, nonce []byte) (*obc.Transaction, error) {
	/// Create a new transaction
	tx, err := obc.NewChaincodeExecute(chaincodeInvocation, uuid, obc.Transaction_CHAINCODE_INVOKE)
	if err != nil {
		client.Errorf("Failed creating new transaction [%s].", err.Error())
		return nil, err
	}

	return client.handleInnerCreation(chaincodeInvocation.GetChaincodeSpec(), nonce, tx)
}

func (client *clientImpl) createQueryTx(chaincodeInvocation *obc.ChaincodeInvocationSpec, uuid string, nonce []byte) (*obc.Transaction, error) {
	// Create a new transaction
	tx, err := obc.NewChaincodeExecute(chaincodeInvocation, uuid, obc.Transaction_CHAINCODE_QUERY)
	if err != nil {
		client.Errorf("Failed creating new transaction [%s].", err.Error())
		return nil, err
	}

	return client.handleInnerCreation(chaincodeInvocation.GetChaincodeSpec(), nonce, tx)
}

func (client *clientImpl) newChaincodeDeployUsingTCert(chaincodeDeploymentSpec *obc.ChaincodeDeploymentSpec, uuid string, tCert tCert, nonce []byte) (*obc.Transaction, error) {
	// Create a new transaction
	tx, err := client.createDeployTx(chaincodeDeploymentSpec, uuid, nonce)
	if err != nil {
		client.Errorf("Failed creating new deploy transaction [%s].", err.Error())
		return nil, err
	}

	return client.tx_endorse(tx, tCert)
}

func (client *clientImpl) newChaincodeExecuteUsingTCert(chaincodeInvocation *obc.ChaincodeInvocationSpec, uuid string, tCert tCert, nonce []byte) (*obc.Transaction, error) {
	/// Create a new transaction
	tx, err := client.createExecuteTx(chaincodeInvocation, uuid, nonce)
	if err != nil {
		client.Errorf("Failed creating new execute transaction [%s].", err.Error())
		return nil, err
	}

	return client.tx_endorse(tx, tCert)
}

func (client *clientImpl) newChaincodeQueryUsingTCert(chaincodeInvocation *obc.ChaincodeInvocationSpec, uuid string, tCert tCert, nonce []byte) (*obc.Transaction, error) {
	// Create a new transaction
	tx, err := client.createQueryTx(chaincodeInvocation, uuid, nonce)
	if err != nil {
		client.Errorf("Failed creating new query transaction [%s].", err.Error())
		return nil, err
	}

	return client.tx_endorse(tx, tCert)
}

func (client *clientImpl) newChaincodeDeployUsingECert(chaincodeDeploymentSpec *obc.ChaincodeDeploymentSpec, uuid string, nonce []byte) (*obc.Transaction, error) {
	// Create a new transaction
	tx, err := client.createDeployTx(chaincodeDeploymentSpec, uuid, nonce)
	if err != nil {
		client.Errorf("Failed creating new deploy transaction [%s].", err.Error())
		return nil, err
	}

	return client.tx_endorse(tx, &ecertAsTCert{client.nodeImpl})
}

func (client *clientImpl) newChaincodeExecuteUsingECert(chaincodeInvocation *obc.ChaincodeInvocationSpec, uuid string, nonce []byte) (*obc.Transaction, error) {
	/// Create a new transaction
	tx, err := client.createExecuteTx(chaincodeInvocation, uuid, nonce)
	if err != nil {
		client.Errorf("Failed creating new execute transaction [%s].", err.Error())
		return nil, err
	}
	return client.tx_endorse(tx, &ecertAsTCert{client.nodeImpl})
}

func (client *clientImpl) newChaincodeQueryUsingECert(chaincodeInvocation *obc.ChaincodeInvocationSpec, uuid string, nonce []byte) (*obc.Transaction, error) {
	// Create a new transaction
	tx, err := client.createQueryTx(chaincodeInvocation, uuid, nonce)
	if err != nil {
		client.Errorf("Failed creating new query transaction [%s].", err.Error())
		return nil, err
	}

	return client.tx_endorse(tx, &ecertAsTCert{client.nodeImpl})
}

// CheckTransaction is used to verify that a transaction
// is well formed with the respect to the security layer
// prescriptions. To be used for internal verifications.
func (client *clientImpl) checkTransaction(tx *obc.Transaction) error {
	if !client.isInitialized {
		return utils.ErrNotInitialized
	}

	if tx.Cert == nil || tx.Signature == nil {
		return utils.ErrTransactionMissingCert
	}

	return client.tx_validate(tx)
}
