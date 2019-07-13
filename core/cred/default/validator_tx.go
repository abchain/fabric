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
	"crypto/ecdsa"
	"crypto/x509"
	"github.com/abchain/fabric/core/crypto/primitives"
	pb "github.com/abchain/fabric/protos"
	"sync"
)

type txHandlerDefault struct {
	sync.RWMutex
	verifier   *x509ExtVerifier
	disableKDF bool
	cache      map[string]*txVerifier
	certcache  map[string]*x509.Certificate

	converter func([]byte) string
}

func NewDefaultTxHandler(disableKDF bool) *txHandlerDefault {
	logger.Debugf("Create tx validator with KDF diabled <%v>", disableKDF)
	return &txHandlerDefault{
		disableKDF: disableKDF,
		cache:      make(map[string]*txVerifier),
		certcache:  make(map[string]*x509.Certificate),
	}
}

func (f *txHandlerDefault) UpdateRootCA(CAs []*x509.Certificate, immeCAs []*x509.Certificate) {

	logger.Debugf("Update rootCA for tx validator with %d root cert and %d immediate cert",
		len(CAs), len(immeCAs))

	verifier := NewX509ExtVerifer(CAs)
	var immePool *x509.CertPool
	if len(immeCAs) != 0 {
		immePool = x509.NewCertPool()
		for _, ca := range immeCAs {
			immePool.AddCert(ca)
		}
	}

	if len(immeCAs) > 0 {
		verifier.SetImmediateCAs(immeCAs)
	}

	f.Lock()
	//when verifier is changed, all the cache must be also cleared
	f.cache = make(map[string]*txVerifier)
	f.verifier = verifier
	f.Unlock()
}

func (f *txHandlerDefault) SetIdConverter(fc func([]byte) string) {
	f.converter = fc
}

func (f *txHandlerDefault) ValidatePeerStatus(id string, status *pb.PeerTxState) (err error) {

	if f.converter == nil {
		logger.Errorf("Converter is not set yet")
		return ErrNotInitialized
	}

	var cer *x509.Certificate

	if len(status.Endorsement) == 0 {

		f.RLock()
		cachedCert := f.certcache[id]
		f.RUnlock()

		if cachedCert == nil {
			logger.Errorf("Status for %d has no certificate and we have no cache", id)
			return ErrCertificate
		}

		cer = cachedCert

	} else {

		cer, err = x509.ParseCertificate(status.Endorsement)
		if err != nil {
			return
		} else if len(cer.UnhandledCriticalExtensions) > 0 {
			//the peer's cert must not include extension
			return x509.UnhandledCriticalExtension{}
		}

		logger.Infof("Verify a new peer's x509 certificate [%s]", cer.Subject.CommonName)
		_, err = f.verifier.Verify(cer)
		if err != nil {
			return err
		}

		defer func() {
			if err != nil {
				return
			}
			//update cache, and rebuild verifier
			f.Lock()

			f.certcache[id] = cer
			logger.Debugf("Peer [%s]'s certificated has been updated to %s", id, cer.SerialNumber)

			f.Unlock()
		}()
	}

	peerId := derivedSubjectKeyID(cer.RawSubjectPublicKeyInfo)
	logger.Debugf("Derived peer's id: %X", peerId)

	//else we checked if subkey is mathed with id
	if drvid := f.converter(peerId); id != drvid {
		logger.Errorf("Id derived from cert (%s) is not matched (expected %s)", drvid, id)
		return ErrCertificate
	}

	var smsg []byte
	smsg, err = status.MsgEncode(peerId)
	if err != nil {
		return
	}

	alg, _ := endorseAlgorithm(cer.PublicKeyAlgorithm)
	err = cer.CheckSignature(alg, smsg, status.Signature)
	if err != nil {
		return
	}

	return
}

func (f *txHandlerDefault) GetValidator(id string) pb.TxPreHandler {

	h, err := f.assignPreHandler(id)

	if err != nil {
		return pb.FailureHandler{err}
	} else {
		return h
	}

}

func (f *txHandlerDefault) assignPreHandler(id string) (*txVerifier, error) {

	f.RLock()

	if ret, ok := f.cache[id]; !ok {

		f.RUnlock()
		f.Lock()
		defer f.Unlock()
		cert, ok := f.certcache[id]
		if !ok {
			return nil, ErrTransactionMissingCert
		}

		//rebuild cache
		ret = newVerifier(id, cert, f)
		f.cache[id] = ret
		logger.Debugf("Assigned tx verifier for peer [%s]", id)

		return ret, nil

	} else {
		f.RUnlock()
		return ret, nil
	}
}

func (f *txHandlerDefault) RemovePreValidator(id string) {
	f.Lock()
	defer f.Unlock()

	delete(f.cache, id)
	delete(f.certcache, id)
}

func newVerifier(id string, cert *x509.Certificate, parent *txHandlerDefault) *txVerifier {

	ret := new(txVerifier)
	//the input cert has been parsed by us so it must be correct
	ret.ecert, _ = x509.ParseCertificate(cert.Raw)
	ret.endorserID = derivedSubjectKeyID(cert.RawSubjectPublicKeyInfo)
	ret.eid = id
	if parent.verifier != nil {
		ret.verifier = parent.verifier.CloneRootVerifer()
		ret.verifier.DuplicateImmediateCA(parent.verifier)
		if cert.IsCA {
			ret.verifier.SetImmediateCAs([]*x509.Certificate{cert})
		}
	}

	ret.disableKDF = parent.disableKDF

	return ret
}

type txVerifier struct {
	eid        string
	verifier   *x509ExtVerifier
	disableKDF bool
	ecert      *x509.Certificate
	endorserID []byte
}

func (v *txVerifier) Handle(txe *pb.TransactionHandlingContext) (*pb.TransactionHandlingContext, error) {

	//complete the security context
	if txe.SecContex == nil {
		txe.SecContex = new(pb.ChaincodeSecurityContext)
	}

	err := v.doValidate(txe)
	if err != nil {
		return nil, err
	}

	txe.PeerID = v.eid
	txe.SecContex.CallerSign = txe.Signature
	//set the default time-stamp (maybe overwritten later)
	txe.SecContex.TxTimestamp = txe.Timestamp
	//create binding
	txe.SecContex.Binding = primitives.Hash(append(txe.SecContex.CallerCert, txe.GetNonce()...))
	txe.SecContex.Metadata = txe.Metadata

	return txe, nil
}

func (v *txVerifier) doValidate(txe *pb.TransactionHandlingContext) error {

	tx := txe.Transaction

	//the key derivation from tcert protocol
	certPubkey, err := txCertKeyDerivationEC(v.ecert.PublicKey.(*ecdsa.PublicKey),
		v.endorserID, tx.GetNonce())
	if err != nil {
		return err
	}

	var refCert *x509.Certificate

	if tx.Cert != nil {
		refCert, err = primitives.DERToX509Certificate(tx.Cert)
		if err != nil {
			return err
		}

		if v.verifier == nil {
			return x509.UnknownAuthorityError{Cert: refCert}
		}

		_, _, err = v.verifier.VerifyWithAttr(refCert)
		if err != nil {
			return err
		}

		txcertPk, ok := refCert.PublicKey.(*ecdsa.PublicKey)
		if !ok {
			logger.Errorf("Tcert not use ecdsa key")
			return ErrInvalidKey
		}

		if !v.disableKDF {
			//must verify the derived key and the key in cert
			//we just compare X for it was almost impossible for attacker to reuse
			//a cert which pubkey has same X but different Y
			if txcertPk.X.Cmp(certPubkey.X) != 0 {
				logger.Errorf("Pubkey of tx cert is not matched to the derived key")
				return ErrInvalidKey
			}

		} else {
			//case for compatible, just drop the drived key and use the cert one
			certPubkey = txcertPk
		}

		txe.SecContex.CallerCert = tx.Cert

	} else {
		refCert = v.ecert
		txe.SecContex.CallerCert = v.ecert.Raw

		//NOTICE:
		//each tx validating prehandler has a copy of ecert, and will be worked
		//in an sequence (non-concurrent) way so we can do this trick
		//(else we have to assigned another copy here)
		defer func(pk interface{}) {
			v.ecert.PublicKey = pk
		}(refCert.PublicKey)
		refCert.PublicKey = certPubkey
	}

	txmsg, err := tx.Digest()
	if err != nil {
		return err
	}

	logger.Debugf("Verify with derived pubkey %s, digest %X", certPubkey.X, txmsg)

	alg, _ := endorseAlgorithm(refCert.PublicKeyAlgorithm)
	err = refCert.CheckSignature(alg, txmsg, tx.GetSignature())
	if err != nil {
		return err
	}
	return nil
}
