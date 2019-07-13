package cred_default_test

import (
	"bytes"
	"encoding/pem"
	"github.com/abchain/fabric/core/config"
	cred_def "github.com/abchain/fabric/core/cred"
	cred "github.com/abchain/fabric/core/cred/default"
	pb "github.com/abchain/fabric/protos"
	"testing"
)

func test_ValidatorPeerPair(peer1, peer2 cred_def.PeerCreds, t *testing.T) {

	session2, err := peer2.CreatePeerCred(peer1.Cred(), nil)
	if err != nil {
		t.Fatal(err)
	}

	sesscer := session2.Cred()
	t.Log(string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: sesscer})))

	session1, err := peer1.CreatePeerCred(sesscer, peer2.Pki())
	if err != nil {
		t.Fatal(err)
	}

	if se1, se2 := session1.Secret(), session2.Secret(); bytes.Compare(se1, se2) != 0 {
		t.Fatalf("shared secret not the same: %X, %X", se1, se2)
	} else if len(se1) == 0 {
		t.Fatalf("empty secret")
	}

	msg := new(pb.Message)
	msg.Payload = []byte{42, 42, 42}
	msg.Timestamp = pb.CreateUtcTimestamp()
	msg, err = peer2.EndorsePeerMsg(msg)
	if err != nil {
		t.Fatal(err)
	}

	err = session1.VerifyPeerMsg(msg)
	if err != nil {
		t.Fatal(err)
	}

	msg.Payload = []byte{42, 42, 42, 42}
	msg.Type = pb.Message_DISC_HELLO
	msg, err = peer1.EndorsePeerMsg(msg)
	if err != nil {
		t.Fatal(err)
	}

	err = session2.VerifyPeerMsg(msg)
	if err != nil {
		t.Fatal(err)
	}

}

func test_PreparePeerPairCommon(conf1, conf2 string, t *testing.T) (cred_def.PeerCreds, cred_def.PeerCreds) {

	ld := cred.NewLoader(nil)

	cer1, key1, err := ld.MustLoadX509KeyPair(config.SubViper(conf1))
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("cer1 subject [%X]", cer1.SubjectKeyId)

	peer1, err := cred.NewPeerCredential(cer1, key1, nil)
	if err != nil {
		t.Fatal(err)
	}

	cer2, key2, err := ld.MustLoadX509KeyPair(config.SubViper(conf2))
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("cer2 subject [%X]", cer2.SubjectKeyId)

	peer2, err := cred.NewPeerCredential(cer2, key2, nil)
	if err != nil {
		t.Fatal(err)
	}

	return peer1, peer2

}

func Test_ValidatorPeer(t *testing.T) {

	peer1, peer2 := test_PreparePeerPairCommon("testblk3", "testblk3", t)
	test_ValidatorPeerPair(peer1, peer2, t)
	peer1, peer2 = test_PreparePeerPairCommon("testblk3", "testblk1", t)
	test_ValidatorPeerPair(peer1, peer2, t)
	peer1, peer2 = test_PreparePeerPairCommon("testblk1", "testblk3", t)
	test_ValidatorPeerPair(peer1, peer2, t)
}

func Test_ValidatorPeerWithRoot(t *testing.T) {

	ld := cred.NewLoader(nil)
	pool := cred.SchemePool(ld.LoadSchemePool(config.SubViper("testpools")))
	ld = cred.AddPredefineSchemes(ld, pool)

	cer1, key1, err := ld.MustLoadX509KeyPair(config.SubViper("testblk6"))
	if err != nil {
		t.Fatal(err)
	}

	peer1, err := cred.NewPeerCredential(cer1, key1, pool["abc"].RootCerts())
	if err != nil {
		t.Fatal(err)
	}

	cer2, key2, err := ld.MustLoadX509KeyPair(config.SubViper("testblk6"))
	if err != nil {
		t.Fatal(err)
	}

	peer2, err := cred.NewPeerCredential(cer2, key2, nil)
	if err != nil {
		t.Fatal(err)
	}

	test_ValidatorPeerPair(peer1, peer2, t)
	test_ValidatorPeerPair(peer2, peer1, t)
}

func Test_ValidatorPeerAttackResist(t *testing.T) {

	ld := cred.NewLoader(nil)

	cer1, key1, err := ld.MustLoadX509KeyPair(config.SubViper("testblk3"))
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("cer1 subject [%X]", cer1.SubjectKeyId)

	peer1, err := cred.NewPeerCredential(cer1, key1, nil)
	if err != nil {
		t.Fatal(err)
	}

	cer2, key2, err := ld.MustLoadX509KeyPair(config.SubViper("testblk3"))
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("cer2 subject [%X]", cer2.SubjectKeyId)

	peer2, err := cred.NewPeerCredential(cer2, key2, nil)
	if err != nil {
		t.Fatal(err)
	}

	cer3, key3, err := ld.MustLoadX509KeyPair(config.SubViper("testblk3"))
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("cer3 subject [%X]", cer2.SubjectKeyId)

	peer3, err := cred.NewPeerCredential(cer3, key3, nil)
	if err != nil {
		t.Fatal(err)
	}

	session2, err := peer2.CreatePeerCred(peer1.Cred(), nil)
	if err != nil {
		t.Fatal(err)
	}

	sesscer := session2.Cred()

	//NORMAL process
	_, err = peer1.CreatePeerCred(sesscer, peer2.Pki())
	if err != nil {
		t.Fatal(err)
	}

	_, err = peer1.CreatePeerCred(sesscer, peer3.Pki())
	if err == nil {
		t.Fatal("peer3 cheat peer1 by using session cert not for theirs")
	}

	_, err = peer3.CreatePeerCred(sesscer, peer2.Pki())
	if err == nil {
		t.Fatal("peer2 cheat peer3 by session with peer1")
	}

	_, err = peer3.CreatePeerCred(sesscer, nil)
	if err == nil {
		t.Fatal("peer1 cheat peer3 by an csr")
	}

}
