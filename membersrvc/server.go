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

package main

import (
	"net"
	"os"
	"path/filepath"
	"runtime"

	"strings"

	// "github.com/abchain/fabric/core/crypto"
	"github.com/abchain/fabric/core/util"
	"github.com/abchain/fabric/flogging"
	"github.com/abchain/fabric/membersrvc/ca"
	"github.com/abchain/fabric/metadata"
	"github.com/op/go-logging"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var logger = logging.MustGetLogger("server")

func main() {

	viper.SetEnvPrefix(envPrefix)
	viper.AutomaticEnv()
	replacer := strings.NewReplacer(".", "_")
	viper.SetEnvKeyReplacer(replacer)
	viper.SetConfigName(cfgFileName)
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./")
	// Path to look for the config file based on GOPATH
	gopath := os.Getenv("GOPATH")
	for _, p := range filepath.SplitList(gopath) {
		cfgpath := filepath.Join(p, "src/github.com/abchain/fabric/membersrvc")
		viper.AddConfigPath(cfgpath)
	}
	err := viper.ReadInConfig()
	if err != nil {
		logger.Panicf("Fatal error when reading %s config file: %s", "membersrvc", err)
	}

	flogging.LoggingInit("server")

	// Init the crypto layer
	// if err := ca.Init(); err != nil {
	// 	logger.Panicf("Failed initializing the crypto layer [%s]", err)
	// }

	// cache configure
	ca.CacheConfiguration()

	logger.Infof("CA Server (" + metadata.Version + ")")

	eca := ca.NewECA()
	defer eca.Stop()

	tca := ca.NewTCA(eca)
	defer tca.Stop()

	tlsca := ca.NewTLSCA(eca)
	defer tlsca.Stop()

	runtime.GOMAXPROCS(viper.GetInt("server.gomaxprocs"))

	var opts []grpc.ServerOption

	if viper.GetBool("security.tls_enabled") {
		logger.Debug("TLS was enabled [security.tls_enabled == true]")
		creds, err := credentials.NewServerTLSFromFile(
			util.CanonicalizeFilePath(viper.GetString("server.tls.cert.file")), 
			util.CanonicalizeFilePath(viper.GetString("server.tls.key.file")))
		if err != nil {
			logger.Panic(err)
		}
		opts = []grpc.ServerOption{grpc.Creds(creds)}
	} else {
		logger.Debug("TLS was not enabled [security.tls_enabled == false]")
	}

	srv := grpc.NewServer(opts...)

	if viper.GetBool("aca.enabled") {
		logger.Info("ACA was enabled [aca.enabled == true]")
	 	aca := ca.NewACA()
	 	defer aca.Stop()
		aca.Start(srv)
	}
	if viper.GetBool("admin.enabled") {
		logger.Info("Admin was enabled [admin.enabled == true]")
		admin := ca.NewAdmin()
		defer admin.Stop()
		admin.Start(srv)
	}

	eca.Start(srv)
	tca.Start(srv)
	tlsca.Start(srv)

	if sock, err := net.Listen("tcp", viper.GetString("server.port")); err != nil {
		logger.Errorf("Fail to start CA Server: %s", err)
		os.Exit(1)
	} else {
		srv.Serve(sock)
		sock.Close()
	}
}
