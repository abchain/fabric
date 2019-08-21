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
	"encoding/json"
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
		t.Fatalf("wrong parse for <%s>: %s, %s, %v", fname, n, tmp, ls)
	}

	fname = "tt:single"
	n, tmp, ls = ParseYFCCName(fname)
	if len(ls) != 0 || tmp != "tt" || n != "single" {
		t.Fatalf("wrong parse for <%s>: %s, %s, %v", fname, n, tmp, ls)
	}

	fname = "tt:single@1,2,3"
	n, tmp, ls = ParseYFCCName(fname)
	if len(ls) != 3 || tmp != "tt" || n != "single" {
		t.Fatalf("wrong parse for <%s>: %s, %s, %v", fname, n, tmp, ls)
	}

	fname = "single@1,2,3"
	n, tmp, ls = ParseYFCCName(fname)
	if len(ls) != 3 || tmp != "" || n != "single" {
		t.Fatalf("wrong parse for <%s>: %s, %s, %v", fname, n, tmp, ls)
	}

	fname = "tt:@1,2,3"
	n, tmp, ls = ParseYFCCName(fname)
	if len(ls) != 3 || tmp != "tt" || n != "" {
		t.Fatalf("wrong parse for <%s>: %s, %s, %v", fname, n, tmp, ls)
	}

	fname = "tt:rr@1"
	n, tmp, ls = ParseYFCCName(fname)
	if len(ls) != 1 || tmp != "tt" || n != "rr" {
		t.Fatalf("wrong parse for <%s>: %s, %s, %v", fname, n, tmp, ls)
	}

}

func Test_ChaincodeInput_Unmarshal(t *testing.T) {

	example1 := `{"function":"ASTRO.TOKEN.TRANSFER","args":["ChMKB0FCQ0hBSU4SCEFzdHJvX3YxEgYIlePV5gUaFB18sHWzREHZIlJg6Ajq4HKMwncB","CglkYW1laW52eXkiCUBBc3Ryb192MSoWChQSbaukOOqE58Q8L1ajIA7WXjcbO0JLCgFvEhYKFPmAE1K+T8V+y4ObshuwV2faNMOdGhYKFBJtq6Q46oTnxDwvVqMgDtZeNxs7IhYKFAAAAAAAAAAAAAAAAAAAAAAAAAAA","CpUBCpIBGo8BIowBCiDPu6oRSF0fwwmiESmKlG30QxiqjHWiB8bBiMgG+wxbwBIg+ek/eiR9t8Nh3gR0F75xBYtmOQGwaG3F4mNISEFt6VggASpECiDQ3gqurvrQK4vcigGhuLEcaWvT1mosXxB4DZW330JkXBIg2FIopvsplA6Fjn5VhCrivRFdHtfMDoLZNOkpyXZIywo="],"ByteArgs":true}`
	example2 := `{"function":"init","args":["a","100","b","200"]}`
	example3 := `{"args":["ChMKB0FCQ0hBSU4SCEFzdHJvX3YxEgYIlePV5gUaFB18sHWzREHZIlJg6Ajq4HKMwncB"],"ByteArgs":true}`

	var v1, v2 ChaincodeInput
	if err := json.Unmarshal([]byte(example1), &v1); err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal([]byte(example2), &v2); err != nil {
		t.Fatal(err)
	}

	if len(v1.Args) != 4 {
		t.Fatal("Unexpected args")
	}

	if len(v2.Args) != 5 {
		t.Fatal("Unexpected args")
	}

	if string(v1.Args[1]) == "ChMKB0FCQ0hBSU4SCEFzdHJvX3YxEgYIlePV5gUaFB18sHWzREHZIlJg6Ajq4HKMwncB" {
		t.Fatal("Unexpected string arg")
	}

	if string(v2.Args[2]) != "100" {
		t.Fatal("wrong string arg")
	}

	v1.Args = nil
	if err := json.Unmarshal([]byte(example3), &v1); err != nil {
		t.Fatal(err)
	}

	if len(v1.Args) != 1 {
		t.Fatal("Unexpected args")
	}
}
