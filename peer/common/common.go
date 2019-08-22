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

package common

import (
	"fmt"
	pb "github.com/abchain/fabric/protos"
	"github.com/spf13/cobra"
)

// UndefinedParamValue defines what undefined parameters in the command line will initialise to
const UndefinedParamValue = ""

var GenDevopsClient func() (pb.DevopsClient, error)
var cachedClient pb.DevopsClient

// GetDevopsClient returns a new client connection for this peer
func GetDevopsClient(cmd *cobra.Command) (pb.DevopsClient, error) {

	if cachedClient == nil {
		var err error
		cachedClient, err = GenDevopsClient()
		if err != nil {
			return nil, err
		}
	}

	return cachedClient, nil
}

func genDevopsClient_default() (pb.DevopsClient, error) {
	clientConn, err := newPeerClientConnection(true)
	if err != nil {
		return nil, fmt.Errorf("Error trying to connect to local peer: %s", err)
	}
	devopsClient := pb.NewDevopsClient(clientConn)
	return devopsClient, nil
}

func init() {
	GenDevopsClient = genDevopsClient_default
}
