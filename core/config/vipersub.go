package config

import (
	"github.com/spf13/viper"
	"strings"
)

var settingCache = map[*viper.Viper]map[string]interface{}{}

func CacheViper(thevp ...*viper.Viper) {

	if len(thevp) == 0 {
		settingCache[viper.GetViper()] = viper.AllSettings()
	} else {
		settingCache[thevp[0]] = thevp[0].AllSettings()
	}
}

func viperSearch(path []string, m map[string]interface{}) map[string]interface{} {
	for _, k := range path {
		m2, ok := m[k]
		if !ok {
			return nil
		}
		m3, ok := m2.(map[string]interface{})
		if !ok {
			return nil
		}
		// continue search from here
		m = m3
	}
	return m
}

func SubViper(path string, thevp ...*viper.Viper) *viper.Viper {

	//we just consider the first vp (or noting)
	var parent *viper.Viper
	if len(thevp) == 0 {
		parent = viper.GetViper()
	} else {
		parent = thevp[0]
	}

	vp := viper.New()
	InitViperForEnv(MergeViperPrefix(path, parent), vp)

	var sets map[string]interface{}
	var ok bool
	if sets, ok = settingCache[parent]; !ok {
		sets = parent.AllSettings()
	}

	path = strings.ToLower(path)
	if sets = viperSearch(strings.Split(path, "."), sets); sets != nil {
		for k, v := range sets {
			vp.Set(k, v)
		}
	}

	return vp
}
