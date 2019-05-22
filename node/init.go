package node

import (
	"fmt"
	"github.com/abchain/fabric/core/config"
	"github.com/abchain/fabric/core/cred"
	"github.com/abchain/fabric/core/cred/driver"
	"github.com/abchain/fabric/core/db"
	gossip_stub "github.com/abchain/fabric/core/gossip/stub"
	"github.com/abchain/fabric/core/gossip/txnetwork"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/ledger/genesis"
	"github.com/abchain/fabric/core/peer"
	"github.com/abchain/fabric/core/sync/strategy"
	sync_stub "github.com/abchain/fabric/core/sync/stub"
	"github.com/abchain/fabric/events/litekfk"
	pb "github.com/abchain/fabric/protos"
	"github.com/spf13/viper"
)

func (ne *NodeEngine) addLedger(vp *viper.Viper, tag string) (*ledger.Ledger, error) {

	tagdb, err := db.StartDB(tag, config.SubViper("db", vp))
	if err != nil {
		return nil, fmt.Errorf("Try to create db fail: %s", err)
	}

	done := false
	defer func() {
		if !done {
			logger.Debugf("clean db [%s] for unsucessful creation of ledger", tag)
			db.StopDB(tagdb)
		}
	}()

	checkonly := vp.GetBool("notUpgrade")
	err = ledger.UpgradeLedger(tagdb, checkonly)
	if err != nil {
		return nil, fmt.Errorf("Upgrade ledger fail: %s", err)
	}

	l, err := ledger.GetNewLedger(tagdb, ledger.NewLedgerConfig(vp))
	if err != nil {
		return nil, fmt.Errorf("Try to create ledger fail: %s", err)
	}
	//sanity check ...
	if lt := l.Tag(); lt != tag {
		panic(fmt.Errorf("some implement has made the tag (%s) obtained in ledger changed to %s?",
			tag, lt))
	}

	//a legacy progress...
	if ne.Options.MakeGenesisForLedger {
		err = genesis.MakeGenesisForLedger(l, "", nil)
		if err != nil {
			return nil, fmt.Errorf("Try to create genesis block for ledger fail: %s", err)
		}
	}

	done = true
	ne.releaseFunc = append(ne.releaseFunc, func() { db.StopDB(tagdb) })
	ne.Ledgers[tag] = l
	return l, nil
}

func (ne *NodeEngine) GenCredDriver() *cred_driver.Credentials_PeerDriver {
	drv := cred_driver.Credentials_PeerCredBase{ne.Cred.Peer, ne.Cred.Tx}
	return &cred_driver.Credentials_PeerDriver{drv.Clone(), nil, ne.Endorsers}
}

//preinit phase simply read all peer's name in the config and create them,
//user can make settings on node and peer which will be respected in the
//Init process
func (ne *NodeEngine) PreInit() {

	ne.Ledgers = make(map[string]*ledger.Ledger)
	ne.Peers = make(map[string]*PeerEngine)
	ne.Endorsers = make(map[string]credentials.TxEndorserFactory)
	ne.Cred.ccSpecValidator = NewCCSpecValidator(nil)
	ne.TxTopic = make(map[string]litekfk.Topic)
	ne.TxTopicNameHandler = nullTransformer
	//add a default txtopic
	topicCfg := litekfk.NewDefaultConfig()
	ne.TxTopic[""] = litekfk.CreateTopic(topicCfg)

	//occupy ledger's objects position
	ledgerTags := viper.GetStringSlice("node.ledgers")
	for _, tag := range ledgerTags {
		ne.Ledgers[tag] = nil
	}

	//occupy peer's objects position
	peerTags := viper.GetStringSlice("node.peers")
	for _, tag := range peerTags {
		p := new(PeerEngine)
		p.PreInit(ne)
		ne.Peers[tag] = p
	}

	//create default peer
	if len(peerTags) == 0 {
		logger.Info("Create old-fashion, default peer")
		p := new(PeerEngine)
		p.PreInit(ne)
		ne.Peers["peer"] = p
	}

	ne.runPeersFunc = []func(){func() {
		logger.Info("Start the running peer stack")
	}}
}

//ne will fully respect an old-fashion (fabric 0.6) config file
func (ne *NodeEngine) ExecInit() error {

	config.CacheViper()

	//init ledgers
	var defaultTag string
	for tag, l := range ne.Ledgers {

		vp := config.SubViper("ledgers." + tag)
		if vp.GetBool("default") {
			if defaultTag != "" {
				logger.Warningf("Duplicated default tag found [%s vs %s], later will be used", defaultTag, tag)
			}
			defaultTag = tag
		}

		//respect the pre-set ledger
		if l != nil {
			logger.Infof("Ledger %s has been set before init", tag)
			continue
		}

		var err error
		if l, err = ne.addLedger(vp, tag); err != nil {
			return fmt.Errorf("Init ledger %s in node fail: %s", tag, err)
		}
		logger.Info("Init ledger:", tag)
	}

	//select default ledger, if not, use first one, or just create one from peer setting
	if len(ne.Ledgers) > 0 {

		if defaultTag == "" {
			for k, _ := range ne.Ledgers {
				defaultTag = k
				break
			}
		}

		logger.Info("Default ledger is:", defaultTag)
		ledger.SetDefaultLedger(ne.Ledgers[defaultTag])
		ne.Ledgers[""] = ne.Ledgers[defaultTag]
	} else {
		//start default db
		db.Start()
		ne.releaseFunc = append(ne.releaseFunc, func() { db.Stop() })
		if l, err := ledger.GetLedger(); err != nil {
			return fmt.Errorf("Init default ledger fail: %s", err)
		} else {
			if ne.Options.MakeGenesisForLedger {
				genesis.MakeGenesis()
			}

			ne.Ledgers[""] = l
			logger.Warningf("No ledger created, use old-fashion default one")
		}
	}

	//create base credentials
	creddrv := new(cred_driver.Credentials_PeerDriver)
	if err := creddrv.Drive(config.SubViper("node")); err == nil {
		ne.Cred.Peer = creddrv.PeerValidator
		ne.Cred.Tx = creddrv.TxValidator
	} else {
		logger.Info("No credentials availiable in node:", err)
	}
	//TODO: create endorsers

	//init peers
	defaultTag = ""
	for tag, p := range ne.Peers {

		vp := config.SubViper(tag)
		if vp.GetBool("default") {
			if defaultTag != "" {
				logger.Warningf("Duplicated default tag found [%s vs %s], later will be used", defaultTag, tag)
			}
			defaultTag = tag
		}

		if err := p.Init(vp, ne, tag); err != nil {
			return fmt.Errorf("Create peer %s fail: %s", tag, err)
		}
		logger.Infof("Create peer: %s", tag)
	}

	if defaultTag == "" {
		for k, _ := range ne.Peers {
			defaultTag = k
			break
		}
	}

	logger.Info("Default peer is:", defaultTag)
	ne.Peers[""] = ne.Peers[defaultTag]

	return nil

}

//wrap two step in one
func (ne *NodeEngine) Init() error {
	ne.PreInit()
	return ne.ExecInit()
}

func (pe *PeerEngine) PreInit(node *NodeEngine) {
	pe.TxHandlerOpts.ccSpecValidator = NewCCSpecValidator(node.Cred.ccSpecValidator)
}

func (pe *PeerEngine) Init(vp *viper.Viper, node *NodeEngine, tag string) error {

	config.CacheViper(vp)

	var err error
	credrv := node.GenCredDriver()
	if err = credrv.Drive(vp); err != nil {
		if node.Options.EnforceSec {
			return fmt.Errorf("Init credential fail: %s", err)
		} else {
			logger.Warningf("Drive cred fail: %s, no security is available", err)
		}

	}
	pe.defaultEndorser = credrv.TxEndorserDef

	pe.srvPoint = new(servicePoint)
	if err = pe.srvPoint.Init(vp); err != nil {
		return fmt.Errorf("Init serverpoint fail: %s", err)
	}
	node.srvPoints = append(node.srvPoints, pe.srvPoint)

	isValidator := vp.GetBool("validator.enabled")
	if isValidator {
		logger.Infof("Peer [%s] is set to be validator", tag)
	}

	var peercfg *peer.PeerConfig
	if peercfg, err = peer.NewPeerConfig(isValidator, vp, pe.srvPoint.spec); err != nil {
		return fmt.Errorf("Init peer config fail: %s", err)
	}
	peercfg.PeerTag = tag

	var pimpl *peer.Impl
	if pimpl, err = peer.CreateNewPeer(credrv.PeerValidator, peercfg); err != nil {
		return fmt.Errorf("Init peer fail: %s", err)
	}

	//register init action right before running to node, which require the
	//running environment but just need to call once, the Run/Stop of PeerEngine
	//never touch these
	node.runPeersFunc = append(node.runPeersFunc, func() {
		logger.Infof("starting peer [%s]", tag)
		pimpl.RunPeer(peercfg)
	})

	pe.Peer = pimpl
	pb.RegisterPeerServer(pe.srvPoint.Server, pimpl)

	//init gossip network
	if gstub := gossip_stub.InitGossipStub(pe.Peer, pe.srvPoint.Server); gstub == nil {
		return fmt.Errorf("Can not create gossip server: %s", err)
	} else {
		pe.txn = txnetwork.GetNetworkEntry(gstub)
	}

	pe.txn.InitCred(credrv.TxValidator)
	var peerLedger *ledger.Ledger

	//test ledger configuration
	if useledger := vp.GetString("ledger"); useledger == "" || useledger == "default" {
		//use default ledger, do nothing
		var err error
		peerLedger, err = ledger.GetLedger()
		if err != nil {
			return fmt.Errorf("Could not get default ledger: %s", err)
		}

	} else if useledger == "sole" {
		//create a new ledger tagged by the peer, add it to node
		var err error
		l, exist := node.Ledgers[useledger]
		if !exist {
			l, err = node.addLedger(vp, useledger)
			if err != nil {
				return fmt.Errorf("Create peer's default ledger [%s] fail: %s", useledger, err)
			}
			logger.Info("Create default sole ledger for peer [%s]", tag)
		}
		peerLedger = l

	} else {
		//select a ledger created before (by node or other peer), throw error if not found
		l, ok := node.Ledgers[useledger]
		if !ok {
			return fmt.Errorf("Could not find specified ledger [%s]", useledger)
		}
		peerLedger = l

	}

	if systub := sync_stub.InitSyncStub(pe.Peer, peerLedger, pe.srvPoint.Server); systub == nil {
		return fmt.Errorf("Can not create syncing server: %s", err)
	} else {
		pe.sync = syncstrategy.GetSyncStrategyEntry(systub)
		if err := pe.sync.Configure(config.SubViper("sync", vp)); err != nil {
			return fmt.Errorf("Could not configure sync entry: %s", err)
		}
	}

	pe.txn.InitLedger(peerLedger)
	pe.TxHandlerOpts.ccSpecValidator = NewCCSpecValidator(node.Cred.ccSpecValidator)
	//construct txhandler groups:
	/*
		ccspec (custom cert),
		tx validator (tx security context),
		plain tx parsing,
		[confidentiality],
		verifier,
		[peer custom],
		[node custom],
		[pooling],
		topic recording,
	*/
	handlerArray := []pb.TxPreHandler{
		pe.TxHandlerOpts.ccSpecValidator,
		validatorToHandler(credrv.TxValidator),
		pb.PlainTxHandler,
		pb.YFCCNameHandler,
	}

	//TODO: handling confidentiality

	handlerArray = append(handlerArray, pb.FuncAsTxPreHandler(
		func(txe *pb.TransactionHandlingContext) (*pb.TransactionHandlingContext, error) {
			if txe.ChaincodeSpec == nil {
				return nil, fmt.Errorf("tx can not be corretly parsed")
			}
			return txe, nil
		}))
	handlerArray = append(handlerArray, pe.TxHandlerOpts.Customs...)
	handlerArray = append(handlerArray, node.CustomFilters...)

	if !pe.TxHandlerOpts.NoPooling {
		handlerArray = append(handlerArray, txPoolHandler{peerLedger})
	}
	handlerArray = append(handlerArray, &recordHandler{node.TxTopic, node.TxTopicNameHandler})

	pe.txn.InitTerminal(pb.MutipleTxHandler(handlerArray...))

	//reset peer trigger update so we should run it finally
	if pe.defaultEndorser != nil {
		err = pe.txn.ResetPeer(pe.defaultEndorser)
	} else {
		networkName := peercfg.PeerEndpoint.GetID().GetName()
		if networkName == "" {
			networkName = tag
		}
		err = pe.txn.ResetPeerSimple([]byte(networkName + "_PeerId16BytePadding"))
	}

	if err != nil {
		return fmt.Errorf("Can not set self peer id: %s", err)
	}

	return nil
}
