package cred_default

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"github.com/spf13/viper"
	"math/big"
	"time"

	"github.com/abchain/fabric/core/config"
)

//any scheme has unique name and can be specified as "file" (load from files),
//"pem" (embedded data as pem block) or "new" (creating a new, self-signed cert,
//and persisted it locally), if mutiple specification is included, pem is most
//preferred, and new is least
type certScheme struct {
	//the first cert of all certs
	certs []*x509.Certificate
	key   crypto.PrivateKey
}

func filterCAs(cers []*x509.Certificate) (ret []*x509.Certificate) {

	for _, cer := range cers {
		if cer.IsCA {
			ret = append(ret, cer)
		}
	}

	return
}

func (cs *certScheme) getKeyPair() (*x509.Certificate, crypto.PrivateKey, error) {

	if len(cs.certs) == 0 {
		return nil, nil, errors.New("No cert found in specified conf")
	}

	//TODO: we can search for the corresponding certs? but now
	//we just return first cert
	return cs.certs[0], cs.key, nil

}

func (cs *certScheme) RootCerts() []*x509.Certificate {
	return filterCAs(cs.certs)
}

func (cs *certScheme) CertPool() *x509.CertPool {

	pool := x509.NewCertPool()
	for _, cer := range cs.RootCerts() {
		pool.AddCert(cer)
	}

	return pool
}

func (cs *certScheme) loadCersFromFile(cerpath string) error {

	if cerpath == "" {
		return nil
	}

	if pemblks, err := loadPEMFromFile(cerpath); err != nil {
		return err
	} else if cers, err := loadCerts(pemblks); err != nil {
		return err
	} else {
		cs.certs = append(cs.certs, cers...)
	}

	return nil
}

func (cs *certScheme) loadKeyFromFile(keypath string) error {

	if keypath == "" {
		return nil
	}

	if pemblks, err := loadPEMFromFile(keypath); err != nil {
		return err
	} else if len(pemblks) == 0 {
		return errors.New("No PEM block data")
	} else if key, err := loadKey(pemblks[0]); err != nil {
		return err
	} else {
		cs.key = key
	}

	return nil
}

func (cs *certScheme) loadCersFromEmbeddedBlock(cerdata string) error {

	cers, err := loadCerts(loadPEMFromBytes([]byte(cerdata)))
	if err != nil {
		return err
	}

	cs.certs = append(cs.certs, cers...)
	return nil
}

func (cs *certScheme) loadKeyFromEmbeddedBlock(keydata string) error {

	if len(keydata) == 0 {
		return nil
	}

	if pemblks := loadPEMFromBytes([]byte(keydata)); len(pemblks) == 0 {
		return errors.New("No PEM block")
	} else if key, err := loadKey(pemblks[0]); err != nil {
		return err
	} else {
		cs.key = key
	}

	return nil
}

func loadKeypair(content []byte) (key crypto.PrivateKey, cer *x509.Certificate, err error) {
	blkpair := loadPEMFromBytes(content)
	if len(blkpair) < 2 {
		err = errors.New("No paired PEM blocks")
		return
	}

	key, err = loadKey(blkpair[0])
	if err != nil {
		logger.Debugf("Load key fail: %s", err)
		return
	}

	cer, err = loadOneCert(blkpair[1])
	return
}

func encodeKeypair(key *ecdsa.PrivateKey, cer *x509.Certificate) ([]byte, error) {

	keyDER, err := marshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}

	pemblk := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyDER,
	}

	keypem := pem.EncodeToMemory(pemblk)
	if keypem == nil {
		return nil, errors.New("Encode key pem fail")
	}

	//pem.EncodeToMemory
	pemblk.Type = "CERTIFICATE"
	pemblk.Bytes = cer.Raw

	cerpem := pem.EncodeToMemory(pemblk)
	if cerpem == nil {
		return nil, errors.New("Encode cer pem fail")
	}

	return bytes.Join([][]byte{keypem, cerpem}, nil), nil
}

type persistor interface {
	Store(key string, value []byte) error
	Load(key string) ([]byte, error)
}

type CertLoader struct {
	persistor
	preset map[string]*certScheme
}

//oncreate we only use 256bit-ECDSA and select SHA256
func (c CertLoader) createNewScheme(vp *viper.Viper, parentScheme *certScheme) (*certScheme, error) {

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	certemp := new(x509.Certificate)

	//init some default value
	certemp.NotBefore = time.Now()
	//by default we create certs for 10 year
	certemp.NotAfter = certemp.NotBefore.Add(time.Hour * 24 * 3650)
	certemp.Subject.CommonName = "credential.abchain.org"
	//by default we set usage to make it work for TLS
	certemp.KeyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment
	certemp.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth,
		x509.ExtKeyUsageClientAuth}
	certemp.SerialNumber = big.NewInt(1)

	if marshaledk, err := x509.MarshalPKIXPublicKey(key.Public()); err != nil {
		logger.Warningf("marshal pk fail: %s", err)
		//use an random as subjectid
		certemp.SubjectKeyId = make([]byte, 20)
		if _, err = rand.Read(certemp.SubjectKeyId); err != nil {
			logger.Errorf("Rand io failure: %s", err)
			return nil, err
		}
	} else {
		certemp.SubjectKeyId = derivedSubjectKeyID(marshaledk)
	}
	certemp.AuthorityKeyId = certemp.SubjectKeyId

	if s := vp.GetString("new.commonname"); s != "" {
		logger.Debugf("set cert's CN as %s", s)
		certemp.Subject.CommonName = s
	}

	if vp.GetBool("new.isca") {
		logger.Debugf("create cert as CA")
		certemp.BasicConstraintsValid = true
		certemp.IsCA = true
		certemp.KeyUsage = certemp.KeyUsage | x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	}

	var parentcert *x509.Certificate
	var signerPriv crypto.PrivateKey
	var parentRootCets []*x509.Certificate
	if parentScheme == nil {
		parentcert = certemp
		signerPriv = crypto.PrivateKey(key)
	} else if parentcert, signerPriv, err = parentScheme.getKeyPair(); err != nil {
		logger.Errorf("Load keypair from specified scheme fail: %s", err)
		return nil, err
	} else {
		logger.Debugf("Cert is authed by [%X]", parentcert.SubjectKeyId)
		//createcert will set authoritykeyid automatically, but may be skipped
		//if the parent and subcert has same Subject, so we still set it here...
		parentRootCets = parentScheme.certs
		certemp.AuthorityKeyId = parentcert.SubjectKeyId

		//use 128bit int ...
		rand16bt := make([]byte, 16)
		if _, err = rand.Read(rand16bt); err != nil {
			logger.Errorf("Rand io failure: %s", err)
			return nil, err
		}
		certemp.SerialNumber.SetBytes(rand16bt)
	}

	cerpem, err := x509.CreateCertificate(rand.Reader, certemp, parentcert,
		key.Public(), signerPriv)
	if err != nil {
		return nil, err
	}

	cer, _ := x509.ParseCertificate(cerpem)
	if cer == nil {
		panic("Gen wrong pem")
	}

	allcerts := append([]*x509.Certificate{cer}, parentRootCets...)
	return &certScheme{certs: allcerts, key: key}, nil

}

func (c CertLoader) LoadX509CertPool(vp *viper.Viper) (*x509.CertPool, error) {

	scheme, err := c.loadScheme(vp)
	if err != nil {
		return nil, err
	}

	return scheme.CertPool(), nil
}

//parse several type of block: the TLS fashion (file.cert/key), or the embedded data spec. by "pem"
//it them search for "cert" and "key" in the sub block
//if block contains both file and embedded data specifications, embedded data is preferred
func (c CertLoader) loadScheme(vp *viper.Viper) (sc *certScheme, serr error) {

	defer func() {
		if serr == nil && len(sc.certs) == 0 {
			serr = errors.New("Empty scheme (no any cert) is not allowed")
			sc = nil
		}
	}()

	if vp.IsSet("pem") {
		scheme := new(certScheme)

		if err := scheme.loadCersFromEmbeddedBlock(vp.GetString("pem.cert")); err != nil {
			return nil, err
		}

		if err := scheme.loadKeyFromEmbeddedBlock(vp.GetString("pem.key")); err != nil {
			return nil, err
		}

		//omit error in rootcert spec
		if err := scheme.loadCersFromEmbeddedBlock(vp.GetString("pem.rootcert")); err != nil {
			logger.Warning("Load embedded cert from rootcert spec fail: %s", err)
		}

		return scheme, nil
	} else if vp.IsSet("file") {
		scheme := new(certScheme)

		if err := scheme.loadCersFromFile(vp.GetString("file.cert")); err != nil {
			return nil, err
		}

		if err := scheme.loadKeyFromFile(vp.GetString("file.key")); err != nil {
			return nil, err
		}

		if err := scheme.loadCersFromFile(vp.GetString("file.rootcert")); err != nil {
			logger.Warning("Load cert file from rootcert spec fail: %s", err)
		}

		return scheme, nil
	} else if vp.IsSet("new") {

		var parentScheme *certScheme
		if s := vp.GetString("new.authby"); s != "" {
			var existed bool
			if parentScheme, existed = c.preset[s]; !existed {
				return nil, errors.New("Specified auth scheme not existed: " + s)
			} else if parentScheme.key == nil {
				return nil, errors.New("Specified auth scheme has no private key: " + s)
			}
		}

		if c.persistor == nil {
			logger.Warningf("No persistor for specified keypair, always create new cert")
		} else {
			if tag := vp.GetString("new.tag"); tag != "" {
				if keypairs, err := c.Load(tag); err != nil {
					logger.Errorf("Load persisted cert for tag %s fail: %s", tag, err)
				} else if key, cer, err := loadKeypair(keypairs); err != nil {
					logger.Errorf("Parse persisted keypair for tag %s fail: %s", tag, err)

				} else {
					ret := &certScheme{certs: []*x509.Certificate{cer}, key: key}
					if parentScheme != nil {
						ret.certs = append(ret.certs, parentScheme.certs...)
					}

					return ret, nil
				}

				defer func() {

					if serr == nil {
						if pbytes, err := encodeKeypair(sc.key.(*ecdsa.PrivateKey), sc.certs[0]); err != nil {
							logger.Errorf("Encode keypair bytes fail: %s", err)
						} else if err = c.Store(tag, pbytes); err != nil {
							logger.Errorf("Persisting created keypair fail: %s", err)
						}
					}
				}()
			}
		}

		scheme, err := c.createNewScheme(vp, parentScheme)
		if err != nil {
			return nil, err
		}
		return scheme, nil
	} else if ds := vp.GetString("predefined"); ds == "" {
		return nil, errors.New("Can not load any schemes")
	} else if dscheme, existed := c.preset[ds]; !existed {
		return nil, errors.New("Specified scheme not existed: " + ds)
	} else {
		return dscheme, nil
	}

}

func (c CertLoader) LoadX509KeyPair(vp *viper.Viper) (*x509.Certificate, crypto.PrivateKey, error) {

	scheme, err := c.loadScheme(vp)
	if err != nil {
		return nil, nil, err
	}

	return scheme.getKeyPair()

}

//enforce the key must existed
func (c CertLoader) MustLoadX509KeyPair(vp *viper.Viper) (*x509.Certificate, crypto.PrivateKey, error) {
	cer, k, err := c.LoadX509KeyPair(vp)
	if err != nil {
		return nil, nil, err
	} else if k == nil {
		return nil, nil, errors.New("Key is not avaliable")
	} else {
		return cer, k, nil
	}
}

//only consider key/cert entry (i.e.: rootcert is not considered)
func (c CertLoader) LoadSchemePool(vp *viper.Viper) map[string]*certScheme {

	ret := make(map[string]*certScheme)

	for _, entry := range vp.GetStringSlice("entries") {
		subentry := config.SubViper(entry, vp)
		if subentry == nil {
			logger.Debugf("entry %s is empty", entry)
			continue
		}

		scheme, err := c.loadScheme(subentry)
		if err != nil {
			logger.Errorf("Can not load scheme in block <%s>: %s", entry, err)
		} else {
			ret[entry] = scheme
		}
	}

	return ret
}

type SchemePool map[string]*certScheme

var predefinedNothing = map[string]*certScheme{}

func NewLoader(p persistor) CertLoader { return CertLoader{p, predefinedNothing} }
func AddPredefineSchemes(l CertLoader, pool SchemePool) CertLoader {
	return CertLoader{l.persistor, pool}
}
