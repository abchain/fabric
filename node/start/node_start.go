package startnode

import (
	"fmt"
	"github.com/abchain/fabric/core/chaincode"
	"github.com/abchain/fabric/core/config"
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
//  the context passed in PreInitFabricNode will act as a "final switch"
//  which will close all running routine inside the node

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
)

func GetNode() *node.NodeEngine { return theNode }

func PreInitFabricNode(ctx context.Context, name string) {
	if theNode != nil {
		panic("Doudble call of init")
	}
	theNode = new(node.NodeEngine)
	theNode.Name = name
	theNode.PreInit()

	peer.PeerGlobalParentCtx = ctx
}

func Final() {

	theNode.FinalRelease()

}

func RunFabricNode() (error, func(ctx context.Context)) {
	status, err := theNode.RunAll()
	if err != nil {
		return err, nil
	}

	return nil, func(ctx context.Context) {

		defer theNode.StopServices(status)
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
	if ccConf := config.SubViper("chaincode"); !ccConf.GetBool("disabled") {
		ccsrv, err := node.CreateServerPoint(ccConf)
		if err != nil {
			logger.Infof("Can not create server spec for chaincode: [%s], merge it into peer", err)
			ccsrv = theNode.DefaultPeer().GetServerPoint()
		} else {
			theNode.AddServicePoint(ccsrv)
		}

		userRunsCC := false
		if ccConf.GetString("mode") == chaincode.DevModeUserRunsChaincode {
			userRunsCC = true
		}

		chaincode.NewSystemChaincodeSupport(theNode.Name)
		ccplatform := chaincode.NewChaincodeSupport(chaincode.DefaultChain, theNode.Name, ccsrv.Spec(), userRunsCC)
		pb.RegisterChaincodeSupportServer(ccsrv.Server, ccplatform)
	} else {
		logger.Info("Chaincode platform setting is disabled")
	}

	devOps := api.NewDevopsServer(theNode)
	//omit the error of chainsrv, later it should return no error
	nbif, _ := theNode.DefaultPeer().Peer.GetNeighbour()
	ocsrv, _ := api.NewOpenchainServerWithPeerInfo(nbif)

	//api, also bind the event hub, and "service" configuration in YA-fabric 0.7/0.8 is abandoned
	if viper.IsSet("node.api") {
		apiConf := config.SubViper("node.api")
		if !apiConf.GetBool("disabled") {
			apisrv, err := node.CreateServerPoint(config.SubViper("node.api"))
			if err != nil {
				return fmt.Errorf("Error setting for API service: %s", err)
			}
			theNode.AddServicePoint(apisrv)

			//init each term, with separated disabled switch on them
			if !apiConf.GetBool("service.disabled") {
				logger.Info("client service is attached")
				pb.RegisterDevopsServer(apisrv.Server, devOps)
			}

			if !apiConf.GetBool("admin.disabled") {
				logger.Info("administrator interface is attached")
				pb.RegisterAdminServer(apisrv.Server, api.NewAdminServer())
			}

			if !apiConf.GetBool("chain.disabled") {
				logger.Info("chain service is attached")
				pb.RegisterOpenchainServer(apisrv.Server, ocsrv)
			}

			if evtConf := config.SubViper("events", apiConf); !evtConf.GetBool("disabled") {
				logger.Info("event service is attached")
				pb.RegisterEventsServer(apisrv.Server, producer.NewEventsServer(
					uint(evtConf.GetInt("buffersize")),
					evtConf.GetInt("timeout")))
			}
		} else {
			logger.Info("api interface has been disabled")
		}

	} else {
		logger.Info("Apply legacy API configurations")
		//respect legacy configuration: enable all interfaces and bind them to default peer, except for
		//event
		apisrv := theNode.DefaultPeer().GetServerPoint()
		pb.RegisterAdminServer(apisrv.Server, api.NewAdminServer())
		pb.RegisterDevopsServer(apisrv.Server, devOps)
		pb.RegisterOpenchainServer(apisrv.Server, ocsrv)

		if evtsrv, err := node.CreateServerPoint(config.SubViper("peer.validator.events")); err != nil {
			return fmt.Errorf("Error setting for event service: %s", err)
		} else {
			theNode.AddServicePoint(evtsrv)
			evtConf := config.SubViper("peer.validator.events")
			pb.RegisterEventsServer(evtsrv.Server, producer.NewEventsServer(
				uint(evtConf.GetInt("buffersize")),
				evtConf.GetInt("timeout")))
		}
	}

	//finally the rest, may be abandoned later
	if viper.GetBool("rest.enabled") {
		go webapi.StartOpenchainRESTServer(ocsrv, devOps)
	}

	return nil

}
