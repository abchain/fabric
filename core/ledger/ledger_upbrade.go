package ledger

import (
	"bytes"
	"fmt"
	"github.com/abchain/fabric/core/db"
	_ "github.com/abchain/fabric/core/util"
	"github.com/abchain/fabric/protos"
	protobuf "github.com/golang/protobuf/proto"
	_ "strconv"
)

const dbVersion = db.DBVersion

func UpgradeLedger(odb *db.OpenchainDB, checkonly bool) error {

	v := odb.GetDBVersion()
	ledgerLogger.Info("Current DB Version is", v)

	if v == dbVersion {
		return nil
	} else if v > dbVersion {
		return fmt.Errorf("Encounter version %d which is higher than expected (%d)", v, dbVersion)
	} else if checkonly {
		return fmt.Errorf("Old Version: %d, needs update to %d", v, dbVersion)
	}

	//low version, need update
	ledgerLogger.Infof("Start upgrading db from version %d to %d, this may require several steps ...", v, dbVersion)

	if len(ledger_updater) < dbVersion {
		panic("No enough upgrader provided")
	}

	ledgerLogger.Infof("Backuping ...")
	tag, err := db.Backup(odb)

	if err != nil {
		return fmt.Errorf("Backup current db fail: %s", err)
	}

	defer func() {

		if err != nil {
			ledgerLogger.Warningf("Current db may be ruined and a backup exist with tag %d,", tag)
			ledgerLogger.Warningf("Consult with your technique supporter for data recovery")
		}
	}()

	for ; v < dbVersion; v++ {
		ledgerLogger.Infof("Upgrading db from ver.%d to ver.%d", v, v+1)
		err = ledger_updater[v](odb)
		if err != nil {
			ledgerLogger.Error("Upgrade fail", err)
			return err
		}
	}

	//done, write new version
	err = odb.UpdateDBVersion(dbVersion)
	if err != nil {
		return fmt.Errorf("Final write db version fail: %s", err)
	}

	ledgerLogger.Infof("Upgrade finish!")

	bakpath := db.GetBackupPath(tag)

	for _, path := range bakpath {
		droperr := db.DropDB(path)
		if droperr != nil {
			ledgerLogger.Warningf("Clear backup created in upgrading fail: %s, need manually delete", droperr)
		}
	}

	return nil
}

/*
   on reconstruction, this require a bunch of memory to accommodate the whole
   chains. i.e: 10M blocks require ~320MB memory (for 256bit hash) but this
   should be enough for a very long era (even ethereum currently [2018] has only
   ~5M blocks)
*/
type reconstructInfo struct {
	begin   uint64
	newhash [][]byte
}

func reconstructBlockChain(odb *db.OpenchainDB) (err error, r *reconstructInfo) {

	var size uint64
	size, err = fetchBlockchainSizeFromDB(odb)
	if err != nil {
		return
	}

	var previousHash []byte
	var bytesBefore, bytesAfter uint64
	var writeBatch *db.DBWriteBatch
	defer func() {
		if err == nil && writeBatch != nil {
			err = writeBatch.BatchCommit()
			//notice the difference between "defer writeBatch.Destroy()"
			writeBatch.Destroy()
		}
	}()

	opBatch := uint64(8)

	for i := uint64(0); i < size; i++ {

		if i%opBatch == 0 {
			if writeBatch != nil {
				err = writeBatch.BatchCommit()
				if err != nil {
					return
				}
				writeBatch.Destroy()
			}
			writeBatch = odb.NewWriteBatch()
		}

		blkk := encodeBlockNumberDBKey(i)

		var blockBytes []byte
		blockBytes, err = odb.GetFromBlockchainCF(blkk)
		if err != nil {
			return
		}

		bytesBefore = bytesBefore + uint64(len(blockBytes))

		var block *protos.Block
		block, err = protos.UnmarshallBlock(blockBytes)
		if err != nil {
			return
		}

		if block.Version == 0 && len(block.Transactions) != 0 {
			//it was the "most legacy" block so we need to normalize it
			block.Txids = make([]string, len(block.Transactions))
			for i, tx := range block.Transactions {
				block.Txids[i] = tx.Txid
			}

			err = db.GetGlobalDBHandle().PutTransactions(block.Transactions)
			if err != nil {
				return
			}
		}

		if bytes.Compare(previousHash, block.GetPreviousBlockHash()) != 0 && previousHash != nil {

			if r == nil {
				ledgerLogger.Warningf("Block from %d has changed its hash from %x to %x, blocks reconstructed",
					i-1, block.GetPreviousBlockHash(), previousHash)
				r = &reconstructInfo{i - 1, make([][]byte, 0, size-i+1)}
				r.newhash = append(r.newhash, previousHash)

				writeBatch.PutCF(writeBatch.GetCFs().IndexesCF,
					encodeBlockHashKey(previousHash), encodeBlockNumber(i-1))
			}
			block.PreviousBlockHash = previousHash
		}

		previousHash, err = block.GetHash()
		if err != nil {
			return
		}
		if r != nil {
			//we must also update index of block
			writeBatch.PutCF(writeBatch.GetCFs().IndexesCF,
				encodeBlockHashKey(previousHash), blkk)
			r.newhash = append(r.newhash, previousHash)
		}

		blockBytes, err = block.GetBlockBytes()
		if err != nil {
			return
		}

		writeBatch.PutCF(writeBatch.GetCFs().BlockchainCF, blkk, blockBytes)
		bytesAfter = bytesAfter + uint64(len(blockBytes))
	}

	ledgerLogger.Infof("Reconstruct blockchain, from %d bytes to %d bytes",
		bytesBefore, bytesAfter)

	return
}

//scan current blockchain and build it as the ONLY global state route
func reconstructGlobalState(odb *db.OpenchainDB) error {

	size, err := fetchBlockchainSizeFromDB(odb)
	if err != nil {
		return fmt.Errorf("Fetch size fail: %s", err)
	}

	//if we have not block yet, skip
	if size == 0 {
		return nil
	}

	//the "root" hash is arbitary, so we just add some marking here
	//(differ from the makegensis module, which is just the "empty" statehash of ledger)
	lastState := []byte("FABRIC_DB_UPGRADING_FROM_V0")
	err = db.GetGlobalDBHandle().PutGenesisGlobalState(lastState)
	if err != nil {
		return fmt.Errorf("Put gensis global state fail: %s", err)
	}

	for n := uint64(1); n < size; n++ {

		block, err := fetchRawBlockFromDB(odb, n)
		if err != nil {
			return fmt.Errorf("Fetch block fail: %s", err)
		}
		if block == nil {
			return fmt.Errorf("Block %d is not exist yet", n)
		}

		err = db.GetGlobalDBHandle().AddGlobalState(lastState, block.GetStateHash())

		if err != nil {
			return fmt.Errorf("Put global state fail: %s", err)
		}

		lastState = block.GetStateHash()
	}

	return nil
}

//This is a highly hacking for the legacy PBFT module and we should not use persistCF
//for purpose of consensus in the later version:
//Here we have a brief explanation according to the PBFT TOCS and pbft-persist.go:

/*
	The PBFT module persisted following items for preparing a view change:

P/QSet: the prepare messages it have for the current view, it have digest of a batch,
	that is, some txs waiting for committing and not involved in any block so they
	do not need to be change

reqbatch: a request batch it have got (if peer is primary) for handling, they have no
    matter with blocks, too

chkpt: the checkpoint it have known, which is just expressed by the blockchain info
	so they must be changed if we reconstruct the whole chain. Peer will use the id
	saved in chkpt for syncing so if old blockhash is used in viewchange message
	everything is ruined

*/

var rawLegacyPBFTPrefix = []byte(db.RawLegacyPBFTPrefix)

func updateLegacyPBFTChkp(odb *db.OpenchainDB, itr *db.DBIterator, r *reconstructInfo) error {

	wb := odb.NewWriteBatch()
	cf := wb.GetCFs().PersistCF
	defer wb.Destroy()

	for ; itr.ValidForPrefix(rawLegacyPBFTPrefix); itr.Next() {

		k := itr.Key().Data()

		var seqNo uint64
		if _, err := fmt.Sscanf(string(k[len(rawLegacyPBFTPrefix):]), "%d", &seqNo); err != nil {
			ledgerLogger.Warningf("Could not restore checkpoint key %s", string(k))
		} else if seqNo > r.begin {
			//construct the checkpoint id, it was generated by helper.GetBlockchainInfoBlob
			//which just call GetBlockchainInfo in ledger, which was a harshal of protos.BlockchainInfo
			info := &protos.BlockchainInfo{
				Height:            seqNo,
				CurrentBlockHash:  r.newhash[seqNo-r.begin],
				PreviousBlockHash: r.newhash[seqNo-r.begin-1],
			}
			newv, err := protobuf.Marshal(info)
			if err != nil {
				return fmt.Errorf("Encode chkpoint %d fail: %s", seqNo, err)
			}

			wb.PutCF(cf, k, newv)
			ledgerLogger.Debugf("Update PBFT checkpoint at %d", seqNo)

		} else if seqNo == r.begin {
			//a little special: the previous blockhash has to be obtained from old checkpoint
			info := &protos.BlockchainInfo{}
			err := protobuf.Unmarshal(itr.Value().Data(), info)
			if err != nil {
				return fmt.Errorf("Could not decode chkpoint %d: %s", seqNo, err)
			}

			if info.Height != seqNo {
				//may be we can just panic because we have a wrong db?
				return fmt.Errorf("Checkpoint data is not match in height: %d vs %d", info.Height, seqNo)
			}

			info.CurrentBlockHash = r.newhash[0]

			newv, err := protobuf.Marshal(info)
			if err != nil {
				return fmt.Errorf("Encode chkpoint %d fail: %s", seqNo, err)
			}

			wb.PutCF(cf, k, newv)
			ledgerLogger.Debugf("Update PBFT checkpoint at %d", seqNo)
		}
	}

	return wb.BatchCommit()
}

func updateDBToV1(odb *db.OpenchainDB) error {

	//rewrite blockchainCF
	err, reconstruct := reconstructBlockChain(odb)
	if err != nil {
		return fmt.Errorf("Reconstruct blockchain fail: %s", err)
	}

	//db v0 have no global state so we need to rebuild it first
	err = reconstructGlobalState(odb)
	if err != nil {
		return fmt.Errorf("Reconstruct globalstate fail: %s", err)
	}

	//copying keys in persistCF for required content
	//currently it just the peer-persisted content in 0.6, which have no
	//any prefix and we need to add PeerStoreKeyPrefix on it
	itr := odb.GetIterator(db.PersistCF)
	defer itr.Close()

	wb := db.GetGlobalDBHandle().NewWriteBatch()
	defer wb.Destroy()

	var peerKeyMove uint
	wbatch := uint(128)

	for ; itr.Valid(); itr.Next() {

		k := itr.Key().Data()

		//if hash is update, the persisted checkpoint for PBFT must be also updated
		if reconstruct != nil {
			if bytes.HasPrefix(k, rawLegacyPBFTPrefix) {
				err = updateLegacyPBFTChkp(odb, itr, reconstruct)
				if err != nil {
					return fmt.Errorf("Reconstruct PBFT consensus fail: %s", err)
				}
			}

			//after run in updateLegacyPBFTChkp the itr may become invalid
			if !itr.Valid() {
				break
			}
		}

		//check prefix, handling consensus or other
		if bytes.HasPrefix(k, []byte("consensus.")) {
			//we do not need to move consensus kv
		} else {
			const peerStoreKeyPrefix = "peer"
			peerPersistor := db.NewPersistor(db.GetPersistKey(peerStoreKeyPrefix))
			//treat it as peer persisted data and move
			peerPersistor.StoreOnBatch(wb, []string{string(k)}, itr.Value().Data())
			peerKeyMove++

			if peerKeyMove%wbatch == 0 {
				err = db.GetGlobalDBHandle().BatchCommit(wb)
				if err != nil {
					return fmt.Errorf("Batch commit persistcf fail: %s", err)
				}
			}

		}
	}

	if peerKeyMove%wbatch != 0 {
		err = db.GetGlobalDBHandle().BatchCommit(wb)
		if err != nil {
			return fmt.Errorf("Last batch commit persistcf fail: %s", err)
		}
	}

	return nil
}

var ledger_updater = []func(odb *db.OpenchainDB) error{updateDBToV1}
