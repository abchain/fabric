package txnetwork

import (
	"bytes"
	"fmt"
	"github.com/abchain/fabric/core/gossip"
	model "github.com/abchain/fabric/core/gossip/model"
	"github.com/abchain/fabric/core/ledger"
	pb "github.com/abchain/fabric/protos"
	proto "github.com/golang/protobuf/proto"
)

type txMemPoolItem struct {
	digest       []byte
	digestSeries uint64
	txid         string
	//	tx           *pb.Transaction //cache of the tx (may just the txid)
	//	committedH   uint64          //0 means not commited

	//so we have a simple list structure
	next *txMemPoolItem
}

func createPeerTxItem(s *pb.PeerTxState) *txMemPoolItem {
	return &txMemPoolItem{
		digest:       s.GetDigest(),
		digestSeries: s.GetNum(),
	}
}

//a item is cloned without next
func (t *txMemPoolItem) clone() *txMemPoolItem {
	n := new(txMemPoolItem)
	*n = *t
	n.next = nil
	return n
}

type peerTxs struct {
	head *txMemPoolItem
	last *txMemPoolItem
}

const (
	TxDigestVerifyLen = 16
	failDig           = "__FAILTXDIGEST__"
)

func getTxDigest(tx *pb.Transaction) []byte {
	dig, err := tx.Digest()
	if err != nil {
		dig = []byte(failDig)
	}

	if len(dig) < TxDigestVerifyLen {
		panic("Wrong code generate digest less than 16 bytes [128bit]")
	}

	return dig[:TxDigestVerifyLen]
}

//return whether tx is match to the digest
func txIsMatch(digest []byte, tx *pb.Transaction) bool {

	return bytes.Compare(digest, getTxDigest(tx)) == 0
}

//return whether tx2 is precede of the digest
func txIsPrecede(digest []byte, tx2 *pb.Transaction) bool {
	n := tx2.GetNonce()
	if len(n) < TxDigestVerifyLen {
		return false
	}
	return bytes.Compare(digest, n[:TxDigestVerifyLen]) == 0
}

func (p *peerTxs) firstSeries() uint64 {
	if p == nil || p.head == nil {
		return 0
	} else {
		return p.head.digestSeries
	}
}

func (p *peerTxs) lastSeries() uint64 {
	if p == nil || p.last == nil {
		return 0
	} else {
		return p.last.digestSeries
	}
}

func (p *peerTxs) inRange(n uint64) bool {

	if p.head == nil {
		return false
	}

	return p.head.digestSeries <= n && p.last.digestSeries >= n

}

func (p *peerTxs) concat(s *peerTxs) error {

	if p.head == nil {
		p.head = s.head
		p.last = s.last
		return nil
	}

	if s.head.digestSeries != p.lastSeries()+1 {
		return fmt.Errorf("Wrong next series: %d", s.head.digestSeries)
	}

	p.last.next = s.head
	p.last = s.last
	return nil
}

//have extra cost and we should avoid using it
func (p *peerTxs) merge(s *peerTxs) (*txMemPoolItem, error) {

	if p.head == nil {
		return s.head, p.concat(s)
	}

	if !s.inRange(p.last.digestSeries + 1) {
		//nothing to merge, give up
		return nil, nil
	}

	//scan txs ...
	for beg := s.head; beg != nil; beg = beg.next {
		if beg.digestSeries > p.last.digestSeries {
			return beg, p.concat(&peerTxs{beg, s.last})
		}
	}

	return nil, nil
}

func (p *peerTxs) fetch(d uint64, beg *txMemPoolItem) *peerTxs {

	if beg == nil {
		beg = p.head
	}

	for ; beg != nil; beg = beg.next {
		if beg.digestSeries == d {
			return &peerTxs{head: beg, last: p.last}
		}
	}

	return nil

}

type txPoolGlobalUpdateOut struct {
	*txPoolGlobal
	epoch uint64
}

func (txPoolGlobalUpdateOut) Gossip_IsUpdateIn() bool { return false }

type txPoolGlobalUpdate struct {
	gossip.CatalogPeerPolicies
}

func (txPoolGlobalUpdate) Gossip_IsUpdateIn() bool { return true }

type txPoolGlobal struct {
	*transactionPool
	//	ind        map[string]*txMemPoolItem
	network    *txNetworkGlobal
	currentCpo gossip.CatalogPeerPolicies
}

type txPoolGlobalDigest struct {
	epoch []byte
}

func (g *txPoolGlobal) GenDigest() model.Digest {

	epoch, err := g.ledger.GetCurrentStateHash()
	if err != nil {
		logger.Errorf("Could not get epoch: %s", err)
		return nil
	}

	return txPoolGlobalDigest{epoch}
}
func (g *txPoolGlobal) MakeUpdate(d_in model.Digest) model.Update {

	if d_in == nil {
		return txPoolGlobalUpdateOut{g, 0}
	}

	d, ok := d_in.(*pb.Gossip_Digest)

	if !ok {
		panic("Type error, not Gossip_Digest")
	}

	epochH, err := g.ledger.GetBlockNumberByState(d.GetEpoch())
	if err != nil {
		logger.Warning("Get epoch height fail, encode all transactions:", err)
		epochH = 0
	}

	return txPoolGlobalUpdateOut{g, epochH}
}

func (g *txPoolGlobal) Update(u_in model.Update) error {

	if u_in == nil {
		return nil
	}

	switch u := u_in.(type) {
	case txPoolGlobalUpdate:
		g.currentCpo = u.CatalogPeerPolicies
	default:
		return fmt.Errorf("Type error, not expected update type")
	}

	return nil
}

func (g *txPoolGlobal) NewPeer(id string) model.ScuttlebuttPeerStatus {

	//check if we have known this peer
	peerStatus := g.network.QueryPeer(id)
	if peerStatus == nil {
		logger.Infof("We hear of peer %s before it is known by us", id)
		return nil
	}

	logger.Debugf("txpool now know new peer [%s]", id)
	ret := new(peerTxMemPool)
	ret.reset(createPeerTxItem(peerStatus))
	return ret
}

func (g *txPoolGlobal) MissedUpdate(string, model.ScuttlebuttPeerUpdate) error {
	return nil
}

func (g *txPoolGlobal) RemovePeer(id string, s_in model.ScuttlebuttPeerStatus) {
	if s_in == nil {
		return
	}
	_, ok := s_in.(*peerTxMemPool)
	if !ok {
		panic("Type error, not peerTxMemPool")
	}

	g.RemoveCache(id)

	logger.Infof("Peer %s is removed", id)
}

func txToDigestState(tx *pb.Transaction) []byte {
	return tx.GetNonce()
}

//update structure used for recving
type txPeerUpdate struct {
	*pb.HotTransactionBlock
}

func (u txPeerUpdate) To() model.VClock {
	if u.HotTransactionBlock == nil {
		return model.BottomClock
	}

	return standardVClock(u.BeginSeries + uint64(len(u.Transactions)) - 1)
}

func getLiteTx(tx *pb.Transaction) *pb.Transaction {
	return &pb.Transaction{Txid: tx.GetTxid()}
}

func isLiteTx(tx *pb.Transaction) bool {
	return tx.GetNonce() == nil
}

func (u txPeerUpdate) fromTxs(s *peerTxs, epochH uint64, cache txCache) {

	if s == nil {
		return
	}

	u.BeginSeries = s.head.digestSeries

	for beg := s.head; beg != nil; beg = beg.next {
		tx, commitedH := cache.GetTx(beg.digestSeries, beg.txid)
		if tx == nil {
			//we have something wrong and have to end here ...
			logger.Errorf("Can not find Tx %s in peer cache", beg.txid)
			return
		}
		if commitedH != 0 && commitedH <= epochH {
			u.Transactions = append(u.Transactions, getLiteTx(tx))
		} else {
			u.Transactions = append(u.Transactions, tx)
		}
	}
}

func (u txPeerUpdate) getRef(refSeries uint64) txPeerUpdate {

	if u.BeginSeries < refSeries {
		//possible panic if headpos exceed len of u.Transactions ?
		//scuttlebutt scheme will check To() first and avoid this case
		return txPeerUpdate{&pb.HotTransactionBlock{u.Transactions[int(refSeries-u.BeginSeries):], refSeries}}
	} else {
		//start from head
		return u
	}
}

func (u txPeerUpdate) completeTxs(l *ledger.Ledger, h TxPreHandler) (ret txPeerUpdate, err error) {
	ret.HotTransactionBlock = &pb.HotTransactionBlock{
		Transactions: make([]*pb.Transaction, len(u.Transactions)),
		BeginSeries:  u.BeginSeries,
	}

	for i, tx := range u.Transactions {
		if isLiteTx(tx) {
			tx, err = l.GetTransactionByID(tx.GetTxid())
			if err != nil {
				err = fmt.Errorf("Checking tx from db fail: %s", err)
				return
			} else if tx == nil {
				err = fmt.Errorf("update give uncommited transactions")
				return
			}
		} else if h != nil {
			tx, err = h.TransactionPreValidation(tx)
			if err != nil {
				err = fmt.Errorf("Verify tx fail: %s", err)
				return
			}
		}
		ret.Transactions[i] = tx
	}

	return
}

func (u txPeerUpdate) toTxs(last *txMemPoolItem) (*peerTxs, error) {

	if len(u.Transactions) == 0 {
		return nil, nil
	}

	head := &txMemPoolItem{
		//		tx:           txs.Transactions[0],
		//		digest:       txToDigestState(txs.Transactions[0]),
		digestSeries: u.BeginSeries,
	}

	current := head

	//construct a peerTxMemPool from txs, and do verification
	for _, tx := range u.Transactions {

		if last != nil && !txIsPrecede(last.digest, tx) {
			return nil, fmt.Errorf("update have invalid transactions chain for tx at %d", last.digestSeries+1)
		}

		last = current
		current.txid = tx.GetTxid()
		current.digest = getTxDigest(tx)

		current = &txMemPoolItem{digestSeries: current.digestSeries + 1}
		last.next = current
	}
	last.next = nil //seal the tail

	return &peerTxs{head, last}, nil
}

//so we make two indexes for pooling txs: the global one (by txid) and a per-peer
//jumping list (by seqnum) with [1/jumplistInterval] size of the global indexs,
//and finally we have a 100/jumplistInterval % overhead
const (
	jumplistInterval = uint64(8)
)

type peerTxMemPool struct {
	*peerTxs
	//index help us to seek by a txid, it do not need to consensus with the size
	//field in peerTxs
	jlindex map[uint64]*txMemPoolItem
}

func (p *peerTxMemPool) reset(txbeg *txMemPoolItem) {
	p.peerTxs = &peerTxs{txbeg, nil}
	p.jlindex = make(map[uint64]*txMemPoolItem)

	if txbeg != nil {
		for ; txbeg != nil; txbeg = txbeg.next {
			if txbeg.digestSeries == (txbeg.digestSeries/jumplistInterval)*jumplistInterval {
				p.jlindex[txbeg.digestSeries/jumplistInterval] = txbeg
			}
			p.last = txbeg
		}

	}
}

func (p *peerTxMemPool) To() model.VClock {

	if p.lastSeries() == 0 {
		return model.BottomClock
	}

	return standardVClock(p.lastSeries())
}

func (p *peerTxMemPool) PickFrom(id string, d_in model.VClock, gu_in model.Update) (model.ScuttlebuttPeerUpdate, model.Update) {

	d := toStandardVClock(d_in)

	gu, ok := gu_in.(txPoolGlobalUpdateOut)
	if !ok {
		panic("Type error, not txPoolGlobalUpdateOut")
	}

	expectedH := uint64(d) + 1
	ret := txPeerUpdate{new(pb.HotTransactionBlock)}
	//logger.Debugf("pick peer %s at height %d with global height %d", p.peerId, expectedH, uint64(gu))

	peerCache := gu.AcquireCache(id, p.firstSeries(), p.lastSeries()+1)

	if !p.inRange(expectedH) {
		//we have a too up-to-date range, so we return a single record to
		//remind far-end they are late
		nh := p.head.clone()
		ret.fromTxs(&peerTxs{nh, nh}, gu.epoch, peerCache)
	} else {
		ret.fromTxs(p.fetch(expectedH, p.jlindex[uint64(d)/jumplistInterval]), uint64(gu.epoch), peerCache)
	}

	return ret, gu_in
}

func purgePool(p *peerTxMemPool, purgeto uint64) {
	//purge the outdate part
	//for zero, we just get UINT64_MAX and no index
	preserved := p.fetch(purgeto, p.jlindex[purgeto/jumplistInterval-1])
	if preserved == nil {
		logger.Warningf("got empty preserved, wrong purge request:", purgeto)
		return
	}

	//also the jumping index must be purged
	for i := p.head.digestSeries / jumplistInterval; i < purgeto/jumplistInterval; i++ {
		delete(p.jlindex, i)
	}

	//handling the "edge jumpling index"
	if purgeto/jumplistInterval*jumplistInterval < purgeto {
		delete(p.jlindex, purgeto/jumplistInterval)
	}

	p.peerTxs = preserved
}

func (p *peerTxMemPool) purge(id string, purgeto uint64, g *txPoolGlobal) {

	//never purge at begin
	if p.head == nil || purgeto <= p.head.digestSeries {
		return
	}

	logger.Debugf("peer %s try to prune cache from %d to %d", id, p.firstSeries(), purgeto)
	cache := g.AcquireCache(id, p.firstSeries(), p.lastSeries()+1)
	cache.PurneCache(purgeto)

	purgePool(p, purgeto)

}

func (p *peerTxMemPool) handlePeerCoVar(id string, peerStatus *pb.PeerTxState, g *txPoolGlobal) error {

	to := peerStatus.GetNum()
	if to <= p.firstSeries() {

		return fmt.Errorf("covar occurs on %s with a peer status %d earlier than current (%d)",
			id, to, p.firstSeries())
	}

	p.purge(id, to, g)
	return nil
}

func (p *peerTxMemPool) handlePeerUpdate(u txPeerUpdate, id string, peerStatus *pb.PeerTxState, g *txPoolGlobal) error {
	logger.Debugf("peer %s try to updated %d incoming txs from series %d", id, len(u.Transactions), u.BeginSeries)

	var err error
	u, err = u.getRef(p.lastSeries()+1).completeTxs(g.ledger, g.preH)
	if err != nil {
		return err
	}

	inTxs, err := u.toTxs(p.last)
	if err != nil {
		//NEVER report this as the fraud of far-end because the original peer can build
		//branch results (by update peer status or sending branched tx chains) deliberately
		//we just skip the update data
		logger.Warningf("***** PROBLEM ****** peer %s update found branched incoming", id)
		return nil
	} else if inTxs == nil {
		logger.Infof("peer %s update nothing in %d txs and quit", id, len(u.Transactions))
		return nil
	}

	g.AcquireCache(id, p.firstSeries(), p.lastSeries()+1).AddTxs(u.Transactions, id == "")

	//sanity check
	err = p.concat(inTxs)
	if err != nil {
		panic("toTxs method should has verified everything, wrong code")
	}

	var mergeCnt int
	//we need to construct the index ...
	for beg := inTxs.head; beg != nil; beg = beg.next {

		if beg.digestSeries == (beg.digestSeries/jumplistInterval)*jumplistInterval {
			p.jlindex[beg.digestSeries/jumplistInterval] = beg
		}
		mergeCnt++

	}

	logger.Debugf("peer %s have updated %d txs", id, mergeCnt)

	//finally we handle the case if pool's cache is overflowed
	if int(p.lastSeries()-p.firstSeries())+1 > PeerTxQueueLimit() {
		to := p.lastSeries() - uint64(PeerTxQueueLimit()) + 1
		logger.Warning("peer %s's cache has reach limit and we have to prune it to %d", to)
		//notice, the cache has been full so we do not need to prune it
		//(the older part has been overwritten)
		purgePool(p, to)
	}

	// var mergeW uint
	// if len(u.Transactions) < int(cat_hottx_one_merge_weight) {
	// 	mergeW = uint(len(u.Transactions))
	// } else {
	// 	mergeW = cat_hottx_one_merge_weight
	// }
	// //scoring the peer (how many new txs have the update provided )
	// g.currentCpo.ScoringPeer(mergeCnt*100/len(u.Transactions), mergeW)

	return nil
}

// there are too many ways that a far-end can waste our bandwidth (and
// limited computational cost) without provding useful information,
// for example, fill some update under un-existed peer name
// so we given up scoring the peer until we have more solid process
// to verify the correctness of data from far-end
func (p *peerTxMemPool) Update(id string, u_in model.ScuttlebuttPeerUpdate, g_in model.ScuttlebuttStatus) error {

	if u_in == nil {
		return nil
	}

	g, ok := g_in.(*txPoolGlobal)
	if !ok {
		panic("Type error, not txPoolGlobal")
	}

	//checkout global status
	var peerStatus *pb.PeerTxState
	if id == "" {
		peerStatus = g.network.QuerySelf()
	} else {
		peerStatus = g.network.QueryPeer(id)
		if peerStatus == nil {
			//boom ..., but it may be caused by an stale peer-removing so not
			//consider as error
			logger.Warningf("Unknown status in global for peer %s", id)
			return nil
		}
	}

	switch u := u_in.(type) {
	case txPeerUpdate:
		return p.handlePeerUpdate(u, id, peerStatus, g)
	case coVarUpdate:
		return p.handlePeerCoVar(id, peerStatus, g)
	default:
		return fmt.Errorf("Type error, not expected update [%v]", u_in)
	}

}

type hotTxCat struct {
	policy gossip.CatalogPolicies
}

func init() {
	gossip.RegisterCat = append(gossip.RegisterCat, initHotTx)
}

func initHotTx(stub *gossip.GossipStub) {

	l, err := ledger.GetLedger()
	if err != nil {
		logger.Errorf("Get ledger fail [%s], could not create hotx tx catalogy", err)
		return
	}

	txglobal := new(txPoolGlobal)
	//	txglobal.ind = make(map[string]*txMemPoolItem)
	txglobal.network = global.CreateNetwork(stub)
	txglobal.transactionPool = newTransactionPool(l)
	txglobal.preH = stub.GetSecurity().GetSecHelper()
	if txglobal.preH == nil {
		logger.Warning("Create txnetwork without security handler")
	}

	hotTx := new(hotTxCat)
	hotTx.policy = gossip.NewCatalogPolicyDefault()

	selfStatus := model.NewScuttlebuttStatus(txglobal)

	self := &peerTxMemPool{}
	self.reset(createPeerTxItem(txglobal.network.QuerySelf()))

	selfStatus.SetSelfPeer(txglobal.network.selfId, self)

	m := model.NewGossipModel(selfStatus)

	stub.AddCatalogHandler(hotTxCatName,
		gossip.NewCatalogHandlerImpl(stub.GetSStub(),
			stub.GetStubContext(), hotTx, m))

	txglobal.network.RegUpdateNotify(standardUpdateFunc(hotTxCatName, m))
	txglobal.network.RegEvictNotify(standardEvictFunc(hotTxCatName, m))
}

const (
	hotTxCatName = "openedTx"
)

//Implement for CatalogHelper
func (c *hotTxCat) Name() string                        { return hotTxCatName }
func (c *hotTxCat) GetPolicies() gossip.CatalogPolicies { return c.policy }

func (c *hotTxCat) TransDigestToPb(d_in model.Digest) *pb.Gossip_Digest {

	d, ok := d_in.(model.ScuttlebuttDigest)
	if !ok {
		panic("Type error, not ScuttlebuttDigest")
	}

	gd := d.GlobalDigest().(txPoolGlobalDigest)
	return toPbDigestStd(d, gd.epoch)

}

func (c *hotTxCat) TransPbToDigest(msg *pb.Gossip_Digest) model.Digest {

	return parsePbDigestStd(msg, msg)

}

func (c *hotTxCat) UpdateMessage() proto.Message { return new(pb.Gossip_Tx) }

func (c *hotTxCat) EncodeUpdate(cpo gossip.CatalogPeerPolicies, u_in model.Update, msg_in proto.Message) proto.Message {

	u, ok := u_in.(model.ScuttlebuttUpdate)

	if !ok {
		panic("Type error, not ScuttlebuttUpdate")
	}

	//global part is not need to be encoded ...
	//gu, ok := u.GlobalUpdate().(txPoolGlobalUpdateOut)

	// if !ok {
	// 	panic("Type error, not txPoolGlobalUpdateOut")
	// }

	msg, ok := msg_in.(*pb.Gossip_Tx)
	if !ok {
		panic("Type error, not Gossip_Tx")
	}

	msg.Txs = make(map[string]*pb.HotTransactionBlock)

	//encode txs
	//TODO: if cpo is availiable and quota is limited, cut some data
	for id, pu_in := range u.PeerUpdate() {
		pu, ok := pu_in.(txPeerUpdate)
		if !ok {
			panic("Type error, not peerTxs")
		}

		msg.Txs[id] = pu.HotTransactionBlock
	}

	return msg
}

func (c *hotTxCat) DecodeUpdate(cpo gossip.CatalogPeerPolicies, msg_in proto.Message) (model.Update, error) {

	msg, ok := msg_in.(*pb.Gossip_Tx)
	if !ok {
		panic("Type error, not Gossip_Tx")
	}

	//can return null data
	if len(msg.Txs) == 0 {
		return nil, nil
	}

	u := model.NewscuttlebuttUpdate(txPoolGlobalUpdate{cpo})

	for id, txs := range msg.Txs {
		u.UpdatePeer(id, txPeerUpdate{txs})
	}

	return u, nil
}

func UpdateLocalHotTx(stub *gossip.GossipStub, txs *pb.HotTransactionBlock) error {
	hotcat := stub.GetCatalogHandler(hotTxCatName)
	if hotcat == nil {
		return fmt.Errorf("Can't not found corresponding hottx catalogHandler")
	}

	selfUpdate := model.NewscuttlebuttUpdate(nil)
	selfUpdate.UpdateLocal(txPeerUpdate{txs})

	if err := hotcat.Model().Update(selfUpdate); err != nil {
		return err
	} else {
		//notify our peer is updated
		hotcat.SelfUpdate()
		return nil
	}
}
