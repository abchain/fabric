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
	"encoding/binary"
	"fmt"
	"github.com/abchain/fabric/core/util"
	"github.com/golang/protobuf/proto"
)

// NewBlock creates a new block with the specified proposer ID, list of,
// transactions, and hash of the state calculated by calling State.GetHash()
// after running all transactions in the block and updating the state.
//
// TODO Remove proposerID parameter. This should be fetched from the config.
// TODO Remove the stateHash parameter. The transactions in this block should
// be run when blockchain.AddBlock() is called. This function will then update
// the stateHash in this block.
//
// func NewBlock(proposerID string, transactions []transaction.Transaction, stateHash []byte) *Block {
// 	block := new(Block)
// 	block.ProposerID = proposerID
// 	block.transactions = transactions
// 	block.stateHash = stateHash
// 	return block
// }

type normalizeBlock struct {
	transactions []*Transaction
	txids        []string
}

func (n *normalizeBlock) Resume(block *Block) {
	block.Transactions = n.transactions
	block.Txids = n.txids
}

//Use to make the block become "normalized" without mutate the block
func makeNormalizeBlock(block *Block) *normalizeBlock {
	r := &normalizeBlock{block.Transactions, block.Txids}
	block.Transactions = nil

	if r.txids == nil && r.transactions != nil {
		block.Txids = make([]string, len(r.transactions))
		for i, tx := range r.transactions {
			block.Txids[i] = tx.Txid
		}
	}

	return r
}

// Bytes returns this block as an array of bytes.
func (block *Block) GetBlockBytes() ([]byte, error) {

	normalize := makeNormalizeBlock(block)
	defer normalize.Resume(block)

	data, err := proto.Marshal(block)

	if err != nil {
		return nil, fmt.Errorf("Could not marshal block: %s", err)
	}

	return data, nil
}

//include all bytes (embedded transactions)
func (block *Block) FullBytes() ([]byte, error) {

	if block == nil {
		return nil, fmt.Errorf("Block is nil")
	}

	if len(block.GetTxids()) > len(block.GetTransactions()) {
		return nil, fmt.Errorf("Block is not full (missing transactions)")
	}

	data, err := proto.Marshal(block)

	if err != nil {
		return nil, fmt.Errorf("Could not marshal block: %s", err)
	}

	return data, nil
}

func (block *Block) VerifyRaw(expectedhash []byte) error {

	h, err := block.GetHash()
	if err != nil {
		return err
	} else if bytes.Compare(h, expectedhash) != 0 {
		return fmt.Errorf("hash is not matched to expected")
	}

	return nil

}

func (block *Block) Verify(expectedhash []byte) error {

	err := block.VerifyRaw(expectedhash)
	if err != nil {
		return err
	}
	//also verify each tx it carried
	if block.GetVersion() > 0 {

		if len(block.GetTransactions()) != len(block.GetTxids()) {
			return fmt.Errorf("Block carry wrong transactions inside it")
		}

		for i, txid := range block.GetTxids() {
			if tx := block.Transactions[i]; !tx.IsValid() || tx.GetTxid() != txid {
				return fmt.Errorf("Wrong tx [%s] in %d!", txid, i)
			}
		}
	}

	return nil
}

var defaultBlockVersion uint32 = 1

func SetDefaultBlockVer(v uint32) {
	defaultBlockVersion = v
}

func DefaultBlockVer() uint32 {
	return defaultBlockVersion
}

// NewBlock creates a new Block given the input parameters.
func NewBlock(transactions []*Transaction, metadata []byte) *Block {
	return NewBlockV(defaultBlockVersion, transactions, metadata)
}

// NewBlock creates a new Block given the input parameters.
func NewBlockV(ver uint32, transactions []*Transaction, metadata []byte) *Block {
	block := new(Block)
	block.Transactions = transactions
	block.ConsensusMetadata = metadata

	block.NonHashData = &NonHashData{LocalLedgerCommitTimestamp: util.CreateUtcTimestamp()}
	block.Version = ver

	if len(block.Transactions) > 0 {
		block.Txids = make([]string, len(block.Transactions))
		for i, tx := range block.Transactions {
			block.Txids[i] = tx.Txid
		}
	}

	return block
}

//clean unnecessary data (txid) for a better transfer format
func (block *Block) Prune() {
	if block == nil {
		return
	}
	if block.Version > 0 && len(block.Transactions) > 0 {
		block.Txids = nil
		for _, tx := range block.Transactions {
			tx.Txid = ""
		}
	}
}

func DefaultTxNormalizedFunc() func(*Transaction) string {
	return normalizedTxFuncWithAlg(util.DefaultCryptoHash())
}

//build all tx
func (block *Block) Normalize() {
	block.NormalizeByCustom(DefaultTxNormalizedFunc())
}

func (block *Block) NormalizeByCustom(nf func(*Transaction) string) {
	if block == nil || block.Version == 0 {
		return
	}

	if txLen := len(block.Transactions); block.Version > 0 && txLen > 0 {
		block.Txids = make([]string, 0, txLen)
		for _, tx := range block.Transactions {
			tx.Txid = nf(tx)
			block.Txids = append(block.Txids, tx.GetTxid())
		}
	}
}

// GetHash returns the hash of this block.
func (block *Block) GetHash() ([]byte, error) {

	if block == nil {
		return nil, fmt.Errorf("block is nil")
	}

	switch block.Version {
	case 0:
		return block.legacyHash()
	case 1:
		return block.hashV1()
	default:
		return nil, fmt.Errorf("No implement for version %d", block.Version)
	}
}

func (block *Block) hashV1() ([]byte, error) {

	normalize := makeNormalizeBlock(block)
	defer normalize.Resume(block)

	h := util.DefaultCryptoHash()

	err := binary.Write(h, binary.BigEndian, block.Version)
	if err != nil {
		return nil, err
	}

	//like transaction, we do not digest nano part
	if block.Timestamp != nil {
		err = binary.Write(h, binary.BigEndian, block.Timestamp.GetSeconds())
		if err != nil {
			return nil, err
		}
	}

	hw := util.NewHashWriter(h)

	err = hw.Write(block.StateHash).Write(block.PreviousBlockHash).Write(block.ConsensusMetadata).Error()
	if err != nil {
		return nil, err
	}

	for _, txid := range block.Txids {
		err = hw.Write([]byte(txid)).Error()
		if err != nil {
			return nil, err
		}
	}

	return h.Sum(nil), nil

}

// returns the hash of this block by the legacy way
func (block *Block) legacyHash() ([]byte, error) {

	//legacy way use special "normalized" process
	txids := block.Txids
	nonHashData := block.NonHashData

	resume := func() {
		block.Txids = txids
		block.NonHashData = nonHashData
	}

	block.Txids = nil
	block.NonHashData = nil

	defer resume()

	data, err := proto.Marshal(block)
	if err != nil {
		return nil, fmt.Errorf("Could not calculate hash of block: %s", err)
	}

	hash := util.ComputeCryptoHash(data)
	return hash, nil
}

// GetStateHash returns the stateHash stored in this block. The stateHash
// is the value returned by state.GetHash() after running all transactions in
// the block.
//func (block *Block) GetStateHash() []byte {
//	return block.StateHash
//}

// SetPreviousBlockHash sets the hash of the previous block. This will be
// called by blockchain.AddBlock when then the block is added.
func (block *Block) SetPreviousBlockHash(previousBlockHash []byte) {
	block.PreviousBlockHash = previousBlockHash
}

func (block *Block) CloneBlock() (*Block, error) {

	newBlock := new(Block)
	clonedByte, err := proto.Marshal(block)
	if err != nil {
		return nil, err
	}
	if err = proto.Unmarshal(clonedByte, newBlock); err != nil {
		return nil, err
	}

	return newBlock, nil
}

// Remove payload from deploy transactions. This is done to make block
// events more lightweight as the payload for these types of transactions
// can be very large.
func (block *Block) GetPruneDeployment() *Block {

	var prunedTxs []*Transaction
	cloneTx := func(transaction *Transaction) *Transaction {
		if transaction.Type != Transaction_CHAINCODE_DEPLOY {
			return transaction
		}

		deploymentSpec := &ChaincodeDeploymentSpec{}
		err := proto.Unmarshal(transaction.GetPayload(), deploymentSpec)
		if err != nil {
			logger.Errorf("Error unmarshalling deployment transaction for block event: %s", err)
			return transaction
		}
		deploymentSpec.CodePackage = nil
		deploymentSpecBytes, err := proto.Marshal(deploymentSpec)
		if err != nil {
			logger.Errorf("Error marshalling deployment transaction for block event: %s", err)
			return transaction
		}

		defer func(pl []byte) { transaction.Payload = pl }(transaction.GetPayload())
		transaction.Payload = nil

		//we do a simple-clone way to copy all txs
		clonedTx := new(Transaction)
		clonedByte, _ := proto.Marshal(transaction)
		if err := proto.Unmarshal(clonedByte, clonedTx); err != nil {
			logger.Errorf("Error unmarshalling pruned transaction%s", err)
			return transaction
		}
		clonedTx.Payload = deploymentSpecBytes
		return clonedTx
	}

	for _, transaction := range block.GetTransactions() {
		prunedTxs = append(prunedTxs, cloneTx(transaction))
	}

	defer func(txs []*Transaction) { block.Transactions = txs }(block.GetTransactions())
	block.Transactions = nil

	if newBlock, err := block.CloneBlock(); err != nil {
		logger.Errorf("Error unmarshalling pruned block %s", err)
		return block
	} else {
		return newBlock
	}

}

// UnmarshallBlock converts a byte array generated by Bytes() back to a block.
func UnmarshallBlock(blockBytes []byte) (*Block, error) {
	block := &Block{}
	err := proto.Unmarshal(blockBytes, block)
	if err != nil {
		logger.Errorf("Error unmarshalling block: %s", err)
		return nil, fmt.Errorf("Could not unmarshal block: %s", err)
	}

	return block, nil
}
