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
		//validator is applied BEFORE pooling and custom filter applied
		//AFTER that, if nopooling is set, CustomValidators will be also
		//omitted
		Customs          []pb.TxPreHandler
		CustomValidators []pb.TxPreHandler
		NoPooling        bool
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
		EnforceSec           bool
		MakeGenesisForLedger bool
	}

	srvPoints []*servicePoint
	//some action stacks, for init before running and final release
	runPeersFunc []func()
	releaseFunc  []func()
}

func (ne *NodeEngine) DefaultLedger() *ledger.Ledger {
	return ne.Ledgers[""]
}

func (ne *NodeEngine) LedgersList() (ret []*ledger.Ledger) {
	for k, l := range ne.Ledgers {
		if k == "" {
			continue
		}
		ret = append(ret, l)
	}
	return
}

func (ne *NodeEngine) DefaultPeer() *PeerEngine {
	return ne.Peers[""]
}

func (ne *NodeEngine) PeersList() (ret []*PeerEngine) {
	for k, p := range ne.Peers {
		if k == "" {
			continue
		}
		ret = append(ret, p)
	}
	return
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

func (ne *NodeEngine) AddTxTopic(name string, topic ...litekfk.Topic) {
	if ne.TxTopic == nil {
		panic("Not inited, don't call before preinit")
	}

	if len(topic) == 0 {
		topicCfg := litekfk.NewDefaultConfig()
		ne.TxTopic[name] = litekfk.CreateTopic(topicCfg)
	} else {
		ne.TxTopic[name] = topic[0]
	}

}

func (ne *NodeEngine) AddServicePoint(s ServicePoint) {
	ne.srvPoints = append(ne.srvPoints, s.servicePoint)
}

//run both peers and service
func (ne *NodeEngine) RunAll() (chan ServicePoint, error) {
	if err := ne.RunPeers(); err != nil {
		return nil, err
	}

	return ne.RunServices()
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
		//skip default peer (which is a shadow of the peer with
		//another name in Peers table)
		if name == "" {
			continue
		}
		if err := p.Run(); err != nil {
			return fmt.Errorf("start peer [%s] fail: %s", name, err)
		}
		defer errExit(p)
	}

	//for first run, also run peers' impl (only once)
	for _, f := range ne.runPeersFunc {
		f()
	}
	ne.runPeersFunc = nil

	success = true
	return nil
}

//return a manage channel for all running services, for any value s
//receive from the channel, it MUST call Start of the s, passing
//this channel to s again, to respawn the rpc service thread,
//BEFORE StopServices is called. Or user should just never touch the channel
func (ne *NodeEngine) RunServices() (chan ServicePoint, error) {

	notify := make(chan ServicePoint)

	success := false
	errExit := func(p *servicePoint) {
		if !success {
			if err := p.Stop(); err != nil {
				logger.Errorf("srvpoint [%s] fail on stop: %s", p.spec.Address, err)
			}
		}
	}

	recycleCnt := 0
	defer func() {
		if success {
			return
		}
		for ; recycleCnt > 0; recycleCnt-- {
			<-notify
		}
	}()

	for _, p := range ne.srvPoints {
		//simply clean status
		p.srvStatus = nil
		if err := p.Start(notify); err != nil {
			return nil, fmt.Errorf("start service [%s] fail: %s", p.spec.Address, err)
		}
		recycleCnt++
		defer errExit(p)
	}

	success = true
	return notify, nil

}

func (ne *NodeEngine) StopServices(notify <-chan ServicePoint) {

	for _, p := range ne.srvPoints {
		p.Stop()
	}

	for i := len(ne.srvPoints); i > 0; i-- {
		endp := <-notify
		logger.Warningf("service [%s] has stopped ", endp.spec.Address)
	}

}

func (ne *NodeEngine) FinalRelease() {

	if ne == nil {
		return
	}

	for _, p := range ne.srvPoints {
		if p.lPort != nil {
			p.lPort.Close()
		}
	}

	for _, f := range ne.releaseFunc {
		f()
	}
}
