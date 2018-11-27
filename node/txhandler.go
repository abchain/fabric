package node

import (
	"fmt"
	cred "github.com/abchain/fabric/core/cred"
	"github.com/abchain/fabric/core/gossip/txnetwork"
	pb "github.com/abchain/fabric/protos"
	"github.com/op/go-logging"
)

var (
	epochInterval = uint64(512)
	txlogger      = logging.MustGetLogger("txhandler")
)

const chkpcnt = 32

type txNetworkHandlerImpl struct {
	*txnetwork.TxNetworkEntry
	OnExit chan struct{}

	endorser         cred.TxEndorserFactory
	defaultTxEndorse cred.TxEndorser
	lastSeries       uint64
	lastDigest       []byte
	chkpoints        [chkpcnt]*txPoint
}

func NewTxNetworkHandler(txnet *txnetwork.TxNetworkEntry, last txPoint, endorser cred.TxEndorserFactory, endorseAttr []string) (*txNetworkHandlerImpl, error) {

	ret := new(txNetworkHandlerImpl)
	ret.TxNetworkEntry = txnet
	ret.endorser = endorser
	ret.lastSeries = last.Series
	ret.lastDigest = last.Digest

	var err error
	ret.defaultTxEndorse, err = endorser.GetEndorser(endorseAttr...)
	if err != nil {
		return nil, err
	}
	ret.OnExit = make(chan struct{})

	txlogger.Infof("Start a txnetwork handler for txnetwork at %d[%x]", ret.lastSeries, ret.lastDigest)

	return ret, nil
}

//build (complete) tx
func handleTx(lastDigest []byte, tx *pb.Transaction, endorser cred.TxEndorser) (*pb.Transaction, error) {
	tx = txnetwork.GetPrecededTx(lastDigest, tx)

	//allow non-sec usage
	if endorser != nil {
		var err error
		tx, err = endorser.EndorseTransaction(tx)
		if err != nil {
			return nil, err
		}
	}

	txdig, err := tx.Digest()
	if err != nil {
		return nil, fmt.Errorf("Can not get digest for tx [%v]: %s", tx, err)
	}
	tx.Txid = pb.TxidFromDigest(txdig)

	return tx, nil
}

func (t *txNetworkHandlerImpl) updateEpoch(chk txPoint) error {

	state := &pb.PeerTxState{Digest: chk.Digest, Num: chk.Series}
	var err error

	if t.endorser != nil {
		state, err = t.endorser.EndorsePeerState(state)
		if err != nil {
			return err
		}
	}

	return t.UpdateLocalPeer(state)
}

func (t *txNetworkHandlerImpl) HandleTxs(txs []*txnetwork.PendingTransaction) error {

	txlogger.Debugf("start handling %d txs", len(txs))
	lastDigest := t.lastDigest
	var htxs []*pb.Transaction

	var err error
	for _, tx := range txs {

		endorser := tx.GetEndorser()
		if endorser == nil {
			endorser = t.defaultTxEndorse
		} else {
			//count of txs is limited so defer should not overflow
			defer endorser.Release()
		}

		if htx, err := handleTx(lastDigest, tx.Transaction, endorser); err == nil {
			htxs = append(htxs, htx)
			lastDigest = txnetwork.GetTxDigest(htx)
			tx.Respond(&pb.Response{pb.Response_SUCCESS, []byte(htx.GetTxid())})
		} else {
			txlogger.Errorf("building complete tx fail: %s, corresponding tx skipped", err)
			tx.Respond(&pb.Response{pb.Response_FAILURE, []byte(err.Error())})
		}
	}

	if len(htxs) == 0 {
		txlogger.Warningf("can't build any tx from %d incoming pending txs", len(txs))
		return nil
	}

	lastSeries := t.lastSeries + uint64(len(htxs))
	nextChkPos := uint(t.lastSeries/uint64(txnetwork.PeerTxQueueLen())) + 1

	if err = t.UpdateLocalHotTx(&pb.HotTransactionBlock{htxs, lastSeries}); err == nil {
		t.lastSeries = lastSeries
		t.lastDigest = lastDigest
	} else {
		txlogger.Errorf("updating completed txs into network fail: %s", err)
		//TODO: though rare, should we drop the running thread? or tracking it by some ways?
		return nil
	}

	//update checkpoint and epoch
	chk := uint(t.lastSeries / uint64(txnetwork.PeerTxQueueLen()))
	if chk >= nextChkPos {
		//checkpoint current, and increase chkpoint pos
		txlogger.Infof("Chkpoint %d reach, record [%d:%x]", nextChkPos, t.lastSeries, t.lastDigest)
		t.chkpoints[chk%chkpcnt] = &txPoint{t.lastDigest, t.lastSeries}

		//when we have passed an epoch border, try to update the epoch
		if t.lastSeries > epochInterval {
			epochPoint := uint((t.lastSeries - epochInterval) / uint64(txnetwork.PeerTxQueueLen()))
			chkp := t.chkpoints[epochPoint%chkpcnt]
			//we have a wrap-up detection
			if chkp != nil && chkp.Series+epochInterval+uint64(txnetwork.PeerTxQueueLen()) > t.lastSeries {
				if err := t.updateEpoch(*chkp); err != nil {
					txlogger.Errorf("updating epoch into network fail: %s", err)
				}
			}
			t.chkpoints[epochPoint%chkpcnt] = nil
		}
	}
	return nil
}

func (t *txNetworkHandlerImpl) Release() {
	if t == nil {
		return
	} else if t.defaultTxEndorse != nil {
		t.defaultTxEndorse.Release()
	}

	close(t.OnExit)

}
