package framework

import (
	"github.com/abchain/fabric/core/config"
	"github.com/op/go-logging"
	"github.com/spf13/viper"
)

var logger = logging.MustGetLogger("consensus/framework")

type FrameworkConfig struct {
	vp *viper.Viper
}

func NewConfig(vp *viper.Viper) FrameworkConfig {
	if vp == nil {
		vp = viper.GetViper()
	} else {
		config.CacheViper(vp)
	}

	ret := FrameworkConfig{vp}
	return ret
}

func (c FrameworkConfig) SubConfig(sub string) *viper.Viper {
	return config.SubViper(sub, c.vp)
}
