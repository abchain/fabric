package txnetwork

import (
	"fmt"
	cred "github.com/abchain/fabric/core/cred"
	"github.com/abchain/fabric/core/gossip"
	model "github.com/abchain/fabric/core/gossip/model"
	"github.com/abchain/fabric/core/gossip/stub"
	"github.com/abchain/fabric/core/ledger"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
)

type TxNetworkEntry struct {
	*txNetworkEntry
	net         *txNetworkGlobal
	stub        *gossip.GossipStub
	doPauseflag bool
}

//Init method can be only executed before gossip network is running, to override
//the default settings
func (e *TxNetworkEntry) InitLedger(l *ledger.Ledger) {
	logger.Debugf("txnetwork has set new ledger %v", l)
	e.net.txPool.resetLedger(l)
}

//[optional] set the credential for peer in txnetwork
func (e *TxNetworkEntry) InitCred(v cred.TxHandlerFactory) {
	logger.Debugf("txnetwork has set new txhandler %v(%T)", v, v)
	e.net.peers.peerHandler = v
	e.net.txPool.preValidator = v

	if v != nil {
		v.SetIdConverter(ToStringId)
	}

}

//set the output for tx received in txnetwork
func (e *TxNetworkEntry) InitTerminal(t pb.TxPreHandler) {
	logger.Debugf("txnetwork has set new tx terminal %v(%T)", t, t)
	e.net.txPool.txTerminal = t
}

func clonePeerTxState(s *pb.PeerTxState) *pb.PeerTxState {
	cpy := new(pb.PeerTxState)
	*cpy = *s
	return cpy
}

func (e *TxNetworkEntry) ResetPeerSimple(id []byte) error {

	if err := e.net.peers.ChangeSelf(id); err != nil {
		return err
	}

	selfState, selfId := e.net.peers.QuerySelf()
	logger.Debugf("Self peer state after change is: %v", selfState)
	e.net.handleSetSelf(selfId, selfState)
	//add a mark to indicate the peer is endorsered
	selfState = clonePeerTxState(selfState)
	selfState.Endorsement = []byte{1}
	e.catalogHandlerUpdateLocal(globalCatName, peerStatus{selfState}, nil)
	logger.Infof("Self peer reseted to [%s] in simple mode", selfId)

	return nil
}

func (e *TxNetworkEntry) ResetPeer(endorser cred.TxEndorserFactory) error {

	id := endorser.EndorserId()
	err := e.net.peers.ChangeSelf(id)

	var selfState *pb.PeerTxState
	if err == nil {
		//self id is changed

		//we have a inconsistent status here (the self is updated in global network
		//while is not yet in each scuttlebutt model), but this should be all right
		//as long as the model is not updated self status, which is controllable
		//by the caller of SetSelf

		var selfId string
		//obtain self state after chaning
		selfState, selfId = e.net.peers.QuerySelf()

		//sanity check
		if selfState == nil {
			panic("state must be obtained succefully after a calling of change self")
		}
		//we also update scuttlebutt model before the state is endorsed, now we have
		//a new self-peer, but not endorsed so do not propagate it on the network
		e.net.handleSetSelf(selfId, selfState)
		logger.Infof("Self peer reseted to [%s]", selfId)

	} else if err != SelfIDNotChange {
		return err
	} else {
		selfState, _ = e.net.peers.QuerySelf()
	}

	//give a new endorsement for the selfState, so it was ready for broadcasting
	selfState = clonePeerTxState(selfState)
	selfState, err = endorser.EndorsePeerState(selfState)
	if err != nil {
		return err
	}

	//everything is done, now disclose our new peer to the whole network
	return e.catalogHandlerUpdateLocal(globalCatName, peerStatus{selfState}, nil)
}

func (e *TxNetworkEntry) catalogHandlerUpdate(catName string, ug model.Update) error {
	cat := e.stub.GetCatalogHandler(catName)
	if cat == nil {
		return fmt.Errorf("Can't not found corresponding cataloghandler [%s]", catName)
	}

	if err := cat.Model().RecvUpdate(ug); err != nil {
		return err
	} else {
		//notify our peer is updated
		cat.SelfUpdate()
		return nil
	}
}

func (e *TxNetworkEntry) catalogHandlerUpdateLocal(catName string, u model.ScuttlebuttPeerUpdate, ug model.Update) error {
	cat := e.stub.GetCatalogHandler(catName)
	if cat == nil {
		return fmt.Errorf("Can't not found corresponding cataloghandler [%s]", catName)
	}

	selfUpdate := model.NewscuttlebuttUpdate(ug)
	selfUpdate.UpdateLocal(u)

	if err := cat.Model().RecvUpdate(selfUpdate); err != nil {
		return err
	} else {
		//notify our peer is updated
		cat.SelfUpdate()
		return nil
	}
}

func (e *TxNetworkEntry) UpdateLocalPeer(s *pb.PeerTxState) error {
	return e.catalogHandlerUpdateLocal(globalCatName, peerStatus{s}, nil)
}

func (e *TxNetworkEntry) UpdateLocalHotTx(txs *pb.HotTransactionBlock) error {
	return e.catalogHandlerUpdateLocal(hotTxCatName, txPeerUpdate{txs}, nil)
}

func (e *TxNetworkEntry) GetPeerStatus() (*pb.PeerTxState, string) {
	return e.net.peers.QuerySelf()
}

func (e *TxNetworkEntry) GetTxStatus(id string) (uint64, []byte) {
	if e.net.selfTxProcess == nil {
		return 0, nil
	}
	return e.net.selfTxProcess(id)
}

var catsToReferOnPaused = []string{globalCatName, hotTxCatName}

func (e *TxNetworkEntry) PauseTxNetwork(dopause bool) {

	for _, catName := range catsToReferOnPaused {
		cat := e.stub.GetCatalogHandler(catName)
		if cat == nil {
			logger.Errorf("Can't not found corresponding cataloghandler [%s]", catName)
			continue
		}
		cat.Model().TriggerReadOnlyMode(dopause)
		if !dopause && e.doPauseflag {
			//on resume, trigger a update process
			cat.SelfUpdate()
		}
		e.doPauseflag = dopause
	}
}

func GetNetworkEntry(stub *gossip.GossipStub) *TxNetworkEntry {

	global.Lock()
	defer global.Unlock()
	e, ok := global.entries[stub]
	if !ok {
		panic("It was impossible that entry is not existed: use a gossipstub not created by stub?")
	}
	return e
}

func init() {
	stub.RegisterCat = append(stub.RegisterCat, initTxnetworkEntrance)
}

func initTxnetworkEntrance(stub *gossip.GossipStub) {

	self := &TxNetworkEntry{
		txNetworkEntry: newTxNetworkEntry(),
		net:            getTxNetwork(stub),
		stub:           stub,
	}

	global.Lock()
	defer global.Unlock()
	global.entries[stub] = self
}

type TxNetworkHandler interface {
	HandleTxs(tx []*PendingTransaction) error
	Release()
}

type txNetworkEntry struct {
	source chan *PendingTransaction
}

const (
	maxOutputBatch = 16
)

func newTxNetworkEntry() *txNetworkEntry {

	return &txNetworkEntry{
		source: make(chan *PendingTransaction, maxOutputBatch*3),
	}
}

func (e *txNetworkEntry) worker(ctx context.Context, h TxNetworkHandler) {

	var txBuffer [maxOutputBatch]*PendingTransaction
	var err error
	txs := txBuffer[:0]

	logger.Infof("Start a worker for txnetwork entry with handler <%T:%p>", h, h)
	defer func() {
		logger.Infof("End the worker for txnetwork entry with handler <%p>", h)
		h.Release()
	}()
	for {

		if err != nil {
			logger.Error("Tx handling encounter failure:", err)
			return
		}

		if len(txs) == 0 {
			//wait for an item
			select {
			case item := <-e.source:
				txs = append(txs, item)
			case <-ctx.Done():
				return
			}
		} else if len(txs) >= maxOutputBatch {
			err = h.HandleTxs(txs)
			txs = txBuffer[:0]
		} else {
			//wait more item or handle we have
			select {
			case item := <-e.source:
				txs = append(txs, item)
			case <-ctx.Done():
				return
			default:
				err = h.HandleTxs(txs)
				txs = txBuffer[:0]
			}
		}

	}
}

func (e *txNetworkEntry) Start(ctx context.Context, h TxNetworkHandler) {

	go e.worker(ctx, h)
}

type PendingTransaction struct {
	*pb.Transaction
	endorser cred.TxEndorser
	resp     chan *pb.Response
}

func (t *PendingTransaction) GetEndorser() cred.TxEndorser {
	return t.endorser
}

func (t *PendingTransaction) Respond(resp *pb.Response) {
	if t.resp != nil {
		t.resp <- resp
	}
}

func (e *txNetworkEntry) broadcast(ptx *PendingTransaction) error {
	if e == nil {
		return fmt.Errorf("txNetwork not init yet")
	}

	select {
	case e.source <- ptx:
		return nil
	default:
		return fmt.Errorf("Buffer full, can not write more")
	}
}

func (e *txNetworkEntry) BroadCastTransaction(tx *pb.Transaction, endorser cred.TxEndorser) error {
	return e.broadcast(&PendingTransaction{tx, endorser, nil})
}

func (e *txNetworkEntry) ExecuteTransaction(ctx context.Context, tx *pb.Transaction, endorser cred.TxEndorser) *pb.Response {

	resp := make(chan *pb.Response)
	ret := e.broadcast(&PendingTransaction{tx, endorser, resp})

	if ret == nil {
		select {
		case ret := <-resp:
			return ret
		case <-ctx.Done():
			return &pb.Response{Status: pb.Response_FAILURE,
				Msg: []byte(fmt.Sprintf("%s", ctx.Err()))}
		}
	} else {
		return &pb.Response{Status: pb.Response_FAILURE,
			Msg: []byte(fmt.Sprintf("Exec transaction fail: %s", ret))}
	}
}
