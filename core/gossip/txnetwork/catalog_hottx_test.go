package txnetwork

import (
	"bytes"
	model "github.com/abchain/fabric/core/gossip/model"
	"github.com/abchain/fabric/core/ledger"
	pb "github.com/abchain/fabric/protos"
	"testing"
)

func TestTxChain(t *testing.T) {
	tx1, _ := buildTestTx(t)
	tx2, _ := buildTestTx(t)
	oldNonce := tx2.GetNonce()
	oldDigest := getTxDigest(tx2)

	tx1 = buildPrecededTx(genesisDigest, tx1)
	dg1 := getTxDigest(tx1)

	tx2 = buildPrecededTx(dg1, tx2)

	if !txIsPrecede(dg1, tx2) {
		t.Fatalf("tx2 is not precede of tx1: %x vs %x", dg1, tx2.GetNonce())
	}

	tx2.Nonce = oldNonce

	nowDigest := getTxDigest(tx2)

	if bytes.Compare(nowDigest, oldDigest) != 0 {
		t.Fatalf("digest not equal: %x vs %x", nowDigest, oldDigest)
	}
}

func prolongItemChain(t *testing.T, head *txMemPoolItem, n int) *peerTxs {

	cur := head

	for i := 0; i < n; i++ {

		tx, _ := buildTestTx(t)
		tx = buildPrecededTx(cur.digest, tx)

		dg := getTxDigest(tx)

		t.Logf("create tx %s with digest %x", tx.GetTxid(), dg)

		cur.next = &txMemPoolItem{
			digest:       dg,
			digestSeries: cur.digestSeries + 1,
			tx:           tx,
		}
		cur = cur.next
	}

	return &peerTxs{head, cur}
}

func populatePoolItems(t *testing.T, n int) *peerTxs {

	return prolongItemChain(t, &txMemPoolItem{digest: genesisDigest}, n)

}

func TestPeerTxs(t *testing.T) {

	txs := populatePoolItems(t, 3)

	if txs.lastSeries() != 3 {
		t.Fatalf("broken series %d", txs.last.digestSeries)
	}

	for beg := txs.head; beg != txs.last; beg = beg.next {
		if !txIsPrecede(beg.digest, beg.next.tx) {
			t.Fatalf("chain 1 broken at %d", beg.digestSeries)
		}
	}

	txs2 := prolongItemChain(t, txs.last.clone(), 4)
	txs2.head = txs2.head.next

	err := txs.concat(txs2)

	if err != nil {
		t.Fatalf("concat chain fail: %s", err)
	}

	if txs.lastSeries() != 7 {
		t.Fatalf("broken series %d after concat", txs.last.digestSeries)
	}

	for beg := txs.head; beg != txs.last; beg = beg.next {
		if !txIsPrecede(beg.digest, beg.next.tx) {
			t.Fatalf("chain 2 broken at %d", beg.digestSeries)
		}
	}

	if txs2.inRange(1) || txs2.inRange(8) || !txs2.inRange(5) {
		t.Fatalf("wrong in range 2 in chain2")
	}

	txs3 := txs.fetch(4, nil)

	if txs3.head.digestSeries != 4 {
		t.Fatalf("wrong chain 3: %d", txs3.head.digestSeries)
	}

	if txs3.lastSeries() != 7 {
		t.Fatalf("wrong chain 3 tail: %d", txs3.lastSeries())
	}

	if bytes.Compare(txs3.head.next.tx.Payload, txs2.head.next.tx.Payload) != 0 {
		t.Fatalf("wrong tx in identify chain 2 and 3")
	}

	if txsnil := txs3.fetch(2, nil); txsnil != nil {
		t.Fatalf("fetch ghost txs: %d", txsnil.head.digestSeries)
	}
}

func assertTxIsIdentify(tb testing.TB, tx1 *pb.Transaction, tx2 *pb.Transaction) {
	dg1, _ := tx1.Digest()
	dg2, _ := tx2.Digest()

	if bytes.Compare(dg1, dg2) != 0 {
		tb.Fatalf("tx is not same: %v vs %v", tx1, tx2)
	}
}

func formIndexs(txchain *peerTxs) []*txMemPoolItem {

	var indexs []*txMemPoolItem

	//collect all items into array
	for i := txchain.head; i != nil; i = i.next {
		indexs = append(indexs, i)
	}

	return indexs
}

func formTestData(ledger *ledger.Ledger, indexs []*txMemPoolItem, commitsetting [][]int) {

	genTxs := func(ii []int) (out []*pb.Transaction) {
		for _, i := range ii {
			out = append(out, indexs[i].tx)
		}
		return
	}

	//add gensis block
	ledger.BeginTxBatch(0)
	ledger.CommitTxBatch(0, nil, nil, nil)

	for ib := 0; ib < len(commitsetting); ib++ {
		ledger.BeginTxBatch(1)
		ledger.TxBegin("txUuid")
		ledger.SetState("chaincode1", "keybase", []byte{byte(ib)})
		ledger.TxFinished("txUuid", true)
		ledger.CommitTxBatch(1, genTxs(commitsetting[ib]), nil, []byte("proof1"))
	}

	return
}

type collection map[string]*txMemPoolItem

func (d collection) Import(items []*txMemPoolItem) {

	for _, i := range items {
		d[i.tx.GetTxid()] = i
	}
}

func newTxCol() collection { return collection(make(map[string]*txMemPoolItem)) }

func completeTxs(txsin []*pb.Transaction, tp *transactionPool) ([]*pb.Transaction, error) {

	var err error
	for i, tx := range txsin {
		tx, err = tp.completeTx(tx)
		if err != nil {
			return txsin[:i], err
		}
		txsin[i] = tx
	}

	return txsin, nil
}

func TestPeerUpdate(t *testing.T) {

	ledger := initTestLedgerWrapper(t)
	initGlobalStatus()

	txchain := populatePoolItems(t, 10)
	txIndexs := formIndexs(txchain)

	var udt = txPeerUpdate{new(pb.HotTransactionBlock)}
	udt.fromTxs(txchain.fetch(1, nil))

	if udt.BeginSeries != 1 {
		t.Fatalf("wrong begin series in udt1: %d", udt.BeginSeries)
	}

	if len(udt.GetTransactions()) != 10 {
		t.Fatalf("wrong tx length in udt1: %d", len(udt.GetTransactions()))
	}

	assertTxIsIdentify(t, txIndexs[3].tx, udt.GetTransactions()[2])
	assertTxIsIdentify(t, txIndexs[6].tx, udt.GetTransactions()[5])
	assertTxIsIdentify(t, txIndexs[8].tx, udt.GetTransactions()[7])
	assertTxIsIdentify(t, txIndexs[9].tx, udt.GetTransactions()[8])

	retTxs, err := udt.toTxs()
	if err != nil {
		t.Fatal("to txs fail:", err)
	}

	if retTxs.lastSeries() != 10 || retTxs.head.digestSeries != 1 {
		t.Fatalf("fail last or head: %d/%d", retTxs.lastSeries(), retTxs.head.digestSeries)
	}

	formTestData(ledger, txIndexs, [][]int{nil, []int{2, 4, 7}, []int{3, 5}})

	udt.HotTransactionBlock = new(pb.HotTransactionBlock)
	udt.fromTxs(retTxs.fetch(1, nil))

	if udt.BeginSeries != 1 {
		t.Fatalf("wrong begin series in udt2: %d", udt.BeginSeries)
	}

	if len(udt.GetTransactions()) != 10 {
		t.Fatalf("wrong tx length in udt2: %d", len(udt.GetTransactions()))
	}

	assertTxIsIdentify(t, txIndexs[1].tx, udt.GetTransactions()[0])
	assertTxIsIdentify(t, txIndexs[6].tx, udt.GetTransactions()[5])
	assertTxIsIdentify(t, txIndexs[8].tx, udt.GetTransactions()[7])
	assertTxIsIdentify(t, txIndexs[9].tx, udt.GetTransactions()[8])

	txPool := newTransactionPool(ledger)
	commitCache := txPool.AcquireCaches("helperCache")
	commitCache.AddTxs(udt.BeginSeries, udt.GetTransactions())

	commitCache.PruneTxs(2, udt.HotTransactionBlock)

	if !isLiteTx(udt.GetTransactions()[1]) {
		t.Fatalf("unexpected full-tx <2>")
	}

	if !isLiteTx(udt.GetTransactions()[6]) {
		t.Fatalf("unexpected full-tx <7>")
	}

	_, err = completeTxs(udt.Transactions, txPool)
	if err != nil {
		t.Fatal("handle udt fail:", err)
	}

	handledudt := udt.getRef(5)

	retTxs, err = handledudt.toTxs()
	if err != nil {
		t.Fatal("to txs fail:", err)
	}

	if retTxs.lastSeries() != 10 || retTxs.head.digestSeries != 5 {
		t.Fatalf("fail last or head: %d/%d", retTxs.lastSeries(), retTxs.head.digestSeries)
	}

	udt.HotTransactionBlock = new(pb.HotTransactionBlock)

	udt.fromTxs(retTxs.fetch(5, nil))

	assertTxIsIdentify(t, txIndexs[5].tx, udt.GetTransactions()[0])
	assertTxIsIdentify(t, txIndexs[6].tx, udt.GetTransactions()[1])
	assertTxIsIdentify(t, txIndexs[7].tx, udt.GetTransactions()[2])

	//check less index
	handledudt = udt.getRef(3)

	retTxs, err = handledudt.toTxs()
	if err != nil {
		t.Fatal("to txs fail:", err)
	}

	if retTxs.lastSeries() != 10 || retTxs.head.digestSeries != 5 {
		t.Fatalf("fail last or head: %d/%d", retTxs.lastSeries(), retTxs.head.digestSeries)
	}

}

func TestPeerTxPool(t *testing.T) {

	defer func(bits uint) {
		SetPeerTxQueueLen(bits)
	}(peerTxQueueLenBit)

	SetPeerTxQueueLen(3)

	ledger := initTestLedgerWrapper(t)
	global := initGlobalStatus()

	txGlobal := &txPoolGlobal{
		network:         global,
		transactionPool: global.txPool,
	}

	defaultPeer := "test"
	txchainBase := populatePoolItems(t, 39)
	indexs := formIndexs(txchainBase)

	formTestData(ledger, indexs, [][]int{nil, []int{8, 12, 15}, []int{23, 13}, []int{7, 38}})

	//we fill txpool from series 5, fill pool with a jumping index of 4 entries
	pool := new(peerTxMemPool)
	pool.reset(indexs[5])

	if len(pool.jlindex) != 4 {
		t.Fatal("unexpected jlindex:", pool.jlindex)
	}

	if pool.jlindex[3].digestSeries != 24 {
		t.Fatal("wrong entry in jlindex:", pool.jlindex[3])
	}

	if pool.lastSeries() != 39 || pool.head.digestSeries != 5 {
		t.Fatalf("fail last or head: %d/%d", pool.lastSeries(), pool.head.digestSeries)
	}

	//test To method
	if vi, ok := pool.To().(standardVClock); !ok {
		t.Fatalf("To vclock wrong: %v", pool.To())
	} else if uint64(vi) != 39 {
		t.Fatalf("To vclock wrong value: %v", vi)
	}

	//test pickFrom method
	ud_out, _ := pool.PickFrom(standardVClock(14), txPoolGlobalUpdateOut{txGlobal, 2})

	ud, ok := ud_out.(txPeerUpdate)

	if !ok {
		t.Fatalf("type fail: %v", ud_out)
	}

	if ud.BeginSeries != 15 {
		t.Fatalf("unexpected begin: %d", ud.BeginSeries)
	}

	assertTxIsIdentify(t, indexs[16].tx, ud.GetTransactions()[1])
	assertTxIsIdentify(t, indexs[23].tx, ud.GetTransactions()[8])
	assertTxIsIdentify(t, indexs[38].tx, ud.GetTransactions()[23])

	//test out-date pick
	ud_out, _ = pool.PickFrom(standardVClock(3), txPoolGlobalUpdateOut{txGlobal, 2})

	if ud_out != nil {
		t.Fatalf("get unexpected output %v", ud_out)
	}

	//test update
	txChainAdd := prolongItemChain(t, txchainBase.last, 20)
	indexAdded := formIndexs(txChainAdd)
	indexs = append(indexs, indexAdded[1:]...)

	//"cut" the new chain ..., keep baseChain unchange
	newAddHead := txChainAdd.head.next
	txChainAdd.head.next = nil
	txChainAdd.head = newAddHead

	//collect more items ...
	for i := txChainAdd.head; i != nil; i = i.next {
		indexs = append(indexs, i)
	}

	udt := txPeerUpdate{new(pb.HotTransactionBlock)}

	//all item in txChainAdd is not commited so epoch is of no use
	udt.fromTxs(txChainAdd)
	if udt.BeginSeries != 40 {
		t.Fatal("unexpected begin series", udt.BeginSeries)
	}

	//must also add global state ...
	pstatus, _ := global.peers.AddNewPeer(defaultPeer)
	pstatus.Digest = txchainBase.head.digest
	pstatus.Endorsement = []byte{2, 3, 3}

	//you update an unknown peer, no effect in fact
	err := pool.Update("anotherTest", udt, txGlobal)
	if err != nil {
		t.Fatal("update fail", err)
	}

	if txGlobal.AcquireCaches("anotherTest").commitData != nil {
		t.Fatal("update unknown peer")
	}

	err = pool.Update(defaultPeer, udt, txGlobal)
	if err != nil {
		t.Fatal("update actual fail", err)
	}

	if pool.lastSeries() != 59 {
		t.Fatal("unexpected last", pool.lastSeries())
	}

	//now you can get tx from ledger
	checkTx := func(pos int) {

		if ok := global.txPool.cPendingTxs[indexs[pos].tx.GetTxid()]; !ok {
			t.Fatalf("get pool tx %d in pending pool fail", pos)
		}

		//assertTxIsIdentify(t, indexs[pos].tx, tx)
	}

	checkTx(40)
	checkTx(42)
	checkTx(45)
	checkTx(55)

	//"cut" the new chain again (which is concat in update of pool)
	indexs[39].next = nil
	//test update including older data
	anotherpool := new(peerTxMemPool)
	anotherpool.reset(indexs[5])

	if anotherpool.lastSeries() != 39 {
		panic("wrong resetting")
	}

	newChainArr := udt.Transactions
	udt = txPeerUpdate{new(pb.HotTransactionBlock)}
	udt.fromTxs(&peerTxs{indexs[39], indexs[39]})
	if udt.BeginSeries != 39 || len(udt.Transactions) != 1 {
		panic("wrong udt")
	}
	udt.Transactions = append(udt.Transactions, newChainArr...)

	anotherpool.Update(defaultPeer, udt, txGlobal)

	if anotherpool.lastSeries() != 59 {
		t.Fatal("unexpected last for update with old data", pool.lastSeries())
	}

	if c := txGlobal.AcquireCaches(defaultPeer).commitData; c[5] == nil {
		t.Fatal("Wrong commit cache status", c)
	}

	//test purge
	pool.purge("test", 50, txGlobal)

	if pool.head.digestSeries != 50 {
		t.Fatalf("wrong head series after purge: %d", pool.head.digestSeries)
	}

	if c := txGlobal.AcquireCaches(defaultPeer).commitData; c[5] != nil {
		t.Fatal("cache still have cache block which should be purged")
	}

	if len(pool.jlindex) != 1 {
		t.Fatal("wrong index after purge", pool.jlindex)
	}

	if _, ok := pool.jlindex[6]; ok {
		t.Fatal("still have index in jumping list after purge")
	}

	//test pickfrom after purge
	ud_out, _ = pool.PickFrom(standardVClock(53), txPoolGlobalUpdateOut{txGlobal, 0})

	ud, ok = ud_out.(txPeerUpdate)

	if !ok {
		t.Fatalf("type fail: %v", ud_out)
	}

	if ud.BeginSeries != 54 {
		t.Fatalf("unexpected begin: %d", ud.BeginSeries)
	}

	assertTxIsIdentify(t, indexs[55].tx, ud.GetTransactions()[1])
	assertTxIsIdentify(t, indexs[59].tx, ud.GetTransactions()[5])

}

func TestCatalogyHandler(t *testing.T) {

	defer func(bits uint) {
		SetPeerTxQueueLen(bits)
	}(peerTxQueueLenBit)

	SetPeerTxQueueLen(3)

	l := initTestLedgerWrapper(t)
	global := initGlobalStatus()
	txglobal := new(txPoolGlobal)
	txglobal.transactionPool = global.txPool
	txglobal.network = global

	const testname = "test"

	txchainBase := populatePoolItems(t, 39)
	indexs := formIndexs(txchainBase)

	pstatus, _ := global.peers.AddNewPeer(testname)
	pstatus.Digest = txchainBase.head.digest
	pstatus.Endorsement = []byte{2, 3, 3}

	hotTx := new(hotTxCat)

	smodel := model.NewScuttlebuttStatus(txglobal)
	m := model.NewGossipModel(smodel)

	//try to build a proto directly
	dig_in := &pb.GossipMsg_Digest_PeerStates{} //any epoch is ok
	dig_in.PeerD = append(dig_in.PeerD, &pb.GossipMsg_Digest_PeerState{PeerName: testname})

	dig := hotTx.TransPbToDigest(&pb.GossipMsg_Digest{D: &pb.GossipMsg_Digest_Peer{Peer: dig_in}})

	//now model should know peer test
	m.RecvPullDigest(dig)

	dig = m.GenPullDigest()
	dig_out := hotTx.TransDigestToPb(dig)

	var ok bool
	for _, p := range dig_out.GetPeer().PeerD {
		if p.PeerName == testname {
			ok = true
			break
		}
	}
	if !ok {
		t.Fatal("model not known expected peer", dig_out)
	}

	var udt = txPeerUpdate{new(pb.HotTransactionBlock)}
	udt.fromTxs(txchainBase.fetch(1, nil))

	u_in := &pb.Gossip_Tx{map[string]*pb.HotTransactionBlock{testname: udt.HotTransactionBlock}}

	u, err := hotTx.TransPbToUpdate(nil, &pb.GossipMsg_Update{U: &pb.GossipMsg_Update_Txs{Txs: u_in}})
	if err != nil {
		t.Fatal("decode update fail", err)
	}

	err = m.RecvUpdate(u)
	if err != nil {
		t.Fatal("do update fail", err)
	}

	formTestData(l, indexs, [][]int{nil, []int{8, 12, 15}, []int{23, 13}, []int{7, 38}})

	for i, p := range dig_in.PeerD {
		if p.PeerName == testname {
			dig_in.PeerD[i].Num = 20
		}
	}

	blk, _ := l.GetBlockByNumber(3)
	dig_in.Epoch = blk.GetStateHash()
	if len(dig_in.Epoch) == 0 {
		panic("no state hash")
	}

	dig = hotTx.TransPbToDigest(&pb.GossipMsg_Digest{D: &pb.GossipMsg_Digest_Peer{Peer: dig_in}})

	u_out := hotTx.TransUpdateToPb(nil, m.RecvPullDigest(dig)).GetTxs()

	if txs, ok := u_out.Txs[testname]; !ok {
		t.Fatal("update not include expected peer")
	} else {

		t.Log(txs.Transactions)

		if len(txs.Transactions) != 19 {
			t.Fatal("unexpected size of update:", len(txs.Transactions))
		} else if txs.BeginSeries != 21 {
			t.Fatal("unexpected begin of begin series:", txs.BeginSeries)
		}

		if !isLiteTx(txs.GetTransactions()[2]) {
			t.Fatalf("unexpected full-tx <23> (at 2)")
		}

		assertTxIsIdentify(t, indexs[21].tx, txs.GetTransactions()[0])
		assertTxIsIdentify(t, indexs[38].tx, txs.GetTransactions()[17])
		assertTxIsIdentify(t, indexs[27].tx, txs.GetTransactions()[6])
		assertTxIsIdentify(t, indexs[39].tx, txs.GetTransactions()[18])

	}
	//commit more tx
	l.BeginTxBatch(1)
	err = l.CommitTxBatch(1, []*pb.Transaction{indexs[21].tx, indexs[27].tx, indexs[39].tx}, nil, []byte("proof2"))
	if err != nil {
		t.Fatal("commit more block fail", err)
	}

	defcache := txglobal.AcquireCaches(testname)
	if committedH := defcache.GetCommit(21, indexs[21].tx); committedH != 5 {
		t.Fatal("commit update fail 21:", committedH)
	}
	if committedH := defcache.GetCommit(27, indexs[27].tx); committedH != 5 {
		t.Fatal("commit update fail 27:", committedH)
	}

}
