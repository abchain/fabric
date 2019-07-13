package config

import (
	"fmt"
	"github.com/spf13/viper"
)

type ClientSpec struct {
	Address string
	tlsSpec
}

func (s *ClientSpec) Init(vp *viper.Viper) error {

	s.Address = vp.GetString("address")
	if s.Address == "" {
		return fmt.Errorf("can not find address configuration")
	}
	logger.Debugf("Set client's address as [%s]", s.Address)

	if vp.IsSet("tls") {
		logger.Debugf("Read tls configuration for client")
		s.tlsSpec.Init(SubViper("tls", vp))
	}

	return nil
}
