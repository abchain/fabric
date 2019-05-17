package node

import (
	"fmt"
	"github.com/abchain/fabric/core/config"
	"github.com/op/go-logging"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"net"

	"github.com/abchain/fabric/core/comm"
	"github.com/abchain/fabric/core/util"
)

var serviceLogger = logging.MustGetLogger("service")
var servers []*grpc.Server

type servicePoint struct {
	*grpc.Server
	spec      *config.ServerSpec
	lPort     net.Listener
	srvStatus error
}

type ServicePoint struct {
	*servicePoint
}

func (ep ServicePoint) Spec() *config.ServerSpec {
	return ep.spec
}

func (ep ServicePoint) Status() error {
	return ep.srvStatus
}

func (ep ServicePoint) CleanStatus() {
	ep.srvStatus = nil
}

func (ep ServicePoint) Release() error {
	return ep.lPort.Close()
}

func CreateServerPoint(conf *viper.Viper) (ServicePoint, error) {
	srvp := new(servicePoint)
	err := srvp.Init(conf)
	if err != nil {
		return ServicePoint{}, err
	}
	return ServicePoint{srvp}, nil
}

func (ep *servicePoint) InitWithConfig(conf *config.ServerSpec) error {

	ep.spec = conf

	if err := ep.SetPort(conf.Address); err != nil {
		return err
	}

	var opts []grpc.ServerOption

	//tls
	if conf.EnableTLS {

		creds, err := conf.GetServerTLSOptions()
		if err != nil {
			return fmt.Errorf("Failed to generate peer's credentials: %v", err)
		}
		opts = append(opts, grpc.Creds(creds))

	}

	//other options (msg size, etc ...)
	msgMaxsize := conf.MessageSize
	if msgMaxsize > 1024*1024*4 {
		//in p2p network we usually require a sync channel of recv and send
		opts = append(opts, grpc.MaxRecvMsgSize(msgMaxsize), grpc.MaxSendMsgSize(msgMaxsize))
	}

	ep.Server = grpc.NewServer(opts...)
	return nil
}

//standard init read all configurations from viper, inwhich we use a subtree of *peer*
//in the 0.6's configurations
func (ep *servicePoint) Init(conf *viper.Viper) error {

	spec := new(config.ServerSpec)
	if err := spec.Init(conf); err != nil {
		return err
	}

	return ep.InitWithConfig(spec)
}

//some routines for create a simple grpc server (currently only port, no tls and other options enable)
func (ep *servicePoint) SetPort(listenAddr string) error {
	var err error
	ep.lPort, err = net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("Failed to listen on %s: %v", listenAddr, err)
	}
	ep.Server = grpc.NewServer()
	return nil
}

func (ep *servicePoint) Start(notify chan<- ServicePoint) error {

	if ep.Server == nil {
		return fmt.Errorf("Server is not inited")
	}

	go func() {
		if ep.srvStatus == nil {
			serviceLogger.Infof("service [%s] has startted", ep.spec.Address)
			ep.srvStatus = ep.Serve(ep.lPort)
		} else {
			serviceLogger.Errorf("service [%s] can not start again for previous error: %s", ep.srvStatus)
		}
		serviceLogger.Infof("service [%s] has exited", ep.spec.Address)
		notify <- ServicePoint{ep}
	}()

	return nil
}

func (ep *servicePoint) Stop() error {

	if ep.Server == nil {
		return fmt.Errorf("Server is not inited")
	}

	serviceLogger.Infof("User stop service [%s]", ep.spec.Address)
	ep.Server.Stop()
	return nil
}

//we still reserved the global APIs for node-wide services
func GetServiceTLSCred() (credentials.TransportCredentials, error) {

	return credentials.NewServerTLSFromFile(
		util.CanonicalizeFilePath(viper.GetString("peer.tls.cert.file")),
		util.CanonicalizeFilePath(viper.GetString("peer.tls.key.file")))

}

func StopServices() {

	serviceLogger.Infof("Stop all services")

	for _, s := range servers {
		s.Stop()
	}

	servers = nil
}

func StartService(listenAddr string, enableTLS bool, regserv ...func(*grpc.Server)) error {
	if "" == listenAddr {
		return fmt.Errorf("Listen address for service not specified")
	}

	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		serviceLogger.Fatalf("Failed to listen: %v", err)
		return err
	}

	var opts []grpc.ServerOption

	if enableTLS {
		creds, err := GetServiceTLSCred()

		if err != nil {
			serviceLogger.Fatalf("Failed to generate credentials %v", err)
			return err
		}
		opts = []grpc.ServerOption{grpc.Creds(creds)}
	}

	msgsize := comm.MaxMessageSize()
	opts = append(opts, grpc.MaxRecvMsgSize(msgsize), grpc.MaxSendMsgSize(msgsize))

	grpcServer := grpc.NewServer(opts...)

	servers = append(servers, grpcServer)

	for _, f := range regserv {
		f(grpcServer)
	}

	serviceLogger.Infof("Starting service with address=%s", listenAddr)

	if grpcErr := grpcServer.Serve(lis); grpcErr != nil {
		return fmt.Errorf("grpc server exited with error: %s", grpcErr)
	} else {
		serviceLogger.Info("grpc server exited")
	}

	return nil
}
