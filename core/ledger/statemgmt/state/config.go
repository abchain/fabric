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

package state

import (
	"fmt"
	"github.com/abchain/fabric/core/config"
	"github.com/spf13/viper"
	"sync"
)

//export config
type StateConfig struct {
	V *stateConfig
}

func (cw StateConfig) ImplConfigs() map[string]interface{} {
	return cw.V.stateImplConfigs
}

type stateConfig struct {
	stateImplName    stateImplType
	stateImplConfigs map[string]interface{}
	deltaHistorySize int
}

func NewStateConfig(vp *viper.Viper) (*stateConfig, error) {
	ret := new(stateConfig)
	err := ret.loadConfig(vp)
	return ret, err
}

var loadConfigOnce sync.Once
var defconf *stateConfig

func DefaultConfig() *stateConfig {

	loadConfigOnce.Do(
		func() {
			var err error
			defconf, err = NewStateConfig(config.SubViper("ledger"))
			if err != nil {
				panic(err)
			}
		})

	return defconf
}

func (c *stateConfig) loadConfig(vp *viper.Viper) error {
	logger.Info("Loading configurations...")
	c.stateImplName = stateImplType(vp.GetString("state.dataStructure.name"))
	c.stateImplConfigs = vp.GetStringMap("state.dataStructure.configs")
	c.deltaHistorySize = vp.GetInt("state.deltaHistorySize")
	logger.Infof("Configurations loaded. stateImplName=[%s], stateImplConfigs=%v, deltaHistorySize=[%d]",
		c.stateImplName, c.stateImplConfigs, c.deltaHistorySize)

	if len(c.stateImplName) == 0 {
		c.stateImplName = defaultStateImpl
		c.stateImplConfigs = nil
	} else if c.stateImplName != buckettreeType && c.stateImplName != trieType && c.stateImplName != rawType {
		return fmt.Errorf("Error during initialization of state implementation. State data structure '%s' is not valid.", c.stateImplName)
	}

	if c.deltaHistorySize < 0 {
		return fmt.Errorf("Delta history size must be greater than or equal to 0. Current value is %d.", c.deltaHistorySize)
	}
	return nil
}
