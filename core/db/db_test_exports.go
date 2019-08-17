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
	"github.com/tecbot/gorocksdb"
	"os"
	"testing"
)

// TestDBWrapper wraps the db. Can be used by other modules for testing
type TestDBWrapper struct {
	performCleanup bool
}

// NewTestDBWrapper constructs a new TestDBWrapper
func NewTestDBWrapper() *TestDBWrapper {
	return &TestDBWrapper{}
}

///////////////////////////
// Test db creation and cleanup functions

func (testDB *TestDBWrapper) GetDB() *OpenchainDB {
	return GetDBHandle()
}

// CleanDB This method closes existing db, remove the db dir.
// Can be called before starting a test so that data from other tests does not interfere
func (testDB *TestDBWrapper) CleanDB(t testing.TB) {
	// cleaning up test db here so that each test does not have to call it explicitly
	// at the end of the test
	testDB.cleanup()
	testDB.removeDBPath()
	t.Logf("Creating testDB")

	Start()
	testDB.performCleanup = true
}

// clean a cf, this is just for test purpose and do not care about concurrency situtaion
func (testDB *TestDBWrapper) CleanCF(t testing.TB, cfName string) {
	cf := GetDBHandle().db.GetCFByName(cfName)
	if err := GetDBHandle().db.DropColumnFamily(cf); err != nil {
		t.Fatal("drop cf fail", err)
	}

	var cfOpt *gorocksdb.Options

	switch cfName {
	case IndexesCF:
		cfOpt = GetDBHandle().indexesCFOpt
	default:
		cfOpt = GetDBHandle().baseOpt
	}

	newcf, err := GetDBHandle().db.CreateColumnFamily(cfOpt, cfName)
	if err != nil {
		t.Fatal("create new cf fail", err)
	}

	GetDBHandle().db.cfMap[cfName] = newcf
	GetDBHandle().db.IndexesCF = newcf
}

// CreateFreshDBGinkgo creates a fresh database for ginkgo testing
func (testDB *TestDBWrapper) CreateFreshDBGinkgo() {
	// cleaning up test db here so that each test does not have to call it explicitly
	// at the end of the test
	testDB.cleanup()
	testDB.removeDBPath()
	Start()
	testDB.performCleanup = true
}

func (testDB *TestDBWrapper) cleanup() {
	if testDB.performCleanup {
		Stop()
		testDB.performCleanup = false
	}
}

func (testDB *TestDBWrapper) removeDBPath() {
	if dbPathSetting != "" {
		os.RemoveAll(getDBPath(""))
	}
}

func (testDB *TestDBWrapper) NewWriteBatch() *DBWriteBatch {
	return GetDBHandle().NewWriteBatch()

}

// WriteToDB tests can use this method for persisting a given batch to db
func (testDB *TestDBWrapper) WriteToDB(t testing.TB, writeBatch *DBWriteBatch) {
	opt := gorocksdb.NewDefaultWriteOptions()
	defer opt.Destroy()
	err := GetDBHandle().db.Write(opt, writeBatch.WriteBatch)
	if err != nil {
		t.Fatalf("Error while writing to db. Error:%s", err)
	}
}

// GetFromDB gets the value for the given key from default column-family
func (testDB *TestDBWrapper) GetFromDB(t testing.TB, key []byte) []byte {
	db := GetDBHandle().db
	opt := gorocksdb.NewDefaultReadOptions()
	defer opt.Destroy()
	slice, err := db.Get(opt, key)
	defer slice.Free()
	if err != nil {
		t.Fatalf("Error while getting key-value from DB: %s", err)
	}
	value := append([]byte(nil), slice.Data()...)
	return value
}

// GetFromStateCF tests can use this method for getting value from StateCF column-family
func (testDB *TestDBWrapper) GetFromStateCF(t testing.TB, key []byte) []byte {
	openchainDB := GetDBHandle()
	value, err := openchainDB.GetValue(StateCF, key)
	if err != nil {
		t.Fatalf("Error while getting from db. Error:%s", err)
	}
	return value
}

// GetFromStateDeltaCF tests can use this method for getting value from StateDeltaCF column-family
func (testDB *TestDBWrapper) GetFromStateDeltaCF(t testing.TB, key []byte) []byte {
	openchainDB := GetDBHandle()
	value, err := openchainDB.GetValue(StateDeltaCF, key)
	if err != nil {
		t.Fatalf("Error while getting from db. Error:%s", err)
	}
	return value
}

// CloseDB closes the db
func (testDB *TestDBWrapper) CloseDB(t testing.TB) {
	Stop()
}

// OpenDB opens the db
func (testDB *TestDBWrapper) OpenDB(t testing.TB) {
	Start()
}

// GetEstimatedNumKeys returns estimated number of key-values in db. This is not accurate in all the cases
func (testDB *TestDBWrapper) GetEstimatedNumKeys(t testing.TB) map[string]string {
	openchainDB := GetDBHandle()
	result := make(map[string]string, 5)

	result["stateCF"] = openchainDB.db.GetPropertyCF("rocksdb.estimate-num-keys", openchainDB.db.StateCF)
	result["stateDeltaCF"] = openchainDB.db.GetPropertyCF("rocksdb.estimate-num-keys", openchainDB.db.StateDeltaCF)
	result["blockchainCF"] = openchainDB.db.GetPropertyCF("rocksdb.estimate-num-keys", openchainDB.db.BlockchainCF)
	result["indexCF"] = openchainDB.db.GetPropertyCF("rocksdb.estimate-num-keys", openchainDB.db.IndexesCF)
	return result
}

// GetDBStats returns statistics for the database
func (testDB *TestDBWrapper) GetDBStats() string {
	openchainDB := GetDBHandle()
	return openchainDB.db.GetProperty("rocksdb.stats")
}

func (testDB *TestDBWrapper) PutGenesisGlobalState(state []byte) error {
	return GetGlobalDBHandle().PutGenesisGlobalState(state)
}
