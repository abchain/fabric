package main

import (
	"fmt"
	"github.com/abchain/fabric/core/config"
	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/ledger"
)

func main() {

	cf := config.SetupTestConf{"FABRIC", "upgrade", ""}
	cf.Setup()

	db.Start()
	defer db.Stop()
	err := ledger.UpgradeLedger(db.GetDBHandle(), false)

	if err != nil {
		fmt.Print("Upgrade fail:", err)
	}

	fmt.Print("Upgrade Finished")
}
