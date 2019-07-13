package crypto

import (
	"crypto/x509"
	"fmt"
	"github.com/abchain/fabric/core/crypto/primitives"
	"github.com/abchain/fabric/core/crypto/utils"
	obc "github.com/abchain/fabric/protos"
)

//partial stack of tCert
type tCertEndorsment interface {
	GetCertificate() *x509.Certificate
	Sign(msg []byte) ([]byte, error)
}

func endorseTxMsg(tx *obc.Transaction) ([]byte, error) {

	ret, err := tx.Digest()
	if err != nil {
		return nil, fmt.Errorf("Failed digesting tx [%s].", err.Error())
	}

	return ret, nil
}

func (node *nodeImpl) tx_endorse(tx *obc.Transaction, endorser tCertEndorsment) (*obc.Transaction, error) {
	// Sign the transaction

	// Append the certificate to the transaction
	node.Debugf("Appending certificate [% x].", endorser.GetCertificate().Raw)
	tx.Cert = endorser.GetCertificate().Raw

	// Sign the transaction and append the signature
	// 1. Marshall tx to bytes
	msg, err := endorseTxMsg(tx)
	if err != nil {
		node.Errorf("Get endorse msg fail", err.Error())
		return nil, err
	}

	// 2. Sign rawTx and check signature
	rawSignature, err := endorser.Sign(msg)
	if err != nil {
		node.Errorf("Failed creating signature [% x]: [%s].", rawSignature, err.Error())
		return nil, err
	}

	// 3. Append the signature
	tx.Signature = rawSignature

	node.Debugf("Appending signature [% x].", rawSignature)

	return tx, nil
}

func (node *nodeImpl) tx_validate(tx *obc.Transaction) error {
	// Verify the transaction
	// 1. Unmarshal cert
	cert, err := primitives.DERToX509Certificate(tx.Cert)
	if err != nil {
		node.Errorf("Failed unmarshalling cert [%s].", err.Error())
		return err
	}

	// Verify transaction certificate against root
	// DER to x509
	x509Cert, err := primitives.DERToX509Certificate(tx.Cert)
	if err != nil {
		node.Debugf("Failed parsing certificate [% x]: [%s].", tx.Cert, err)

		return err
	}

	// 1. Get rid of the extensions that cannot be checked now
	x509Cert.UnhandledCriticalExtensions = nil
	// 2. Check against TCA certPool
	if _, err = primitives.CheckCertAgainRoot(x509Cert, node.tcaCertPool); err != nil {
		node.Warningf("Failed verifing certificate against TCA cert pool [%s].", err.Error())
		// 3. Check against ECA certPool, if this check also fails then return an error
		if _, err = primitives.CheckCertAgainRoot(x509Cert, node.ecaCertPool); err != nil {
			node.Warningf("Failed verifing certificate against ECA cert pool [%s].", err.Error())

			return fmt.Errorf("Certificate has not been signed by a trusted authority. [%s]", err)
		}
	}

	// 3. Marshall tx without signature
	msg, err := endorseTxMsg(tx)
	if err != nil {
		node.Errorf("Get endorse msg fail [%s]", err.Error())
		return err
	}

	// 2. Verify signature
	ok, err := node.verify(cert.PublicKey, msg, tx.Signature)
	if err != nil {
		node.Errorf("Verify tx signature fail [%s].", err.Error())
		return err
	}

	if !ok {
		return utils.ErrInvalidTransactionSignature
	}

	return nil
}
