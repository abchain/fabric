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
	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/op/go-logging"
	"sync"
	"time"
)

var genesisLogger = logging.MustGetLogger("genesis")

var makeGenesisError error
var once sync.Once

// MakeGenesis creates the genesis block and adds it to the blockchain.
func MakeGenesis() error {
	once.Do(func() {
		ledger, err := ledger.GetLedger()
		if err != nil {
			makeGenesisError = err
			return
		}

		if ledger.GetBlockchainSize() == 0 {
			genesisLogger.Info("Creating genesis block.")

			gensisstate, err := ledger.GetCurrentStateHash()
			if err != nil {
				makeGenesisError = err
				return
			}

			db.GetGlobalDBHandle().PutGenesisGlobalState(gensisstate)

			if makeGenesisError = ledger.BeginTxBatch(0); makeGenesisError == nil {
				makeGenesisError = ledger.CommitTxBatch(0, nil, nil, nil)
			}
		}
	})
	return makeGenesisError
}

type genesisBlock struct {
	*protos.Block
}

var defaultTime = time.Date(2019, time.June, 9, 15, 0, 0, 0, time.FixedZone("UTC-8", -8*60*60))

func NewGenesisBlock(blk *protos.Block) genesisBlock { return genesisBlock{blk} }

func NewGenesisBlockTemplateDefault(metadata []byte) genesisBlock {
	return NewGenesisBlockTemplate(metadata, defaultTime)
}

func NewGenesisBlockTemplate(metadata []byte, epochT time.Time) genesisBlock {
	block := protos.NewBlock(nil, metadata)
	block.Timestamp = &timestamp.Timestamp{Seconds: epochT.Unix()}
	return genesisBlock{block}
}

func (b genesisBlock) MakeGenesisForLedgerDirect(l *ledger.Ledger, initState ledger.TxExecStates) error {

	if l.GetBlockchainSize() == 0 {

		commitAgent, err := ledger.NewTxEvaluatingAgent(l)
		if err != nil {
			return err
		}

		if initState.IsEmpty() {
			genesisLogger.Warningf("Require to build genesis block with empty initstate")
		} else {
			commitAgent.MergeExec(initState)
		}

		b.Block, err = commitAgent.PreviewBlock(0, b.Block)
		if err != nil {
			return err
		}

		blkcmt := ledger.NewBlockAgent(l)
		err = blkcmt.SyncCommitBlock(0, b.Block)
		if err != nil {
			return err
		}

		err = commitAgent.StateCommitOne(0, b.Block)
		if err != nil {
			return err
		}

		info, err := l.GetBlockchainInfo()
		if err != nil {
			return err
		}

		if shash := info.GetCurrentStateHash(); len(shash) == 0 {
			return ledger.ErrResourceNotFound
		} else if err = db.GetGlobalDBHandle().PutGenesisGlobalState(shash); err != nil {
			return err
		}
	}
	return nil
}

func (b genesisBlock) MakeGenesisForLedger(l *ledger.Ledger, chaincode string, initValue map[string][]byte) error {

	genesisLogger.Info("Creating genesis block for ledger", chaincode)

	exs := ledger.TxExecStates{}
	exs.InitForInvoking(l)
	if initValue == nil {
		initValue = map[string][]byte{"_genesis_": []byte{42, 42, 42}}
	}
	for k, v := range initValue {
		exs.Set(chaincode, k, v)
	}

	return b.MakeGenesisForLedgerDirect(l, exs)
}
