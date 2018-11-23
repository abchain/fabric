package node

/*

	YA-fabric: Peer Engine:

	This is an aggregation of required features for a system-chaincode working
	for its purpose (in most case for consensus), a peer Engine include:

	* A module implement for acting as a peer in the P2P network
	* A complete txnetwork and expose its entry
	* A ledger for tracing the state and history of txnetwork
	* A state syncing module
	* Credential module which is effective in tx handling and broadcasting

*/

import (
	cred "github.com/abchain/fabric/core/cred"
	"github.com/abchain/fabric/core/gossip/txnetwork"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/peer"
	"github.com/abchain/fabric/events/litekfk"
	"github.com/op/go-logging"
)

var (
	logger = logging.MustGetLogger("engine")
)

type PeerEngine struct {
	*cred.Credentials
	*txnetwork.TxNetworkEntry
	peer.Peer

	//all the received transactions can be read out from different topic,
	//according to the configuration in transation filter
	TxTopic map[string]litekfk.Topic

	//don't access ledger from PeerEngine, visit it in NodeEngine instead
	ledger   *ledger.Ledger
	srvPoint *servicePoint

	//run-time vars
	defaultAttr []string
	lastID      string
	lastCache   txPoint
	runStatus   chan error
}

/*
	node engine intregated mutiple PeerEngine and ledgers, system should access
	ledger here
*/
type NodeEngine struct {
	Ledgers map[string]*ledger.Ledger
	Peers   map[string]*PeerEngine
}

func (ne *NodeEngine) DefaultLedger() *ledger.Ledger {
	return ne.Ledgers[""]
}

func (ne *NodeEngine) DefaultPeer() *PeerEngine {
	return ne.Peers[""]
}
