package cred_default

import (
	"crypto/tls"
	"crypto/x509"
	"github.com/spf13/viper"
	"sync"

	"github.com/abchain/fabric/core/config"
	"github.com/abchain/fabric/core/db"
)

type layeredPersistor []persistor

func (m layeredPersistor) Store(key string, value []byte) error {
	return m[0].Store(key, value)
}

func (m layeredPersistor) Load(key string) (ret []byte, err error) {

	for _, p := range m {
		ret, err = p.Load(key)
		if err == nil && len(ret) > 0 {
			break
		}
	}

	return
}

func combinePersistor(parent, child persistor) persistor {
	lm, ok := parent.(layeredPersistor)
	if ok {
		return layeredPersistor(append([]persistor{child}, lm...))
	} else {
		return layeredPersistor([]persistor{child, parent})
	}
}

var globalPoolpersistor, tlsFactoryPersistor, driverPersistor db.PersistorKey
var globalCertPool map[string]*certScheme
var gloablPoolLoad sync.Once

func init() {
	config.TLSImplFactory = configTLSImpl
	globalPoolpersistor = db.PersistKeyRegister("globalCertPool")
	tlsFactoryPersistor = db.PersistKeyRegister("tlsCerts")
	driverPersistor = db.PersistKeyRegister("driverCerts")
}

func initGlobalPool() {

	gloablPoolLoad.Do(func() {
		ld := NewLoader(db.NewPersistor(globalPoolpersistor))
		globalCertPool = ld.LoadSchemePool(config.SubViper("x509Certs"))
		logger.Infof("Init x509 certificate pool: %d schemes loaded", len(globalCertPool))
	})

}

type schemeAdapter struct {
	*certScheme
}

//extend scheme for TLS
func (cs schemeAdapter) GetTLSCert() (*tls.Certificate, error) {

	tlsCert := new(tls.Certificate)
	for _, cer := range cs.certs {
		tlsCert.Certificate = append(tlsCert.Certificate, cer.Raw)
	}

	tlsCert.PrivateKey = cs.key

	return tlsCert, nil
}

func (cs schemeAdapter) GetRootCerts() (*x509.CertPool, error) {
	return cs.CertPool(), nil
}

func configTLSImpl(vp *viper.Viper) config.TLSImpl {

	//trigger init on each TLS impl factory and driver
	initGlobalPool()

	p := combinePersistor(db.NewPersistor(globalPoolpersistor),
		db.NewPersistor(tlsFactoryPersistor))

	ld := AddPredefineSchemes(NewLoader(p), globalCertPool)
	cs, err := ld.loadScheme(vp)

	if err != nil {
		logger.Errorf("Load TLS configure fail: %s", err)
		return config.FailTLSImpl{err}
	}

	return schemeAdapter{cs}
}
