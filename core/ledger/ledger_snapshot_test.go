package ledger

import (
	"github.com/abchain/fabric/core/ledger/testutil"
	"github.com/abchain/fabric/protos"
	"testing"
)

//14 records
var testStates = [][3]string{
	{"chaincode1", "key1", "value1"}, //0
	{"chaincode2", "key2", "value2"},
	{"chaincode1", "key3", "value3"},
	{"chaincode2", "key1", "value4"}, //3
	{"chaincode3", "key2", "value5"},
	{"chaincode2", "key3", "value6"},
	{"chaincode3", "key2", "value7"}, //6
	{"chaincode1", "key3", "value8"},
	{"chaincode2", "key1", "value9"},
	{"chaincode3", "key1", "value0"}, //9
	{"chaincode2", "key3", "valuex"},
	{"chaincode2", "key2", "valueA"},
	{"chaincode1", "key3", "valueB"}, //12
	{"chaincode3", "key1", "valueC"},
}

func populateLedgerForSnapshotTesting(w *ledgerTestWrapper, t *testing.T, testStates [][3]string) {
	ledger := w.ledger

	sn := ledger.snapshots
	sn.snapshotInterval = 3
	sn.snsIndexed = make([][]byte, 3)

	for i, ss := range testStates {

		ledger.BeginTxBatch(i)
		ledger.TxBegin("txUuid")
		ledger.SetState(ss[0], ss[1], []byte(ss[2]))
		ledger.TxFinished("txUuid", true)
		transaction, _ := buildTestTx(t)
		ledger.CommitTxBatch(i, []*protos.Transaction{transaction}, nil, []byte("test"))
	}
}

func TestSnapshot_indexing(t *testing.T) {

	ledgerTestWrapper := createFreshDBAndTestLedgerWrapper(t)

	populateLedgerForSnapshotTesting(ledgerTestWrapper, t, testStates)
	sn := ledgerTestWrapper.ledger.snapshots

	ind, blk := sn.historyIndex(0)
	testutil.AssertEquals(t, ind, -1)

	ind, blk = sn.historyIndex(5)
	testutil.AssertEquals(t, ind, -1)

	ind, blk = sn.historyIndex(6)
	testutil.AssertEquals(t, ind, 2)
	testutil.AssertEquals(t, blk, uint64(6))

	ind, blk = sn.historyIndex(8)
	testutil.AssertEquals(t, ind, 2)
	testutil.AssertEquals(t, blk, uint64(6))

	ind, blk = sn.historyIndex(12)
	testutil.AssertEquals(t, ind, 1)
	testutil.AssertEquals(t, blk, uint64(12))

	ind, blk = sn.historyIndex(13)
	testutil.AssertEquals(t, ind, 1)
	testutil.AssertEquals(t, blk, uint64(12))
}

func TestSnapshot_caching(t *testing.T) {

	ledgerTestWrapper := createFreshDBAndTestLedgerWrapper(t)
	sn := ledgerTestWrapper.ledger.snapshots
	populateLedgerForSnapshotTesting(ledgerTestWrapper, t, testStates[:1])
	testutil.AssertNil(t, sn.snsIndexed[0])
	testutil.AssertNil(t, sn.snsIndexed[1])
	testutil.AssertNil(t, sn.snsIndexed[2])

	populateLedgerForSnapshotTesting(ledgerTestWrapper, t, testStates[1:3])
	testutil.AssertNotNil(t, sn.snsIndexed[0])
	testutil.AssertNil(t, sn.snsIndexed[1])
	populateLedgerForSnapshotTesting(ledgerTestWrapper, t, testStates[3:11])
	testutil.AssertNotNil(t, sn.snsIndexed[2])
	testutil.AssertEquals(t, sn.beginIntervalNum, uint64(1))
	populateLedgerForSnapshotTesting(ledgerTestWrapper, t, testStates[11:])

	testutil.AssertEquals(t, sn.currentHeight, uint64(len(testStates)))
	testutil.AssertEquals(t, sn.beginIntervalNum, uint64(2))
}

func TestSnapshot_cache_outoforder(t *testing.T) {

	ledgerTestWrapper := createFreshDBAndTestLedgerWrapper(t)
	populateLedgerForSnapshotTesting(ledgerTestWrapper, t, nil) //not set anyblock

	sn := ledgerTestWrapper.ledger.snapshots
	sn.Update([]byte("test1A"), 3)
	testutil.AssertNil(t, sn.snsIndexed[0])
	testutil.AssertNil(t, sn.snsIndexed[1])
	testutil.AssertNil(t, sn.snsIndexed[2])
	//notice: we also test bloom filter, which only the last char is different in
	//our implements ...
	testutil.AssertEquals(t, sn.stableStatus.Match([]byte("test1A")), true)

	sn.Update([]byte("test1B"), 0)
	testutil.AssertEquals(t, sn.snsIndexed[0], []byte("test1B"))
	testutil.AssertEquals(t, sn.stableStatus.Match([]byte("test1A")), true)
	testutil.AssertEquals(t, sn.stableStatus.Match([]byte("test1B")), true)
	sn.Update([]byte("test1C"), 2)
	testutil.AssertEquals(t, sn.stableStatus.Match([]byte("test1B")), true)
	testutil.AssertEquals(t, sn.stableStatus.Match([]byte("test1C")), false)
	sn.Update([]byte("test1D"), 4)
	testutil.AssertEquals(t, sn.snsIndexed[1], []byte("test1A"))
	testutil.AssertEquals(t, sn.snsIndexed[0], []byte("test1B"))
	testutil.AssertNil(t, sn.snsIndexed[2])
	testutil.AssertEquals(t, sn.stableStatus.Match([]byte("test1A")), true)
	testutil.AssertEquals(t, sn.stableStatus.Match([]byte("test1D")), false)

	sn.Update([]byte("test2E"), 6)
	testutil.AssertEquals(t, sn.stableStatus.Match([]byte("test1A")), true)
	testutil.AssertEquals(t, sn.stableStatus.Match([]byte("test1B")), true)
	testutil.AssertEquals(t, sn.stableStatus.Match([]byte("test2E")), true)
	sn.Update([]byte("test2F"), 15)
	testutil.AssertEquals(t, sn.stableStatus.Match([]byte("test1B")), false)
	testutil.AssertEquals(t, sn.stableStatus.Match([]byte("test2E")), false)
	testutil.AssertEquals(t, sn.stableStatus.Match([]byte("test2F")), true)
	sn.Update([]byte("test2G"), 15)
	testutil.AssertEquals(t, sn.stableStatus.Match([]byte("test2G")), false)
	testutil.AssertEquals(t, sn.stableStatus.Match([]byte("test2F")), true)
	sn.Update([]byte("test2H"), 16)
	sn.Update([]byte("test2A"), 0)
	testutil.AssertEquals(t, sn.snsIndexed[2], []byte("test2F"))
	testutil.AssertNil(t, sn.snsIndexed[1])
	testutil.AssertNil(t, sn.snsIndexed[0])
	//block 6 should not be cached
	testutil.AssertNil(t, sn.db.GetManagedSnapshot(indexState([]byte("test2E"))))

}

func TestSnapshot_get(t *testing.T) {

	ledgerTestWrapper := createFreshDBAndTestLedgerWrapper(t)

	populateLedgerForSnapshotTesting(ledgerTestWrapper, t, testStates)
	ledger := ledgerTestWrapper.ledger

	rv, _, err := ledger.GetSnapshotState("chaincode1", "key3", 0)
	testutil.AssertError(t, err, "snapshot error")

	//see block 6
	rv, _, err = ledger.GetSnapshotState("chaincode2", "key1", 8)
	testutil.AssertNoError(t, err, "snapshot error")
	testutil.AssertEquals(t, string(rv), "value4")

	//see block 9
	rv, _, err = ledger.GetSnapshotState("chaincode2", "key3", 11)
	testutil.AssertNoError(t, err, "snapshot error")
	testutil.AssertEquals(t, string(rv), "value6")

	//see block 12
	rv, _, err = ledger.GetSnapshotState("chaincode1", "key3", 12)
	testutil.AssertNoError(t, err, "snapshot error")
	testutil.AssertEquals(t, string(rv), "valueB")

	//can see top (13)
	rv, _, err = ledger.GetSnapshotState("chaincode3", "key1", 13)
	testutil.AssertNoError(t, err, "snapshot error")
	testutil.AssertEquals(t, string(rv), "valueC")
}
