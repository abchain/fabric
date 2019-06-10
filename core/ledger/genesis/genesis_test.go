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

package genesis

import (
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/ledger/testutil"
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	testutil.SetupTestConfig()
	os.Exit(m.Run())
}

func TestGenesis(t *testing.T) {

	ledger := ledger.InitTestLedger(t)

	if ledger.GetBlockchainSize() != 0 {
		t.Fatalf("Expected blockchain size of 0, but got %d", ledger.GetBlockchainSize())
	}

	makeGenesisErr := MakeGenesis()
	if makeGenesisErr != nil {
		t.Fatalf("Error creating genesis block, %s", makeGenesisErr)
	}
	if ledger.GetBlockchainSize() != 1 {
		t.Fatalf("Expected blockchain size of 1, but got %d", ledger.GetBlockchainSize())
	}
}

func TestCustomGenesis(t *testing.T) {

	ledger := ledger.InitTestLedger(t)

	if ledger.GetBlockchainSize() != 0 {
		t.Fatalf("Expected blockchain size of 0, but got %d", ledger.GetBlockchainSize())
	}

	tblk := NewGenesisBlockTemplate([]byte("test"), time.Time{})
	makeGenesisErr := tblk.MakeGenesisForLedger(ledger, "customCC", map[string][]byte{
		"key1": []byte("value1"),
		"key2": []byte("value2"),
		"key3": []byte("value3"),
	})
	testutil.AssertNoError(t, makeGenesisErr, "make genesis")
	testutil.AssertEquals(t, ledger.GetBlockchainSize(), uint64(1))

	v, serr := ledger.GetState("customCC", "key1", true)
	testutil.AssertNoError(t, serr, "get state")
	testutil.AssertEquals(t, v, []byte("value1"))
}
