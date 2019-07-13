package cred_default

import (
	"crypto/x509"
	"encoding/asn1"
)

var (
	HyperledgerFabricAttrHId = asn1.ObjectIdentifier{1, 2, 3, 4, 5, 6, 9}
	HyperledgerFabricTCertId = asn1.ObjectIdentifier{1, 2, 3, 4, 5, 6, 7}
)

/*
	x509 verifier verify a x509 chain, require all of the critical ext must also
	be included in root CA (MUST match), the immediate CA is omitted
*/
type x509ExtVerifier struct {
	rootCA    *x509.CertPool
	rootCache map[*x509.Certificate]*ObjIdIndex

	immeCAPool *x509.CertPool
}

func NewX509ExtVerifer(certs []*x509.Certificate) *x509ExtVerifier {
	ret := &x509ExtVerifier{
		rootCA:    x509.NewCertPool(),
		rootCache: make(map[*x509.Certificate]*ObjIdIndex),
	}

	for _, cert := range certs {
		ind := new(ObjIdIndex)
		for _, ext := range cert.Extensions {
			//ext.value is nonsense but we just add it ...
			ind.AddItem(ext.Value, ext.Id)
		}
		// add the hyperledger id to be compatible even with the old tcerts from membersrvc
		ind.AddItem(true, HyperledgerFabricAttrHId)
		ind.AddItem(true, HyperledgerFabricTCertId)

		ret.rootCA.AddCert(cert)
		ret.rootCache[cert] = ind
	}

	return ret
}

func (v *x509ExtVerifier) CloneRootVerifer() *x509ExtVerifier {
	return &x509ExtVerifier{
		rootCA:    v.rootCA,
		rootCache: v.rootCache,
	}
}

func (v *x509ExtVerifier) DuplicateImmediateCA(another *x509ExtVerifier) {

	allentries := another.immeCAPool.Subjects()

	v.immeCAPool = x509.NewCertPool()
	for _, pem := range allentries {
		v.immeCAPool.AppendCertsFromPEM(pem)
	}
}

func (v *x509ExtVerifier) SetImmediateCAs(certs []*x509.Certificate) {

	if v.immeCAPool == nil {
		v.immeCAPool = x509.NewCertPool()
	}

	for _, ca := range certs {
		v.immeCAPool.AddCert(ca)
	}
}

/* return the FISRT chain with matched ext, if not found matched chain, return nil, and error indicate it*/
func (v *x509ExtVerifier) Verify(cert *x509.Certificate) ([][]*x509.Certificate, error) {

	opt := x509.VerifyOptions{}

	if v == nil {
		opt.Roots = x509.NewCertPool()
		opt.Roots.AddCert(cert)
	} else {
		opt.Roots = v.rootCA
		opt.Intermediates = v.immeCAPool
	}

	unhandledExt := cert.UnhandledCriticalExtensions
	cert.UnhandledCriticalExtensions = nil
	defer func() {
		cert.UnhandledCriticalExtensions = unhandledExt
	}()

	return cert.Verify(opt)
}

/* return the FISRT chain with matched ext, if not found matched chain, return nil, and error indicate it*/
func (v *x509ExtVerifier) VerifyWithAttr(cert *x509.Certificate) (chains [][]*x509.Certificate, matched []*x509.Certificate, err error) {

	chains, err = v.Verify(cert)
	if err != nil {
		return
	}

	for _, chain := range chains {
		ca := chain[len(chain)-1]
		r, ok := v.rootCache[ca]
		if !ok {
			//something wrong? but we just skip it...
			logger.Warningf("Could not found root cert in cache, something wrong? [%v]",
				ca.Subject)
			continue
		}

		for _, extid := range chain[0].UnhandledCriticalExtensions {
			if _, ok, _ = r.GetItem(extid); !ok {
				//ext is not matched
				break
			}
		}
		if ok {
			matched = chain
			break
		}
	}

	if matched == nil {
		err = x509.UnhandledCriticalExtension{}
		return
	}

	return
}
