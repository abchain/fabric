package config_test

import (
	"github.com/abchain/fabric/core/config"
	"github.com/spf13/viper"
	"testing"
)

//This test require two enviroment is set: "CORE_TESTSUB_AA" and "CORE_TESTSUB_AA_XX",
//both of them must have val can represent a non-zero interger
func TestViperEnvSetting(t *testing.T) {

	config.InitViperForEnv("CORE")
	v1 := viper.GetInt("testsub.aa")
	v2 := viper.GetInt("testsub.aa.xx")
	if v1 == 0 || v2 == 0 {
		t.Log("environment is not correctly set yet, test fail")
		t.SkipNow()
	}

	sub1 := config.SubViper("testsub")
	if v := sub1.GetInt("aa"); v != v1 {
		t.Fatalf("not match for sub viper 1: %d vs %d", v, v1)
	}

	sub2 := config.SubViper("testsub.aa")
	if v := sub2.GetInt("xx"); v != v2 {
		t.Fatalf("not match for sub viper 2: %d vs %d", v, v2)
	}

	sub3 := config.SubViper("aa", sub1)
	if v := sub3.GetInt("xx"); v != v2 {
		t.Fatalf("not match for sub viper 3: %d vs %d", v, v2)
	}
}

func TestViperSetting(t *testing.T) {

	conf := config.SetupTestConf{"CORE", "chaincodetest", "../chaincode"}
	conf.Setup()

	originalSetting := viper.GetStringMap("peer")
	before := len(originalSetting)
	t.Log(originalSetting)
	viper.Set("peer.fileSystemPath", "abcdef/ghijk")
	after := len(viper.Sub("peer").AllSettings())

	if viper.GetString("peer.version") == "" {
		t.Fatal("Unexpected viper behavior 1")
	}

	if before == after {
		t.Errorf("Viper seems have repaired its sub, we could use their implement")
	}
	t.Logf("Viper will use the overwrite layer in sub method so the item is different: [%d vs %d]", before, after)

	after = len(config.SubViper("peer").AllSettings())
	t.Log(config.SubViper("peer").AllSettings())
	//notice we add env prefix record in each subviper, so we have 1 record more
	//(and we should just check after have enough records like before)
	if before > after {
		t.Errorf("Wrong items count: %d vs %d", before, after)
	}

	config.CacheViper()
	after = len(config.SubViper("peer").AllSettings())
	if before > after {
		t.Errorf("Wrong items count after making cache: %d vs %d", before, after)
	}

	//sub a sub viper
	subofsub := config.SubViper("sync", config.SubViper("peer"))
	if subofsub.Get("blocks") == nil {
		t.Fatal("sub of sub vp fail:", subofsub.AllSettings())
	}
}
