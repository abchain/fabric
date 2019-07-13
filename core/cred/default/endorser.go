package cred_default

/*
	a default tx validator for txnetwork, this is supposed to be compatible with
	many possible endorsement schemes base on x.509 certificate, including the
	old 0.6 membersrvc's tcert(with the intrinsic kdf being disabled)

	we purpose a key derivation algorithm similar to the tcert derivation in 0.6
	but replace the tcertid with nounce of tx, and the KDF is come from the gossip
	network id.
*/

import (
	"crypto"
	"crypto/rand"
	"crypto/x509"
	pb "github.com/abchain/fabric/protos"

	cred "github.com/abchain/fabric/core/cred"
)

type localTxEndorserDefault struct {
	*x509.Certificate
	txKey crypto.Signer
	attrs []string
}

func NewEndorser(cer *x509.Certificate, key crypto.PrivateKey) (*localTxEndorserDefault, error) {

	signer, ok := key.(crypto.Signer)
	if !ok {
		logger.Errorf("incoming key [%T] could not cast to signer", key)
		return nil, ErrInvalidKey
	}

	ret := new(localTxEndorserDefault)
	ret.Certificate = cer
	ret.txKey = signer
	logger.Debugf("Create new tx endorser by cert [%s]", cer.Subject.CommonName)
	return ret, nil
}

func (txe *localTxEndorserDefault) EndorserId() []byte {
	return derivedSubjectKeyID(txe.RawSubjectPublicKeyInfo)
}

func (txe *localTxEndorserDefault) EndorsePeerState(st *pb.PeerTxState) (*pb.PeerTxState, error) {

	//additional information can be embedded into certification
	_, hash := endorseAlgorithm(txe.PublicKeyAlgorithm)

	hf := hash.New()
	smsg, err := st.MsgEncode(txe.SubjectKeyId)
	if err != nil {
		return nil, err
	} else if _, err = hf.Write(smsg); err != nil {
		return nil, err
	}

	st.Signature, err = txe.txKey.Sign(rand.Reader, hf.Sum(nil), hash)
	if err != nil {
		logger.Errorf("do peerstate endorsing signature fail: %s", err)
		return nil, err
	}
	st.Endorsement = txe.Raw

	return st, nil
}

func (txe *localTxEndorserDefault) GetEndorser(attr ...string) (cred.TxEndorser, error) {
	if len(attr) > 0 {
		return nil, ErrNotImplemented
	}

	if txe.PublicKeyAlgorithm != x509.ECDSA {
		logger.Errorf("the keyalog (%s) for endorser is not ECDSA", txe.PublicKeyAlgorithm)
		return nil, ErrInvalidKey
	}

	return txe, nil
}

func (txe *localTxEndorserDefault) EndorseTransaction(tx *pb.Transaction) (*pb.Transaction, error) {

	dprik, err := txCertPrivKDerivationEC(txe.txKey, txe.SubjectKeyId, tx.GetNonce())
	if err != nil {
		return nil, err
	}

	dig, err := tx.Digest()
	if err != nil {
		return nil, err
	}

	_, hash := endorseAlgorithm(txe.PublicKeyAlgorithm)
	hf := hash.New()
	if _, err = hf.Write(dig); err != nil {
		return nil, err
	}

	tx.Signature, err = dprik.Sign(rand.Reader, hf.Sum(nil), nil)
	if err != nil {
		logger.Errorf("do tx endorsing signature fail: %s", err)
		return nil, err
	}

	logger.Debugf("Sign with derived key (pkx: %s), digest %X", dprik.PublicKey.X, dig)

	return tx, nil
}

func (txe *localTxEndorserDefault) Release() {}
