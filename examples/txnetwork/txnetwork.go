package main

import (
	"github.com/abchain/fabric/core/chaincode"
	_ "github.com/abchain/fabric/core/cred/default"
	"github.com/abchain/fabric/core/embedded_chaincode/api"
	cc "github.com/abchain/fabric/examples/txnetwork/chaincode"
	node "github.com/abchain/fabric/node/start"
	"golang.org/x/net/context"
)

var ccConf = &api.EmbeddedChaincode{
	Name:      "txnetwork",
	Chaincode: new(cc.SimpleChaincode),
}

func main() {

	pf := func() error {
		if err := api.RegisterECC(ccConf); err != nil {
			return nil
		}

		if err := api.LaunchEmbeddedCC(context.Background(), ccConf.Name,
			string(chaincode.DefaultChain), nil, node.GetNode().DefaultLedger()); err != nil {
			return err
		}
		return nil
	}

	node.RunNode(&node.NodeConfig{PostRun: pf})

}
