package cred_default_test

import (
	"testing"

	"github.com/abchain/fabric/core/config"
	_ "github.com/abchain/fabric/core/cred/default"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"net"
	"time"
)

func testConnect(spec *config.ServerSpec, clispec *config.ClientSpec, t *testing.T, expectedFail bool) {

	sopt, err := spec.GetServerTLSOptions()
	if err != nil {
		t.Fatal(err)
	}

	endChan := make(chan interface{})
	defer func() {
		<-endChan
	}()

	grpcServer := grpc.NewServer(grpc.Creds(sopt))

	lis, err := net.Listen("tcp", spec.Address)
	if err != nil {
		t.Fatal(err)
	}
	defer lis.Close()

	go func() {
		err := grpcServer.Serve(lis)
		t.Logf("server exit: %s", err)
		close(endChan)
	}()

	if clispec == nil {
		clispec = spec.GetClient()
	}

	copt, err := clispec.GetClientTLSOptions()
	if err != nil {
		t.Fatal(err)
	}

	dctx, endf := context.WithTimeout(context.Background(), time.Second*3)
	defer endf()

	conn, err := grpc.DialContext(dctx, clispec.Address,
		grpc.WithTransportCredentials(copt), grpc.WithBlock())
	if expectedFail {
		if err == nil {
			t.Fatal("unexpected success")
		}
		t.Log(err)
	} else {
		if err != nil {
			t.Fatal(err)
		}

		conn.Close()
	}

}

func testSrvConnect(spec *config.ServerSpec, t *testing.T) {
	testConnect(spec, nil, t, false)
}

func Test_ConfigTLS(t *testing.T) {

	srvSpec := new(config.ServerSpec)
	if err := srvSpec.Init(config.SubViper("tlsconfig1")); err != nil {
		t.Fatal(err)
	}

	testSrvConnect(srvSpec, t)

}

func Test_ConfigTLS2(t *testing.T) {

	srvSpec := new(config.ServerSpec)
	if err := srvSpec.Init(config.SubViper("tlsconfig2")); err != nil {
		t.Fatal(err)
	}

	testSrvConnect(srvSpec, t)

}

func Test_ConfigTLS3(t *testing.T) {

	srvSpec := new(config.ServerSpec)
	if err := srvSpec.Init(config.SubViper("tlsconfig3")); err != nil {
		t.Fatal(err)
	}

	cliSpec := new(config.ClientSpec)
	if err := cliSpec.Init(config.SubViper("tlsconfig3cli")); err != nil {
		t.Fatal(err)
	}

	testConnect(srvSpec, cliSpec, t, false)

	cliSpec.TLSHostOverride = ""
	testConnect(srvSpec, cliSpec, t, true)
}
