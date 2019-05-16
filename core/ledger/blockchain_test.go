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

package ledger

import (
	"testing"
	"time"

	"github.com/abchain/fabric/core/ledger/testutil"
	"github.com/abchain/fabric/core/util"
	"github.com/abchain/fabric/protos"
)

func TestBlockchain_InfoNoBlock(t *testing.T) {
	testDBWrapper.CleanDB(t)
	blockchainTestWrapper := newTestBlockchainWrapper(t)
	blockchain := blockchainTestWrapper.blockchain
	blockchainInfo, err := blockchain.getBlockchainInfo()
	testutil.AssertNoError(t, err, "Error while invoking getBlockchainInfo() on an emply blockchain")
	testutil.AssertEquals(t, blockchainInfo.Height, uint64(0))
	testutil.AssertEquals(t, blockchainInfo.CurrentBlockHash, nil)
	testutil.AssertEquals(t, blockchainInfo.PreviousBlockHash, nil)
}

func TestBlockchain_Info(t *testing.T) {
	testDBWrapper.CleanDB(t)
	blockchainTestWrapper := newTestBlockchainWrapper(t)
	blocks, _, _ := blockchainTestWrapper.populateBlockChainWithSampleData()

	blockchain := blockchainTestWrapper.blockchain
	blockchainInfo, _ := blockchain.getBlockchainInfo()
	testutil.AssertEquals(t, blockchainInfo.Height, uint64(3))
	currentBlockHash, _ := blocks[len(blocks)-1].GetHash()
	previousBlockHash, _ := blocks[len(blocks)-2].GetHash()
	testutil.AssertEquals(t, blockchainInfo.CurrentBlockHash, currentBlockHash)
	testutil.AssertEquals(t, blockchainInfo.PreviousBlockHash, previousBlockHash)
}

func testBlockChain_SingleBlock(t *testing.T) {
	testDBWrapper.CleanDB(t)
	blockchainTestWrapper := newTestBlockchainWrapper(t)
	blockchain := blockchainTestWrapper.blockchain

	// Create the Chaincode specification
	chaincodeSpec := &protos.ChaincodeSpec{Type: protos.ChaincodeSpec_GOLANG,
		ChaincodeID: &protos.ChaincodeID{Path: "Contracts"},
		CtorMsg:     &protos.ChaincodeInput{Args: util.ToChaincodeArgs("Initialize", "param1")}}
	chaincodeDeploymentSepc := &protos.ChaincodeDeploymentSpec{ChaincodeSpec: chaincodeSpec}
	uuid := testutil.GenerateID(t)
	newChaincodeTx, err := protos.NewChaincodeDeployTransaction(chaincodeDeploymentSepc, uuid)
	testutil.AssertNoError(t, err, "Failed to create new chaincode Deployment Transaction")
	t.Logf("New chaincode tx: %v", newChaincodeTx)

	block1 := protos.NewBlock([]*protos.Transaction{newChaincodeTx}, nil)
	blockNumber := blockchainTestWrapper.addNewBlock(block1, []byte("stateHash1"))
	t.Logf("New chain: %v", blockchain)
	testutil.AssertEquals(t, blockNumber, uint64(0))
	testutil.AssertEquals(t, blockchain.getSize(), uint64(1))
	testutil.AssertEquals(t, blockchainTestWrapper.fetchBlockchainSizeFromDB(), uint64(1))
}

func TestBlockChain_SingleBlockV0(t *testing.T) {
	defV := protos.DefaultBlockVer()

	protos.SetDefaultBlockVer(0)
	testBlockChain_SingleBlock(t)

	protos.SetDefaultBlockVer(defV)
}

func TestBlockChain_SingleBlock(t *testing.T) {

	testBlockChain_SingleBlock(t)
}

func testBlockChain_SimpleChain(t *testing.T) {
	testDBWrapper.CleanDB(t)
	blockchainTestWrapper := newTestBlockchainWrapper(t)
	blockchain := blockchainTestWrapper.blockchain
	allBlocks, allStateHashes, err := blockchainTestWrapper.populateBlockChainWithSampleData()
	if err != nil {
		t.Logf("Error populating block chain with sample data: %s", err)
		t.Fail()
	}
	testutil.AssertEquals(t, blockchain.getSize(), uint64(len(allBlocks)))
	testutil.AssertEquals(t, blockchainTestWrapper.fetchBlockchainSizeFromDB(), uint64(len(allBlocks)))

	for i := range allStateHashes {
		t.Logf("Checking state hash for block number = [%d]", i)
		testutil.AssertEquals(t, blockchainTestWrapper.getBlock(uint64(i)).GetStateHash(), allStateHashes[i])
	}

	for i := range allBlocks {
		t.Logf("Checking block hash for block number = [%d]", i)
		blockhash, _ := blockchainTestWrapper.getBlock(uint64(i)).GetHash()
		expectedBlockHash, _ := allBlocks[i].GetHash()
		testutil.AssertEquals(t, blockhash, expectedBlockHash)
	}

	testutil.AssertNil(t, blockchainTestWrapper.getBlock(uint64(0)).PreviousBlockHash)

	i := 1
	for i < len(allBlocks) {
		t.Logf("Checking previous block hash for block number = [%d]", i)
		expectedPreviousBlockHash, _ := allBlocks[i-1].GetHash()
		testutil.AssertEquals(t, blockchainTestWrapper.getBlock(uint64(i)).PreviousBlockHash, expectedPreviousBlockHash)
		i++
	}
}

func TestBlockChain_SimpleChainV0(t *testing.T) {
	defV := protos.DefaultBlockVer()

	protos.SetDefaultBlockVer(0)
	testBlockChain_SimpleChain(t)

	protos.SetDefaultBlockVer(defV)

}

func TestBlockChain_SimpleChain(t *testing.T) {
	testBlockChain_SimpleChain(t)
}

func testBlockChainEmptyChain(t *testing.T) {
	testDBWrapper.CleanDB(t)
	blockchainTestWrapper := newTestBlockchainWrapper(t)
	testutil.AssertEquals(t, blockchainTestWrapper.blockchain.getSize(), uint64(0))
	block := blockchainTestWrapper.getLastBlock()
	if block != nil {
		t.Fatalf("Get last block on an empty chain should return nil.")
	}
	t.Logf("last block = [%s]", block)
}

func TestBlockChainEmptyChainV0(t *testing.T) {
	defV := protos.DefaultBlockVer()

	protos.SetDefaultBlockVer(0)
	testBlockChainEmptyChain(t)

	protos.SetDefaultBlockVer(defV)

}

func TestBlockChainEmptyChain(t *testing.T) {
	testBlockChainEmptyChain(t)
}

func testBlockchainBlockLedgerCommitTimestamp(t *testing.T) {
	testDBWrapper.CleanDB(t)
	blockchainTestWrapper := newTestBlockchainWrapper(t)
	block1 := protos.NewBlock(nil, nil)
	startTime := util.CreateUtcTimestamp()
	time.Sleep(2 * time.Second)
	blockchainTestWrapper.addNewBlock(block1, []byte("stateHash1"))
	lastBlock := blockchainTestWrapper.getLastBlock()
	if lastBlock.NonHashData == nil {
		t.Fatal("Expected block to have non-hash-data, but it was nil")
	}
	if lastBlock.NonHashData.LocalLedgerCommitTimestamp == nil {
		t.Fatal("Expected block to have non-hash-data timestamp, but it was nil")
	}
	if startTime.Seconds >= lastBlock.NonHashData.LocalLedgerCommitTimestamp.Seconds {
		t.Fatal("Expected block time to be after start time")
	}
}

func TestBlockchainBlockLedgerCommitTimestampV0(t *testing.T) {
	defV := protos.DefaultBlockVer()

	protos.SetDefaultBlockVer(0)
	testBlockchainBlockLedgerCommitTimestamp(t)

	protos.SetDefaultBlockVer(defV)

}

func TestBlockchainBlockLedgerCommitTimestamp(t *testing.T) {
	testBlockchainBlockLedgerCommitTimestamp(t)
}

func TestBlockchainBlockExistChecking(t *testing.T) {

	testDBWrapper.CleanDB(t)
	testBlockchainWrapper := newTestBlockchainWrapper(t)
	defer func() { testBlockchainWrapper.blockchain.indexer.stop() }()
	_, _, err := testBlockchainWrapper.populateBlockChainWithSampleData()
	if err != nil {
		t.Logf("Error populating block chain with sample data: %s", err)
		t.Fail()
	}

	writeBatch := testDBWrapper.NewWriteBatch()
	defer writeBatch.Destroy()

	addDistancedBlock := func(h uint64, prevhash []byte) []byte {
		block1 := protos.NewBlock(nil, nil)
		block1.PreviousBlockHash = prevhash
		block1.Timestamp = util.CreateUtcTimestamp()
		err = testBlockchainWrapper.blockchain.addPersistenceChangesForNewBlock(block1, h, writeBatch)
		testutil.AssertNoError(t, err, "Error while adding a new block")
		testDBWrapper.WriteToDB(t, writeBatch)
		testBlockchainWrapper.blockchain.blockPersistenceStatus(true)

		bkhash, _ := block1.GetHash()
		return bkhash
	}

	bkh := addDistancedBlock(5, []byte("blockhash 0"))
	bkh = addDistancedBlock(6, bkh)
	addDistancedBlock(7, bkh)

	testutil.AssertEquals(t, testBlockchainWrapper.testBlockExisted(2), true)
	testutil.AssertEquals(t, testBlockchainWrapper.testBlockExisted(6), true)
	testutil.AssertEquals(t, testBlockchainWrapper.testBlockExisted(8), false)
	testutil.AssertEquals(t, testBlockchainWrapper.testBlockExisted(9), false)

	testutil.AssertEquals(t, testBlockchainWrapper.testBlockExistedRange(4, true), uint64(4))
	testutil.AssertEquals(t, testBlockchainWrapper.testBlockExistedRange(5, false), uint64(4))
	testutil.AssertEquals(t, testBlockchainWrapper.testBlockExistedRange(6, true), uint64(8))
	testutil.AssertEquals(t, testBlockchainWrapper.testBlockExistedRange(7, false), uint64(4))
	testutil.AssertEquals(t, testBlockchainWrapper.testBlockExistedRange(9, false), uint64(9))
	testutil.AssertEquals(t, testBlockchainWrapper.testBlockExistedRange(2, false), uint64(0))
	testutil.AssertEquals(t, testBlockchainWrapper.testBlockExistedRange(0, true), uint64(3))

	bkh = addDistancedBlock(11, []byte("blockhash 10"))
	bkh = addDistancedBlock(12, bkh)

	testutil.AssertEquals(t, testBlockchainWrapper.testBlockExisted(6), true)
	testutil.AssertEquals(t, testBlockchainWrapper.testBlockExistedRange(6, true), uint64(8))
	testutil.AssertEquals(t, testBlockchainWrapper.testBlockExistedRange(12, true), uint64(13))
	testutil.AssertEquals(t, testBlockchainWrapper.testBlockExistedRange(0, true), uint64(3))

}
