package cred_default

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"encoding/binary"
	"math/big"
	"sync"
	"time"

	cred "github.com/abchain/fabric/core/cred"
	pb "github.com/abchain/fabric/protos"
)

func PeerMessageEncoding(msg *pb.Message) []byte {

	encBt := make([]byte, len(msg.Payload)+12)

	binary.BigEndian.PutUint32(encBt, uint32(msg.Type))
	binary.BigEndian.PutUint64(encBt[4:], uint64(msg.Timestamp.GetSeconds()))
	copy(encBt[12:], msg.Payload)

	return encBt
}

type peerCredentialDefault struct {
	cert        *x509.Certificate
	sharedKey   []byte
	workSignAlg x509.SignatureAlgorithm
}

func (pc peerCredentialDefault) Cred() []byte {
	return pc.cert.Raw
}

func (pc peerCredentialDefault) Secret() []byte {
	return pc.sharedKey
}

func endorseAlgorithm(pkalg x509.PublicKeyAlgorithm) (x509.SignatureAlgorithm, crypto.Hash) {
	switch pkalg {
	case x509.RSA:
		return x509.SHA512WithRSA, crypto.SHA512
	case x509.DSA:
		return x509.DSAWithSHA256, crypto.SHA256
	case x509.ECDSA:
		//simply pick the highest bits (the exceess bits is just truncated)
		return x509.ECDSAWithSHA512, crypto.SHA512
	default:
		return x509.UnknownSignatureAlgorithm, crypto.SHA1
	}
}

func (pc peerCredentialDefault) VerifyPeerMsg(msg *pb.Message) error {

	return pc.cert.CheckSignature(pc.workSignAlg,
		PeerMessageEncoding(msg), msg.GetSignature())
}

type localPeersCredentialDefault struct {
	sync.RWMutex
	peerCredentialDefault

	verifier *x509ExtVerifier
	peerKey  crypto.Signer
}

func NewPeerCredential(cer *x509.Certificate, key crypto.PrivateKey,
	pool []*x509.Certificate) (*localPeersCredentialDefault, error) {

	signer, ok := key.(crypto.Signer)
	if !ok {
		logger.Errorf("incoming key [%T] could not cast to signer", key)
		return nil, ErrInvalidKey
	}

	ret := new(localPeersCredentialDefault)
	ret.peerCredentialDefault.cert = cer
	ret.peerKey = signer
	if len(pool) > 0 {
		if !cer.IsCA {
			logger.Errorf("A cert chain require the input cer is CA")
			return nil, ErrInvalidReference
		}

		ret.verifier = NewX509ExtVerifer(pool)
	}

	logger.Debugf("Create new peer credential by cert [%s]", cer.Subject.CommonName)
	return ret, nil
}

func certTemplateFromCSR(csr *x509.CertificateRequest) *x509.Certificate {

	cer := new(x509.Certificate)
	randIn := make([]byte, 20)
	rand.Read(randIn)
	cer.SerialNumber = big.NewInt(0).SetBytes(randIn)

	cer.NotBefore = time.Now()
	//by default we create certs for 1 hours
	cer.NotAfter = cer.NotBefore.Add(time.Hour)
	cer.Subject = csr.Subject
	cer.DNSNames = csr.DNSNames
	cer.IPAddresses = csr.IPAddresses
	//this field is not avaliable in Go 1.9 yet
	//newcsr.URIs = cer.URIs
	cer.ExtraExtensions = csr.ExtraExtensions

	return cer
}

func (psc *localPeersCredentialDefault) createConnectingPeerCred(cred []byte) (cred.PeerCred, error) {

	csr, err := x509.ParseCertificateRequest(cred)
	if err != nil {
		logger.Errorf("Error on parsing csr: %s", err)
		return nil, err
	}

	if err := csr.CheckSignature(); err != nil {
		logger.Errorf("CSR signature failure: %s", err)
		return nil, ErrInvalidSignature
	}

	salg, _ := endorseAlgorithm(csr.PublicKeyAlgorithm)
	if salg == x509.UnknownSignatureAlgorithm {
		logger.Errorf("far-end's csr use a public key [%s] could not assigned sign alg.",
			csr.PublicKeyAlgorithm)
		return nil, ErrInvalidKey
	}

	sessionCer := certTemplateFromCSR(csr)
	sessionCer.AuthorityKeyId = psc.cert.SubjectKeyId

	cerder, err := x509.CreateCertificate(rand.Reader, sessionCer,
		psc.cert, csr.PublicKey, psc.peerKey)
	if err != nil {
		logger.Errorf("Error on creating session certificate: %s", err)
		return nil, err
	}

	//this should not wrong for we just create it
	cer, _ := x509.ParseCertificate(cerder)
	if cer == nil {
		panic("x509 lib create error content")
	}

	//TODO: now we just use the series number in CSR as shared key, some
	//key-exchange scheme should be applied

	return peerCredentialDefault{cer, cer.SerialNumber.Bytes(), salg}, nil
}

func (psc *localPeersCredentialDefault) CreatePeerCred(cred []byte, pki []byte) (cred.PeerCred, error) {

	if pki == nil {
		return psc.createConnectingPeerCred(cred)
	}

	cer, err := x509.ParseCertificate(pki)
	if err != nil {
		logger.Errorf("Error on parsing far-end's cert.: %s", err)
		return nil, err
	}

	_, err = psc.verifier.Verify(cer)
	if err != nil {
		logger.Errorf("verify far-end's cert. fail: %s", err)
		return nil, err
	}

	sessionCer, err := x509.ParseCertificate(cred)
	if err != nil {
		logger.Errorf("Error on parsing provided session cert.: %s", err)
		return nil, err
	}

	//sessionCer is not verified, it is just check by both-side's cert
	if bytes.Compare(sessionCer.RawSubjectPublicKeyInfo, psc.cert.RawSubjectPublicKeyInfo) != 0 {
		logger.Errorf("Far-end provided session cert which is not from us! (%X)",
			sessionCer.RawSubjectPublicKeyInfo)
		return nil, ErrInvalidReference
	}

	if err = sessionCer.CheckSignatureFrom(cer); err != nil {
		logger.Errorf("Invalid sig from far-end: %s", err)
		return nil, err
	}

	salg, _ := endorseAlgorithm(cer.PublicKeyAlgorithm)
	if salg == x509.UnknownSignatureAlgorithm {
		logger.Errorf("far-end's cert use a public key [%s] could not assigned sign alg.",
			cer.PublicKeyAlgorithm)
		return nil, ErrInvalidKey
	}

	return peerCredentialDefault{cer, sessionCer.SerialNumber.Bytes(), salg}, nil
}

func requestFromCurCert(cer *x509.Certificate) *x509.CertificateRequest {

	newcsr := new(x509.CertificateRequest)
	newcsr.Subject = cer.Subject
	newcsr.DNSNames = cer.DNSNames
	newcsr.IPAddresses = cer.IPAddresses
	//this field is not avaliable in Go 1.9 yet
	//newcsr.URIs = cer.URIs
	newcsr.ExtraExtensions = cer.ExtraExtensions

	return newcsr
}

func (psc *localPeersCredentialDefault) Cred() []byte {

	csr, err := x509.CreateCertificateRequest(rand.Reader, requestFromCurCert(psc.cert), psc.peerKey)
	if err != nil {
		logger.Errorf("Can not obtain CSR: %s", err)
		return nil
	}

	return csr
}

func (psc *localPeersCredentialDefault) Pki() []byte {
	return psc.cert.Raw
}

func (psc *localPeersCredentialDefault) EndorsePeerMsg(msg *pb.Message) (*pb.Message, error) {

	salg, hash := endorseAlgorithm(psc.cert.PublicKeyAlgorithm)
	if salg == x509.UnknownSignatureAlgorithm {
		logger.Errorf("cert use a public key [%s] could not assigned sign alg.", psc.cert.PublicKeyAlgorithm)
		return nil, ErrInvalidKey
	}

	hf := hash.New()
	_, err := hf.Write(PeerMessageEncoding(msg))
	if err != nil {
		logger.Errorf("hash encode fail: %s", err)
		return nil, err
	}

	//RSA require hash for opt, the sign-verify process is subtle, we may use some
	//more robust implement...
	sig, err := psc.peerKey.Sign(rand.Reader, hf.Sum(nil), hash)
	if err != nil {
		logger.Errorf("do signature fail: %s", err)
		return nil, err
	}
	msg.Signature = sig
	return msg, nil
}
