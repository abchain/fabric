package db

import (
	"fmt"
	"github.com/tecbot/gorocksdb"
)

// base class of db handler and txdb handler
type baseHandler struct {
	*gorocksdb.DB
	cfMap map[string]*gorocksdb.ColumnFamilyHandle
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

// // Open underlying rocksdb
// func (openchainDB *baseHandler) opendb(dbPath string, cf []string, cfOpts []*gorocksdb.Options) []*gorocksdb.ColumnFamilyHandle {

// 	dbLogger.Infof("Try opendb on <%s> with %d cfs", dbPath, len(cf))

// 	opts := openchainDB.OpenOpt.Options()
// 	defer opts.Destroy()

// 	cfNames := append(cf, "default")

// 	if cfOpts == nil {
// 		cfOpts = make([]*gorocksdb.Options, len(cfNames))
// 	} else {
// 		cfOpts = append(cfOpts, opts)
// 	}

// 	for i, op := range cfOpts {
// 		if op == nil {
// 			cfOpts[i] = opts
// 		}
// 	}

// 	db, cfHandlers, err := gorocksdb.OpenDbColumnFamilies(opts, dbPath, cfNames, cfOpts)

// 	if err != nil {
// 		dbLogger.Error("Error opening DB:", err)
// 		return nil
// 	}

// 	dbLogger.Infof("gorocksdb.OpenDbColumnFamilies <%s>, len cfHandlers<%d>", dbPath, len(cfHandlers))

// 	openchainDB.DB = db
// 	//destry default CF, we never use it
// 	cfHandlers[len(cf)].Destroy()
// 	return cfHandlers[:len(cf)]
// }

func (h *baseHandler) close() {

	if h.cfMap != nil {
		for _, cf := range h.cfMap {
			cf.Destroy()
		}
		dbLogger.Infof("Release %d cf objects", len(h.cfMap))
	}

	if h.DB != nil {
		dbLogger.Infof("Close database object <%s>", h.DB.Name())
		h.DB.Close()
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

func makeCopy(src []byte) []byte {
	dest := make([]byte, len(src))
	copy(dest, src)
	return dest
}
