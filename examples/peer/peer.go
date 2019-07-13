package main

import (
	_ "github.com/abchain/fabric/core/cred/default"
	node "github.com/abchain/fabric/node/start"
)

func main() {

	node.RunNode(&node.NodeConfig{})

}
