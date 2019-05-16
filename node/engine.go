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
	"fmt"
	cred "github.com/abchain/fabric/core/cred"
	"github.com/abchain/fabric/core/gossip/txnetwork"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/peer"
	"github.com/abchain/fabric/core/sync/strategy"
	"github.com/abchain/fabric/events/litekfk"
	pb "github.com/abchain/fabric/protos"
	"github.com/op/go-logging"
	"golang.org/x/net/context"
)

var (
	logger = logging.MustGetLogger("engine")
)

type PeerEngine struct {
	TxHandlerOpts struct {
		Customs   []pb.TxPreHandler
		NoPooling bool
		*ccSpecValidator
	}

	txn  *txnetwork.TxNetworkEntry
	sync *syncstrategy.SyncEntry
	peer.Peer

	defaultEndorser cred.TxEndorserFactory
	defaultAttr     []string
	srvPoint        *servicePoint

	//run-time vars
	lastID string

	lastCache  txPoint
	exitNotify chan interface{}
	runStatus  error
	stopFunc   context.CancelFunc
}

func (pe *PeerEngine) TxNetwork() *txnetwork.TxNetworkEntry { return pe.txn }
func (pe *PeerEngine) Syncer() *syncstrategy.SyncEntry      { return pe.sync }

/*
	node engine intregated mutiple PeerEngine, ledgers and credentials, system should access
	ledger here
*/
type NodeEngine struct {
	Name string

	Ledgers   map[string]*ledger.Ledger
	Peers     map[string]*PeerEngine
	Endorsers map[string]cred.TxEndorserFactory
	Cred      struct {
		Peer cred.PeerCreds
		Tx   cred.TxHandlerFactory
		*ccSpecValidator
	}
	CustomFilters []pb.TxPreHandler

	//all the received transactions can be read out from different topic by its chaincode name,
	//according to the configuration in transation filter
	TxTopic            map[string]litekfk.Topic
	TxTopicNameHandler CCNameTransformer

	Options struct {
		EnforceSec bool
	}

	srvPoints    []*servicePoint
	runPeersFunc func()
}

func (ne *NodeEngine) DefaultLedger() *ledger.Ledger {
	return ne.Ledgers[""]
}

func (ne *NodeEngine) DefaultPeer() *PeerEngine {
	return ne.Peers[""]
}

func (ne *NodeEngine) SelectEndorser(name string) (cred.TxEndorserFactory, error) {

	if ne.Endorsers != nil {
		opt, ok := ne.Endorsers[name]
		if ok {
			return opt, nil
		}
	}

	//TODO: we can create external type of endorser if being configured to
	return nil, fmt.Errorf("Specified endorser %s is not exist", name)

}

func (ne *NodeEngine) SelectLedger(name string) (*ledger.Ledger, error) {

	if ne.Ledgers != nil {
		l, ok := ne.Ledgers[name]
		if ok {
			return l, nil
		}
	}

	//TODO: we can create external type of endorser if being configured to
	return nil, fmt.Errorf("Specified ledger %s is not exist", name)

}

func (ne *NodeEngine) AddServicePoint(s ServicePoint) {
	ne.srvPoints = append(ne.srvPoints, s.servicePoint)
}

func (ne *NodeEngine) RunPeers() error {

	if ne.runPeersFunc == nil {
		return fmt.Errorf("Peers have been run, should not call again")
	}

	success := false
	errExit := func(p *PeerEngine) {
		if !success {
			p.Stop()
		}
	}

	for name, p := range ne.Peers {
		//skip default peer (which is a shadow)
		if name == "" {
			continue
		}
		if err := p.Run(); err != nil {
			return fmt.Errorf("start peer [%s] fail: %s", name, err)
		}
		defer errExit(p)
	}

	//also run peers' impl (only once)
	ne.runPeersFunc()
	ne.runPeersFunc = nil

	success = true
	return nil
}

func (ne *NodeEngine) RunServices() (<-chan ServicePoint, error) {

	notify := make(chan ServicePoint)

	success := false
	errExit := func(p *servicePoint) {
		if !success {
			if err := p.Stop(); err != nil {
				logger.Errorf("srvpoint [%s] fail on stop: %s", p.spec.Address, err)
			}
		}
	}

	for _, p := range ne.srvPoints {
		if err := p.Start(notify); err != nil {
			return nil, fmt.Errorf("start service [%s] fail: %s", p.spec.Address, err)
		}
		defer errExit(p)
	}

	success = true
	return notify, nil

}

func (ne *NodeEngine) StopServices(notify <-chan ServicePoint) {

	for _, p := range ne.srvPoints {
		if p.srvStatus != nil {
			logger.Infof("service [%s] has stopped before (%s)", p.spec.Address, p.srvStatus)
		}
		p.Stop()
		for endp := <-notify; p != endp.servicePoint; {
			logger.Warningf("service [%s] stopped occasionally when stopping (%s)", endp.spec.Address, p.spec.Address)
		}

		logger.Warningf("service [%s] has stopped", p.spec.Address)
	}
}

func (ne *NodeEngine) FinalRelease() {

	for _, p := range ne.srvPoints {
		if p.lPort != nil {
			p.lPort.Close()
		}
	}
}

func (ne *NodeEngine) RunAll() (<-chan ServicePoint, error) {
	if err := ne.RunPeers(); err != nil {
		return nil, err
	}

	return ne.RunServices()
}
