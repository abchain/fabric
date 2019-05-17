package startnode

import (
	"fmt"
	"github.com/abchain/fabric/core/crypto"
	"github.com/abchain/fabric/node"
	"golang.org/x/net/context"
	"os"
	"os/signal"
)

type NodeConfig struct {
	precfg   *GlobalConfig
	Settings map[string]interface{}
	//call after preinit and before init, for adjusting the configurations
	//in node
	Schemes func(*node.NodeEngine)
	//will be call after init and before running, to execute additional works,
	//e.g. prepare the chaincodes, etc
	PostRun func() error
	//instead of the general guard function, being call after node is running,
	//and node will be stop after this function is returned
	TaskRun func()
}

//execute config first, so user can use viper and some config modules and log
func (ncfg *NodeConfig) PreConfig() error {
	cfg := new(GlobalConfig)
	cfg.LogRole = "node"
	cfg.NotUseSourceConfig = true
	cfg.DefaultSetting = ncfg.Settings

	if err := cfg.Apply(); err != nil {
		return err
	}
	return nil
}

//mimic peer.main()
func RunNode(ncfg *NodeConfig) {

	logger.Info("YA-fabric node runner start ...")

	if ncfg.precfg == nil {
		if err := ncfg.PreConfig(); err != nil {
			panic(fmt.Errorf("Init fail: %s", err))
		}
	}

	// Init the crypto layer
	if err := crypto.Init(); err != nil {
		panic(fmt.Errorf("Failed to initialize the crypto layer: %s", err))
	}

	defer Final()
	PreInitFabricNode("Default")
	if ncfg.Schemes != nil {
		ncfg.Schemes(theNode)
	}

	if err := InitFabricNode(); err != nil {
		panic(fmt.Errorf("Failed to init node: %s", err))
	}

	if ncfg.PostRun != nil {
		if err := ncfg.PostRun(); err != nil {
			logger.Errorf("post run fail: %s, exit immediately", err)
			return
		}
	}

	if err, guardf := RunFabricNode(); err != nil {
		panic(fmt.Errorf("Fail to run node: %s", err))
	} else if ncfg.TaskRun == nil {
		gctx, endf := context.WithCancel(context.Background())

		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		go func() {

			<-c
			endf()
			logger.Info("Get ctrl-c and exit")

		}()

		//block here
		guardf(gctx)
	} else {
		logger.Info("start running user specified process ...")
		ncfg.TaskRun()
		logger.Info("user specified process end and node exit ...")
		guardf(nil)
	}

	logger.Info("YA-fabric node normally exit ...")
}
