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

// The 'viper' package for configuration handling is very flexible, but has
// been found to have extremely poor performance when configuration values are
// accessed repeatedly. The function CacheConfiguration() defined here caches
// all configuration values that are accessed frequently.  These parameters
// are now presented as function calls that access local configuration
// variables.  This seems to be the most robust way to represent these
// parameters in the face of the numerous ways that configuration files are
// loaded and used (e.g, normal usage vs. test cases).

// The CacheConfiguration() function is allowed to be called globally to
// ensure that the correct values are always cached; See for example how
// certain parameters are forced in 'ChaincodeDevMode' in main.go.

package peer

import (
	"github.com/abchain/fabric/core/config"
	"github.com/spf13/viper"
	"strings"
	"time"

	pb "github.com/abchain/fabric/protos"
	"google.golang.org/grpc"
)

type PeerConfig struct {
	PeerTag      string
	IsValidator  bool
	PeerEndpoint *pb.PeerEndpoint
	Discovery    struct {
		Roots       []string
		Persist     bool
		Hidden      bool
		Disable     bool
		TouchPeriod time.Duration
		MaxNodes    int
	}
	NewPeerClientConn func(string) (*grpc.ClientConn, error)
}

func NewPeerConfig(forValidator bool, vp *viper.Viper, spec *config.ServerSpec) (*PeerConfig, error) {

	cfg := new(PeerConfig)
	cfg.IsValidator = forValidator
	if err := cfg.Configuration(vp, spec); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *PeerConfig) Configuration(vp *viper.Viper, spec *config.ServerSpec) error {
	rootList := vp.GetStringSlice("discovery.rootnode")
	if len(rootList) == 1 {
		//try splitting rootList with old fashion conf
		rootList = strings.Split(rootList[0], ",")
	}
	c.Discovery.Roots = rootList
	c.Discovery.Persist = vp.GetBool("discovery.persist")
	c.Discovery.Hidden = vp.GetBool("discovery.hidden")
	c.Discovery.Disable = vp.GetBool("discovery.disable")
	c.Discovery.TouchPeriod = vp.GetDuration("discovery.touchPeriod")
	c.Discovery.MaxNodes = vp.GetInt("discovery.touchMaxNodes")

	var peerType pb.PeerEndpoint_Type
	if c.IsValidator {
		peerType = pb.PeerEndpoint_VALIDATOR
	} else {
		peerType = pb.PeerEndpoint_NON_VALIDATOR
	}

	// automatic correction for the ID prefix
	var peerPrefix = ""
	if c.Discovery.Hidden {
		if c.Discovery.Disable {
			peerPrefix = "NVP" // Non-validator peer
		} else {
			peerPrefix = "NSP" // name service peer
		}
	} else {
		if c.Discovery.Disable {
			peerPrefix = "BRP" // bridge peer
		}
	}
	var peerID = vp.GetString("id")
	if len(peerID) > 3 && strings.Compare(peerPrefix, peerID[:3]) != 0 {
		peerID = peerPrefix + peerID
	}

	c.PeerEndpoint = &pb.PeerEndpoint{ID: &pb.PeerID{Name: peerID}, Address: spec.ExternalAddr, Type: peerType}
	peerLogger.Infof("Init peer endpoint: %s", c.PeerEndpoint)

	clispec := spec.GetClient()
	if clispec.EnableTLS {
		c.NewPeerClientConn = func(peerAddress string) (*grpc.ClientConn, error) {
			tlscred, err := clispec.GetClientTLSOptions()
			if err != nil {
				return nil, err
			}
			//func is used by chatwith, which has its own goroutine
			return grpc.Dial(peerAddress,
				grpc.WithTransportCredentials(tlscred),
				grpc.WithTimeout(time.Second*3), //TODO
				grpc.WithBlock())
		}

	}

	return nil
}

// SecurityEnabled returns the securityEnabled property from cached configuration
func securityEnabled() bool {
	return config.SecurityEnabled()
}
