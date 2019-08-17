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
	"default",
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

//a wrapper for global ocDB
type OpenchainDB struct {
	//lock to access db field
	sync.RWMutex
	db    *ocDB
	dbTag string

	baseOpt      *gorocksdb.Options
	indexesCFOpt *gorocksdb.Options

	managedSnapshots struct {
		sync.Mutex
		m map[string]*DBSnapshot
	}
}

var originalDB = &OpenchainDB{db: &ocDB{}}

type ocDB struct {
	baseHandler
	openchainCFs
	dbName string

	extendedLock chan int //use a channel as locking for opening extend interface
	onReleased   func()
}

func (oc *ocDB) DropDB() {

	err := DropDB(oc.dbName)

	if err == nil {
		dbLogger.Infof("[%s] Drop whole db <%s>", printGID, oc.dbName)
	} else {
		dbLogger.Errorf("[%s] Drop whole db <%s> FAIL: %s", printGID, oc.dbName, err)
	}
}

func (oc *ocDB) release() bool {

	if oc == nil {
		return false
	}

	//we "absorb" a lock, the final release also "absorb" one, so
	//the one which could not asbord any lock will be the last
	//and response to do cleaning
	select {
	case <-oc.extendedLock:
		return false
	default:
		dbLogger.Infof("[%s] Final release current db <%s>", printGID, oc.dbName)

		if oc.onReleased != nil {
			oc.onReleased()
		} else {
			//sanity check
			panic("The caller abandon this object do not assigned a onRelease method")
		}

		return true
	}

}

func (openchainDB *ocDB) postOpen() {
	openchainDB.feed(openchainDB.cfMap)

	//TODO: custom maxOpenedExtend
	openchainDB.extendedLock = make(chan int, maxOpenedExtend)
}

func (openchainDB *ocDB) open(dbpath string, opts *gorocksdb.Options, cfopts []*gorocksdb.Options) error {

	openchainDB.dbName = dbpath

	hdb, cfHandlers, err := gorocksdb.OpenDbColumnFamilies(opts, dbpath, columnfamilies, cfopts)

	if err != nil {
		dbLogger.Error("Error opening DB:", err)
		return nil
	}

	if len(cfHandlers) != len(columnfamilies) {
		return errors.New("rocksdb may ruin or not work as expected")
	}

	dbLogger.Infof("gorocksdb.OpenDbColumnFamilies <%s>, len cfHandlers<%d>", dbpath, len(cfHandlers))
	openchainDB.DB = hdb

	//feed cfs
	openchainDB.cfMap = make(map[string]*gorocksdb.ColumnFamilyHandle)
	for i, cfName := range columnfamilies {
		openchainDB.cfMap[cfName] = cfHandlers[i]
	}

	openchainDB.postOpen()
	return nil
}

func (openchainDB *ocDB) recreateCFs(cfName []string, opts []*gorocksdb.Options) error {

	var err error
	for i, name := range cfName {
		if name == "" {
			continue
		}

		openchainDB.cfMap[name], err = openchainDB.CreateColumnFamily(opts[i], name)
		if err != nil {
			dbLogger.Errorf("Error creating [%s]: %s", name, err)
			return err
		}
		dbLogger.Infof("Recreate %s", name)
	}

	return nil
}

func (openchainDB *OpenchainDB) GetDBPath() string { return openchainDB.db.dbName }
func (openchainDB *OpenchainDB) GetDBTag() string  { return openchainDB.dbTag }

func (openchainDB *OpenchainDB) getDBKey(kc string) []string {
	return []string{openchainDB.dbTag, kc}
}

// func (openchainDB *OpenchainDB) getLegacyDBKey(kc string) []byte {
// 	if openchainDB.dbTag == "" {
// 		return []byte(kc)
// 	} else {
// 		return []byte(openchainDB.dbTag + "." + kc)
// 	}
// }

func (openchainDB *OpenchainDB) CleanManagedSnapshot() {
	openchainDB.managedSnapshots.Lock()
	defer openchainDB.managedSnapshots.Unlock()

	for _, sn := range openchainDB.managedSnapshots.m {
		sn.Release()
	}
	openchainDB.managedSnapshots.m = make(map[string]*DBSnapshot)
}

func (openchainDB *OpenchainDB) GetManagedSnapshot(name string) *DBSnapshot {
	openchainDB.managedSnapshots.Lock()
	defer openchainDB.managedSnapshots.Unlock()
	if ret := openchainDB.managedSnapshots.m[name]; ret != nil {
		return ret.clone()
	} else {
		return nil
	}
}

// unmanage the snapshot
func (openchainDB *OpenchainDB) UnManageSnapshot(name string) {
	openchainDB.managedSnapshots.Lock()
	defer openchainDB.managedSnapshots.Unlock()
	ret := openchainDB.managedSnapshots.m[name]
	if ret != nil {
		delete(openchainDB.managedSnapshots.m, name)
		ret.Release()
	}
}

//manage a new snapshot for current db, if name is duplicated, the old one is returned
//and should be released. notice the managed snapshot CANNOT call Release anymore
func (openchainDB *OpenchainDB) ManageSnapshot(name string) *DBSnapshot {

	newsn := openchainDB.GetSnapshot()
	newsn.hosting = &snapshotManager{count: 1}

	openchainDB.managedSnapshots.Lock()
	defer openchainDB.managedSnapshots.Unlock()

	//sanity check ...
	if len(openchainDB.managedSnapshots.m) > maxOpenedExtend/2 {
		dbLogger.Warningf("We have cached too many snapshots, never cache current")
		newsn.Release()
		return nil
	}

	old := openchainDB.managedSnapshots.m[name]
	openchainDB.managedSnapshots.m[name] = newsn
	return old
}

func (openchainDB *OpenchainDB) GetDBVersion() int {

	//v, _ := globalDataDB.get(globalDataDB.persistCF, openchainDB.getDBKey(currentVersionKey))
	v, _ := odbPersistor.LoadByKeys(openchainDB.getDBKey(currentVersionKey))
	if len(v) == 0 {
		return 0
	}
	return int(v[0])
}

func (openchainDB *OpenchainDB) UpdateDBVersion(v int) error {
	return odbPersistor.StoreByKeys(openchainDB.getDBKey(currentVersionKey), []byte{byte(v)})
	//return globalDataDB.put(globalDataDB.persistCF, openchainDB.getDBKey(currentVersionKey), []byte{byte(v)})
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

	openchainDB.Lock()
	defer openchainDB.Unlock()

	oldDB := openchainDB.db
	//TODO: we must create a rerollable cache for doing dropping cf task ...
	if err := openchainDB.db.DropColumnFamily(oldDB.StateCF); err != nil {
		dbLogger.Errorf("Error dropping state CF: %s", err)
		return err
	}

	if err := openchainDB.db.DropColumnFamily(oldDB.StateDeltaCF); err != nil {
		dbLogger.Errorf("Error dropping state delta CF: %s", err)
		return err
	}

	//move data ...
	newDB := &ocDB{
		baseHandler: openchainDB.db.baseHandler,
		dbName:      openchainDB.db.dbName,
	}

	_, cfopts := openchainDB.openDBOptions()
	if err := newDB.recreateCFs([]string{"",
		StateCF, StateDeltaCF}, cfopts); err != nil {
		return err
	}

	newDB.postOpen()

	defer oldDB.release()
	openchainDB.db = newDB

	dbLogger.Debugf("Status of new/old DB: %v", newDB.openchainCFs, oldDB.openchainCFs)
	oldDB.onReleased = func() {
		oldDB.StateCF.Destroy()
		oldDB.StateDeltaCF.Destroy()
		dbLogger.Infof("Destroy 2 state cf objects")
	}

	return nil
}

func (openchainDB *OpenchainDB) DeleteAll() error {

	openchainDB.Lock()
	defer openchainDB.Unlock()

	oldDB := openchainDB.db

	if err := openchainDB.db.DropColumnFamily(oldDB.StateCF); err != nil {
		dbLogger.Errorf("Error dropping state CF: %s", err)
		return err
	}

	if err := openchainDB.db.DropColumnFamily(oldDB.StateDeltaCF); err != nil {
		dbLogger.Errorf("Error dropping state delta CF: %s", err)
		return err
	}

	if err := openchainDB.db.DropColumnFamily(oldDB.BlockchainCF); err != nil {
		dbLogger.Errorf("Error dropping chain CF: %s", err)
		return err
	}

	if err := openchainDB.db.DropColumnFamily(oldDB.IndexesCF); err != nil {
		dbLogger.Errorf("Error dropping index CF: %s", err)
		return err
	}

	newDB := &ocDB{
		baseHandler: openchainDB.db.baseHandler,
		dbName:      openchainDB.db.dbName,
	}

	_, cfopts := openchainDB.openDBOptions()
	if err := newDB.recreateCFs([]string{BlockchainCF,
		StateCF, StateDeltaCF, IndexesCF}, cfopts); err != nil {
		return err
	}

	newDB.postOpen()

	defer oldDB.release()
	openchainDB.db = newDB

	dbLogger.Debugf("Status of new/old DB: %v", newDB.openchainCFs, oldDB.openchainCFs)
	oldDB.onReleased = func() {
		oldDB.StateCF.Destroy()
		oldDB.StateDeltaCF.Destroy()
		oldDB.BlockchainCF.Destroy()
		oldDB.IndexesCF.Destroy()
		dbLogger.Infof("Destroy all of 4 main cf objects")
	}

	return nil
}

const (
	//the maxium of long-run rocksdb interfaces can be open at the same time
	maxOpenedExtend = 128
)

type snapshotManager struct {
	sync.Mutex
	count uint
}

func (m *snapshotManager) addRef() {
	m.Lock()
	defer m.Unlock()
	//sanity check
	if m.count == 0 {
		panic("Try to ref an object which has been released")
	}
	m.count++
}

func (m *snapshotManager) releaseRef() bool {
	m.Lock()
	defer m.Unlock()
	//sanity check
	if m.count == 0 {
		panic("Try to deref an object which has been released")
	}
	m.count--
	return m.count == 0
}

type DBSnapshot struct {
	h        *ocDB
	snapshot *gorocksdb.Snapshot
	hosting  *snapshotManager
}

type DBIterator struct {
	h *ocDB
	*gorocksdb.Iterator
}

type DBWriteBatch struct {
	h *ocDB
	*gorocksdb.WriteBatch
}

// GetSnapshot create a point-in-time view of the DB.
func (openchainDB *OpenchainDB) GetSnapshot() *DBSnapshot {
	openchainDB.RLock()
	defer openchainDB.RUnlock()

	openchainDB.db.extendedLock <- 0
	return &DBSnapshot{openchainDB.db,
		openchainDB.db.NewSnapshot(), nil}
}

func (openchainDB *OpenchainDB) getExtended() *ocDB {

	openchainDB.RLock()
	defer openchainDB.RUnlock()

	openchainDB.db.extendedLock <- 0
	return openchainDB.db
}

func (openchainDB *OpenchainDB) NewWriteBatch() *DBWriteBatch {
	return &DBWriteBatch{openchainDB.getExtended(), gorocksdb.NewWriteBatch()}
}

func (openchainDB *OpenchainDB) GetIterator(cfName string) *DBIterator {
	theDb := openchainDB.getExtended()

	cf := theDb.cfMap[cfName]

	if cf == nil {
		panic(fmt.Sprintf("Wrong CF Name %s", cfName))
	}

	opt := gorocksdb.NewDefaultReadOptions()
	opt.SetFillCache(true)
	defer opt.Destroy()

	return &DBIterator{theDb, theDb.NewIteratorCF(opt, cf)}
}

func (e *DBWriteBatch) Destroy() {

	if e == nil {
		return
	} else if e.h == nil {
		panic("Object is duplicated destroy")
	}

	if e.WriteBatch != nil {
		e.WriteBatch.Destroy()
	}
	e.h.release()
	e.h = nil
}

func (e *DBIterator) Close() {

	if e == nil {
		return
	} else if e.h == nil {
		panic("Object is duplicated closed")
	}

	if e.Iterator != nil {
		e.Iterator.Close()
	}
	e.h.release()
	e.h = nil
}

func (e *DBSnapshot) clone() *DBSnapshot {

	if e == nil {
		return nil
	} else if e.hosting == nil {
		//sanity check
		panic("Wrong code: called clone for unmanaged snapshot")
	}
	e.hosting.addRef()
	return &DBSnapshot{e.h, e.snapshot, e.hosting}
}

//protect the close function declared in ocDB to avoiding a wrong calling
func (*DBSnapshot) Close(*DBSnapshot) {}

func (e *DBSnapshot) Release() {

	if e == nil {
		return
	} else if e.h == nil {
		//sanity check
		panic("snapshot is duplicated release")
	}

	if e.hosting != nil && !e.hosting.releaseRef() {
		return
	}
	if e.snapshot != nil {
		e.h.ReleaseSnapshot(e.snapshot)
	}
	e.h.release()
	e.h = nil
}

func (e *DBWriteBatch) GetCFs() *openchainCFs {
	return &e.h.openchainCFs
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
	return e.h.getFromSnapshot(e.snapshot, e.h.BlockchainCF, key)
}

func (e *DBSnapshot) GetFromStateCFSnapshot(key []byte) ([]byte, error) {

	if e.snapshot == nil {
		return nil, fmt.Errorf("Snapshot is not inited")
	}

	return e.h.getFromSnapshot(e.snapshot, e.h.StateCF, key)
}

func (e *DBSnapshot) GetFromIndexCFSnapshot(key []byte) ([]byte, error) {

	if e.snapshot == nil {
		return nil, fmt.Errorf("Snapshot is not inited")
	}

	return e.h.getFromSnapshot(e.snapshot, e.h.IndexesCF, key)
}

// GetStateCFSnapshotIterator get iterator for column family - stateCF. This iterator
// is based on a snapshot and should be used for long running scans, such as
// reading the entire state. Remember to call iterator.Close() when you are done.
func (e *DBSnapshot) GetStateCFSnapshotIterator() *gorocksdb.Iterator {

	if e.snapshot == nil {
		dbLogger.Error("Snapshot is not inited")
		return nil
	}
	return e.h.getSnapshotIterator(e.snapshot, e.h.StateCF)
}

func (e *DBSnapshot) GetFromStateDeltaCFSnapshot(key []byte) ([]byte, error) {

	if e.snapshot == nil {
		return nil, fmt.Errorf("Snapshot is not inited")
	}
	return e.h.getFromSnapshot(e.snapshot, e.h.StateDeltaCF, key)
}
