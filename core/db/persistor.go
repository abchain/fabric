package db

import (
	"github.com/tecbot/gorocksdb"
	"strings"
)

//some per-def prefix
const RawLegacyPBFTPrefix = "consensus.chkpt."

var persistorRegChecker = map[string]int{}
var persistorReg []string

type PersistorKey int

func PersistKeyRegister(k string) PersistorKey {

	if _, ok := persistorRegChecker[k]; ok {
		panic(k + " is duplicated persist key, select another one")
	}

	ret := len(persistorReg)
	persistorRegChecker[k] = ret
	persistorReg = append(persistorReg, k)
	return PersistorKey(ret)
}

func GetPersistKey(k string) PersistorKey {
	if v, ok := persistorRegChecker[k]; !ok {
		panic(k + " is not registried")
	} else {
		return PersistorKey(v)
	}
}

func ListPersistorKeys() []string { return persistorReg }

// Persistor enables module to persist and restore data to the database
// type Persistor interface {
// 	StoreByKeys(keys []string, value []byte) error
// 	Store(key string, value []byte) error
// 	LoadByKeys(keys []string) ([]byte, error)
// 	Load(key string) ([]byte, error)
// }

type persistor struct {
	prefix          string
	iteratingPrefix []byte
	iterator        *gorocksdb.Iterator
}

func NewPersistor(index PersistorKey) *persistor {

	return &persistor{prefix: persistorReg[index]}
}

func (p *persistor) buildStoreKey(keys []string) []byte {
	return []byte(strings.Join(append([]string{p.prefix}, keys...), "."))
}

func (p *persistor) EndIteration() {
	p.iterator.Close()
	p.iterator = nil
}

func (p *persistor) StartIteration(keys []string) {
	p.iteratingPrefix = p.buildStoreKey(keys)
	p.iterator = GetGlobalDBHandle().GetIterator(PersistCF)
	p.iterator.Seek(p.iteratingPrefix)
}

//API for iteration
func (p *persistor) Valid() bool {
	if p.iterator == nil {
		return false
	}
	return p.iterator.ValidForPrefix(p.iteratingPrefix)
}

func (p *persistor) Next() { p.iterator.Next() }

func (p *persistor) PeekKey() []byte {
	//Value/Key() in iterator need not to be Free() but its Data()
	//must be copied
	//ret = append(ret, it.Value().Data()) --- THIS IS WRONG
	//ret = append(ret, makeCopy(it.Value().Data()))
	return p.iterator.Key().Data()
}

func (p *persistor) Key() []byte {
	return makeCopy(p.PeekKey())
}

func (p *persistor) PeekValue() []byte {
	return p.iterator.Value().Data()
}

func (p *persistor) Value() []byte {
	return makeCopy(p.PeekValue())
}

// Store enables a peer to persist the given key series,value pair to the database
func (p *persistor) StoreByKeys(keys []string, value []byte) error {
	dbhandler := GetGlobalDBHandle()

	//dbg.Infof("add db.PersistCF: <%s> --> <%x>", key, value)
	return dbhandler.PutValue(PersistCF, p.buildStoreKey(keys), value)
}

func (p *persistor) StoreOnBatch(wb *gorocksdb.WriteBatch, keys []string, value []byte) {
	dbhandler := GetGlobalDBHandle()
	wb.PutCF(dbhandler.persistCF, p.buildStoreKey(keys), value)
}

// Load enables a peer to read the value that corresponds to the given database key
func (p *persistor) LoadByKeys(keys []string) ([]byte, error) {
	dbhandler := GetGlobalDBHandle()
	return dbhandler.GetValue(PersistCF, p.buildStoreKey(keys))
}

func (p *persistor) Store(key string, value []byte) error {
	return p.StoreByKeys([]string{key}, value)
}

func (p *persistor) Load(key string) ([]byte, error) {
	return p.LoadByKeys([]string{key})
}
