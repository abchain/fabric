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
	"github.com/abchain/fabric/core/crypto/primitives"
	"github.com/spf13/viper"
)

// Init initializes the crypto layer. It load from viper the security level
// and the logging setting.
func InitCryptoGlobal(vp *viper.Viper) (err error) {

	if vp == nil {
		vp = viper.GetViper()
	}

	// Init security level
	securityLevel := 256
	if vp.IsSet("security.level") {
		ovveride := vp.GetInt("security.level")
		if ovveride != 0 {
			securityLevel = ovveride
		}
	}

	hashAlgorithm := "SHA3"
	if vp.IsSet("security.hashAlgorithm") {
		ovveride := vp.GetString("security.hashAlgorithm")
		if ovveride != "" {
			hashAlgorithm = ovveride
		}
	}

	logger.Debugf("Working at security level [%d]", securityLevel)
	if err = primitives.InitSecurityLevel(hashAlgorithm, securityLevel); err != nil {
		logger.Errorf("Failed setting security level: [%s]", err)

		return
	}

	return
}
