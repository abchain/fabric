package db

import (
	"github.com/spf13/viper"
	"github.com/tecbot/gorocksdb"
)

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

// basically it was just a fixed-length prefix extractor
type indexCFPrefixGen struct{}

func (*indexCFPrefixGen) Transform(src []byte) []byte {
	return src[:1]
}
func (*indexCFPrefixGen) InDomain(src []byte) bool {
	//skip "lastIndexedBlockKey"
	return len(src) > 0 && int(src[0]) > 0
}
func (s *indexCFPrefixGen) Name() string {
	return "indexPrefixGen\x00"
}

// method has been deprecated
func (*indexCFPrefixGen) InRange(src []byte) bool {
	return false
}

//define options for db
func (openchainDB *OpenchainDB) buildOpenDBOptions(base baseOpt) (*gorocksdb.Options,
	[]*gorocksdb.Options) {

	if openchainDB.baseOpt == nil {
		openchainDB.baseOpt = base.Options()
	}

	//cache opts for indexesCF
	if openchainDB.indexesCFOpt == nil {
		iopt := base.Options()
		if globalDataDB.GetDBVersion() > 1 {
			/*
				It seems gorocksdb has no way to completely release a go object
				used in opitons, so the leaking of memory can not be avoided
				when a Stop() is called
				we have to reused the option in most case to mitigate the leaking
			*/
			iopt.SetPrefixExtractor(&indexCFPrefixGen{})
			openchainDB.indexesCFOpt = iopt
		}
	}

	return openchainDB.openDBOptions()
}

func (openchainDB *OpenchainDB) openDBOptions() (*gorocksdb.Options,
	[]*gorocksdb.Options) {

	//sanity check
	if openchainDB.baseOpt == nil {
		panic("Called before buildOpenDBOptions is invoked")
	}

	var dbOpts = make([]*gorocksdb.Options, len(columnfamilies))
	for i, cf := range columnfamilies {
		switch cf {
		case IndexesCF:
			dbOpts[i] = openchainDB.indexesCFOpt
		default:
			dbOpts[i] = openchainDB.baseOpt
		}
	}

	return openchainDB.baseOpt, dbOpts

}

func (openchainDB *OpenchainDB) cleanDBOptions() {

	if openchainDB.indexesCFOpt != nil {
		/* problem and hacking:
		   rocksdb crashs if the option with prefix extractor is destroy
		   the possible cause may be:
			   https://github.com/facebook/rocksdb/issues/1095
		   whatever we have no way to resolve it but just reset the handle to nil
		   before destory the option
		   (it was weired that cmo (merge operator) is not release in gorocksdb)
		*/
		openchainDB.indexesCFOpt.SetPrefixExtractor(gorocksdb.NewNativeSliceTransform(nil))
		openchainDB.indexesCFOpt.Destroy()
		openchainDB.indexesCFOpt = nil
	}

	if openchainDB.baseOpt != nil {
		openchainDB.baseOpt.Destroy()
		openchainDB.baseOpt = nil
	}

}

//open of txdb is ensured to be Once
func (txdb *GlobalDataDB) buildOpenDBOptions(base baseOpt) (*gorocksdb.Options,
	[]*gorocksdb.Options) {

	sOpt := base.Options()
	sOpt.SetMergeOperator(&globalstatusMO{})

	txOpts := make([]*gorocksdb.Options, len(txDbColumnfamilies))
	for i, cf := range txDbColumnfamilies {
		switch cf {
		case GlobalCF:
			if txdb.globalStateOpt == nil {
				sOpt := base.Options()
				sOpt.SetMergeOperator(&globalstatusMO{})
				txdb.globalStateOpt = sOpt
			}
			txOpts[i] = txdb.globalStateOpt
		default:
			txOpts[i] = sOpt
		}
	}

	return sOpt, txOpts
}

func (txdb *GlobalDataDB) cleanDBOptions() {

	if txdb.globalStateOpt != nil {
		txdb.globalStateOpt.Destroy()
		txdb.globalStateOpt = nil
	}

}
