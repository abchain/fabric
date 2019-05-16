package main

import (
	"time"
	"github.com/abchain/fabric/core/embedded_chaincode/api"
	"github.com/abchain/fabric/node/legacy"
	node "github.com/abchain/fabric/node/start"
	"github.com/spf13/viper"
	pb "github.com/abchain/fabric/protos"
	"os"
	"log"
	"net/http"
	_ "net/http/pprof"
	"github.com/op/go-logging"
	"github.com/abchain/fabric/core/statesync/testsync/chaincode"
)

var logger = logging.MustGetLogger("main")

func runTest() error {

	syncTarget := viper.GetString("peer.syncTarget")
	if len(syncTarget) == 0 {
		return nil
	}

	for {
		<-time.After(1 * time.Second)
		syncStub := node.GetNode().DefaultPeer().StateSyncStub
		peer := &pb.PeerID{syncTarget}
		h := syncStub.StreamStub.PickHandler(peer)

		if h == nil {
			logger.Infof("Stream Handler <%s> not ready...\n\n\n", peer)
		} else {
			logger.Infof("Stream Handler <%s> ready!!\n\n\n", peer)

			blockSync := viper.GetString("peer.syncType")
			err := syncStub.SyncToStateByPeer(nil,nil, peer, blockSync)
			if err != nil {
				logger.Infof("Failed to sync to <%s>: %s", peer, err)
			} else {
				logger.Infof("Successfully sync to <%s>", peer)
			}
			os.Exit(0)
		}
	}

	return nil
}


func startDebug() {
	enablePprof := viper.GetBool("peer.enablePprof")
	enablePprof = true
	debugUrl := viper.GetString("peer.debug.listenAddress")

	if len(debugUrl) > 0 && enablePprof {
		go func() {
			log.Println(http.ListenAndServe(debugUrl, nil))
		}()
	}
}

func main() {

	adapter := &legacynode.LegacyEngineAdapter{}

	postRun := func() error {
		regFunc := func(name string) error {
			return api.RegisterECC(&api.EmbeddedChaincode{name, new(testsync_chaincode.TestSyncChaincode)})
		}

		regFunc("example01_")
		regFunc("example02_")
		regFunc("example03_")

		if err := adapter.Init(); err != nil {
			return err
		}

		go startDebug()
		go runTest()
		return nil
	}

	node.RunNode(&node.NodeConfig{PostRun: postRun, Schemes: adapter.Scheme})

}