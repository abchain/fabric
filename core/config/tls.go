package config

import (
	"crypto/tls"
	"crypto/x509"
	"github.com/spf13/viper"
	"google.golang.org/grpc/credentials"
)

type TLSImpl interface {
	GetTLSCert() (*tls.Certificate, error)
	GetRootCerts() (*x509.CertPool, error)
}

type FailTLSImpl struct {
	E error
}

func (e FailTLSImpl) GetTLSCert() (*tls.Certificate, error) { return nil, e.E }
func (e FailTLSImpl) GetRootCerts() (*x509.CertPool, error) { return nil, e.E }

type dummyImply struct{}

func (dummyImply) Error() string { return "Not implied" }

//an impl which create util for tls's crypto elements
var TLSImplFactory func(vp *viper.Viper) TLSImpl = func(*viper.Viper) TLSImpl {
	return FailTLSImpl{dummyImply{}}
}

type tlsSpec struct {
	TLSImpl
	EnableTLS       bool
	TLSHostOverride string
}

func (ts *tlsSpec) Init(vp *viper.Viper) {

	ts.EnableTLS = vp.GetBool("enabled")
	if !ts.EnableTLS {
		return
	}

	ts.TLSImpl = TLSImplFactory(vp)
	ts.TLSHostOverride = vp.GetString("serverhostoverride")
}

//we also make an implement to generating the grpc credential
func (ts *tlsSpec) GetServerTLSOptions() (credentials.TransportCredentials, error) {

	tlscer, err := ts.GetTLSCert()
	if err != nil {
		return nil, err
	}

	return credentials.NewServerTLSFromCert(tlscer), nil
}

func (ts *tlsSpec) GetClientTLSOptions() (credentials.TransportCredentials, error) {

	tlspool, err := ts.GetRootCerts()
	if err != nil {
		return nil, err
	}

	return credentials.NewClientTLSFromCert(tlspool, ts.TLSHostOverride), nil
}
