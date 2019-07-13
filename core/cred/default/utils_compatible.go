//+build go1.10

package cred_default

import (
	"crypto/x509"
)

var marshalPKCS8PrivateKey = x509.MarshalPKCS8PrivateKey
