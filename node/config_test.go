package node

import (
	"github.com/abchain/fabric/core/config"
	_ "github.com/abchain/fabric/core/cred/default"
	"github.com/abchain/fabric/core/db"
	"io/ioutil"
	"testing"
)

func setDataPath(path string) {
	db.InitDBPath(path)
}

func TestLegacyInit(t *testing.T) {

	cf := config.SetupTestConf{"FABRIC", "conf_legacy_test", ""}
	cf.Setup()

	tempDir, err := ioutil.TempDir("", "fabric-db-test")
	if err != nil {
		t.Fatal("tempfile fail", err)
	}

	setDataPath(tempDir)

	ne := new(NodeEngine)
	ne.Name = "test"
	defer ne.FinalRelease()
	if err := ne.Init(); err != nil {
		t.Fatal(err)
	}

	if _, ok := ne.Ledgers[""]; !ok {
		t.Fatal("no ledger")
	}

	if _, ok := ne.Peers[""]; !ok {
		t.Fatal("no peer")
	}

	if len(ne.srvPoints) == 0 {
		t.Fatal("no srvpoint")
	}
}

func TestInit(t *testing.T) {

	cf := config.SetupTestConf{"FABRIC", "conf_test", ""}
	cf.Setup()

	tempDir, err := ioutil.TempDir("", "fabric-db-test")
	if err != nil {
		t.Fatal("tempfile fail", err)
	}

	setDataPath(tempDir)

	ne := new(NodeEngine)
	ne.Name = "test"
	defer ne.FinalRelease()
	if err := ne.Init(); err != nil {
		t.Fatal(err)
	}

	if len(ne.Ledgers) != 4 {
		t.Fatal("missed ledger:", ne.Ledgers)
	}

	if len(ne.Peers) != 4 {
		t.Fatal("missed peer:", ne.Peers)
	}

	if len(ne.srvPoints) == 0 {
		t.Fatal("no srvpoint")
	}
}
