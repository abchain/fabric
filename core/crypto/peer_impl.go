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
	"crypto/ecdsa"
	"crypto/x509"
	"fmt"
	"sync"

	"github.com/abchain/fabric/core/crypto/primitives"
	"github.com/abchain/fabric/core/crypto/utils"
	obc "github.com/abchain/fabric/protos"
)

type peerImpl struct {
	*nodeImpl

	nodeEnrollmentCertificatesMutex sync.RWMutex
	nodeEnrollmentCertificates      map[string]*x509.Certificate
}

// Public methods

// GetID returns this peer's identifier
func (peer *peerImpl) GetID() []byte {
	return utils.Clone(peer.id)
}

// GetEnrollmentID returns this peer's enrollment id
func (peer *peerImpl) GetEnrollmentID() string {
	return peer.enrollID
}

// TransactionPreValidation verifies that the transaction is
// well formed with the respect to the security layer
// prescriptions (i.e. signature verification).
func (peer *peerImpl) TransactionPreValidation(tx *obc.Transaction) (*obc.Transaction, error) {
	if !peer.IsInitialized() {
		return nil, utils.ErrNotInitialized
	}

	//	peer.debug("Pre validating [%s].", tx.String())
	peer.Debugf("Tx confdential level [%s].", tx.ConfidentialityLevel.String())

	if tx.Cert == nil {
		return tx, utils.ErrTransactionCertificate
	}

	if tx.Signature == nil {
		return tx, utils.ErrTransactionSignature
	}

	return tx, peer.tx_validate(tx)
}

// TransactionPreValidation verifies that the transaction is
// well formed with the respect to the security layer
// prescriptions (i.e. signature verification). If this is the case,
// the method prepares the transaction to be executed.
func (peer *peerImpl) TransactionPreExecution(tx *obc.Transaction) (*obc.Transaction, error) {
	return nil, utils.ErrNotImplemented
}

// Sign signs msg with this validator's signing key and outputs
// the signature if no error occurred.
func (peer *peerImpl) Sign(msg []byte) ([]byte, error) {
	return peer.signWithEnrollmentKey(msg)
}

// Verify checks that signature if a valid signature of message under vkID's verification key.
// If the verification succeeded, Verify returns nil meaning no error occurred.
// If vkID is nil, then the signature is verified against this validator's verification key.
func (peer *peerImpl) Verify(vkID, signature, message []byte) error {
	if len(vkID) == 0 {
		return fmt.Errorf("Invalid peer id. It is empty.")
	}
	if len(signature) == 0 {
		return fmt.Errorf("Invalid signature. It is empty.")
	}
	if len(message) == 0 {
		return fmt.Errorf("Invalid message. It is empty.")
	}

	cert, err := peer.getEnrollmentCert(vkID)
	if err != nil {
		peer.Errorf("Failed getting enrollment cert for [% x]: [%s]", vkID, err)

		return err
	}

	vk := cert.PublicKey.(*ecdsa.PublicKey)

	ok, err := peer.verify(vk, message, signature)
	if err != nil {
		peer.Errorf("Failed verifying signature for [% x]: [%s]", vkID, err)

		return err
	}

	if !ok {
		peer.Errorf("Failed invalid signature for [% x]", vkID)

		return utils.ErrInvalidSignature
	}

	return nil
}

func (peer *peerImpl) GetStateEncryptor(deployTx, invokeTx *obc.Transaction) (StateEncryptor, error) {
	return nil, utils.ErrNotImplemented
}

func (peer *peerImpl) GetTransactionBinding(tx *obc.Transaction) ([]byte, error) {
	return primitives.Hash(append(tx.Cert, tx.Nonce...)), nil
}

// Private methods

func (peer *peerImpl) register(eType NodeType, name string, pwd []byte, enrollID, enrollPWD string, regFunc registerFunc) error {

	if err := peer.nodeImpl.register(eType, name, pwd, enrollID, enrollPWD, regFunc); err != nil {
		peer.Errorf("Failed registering peer [%s]: [%s]", enrollID, err)
		return err
	}

	return nil
}

func (peer *peerImpl) init(eType NodeType, id string, pwd []byte, initFunc initalizationFunc) error {

	peerInitFunc := func(eType NodeType, name string, pwd []byte) error {
		// Initialize keystore
		peer.Debug("Init keystore...")
		err := peer.initKeyStore()
		if err != nil {
			if err != utils.ErrKeyStoreAlreadyInitialized {
				peer.Error("Keystore already initialized.")
			} else {
				peer.Errorf("Failed initiliazing keystore [%s].", err)

				return err
			}
		}
		peer.Debug("Init keystore...done.")

		// EnrollCerts
		peer.nodeEnrollmentCertificates = make(map[string]*x509.Certificate)

		if initFunc != nil {
			return initFunc(eType, id, pwd)
		}

		return nil
	}

	if err := peer.nodeImpl.init(eType, id, pwd, peerInitFunc); err != nil {
		return err
	}

	return nil
}

func (peer *peerImpl) close() error {
	return peer.nodeImpl.close()
}
