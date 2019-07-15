package txnetwork

import (
	"fmt"
	"github.com/abchain/fabric/core/gossip"
	model "github.com/abchain/fabric/core/gossip/model"
	"github.com/abchain/fabric/core/gossip/stub"
	pb "github.com/abchain/fabric/protos"
	"time"
)

const (
	EndorseVerLimit = 128
)

var PeerTemporaryUnavail = fmt.Errorf("Peer is temporary unavailable")

type peerStatus struct {
	*pb.PeerTxState
}

func (s peerStatus) To() model.VClock {
	//an "empty" status (no num and endorsement) has the lowest clock
	if s.GetNum() == 0 && len(s.Endorsement) == 0 {
		return model.BottomClock
	}

	//now the vclock is consistented with both tx series and endorsement versions
	//we also shift clock by 1 so every "valid" clock is start from 1,
	//and "unknown" is from bottomclock (respresent by 0 in protobuf-struct)
	return standardVClock(s.GetNum()*uint64(EndorseVerLimit) + uint64(s.GetEndorsementVer()) + 1)
}

func (s peerStatus) PickFrom(d_in model.VClock, u_in model.Update) (model.ScuttlebuttPeerUpdate, model.Update) {

	//a (parital) deep copy is made
	//notice we could not simply copy ret for there may be some additional field
	//defined by protobuf
	ret := new(pb.PeerTxState)
	ret.Num = s.GetNum()
	ret.Digest = s.GetDigest()
	ret.Signature = s.GetSignature()
	ret.Endorsement = s.GetEndorsement()
	ret.EndorsementVer = s.GetEndorsementVer()
	return peerStatus{ret}, u_in

}

func (s *peerStatus) Update(id string, u_in model.ScuttlebuttPeerUpdate, g_in model.ScuttlebuttStatus) error {

	u, ok := u_in.(peerStatus)
	if !ok {
		panic("Type error, not peerStatus")
	}

	g, ok := g_in.(*peersGlobal)
	if !ok {
		panic("Type error, not txNetworkGlobal")
	}

	if g.peerHandler != nil {

		if err := g.peerHandler.ValidatePeerStatus(id, u.PeerTxState); err != nil {
			if err == PeerTemporaryUnavail {
				//we could not update peer temparory, but this is not consider as an error (or blame our neighbour)
				logger.Warningf("Peer [%s] is not allowed to be update now", id)
				return nil
			}
			return fmt.Errorf("Peer [%s]'s state is invalid: %s", id, err)
		}
	} else if len(u.GetEndorsement()) == 0 {

		return fmt.Errorf("Peer [%s]'s new status has no endorsement: %v", id, u)

	}

	//scuttlebutt mode should avoiding this
	if u.GetNum() < s.GetNum() {
		panic("Wrong series, model error")
	}

	lastSeries := s.PeerTxState.GetNum()
	established := len(s.Endorsement) == 0
	if established {
		logger.Infof("We have establish new gossip peer [%s]:[%d:%x]", id, u.GetNum(), u.GetDigest())
	} else {
		//this enable local update reuse old endorsement
		if len(u.GetEndorsement()) == 0 {
			u.Endorsement = s.GetEndorsement()
		}
		logger.Infof("We have update gossip peer [%s] from %d to [%d:%x]", id, lastSeries, u.GetNum(), u.GetDigest())
	}

	s.PeerTxState = u.PeerTxState

	g.TouchPeer(id, s.PeerTxState)
	g.network.handleUpdate(id, established)
	return nil
}

type peerStatusItem struct {
	peerId string
	*pb.PeerTxState
	lastAccess time.Time
}

//txnetworkglobal manage all the peers across whole networks (mutiple peers)
type peersGlobal struct {
	network *txNetworkGlobal
	*txNetworkPeers
}

func (*peersGlobal) GenDigest() model.Digest                { return nil }
func (*peersGlobal) MakeUpdate(_ model.Digest) model.Update { return nil }
func (*peersGlobal) Update(_ model.Update) error            { return nil }
func (*peersGlobal) MissedUpdate(id string, u model.ScuttlebuttPeerUpdate) error {
	logger.Warningf("peer receive a missed update from [%s]: %v", id, u)
	return nil
}

func (g *peersGlobal) NewPeer(id string) model.ScuttlebuttPeerStatus {

	ret, rmids := g.AddNewPeer(id)

	if rmids != nil {
		g.network.handleEvict(rmids)
	}

	return ret
}

func (g *peersGlobal) RemovePeer(id string, _ model.ScuttlebuttPeerStatus) {

	ok := g.txNetworkPeers.RemovePeer(id)
	logger.Infof("Request require peer [%s] to be remove (done: %v)", id, ok)

	if ok {
		g.network.txPool.RemoveCaches(id)
		g.network.handleEvict([]string{id})
	}
}

type globalCat struct {
	policy gossip.CatalogPolicies
}

func init() {
	stub.RegisterCat = append(stub.RegisterCat, initNetworkStatus)
}

func initNetworkStatus(stub *gossip.GossipStub) {

	peerG := new(peersGlobal)
	peerG.network = getTxNetwork(stub)
	peerG.txNetworkPeers = peerG.network.peers

	selfstatus := model.NewScuttlebuttStatus(peerG)
	m := model.NewGossipModel(selfstatus)
	setself := func(newID string, state *pb.PeerTxState) {
		m.Lock()
		defer m.Unlock()
		selfstatus.SetSelfPeer(newID, &peerStatus{state})
		logger.Infof("TXPeers cat reset self peer to %s", newID)
	}

	//use extended mode of scuttlebutt scheme, see code and wiki
	selfstatus.Extended = true
	if selfs, id := peerG.QuerySelf(); selfs != nil {
		setself(id, selfs)
	}

	globalcat := new(globalCat)
	globalcat.policy = gossip.NewCatalogPolicyDefault()

	h := gossip.NewCatalogHandlerImpl(stub.GetSStub(),
		stub.GetStubContext(), globalcat, m)
	stub.AddCatalogHandler(h)
	stub.SubScribeNewPeerNotify(h)
	peerG.network.RegSetSelfPeer(setself)
}

const (
	globalCatName = "global"
)

//Implement for CatalogHelper
func (c *globalCat) Name() string                        { return globalCatName }
func (c *globalCat) GetPolicies() gossip.CatalogPolicies { return c.policy }

func (c *globalCat) TransDigestToPb(d_in model.Digest) *pb.GossipMsg_Digest {
	d, ok := d_in.(model.ScuttlebuttDigest)
	if !ok {
		panic("Type error, not ScuttlebuttDigest")
	}
	return toPbDigestStd(d, nil)
}

func (c *globalCat) TransPbToDigest(msg *pb.GossipMsg_Digest) model.Digest {
	//	logger.Debugf("peer's digest msg: %v", msg)
	return parsePbDigestStd(msg, nil)
}

func (c *globalCat) TransUpdateToPb(cpo gossip.CatalogPeerPolicies, u_in model.Update) *pb.GossipMsg_Update {
	u, ok := u_in.(model.ScuttlebuttUpdate)

	if !ok {
		panic("Type error, not ScuttlebuttUpdate")
	}

	msg := &pb.Gossip_TxState{make(map[string]*pb.PeerTxState)}

	for _, iu_in := range u.PeerUpdate() {
		iu, ok := iu_in.U.(peerStatus)
		if !ok {
			panic("Type error, not peerTxStatusUpdate")
		}
		msg.Txs[iu_in.Id] = iu.PeerTxState
	}

	//	logger.Debugf("peer's update msg: %v", msg)
	return &pb.GossipMsg_Update{U: &pb.GossipMsg_Update_State{State: msg}}
}

func (c *globalCat) TransPbToUpdate(cpo gossip.CatalogPeerPolicies, msg_in *pb.GossipMsg_Update) (model.Update, error) {
	msg := msg_in.GetState()
	if msg == nil {
		return nil, fmt.Errorf("message error, not Gossip_TxState")
	}

	u := model.NewscuttlebuttUpdate(nil)
	if len(msg.Txs) == 0 {
		return nil, nil
	}
	//detected a malicious behavior
	if _, ok := msg.Txs[""]; ok {
		return nil, fmt.Errorf("Peer try to update a invalid id (self)")
	}

	for id, iu := range msg.Txs {
		u.UpdatePeer(id, peerStatus{iu})
	}

	return u, nil
}
