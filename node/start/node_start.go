package startnode

import (
	"fmt"
	"github.com/abchain/fabric/core/chaincode"
	"github.com/abchain/fabric/core/config"
	"github.com/abchain/fabric/core/embedded_chaincode"
	"github.com/abchain/fabric/core/peer"
	"github.com/abchain/fabric/events/producer"
	"github.com/abchain/fabric/node"
	webapi "github.com/abchain/fabric/node/rest"
	api "github.com/abchain/fabric/node/service"
	pb "github.com/abchain/fabric/protos"
	"github.com/op/go-logging"
	"github.com/spf13/viper"
	"golang.org/x/net/context"
)

//entries for init and run the fabric node, including following steps
//* Pre-init (call PreInitFabricNode), which will create the global node
//  object and execute PreInit in the node, and init other required
//  variables

//* Init (call InitFabricNode), init the node object, all of the
//  rpc services is ready now

//* Run (call RunFabricNode), the node start accepting incoming request
//  and communicating to the world

//* Keep, run the guard function, which will keep block until the incoming
//  context is canceled, and just keep tracking the status of each rpc
//  services. When finish, the running rpc services in the node will be stopped
//  (and RunFabricNode can be called again)
//  if a nil context is passed, it exit without blocking and simply stop
//  current running node

//* Final, when exit, just do not forget call Final to release most of the
//  resources (process is recommended to be exit after that)

var (
	logger  = logging.MustGetLogger("engine")
	theNode *node.NodeEngine
	theDoom context.CancelFunc //use to cancel all ops in the node
)

func GetNode() *node.NodeEngine { return theNode }

func PreInitFabricNode(name string) {
	if theNode != nil {
		panic("Doudble call of init")
	}
	theNode = new(node.NodeEngine)
	theNode.Name = name
	theNode.PreInit()

	peer.PeerGlobalParentCtx, theDoom = context.WithCancel(context.Background())
}

func Final() {

	if theDoom != nil {
		theDoom()
		theNode.FinalRelease()
	}

}

func RunFabricNode() (error, func(ctx context.Context)) {
	status, err := theNode.RunAll()
	if err != nil {
		return err, nil
	}

	return nil, func(ctx context.Context) {

		defer theNode.StopServices(status)
		if ctx == nil {
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case srvp := <-status:
				logger.Errorf("server point [%s] fail: %s", srvp.Spec().Address, srvp.Status())
				//simply respawn it (just fail for we do not clean the error)
				//TODO: we may need a more robust way before we can really respawn the
				//server (for example, retry after serveral seconds, stop after some
				//times of retrying, etc)
				srvp.Start(status)
			}
		}
	}
}

func InitFabricNode() error {

	if err := theNode.ExecInit(); err != nil {
		return fmt.Errorf("NODE INIT FAILURE: ***** %s *****", err)
	}

	//create node and other infrastructures ... (if no setting, use default peer's server point)
	//chaincode: TODO: support mutiple chaincode platforms
	ccsrv, err := node.CreateServerPoint(config.SubViper("chaincode"))
	if err != nil {
		logger.Infof("Can not create server spec for chaincode: [%s], merge it into peer", err)
		ccsrv = theNode.DefaultPeer().GetServerPoint()
	} else {
		theNode.AddServicePoint(ccsrv)
	}

	userRunsCC := false
	if viper.GetString("chaincode.mode") == chaincode.DevModeUserRunsChaincode {
		userRunsCC = true
	}

	ccplatform := chaincode.NewChaincodeSupport(chaincode.DefaultChain, theNode.Name, ccsrv.Spec(), userRunsCC)
	pb.RegisterChaincodeSupportServer(ccsrv.Server, ccplatform)

	//TODO: now we just launch system chaincode for default ledger
	err = embedded_chaincode.RegisterSysCCs(theNode.DefaultLedger(), ccplatform)
	if err != nil {
		return fmt.Errorf("launch system chaincode fail: %s", err)
	}

	var apisrv, evtsrv node.ServicePoint
	var evtConf *viper.Viper
	//api, also bind the event hub, and "service" configuration in YA-fabric 0.7/0.8 is abandoned
	if viper.IsSet("node.api") {
		if apisrv, err = node.CreateServerPoint(config.SubViper("node.api")); err != nil {
			return fmt.Errorf("Error setting for API service: %s", err)
		}
		theNode.AddServicePoint(apisrv)
		evtsrv = apisrv
		evtConf = config.SubViper("node.api.events")
	} else {
		//for old fashion, we just bind it into deafult peer
		apisrv = theNode.DefaultPeer().GetServerPoint()
		//and respect the event configuration
		if evtsrv, err = node.CreateServerPoint(config.SubViper("peer.validator.events")); err != nil {
			return fmt.Errorf("Error setting for event service: %s", err)
		}
		evtConf = config.SubViper("peer.validator.events")
	}

	devOps := api.NewDevopsServer(theNode)
	pb.RegisterAdminServer(apisrv.Server, api.NewAdminServer())
	pb.RegisterDevopsServer(apisrv.Server, devOps)
	pb.RegisterEventsServer(evtsrv.Server, producer.NewEventsServer(
		uint(evtConf.GetInt("buffersize")),
		evtConf.GetInt("timeout")))

	//TODO: openchain should be able to use mutiple peer
	nbif, _ := theNode.DefaultPeer().Peer.GetNeighbour()
	if ocsrv, err := api.NewOpenchainServerWithPeerInfo(nbif); err != nil {
		return fmt.Errorf("Error creating OpenchainServer: %s", err)
	} else {
		pb.RegisterOpenchainServer(apisrv.Server, ocsrv)
		//finally the rest, may be abandoned later
		if viper.GetBool("rest.enabled") {
			go webapi.StartOpenchainRESTServer(ocsrv, devOps)
		}
	}

	return nil

}
