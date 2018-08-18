/*
Copyright BlackPai Corp. 2016 All Rights Reserved.

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

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/protos"
	"github.com/spf13/viper"
	"github.com/abchain/fabric/flogging"
	"github.com/op/go-logging"
	"github.com/tecbot/gorocksdb"
	"github.com/abchain/fabric/core/util"
	"strconv"
)

var logger = logging.MustGetLogger("dbscan")

type detailPrinter func(data []byte)

func main() {
	flagSetName := os.Args[0]
	flagSet := flag.NewFlagSet(flagSetName, flag.ExitOnError)
	dbDirPtr := flagSet.String("dbpath", "", "path to db dump")
	statehashPtr := flagSet.String("statehash", "", "path to db dump")
	switchPtr := flagSet.String("switch", "", "path to db dump")
	dumpPtr := flagSet.String("dump", "", "path to db dump")
	flagSet.Parse(os.Args[1:])

	dbDir := *dbDirPtr
	statehash := *statehashPtr
	switchTarget := *switchPtr

	fmt.Printf("dbDir = [%s]\n", dbDir)
	fmt.Printf("switch to = [%s]\n", switchTarget)

	blockNum, _ := strconv.Atoi(statehash)
	switchTargetNum, _ := strconv.Atoi(switchTarget)

	//statehashByte, _ := hex.DecodeString(statehash)
	//fmt.Printf("statehash = [%x]\n", getBlockStateHash(uint64(blockNum)))
	//return

	flogging.LoggingInit("client")

	if len(dbDir) == 0 {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", flagSetName)
		flagSet.PrintDefaults()
		os.Exit(3)
	}

	viper.Set("peer.fileSystemPath", dbDir)

	// ensure dbDir exists
	if _, err := os.Stat(dbDir); os.IsNotExist(err) {
		fmt.Printf("<%s> does not exist\n", dbDir)
		os.Exit(4)
	}

	if _, err := os.Stat(dbDir + "/txdb"); os.IsNotExist(err) {
		fmt.Printf("<%s> does not contain a sub-dir named 'txdb'\n", dbDir)
		os.Exit(5)
	}

	db.Start()

	orgdb := db.GetDBHandle()

	if blockNum > 0 {
		fmt.Printf("statehash = [%x]\n", getBlockStateHash(uint64(blockNum)))
	}

	if switchTarget != "" {
		swtch(uint64(switchTargetNum), orgdb)
	}

	txdb := db.GetGlobalDBHandle()
	defer db.Stop()

	if len(*dumpPtr) > 0 {

		//scan(orgdb.GetIterator(db.IndexesCF).Iterator, db.IndexesCF, nil)
		scan(orgdb.GetIterator(db.BlockchainCF).Iterator, db.BlockchainCF, blockDetailPrinter)
		scan(orgdb.GetIterator(db.PersistCF).Iterator, db.PersistCF, nil)

		//scan(txdb.GetIterator(db.TxCF), db.TxCF, txDetailPrinter)
		scan(txdb.GetIterator(db.GlobalCF), db.GlobalCF, gsDetailPrinter)
		scan(txdb.GetIterator(db.PersistCF), db.PersistCF, nil)
	}
}



//func persistCFDetailPrinter(valueBytes []byte) {
//
//	v, err := protos.UnmarshallTransaction(valueBytes)
//
//	if err != nil {
//		return
//	}
//
//	fmt.Printf("	Txid = [%s]\n", v.Txid)
//	fmt.Printf("	Payload = [%x]\n", v.Payload)
//}

func swtch(target uint64, orgdb *db.OpenchainDB) error {

	size, err := fetchBlockchainSizeFromDB()
	if err != nil {
		return nil
	}

	if target >= size {
		fmt.Printf("BlockchainSize: %d\n", size)
		return nil
	}

	orgdb.StateSwitch(getBlockStateHash(target))
	return nil
}

func getBlockStateHash(blockNumber uint64) []byte {

	size, err := fetchBlockchainSizeFromDB()
	if err != nil {
		return nil
	}

	if blockNumber >= size {

		fmt.Printf("BlockchainSize: %d\n", size)

		return nil
	}


	block, err := fetchRawBlockFromDB(blockNumber)

	if err != nil {
		return nil
	}

	//fmt.Printf("====== Dump[%d] %v: ====== \n", blockNumber, block)

	return block.StateHash
}


var blockCountKey = []byte("blockCount")

func fetchBlockchainSizeFromDB() (uint64, error) {
	bytes, err := db.GetDBHandle().GetFromBlockchainCF(blockCountKey)
	if err != nil {
		return 0, err
	}
	if bytes == nil {
		return 0, nil
	}
	return util.DecodeToUint64(bytes), nil
}


func fetchRawBlockFromDB(blockNumber uint64) (*protos.Block, error) {

	blockBytes, err := db.GetDBHandle().GetFromBlockchainCF(util.EncodeUint64(blockNumber))
	if err != nil {
		return nil, err
	}
	if blockBytes == nil {
		return nil, nil
	}
	blk, err := protos.UnmarshallBlock(blockBytes)
	if err != nil {
		return nil, err
	}

	return blk, nil
}

func scan(itr *gorocksdb.Iterator, cfName string, printer detailPrinter) {

	if itr == nil {
		return
	}
	fmt.Printf("\n================================================================\n")
	fmt.Printf("====== Dump %s: ====== \n", cfName)
	fmt.Printf("================================================================\n")

	totalKVs := 0
	itr.SeekToFirst()
	for ; itr.Valid(); itr.Next() {
		k := itr.Key()
		v := itr.Value()
		keyBytes := k.Data()

		var keyName string
		if cfName == db.TxCF || cfName == db.PersistCF {
			keyName = string(keyBytes)
		} else {
			keyName = fmt.Sprintf("%x", keyBytes)
		}

		fmt.Printf("Index<%d>: key=[%s], value=[%x]\n", totalKVs, keyName, v.Data())
		if printer != nil {
			fmt.Println("    Value Details:")
			printer(v.Data())
			fmt.Println("")
		}

		k.Free()
		v.Free()
		totalKVs++

	}
	itr.Close()

	return
}

func txDetailPrinter(valueBytes []byte) {

	v, err := protos.UnmarshallTransaction(valueBytes)

	if err != nil {
		return
	}

	fmt.Printf("	Txid = [%s]\n", v.Txid)
	fmt.Printf("	Payload = [%x]\n", v.Payload)
}

func blockDetailPrinter(blockBytes []byte) {

	block, err := protos.UnmarshallBlock(blockBytes)

	if err != nil {
		return
	}

	fmt.Printf("	Number of transactions = [%d]\n", len(block.Transactions))
	fmt.Printf("	Number of txid = [%d]\n", len(block.Txids))
	fmt.Printf("	block version = [%d]\n", block.Version)
	fmt.Printf("	StateHash = [%x]\n", block.StateHash)
}

func gsDetailPrinter(inputBytes []byte) {

	gs, err := protos.UnmarshallGS(inputBytes)

	if err != nil {
		return
	}

	fmt.Printf("	Count = [%d]\n", gs.Count)
	fmt.Printf("	LastBranchNodeStateHash = [%x]\n", gs.LastBranchNodeStateHash)
	fmt.Printf("	ParentNodeStateHash = [%x]\n", gs.ParentNodeStateHash)
	fmt.Printf("	Branched = [%t]\n", gs.Branched())
	fmt.Printf("	NextNodeStateHash count = [%d]\n", len(gs.NextNodeStateHash))
}
