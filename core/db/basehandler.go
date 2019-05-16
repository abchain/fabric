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

// base class of db handler and txdb handler
type baseHandler struct {
	*gorocksdb.DB
	cfMap   map[string]*gorocksdb.ColumnFamilyHandle
	OpenOpt baseOpt
}

type baseOpt struct {
	conf *viper.Viper
}

func (o baseOpt) Inited() bool { return o.conf != nil }

func (o baseOpt) Options() (opts *gorocksdb.Options) {

	//most common options
	opts = gorocksdb.NewDefaultOptions()
	opts.SetCreateIfMissing(true)
	opts.SetCreateIfMissingColumnFamilies(true)

	if !o.Inited() {
		return
	}

	vp := o.conf
	maxLogFileSize := vp.GetInt("maxLogFileSize")
	if maxLogFileSize > 0 {
		dbLogger.Infof("Setting rocksdb maxLogFileSize to %d", maxLogFileSize)
		opts.SetMaxLogFileSize(maxLogFileSize)
	}

	keepLogFileNum := vp.GetInt("keepLogFileNum")
	if keepLogFileNum > 0 {
		dbLogger.Infof("Setting rocksdb keepLogFileNum to %d", keepLogFileNum)
		opts.SetKeepLogFileNum(keepLogFileNum)
	}

	logLevelStr := vp.GetString("loglevel")
	logLevel, ok := rocksDBLogLevelMap[logLevelStr]

	if ok {
		dbLogger.Infof("Setting rocks db InfoLogLevel to %d", logLevel)
		opts.SetInfoLogLevel(logLevel)
	}

	return
}

func GetDBHandle() *OpenchainDB {
	return originalDB
}

func GetGlobalDBHandle() *GlobalDataDB {
	return globalDataDB
}

func DBOptions(vp *viper.Viper) baseOpt {
	if vp == nil {
		return DefaultOption()
	}
	return baseOpt{vp}
}

func DefaultOption() baseOpt {

	getDefaultOption.Do(func() {
		//test new configuration, then the legacy one
		vp := config.SubViper("node.db")
		if vp != nil {
			defaultOption = baseOpt{vp}
		} else {
			vp = config.SubViper("peer.db")
			if vp == nil {
				dbLogger.Warning("DB has no any custom options, complete default")
			}
			defaultOption = baseOpt{vp}
		}
	})
	return defaultOption
}

//if global db is not opened, it is also created
func startDBInner(odb *OpenchainDB, opts baseOpt, forcePath bool) error {

	globalDataDB.openDB.Do(func() {
		globalDataDB.OpenOpt = DefaultOption()
		if err := globalDataDB.open(getDBPath("txdb")); err != nil {
			globalDataDB.openError = fmt.Errorf("open global db fail: %s", err)
			return
		}
		if err := globalDataDB.setDBVersion(); err != nil {
			globalDataDB.openError = fmt.Errorf("handle global db version fail: %s", err)
			return
		}
	})
	if globalDataDB.openError != nil {
		return globalDataDB.openError
	}

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

	odb.db.OpenOpt = opts
	err = odb.db.open(orgDBPath, odb.buildOpenDBOptions())
	if err != nil {
		return fmt.Errorf("open db [%s] fail: %s", odb.dbTag, err)
	}

	return nil
}

func StartDB(tag string, vp *viper.Viper) (*OpenchainDB, error) {

	ret := new(OpenchainDB)
	ret.dbTag = tag
	if err := startDBInner(ret, DBOptions(vp), false); err != nil {
		return nil, err
	}

	return ret, nil
}

// Start the db, init the openchainDB instance and open the db. Note this method has no guarantee correct behavior concurrent invocation.
func Start() {

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

	odb.db.additionalClean = func() {
		odb.cleanDBOptions()
		dbLogger.Infof("current db [%s] has been completed stopped", odb.db.dbName)
		close(notifyChn)
	}
	odb.db.finalRelease()

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

}

func DropDB(path string) error {

	opt := gorocksdb.NewDefaultOptions()
	defer opt.Destroy()

	return gorocksdb.DestroyDb(path, opt)
}

func GetCurrentDBPath() []string {
	return []string{
		getDBPath("txdb"),
		originalDB.db.dbName,
	}
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

func (h *baseHandler) get(cf *gorocksdb.ColumnFamilyHandle, key []byte) ([]byte, error) {
	opt := gorocksdb.NewDefaultReadOptions()
	defer opt.Destroy()

	slice, err := h.GetCF(opt, cf, key)

	if err != nil {
		dbLogger.Errorf("[%s] Error while trying to retrieve key: %s", printGID, key)
		return nil, err
	}

	defer slice.Free()
	if slice.Data() == nil {
		// nil value is not error
		// dbLogger.Errorf("No such value for column family<%s.%s>, key<%s>[%x].",
		// 	baseHandler.dbName, cfName, string(key), key)
		return nil, nil
	}

	data := makeCopy(slice.Data())
	return data, nil
}

func (h *baseHandler) put(cf *gorocksdb.ColumnFamilyHandle, key []byte, value []byte) error {
	opt := gorocksdb.NewDefaultWriteOptions()
	defer opt.Destroy()

	err := h.PutCF(opt, cf, key, value)
	if err != nil {
		dbLogger.Errorf("[%s] Error while trying to write key: %s", printGID, key)
	}

	return err
}

func (h *baseHandler) delete(cf *gorocksdb.ColumnFamilyHandle, key []byte) error {
	opt := gorocksdb.NewDefaultWriteOptions()
	defer opt.Destroy()

	err := h.DeleteCF(opt, cf, key)
	if err != nil {
		dbLogger.Errorf("[%s] Error while trying to delete key: %s", printGID, key)
	}

	return err
}

func (h *baseHandler) GetCFByName(cfName string) *gorocksdb.ColumnFamilyHandle {
	return h.cfMap[cfName]
}

func (h *baseHandler) GetValue(cfName string, key []byte) ([]byte, error) {
	cf, ok := h.cfMap[cfName]
	if !ok {
		return nil, fmt.Errorf("No cf for [%s]", cfName)
	}

	return h.get(cf, key)
}

func (h *baseHandler) PutValue(cfName string, key []byte, value []byte) error {
	cf, ok := h.cfMap[cfName]
	if !ok {
		return fmt.Errorf("No cf for [%s]", cfName)
	}

	return h.put(cf, key, value)
}

func (h *baseHandler) NewWriteBatch() *gorocksdb.WriteBatch {
	return gorocksdb.NewWriteBatch()
}

func (h *baseHandler) BatchCommit(writeBatch *gorocksdb.WriteBatch) error {

	opt := gorocksdb.NewDefaultWriteOptions()
	defer opt.Destroy()

	return h.Write(opt, writeBatch)
}

func (h *baseHandler) DeleteKey(cfName string, key []byte) error {

	cf, ok := h.cfMap[cfName]
	if !ok {
		return fmt.Errorf("No cf for [%s]", cfName)
	}

	return h.delete(cf, key)
}

// Open underlying rocksdb
func (openchainDB *baseHandler) opendb(dbPath string, cf []string, cfOpts []*gorocksdb.Options) []*gorocksdb.ColumnFamilyHandle {

	dbLogger.Infof("Try opendb on <%s> with %d cfs", dbPath, len(cf))

	opts := openchainDB.OpenOpt.Options()
	defer opts.Destroy()

	cfNames := append(cf, "default")

	if cfOpts == nil {
		cfOpts = make([]*gorocksdb.Options, len(cfNames))
	} else {
		cfOpts = append(cfOpts, opts)
	}

	for i, op := range cfOpts {
		if op == nil {
			cfOpts[i] = opts
		}
	}

	db, cfHandlers, err := gorocksdb.OpenDbColumnFamilies(opts, dbPath, cfNames, cfOpts)

	if err != nil {
		dbLogger.Error("Error opening DB:", err)
		return nil
	}

	dbLogger.Infof("gorocksdb.OpenDbColumnFamilies <%s>, len cfHandlers<%d>", dbPath, len(cfHandlers))

	openchainDB.DB = db
	//destry default CF, we never use it
	cfHandlers[len(cf)].Destroy()
	return cfHandlers[:len(cf)]
}

func (h *baseHandler) close() {

	if h.cfMap != nil {
		for _, cf := range h.cfMap {
			cf.Destroy()
		}
	}

	if h.DB != nil {
		h.Close()
	}

}

func (openchainDB *baseHandler) getSnapshotIterator(snapshot *gorocksdb.Snapshot,
	cfHandler *gorocksdb.ColumnFamilyHandle) *gorocksdb.Iterator {
	opt := gorocksdb.NewDefaultReadOptions()
	defer opt.Destroy()
	opt.SetSnapshot(snapshot)
	iter := openchainDB.NewIteratorCF(opt, cfHandler)
	return iter
}

func (openchainDB *baseHandler) getFromSnapshot(snapshot *gorocksdb.Snapshot,
	cfHandler *gorocksdb.ColumnFamilyHandle, key []byte) ([]byte, error) {
	opt := gorocksdb.NewDefaultReadOptions()
	defer opt.Destroy()
	opt.SetSnapshot(snapshot)
	slice, err := openchainDB.GetCF(opt, cfHandler, key)
	if err != nil {
		dbLogger.Errorf("Error while trying to retrieve key: %s", key)
		return nil, err
	}
	defer slice.Free()

	if slice.Data() == nil {
		return nil, nil
	}

	data := makeCopy(slice.Data())
	return data, nil
}

var dbPathSetting = ""

func InitDBPath(path string) {
	dbLogger.Infof("DBPath has been set as [%s]", path)
	dbPathSetting = path
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

func makeCopy(src []byte) []byte {
	dest := make([]byte, len(src))
	copy(dest, src)
	return dest
}
