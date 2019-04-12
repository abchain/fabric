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

package db

import (
	"errors"
	"fmt"
	"github.com/tecbot/gorocksdb"
	"sync"
)

var columnfamilies = []string{
	BlockchainCF, // blocks of the block chain
	StateCF,      // world state
	StateDeltaCF, // open transaction state
	IndexesCF,    // tx uuid -> blockno
	PersistCF,    // persistent per-peer state (consensus)
}

type openchainCFs struct {
	BlockchainCF *gorocksdb.ColumnFamilyHandle
	StateCF      *gorocksdb.ColumnFamilyHandle
	StateDeltaCF *gorocksdb.ColumnFamilyHandle
	IndexesCF    *gorocksdb.ColumnFamilyHandle
	PersistCF    *gorocksdb.ColumnFamilyHandle
}

func (c *openchainCFs) feed(cfmap map[string]*gorocksdb.ColumnFamilyHandle) {
	c.BlockchainCF = cfmap[BlockchainCF]
	c.StateCF = cfmap[StateCF]
	c.StateDeltaCF = cfmap[StateDeltaCF]
	c.IndexesCF = cfmap[IndexesCF]
	c.PersistCF = cfmap[PersistCF]
}

type ocDB struct {
	baseHandler
	openchainCFs
	extendedLock chan int //use a channel as locking for opening extend interface
	dbName       string
	finalDrop    bool
}

type OpenchainDB struct {
	db           *ocDB
	dbTag        string
	indexesCFOpt *gorocksdb.Options
	sync.RWMutex
}

var originalDB = &OpenchainDB{db: &ocDB{}}

func (oc *ocDB) dropDB() {

	err := DropDB(oc.dbName)

	if err == nil {
		dbLogger.Infof("[%s] Drop whole db <%s>", printGID, oc.dbName)
	} else {
		dbLogger.Errorf("[%s] Drop whole db <%s> FAIL: %s", printGID, oc.dbName, err)
	}
}

func (openchainDB *ocDB) open(dbpath string, cfopts []*gorocksdb.Options) error {

	openchainDB.dbName = dbpath

	cfhandlers := openchainDB.opendb(dbpath, columnfamilies, cfopts)

	if len(cfhandlers) != len(columnfamilies) {
		return errors.New("rocksdb may ruin or not work as expected")
	}

	//feed cfs
	openchainDB.cfMap = make(map[string]*gorocksdb.ColumnFamilyHandle)
	for i, cfName := range columnfamilies {
		openchainDB.cfMap[cfName] = cfhandlers[i]
	}

	openchainDB.feed(openchainDB.cfMap)

	//TODO: custom maxOpenedExtend
	openchainDB.extendedLock = make(chan int, maxOpenedExtend)

	return nil
}

func (openchainDB *OpenchainDB) getDBKey(kc string) []byte {
	if openchainDB.dbTag == "" {
		return []byte(kc)
	} else {
		return []byte(openchainDB.dbTag + "." + kc)
	}
}

func (openchainDB *OpenchainDB) GetDBVersion() int {

	v, _ := globalDataDB.get(globalDataDB.persistCF, openchainDB.getDBKey(currentVersionKey))
	if len(v) == 0 {
		return 0
	}
	return int(v[0])
}

func (openchainDB *OpenchainDB) UpdateDBVersion(v int) error {
	return globalDataDB.put(globalDataDB.persistCF, openchainDB.getDBKey(currentVersionKey), []byte{byte(v)})
}

// override methods with rwlock
func (openchainDB *OpenchainDB) GetValue(cfname string, key []byte) ([]byte, error) {
	openchainDB.RLock()
	defer openchainDB.RUnlock()
	return openchainDB.db.GetValue(cfname, key)
}

func (openchainDB *OpenchainDB) DeleteKey(cfname string, key []byte) error {
	openchainDB.RLock()
	defer openchainDB.RUnlock()
	return openchainDB.db.DeleteKey(cfname, key)
}

func (openchainDB *OpenchainDB) PutValue(cfname string, key []byte, value []byte) error {
	openchainDB.RLock()
	defer openchainDB.RUnlock()
	return openchainDB.db.PutValue(cfname, key, value)
}

//func (orgdb *OpenchainDB) getTxids(blockNumber uint64) []string {
//
//	block, err := orgdb.FetchBlockFromDB(blockNumber, false)
//	if err != nil {
//		dbg.Errorf("Error Fetch BlockFromDB by blockNumber<%d>. Err: %s", blockNumber, err)
//		return nil
//	}
//
//	if block == nil {
//		dbg.Errorf("No such a block, blockNumber<%d>. Err: %s", blockNumber)
//		return nil
//	}
//
//	return block.Txids
//}

// func (orgdb *OpenchainDB) FetchBlockFromDB(blockNumber uint64) (*protos.Block, error) {

// 	orgdb.RLock()
// 	defer orgdb.RUnlock()

// 	blockBytes, err := orgdb.db.get(orgdb.db.BlockchainCF, EncodeBlockNumberDBKey(blockNumber))
// 	if err != nil {

// 		return nil, err
// 	}
// 	if blockBytes == nil {

// 		return nil, nil
// 	}
// 	block, errUnmarshall := protos.UnmarshallBlock(blockBytes)

// 	return block, errUnmarshall
// }

func (orgdb *OpenchainDB) GetFromBlockchainCF(key []byte) ([]byte, error) {

	orgdb.RLock()
	defer orgdb.RUnlock()

	return orgdb.db.get(orgdb.db.BlockchainCF, key)
}

// DeleteState delets ALL state keys/values from the DB. This is generally
// only used during state synchronization when creating a new state from
// a snapshot.
func (openchainDB *OpenchainDB) DeleteState() error {

	openchainDB.RLock()
	defer openchainDB.RUnlock()

	err := openchainDB.db.DropColumnFamily(openchainDB.db.StateCF)
	if err != nil {
		dbLogger.Errorf("Error dropping state CF: %s", err)
		return err
	}
	err = openchainDB.db.DropColumnFamily(openchainDB.db.StateDeltaCF)
	if err != nil {
		dbLogger.Errorf("Error dropping state delta CF: %s", err)
		return err
	}
	opts := gorocksdb.NewDefaultOptions()
	defer opts.Destroy()
	openchainDB.db.StateCF, err = openchainDB.db.CreateColumnFamily(opts, StateCF)
	if err != nil {
		dbLogger.Errorf("Error creating state CF: %s", err)
		return err
	}
	openchainDB.db.StateDeltaCF, err = openchainDB.db.CreateColumnFamily(opts, StateDeltaCF)
	if err != nil {
		dbLogger.Errorf("Error creating state delta CF: %s", err)
		return err
	}

	openchainDB.db.cfMap[StateCF] = openchainDB.db.StateCF
	openchainDB.db.cfMap[StateDeltaCF] = openchainDB.db.StateDeltaCF

	return nil
}

const (
	//the maxium of long-run rocksdb interfaces can be open at the same time
	maxOpenedExtend = 128
)

type extHandler struct {
	*ocDB
}

//protect the close function declared in ocDB to avoiding a wrong calling
func (extHandler) Close(extHandler) {}

type refCount struct {
	sync.Mutex
	count uint
}

type DBSnapshot struct {
	ref refCount

	extHandler
	snapshot *gorocksdb.Snapshot
}

type DBIterator struct {
	h extHandler
	*gorocksdb.Iterator
}

type DBWriteBatch struct {
	h extHandler
	*gorocksdb.WriteBatch
}

func (openchainDB *OpenchainDB) getExtended() *ocDB {

	openchainDB.RLock()
	defer openchainDB.RUnlock()

	openchainDB.db.extendedLock <- 0
	return openchainDB.db
}

//extend interface
// GetIterator returns an iterator for the given column family
func (e *extHandler) release() {

	//to make test and some wrapper also work
	if e.ocDB == nil {
		return
	}

	//we "absorb" a lock
	select {
	case <-e.extendedLock:
	default:
		dbLogger.Infof("[%s] Release current db <%s>", printGID, e.dbName)
		e.close()

		if e.finalDrop {
			e.dropDB()
		}
	}

}

func (openchainDB *OpenchainDB) NewWriteBatch() *DBWriteBatch {
	db := openchainDB.getExtended()

	return &DBWriteBatch{extHandler{db}, gorocksdb.NewWriteBatch()}
}

// GetSnapshot create a point-in-time view of the DB.
func (openchainDB *OpenchainDB) GetSnapshot() *DBSnapshot {

	db := openchainDB.getExtended()

	return &DBSnapshot{refCount{count: 1}, extHandler{db}, db.NewSnapshot()}
}

func (openchainDB *OpenchainDB) GetIterator(cfName string) *DBIterator {

	db := openchainDB.getExtended()

	cf := db.cfMap[cfName]

	if cf == nil {
		panic(fmt.Sprintf("Wrong CF Name %s", cfName))
	}

	opt := gorocksdb.NewDefaultReadOptions()
	opt.SetFillCache(true)
	defer opt.Destroy()

	return &DBIterator{extHandler{db}, db.NewIteratorCF(opt, cf)}
}

func (e *DBWriteBatch) Destroy() {

	if e == nil {
		return
	}

	if e.WriteBatch != nil {
		e.WriteBatch.Destroy()
	}
	e.h.release()
}

func (e *DBIterator) Close() {

	if e == nil {
		return
	}

	if e.Iterator != nil {
		e.Iterator.Close()
	}
	e.h.release()
}

func (e *DBSnapshot) Clone() *DBSnapshot {

	e.ref.Lock()
	defer e.ref.Unlock()
	e.ref.count++

	return e
}

func (e *DBSnapshot) Release() {

	if e == nil {
		return
	}

	e.ref.Lock()
	defer e.ref.Unlock()

	if e.ref.count == 0 {
		panic("wrong reference count in object")
	}
	e.ref.count--
	if e.ref.count > 0 {
		return
	}

	if e.snapshot != nil {
		e.ReleaseSnapshot(e.snapshot)
	}
	e.release()
}

func (e *DBWriteBatch) GetDBHandle() *ocDB {
	return e.h.ocDB
}

func (e *DBWriteBatch) BatchCommit() error {
	return e.h.BatchCommit(e.WriteBatch)
}

func (e *DBSnapshot) GetSnapshot() *gorocksdb.Snapshot {
	return e.snapshot
}

// // Some legacy entries, we make all "fromsnapshot" function becoming simple api (not member func)....
// func (e *DBSnapshot) FetchBlockchainSizeFromSnapshot() (uint64, error) {

// 	blockNumberBytes, err := e.GetFromBlockchainCFSnapshot(BlockCountKey)
// 	if err != nil {
// 		return 0, err
// 	}
// 	var blockNumber uint64
// 	if blockNumberBytes != nil {
// 		blockNumber = DecodeToUint64(blockNumberBytes)
// 	}
// 	return blockNumber, nil
// }

// GetFromBlockchainCFSnapshot get value for given key from column family in a DB snapshot - blockchainCF
func (e *DBSnapshot) GetFromBlockchainCFSnapshot(key []byte) ([]byte, error) {

	if e.snapshot == nil {
		return nil, fmt.Errorf("Snapshot is not inited")
	}
	return e.getFromSnapshot(e.snapshot, e.BlockchainCF, key)
}

func (e *DBSnapshot) GetFromStateCFSnapshot(key []byte) ([]byte, error) {

	if e.snapshot == nil {
		return nil, fmt.Errorf("Snapshot is not inited")
	}

	return e.getFromSnapshot(e.snapshot, e.StateCF, key)
}

// GetStateCFSnapshotIterator get iterator for column family - stateCF. This iterator
// is based on a snapshot and should be used for long running scans, such as
// reading the entire state. Remember to call iterator.Close() when you are done.
func (e *DBSnapshot) GetStateCFSnapshotIterator() *gorocksdb.Iterator {

	if e.snapshot == nil {
		dbLogger.Error("Snapshot is not inited")
		return nil
	}
	return e.getSnapshotIterator(e.snapshot, e.StateCF)
}

func (e *DBSnapshot) GetFromStateDeltaCFSnapshot(key []byte) ([]byte, error) {

	if e.snapshot == nil {
		return nil, fmt.Errorf("Snapshot is not inited")
	}
	return e.getFromSnapshot(e.snapshot, e.StateDeltaCF, key)
}
