package db

import (
	"bytes"
	"github.com/abchain/fabric/core/util"
	pb "github.com/abchain/fabric/protos"
	"github.com/tecbot/gorocksdb"
	"math/rand"
	"testing"
	"time"
)

var populatedTxCnt = 32

func compareTx(a *pb.Transaction, b *pb.Transaction) bool {
	if a == nil || b == nil {
		return false
	}
	return a.Txid == b.Txid && bytes.Compare(a.Payload, b.Payload) == 0
}

func TestTxDBRW(t *testing.T) {

	Start()
	defer deleteTestDBPath()
	defer Stop()

	//populate txs
	txs := make([]*pb.Transaction, populatedTxCnt)
	txids := make([]string, populatedTxCnt)
	for i, _ := range txs {
		txs[i] = new(pb.Transaction)
		tx := txs[i]
		tx.Txid = util.GenerateUUID()
		tx.Payload = util.GenerateBytesUUID()
		txids[i] = tx.Txid
	}

	txmap := make(map[string]*pb.Transaction)
	for _, tx := range txs {
		txmap[tx.Txid] = tx
	}

	err := globalDataDB.PutTransactions(txs)
	if err != nil {
		t.Fatal("put tx fail:", err)
	}

	//shulffle txids (our go version have not shuffle yet)
	r := rand.New(rand.NewSource(time.Now().Unix()))
	for i, tx := range txids[:populatedTxCnt-1] {
		mv := populatedTxCnt - r.Intn(populatedTxCnt-i) - 1
		txids[i] = txids[mv]
		txids[mv] = tx
	}

	tx, err := globalDataDB.GetTransaction(txids[0])
	if err != nil || tx == nil {
		t.Fatal("get singele tx fail:", err)
	}

	if !compareTx(tx, txmap[txids[0]]) {
		t.Fatal("Wrong tx:", tx, "expected:", txmap[txids[0]])
	}

	tx, err = globalDataDB.GetTransaction("unexistTxid")
	if err != nil || tx != nil {
		t.Fatal("get unexisted singele tx fail:", err)
	}

	gtxs := globalDataDB.GetTransactions(txids[:10])
	if len(gtxs) < 10 {
		t.Fatal("get mutiple tx fail:", err)
	}

	for i, gtx := range gtxs {
		if !compareTx(gtx, txmap[txids[i]]) {
			t.Fatal("Wrong tx:", gtx, "expected:", txmap[txids[i]])
		}
	}

	gtxs = globalDataDB.GetTransactions(txids)
	if len(gtxs) < len(txids) {
		t.Fatal("get all txs fail:", err)
	}

	for i, gtx := range gtxs {
		if !compareTx(gtx, txmap[txids[i]]) {
			t.Fatal("Wrong tx:", gtx, "expected:", txmap[txids[i]])
		}
	}

	unexistTxid := []string{"unexisted1", "unexisted2", "unexisted3"}

	gtxs = globalDataDB.GetTransactions(unexistTxid)
	if len(gtxs) != 0 {
		t.Fatal("get unexisted txs fail:", err)
	}

	gtxs = globalDataDB.GetTransactions(append(txids, unexistTxid...))
	if len(gtxs) != len(txids) {
		t.Fatal("get all txs fail:", err)
	}

	for i, gtx := range gtxs {
		if !compareTx(gtx, txmap[txids[i]]) {
			t.Fatal("Wrong tx:", gtx, "expected:", txmap[txids[i]])
		}
	}
}

func dumpCF(t *testing.T) {
	t.Log("------Current CF-----")
	itr := globalDataDB.GetIterator(GlobalCF)
	if itr == nil {
		t.Fatal("No iterator!")
	}
	defer itr.Close()

	itr.SeekToFirst()
	for ; itr.Valid(); itr.Next() {

		k := itr.Key()
		v := itr.Value()
		defer k.Free()
		defer v.Free()

		gs, err := pb.UnmarshallGS(v.Data())

		if err != nil {
			t.Fatal("Wrong data in gs")
		}

		t.Log(string(k.Data()), gs)
	}

	t.Log("---------------------")
}

func testPopulatePath(t *testing.T, parent string, path []string) {

	for _, s := range path {

		err := globalDataDB.AddGlobalState([]byte(parent), []byte(s))
		if err != nil {
			t.Fatal("Add state fail", err)
		}

		parent = s
	}
}

func assertTrue(t *testing.T, v bool) {
	if v {
		return
	}

	t.Fatalf("Get unexpected FALSE result")
}

func assertIntEqual(t *testing.T, v int, exp int) {
	if v == exp {
		return
	}

	t.Fatalf("Value is not equal: get %d, expected %d", v, exp)
}

func assertUint64Equal(t *testing.T, v uint64, exp uint64) {
	if v == exp {
		return
	}

	t.Fatalf("Value is not equal: get %d, expected %d", v, exp)
}

func assertByteEqual(t *testing.T, v []byte, expected string) {
	if expected == "" && v == nil {
		return
	} else if string(v) == expected {
		return
	}

	t.Fatalf("Value is not equal: get %s, expected %s", string(v), expected)
}

func TestMergeOpEncoding(t *testing.T) {
	for i := 0; i < 1000; i++ {
		t1, t2 := util.GenerateBytesUUID(), util.GenerateBytesUUID()
		rc := rand.Uint64()
		enc := encodePathUpdate(rc, t1, t2)

		c, h, e := decodeMergeValue(enc[1:])
		if e != nil {
			t.Fatalf("first fail: %s;%X", e, enc)
		}

		td1, td2, e := decodePathInfo(h)
		if e != nil {
			t.Fatalf("second fail: %s;%X", e, h)
		}

		if bytes.Compare(td1, t1) != 0 {
			t.Fatalf("t1 fail: %X vs %X", td1, t1)
		}
		if bytes.Compare(td2, t2) != 0 {
			t.Fatalf("t2 fail: %X vs %X", td2, t2)
		}
		if c != rc {
			t.Fatalf("count fail: %d vs %d", c, rc)
		}
	}
}

func testTerminalKeyGen() []byte {
	return []byte(util.GenerateUUID())[:8]
}

func adjustTKGen(f func() []byte) (ret func() []byte) {
	terminalKeyGen, ret = f, terminalKeyGen
	return
}

func adjustIDStart(s uint64) (old uint64) {
	old, detachedIdStart = detachedIdStart, s
	return
}

func dumpUpdating(t *testing.T, tip string, txdb *GlobalDataDB) {
	t.Logf("----------- dumping updating: %s ------------", tip)
	for k, v := range txdb.updating {
		t.Logf("[%s]:branchid %d; before {%v}, after {%v}, activing %d", k, v.branchedId, v.before, v.after, len(v.activingPath))
	}
	t.Logf(" ---------- dumping updating over -----------")
}

func pollstate(t *testing.T, gs_old *pb.GlobalState, txdb *GlobalDataDB) *pb.GlobalState {
	gs := new(pb.GlobalState)
	*gs = *gs_old
	var err error
	for ud, ok := txdb.updating[stateToEdge(gs).String()]; ok; ud, ok = txdb.updating[stateToEdge(gs).String()] {
		//		t.Logf("found path [%#v][%#v]@%d", ud.before, ud.after, ud.branchedId)
		gs, err = ud.updateGlobalState(gs)
		//		t.Logf("poll to [%v]", gs)
		if err != nil {
			t.Fatalf("state was ruined (has no valid update: %s)", err)
		}
	}
	return gs
}

func TestTxDBGlobalState_branchtasks(t *testing.T) {

	tdb := new(GlobalDataDB)
	tdb.globalHashLimit = 6
	tdb.updating = make(map[string]*pathUnderUpdating)
	tdb.activing = make(map[string][]*pathUpdateTask)

	testgs := new(pb.GlobalState)
	testgs.Count = 20
	testgs.NextBranchNodeStateHash = []byte("*braEnd")
	testgs.LastBranchNodeStateHash = []byte("*bBegin")

	ud1 := tdb.newPathUpdatedByBranchNode([]byte("test1"), testgs)
	if err := tdb.registryTasks(ud1); err != nil {
		t.Fatalf("Ud1 fail: %s [%v]", err, ud1)
	}
	dumpUpdating(t, "task 1", tdb)

	if len(tdb.activing) != 2 && len(tdb.updating) != 1 {
		t.Fatalf("reg fail: [%v]", tdb.activing)
	}

	testgs.Count = 15
	testgsUd := pollstate(t, testgs, tdb)

	if testgsUd.Count != 16 {
		t.Fatalf("update error on count: [%v]", testgsUd)
	}

	assertByteEqual(t, testgsUd.LastBranchNodeStateHash, "*bBegin")
	assertByteEqual(t, testgsUd.NextBranchNodeStateHash, "test1")

	testgs.Count = 23
	testgsUd = pollstate(t, testgs, tdb)

	if testgsUd.Count != 24 {
		t.Fatalf("update error on count: [%v]", testgsUd)
	}

	assertByteEqual(t, testgsUd.LastBranchNodeStateHash, "test1")
	assertByteEqual(t, testgsUd.NextBranchNodeStateHash, "*braEnd")

	ud2 := tdb.newPathUpdatedByBranchNode([]byte("test2"), testgsUd)
	if err := tdb.registryTasks(ud2); err != nil {
		t.Fatalf("Ud2 fail: %s [%v]", err, ud2)
	}
	dumpUpdating(t, "task 2", tdb)

	if len(tdb.activing) != 3 && len(tdb.updating) != 2 {
		t.Fatalf("reg fail: [%v]", tdb.activing)
	}

	testgs.Count = 27
	testgsUd = pollstate(t, testgs, tdb)
	if testgsUd.Count != 29 {
		t.Fatalf("update error on count: [%v]", testgsUd)
	}
	assertByteEqual(t, testgsUd.LastBranchNodeStateHash, "test2")
	assertByteEqual(t, testgsUd.NextBranchNodeStateHash, "*braEnd")

	testgs.Count = 22
	testgsUd = pollstate(t, testgs, tdb)
	if testgsUd.Count != 24 {
		t.Fatalf("update error on count: [%v]", testgsUd)
	}
	assertByteEqual(t, testgsUd.LastBranchNodeStateHash, "test1")
	assertByteEqual(t, testgsUd.NextBranchNodeStateHash, "test2")

	testgs.Count = 23
	testgs.LastBranchNodeStateHash = []byte("test1")
	testgs.NextBranchNodeStateHash = []byte("*braEnd")

	testgsUd = pollstate(t, testgs, tdb)
	if testgsUd.Count != 24 {
		t.Fatalf("update error on count: [%v]", testgsUd)
	}
	assertByteEqual(t, testgsUd.LastBranchNodeStateHash, "test1")
	assertByteEqual(t, testgsUd.NextBranchNodeStateHash, "test2")

	ud1.before.finalize(tdb)
	if len(ud1.activingPath) != 2 {
		t.Fatalf("fail after finalize ud1 before: [%v]", ud1)
	}

	ud2.before.finalize(tdb)
	if len(ud2.activingPath) != 1 {
		t.Fatalf("fail after finalize ud2 before: [%v]", ud2)
	}

	ud1.after.finalize(tdb)
	if len(ud1.activingPath) != 1 || len(tdb.updating) != 2 {
		t.Fatalf("fail after finalize ud1 after: [%v], [%v]", ud1, tdb.updating)
	}

	ud2.after.finalize(tdb)
	if len(ud2.activingPath) != 0 {
		t.Fatalf("fail after finalize ud2 after: [%v]", ud2)
	}

	if len(tdb.updating) != 0 || len(tdb.activing) != 0 {
		t.Fatalf("fail all task is cleared: [%v], [%v]", tdb.updating, tdb.activing)
	}
}

func TestTxDBGlobalState_connectedtasks(t *testing.T) {

	tdb := new(GlobalDataDB)
	tdb.globalHashLimit = 6
	tdb.updating = make(map[string]*pathUnderUpdating)
	tdb.activing = make(map[string][]*pathUpdateTask)

	testgs1 := new(pb.GlobalState)
	testgs1.Count = 10
	testgs1.NextNodeStateHash = [][]byte{[]byte("dummy")}
	testgs1.NextBranchNodeStateHash = []byte("*braEnd1")
	testgs1.LastBranchNodeStateHash = []byte("*bBegin1")

	testgs2 := new(pb.GlobalState)
	testgs2.Count = 20
	testgs2.ParentNodeStateHash = [][]byte{[]byte("dummy")}
	testgs2.NextBranchNodeStateHash = []byte("*braEnd2")
	testgs2.LastBranchNodeStateHash = []byte("*bBegin2")

	ud1 := tdb.newPathUpdatedByConnected(testgs1, testgs2, []byte("test1"))
	if err := tdb.registryTasks(ud1); err != nil {
		t.Fatalf("Ud1 fail: %s [%v]", err, ud1)
	}
	dumpUpdating(t, "task 1", tdb)

	if ud1.before != nil {
		t.Fatalf("malform ud1: [%v]", ud1)
	}
	assertIntEqual(t, int(ud1.after.idoffset), 12)

	ud2 := tdb.newPathUpdatedByConnected(testgs2, testgs1, []byte("test2"))
	if err := tdb.registryTasks(ud2); err != nil {
		t.Fatalf("Ud2 fail: %s [%v]", err, ud2)
	}
	dumpUpdating(t, "task 2", tdb)

	if ud2.after != nil {
		t.Fatalf("malform ud2: [%v]", ud2)
	}
	assertIntEqual(t, int(ud2.before.idoffset), 1)
	assertIntEqual(t, len(ud1.activingPath), 1)
	assertIntEqual(t, len(tdb.updating), 2)
	assertIntEqual(t, len(ud2.before.updatingPath), 1)
	assertIntEqual(t, len(ud1.after.updatingPath), 1)

	testgsUd := pollstate(t, testgs1, tdb)
	if len(tdb.activing[stateToEdge(testgsUd).String()]) != 2 {
		t.Fatalf("malform activing on gs [%v]: [%v]", testgsUd, tdb.activing)
	}

	assertIntEqual(t, int(testgsUd.Count), 22)

	testgs3 := new(pb.GlobalState)
	testgs3.Count = 4
	testgs3.NextNodeStateHash = [][]byte{[]byte("dummy")}
	testgs3.NextBranchNodeStateHash = []byte("*braEnd3")
	testgs3.LastBranchNodeStateHash = []byte("*bBegin3")

	testgs1.Count = 15
	testgs1.ParentNodeStateHash = [][]byte{[]byte("dummy")}
	testgs1.NextNodeStateHash = nil
	testgs1 = pollstate(t, testgs1, tdb)

	ud3 := tdb.newPathUpdatedByConnected(testgs1, testgs3, []byte("test3"))
	if err := tdb.registryTasks(ud3); err != nil {
		t.Fatalf("Ud3 fail: %s [%v]", err, ud3)
	}
	ud4 := tdb.newPathUpdatedByConnected(testgs3, testgs1, []byte("test4"))
	if err := tdb.registryTasks(ud4); err != nil {
		t.Fatalf("Ud4 fail: %s [%v]", err, ud4)
	}
	dumpUpdating(t, "task 3", tdb)

	assertIntEqual(t, len(tdb.activing), 1)
	assertIntEqual(t, len(tdb.updating), 4)
	testgsUd = pollstate(t, testgsUd, tdb)
	if len(tdb.activing[stateToEdge(testgsUd).String()]) != 2 {
		t.Fatalf("malform activing on gs [%v]: [%v]", testgsUd, tdb.activing)
	}

	if ud1.after.updatingPath != nil {
		t.Fatalf("canceled ud1 is not set: [%v]", ud1)
	}

	assertIntEqual(t, len(ud3.before.updatingPath), 3)

	ud5 := tdb.newPathUpdatedByBranchNode([]byte("test5"), testgsUd)
	if err := tdb.registryTasks(ud5); err != nil {
		t.Fatalf("Ud5 fail: %s [%v]", err, ud5)
	}
	dumpUpdating(t, "task 4", tdb)
	assertIntEqual(t, len(tdb.updating), 5)
	assertIntEqual(t, len(tdb.activing), 2)

	testgs2.Count = 13
	testgs2.ParentNodeStateHash = nil
	testgs2.NextNodeStateHash = [][]byte{[]byte("dummy")}
	testgs2 = pollstate(t, testgs2, tdb)
	assertByteEqual(t, testgs2.LastBranchNodeStateHash, "*bBegin2")

	testgs4 := new(pb.GlobalState)
	testgs4.Count = 7
	testgs4.ParentNodeStateHash = [][]byte{[]byte("dummy")}
	testgs4.NextBranchNodeStateHash = []byte("*braEnd4")
	testgs4.LastBranchNodeStateHash = []byte("*bBegin4")

	ud6 := tdb.newPathUpdatedByConnected(testgs4, testgs2, []byte("test6"))
	if err := tdb.registryTasks(ud6); err != nil {
		t.Fatalf("Ud6 fail: %s [%v]", err, ud6)
	}
	ud7 := tdb.newPathUpdatedByConnected(testgs2, testgs4, []byte("test7"))
	if err := tdb.registryTasks(ud7); err != nil {
		t.Fatalf("Ud7 fail: %s [%v]", err, ud7)
	}
	dumpUpdating(t, "task 5", tdb)
	assertIntEqual(t, len(tdb.activing), 2)
	assertTrue(t, tdb.activing[ud5.after.index()] != nil)

	ud5.after.finalize(tdb)
	dumpUpdating(t, "task 6", tdb)
	assertIntEqual(t, len(tdb.activing), 1)

	ud6.before.finalize(tdb)
	dumpUpdating(t, "task 7", tdb)
	assertIntEqual(t, len(tdb.activing), 1)
	//the interval that ud6 built can be removed
	assertIntEqual(t, len(tdb.updating), 6)

	tt := new(pb.GlobalState)
	tt.Count = 11
	tt.NextBranchNodeStateHash = []byte("*braEnd1")
	tt.LastBranchNodeStateHash = []byte("*bBegin1")

	tt = pollstate(t, tt, tdb)
	assertByteEqual(t, tt.LastBranchNodeStateHash, "test5")
	assertByteEqual(t, tt.NextBranchNodeStateHash, "*braEnd3")
	assertIntEqual(t, int(tt.Count), 25)

	ud7.after.finalize(tdb)
	if len(tdb.updating) != 0 || len(tdb.activing) != 0 {
		t.Fatalf("fail all task is cleared: [%v], [%v]", tdb.updating, tdb.activing)
	}
}

func TestTxDBGlobalState_Basic(t *testing.T) {

	//test for the base state rw: a linear state, include detaching and connecting
	defer adjustTKGen(adjustTKGen(testTerminalKeyGen))
	defer adjustIDStart(adjustIDStart(100))
	Start()
	globalDataDB.globalHashLimit = 8
	defer deleteTestDBPath()
	defer Stop()

	err := globalDataDB.PutGenesisGlobalState([]byte("sroot"))
	if err != nil {
		t.Fatal("Add state fail", err)
	}

	dumpCF(t)
	gs := globalDataDB.getGlobalStateRaw([]byte("sroot"))
	assertTrue(t, globalDataDB.isVirtualState(gs.LastBranchNodeStateHash))
	assertTrue(t, globalDataDB.isVirtualState(gs.NextBranchNodeStateHash))
	sbegin := string(gs.LastBranchNodeStateHash)
	send := string(gs.NextBranchNodeStateHash)

	testPopulatePath(t, "sroot", []string{"s1", "s2"})
	dumpCF(t)

	gs = globalDataDB.getGlobalStateRaw([]byte("sroot"))
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), send)
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), sbegin)

	gs = globalDataDB.getGlobalStateRaw([]byte("s2"))
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), send)
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), sbegin)

	testPopulatePath(t, "s3", []string{"s4", "s5"})
	dumpCF(t)
	gs = globalDataDB.getGlobalStateRaw([]byte("s5"))
	send = string(gs.NextBranchNodeStateHash)

	err = globalDataDB.AddGlobalState([]byte("s2"), []byte("s3"))
	if err != nil {
		t.Fatal("connect state fail", err)
	}
	dumpCF(t)

	gs = globalDataDB.getGlobalStateRaw([]byte("s5"))
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), send)
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), sbegin)

	gs = globalDataDB.getGlobalStateRaw([]byte("s2"))
	assertTrue(t, !gs.Branched())
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), send)
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), sbegin)
}

func TestTxDBGlobalState_BranchRW(t *testing.T) {

	defer adjustTKGen(adjustTKGen(testTerminalKeyGen))
	defer adjustIDStart(adjustIDStart(100))
	Start()
	globalDataDB.globalHashLimit = 8
	defer deleteTestDBPath()
	defer Stop()

	err := globalDataDB.PutGenesisGlobalState([]byte("sroot"))
	if err != nil {
		t.Fatal("Add state fail", err)
	}

	testPopulatePath(t, "sroot", []string{"s1", "s2", "s3", "s4", "s5", "s6"})
	dumpCF(t)

	gs := globalDataDB.GetGlobalState([]byte("sroot"))
	assertByteEqual(t, gs.NextNode(), "s1")
	assertByteEqual(t, gs.ParentNode(), "")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "")
	assertIntEqual(t, int(gs.Count), 0)

	gs = globalDataDB.GetGlobalState([]byte("s5"))
	assertByteEqual(t, gs.NextNode(), "s6")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "")
	assertIntEqual(t, int(gs.Count), 5)

	//branch 1
	testPopulatePath(t, "s1", []string{"s11", "s12", "s13"})
	dumpCF(t)

	gs = globalDataDB.GetGlobalState([]byte("sroot"))
	assertByteEqual(t, gs.NextNode(), "s1")
	assertByteEqual(t, gs.ParentNode(), "")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s1")

	gs = globalDataDB.GetGlobalState([]byte("s5"))
	assertByteEqual(t, gs.NextNode(), "s6")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s1")

	gs = globalDataDB.GetGlobalState([]byte("s6"))
	assertByteEqual(t, gs.NextNode(), "")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s1")

	gs = globalDataDB.GetGlobalState([]byte("s2"))
	assertByteEqual(t, gs.ParentNode(), "s1")
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s1")

	gs = globalDataDB.GetGlobalState([]byte("s11"))
	assertByteEqual(t, gs.NextNode(), "s12")
	assertByteEqual(t, gs.ParentNode(), "s1")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s1")
	assertIntEqual(t, int(gs.Count), 2)

	gs = globalDataDB.GetGlobalState([]byte("s13"))
	assertByteEqual(t, gs.ParentNode(), "s12")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s1")
	assertIntEqual(t, int(gs.Count), 4)

	gs = globalDataDB.GetGlobalState([]byte("s1"))
	assertByteEqual(t, gs.ParentNode(), "sroot")
	assertIntEqual(t, len(gs.NextNodeStateHash), 2)
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s1")
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "")

	//branch 2
	testPopulatePath(t, "s5", []string{"s51", "s52"})
	dumpCF(t)

	gs = globalDataDB.GetGlobalState([]byte("s5"))
	assertIntEqual(t, len(gs.NextNodeStateHash), 2)
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s5")
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s1")

	gs = globalDataDB.GetGlobalState([]byte("s6"))
	assertByteEqual(t, gs.ParentNode(), "s5")
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s5")
	assertIntEqual(t, int(gs.Count), 8)

	gs = globalDataDB.GetGlobalState([]byte("s51"))
	assertByteEqual(t, gs.ParentNode(), "s5")
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s5")
	assertIntEqual(t, int(gs.Count), 7)

	gs = globalDataDB.GetGlobalState([]byte("s4"))
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s1")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s5")
	assertIntEqual(t, int(gs.Count), 6)

	gs = globalDataDB.GetGlobalState([]byte("s1"))
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s1")

	//branch 3
	testPopulatePath(t, "s1", []string{"s1x1", "s1x2"})
	dumpCF(t)

	gs = globalDataDB.GetGlobalState([]byte("s4"))
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s1")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s5")

	gs = globalDataDB.GetGlobalState([]byte("s1"))
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s1")

	gs = globalDataDB.GetGlobalState([]byte("s11"))
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s1")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")
	assertIntEqual(t, int(gs.Count), 2)

	gs = globalDataDB.GetGlobalState([]byte("s1x2"))
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s1")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")
	assertIntEqual(t, int(gs.Count), 4)

	//branch 4
	testPopulatePath(t, "s4", []string{"s41", "s42"})
	dumpCF(t)

	gs = globalDataDB.GetGlobalState([]byte("s4"))
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s1")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s4")

	gs = globalDataDB.GetGlobalState([]byte("s6"))
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s5")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")

	gs = globalDataDB.GetGlobalState([]byte("s5"))
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s4")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s5")

	gs = globalDataDB.GetGlobalState([]byte("s3"))
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s1")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s4")

	gs = globalDataDB.GetGlobalState([]byte("s42"))
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s4")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")

}

func TestTxDBGlobalState_ComplexBranch(t *testing.T) {

	//test many different type of connectionstate: T-shape, H-shape etc

	defer adjustTKGen(adjustTKGen(testTerminalKeyGen))
	defer adjustIDStart(adjustIDStart(100))
	Start()
	globalDataDB.globalHashLimit = 8
	defer deleteTestDBPath()
	defer Stop()

	err := globalDataDB.PutGenesisGlobalState([]byte("sroot"))
	if err != nil {
		t.Fatal("Add state fail", err)
	}

	testPopulatePath(t, "sroot", []string{"s1", "s2", "s3"})
	testPopulatePath(t, "s4", []string{"s5", "s6", "s7"})
	dumpCF(t)

	//H shape
	if err := globalDataDB.AddGlobalState([]byte("s1"), []byte("s5")); err != nil {
		t.Fatal("Add state fail", err)
	}
	dumpCF(t)

	gs := globalDataDB.GetGlobalState([]byte("s7"))
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s5")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")

	gs = globalDataDB.GetGlobalState([]byte("s4"))
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s5")

	gs = globalDataDB.GetGlobalState([]byte("s5"))
	assertTrue(t, gs.Branched())
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s5")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")

	//T shape (from head)
	testPopulatePath(t, "s8", []string{"s9", "s10"})
	if err := globalDataDB.AddGlobalState([]byte("s10"), []byte("s2")); err != nil {
		t.Fatal("Add state fail", err)
	}
	dumpCF(t)

	gs = globalDataDB.GetGlobalState([]byte("s8"))
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s2")

	gs = globalDataDB.GetGlobalState([]byte("s2"))
	assertTrue(t, gs.Branched())
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s2")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")

	//T shape (from tail)
	testPopulatePath(t, "s11", []string{"s12", "s13"})
	if err := globalDataDB.AddGlobalState([]byte("s6"), []byte("s11")); err != nil {
		t.Fatal("Add state fail", err)
	}
	dumpCF(t)

	gs = globalDataDB.GetGlobalState([]byte("s7"))
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s6")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")

	gs = globalDataDB.GetGlobalState([]byte("s13"))
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s6")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")

	gs = globalDataDB.GetGlobalState([]byte("s11"))
	assertTrue(t, !gs.Branched())
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s6")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")

	//cyclic
	if err := globalDataDB.AddGlobalState([]byte("s12"), []byte("s9")); err != nil {
		t.Fatal("Add state fail", err)
	}
	dumpCF(t)

	gs = globalDataDB.GetGlobalState([]byte("s12"))
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s6")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s12")

	gs = globalDataDB.GetGlobalState([]byte("s10"))
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s9")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s2")
}

func handleTask(sn *gorocksdb.Snapshot, tsks []*pathUpdateTask) {

	wb := globalDataDB.NewWriteBatch()
	defer wb.Destroy()

	//execute all tasks ...
	for _, task := range tsks {
		defer task.finalize(globalDataDB)
		for task.updatePath(sn, wb, globalDataDB) == shouldCommit {
		}
	}

	globalDataDB.BatchCommit(wb)
}

func TestTxDBGlobalState_ComplexCyclic1(t *testing.T) {

	defer adjustTKGen(adjustTKGen(testTerminalKeyGen))
	defer adjustIDStart(adjustIDStart(100))
	Start()
	globalDataDB.globalHashLimit = 8
	defer deleteTestDBPath()
	defer Stop()

	testPopulatePath(t, "sroot", []string{"s1", "s2", "s3", "s4", "s5", "s6"})
	dumpCF(t)

	wb := globalDataDB.NewWriteBatch()
	defer wb.Destroy()

	//we must also verify reading nodes under updating is correct
	var s3Count uint64

	if sn, tsks, err := globalDataDB.addGSCritical([]byte("s5"), []byte("s2")); err != nil {
		t.Fatal("Add state fail", err)
	} else {

		if err := globalDataDB.BatchCommit(wb); err != nil {
			t.Fatal("Commiting final add global state fail:", err)
		}

		gs := globalDataDB.getGlobalStateRaw([]byte("s3"))
		gs, _ = globalDataDB.innerUpdateState(gs)

		s3Count = gs.Count
		t.Log(gs)

		assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s2")
		assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s5")

		handleTask(sn, tsks)
		defer globalDataDB.ReleaseSnapshot(sn)
	}

	dumpCF(t)

	gs := globalDataDB.GetGlobalState([]byte("s2"))
	assertTrue(t, gs.Branched())
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s2")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s5")

	gs = globalDataDB.GetGlobalState([]byte("s5"))
	assertTrue(t, gs.Branched())
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s2")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s5")

	gs = globalDataDB.GetGlobalState([]byte("s3"))
	assertTrue(t, !gs.Branched())
	assertUint64Equal(t, gs.Count, s3Count)
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s2")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s5")

	gs = globalDataDB.GetGlobalState([]byte("s6"))
	assertTrue(t, !gs.Branched())
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s5")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")

	assertIntEqual(t, len(globalDataDB.activing), 0)
}

func TestTxDBGlobalState_ComplexCyclic2(t *testing.T) {

	defer adjustTKGen(adjustTKGen(testTerminalKeyGen))
	defer adjustIDStart(adjustIDStart(100))
	Start()
	globalDataDB.globalHashLimit = 8
	defer deleteTestDBPath()
	defer Stop()

	testPopulatePath(t, "sroot", []string{"s1", "s2", "s3", "s4", "s5"})

	wb := globalDataDB.NewWriteBatch()
	defer wb.Destroy()

	var s3Count uint64

	if sn, tsks, err := globalDataDB.addGSCritical([]byte("s2"), []byte("s5")); err != nil {
		t.Fatal("Add state fail", err)
	} else {

		if err := globalDataDB.BatchCommit(wb); err != nil {
			t.Fatal("Commiting final add global state fail:", err)
		}

		gs := globalDataDB.getGlobalStateRaw([]byte("s3"))
		gs, _ = globalDataDB.innerUpdateState(gs)

		s3Count = gs.Count
		t.Log(gs)

		assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s2")
		assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s5")

		handleTask(sn, tsks)
		defer globalDataDB.ReleaseSnapshot(sn)
	}
	dumpCF(t)

	gs := globalDataDB.GetGlobalState([]byte("s2"))
	assertTrue(t, gs.Branched())
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s2")

	//notice s5 is a branched node (has two parents) but has no nextbranch
	gs = globalDataDB.GetGlobalState([]byte("s5"))
	assertTrue(t, gs.Branched())
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s5")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")

	gs = globalDataDB.GetGlobalState([]byte("s3"))
	assertTrue(t, !gs.Branched())
	assertUint64Equal(t, gs.Count, s3Count)
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s2")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s5")

	gs = globalDataDB.GetGlobalState([]byte("sroot"))
	assertTrue(t, !gs.Branched())
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s2")

	assertIntEqual(t, len(globalDataDB.activing), 0)
}

func TestTxDBGlobalState_ComplexCyclic3(t *testing.T) {

	defer adjustTKGen(adjustTKGen(testTerminalKeyGen))
	defer adjustIDStart(adjustIDStart(100))
	Start()
	globalDataDB.globalHashLimit = 8
	defer deleteTestDBPath()
	defer Stop()

	testPopulatePath(t, "sroot", []string{"s1", "s2", "s3", "s4", "s5"})

	wb := globalDataDB.NewWriteBatch()
	defer wb.Destroy()

	var s3Count uint64

	if sn, tsks, err := globalDataDB.addGSCritical([]byte("s5"), []byte("s2")); err != nil {
		t.Fatal("Add state fail", err)
	} else {

		if err := globalDataDB.BatchCommit(wb); err != nil {
			t.Fatal("Commiting final add global state fail:", err)
		}

		gs := globalDataDB.getGlobalStateRaw([]byte("s3"))
		gs, _ = globalDataDB.innerUpdateState(gs)

		s3Count = gs.Count
		t.Log(gs)

		assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s2")
		assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s2")

		handleTask(sn, tsks)
		defer globalDataDB.ReleaseSnapshot(sn)
	}
	dumpCF(t)

	gs := globalDataDB.GetGlobalState([]byte("s2"))
	assertTrue(t, gs.Branched())
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s2")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s2")

	gs = globalDataDB.GetGlobalState([]byte("s5"))
	assertTrue(t, !gs.Branched())
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s2")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s2")

	gs = globalDataDB.GetGlobalState([]byte("s3"))
	assertTrue(t, !gs.Branched())
	assertUint64Equal(t, gs.Count, s3Count)
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s2")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s2")

	gs = globalDataDB.GetGlobalState([]byte("sroot"))
	assertTrue(t, !gs.Branched())
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s2")

	assertIntEqual(t, len(globalDataDB.updating), 0)
	assertIntEqual(t, len(globalDataDB.activing), 0)
}

func TestTxDBGlobalState_ComplexCyclic4(t *testing.T) {

	defer adjustTKGen(adjustTKGen(testTerminalKeyGen))
	defer adjustIDStart(adjustIDStart(100))
	Start()
	globalDataDB.globalHashLimit = 8
	defer deleteTestDBPath()
	defer Stop()

	testPopulatePath(t, "sroot", []string{"s1", "s2", "s3", "s4", "s5"})

	wb := globalDataDB.NewWriteBatch()
	defer wb.Destroy()

	var s3Count uint64

	if sn, tsks, err := globalDataDB.addGSCritical([]byte("s4"), []byte("sroot")); err != nil {
		t.Fatal("Add state fail", err)
	} else {

		if err := globalDataDB.BatchCommit(wb); err != nil {
			t.Fatal("Commiting final add global state fail:", err)
		}

		gs := globalDataDB.getGlobalStateRaw([]byte("s3"))
		gs, _ = globalDataDB.innerUpdateState(gs)

		s3Count = gs.Count
		t.Log(gs)

		assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s4")
		assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s4")

		handleTask(sn, tsks)
		defer globalDataDB.ReleaseSnapshot(sn)
	}
	dumpCF(t)

	gs := globalDataDB.GetGlobalState([]byte("s5"))
	assertTrue(t, !gs.Branched())
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s4")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")

	gs = globalDataDB.GetGlobalState([]byte("s3"))
	assertTrue(t, !gs.Branched())
	assertUint64Equal(t, gs.Count, s3Count)
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s4")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s4")

	gs = globalDataDB.GetGlobalState([]byte("sroot"))
	assertTrue(t, !gs.Branched())
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s4")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "s4")

	assertIntEqual(t, len(globalDataDB.updating), 0)
	assertIntEqual(t, len(globalDataDB.activing), 0)
}

func TestTxDBGlobalState_PureCyclic(t *testing.T) {

	defer adjustTKGen(adjustTKGen(testTerminalKeyGen))
	defer adjustIDStart(adjustIDStart(100))
	Start()
	globalDataDB.globalHashLimit = 8
	defer deleteTestDBPath()
	defer Stop()

	testPopulatePath(t, "sroot", []string{"s1", "s2", "s3", "s4", "s5"})

	if err := globalDataDB.AddGlobalState([]byte("s5"), []byte("sroot")); err != nil {
		t.Fatal("Add state fail", err)
	}

	dumpCF(t)

	if err := globalDataDB.AddGlobalState([]byte("s6"), []byte("s3")); err != nil {
		t.Fatal("Add state fail", err)
	}

	dumpCF(t)
	gs := globalDataDB.GetGlobalState([]byte("s4"))
	assertTrue(t, !gs.Branched())
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "s3")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "sroot")

}

func TestTxDBGlobalState_RandomRW(t *testing.T) {

	defer adjustTKGen(adjustTKGen(testTerminalKeyGen))
	defer adjustIDStart(adjustIDStart(100))
	Start()
	globalDataDB.globalHashLimit = 8
	defer deleteTestDBPath()
	defer Stop()

	//populated global states
	//we have such a graphic:
	//                                       |--<root7>
	//                                       |
	// <sroot> ---- <root1> ---- <root2> --- <root3>
	//               |
	//               |--<root4> ---- <root5>
	//               |
	//               |-<root6>
	//
	// The adjacent matrix:
	adjm := map[string][]string{
		"sroot": []string{"root1", "root4", "root6"},
		"root1": []string{"root2"},
		"root2": []string{"root3", "root7"},
		"root3": nil,
		"root4": []string{"root5"},
		"root5": nil,
		"root6": nil,
		"root7": nil,
	}

	addTask := func(curtasks [][2]string, state string) [][2]string {
		nodes, ok := adjm[state]
		if !ok {
			t.Fatal("Not valid state", state)
		}

		for _, newnode := range nodes {
			curtasks = append(curtasks, [2]string{state, newnode})
		}

		return curtasks
	}

	//random populating ...
	err := globalDataDB.PutGenesisGlobalState([]byte("sroot"))
	if err != nil {
		t.Fatal("Add state fail", err)
	}

	r := rand.New(rand.NewSource(time.Now().Unix()))
	curTasks := addTask(nil, "sroot")

	dumpCF(t)
	for len(curTasks) != 0 {

		rpos := r.Intn(len(curTasks))
		tsk := curTasks[rpos]
		newstate := tsk[1]

		t.Log("Add state", tsk)

		err = globalDataDB.AddGlobalState([]byte(tsk[0]), []byte(newstate))
		if err != nil {
			t.Fatal("Add state fail", err)
		}

		dumpCF(t)
		//switch with tail ...
		if rpos != len(curTasks)-1 {
			curTasks[rpos][0] = curTasks[len(curTasks)-1][0]
			curTasks[rpos][1] = curTasks[len(curTasks)-1][1]
		}

		curTasks = addTask(curTasks[:len(curTasks)-1], newstate)
	}

	//test
	gs := globalDataDB.GetGlobalState([]byte("sroot"))
	assertIntEqual(t, len(gs.NextNodeStateHash), 3)
	assertByteEqual(t, gs.ParentNode(), "")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "sroot")
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "")
	assertIntEqual(t, int(gs.Count), 1)

	gs = globalDataDB.GetGlobalState([]byte("root1"))
	assertByteEqual(t, gs.NextNode(), "root2")
	assertByteEqual(t, gs.ParentNode(), "sroot")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "root2")
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "sroot")

	gs = globalDataDB.GetGlobalState([]byte("root2"))
	assertIntEqual(t, len(gs.NextNodeStateHash), 2)
	assertByteEqual(t, gs.ParentNode(), "root1")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "root2")
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "sroot")

	gs = globalDataDB.GetGlobalState([]byte("root3"))
	assertByteEqual(t, gs.NextNode(), "")
	assertByteEqual(t, gs.ParentNode(), "root2")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "root2")

	gs = globalDataDB.GetGlobalState([]byte("root4"))
	assertByteEqual(t, gs.NextNode(), "root5")
	assertByteEqual(t, gs.ParentNode(), "sroot")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "sroot")

	gs = globalDataDB.GetGlobalState([]byte("root6"))
	assertByteEqual(t, gs.NextNode(), "")
	assertByteEqual(t, gs.ParentNode(), "sroot")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "sroot")

	gs = globalDataDB.GetGlobalState([]byte("root7"))
	assertByteEqual(t, gs.NextNode(), "")
	assertByteEqual(t, gs.ParentNode(), "root2")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "root2")

	gs = globalDataDB.GetGlobalState([]byte("root5"))
	assertByteEqual(t, gs.NextNode(), "")
	assertByteEqual(t, gs.ParentNode(), "root4")
	assertByteEqual(t, gs.GetNextBranchNodeStateHash(), "")
	assertByteEqual(t, gs.GetLastBranchNodeStateHash(), "sroot")
}
