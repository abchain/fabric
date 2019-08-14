package cred_default_test

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"github.com/abchain/fabric/core/config"
	cred "github.com/abchain/fabric/core/cred/default"
	"os"
	"testing"
)

func TestMain(m *testing.M) {

	cfg := config.SetupTestConf{"", "x509", ""}
	cfg.Setup()

	os.Exit(m.Run())
}

func Test_LoaderForFiles(t *testing.T) {
	ld := cred.NewLoader(nil)

	cer, key, err := ld.MustLoadX509KeyPair(config.SubViper("testblk1"))
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("%T", key)

	var cmpV []byte
	fmt.Sscanf("3BF977A5F44928543846F5", "%X", &cmpV)
	if len(cmpV) == 0 {
		panic("Not recognized value")
	}

	if bytes.Compare(cer.SubjectKeyId[:len(cmpV)], cmpV) != 0 {
		t.Fatalf("Subject keyid not expected: %X", cer.SubjectKeyId)
	}
}

func Test_LoaderForEmbedded(t *testing.T) {
	ld := cred.NewLoader(nil)

	cer, _, err := ld.MustLoadX509KeyPair(config.SubViper("testblk2"))
	if err != nil {
		t.Fatal(err)
	}

	t.Log(cer.KeyUsage, cer.ExtKeyUsage)

	var cmpV []byte
	fmt.Sscanf("3BF977A5F44928543846F5", "%X", &cmpV)
	if len(cmpV) == 0 {
		panic("Not recognized value")
	}

	if bytes.Compare(cer.SubjectKeyId[:len(cmpV)], cmpV) != 0 {
		t.Fatalf("Subject keyid not expected: %X", cer.SubjectKeyId)
	}
}

func Test_LoaderForNewSelfSignCert(t *testing.T) {

	ld := cred.NewLoader(nil)

	cer, _, err := ld.MustLoadX509KeyPair(config.SubViper("testblk3"))
	if err != nil {
		t.Fatal(err)
	}

	t.Log(string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cer.Raw})))

	if !cer.IsCA {
		t.Fatal("cert is not CA")
	} else if cer.Subject.CommonName != "test.abc" {
		t.Fatal("CN is not match:", cer.Subject)
	}
}

func Test_LoaderForPoolBlockAndNewCert(t *testing.T) {

	ld := cred.NewLoader(nil)
	pool := cred.SchemePool(ld.LoadSchemePool(config.SubViper("testpools")))
	t.Log(pool)
	if len(pool) != 3 {
		t.Fatal("Wrong pool data")
	}

	cerpool := pool["abc"].CertPool()

	ld = cred.AddPredefineSchemes(ld, pool)

	cer, _, err := ld.MustLoadX509KeyPair(config.SubViper("testblk4"))
	if err != nil {
		t.Fatal(err)
	}

	t.Log(string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cer.Raw})))

	if cer.IsCA {
		t.Fatal("cert is unexpected CA")
	} else if cer.Subject.CommonName != "rrr.abc" {
		t.Fatal("CN is not match:", cer.Subject)
	}

	if cch, err := cer.Verify(x509.VerifyOptions{Roots: cerpool}); err != nil {
		t.Fatal("verify fail", err)
	} else if err = cer.CheckSignatureFrom(cch[0][1]); err != nil {
		t.Fatal("signature fail", err)
	} else if cch[0][1].Subject.CommonName != "test.abc" {
		t.Fatal("unexpected root:", cch[0][1].Subject.CommonName)
	}

	cer, _, err = ld.MustLoadX509KeyPair(config.SubViper("testblk41"))
	if err != nil {
		t.Fatal(err)
	}

	t.Log(string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cer.Raw})))

	_, _, err = ld.MustLoadX509KeyPair(config.SubViper("testblk4f"))
	if err == nil {
		t.Fatal("unexpected success")
	}

}

type dummyPersistor map[string][]byte

func (m dummyPersistor) Store(key string, value []byte) error {
	m[key] = value
	return nil
}

func (m dummyPersistor) Load(key string) ([]byte, error) {
	if v, existed := m[key]; !existed {
		return nil, errors.New("Not existed")
	} else {
		return v, nil
	}
}

func newDummyPersistor() dummyPersistor { return dummyPersistor(make(map[string][]byte)) }

func Test_LoaderFoPersistedKeypair(t *testing.T) {

	persistor := newDummyPersistor()

	ld := cred.NewLoader(persistor)

	cer, _, err := ld.MustLoadX509KeyPair(config.SubViper("testblk5"))
	if err != nil {
		t.Fatal(err)
	}

	if _, existed := persistor["blk5"]; !existed {
		t.Fatal("No record")
	}

	t.Log(string(persistor["blk5"]))

	cer2, _, err := ld.MustLoadX509KeyPair(config.SubViper("testblk5"))
	if err != nil {
		t.Fatal(err)
	}

	if len(cer.SubjectKeyId) == 0 || bytes.Compare(cer.SubjectKeyId, cer2.SubjectKeyId) != 0 {
		t.Fatalf("subjectkey not identical: %X vs %X", cer.SubjectKeyId, cer2.SubjectKeyId)
	}

	ld2 := cred.NewLoader(persistor)
	cer3, _, err := ld2.MustLoadX509KeyPair(config.SubViper("testblk5"))
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Compare(cer3.SubjectKeyId, cer2.SubjectKeyId) != 0 {
		t.Fatalf("subjectkey not identical: %X vs %X", cer2.SubjectKeyId, cer3.SubjectKeyId)
	}

	//ensure a no persistor loader never fail
	ld3 := cred.NewLoader(nil)

	_, _, err = ld3.MustLoadX509KeyPair(config.SubViper("testblk5"))
	if err != nil {
		t.Fatal(err)
	}

}
