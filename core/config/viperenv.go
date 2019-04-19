package config

import (
	"github.com/spf13/viper"
	"strings"
)

//more additional work must be done to made subviper support enviroment correctly
const envprefixKey = "YAFABRIC_ENVPREFIX_SET"

var defaultReplacer = strings.NewReplacer(".", "_")

func InitViperForEnv(prefix string, vp ...*viper.Viper) {

	var thevp *viper.Viper
	if len(vp) == 0 {
		thevp = viper.GetViper()
	} else {
		thevp = vp[0]
	}

	thevp.SetDefault(envprefixKey, strings.ToUpper(prefix))

	thevp.SetEnvPrefix(prefix)
	thevp.SetEnvKeyReplacer(defaultReplacer)
	thevp.AutomaticEnv()
}

func MergeViperPrefix(path string, vp *viper.Viper) string {

	root := vp.GetString(envprefixKey)
	if root != "" {
		path = root + "." + path
	}

	return defaultReplacer.Replace(path)
}
