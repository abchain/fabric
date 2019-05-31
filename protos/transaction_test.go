/*
Copyright IBM Corp. 2016 All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

		 http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package protos

import (
	"bytes"
	"github.com/golang/protobuf/proto"
	"testing"
)

func Test_Transaction_CreateNew(t *testing.T) {

	cidBytes, err := proto.Marshal(&ChaincodeID{Path: "Contract001"})
	if err != nil {
		t.Fatalf("Could not marshal chaincode: %s", err)
	}
	tx := &Transaction{ChaincodeID: cidBytes}
	t.Logf("Transaction: %v", tx)

	data, err := proto.Marshal(tx)
	if err != nil {
		t.Errorf("Error marshalling transaction: %s", err)
	}
	t.Logf("Marshalled data: %v", data)

	// TODO: This doesn't seem like a proper test. Needs to be edited.
	txUnmarshalled := &Transaction{}
	err = proto.Unmarshal(data, txUnmarshalled)
	t.Logf("Unmarshalled transaction: %v", txUnmarshalled)
	if err != nil {
		t.Errorf("Error unmarshalling block: %s", err)
	}

}

func Test_Transaction_Digest(t *testing.T) {

	cidBytes, err := proto.Marshal(&ChaincodeID{Path: "Contract001"})
	if err != nil {
		t.Fatalf("Could not marshal chaincode: %s", err)
	}
	tx := &Transaction{ChaincodeID: cidBytes}
	t.Logf("Transaction: %v", tx)

	//should be correct on re-entry
	d1, err := tx.Digest()
	if err != nil {
		t.Errorf("Error digesting transaction 1: %s", err)
	}

	d2, err := tx.Digest()
	if err != nil {
		t.Errorf("Error digesting transaction 2: %s", err)
	}

	if bytes.Compare(d1, d2) != 0 {
		t.Errorf("tx digest re-entry fail")
	}

	//should be correct on re-entry
	d1a, err := tx.DigestWithAlg("sha256")
	if err != nil {
		t.Errorf("Error digesting transaction 3: %s", err)
	}

	d2a, err := tx.DigestWithAlg("sha256")
	if err != nil {
		t.Errorf("Error digesting transaction 4: %s", err)
	}

	if bytes.Compare(d1a, d2a) != 0 {
		t.Errorf("tx digest with sha256 re-entry fail")
	}

	if bytes.Compare(d1, d1a) == 0 {
		t.Errorf("tx get same digest with different algo")
	}

	tx.Payload = []byte("some bytes")
	d3, err := tx.Digest()
	if err != nil {
		t.Errorf("Error digesting transaction 5: %s", err)
	}

	if bytes.Compare(d1, d3) == 0 {
		t.Errorf("tx get same digest with different content")
	}
}

func Test_YFCCNameParse(t *testing.T) {
	var fname string
	fname = "single"
	n, tmp, ls := ParseYFCCName(fname)
	if len(ls) != 0 || tmp != "" || n != "single" {
		t.Fatal("wrong parse for <%s>: %s, %s, %v", fname, n, tmp, ls)
	}

	fname = "tt:single"
	n, tmp, ls = ParseYFCCName(fname)
	if len(ls) != 0 || tmp != "tt" || n != "single" {
		t.Fatal("wrong parse for <%s>: %s, %s, %v", fname, n, tmp, ls)
	}

	fname = "tt:single@1,2,3"
	n, tmp, ls = ParseYFCCName(fname)
	if len(ls) != 3 || tmp != "tt" || n != "single" {
		t.Fatal("wrong parse for <%s>: %s, %s, %v", fname, n, tmp, ls)
	}

	fname = "single@1,2,3"
	n, tmp, ls = ParseYFCCName(fname)
	if len(ls) != 3 || tmp != "" || n != "single" {
		t.Fatal("wrong parse for <%s>: %s, %s, %v", fname, n, tmp, ls)
	}

	fname = "tt:@1,2,3"
	n, tmp, ls = ParseYFCCName(fname)
	if len(ls) != 3 || tmp != "tt" || n != "" {
		t.Fatal("wrong parse for <%s>: %s, %s, %v", fname, n, tmp, ls)
	}

	fname = "tt:rr@1"
	n, tmp, ls = ParseYFCCName(fname)
	if len(ls) != 1 || tmp != "tt" || n != "rr" {
		t.Fatal("wrong parse for <%s>: %s, %s, %v", fname, n, tmp, ls)
	}

}
