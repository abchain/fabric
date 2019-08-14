/*
Copyright IBM Corp. 2016 All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

		 http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"flag"
	"fmt"
	"runtime"

	"github.com/abchain/fabric/flogging"
	"github.com/op/go-logging"
	"github.com/spf13/viper"
)

var configLogger = logging.MustGetLogger("config")

// SetupTestConfig setup the config during test execution
func SetupTestConfig(pathToOpenchainYaml string) {

	cfg := SetupTestConf{"", "", pathToOpenchainYaml}
	cfg.Setup()
}

type SetupTestConf struct {
	Prefix     string
	ConfigName string
	YamlPath   string
}

func (c SetupTestConf) Setup() {

	testMode = true

	if c.Prefix == "" {
		c.Prefix = "HYPERLEDGER"
	}
	if c.ConfigName == "" {
		c.ConfigName = "core"
	}
	if c.YamlPath == "" {
		c.YamlPath = "."
	}

	flag.Parse()

	// Now set the configuration file
	InitViperForEnv(c.Prefix)
	viper.SetConfigName(c.ConfigName) // name of config file (without extension)
	viper.AddConfigPath(".")
	viper.AddConfigPath(c.YamlPath) // path to look for the config file in
	err := viper.ReadInConfig()     // Find and read the config file
	if err != nil {                 // Handle errors reading the config file
		panic(fmt.Errorf("Fatal error config file: %s \n", err))
	}

	flogging.LoggingInit("test")

	err = InitCryptoGlobal(viper.GetViper())
	if err != nil {
		panic(fmt.Errorf("Failed initializing the crypto layer [%s]", err))
	}

	// Set the number of maxprocs
	var numProcsDesired = viper.GetInt("peer.gomaxprocs")
	configLogger.Debugf("setting Number of procs to %d, was %d\n", numProcsDesired, runtime.GOMAXPROCS(2))
}
