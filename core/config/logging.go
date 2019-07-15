package config

import (
	logging "github.com/op/go-logging"
)

var defaultLoggingLvl = map[string]logging.Level{}
var moduleDebugging = map[string]bool{}

func DebugModule(moduleName string) {

	if _, ok := defaultLoggingLvl[moduleName]; !ok {
		//obtain the preset level of sepcified module
		logger.Warningf("Start debugging module [%s] (log level set to DEBUG)", moduleName)
		defaultLoggingLvl[moduleName] = logging.GetLevel(moduleName)
	}

	logging.SetLevel(logging.DEBUG, moduleName)
	moduleDebugging[moduleName] = true
}

func RestoreMoudleLogging(moduleName string) {

	delete(moduleDebugging, moduleName)

	if lvl, ok := defaultLoggingLvl[moduleName]; ok {
		logging.SetLevel(lvl, moduleName)
		logger.Infof("Module [%s] is restored to original state", moduleName)
	} else {
		logger.Errorf("No preset level for module [%s] (Not debug?)", moduleName)
	}

}

func RestoreAllModule() {

	for n, _ := range moduleDebugging {
		RestoreMoudleLogging(n)
	}

	moduleDebugging = map[string]bool{}
}
