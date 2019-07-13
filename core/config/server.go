package config

import (
	"fmt"
	"github.com/spf13/viper"
	"net"
)

//a server's specification: including the listen address,
//declaimed address (which a external accessor should use),
//TLS scheme etc...
type ServerSpec struct {
	Address      string
	ExternalAddr string
	MessageSize  int
	AutoDetect   bool
	tlsSpec
}

//Init the spec from viper
func (s *ServerSpec) Init(vp *viper.Viper) error {

	s.ExternalAddr = vp.GetString("address")
	if s.ExternalAddr == "" {
		return fmt.Errorf("can not find address configuration")
	}

	s.AutoDetect = viper.GetBool("addressAutoDetect")
	if s.AutoDetect {
		// Need to get the port from the peer.address setting, and append to the determined host IP
		_, port, err := net.SplitHostPort(s.ExternalAddr)
		if err != nil {
			return fmt.Errorf("Error auto detecting Peer's address: %s", err)
		}
		s.ExternalAddr = net.JoinHostPort(GetLocalIP(), port)
		logger.Infof("Auto detected peer address: %s", s.ExternalAddr)
	}

	s.Address = vp.GetString("listenAddress")
	if s.Address == "" {
		s.Address = s.ExternalAddr
	}
	logger.Debugf("Set serverspec's address as [%s %s]", s.Address, s.ExternalAddr)

	if vp.IsSet("tls") {
		logger.Debugf("Read tls configuration for serverspec")
		s.tlsSpec.Init(SubViper("tls", vp))
	}

	s.MessageSize = vp.GetInt("messagesizelimit")

	return nil
}

func (s *ServerSpec) GetClient() *ClientSpec {

	ret := new(ClientSpec)
	ret.Address = s.ExternalAddr
	ret.tlsSpec = s.tlsSpec
	return ret
}
