package cred_default_test

import (
	"fmt"
	"github.com/abchain/fabric/core/config"
	cred_def "github.com/abchain/fabric/core/cred"
	cred "github.com/abchain/fabric/core/cred/default"
	pb "github.com/abchain/fabric/protos"
	"testing"
)

func getEndorser(conf string, t *testing.T) cred_def.TxEndorserFactory {

	ld := cred.NewLoader(nil)

	cer1, key1, err := ld.MustLoadX509KeyPair(config.SubViper(conf))
	if err != nil {
		t.Fatal(err)
	}

	ret, err := cred.NewEndorser(cer1, key1)
	if err != nil {
		t.Fatal(err)
	}

	return ret
}

func Test_PeerStateValidating(t *testing.T) {

	endorser := getEndorser("testblk3", t)

	txval := cred.NewDefaultTxHandler(false)
	txval.SetIdConverter(func(_ []byte) string { return "test" })

	st := new(pb.PeerTxState)
	st.Num = 1000

	st, err := endorser.EndorsePeerState(st)
	if err != nil {
		t.Fatal(err)
	}

	t.Log(st)

	err = txval.ValidatePeerStatus("test", st)
	if err != nil {
		t.Fatal(err)
	}

	err = txval.ValidatePeerStatus("wrongid", st)
	if err == nil {
		t.Fatal("unexpected validate result: wrong id")
	}

	st.Signature = nil
	err = txval.ValidatePeerStatus("test", st)
	if err == nil {
		t.Fatal("unexpected validate result: pass no signature")
	}

	st.Signature = []byte{42, 42, 42, 42}
	err = txval.ValidatePeerStatus("test", st)
	if err == nil {
		t.Fatal("unexpected validate result: pass wrong signature")
	}
}

func Test_TxValidating(t *testing.T) {

	endorser := getEndorser("testblk3", t)

	txval := cred.NewDefaultTxHandler(false)
	txval.SetIdConverter(func(_ []byte) string { return "test" })

	st := new(pb.PeerTxState)
	st.Num = 1000

	st, err := endorser.EndorsePeerState(st)
	if err != nil {
		t.Fatal(err)
	}

	err = txval.ValidatePeerStatus("test", st)
	if err != nil {
		t.Fatal(err)
	}

	defer txval.RemovePreValidator("test")

	txendorser, err := endorser.GetEndorser()
	if err != nil {
		t.Fatal(err)
	}

	defer txendorser.Release()

	testccid := pb.ChaincodeID{Name: "test"}

	tx1, _ := pb.NewTransaction(testccid, "", "tf", []string{"1", "2"})
	tx1.Nonce = []byte{1}

	tx1, err = txendorser.EndorseTransaction(tx1)
	if err != nil {
		t.Fatal(err)
	}

	t.Log(tx1)

	ph := txval.GetValidator("test")
	_, err = ph.Handle(pb.NewTransactionHandlingContext(tx1))
	if err != nil {
		t.Fatal(err)
	}

	tx2, _ := pb.NewTransaction(testccid, "", "tf", []string{"1", "42"})
	tx2.Nonce = []byte{2, 1, 3}

	tx2, err = txendorser.EndorseTransaction(tx2)
	if err != nil {
		t.Fatal(err)
	}

	t.Log(tx2)
	_, err = ph.Handle(pb.NewTransactionHandlingContext(tx2))
	if err != nil {
		t.Fatal(err)
	}

	tx2.Signature = tx1.Signature
	_, err = ph.Handle(pb.NewTransactionHandlingContext(tx2))
	if err == nil {
		t.Fatal("can not distinguish wrong signature")
	}
}

func Test_EndorseIdNotDuplicated(t *testing.T) {

	checklist := map[string]bool{}

	for i := 0; i < 1000; i++ {

		endorser := getEndorser("testblk3", t)
		sid := fmt.Sprintf("%X", endorser.EndorserId())
		if _, ok := checklist[sid]; ok {
			t.Fatal("Duplicated ID: ", sid)
		}

		checklist[sid] = true
	}

	if len(checklist) < 1000 {
		t.Fatal("Unexpected list len ", len(checklist))
	}
}
