package db

import (
	"fmt"
	"github.com/abchain/fabric/core/util"
	_ "github.com/abchain/fabric/protos"
	"github.com/tecbot/gorocksdb"
	"path/filepath"
)

var CurrentDBPathKey = []byte("blockCount")

func getCheckPointPath(statehashname string) string {
	chkpPath := getDBPath("checkpoint")
	util.MkdirIfNotExist(chkpPath)

	return filepath.Join(chkpPath, statehashname)
}

func encodeStatehash(statehash []byte) string {
	return fmt.Sprintf("%x", statehash)
}

func createCheckpoint(db *gorocksdb.DB, cpPath string) error {
	checkpoint, err := db.NewCheckpoint()
	if err != nil {
		return fmt.Errorf("[%s] Create checkpoint object fail: %s", printGID, err)
	}
	defer checkpoint.Destroy()
	err = checkpoint.CreateCheckpoint(cpPath, 0)
	if err != nil {
		return fmt.Errorf("[%s] Copy checkpoint to %s fail: %s", printGID, cpPath, err)
	}

	return nil
}

const checkpointNamePrefix = "checkpoint"

var checkpointNamePersistorIndex = PersistKeyRegister(checkpointNamePrefix)

func (oc *OpenchainDB) CheckpointCurrent(statehash []byte) error {
	statename := encodeStatehash(statehash)

	//todo: can use persist data to check if checkpoint is existed
	err := createCheckpoint(oc.db.DB, getCheckPointPath(statename))
	if err != nil {
		return err
	}

	err = NewPersistor(checkpointNamePersistorIndex).StoreByKeys([]string{oc.dbTag, statename}, statehash)
	if err != nil {
		return err
	}

	return nil
}

func (oc *OpenchainDB) StateSwitch(statehash []byte) error {

	statename := encodeStatehash(statehash)

	dbLogger.Infof("[%s] Start state switching to %s", printGID, statename)
	opts, cfopts := oc.openDBOptions()

	//open checkpoint without a wrapper of ocdb
	//CAUTION: you CAN NOT build checkpoint from RO-opened db
	chkp, cfhandles, err := gorocksdb.OpenDbColumnFamilies(opts,
		getCheckPointPath(statename), columnfamilies, cfopts)
	if err != nil {
		return fmt.Errorf("[%s] Open checkpoint [%s] fail: %s", printGID, statename, err)
	}

	defer func() {
		for _, cf := range cfhandles {
			cf.Destroy()
		}
		chkp.Close()
	}()

	newtag := util.GenerateUUID()
	newdbPath := getDBPath("db_" + oc.dbTag + newtag)
	dbLogger.Infof("[%s] Create new state db on %s", printGID, newdbPath)

	//copy this checkpoint to active db path
	err = createCheckpoint(chkp, newdbPath)
	if err != nil {
		return err
	}

	//write the new db tag, if we fail here, we just have an discarded path
	//err = globalDataDB.put(globalDataDB.persistCF, oc.getDBKey(currentDBKey), []byte(newtag))
	err = odbPersistor.StoreByKeys(oc.getDBKey(currentDBKey), []byte(newtag))
	if err != nil {
		dbLogger.Errorf("[%s] Can't write globaldb: <%s>. Fail to create new state db at %s",
			printGID, err, newdbPath)
		return err
	}

	//now open the new state ...
	newdb := &ocDB{}
	err = newdb.open(newdbPath, opts, cfopts)
	if err != nil {
		return err
	}

	//prepare ok, now do the switch ...
	oc.Lock()
	olddb := oc.db
	oc.db = newdb
	oc.CleanManagedSnapshot()
	oc.Unlock()
	dbLogger.Infof("[%s] State switch to %s done", printGID, statename)

	//done, now release one refcount, and close db if needed
	//DATA RACE? No, it should be SAFE
	olddb.onReleased = func() {
		olddb.close()
		olddb.DropDB()
	}
	if hasReleased := olddb.release(); hasReleased {
		dbLogger.Infof("[%s] Delay release current db <%s>", printGID, olddb.dbName)
	} else {
		dbLogger.Infof("[%s] Release current db <%s>", printGID, olddb.dbName)
	}

	return nil
}
