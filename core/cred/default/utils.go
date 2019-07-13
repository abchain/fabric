package cred_default

import (
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io/ioutil"

	"github.com/abchain/fabric/core/crypto/primitives"
	"github.com/abchain/fabric/core/util"
)

func loadPEMFromBytes(content []byte) (ret []*pem.Block) {

	for blk, rst := pem.Decode(content); blk != nil; blk, rst = pem.Decode(rst) {
		logger.Debugf("Decode pem block: %s", blk.Type)
		ret = append(ret, blk)
	}

	return ret
}

func loadPEMFromFile(path string) ([]*pem.Block, error) {

	cpath := util.CanonicalizeFilePath(path)
	logger.Debugf("Load pem file: %s (before canonicalized %s)", cpath, path)
	content, err := ioutil.ReadFile(cpath)
	if err != nil {
		return nil, err
	}

	return loadPEMFromBytes(content), nil
}

//todo: handling encryption?
func loadCerts(pemblks []*pem.Block) (ret []*x509.Certificate, e error) {

	for _, blk := range pemblks {
		cer, err := x509.ParseCertificate(blk.Bytes)
		if err == nil {
			logger.Debugf("Decode pem cert done, subject %.8X", cer.SubjectKeyId)
			ret = append(ret, cer)
		}
	}

	return
}

func loadOneCert(blk *pem.Block) (*x509.Certificate, error) {
	cers, err := loadCerts([]*pem.Block{blk})
	if err != nil {
		return nil, err
	} else if len(cers) == 0 {
		return nil, errors.New("No certs avaliable")
	}
	return cers[0], nil
}

func loadPublicKey(pemblk *pem.Block) (k crypto.PublicKey, err error) {
	//todo: encryped private key
	switch pemblk.Type {
	case "RSA PUBLIC KEY":
		//k, err = x509.ParsePKCS1PublicKey(pemblk.Bytes)
		return nil, errors.New("Not supported (PKCS1# pk, in Go 1.9)")
	case "PUBLIC KEY":
		k, err = x509.ParsePKIXPublicKey(pemblk.Bytes)
	default:
		return nil, errors.New("Unrecognized type: " + pemblk.Type)
	}

	return
}

func loadKey(pemblk *pem.Block) (k crypto.PrivateKey, err error) {
	//todo: encryped private key
	switch pemblk.Type {
	case "RSA PRIVATE KEY":
		k, err = x509.ParsePKCS1PrivateKey(pemblk.Bytes)
	case "PRIVATE KEY":
		k, err = x509.ParsePKCS8PrivateKey(pemblk.Bytes)
	default:
		return nil, errors.New("Unrecognized type: " + pemblk.Type)
	}

	return
}

func derivedSubjectKeyID(marshaledKey []byte) []byte {

	if id := primitives.Hash(marshaledKey); len(id) > 20 {
		return id[:20]
	} else {
		return id
	}
}
