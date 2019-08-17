package db

import (
	"fmt"
	"github.com/abchain/fabric/core/config"
	"github.com/abchain/fabric/core/util"
	flog "github.com/abchain/fabric/flogging"
	"github.com/op/go-logging"
	"github.com/spf13/viper"
	"github.com/tecbot/gorocksdb"
	"path/filepath"
	"time"
)

var dbLogger = logging.MustGetLogger("db")
var printGID = flog.GoRDef
var rocksDBLogLevelMap = map[string]gorocksdb.InfoLogLevel{
	"debug": gorocksdb.DebugInfoLogLevel,
	"info":  gorocksdb.InfoInfoLogLevel,
	"warn":  gorocksdb.WarnInfoLogLevel,
	"error": gorocksdb.ErrorInfoLogLevel,
	"fatal": gorocksdb.FatalInfoLogLevel}

const DBVersion = 1
const txDBVersion = 3

// cf in txdb
const TxCF = "txCF"
const GlobalCF = "globalCF"
const ConsensusCF = "consensusCF"
const PersistCF = "persistCF"

// cf in db
const BlockchainCF = "blockchainCF"
const StateCF = "stateCF"
const StateDeltaCF = "stateDeltaCF"
const IndexesCF = "indexesCF"

const odbsettings = "openchainDBsetting"

var odbPersistor = NewPersistor(PersistKeyRegister(odbsettings))

const currentDBKey = "currentDB"
const currentVersionKey = "currentVer"

const currentGlobalVersionKey = "currentVerGlobal"

//ver global is a simple key, we reg it for duplicating detecting but not use it
var _ = PersistKeyRegister(currentGlobalVersionKey)

func GetDBHandle() *OpenchainDB {
	return originalDB
}

func GetGlobalDBHandle() *GlobalDataDB {
	return globalDataDB
}

func DefaultOption() baseOpt {

	//test new configuration, then the legacy one
	vp := config.SubViper("node.db")
	if vp != nil {
		return baseOpt{vp}
	} else {
		vp = config.SubViper("peer.db")
		if vp == nil {
			dbLogger.Warning("DB has no any custom options, complete default")
		}
		return baseOpt{vp}
	}

}

//if global db is not opened, it is also created
func startDBInner(odb *OpenchainDB, opts baseOpt, forcePath bool) error {

	//k, err := globalDataDB.get(globalDataDB.persistCF, odb.getDBKey(currentDBKey))
	k, err := odbPersistor.LoadByKeys(odb.getDBKey(currentDBKey))
	if err != nil {
		return fmt.Errorf("get db [%s]'s path fail: %s", odb.dbTag, err)
	}

	var orgDBPath string
	if k == nil {
		if forcePath {
			orgDBPath = getDBPath("db")
		} else {
			newtag := util.GenerateUUID()
			err = odbPersistor.StoreByKeys(odb.getDBKey(currentDBKey), []byte(newtag))
			//err = globalDataDB.put(globalDataDB.persistCF, odb.getDBKey(currentDBKey), []byte(newtag))
			if err != nil {
				return fmt.Errorf("Save storage tag for db <%s> fail: %s", odb.dbTag, newtag)
			}
			orgDBPath = getDBPath("db_" + odb.dbTag + newtag)
		}
	} else {
		orgDBPath = getDBPath("db_" + odb.dbTag + string(k))
	}

	//check if db is just created
	roOpt := gorocksdb.NewDefaultOptions()
	defer roOpt.Destroy()
	roDB, err := gorocksdb.OpenDbForReadOnly(roOpt, orgDBPath, false)
	if err != nil {
		err = odb.UpdateDBVersion(DBVersion)
		if err != nil {
			return fmt.Errorf("set db [%s]'s version fail: %s", odb.dbTag, err)
		}
	} else {
		roDB.Close()
		dbLogger.Infof("DB [%s] at %s is created before", odb.dbTag, orgDBPath)
	}

	odb.managedSnapshots.m = make(map[string]*DBSnapshot)
	if odb.db == nil {
		odb.db = new(ocDB)
	}

	dbopt, cfopt := odb.buildOpenDBOptions(opts)
	err = odb.db.open(orgDBPath, dbopt, cfopt)
	if err != nil {
		return fmt.Errorf("open db [%s] fail: %s", odb.dbTag, err)
	}

	//notice: we only create cf/db on first opending
	odb.baseOpt.SetCreateIfMissing(false)
	odb.baseOpt.SetCreateIfMissingColumnFamilies(false)
	odb.indexesCFOpt.SetCreateIfMissingColumnFamilies(false)

	return nil
}

func startGlobalDB() {

	globalDataDB.openDB.Do(func() {
		if err := globalDataDB.open(getDBPath("txdb"), DefaultOption()); err != nil {
			globalDataDB.openError = fmt.Errorf("open global db fail: %s", err)
			return
		}
		if err := globalDataDB.setDBVersion(); err != nil {
			globalDataDB.openError = fmt.Errorf("handle global db version fail: %s", err)
			return
		}
	})
}

func StartDB(tag string, vp *viper.Viper) (*OpenchainDB, error) {

	startGlobalDB()
	ret := &OpenchainDB{dbTag: tag}
	if err := startDBInner(ret, baseOpt{vp}, false); err != nil {
		return nil, err
	}

	return ret, nil
}

// Start the db, init the openchainDB instance and open the db. Note this method has no guarantee correct behavior concurrent invocation.
func Start() {

	startGlobalDB()
	if err := startDBInner(originalDB, DefaultOption(), true); err != nil {
		panic(err)
	}
}

// Stop the db. Note this method has no guarantee correct behavior concurrent invocation.
func Stop() {

	//we not care about the concurrent problem because it is never touched except for legacy usage
	StopDB(originalDB)
	originalDB = &OpenchainDB{db: &ocDB{}}

	openglobalDBLock.Lock()
	defer openglobalDBLock.Unlock()
	globalDataDB.close()
	globalDataDB.cleanDBOptions()
	globalDataDB = new(GlobalDataDB)
}

//NOTICE: stop the db do not ensure a completely "clean" of all resource, memory leak
//is highly possible and we should avoid to use it frequently
func StopDB(odb *OpenchainDB) {

	// notice you don't need to lock odb for you don't change the underlying db field
	// odb.Lock()
	// defer odb.Unlock()
	odb.CleanManagedSnapshot()

	notifyChn := make(chan interface{})

	odb.db.onReleased = func() {
		dbLogger.Infof("current db [%s] has been completed stopped", odb.db.dbName)
		close(notifyChn)
	}
	odb.db.release()

	//we wait here, until all resource is ENSURED to be release
	//(we put a default timeout here, for warnning and debug purpose)
	select {
	case _, notClosed := <-notifyChn:
		if notClosed {
			panic("wrong code, channel get msg rather than closed notify")
		}
	case <-time.NewTimer(5 * time.Second).C:
		panic("can not stop db clean, check your code")
	}

	odb.db.close()
	odb.cleanDBOptions()
}

func DropDB(path string) error {

	opt := gorocksdb.NewDefaultOptions()
	defer opt.Destroy()

	return gorocksdb.DestroyDb(path, opt)
}

//generate paths for backuping, only understanded by this package
func GetBackupPath(tag string) []string {

	return []string{
		getDBPath("txdb_" + tag + "_bak"),
		getDBPath("db_" + tag + "_bak"),
	}
}

func Backup(odb *OpenchainDB) (tag string, err error) {

	tag = util.GenerateUUID()
	paths := GetBackupPath(tag)

	if len(paths) < 2 {
		panic("Not match backup paths with backup expected")
	}

	err = createCheckpoint(globalDataDB.DB, paths[0])
	if err != nil {
		return
	}

	err = createCheckpoint(odb.db.DB, paths[1])
	if err != nil {
		return
	}

	return
}

var dbPathSetting = ""

func InitDBPath(path string) {
	dbLogger.Infof("DBPath has been set as [%s]", path)
	dbPathSetting = path
}

func GetCurrentDBPath() []string {
	return []string{
		getDBPath("txdb"),
		originalDB.db.dbName,
	}
}

func getDBPath(dbname ...string) string {
	var dbPath string
	if dbPathSetting == "" {
		dbPath = config.GlobalFileSystemPath()
		//though null string is OK, we still avoid this problem
		if dbPath == "" {
			panic("DB path not specified in configuration file. Please check that property 'peer.fileSystemPath' is set")
		}
	} else {
		dbPath = util.CanonicalizePath(dbPathSetting)
	}

	if util.MkdirIfNotExist(dbPath) {
		dbLogger.Infof("dbpath %s not exist, we have created it", dbPath)
	}

	return filepath.Join(append([]string{dbPath}, dbname...)...)
}
