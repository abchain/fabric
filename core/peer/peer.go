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

package peer

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/context"

	"google.golang.org/grpc"

	"github.com/golang/protobuf/proto"
	"github.com/op/go-logging"

	"github.com/abchain/fabric/core/comm"
	cred "github.com/abchain/fabric/core/cred"
	"github.com/abchain/fabric/core/db"
	"github.com/abchain/fabric/core/discovery"
	"github.com/abchain/fabric/core/peer/acl"
	"github.com/abchain/fabric/core/util"
	pb "github.com/abchain/fabric/protos"
)

// Peer provides interface for a peer
type Peer interface {
	GetPeerEndpoint() (*pb.PeerEndpoint, error)
	GetNeighbour() (Neighbour, error)
	//init stream stubs, with options ...
	AddStreamStub(string, pb.StreamHandlerFactory, ...interface{}) error
	GetStreamStub(string) *pb.StreamStub
	GetPeerCtx() context.Context
}

type StreamFilter interface {
	QualitifiedPeer(*pb.PeerEndpoint) bool
}

type StreamPostHandler interface {
	NotifyNewPeer(*pb.PeerID)
}

type Neighbour interface {
	Broadcast(*pb.Message, pb.PeerEndpoint_Type) []error
	Unicast(*pb.Message, *pb.PeerID) error
	GetPeerEndpoint() (*pb.PeerEndpoint, error) //for convinient, we also include this method
	GetPeers() (*pb.PeersMessage, error)
	GetDiscoverer() (Discoverer, error)
	GetACL() (acl.AccessControl, error)
}

// ChatStream interface supported by stream between Peers
type ChatStream interface {
	Send(*pb.Message) error
	Recv() (*pb.Message, error)
	Context() context.Context
}

var peerLogger = logging.MustGetLogger("peer")

// NewPeerClientConnectionWithAddress Returns a new grpc.ClientConn to the configured PEER.
func NewPeerClientConnectionWithAddress(peerAddress string) (*grpc.ClientConn, error) {
	if comm.TLSEnabledForLocalSrv() {
		return comm.NewClientConnectionWithAddress(peerAddress, true, true, comm.InitTLSForPeer())
	}
	return comm.NewClientConnectionWithAddress(peerAddress, true, false, nil)
}

type handlerMap struct {
	sync.RWMutex
	m              map[pb.PeerID]MessageHandler
	glareMap       map[pb.PeerID]MessageHandler

	cachedPeerList []*pb.PeerEndpoint
}

// Impl implementation of the Peer service
type Impl struct {
	self  *pb.PeerEndpoint
	pctx  context.Context
	onEnd context.CancelFunc
	//	handlerFactory HandlerFactory
	handlerMap *handlerMap
	//  each stubs ...
	streamStubs        map[string]*pb.StreamStub
	streamFilters      map[string]StreamFilter
	streamPostHandlers map[string]StreamPostHandler
	gossipStub         *pb.StreamStub
	syncStub           *pb.StreamStub

	secHelper     cred.PeerCreds
	reconnectOnce sync.Once
	discHelper    struct {
		discovery.Discovery
		touchPeriod   time.Duration
		touchMaxNodes int
		hidden        bool
		disable       bool
	}
	aclHelper acl.AccessControl
	persistor db.Persistor
}

// MessageHandler standard interface for handling Openchain messages.
type MessageHandler interface {
	LegacyMessageHandler
	SendMessage(msg *pb.Message) error
	To() (pb.PeerEndpoint, error)
	Stop() error
	CloseSend()
	IsActive() bool
}

var PeerGlobalParentCtx = context.Background()

func NewPeer(self *pb.PeerEndpoint) *Impl {

	peer := new(Impl)

	peer.self = self
	peer.handlerMap = &handlerMap{
		m: make(map[pb.PeerID]MessageHandler),
		glareMap: make(map[pb.PeerID]MessageHandler),
	}

	pctx, endf := context.WithCancel(PeerGlobalParentCtx)
	peer.pctx = pctx
	peer.onEnd = endf
	peer.persistor = db.NewPersistor(db.PeerStoreKeyPrefix)

	//mapping of all streamstubs above:
	peer.streamStubs = make(map[string]*pb.StreamStub)
	peer.streamFilters = make(map[string]StreamFilter)
	peer.streamPostHandlers = make(map[string]StreamPostHandler)

	return peer
}

// NewPeerWithEngine returns a Peer which uses the supplied handler factory function for creating new handlers on new Chat service invocations.
func CreateNewPeer(cred cred.PeerCreds, config *PeerConfig) (peer *Impl, err error) {

	peer = NewPeer(config.PeerEndpoint)
	peer.secHelper = cred
	// Install security object for peer
	if securityEnabled() {
		if peer.secHelper == nil {
			return nil, fmt.Errorf("Security helper not provided")
		}
	}

	return peer, nil

}

func (p *Impl) RunPeer(config *PeerConfig) {

	peerNodes := p.initDiscovery(config)
	p.chatWithSomePeers(peerNodes)
}

func (p *Impl) EndPeer() {
	if p.onEnd != nil {
		p.onEnd()
	}

}

// Chat implementation of the the Chat bidi streaming RPC function
func (p *Impl) Chat(stream pb.Peer_ChatServer) error {
	return p.handleChat(stream.Context(), stream, false)
}

func (p *Impl) ProcessTransaction(context.Context, *pb.Transaction) (*pb.Response, error) {
	return nil, fmt.Errorf("Method is abandoned")
}

func (p *Impl) GetPeerCtx() context.Context { return p.pctx }

// GetPeers returns the currently registered PeerEndpoints which are also in peer discovery list
func (p *Impl) GetPeers() (*pb.PeersMessage, error) {

	p.handlerMap.RLock()
	defer p.handlerMap.RUnlock()

	if p.handlerMap.cachedPeerList == nil {

		peers, err := p.genPeersList()
		if err != nil {
			return nil, err
		}

		//upgrading rlock to wlock
		p.handlerMap.RUnlock()
		p.handlerMap.Lock()

		p.handlerMap.cachedPeerList = peers

		p.handlerMap.Unlock()
		p.handlerMap.RLock()
	}

	return &pb.PeersMessage{Peers: p.handlerMap.cachedPeerList}, nil
}

//requier rlock to handlerMap
func (p *Impl) genPeersList() ([]*pb.PeerEndpoint, error) {

	peers := []*pb.PeerEndpoint{}
	for _, msgHandler := range p.handlerMap.m {
		peerEndpoint, err := msgHandler.To()
		if err != nil {
			return nil, fmt.Errorf("Error getting peers: %s", err)
		}

		if p.discHelper.FindNode(peerEndpoint.Address) {
			peers = append(peers, &peerEndpoint)
		}
	}

	return peers, nil
}

func getPeerAddresses(peersMsg *pb.PeersMessage) []string {
	peers := peersMsg.GetPeers()
	addresses := make([]string, len(peers))
	for i, v := range peers {
		addresses[i] = v.Address
	}
	return addresses
}

// PeersDiscovered used by MessageHandlers for notifying this coordinator of discovered PeerEndoints. May include this Peer's PeerEndpoint.
func (p *Impl) PeersDiscovered(peersMessage *pb.PeersMessage) error {

	p.handlerMap.RLock()
	defer p.handlerMap.RUnlock()
	for _, peerEndpoint := range peersMessage.Peers {
		// Filter out THIS Peer's endpoint
		if *getHandlerKeyFromPeerEndpoint(p.self) == *getHandlerKeyFromPeerEndpoint(peerEndpoint) {
			// NOOP
		} else if _, ok := p.handlerMap.m[*getHandlerKeyFromPeerEndpoint(peerEndpoint)]; ok == false {
			// Start chat with Peer
			p.chatWithSomePeers([]string{peerEndpoint.Address})
		}
	}
	return nil
}

func getHandlerKey(peerMessageHandler MessageHandler) (*pb.PeerID, error) {
	peerEndpoint, err := peerMessageHandler.To()
	if err != nil {
		return &pb.PeerID{}, fmt.Errorf("Error getting messageHandler key: %s", err)
	}
	return peerEndpoint.ID, nil
}

func getHandlerKeyFromPeerEndpoint(peerEndpoint *pb.PeerEndpoint) *pb.PeerID {
	return peerEndpoint.ID
}

func (p *Impl) AddStreamStub(name string, factory pb.StreamHandlerFactory, opts ...interface{}) error {
	if _, ok := p.streamStubs[name]; ok {
		return fmt.Errorf("streamstub %s is exist", name)
	}

	for _, opt := range opts {
		switch v := opt.(type) {
		case StreamFilter:
			p.streamFilters[name] = v
		case StreamPostHandler:
			p.streamPostHandlers[name] = v
		default:
			return fmt.Errorf("Unrecognized option: %v", opt)
		}
	}

	stub := pb.NewStreamStub(factory, p.self.ID)
	p.streamStubs[name] = stub
	peerLogger.Infof("Add a new streamstub [%s]", name)
	return nil
}

func (p *Impl) GetStreamStub(name string) *pb.StreamStub {
	return p.streamStubs[name]
}

func (p *Impl) GetDiscoverer() (Discoverer, error) {
	return p, nil
}

func (p *Impl) GetACL() (acl.AccessControl, error) {
	return p.aclHelper, nil
}

func (p *Impl) GetNeighbour() (Neighbour, error) {
	return p, nil
}

func (p *Impl) overwrite(acitve bool, peerKey *pb.PeerID, existing MessageHandler) bool {

	overwrite := false
	if strings.Compare(p.self.ID.Name, peerKey.Name) > 0 {
		if acitve && !existing.IsActive() {
			overwrite = true
		}
	} else if strings.Compare(p.self.ID.Name, peerKey.Name) < 0 {
		if !acitve && existing.IsActive(){
			overwrite = true
		}
	}
	return overwrite
}



// RegisterHandler register a MessageHandler with this coordinator
func (p *Impl) RegisterHandler(ctx context.Context, initiated bool, messageHandler MessageHandler) error {
	key, err := getHandlerKey(messageHandler)
	if err != nil {
		return fmt.Errorf("Error registering handler: %s", err)
	}
	p.handlerMap.Lock()
	defer p.handlerMap.Unlock()

	if existing, ok := p.handlerMap.m[*key]; ok {
		if p.overwrite(initiated, key, existing) {
			p.handlerMap.glareMap[*key] = existing

			peerLogger.Debugf("close glared handler with key: %s, active: %t", key, initiated)
			// close the previous connection
			go existing.CloseSend()
		} else {
			//Duplicate, return error
			return newDuplicateHandlerError(messageHandler)
		}
	}

	p.handlerMap.m[*key] = messageHandler
	p.handlerMap.cachedPeerList = nil
	peerLogger.Debugf("registered handler with key: %s, active: %t", key, initiated)

	if !initiated {
		return nil
	}

	//also start all other stream stubs
	ctxk := peerConnCtxKey("conn")
	v := ctx.Value(ctxk)

	if v == nil {
		peerLogger.Errorf("No connection can be found in context")
	} else {

		clidone := make(chan error)

		for name, stub := range p.streamStubs {

			//do filter first
			ep, _ := messageHandler.To()
			if filter, ok := p.streamFilters[name]; ok && !filter.QualitifiedPeer(&ep) {

				peerLogger.Debugf("ignore streamhandler %s for remote peer %s", name, key.GetName())
				continue
			}

			go func(conn *grpc.ClientConn, stub *pb.StreamStub, name string) {
				peerLogger.Debugf("start <%s> streamhandler for peer %s", name, key.GetName())
				err, retf := stub.HandleClient(conn, key)
				defer retf()
				clidone <- err
			}(v.(*grpc.ClientConn), stub, name)

			err := <-clidone
			if err != nil {
				peerLogger.Errorf("running streamhandler <%s> fail: %s", name, err)
			} else if posth, ok := p.streamPostHandlers[name]; ok {
				posth.NotifyNewPeer(key)
			}
		}
	}

	return nil
}

// DeregisterHandler deregisters an already registered MessageHandler for this coordinator
func (p *Impl) DeregisterHandler(messageHandler MessageHandler) error {
	key, err := getHandlerKey(messageHandler)
	if err != nil {
		return fmt.Errorf("Error deregistering handler: %s", err)
	}
	p.handlerMap.Lock()
	defer p.handlerMap.Unlock()

	if _, ok := p.handlerMap.glareMap[*key]; ok {
		// found it in glareMap first
		delete(p.handlerMap.glareMap, *key)
		peerLogger.Debugf("Deregistered glared handler with key: %s", key)
		p.handlerMap.cachedPeerList = nil
		return nil
	}

	if _, ok := p.handlerMap.m[*key]; !ok {
		// Handler NOT found
		return fmt.Errorf("Error deregistering handler, could not find handler with key: %s", key)
	}
	delete(p.handlerMap.m, *key)
	p.handlerMap.cachedPeerList = nil
	peerLogger.Debugf("Deregistered handler with key: %s", key)
	return nil
}

// Clone the handler map to avoid locking across SendMessage
func (p *Impl) cloneHandlerMap(typ pb.PeerEndpoint_Type) map[pb.PeerID]MessageHandler {
	p.handlerMap.RLock()
	defer p.handlerMap.RUnlock()
	clone := make(map[pb.PeerID]MessageHandler)
	for id, msgHandler := range p.handlerMap.m {
		//pb.PeerEndpoint_UNDEFINED collects all peers
		if typ != pb.PeerEndpoint_UNDEFINED {
			toPeerEndpoint, _ := msgHandler.To()
			//ignore endpoints that don't match type filter
			if typ != toPeerEndpoint.Type {
				continue
			}
		}
		clone[id] = msgHandler
	}
	return clone
}

// Broadcast broadcast a message to each of the currently registered PeerEndpoints of given type
// Broadcast will broadcast to all registered PeerEndpoints if the type is PeerEndpoint_UNDEFINED
func (p *Impl) Broadcast(msg *pb.Message, typ pb.PeerEndpoint_Type) []error {
	cloneMap := p.cloneHandlerMap(typ)
	errorsFromHandlers := make(chan error, len(cloneMap))
	var bcWG sync.WaitGroup

	start := time.Now()

	for _, msgHandler := range cloneMap {
		bcWG.Add(1)
		go func(msgHandler MessageHandler) {
			defer bcWG.Done()
			host, _ := msgHandler.To()
			t1 := time.Now()
			err := msgHandler.SendMessage(msg)
			if err != nil {
				toPeerEndpoint, _ := msgHandler.To()
				errorsFromHandlers <- fmt.Errorf("Error broadcasting msg (%s) to PeerEndpoint (%s): %s", msg.Type, toPeerEndpoint, err)
			}
			peerLogger.Debugf("Sending %d bytes to %s took %v", len(msg.Payload), host.Address, time.Since(t1))

		}(msgHandler)

	}
	bcWG.Wait()
	close(errorsFromHandlers)
	var returnedErrors []error
	for err := range errorsFromHandlers {
		returnedErrors = append(returnedErrors, err)
	}

	elapsed := time.Since(start)
	peerLogger.Debugf("Broadcast took %v", elapsed)

	return returnedErrors
}

func (p *Impl) getMessageHandler(receiverHandle *pb.PeerID) (MessageHandler, error) {
	p.handlerMap.RLock()
	defer p.handlerMap.RUnlock()
	msgHandler, ok := p.handlerMap.m[*receiverHandle]
	if !ok {
		return nil, fmt.Errorf("Message handler not found for receiver %s", receiverHandle.Name)
	}
	return msgHandler, nil
}

// Unicast sends a message to a specific peer.
func (p *Impl) Unicast(msg *pb.Message, receiverHandle *pb.PeerID) error {
	msgHandler, err := p.getMessageHandler(receiverHandle)
	if err != nil {
		return err
	}
	err = msgHandler.SendMessage(msg)
	if err != nil {
		toPeerEndpoint, _ := msgHandler.To()
		return fmt.Errorf("Error unicasting msg (%s) to PeerEndpoint (%s): %s", msg.Type, toPeerEndpoint, err)
	}
	return nil
}

// SendTransactionsToPeer forwards transactions to the specified peer address.
func (p *Impl) SendTransactionsToPeer(peerAddress string, transaction *pb.Transaction) (response *pb.Response) {
	conn, err := NewPeerClientConnectionWithAddress(peerAddress)
	if err != nil {
		return &pb.Response{Status: pb.Response_FAILURE, Msg: []byte(fmt.Sprintf("Error creating client to peer address=%s:  %s", peerAddress, err))}
	}
	defer conn.Close()
	serverClient := pb.NewPeerClient(conn)
	peerLogger.Debugf("Sending TX to Peer: %s", peerAddress)
	response, err = serverClient.ProcessTransaction(context.Background(), transaction)
	if err != nil {
		return &pb.Response{Status: pb.Response_FAILURE, Msg: []byte(fmt.Sprintf("Error calling ProcessTransaction on remote peer at address=%s:  %s", peerAddress, err))}
	}
	return response
}

func (p *Impl) ensureConnected() {

	tickChan := time.NewTicker(p.discHelper.touchPeriod).C
	peerLogger.Debugf("Starting Peer reconnect service (touch service), with period = %s", p.discHelper.touchPeriod)
	for {
		// Simply loop and check if need to reconnect
		<-tickChan
		peersMsg, err := p.GetPeers()
		if err != nil {
			peerLogger.Errorf("Error in touch service: %s", err.Error())
		}
		allNodes := p.discHelper.GetAllNodes() // these will always be returned in random order
		if len(peersMsg.Peers) < len(allNodes) {
			peerLogger.Warning("Touch service indicates dropped connections, attempting to reconnect...")
			delta := util.FindMissingElements(allNodes, getPeerAddresses(peersMsg))
			if len(delta) > p.discHelper.touchMaxNodes {
				delta = delta[:p.discHelper.touchMaxNodes]
			}
			p.chatWithSomePeers(delta)
		} else {
			peerLogger.Debug("Touch service indicates no dropped connections")
		}
		peerLogger.Debugf("Connected to: %v", getPeerAddresses(peersMsg))
		peerLogger.Debugf("Discovery knows about: %v", allNodes)
	}

}

// chatWithSomePeers initiates chat with 1 or all peers according to whether the node is a validator or not
func (p *Impl) chatWithSomePeers(addresses []string) {
	// start the function to ensure we are connected
	p.reconnectOnce.Do(func() {
		go p.ensureConnected()
	})
	if len(addresses) == 0 {
		peerLogger.Debug("Starting up the first peer of a new network")
		return // nothing to do
	}
	for _, address := range addresses {

		if address == p.self.GetAddress() {
			peerLogger.Debugf("Skipping own address: %v", address)
			continue
		}
		go p.chatWithPeer(address)
	}
}

type peerConnCtxKey string

func (p *Impl) chatWithPeer(address string) error {
	peerLogger.Debugf("Initiating Chat with peer address: %s", address)
	conn, err := NewPeerClientConnectionWithAddress(address)
	if err != nil {
		peerLogger.Errorf("Error creating connection to peer address %s: %s", address, err)
		return err
	}
	defer conn.Close()
	serverClient := pb.NewPeerClient(conn)
	ctx := context.WithValue(context.Background(), peerConnCtxKey("conn"), conn)

	stream, err := serverClient.Chat(ctx)
	if err != nil {
		peerLogger.Errorf("Error establishing chat with peer address %s: %s", address, err)
		return err
	}
	peerLogger.Debugf("Established Chat with peer address: %s", address)
	err = p.handleChat(ctx, stream, true)
	stream.CloseSend()
	if err != nil {
		peerLogger.Errorf("Ending Chat with peer address %s due to error: %s", address, err)
		return err
	}
	return nil
}

// Chat implementation of the the Chat bidi streaming RPC function
func (p *Impl) handleChat(ctx context.Context, stream ChatStream, initiatedStream bool) error {
	deadline, ok := ctx.Deadline()
	peerLogger.Debugf("Current context deadline = %s, ok = %v", deadline, ok)
	handler, err := NewPeerHandler(p, stream, initiatedStream)
	if err != nil {
		return fmt.Errorf("Error creating handler during handleChat initiation: %s", err)
	}

	var legacyHandler LegacyMessageHandler
	if legacy_Engine != nil {
		legacyHandler, err = legacy_Engine.HandlerFactory(handler)
		if err != nil {
			peerLogger.Errorf("Could not obtain legacy handler: %s", err)
		}
	}

	defer handler.Stop()
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			peerLogger.Debug("Received EOF, ending Chat")
			return nil
		}
		if err != nil {
			e := fmt.Errorf("Error during Chat, stopping handler: %s", err)
			peerLogger.Error(e.Error())
			return e
		}

		//legacy message type in chatting stream, sent it to engine
		if in.Type == pb.Message_CONSENSUS && legacyHandler != nil {
			err = legacyHandler.HandleMessage(in)
		} else {
			err = handler.HandleMessage(in)
		}

		if err != nil {
			peerLogger.Errorf("Error handling message: %s", err)
			return err
		}
	}
}

// GetPeerEndpoint returns the endpoint for this peer
func (p *Impl) GetPeerEndpoint() (*pb.PeerEndpoint, error) {
	var ep pb.PeerEndpoint
	//we use an partial copy of the cached endpoint
	ep = *p.self
	if p.secHelper != nil {
		// Set the PkiID on the PeerEndpoint if security is enabled
		ep.PkiID = p.secHelper.PeerPki()
	}
	return &ep, nil
}

func (p *Impl) newHelloMessage() (*pb.HelloMessage, error) {

	ep, err := p.GetPeerEndpoint()
	if err != nil {
		return nil, err
	}

	if p.secHelper != nil {
		return &pb.HelloMessage{PeerEndpoint: ep, PeerCredential: p.secHelper.PeerCred()}, nil
	} else {
		return &pb.HelloMessage{PeerEndpoint: ep}, nil
	}

}

// NewOpenchainDiscoveryHello constructs a new HelloMessage for sending
func (p *Impl) NewOpenchainDiscoveryHello() (*pb.Message, error) {
	helloMessage, err := p.newHelloMessage()
	if err != nil {
		return nil, fmt.Errorf("Error getting new HelloMessage: %s", err)
	}
	data, err := proto.Marshal(helloMessage)
	if err != nil {
		return nil, fmt.Errorf("Error marshalling HelloMessage: %s", err)
	}
	// Need to sign the Discovery Hello message
	newDiscoveryHelloMsg := &pb.Message{Type: pb.Message_DISC_HELLO, Payload: data, Timestamp: util.CreateUtcTimestamp()}
	err = p.signMessageMutating(newDiscoveryHelloMsg)
	if err != nil {
		return nil, fmt.Errorf("Error signing new HelloMessage: %s", err)
	}
	return newDiscoveryHelloMsg, nil
}

// signMessage modifies the passed in Message by setting the Signature based upon the Payload.
func (p *Impl) signMessageMutating(msg *pb.Message) error {
	if p.secHelper != nil {
		var err error
		msg, err = p.secHelper.EndorsePeerMsg(msg)
		if err != nil {
			return fmt.Errorf("Error signing Openchain Message: %s", err)
		}
	}
	return nil
}

// initDiscovery load the addresses from the discovery list previously saved to disk and adds them to the current discovery list
func (p *Impl) initDiscovery(cfg *PeerConfig) []string {

	if !cfg.Discovery.Persist {
		peerLogger.Warning("Discovery list will not be persisted to disk")
		p.discHelper.Discovery = discovery.NewDiscoveryImpl(nil)
	} else {
		p.discHelper.Discovery = discovery.NewDiscoveryImpl(p.persistor)
		err := p.discHelper.LoadDiscoveryList()
		if err != nil {
			peerLogger.Errorf("load discoverylist fail: %s", err)
		}
	}

	p.discHelper.hidden = cfg.Discovery.Hidden
	p.discHelper.disable = cfg.Discovery.Disable

	p.discHelper.touchPeriod = cfg.Discovery.TouchPeriod
	if p.discHelper.touchPeriod.Seconds() < 5.0 {
		peerLogger.Warningf("obtain too small touch peroid: %v, set to 5s", p.discHelper.touchPeriod)
		p.discHelper.touchPeriod = time.Second * 5
	}
	p.discHelper.touchMaxNodes = cfg.Discovery.MaxNodes

	addresses := p.discHelper.GetAllNodes()
	peerLogger.Debugf("Retrieved discovery list from disk: %v", addresses)
	addresses = append(addresses, cfg.Discovery.Roots...)
	peerLogger.Debugf("Retrieved total discovery list: %v", addresses)
	return addresses
}

func (p *Impl) isDiscoveryDisable() bool {
	return p.discHelper.disable
}

func (p *Impl) isHiddenPeer() bool {
	return p.discHelper.hidden
}

// =============================================================================
// Discoverer
// =============================================================================

// Discoverer enables a peer to access/persist/restore its discovery list
type Discoverer interface {
	GetDiscHelper() discovery.Discovery
}

func (p *Impl) GetDiscHelper() discovery.Discovery {
	return p.discHelper
}
