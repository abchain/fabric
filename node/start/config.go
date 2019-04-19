package startnode

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/op/go-logging"
	"github.com/spf13/viper"

	"github.com/abchain/fabric/core/config"
	"github.com/abchain/fabric/flogging"
)

var consolelogger = logging.MustGetLogger("fabricconsole")

func initViper(envprefix string, filename string, configPath ...string) error {

	config.InitViperForEnv(envprefix)

	for _, c := range configPath {
		viper.AddConfigPath(c)
	}

	viper.SetConfigName(filename) // Name of config file (without extension)

	return viper.ReadInConfig() // Find and read the config file

}

func useSourceExample() []string {
	examplePaths := make([]string, 1, 10)
	examplePaths[0] = "./" // Path to look for the config file in
	// Path to look for the config file in based on GOPATH
	gopath := os.Getenv("GOPATH")
	for _, p := range filepath.SplitList(gopath) {
		example := filepath.Join(p, "src/github.com/abchain/fabric/examples/config")
		examplePaths = append(examplePaths, example)
	}

	return examplePaths
}

//deprecated: use the local package in peerex/logging
func InitLogger(module string) *logging.Logger {
	return logging.MustGetLogger(module)
}

type GlobalConfig struct {
	EnvPrefix          string
	ConfigFileName     string
	ConfigPath         []string
	SkipConfigFile     bool
	NotUseSourceConfig bool
	DefaultSetting     map[string]interface{}

	Perf PerformanceTuning

	LogRole string //peer, node, network, chaincode, version, can apply different log level from config file
}

func (g *GlobalConfig) Apply() error {

	for k, v := range g.DefaultSetting {
		viper.SetDefault(k, v)
	}

	if g.EnvPrefix == "" {
		g.EnvPrefix = viperEnvPrefix
	}

	if g.ConfigFileName == "" {
		g.ConfigFileName = viperFileName
	}

	if g.ConfigPath == nil {
		g.ConfigPath = []string{"."}
	}

	if !g.NotUseSourceConfig {
		g.ConfigPath = append(g.ConfigPath, useSourceExample()...)
	}

	err := initViper(g.EnvPrefix, g.ConfigFileName, g.ConfigPath...)
	if !g.SkipConfigFile && err != nil {
		return err
	} else if err != nil {
		defer consolelogger.Warningf("Can not read viper file (%s), skip", err)
	}

	if g.LogRole == "" {
		g.LogRole = "node"
	}

	flogging.LoggingInit(g.LogRole)
	if err = flogging.LoggingFileInit(config.GlobalFileSystemPath()); err != nil {
		consolelogger.Warning("Could not output log to file:", err)
	}

	g.Perf.Apply()

	consolelogger.Info("Global init done ...")

	return nil
}

type PerformanceTuning struct {
	MaxProcs int
}

func (p *PerformanceTuning) Apply() {

	if p.MaxProcs == 0 {
		p.MaxProcs = viper.GetInt("node.gomaxprocs")
		if p.MaxProcs == 0 {
			p.MaxProcs = viper.GetInt("peer.gomaxprocs")
		}
	}
	consolelogger.Debugf("set maxprocs as %d", p.MaxProcs)
	runtime.GOMAXPROCS(p.MaxProcs)
}
