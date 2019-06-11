package db

import (
	"bytes"
	"github.com/tecbot/gorocksdb"
	"io/ioutil"
	"path/filepath"
	"testing"
)

func TestDBCheckPoint(t *testing.T) {

	orgdb, orgdir := newTestDB("orgdir", nil)
	t.Log("orgdir:", orgdir)

	// insert keys
	givenKeys := [][]byte{[]byte("key1"), []byte("key2"), []byte("key3")}
	givenVal := []byte("valaa")
	wo := gorocksdb.NewDefaultWriteOptions()
	for _, k := range givenKeys {
		orgdb.Put(wo, k, givenVal)
	}

	suffix := "checkpoint"
	newdir, err := ioutil.TempDir("", "gorocksdb-"+suffix)
	newdir = filepath.Join(newdir, "1")
	t.Log("checkpoint dir:", newdir)

	var newdb *gorocksdb.DB
	var checkpoint *gorocksdb.Checkpoint

	checkpoint, _ = orgdb.NewCheckpoint()
	defer checkpoint.Destroy()

	err = checkpoint.CreateCheckpoint(newdir, 0)
	if err != nil {
		t.Fatal("create fail:", err)
	}
	orgdb.Close()

	opts := gorocksdb.NewDefaultOptions()
	newdb, err = gorocksdb.OpenDb(opts, newdir)
	defer newdb.Close()
	if err != nil {
		t.Fatal("open fail:", err)
	}

	// test keys
	var value *gorocksdb.Slice
	ro := gorocksdb.NewDefaultReadOptions()
	for _, k := range givenKeys {
		value, err = newdb.Get(ro, k)
		defer value.Free()
		if bytes.Compare(value.Data(), givenVal) != 0 {
			t.Fatalf("cp: <%s><%x>, givenVal:<%x>\n", k, value.Data(), givenVal)
		}
	}

}

func newTestDB(name string, applyOpts func(opts *gorocksdb.Options)) (*gorocksdb.DB, string) {
	dir, _ := ioutil.TempDir("", "gorocksdb-"+name)
	opts := gorocksdb.NewDefaultOptions()
	rateLimiter := gorocksdb.NewRateLimiter(1024, 100*1000, 10)
	opts.SetRateLimiter(rateLimiter)
	opts.SetCreateIfMissing(true)
	db, _ := gorocksdb.OpenDb(opts, dir)
	return db, dir
}

func TestCreateCheckPoint(t *testing.T) {

	Start()
	defer deleteTestDBPath()
	defer Stop()

	openchainDB := GetDBHandle()

	openchainDB.PutValue(StateCF, []byte("key1"), []byte("value1"))
	openchainDB.PutValue(StateCF, []byte("key2"), []byte("value2"))
	openchainDB.PutValue(StateDeltaCF, []byte("key2"), []byte("value2"))

	err := openchainDB.CheckpointCurrent([]byte("chkpoint1"))
	if err != nil {
		t.Fatal("checkpoint fail", err)
	}

	openchainDB.PutValue(StateCF, []byte("key3"), []byte("value3"))
	openchainDB.PutValue(StateDeltaCF, []byte("key4"), []byte("value4"))

	err = openchainDB.CheckpointCurrent([]byte("chkpoint2"))

	if err != nil {
		t.Fatal("checkpoint 2 fail", err)
	}
}

func TestReStart(t *testing.T) {

	Start()
	defer deleteTestDBPath()

	openchainDB := GetDBHandle()

	openchainDB.PutValue(StateCF, []byte("key1"), []byte("value1"))
	openchainDB.PutValue(StateCF, []byte("key2"), []byte("value2"))
	openchainDB.PutValue(StateDeltaCF, []byte("key2"), []byte("value2"))

	chkpoint1 := []byte{0, 0, 0, 1}

	err := openchainDB.CheckpointCurrent(chkpoint1)
	if err != nil {
		t.Fatal("checkpoint fail", err)
	}

	openchainDB.PutValue(StateCF, []byte("key3"), []byte("value3"))
	openchainDB.PutValue(StateDeltaCF, []byte("key4"), []byte("value4"))

	err = openchainDB.StateSwitch(chkpoint1)
	if err != nil {
		t.Fatal("switch to chkp1 fail", err)
	}

	openchainDB.PutValue(StateCF, []byte("key5"), []byte("value5"))
	Stop()

	Start()
	defer Stop()
	openchainDB = GetDBHandle()

	v, _ := openchainDB.GetValue(StateCF, []byte("key3"))
	if v != nil {
		t.Fatal("get ghost key3", string(v))
	}

	v, _ = openchainDB.GetValue(StateCF, []byte("key5"))
	assertByteEqual(t, v, "value5")

}

func TestSwitchCheckPoint(t *testing.T) {

	Start()
	defer deleteTestDBPath()
	defer Stop()

	openchainDB := GetDBHandle()

	openchainDB.PutValue(StateCF, []byte("key1"), []byte("value1"))
	openchainDB.PutValue(StateCF, []byte("key2"), []byte("value2"))
	openchainDB.PutValue(StateDeltaCF, []byte("key2"), []byte("value2"))

	chkpoint1 := []byte{0, 0, 0, 1}
	chkpoint2 := []byte{0, 0, 0, 2}
	chkpoint3 := []byte{0, 0, 0, 3}

	err := openchainDB.CheckpointCurrent(chkpoint1)
	if err != nil {
		t.Fatal("checkpoint 1 fail", err)
	}

	openchainDB.PutValue(StateCF, []byte("key3"), []byte("value3"))
	openchainDB.PutValue(StateDeltaCF, []byte("key4"), []byte("value4"))

	err = openchainDB.CheckpointCurrent(chkpoint2)

	if err != nil {
		t.Fatal("checkpoint 2 fail", err)
	}

	err = openchainDB.StateSwitch(chkpoint1)
	if err != nil {
		t.Fatal("switch to chkp1 fail", err)
	}

	v, _ := openchainDB.GetValue(StateCF, []byte("key3"))
	if v != nil {
		t.Fatal("get ghost key3", string(v))
	}

	v, _ = openchainDB.GetValue(StateCF, []byte("key1"))
	assertByteEqual(t, v, "value1")
	v, _ = openchainDB.GetValue(StateDeltaCF, []byte("key2"))
	assertByteEqual(t, v, "value2")

	openchainDB.PutValue(StateCF, []byte("key5"), []byte("value5"))

	err = openchainDB.CheckpointCurrent(chkpoint3)
	if err != nil {
		t.Fatal("checkpoint 3 fail", err)
	}

	err = openchainDB.StateSwitch(chkpoint2)
	if err != nil {
		t.Fatal("switch to chkp1 fail", err)
	}

	v, _ = openchainDB.GetValue(StateCF, []byte("key1"))
	assertByteEqual(t, v, "value1")
	v, _ = openchainDB.GetValue(StateCF, []byte("key3"))
	assertByteEqual(t, v, "value3")
	v, _ = openchainDB.GetValue(StateDeltaCF, []byte("key4"))
	assertByteEqual(t, v, "value4")

	//snapshot
	sn := openchainDB.GetSnapshot()

	err = openchainDB.StateSwitch(chkpoint3)
	if err != nil {
		t.Fatal("switch to chkp1 fail", err)
	}

	v, _ = openchainDB.GetValue(StateCF, []byte("key3"))
	if v != nil {
		t.Fatal("get ghost key3", string(v))
	}

	v, _ = openchainDB.GetValue(StateCF, []byte("key5"))
	assertByteEqual(t, v, "value5")

	v, _ = sn.h.getFromSnapshot(sn.snapshot, sn.StateCF, []byte("key3"))
	assertByteEqual(t, v, "value3")

	sn.Release()

	allcks := GetGlobalDBHandle().ListCheckpoints()
	chkret := map[int]bool{1: false, 2: false, 3: false}
	for _, ckn := range allcks {
		if len(ckn) != 4 {
			t.Fatalf("Invalid chkpoint name: %x", ckn)
		}
		v, ok := chkret[int(ckn[3])]
		if v || !ok {
			t.Fatalf("Invalid ret: %v", chkret)
		}
		chkret[int(ckn[3])] = true
	}

	for _, v := range chkret {
		if !v {
			t.Fatalf("Invalid ret: %v", chkret)
		}
	}
}
