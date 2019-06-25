package txnetwork

import (
	"container/list"
	"fmt"
	cred "github.com/abchain/fabric/core/cred"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/util"
	pb "github.com/abchain/fabric/protos"
	"sync"
	"time"
)

const (
	def_maxPeer = 1024
)

//txnetworkglobal manage datas required by single gossip-base txnetwork: (peers, txs, etc...)
type txNetworkGlobal struct {
	notifies
	peers  *txNetworkPeers
	txPool *transactionPool
}

//if this is set, network will be created with default peer status,
//which is useful in testing and some other purpose (i.e. create a simple instance)
var DefaultInitPeer struct {
	Id    string
	State *pb.PeerTxState
}

func ToStringId(id []byte) string {
	return fmt.Sprintf("%x", id)
}

func createNetworkGlobal() *txNetworkGlobal {

	sid := util.GenerateBytesUUID()
	if len(sid) < TxDigestVerifyLen {
		panic("Wrong code generate uuid less than 16 bytes [128bit]")
	}

	peers := &txNetworkPeers{
		maxPeers: def_maxPeer,
		lruQueue: list.New(),
		lruIndex: make(map[string]*list.Element),
	}

	l, err := ledger.GetLedger()
	if err != nil {
		logger.Warning("Could not get default ledger", err)
	}

	txPool := newTransactionPool(l)

	if DefaultInitPeer.Id != "" {
		peers.selfId = DefaultInitPeer.Id
		logger.Infof("Create self peer [%s]", peers.selfId)
		//also create self peer
		peers.lruIndex[peers.selfId] = &list.Element{Value: &peerStatusItem{"", DefaultInitPeer.State, time.Now()}}
	}

	return &txNetworkGlobal{
		peers:  peers,
		txPool: txPool,
	}
}

type notifies struct {
	sync.RWMutex
	onupdate  []func(string, bool) error
	onevicted []func([]string) error
	onsetself []func(string, *pb.PeerTxState)
}

func (g *notifies) RegSetSelfPeer(f func(string, *pb.PeerTxState)) {
	g.Lock()
	defer g.Unlock()
	g.onsetself = append(g.onsetself, f)
}

func (g *notifies) RegEvictNotify(f func([]string) error) {
	g.Lock()
	defer g.Unlock()
	g.onevicted = append(g.onevicted, f)
}

func (g *notifies) RegUpdateNotify(f func(string, bool) error) {
	g.Lock()
	defer g.Unlock()
	g.onupdate = append(g.onupdate, f)
}

func (g *notifies) handleUpdate(id string, created bool) {
	ret := make(chan error)
	g.RLock()
	go func(onupdate []func(string, bool) error) {
		logger.Debugf("Trigger %d notifies for updating peer [%s], (create mode %v)", len(onupdate), id, created)
		for _, f := range onupdate {
			ret <- f(id, created)
		}

		close(ret)
	}(g.onupdate)
	g.RUnlock()

	for e := range ret {
		if e != nil {
			logger.Errorf("Handle update function fail: %s", e)
		}
	}
}

func (g *notifies) handleEvict(ids []string) {
	ret := make(chan error)
	g.RLock()
	go func(onevicted []func([]string) error) {
		logger.Debugf("Trigger %d notifies for evicting peers [%v]", len(onevicted), ids)
		for _, f := range onevicted {
			ret <- f(ids)
		}

		close(ret)
	}(g.onevicted)
	g.RUnlock()

	for e := range ret {
		if e != nil {
			logger.Errorf("Handle evicted function fail: %s", e)
		}
	}
}

func (g *notifies) handleSetSelf(id string, state *pb.PeerTxState) {
	g.RLock()
	fs := g.onsetself
	g.RUnlock()

	//set self is not need to spawn a thread because it always is called outside any model
	for _, f := range fs {
		f(id, state)
	}
}

type txNetworkPeers struct {
	maxPeers    int
	peerHandler cred.TxHandlerFactory

	sync.RWMutex
	selfId   string
	lruQueue *list.List
	lruIndex map[string]*list.Element
}

func (g *txNetworkPeers) truncateTailPeer(cnt int) (ret []string) {

	for ; cnt > 0; cnt-- {
		v, ok := g.lruQueue.Remove(g.lruQueue.Back()).(*peerStatusItem)
		if !ok {
			panic("Type error, not peerStatus")
		}
		delete(g.lruIndex, v.peerId)
		ret = append(ret, v.peerId)
	}

	return
}

func (g *txNetworkPeers) AddNewPeer(id string) (ret *peerStatus, ids []string) {

	g.Lock()
	defer g.Unlock()

	//lruIndex has included self ID so we simply have blocked a malcious behavior
	if _, ok := g.lruIndex[id]; ok {
		logger.Errorf("Request add duplicated peer [%s]", id)
		return
	}

	if len(g.lruIndex) > g.maxPeers {
		ids = g.truncateTailPeer(len(g.lruIndex) - g.maxPeers)
	}

	//if we can't truncate anypeer, just give up
	if len(g.lruIndex) > g.maxPeers {
		logger.Errorf("Can't not add peer [%s], exceed limit %d", id, g.maxPeers)
		return
	}

	ret = &peerStatus{new(pb.PeerTxState)}

	g.lruIndex[id] = g.lruQueue.PushBack(
		&peerStatusItem{
			id,
			ret.PeerTxState,
			time.Time{},
		})

	logger.Infof("We have known new gossip peer [%s]", id)

	return
}

func (g *txNetworkPeers) RemovePeer(id string) bool {

	g.Lock()
	defer g.Unlock()

	item, ok := g.lruIndex[id]

	if ok {
		logger.Infof("gossip peer [%s] is removed", id)
		g.lruQueue.Remove(item)
		delete(g.lruIndex, id)
		if g.peerHandler != nil {
			g.peerHandler.RemovePreValidator(id)
		}
	}
	return ok
}

var SelfIDNotChange = fmt.Errorf("Try to change to an id current used")

func (g *txNetworkPeers) ChangeSelf(id []byte) error {

	if len(id) < TxDigestVerifyLen {
		return fmt.Errorf("Endorser do not has a id long enough (%d bytes)", TxDigestVerifyLen)
	}

	g.Lock()
	defer g.Unlock()

	sid := ToStringId(id)
	if g.selfId == sid {
		return SelfIDNotChange
	}

	_, ok := g.lruIndex[sid]
	if ok {
		return fmt.Errorf("ID %s has existed and can't not be use", id)
	}

	//old self peer is always being kept
	old, ok := g.lruIndex[g.selfId]
	if ok {
		s, ok := old.Value.(*peerStatusItem)
		if !ok {
			panic(fmt.Errorf("old self is set to unknown data: %T<%v>", old.Value, old.Value))
		}

		s.peerId = g.selfId
		g.lruQueue.PushFront(old)

	}

	//also create self peer, notice we do not endorse it so new self
	//peer will not be propagated before a local-peer updating
	newself := &peerStatusItem{
		"", //mark it was self if
		&pb.PeerTxState{
			Digest: id[:TxDigestVerifyLen],
		},
		time.Now()}

	logger.Infof("Set self peer to [%s]", sid)
	g.selfId = sid
	g.lruIndex[g.selfId] = &list.Element{Value: newself}

	return nil
}

func (g *txNetworkPeers) QueryPeer(id string) *pb.PeerTxState {
	g.RLock()
	defer g.RUnlock()

	i, ok := g.lruIndex[id]
	if ok {
		s := i.Value.(*peerStatusItem).PeerTxState
		if len(s.Endorsement) == 0 {
			//never return an peer which is not inited
			return nil
		}
		return s
	}

	return nil
}

func (g *txNetworkPeers) QuerySelf() (*pb.PeerTxState, string) {

	g.RLock()
	defer g.RUnlock()
	if g.selfId == "" {
		return nil, ""
	}

	i, ok := g.lruIndex[g.selfId]
	if !ok {
		return nil, ""
	}

	return i.Value.(*peerStatusItem).PeerTxState, g.selfId
}

func (g *txNetworkPeers) BlockPeer(id string) {
	g.RLock()
	defer g.RUnlock()
}

func (g *txNetworkPeers) TouchPeer(id string, status *pb.PeerTxState) {
	g.Lock()
	defer g.Unlock()

	//notice scuttlebutt mode has blocked the real selfID being updated from outside
	if item, ok := g.lruIndex[id]; ok {
		if s, ok := item.Value.(*peerStatusItem); ok {
			s.PeerTxState = status
			s.lastAccess = time.Now()
			if s.peerId != "" {
				g.lruQueue.MoveToFront(item)
			}
		}

	}

}

type transactionPool struct {
	ledger       *ledger.Ledger
	txTerminal   pb.TxPreHandler
	preValidator cred.TxHandlerFactory

	sync.RWMutex
	cCaches       map[string]commitData
	cPendingTxs   map[string]bool
	selfTxProcess func() (uint64, []byte)
	currentEpoch  struct {
		height uint64
		hash   []byte
	}
}

func newTransactionPool(ledger *ledger.Ledger) *transactionPool {
	ret := new(transactionPool)
	ret.cCaches = make(map[string]commitData)
	ret.cPendingTxs = make(map[string]bool)

	ledger.AddCommitHook(ret.onCommit)
	ret.ledger = ledger

	return ret
}

func (tp *transactionPool) onCommit(txids []string, _ uint64) {
	tp.Lock()
	defer tp.Unlock()
	for _, id := range txids {
		delete(tp.cPendingTxs, id)
	}
}

func (tp *transactionPool) addPendingTx(txid string) {
	tp.Lock()
	defer tp.Unlock()
	tp.cPendingTxs[txid] = true
}

func (tp *transactionPool) txIsPending(txid string) (ok bool) {
	tp.RLock()
	defer tp.RUnlock()
	_, ok = tp.cPendingTxs[txid]
	return
}

func (tp *transactionPool) confirmCommit(txid string) uint64 {

	if tp.txIsPending(txid) {
		return 0
	}

	h, _, err := tp.ledger.GetBlockNumberByTxid(txid)
	if err != nil {
		//consider txpool will also receive commit signal from other network,
		//this case is ubiquitous
		logger.Debugf("Can not find index of Tx [%s] from ledger, repool it", txid)
		tp.addPendingTx(txid)
		return 0
	}
	return h
}

func (tp *transactionPool) setCommitData(peer string, c commitData) {
	tp.Lock()
	defer tp.Unlock()
	tp.cCaches[peer] = c

}

//the tx-complete filter
func (tp *transactionPool) completeTx(txin *pb.Transaction) (tx *pb.Transaction, err error) {
	if isLiteTx(txin) {
		tx, err = tp.ledger.GetTransactionByID(txin.GetTxid())
		if err != nil {
			err = fmt.Errorf("Checking tx from db fail: %s", err)
			return
		} else if tx == nil {
			err = fmt.Errorf("update give uncommited transactions")
			return
		}
	} else {
		tx = txin
	}

	return
}

//act as the "terminal" of txnetwork process, any tx which has been indexed should be consider
//as duplicated/re-transfer and should not be thrown out of txnetwork
func (tp *transactionPool) buildGetCommitHandler(heightRet chan<- uint64) pb.TxPreHandler {
	return pb.TxFuncAsTxPreHandler(func(tx *pb.Transaction) (*pb.Transaction, error) {
		h, _, _ := tp.ledger.GetBlockNumberByTxid(tx.GetTxid())
		heightRet <- h
		if h != 0 {
			return tx, pb.ValidateInterrupt
		}
		return tx, nil
	})
}

func (tp *transactionPool) buildCompleteTxHandler() pb.TxPreHandler {
	return pb.TxFuncAsTxPreHandler(func(tx *pb.Transaction) (*pb.Transaction, error) {
		return tp.completeTx(tx)
	})
}

func (tp *transactionPool) SetEpoch(h uint64, hh []byte) {
	tp.Lock()
	defer tp.Unlock()

	tp.currentEpoch.height, tp.currentEpoch.hash = h, hh
}

func (tp *transactionPool) CurrentEpoch() (uint64, []byte) {
	tp.RLock()
	defer tp.RUnlock()
	return tp.currentEpoch.height, tp.currentEpoch.hash
}

func (tp *transactionPool) AcquireCachesRead(peer string) txCacheRead {
	tp.RLock()
	defer tp.RUnlock()
	c, ok := tp.cCaches[peer]

	if !ok {
		logger.Errorf("Can not get reading cache of peer %s", peer)
		return txCacheRead{}
	}

	return txCacheRead{c, tp}
}

func (tp *transactionPool) AcquireCaches(peer string) *txCache {

	tp.RLock()
	c, ok := tp.cCaches[peer]
	tp.RUnlock()

	if !ok {
		c = commitData(nil)
		tp.Lock()
		defer tp.Unlock()
		tp.cCaches[peer] = c
		logger.Debugf("create new txcache for peer %s", peer)
	}
	return &txCache{txCacheRead{c, tp}, peer, nil}
}

func (tp *transactionPool) RemoveCaches(peer string) {
	tp.Lock()
	defer tp.Unlock()

	delete(tp.cCaches, peer)
}

func (tp *transactionPool) ResetLedger() {
	tp.ClearCaches()
}

func (tp *transactionPool) ClearCaches() {
	tp.Lock()
	defer tp.Unlock()

	tp.cCaches = make(map[string]commitData)
}
