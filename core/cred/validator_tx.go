package credentials

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
	"errors"
	"github.com/abchain/fabric/core/crypto"
	"github.com/abchain/fabric/core/crypto/primitives"
	"github.com/abchain/fabric/core/crypto/utils"
	pb "github.com/abchain/fabric/protos"
	"sync"
)

type txHandlerDefault struct {
	sync.RWMutex
	rootVerifier *x509ExtVerifier
	immeCAPool   *x509.CertPool
	immeCAs      []*x509.Certificate
	disableKDF   bool
	cache        map[string]*txVerifier
	certcache    map[string]*x509.Certificate
}

func NewDefaultTxHandler(disableKDF bool) TxHandlerFactory {
	return &txHandlerDefault{
		disableKDF: disableKDF,
		cache:      make(map[string]*txVerifier),
	}
}

func (f *txHandlerDefault) UpdateRootCA(CAs []*x509.Certificate, immeCAs []*x509.Certificate) {
	verifier := NewX509ExtVerifer(CAs)
	var immePool *x509.CertPool
	if len(immeCAs) != 0 {
		immePool = x509.NewCertPool()
		for _, ca := range immeCAs {
			immePool.AddCert(ca)
		}
	}

	f.Lock()
	//when verifier is changed, all the cache must be also cleared
	f.cache = make(map[string]*txVerifier)
	f.rootVerifier = verifier
	f.immeCAs = immeCAs
	f.immeCAPool = immePool
	f.Unlock()
}

func (f *txHandlerDefault) ValidatePeerStatus(id string, status *pb.PeerTxState) (err error) {

	f.RLock()
	cachedCert := f.certcache[id]
	f.RUnlock()

	var ok bool
	var certPubkey *ecdsa.PublicKey
	if len(status.Endorsement) == 0 {
		//we allow to use cached certificate
		f.RLock()
		defer f.RUnlock()

		if cachedCert == nil {
			err = errors.New("Status has no certificate and we have no cache")
			return
		}
		certPubkey = cachedCert.PublicKey.(*ecdsa.PublicKey)
	} else {
		var cer *x509.Certificate
		cer, err = primitives.DERToX509Certificate(status.Endorsement)
		if err != nil {
			return
		} else if len(cer.UnhandledCriticalExtensions) > 0 {
			//the peer's cert must not include extension
			return x509.UnhandledCriticalExtension{}
		}
		_, err = f.rootVerifier.Verify(cer, nil)
		if err != nil {
			return
		}

		certPubkey, ok = cer.PublicKey.(*ecdsa.PublicKey)
		if !ok {
			err = errors.New("Tcert not use ecdsa signature")
			return
		}

		//peer's cert can be updated but we require it must have same
		//pubkey as the original one
		if cachedCert != nil {
			oldpk := cachedCert.PublicKey.(*ecdsa.PublicKey)
			if certPubkey.X.Cmp(oldpk.X) != 0 {
				err = errors.New("Tcert is not match with previous")
				return
			}
		}

		logger.Infof("Verify a new peer's x509 certificate [%x]", cer.SubjectKeyId)

		defer func() {
			if err != nil {
				return
			}
			//update cache, and rebuild verifier
			f.Lock()

			f.certcache[id] = cer
			f.cache[id] = newVerifier(id, cer, f)

			f.Unlock()
		}()
	}

	var smsg []byte
	smsg, err = status.MsgEncode(id)
	if err != nil {
		return
	}

	ok, err = primitives.ECDSAVerify(certPubkey, smsg, status.Signature)
	if err != nil {
		return
	} else if !ok {
		return utils.ErrInvalidTransactionSignature
	}
	return
}

func (f *txHandlerDefault) Handle(txe *pb.TransactionHandlingContext) (*pb.TransactionHandlingContext, error) {

	v, err := f.assignPreHandler(txe.PeerID)
	if err != nil {
		return nil, err
	}

	//complete the security context
	if txe.SecContex == nil {
		txe.SecContex = new(pb.ChaincodeSecurityContext)
	}

	err = v.doValidate(txe)
	if err != nil {
		return nil, err
	}

	txe.SecContex.CallerSign = txe.Signature
	//set the default time-stamp (maybe overwritten later)
	txe.SecContex.TxTimestamp = txe.Timestamp
	//create binding
	txe.SecContex.Binding = primitives.Hash(append(txe.SecContex.CallerCert, txe.GetNonce()...))
	txe.SecContex.Metadata = txe.Metadata

	return txe, nil
}

func (f *txHandlerDefault) assignPreHandler(id string) (*txVerifier, error) {

	f.RLock()

	if ret, ok := f.cache[id]; !ok {

		f.RUnlock()
		f.Lock()
		defer f.Unlock()
		cert, ok := f.certcache[id]
		if !ok {
			return nil, errors.New("Peer has not been verified")
		}

		//rebuild cache
		ret = newVerifier(id, cert, f)
		f.cache[id] = ret

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

func (f *txHandlerDefault) GetValidator(string) pb.TxPreHandler {
	return nil
}

func newVerifier(id string, cert *x509.Certificate, parent *txHandlerDefault) *txVerifier {

	ret := new(txVerifier)
	ret.ecert = cert
	ret.eid = id
	ret.rootVerifier = parent.rootVerifier
	ret.disableKDF = parent.disableKDF
	ret.ecertPool = x509.NewCertPool()

	if len(parent.immeCAs) != 0 {
		for _, ca := range parent.immeCAs {
			ret.ecertPool.AddCert(ca)
		}
	}

	if cert.IsCA {
		ret.ecertPool.AddCert(cert)
	}

	return ret
}

type txVerifier struct {
	eid          string
	rootVerifier *x509ExtVerifier
	disableKDF   bool
	ecert        *x509.Certificate
	ecertPool    *x509.CertPool
}

func (v *txVerifier) doValidate(txe *pb.TransactionHandlingContext) error {

	tx := txe.Transaction

	//the key derivation from tcert protocol
	certPubkey, err := crypto.TxCertKeyDerivationEC(v.ecert.PublicKey.(*ecdsa.PublicKey), []byte(v.eid), tx.GetNonce())
	if err != nil {
		return err
	}

	var refCert *x509.Certificate

	if tx.Cert != nil {
		refCert, err = primitives.DERToX509Certificate(tx.Cert)
		if err != nil {
			return err
		}

		if v.rootVerifier == nil {
			return x509.UnknownAuthorityError{Cert: refCert}
		}

		_, _, err = v.rootVerifier.VerifyWithAttr(refCert, v.ecertPool)
		if err != nil {
			return err
		}

		txcertPk, ok := refCert.PublicKey.(*ecdsa.PublicKey)
		if !ok {
			return errors.New("Tcert not use ecdsa signature")
		}

		if !v.disableKDF {
			//must verify the derived key and the key in cert
			//we just compare X for it was almost impossible for attacker to reuse
			//a cert which pubkey has same X but different Y
			if txcertPk.X.Cmp(certPubkey.X) != 0 {
				return errors.New("Pubkey of tx cert is not matched to the derived key")
			}

		} else {
			//case for compatible, just drop the drived key and use the cert one
			certPubkey = txcertPk
		}

		txe.SecContex.CallerCert = tx.Cert

	} else {
		refCert = v.ecert
		txe.SecContex.CallerCert = v.ecert.Raw
	}

	txmsg, err := crypto.TxToSignatureMsg(tx)
	if err != nil {
		return err
	}

	ok, err := primitives.ECDSAVerify(certPubkey, txmsg, tx.GetSignature())
	if err != nil {
		return err
	} else if !ok {
		return utils.ErrInvalidTransactionSignature
	}

	return nil
}

type TxHandlerFactoryX509 interface {
	TxHandlerFactory
	UpdateRootCA(ca []*x509.Certificate, immeca []*x509.Certificate)
}

//in case if we have a custom txhandler ...
func UpdateRootCAForTxHandler(h_in TxHandlerFactory, CAs []*x509.Certificate, immeCAs ...*x509.Certificate) {
	h, ok := h_in.(TxHandlerFactoryX509)
	if !ok {
		logger.Errorf("Wrong param, not default txhandler")
	}

	h.UpdateRootCA(CAs, immeCAs)
}
