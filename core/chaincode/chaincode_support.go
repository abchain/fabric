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

package chaincode

import (
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/spf13/viper"
	"golang.org/x/net/context"

	"github.com/abchain/fabric/core/chaincode/container"
	"github.com/abchain/fabric/core/chaincode/container/ccintf"
	"github.com/abchain/fabric/core/chaincode/platforms"
	"github.com/abchain/fabric/core/config"
	cred "github.com/abchain/fabric/core/cred"
	"github.com/abchain/fabric/core/ledger"
	pb "github.com/abchain/fabric/protos"
)

// ChainName is the name of the chain to which this chaincode support belongs to.
type ChainName string

const (
	// DefaultChain is the name of the default chain.
	DefaultChain ChainName = "default"
	SystemChain  ChainName = "syscc"
	// DevModeUserRunsChaincode property allows user to run chaincode in development environment
	DevModeUserRunsChaincode       string = "dev"
	NetworkModeChaincode           string = "net"
	chaincodeStartupTimeoutDefault int    = 5000
	chaincodeDeployTimeoutDefault  int    = 30000
	chaincodeExecTimeoutDefault    int    = 30000
	peerAddressDefault             string = "0.0.0.0:7051"

	//TLSRootCertFile = "chaincodeCA.pem"
)

// chains is a map between different blockchains and their ChaincodeSupport.
//this needs to be a first class, top-level object... for now, lets just have a placeholder
var chains map[ChainName]*ChaincodeSupport

func init() {
	chains = make(map[ChainName]*ChaincodeSupport)
}

//chaincode runtime environment encapsulates handler and container environment
//This is where the VM that's running the chaincode would hook in
type chaincodeRTEnv struct {
	handler      *Handler
	launchNotify chan error
	launchResult error
	waitCtx      context.Context
}

// runningChaincodes contains maps of chaincodeIDs to their chaincodeRTEs
type runningChaincodes struct {
	sync.RWMutex
	// chaincode environment for each chaincode
	chaincodeMap map[string]map[*ledger.Ledger]*chaincodeRTEnv

	// only for usercc, cache pre-connected chaincode and use them for launching
	freeChainCodes map[string][]*chaincodeRTEnv
}

// GetChain returns the chaincode support for a given chain
func GetChain(name ChainName) *ChaincodeSupport {
	return chains[name]
}

func GetDefaultChain() *ChaincodeSupport {
	return chains[DefaultChain]
}

func GetSystemChain() *ChaincodeSupport {
	return chains[SystemChain]
}

//call this under lock
func (chaincodeSupport *ChaincodeSupport) preLaunchSetup(l *ledger.Ledger, chaincode string) *chaincodeRTEnv {
	//register placeholder Handler.
	ret := &chaincodeRTEnv{
		launchNotify: make(chan error, 1),
	}

	if _, ok := chaincodeSupport.runningChaincodes.chaincodeMap[chaincode]; !ok {
		chaincodeSupport.runningChaincodes.chaincodeMap[chaincode] = make(map[*ledger.Ledger]*chaincodeRTEnv)
	}
	chaincodeSupport.runningChaincodes.chaincodeMap[chaincode][l] = ret
	return ret
}

const (
	codepackCCName = ".repoCC"
	deployTxKey    = "__YAFABRIC_deployTx"
)

//used to filter some chaincode name from external accessing
var ReservedCCName = map[string]bool{codepackCCName: true}

func (chaincodeSupport *ChaincodeSupport) FinalDeploy(chrte *chaincodeRTEnv, txe *pb.TransactionHandlingContext, outstate ledger.TxExecStates) error {

	if depTx, err := strippedTxForDeployment(txe); err != nil {
		return fmt.Errorf("strip deptx fail :%s", err)
	} else {
		depTxByte, err := proto.Marshal(depTx)
		if err != nil {
			return fmt.Errorf("encode deptx fail: %s", err)
		}
		chrte.handler.deployTXSecContext = depTx
		outstate.Set(txe.ChaincodeName, deployTxKey, depTxByte)
	}
	return nil
}

//call this under lock
func (chaincodeSupport *ChaincodeSupport) chaincodeHasBeenLaunched(l *ledger.Ledger, chaincode string) (chrte *chaincodeRTEnv, hasbeenlaunched bool) {

	if chaincodeSupport.userRunsCC {
		//check cached, prelaunched sessions
		defer func() {
			if !hasbeenlaunched {
				for _, chrte = range chaincodeSupport.runningChaincodes.freeChainCodes[chaincode] {
					//take one cached session out
					chrte.handler.Ledger = l
					if _, ok := chaincodeSupport.runningChaincodes.chaincodeMap[chaincode]; !ok {
						chaincodeSupport.runningChaincodes.chaincodeMap[chaincode] = make(map[*ledger.Ledger]*chaincodeRTEnv)
					}
					chaincodeSupport.runningChaincodes.chaincodeMap[chaincode][l] = chrte
					fcc := chaincodeSupport.runningChaincodes.freeChainCodes[chaincode]
					chaincodeSupport.runningChaincodes.freeChainCodes[chaincode] = fcc[1:]
					hasbeenlaunched = true
					chaincodeLogger.Debugf("pick free launching chaincode %s", chaincode)
				}
			}
		}()
	}

	if ml, ledgerExist := chaincodeSupport.runningChaincodes.chaincodeMap[chaincode]; !ledgerExist {
		return
	} else {
		chaincodeLogger.Debugf("test for cc[%s]'s map %v", chaincode, ml)
		chrte, hasbeenlaunched = ml[l]
		chaincodeLogger.Debugf("found %v for ledger %p", hasbeenlaunched, l)
		return
	}
}

func RemoveChaincodeSupport(cName ChainName) {
	delete(chains, cName)
}

func SetChaincodeSupport(cName ChainName, ccsp *ChaincodeSupport) {
	_, registed := chains[cName]
	if registed {
		panic("Duplicated registing chaincode")
	}
	chains[cName] = ccsp
}

func NewSystemChaincodeSupport(nodeName string, chainName ...ChainName) *ChaincodeSupport {

	sysccName := SystemChain
	if len(chainName) > 0 {
		sysccName = chainName[0]
	}

	_, registed := chains[sysccName]
	if registed {
		panic("Duplicated registing chaincode")
	}

	s := &ChaincodeSupport{name: sysccName,
		runningChaincodes: &runningChaincodes{
			chaincodeMap: make(map[string]map[*ledger.Ledger]*chaincodeRTEnv),
		},
		nodeID:           nodeName,
		ccStartupTimeout: 3 * time.Second,
		ccDeployTimeout:  3 * time.Second,
	}

	chains[sysccName] = s

	//we only respect exec timeout
	tOut, err := strconv.Atoi(viper.GetString("chaincode.scc.exectimeout"))
	if err != nil {
		tOut = chaincodeExecTimeoutDefault
	}
	s.ccExecTimeout = time.Duration(tOut) * time.Millisecond
	return s
}

// NewChaincodeSupport creates a new ChaincodeSupport instance
func NewChaincodeSupport(chainname ChainName, nodeName string, srvSpec *config.ServerSpec, userrunsCC bool) *ChaincodeSupport {

	s := &ChaincodeSupport{name: chainname,
		runningChaincodes: &runningChaincodes{
			chaincodeMap: make(map[string]map[*ledger.Ledger]*chaincodeRTEnv),
		},
		userRunsCC:  userrunsCC,
		clientGuide: srvSpec.GetClient(),
		nodeID:      nodeName}

	s.debugCC = viper.GetBool("chaincode.debugmode")

	//initialize global chain
	chains[chainname] = s
	chaincodeLogger.Infof("Chaincode support %s using peerAddress: %s\n", chainname, s.clientGuide.Address)

	//get chaincode startup timeout
	tOut, err := strconv.Atoi(viper.GetString("chaincode.startuptimeout"))
	if err != nil {
		tOut = chaincodeStartupTimeoutDefault
		chaincodeLogger.Infof("could not retrive startup timeout var...setting to %d secs\n", tOut/1000)
	}

	s.ccStartupTimeout = time.Duration(tOut) * time.Millisecond

	//get chaincode deploy timeout
	tOut, err = strconv.Atoi(viper.GetString("chaincode.deploytimeout"))
	if err != nil {
		tOut = chaincodeDeployTimeoutDefault
		chaincodeLogger.Infof("could not retrive deploy timeout var...setting to %d secs\n", tOut/1000)
	}

	s.ccDeployTimeout = time.Duration(tOut) * time.Millisecond

	//get chaincode exec timeout
	tOut, err = strconv.Atoi(viper.GetString("chaincode.exectimeout"))
	if err != nil {
		tOut = chaincodeExecTimeoutDefault
		chaincodeLogger.Infof("could not retrive exec timeout var...setting to %d secs\n", tOut/1000)
	}

	s.ccExecTimeout = time.Duration(tOut) * time.Millisecond

	kadef := 0
	if ka := viper.GetString("chaincode.keepalive"); ka == "" {
		s.keepalive = time.Duration(kadef) * time.Second
	} else {
		t, terr := strconv.Atoi(ka)
		if terr != nil {
			chaincodeLogger.Errorf("Invalid keepalive value %s (%s) defaulting to %d", ka, terr, kadef)
			t = kadef
		} else if t <= 0 {
			chaincodeLogger.Debugf("Turn off keepalive(value %s)", ka)
			t = kadef
		}
		s.keepalive = time.Duration(t) * time.Second
	}

	return s
}

// // ChaincodeStream standard stream for ChaincodeMessage type.
// type ChaincodeStream interface {
// 	Send(*pb.ChaincodeMessage) error
// 	Recv() (*pb.ChaincodeMessage, error)
// }

// ChaincodeSupport responsible for providing interfacing with chaincodes from the Peer.
type ChaincodeSupport struct {
	name              ChainName
	runningChaincodes *runningChaincodes
	peerAddress       string
	ccStartupTimeout  time.Duration
	ccDeployTimeout   time.Duration
	ccExecTimeout     time.Duration
	userRunsCC        bool
	debugCC           bool
	nodeID            string
	clientGuide       *config.ClientSpec
	keepalive         time.Duration
}

func (chaincodeSupport *ChaincodeSupport) UserRunsCC() bool {
	return chaincodeSupport.userRunsCC
}

func (chaincodeSupport *ChaincodeSupport) registerHandler(cID *pb.ChaincodeID, ledgerTag string, stream ccintf.ChaincodeStream) (*Handler, *workingStream, error) {

	key := cID.Name
	chaincodeSupport.runningChaincodes.Lock()
	defer chaincodeSupport.runningChaincodes.Unlock()

	var handler *Handler
	var err error
	for l, chrte := range chaincodeSupport.runningChaincodes.chaincodeMap[key] {

		//so it was a pending runtime (just launched) and can be use
		//(all cc just created is equivalence for pending launching instance
		//and we can assigned to any one of them)
		if chrte.handler == nil && (ledgerTag == "" || l.Tag() == ledgerTag) {
			handler = newChaincodeSupportHandler(chaincodeSupport)
			handler.ChaincodeID = cID
			handler.Ledger = l

			chrte.handler = handler
			defer func(chrte *chaincodeRTEnv) { chrte.launchNotify <- err }(chrte)
			break
		}

	}

	//if we can not find any available pending runtime, maybe we can cache it (only for userruncc mode)
	if handler == nil {
		if !chaincodeSupport.userRunsCC {
			return nil, nil, fmt.Errorf("Can't register chaincode without invoking deploy tx")
		} else {
			handler = newChaincodeSupportHandler(chaincodeSupport)
			handler.ChaincodeID = cID
			chaincodeSupport.runningChaincodes.freeChainCodes[key] = append(chaincodeSupport.runningChaincodes.freeChainCodes[key],
				&chaincodeRTEnv{handler: handler})
		}
	}

	var ws *workingStream
	ws, err = handler.addNewStream(stream)
	if err != nil {
		return nil, nil, err
	}
	// ----------- YA-fabric 0.9 note -------------
	//the protocol (cc shim require an ACT from server) should be malformed
	//for the handshaking of connection can be responsed by grpc itself
	//we will eliminate this response in the later version and the code
	//following is just for compatible

	chaincodeLogger.Debugf("cc [%s] is lauching, sending back %s", key, pb.ChaincodeMessage_REGISTERED)
	err = ws.Send(&pb.ChaincodeMessage{Type: pb.ChaincodeMessage_REGISTERED})
	if err != nil {
		return nil, nil, err
	}
	// --------------------------------------------

	chaincodeLogger.Debugf("registered handler complete for chaincode %s", key)

	return handler, ws, nil
}

func (chaincodeSupport *ChaincodeSupport) deregisterHandler(chaincodehandler *Handler) {

	key := chaincodehandler.ChaincodeID.Name
	l := chaincodehandler.Ledger
	chaincodeSupport.runningChaincodes.Lock()
	defer chaincodeSupport.runningChaincodes.Unlock()

	chaincodeLogger.Debugf("Deregistered handler with key %s and ledger %p", key, l)
	if l == nil {
		//this should be a free rte ...
		for i, rte := range chaincodeSupport.runningChaincodes.freeChainCodes[key] {
			if rte.handler == chaincodehandler {
				freeRtes := chaincodeSupport.runningChaincodes.freeChainCodes[key]
				freeRtes = append(freeRtes[:i], freeRtes[i+1:]...)
				chaincodeSupport.runningChaincodes.freeChainCodes[key] = freeRtes
				break
			}
		}
	} else {

		if _, ok := chaincodeSupport.runningChaincodes.chaincodeMap[key]; !ok {
			chaincodeLogger.Warning("handler for chaincode %s has been pruned", key)
			return
		}

		delete(chaincodeSupport.runningChaincodes.chaincodeMap[key], l)
		if len(chaincodeSupport.runningChaincodes.chaincodeMap[key]) == 0 {
			chaincodeLogger.Debugf("clean handlers group with key: %s", key)
			delete(chaincodeSupport.runningChaincodes.chaincodeMap, key)
		}
	}

}

// launchAndWaitForRegister will launch container if not already running. Use the targz to create the image if not found
func (chaincodeSupport *ChaincodeSupport) launchAndWaitForRegister(ctxt context.Context, netTag string, cds *pb.ChaincodeDeploymentSpec, cLang pb.ChaincodeSpec_Type, targz io.Reader) error {

	if chaincodeSupport.userRunsCC && cds.GetExecEnv() != pb.ChaincodeDeploymentSpec_SYSTEM {
		return fmt.Errorf("chaincode is user-running and no need to launch")
	}

	chaincode := cds.GetChaincodeSpec().GetChaincodeID().GetName()
	if chaincode == "" {
		return fmt.Errorf("chaincode name not set")
	}

	var args, env []string
	var err error
	//launch the chaincode
	if cds.GetExecEnv() != pb.ChaincodeDeploymentSpec_SYSTEM {
		args, env, err = platforms.GetArgsAndEnv(cds.ChaincodeSpec, netTag, chaincodeSupport.clientGuide)
		if err != nil {
			return err
		}
	} else {
		args = platforms.GetSystemEnvArgsAndEnv(cds.ChaincodeSpec)
	}

	chaincodeLogger.Debugf("start container: %s(chain:%s,nodeid:%s)", chaincode, chaincodeSupport.name, chaincodeSupport.nodeID)
	chaincodeLogger.Debugf("envs are %v, %v", args, env)

	vmtype, _ := chaincodeSupport.getVMType(cds)

	sir := container.StartImageReq{CCID: ccintf.CCID{ChaincodeSpec: cds.ChaincodeSpec, NetworkID: netTag, PeerID: chaincodeSupport.nodeID}, Reader: targz, Args: args, Env: env}

	ipcCtxt := context.WithValue(ctxt, ccintf.GetCCHandlerKey(), chaincodeSupport)

	resp, err := container.VMCProcess(ipcCtxt, vmtype, sir)
	if err != nil || (resp != nil && resp.(container.VMCResp).Err != nil) {
		if err == nil {
			err = resp.(container.VMCResp).Err
		}
		err = fmt.Errorf("Error starting container: %s", err)
		return err
	}

	return nil
}

func (chaincodeSupport *ChaincodeSupport) finishLaunching(l *ledger.Ledger, chaincode string, notify error) {

	//we need a "lasttime checking", so if the launching chaincode is not registered,
	//we just erase it and notify a termination
	if rte, ok := chaincodeSupport.chaincodeHasBeenLaunched(l, chaincode); !ok {
		//nothing to do
		chaincodeLogger.Warningf("trying to terminate the launching for unexist chaincode %s", chaincode)
		return
		// } else if rte.handler != nil {
		// 	//chaincode is registered ...
		// 	return false
		// } else {
	} else {

		//sanity check
		if rte.waitCtx == nil {
			panic("another routine has make this calling, we have wrong code?")
		}
		rte.launchResult = notify
		rte.waitCtx = nil
	}

	//if we get err notify, we must clear the rte even it has created a handler
	if notify != nil {
		chaincodeLogger.Debugf("cleaning prelaunched rt for [%s] (error %s)", chaincode, notify)
		ml := chaincodeSupport.runningChaincodes.chaincodeMap[chaincode]
		delete(ml, l)
		if len(ml) == 0 {
			chaincodeLogger.Debugf("cleaning the whole chaincode map for [%s]", chaincode)
			delete(chaincodeSupport.runningChaincodes.chaincodeMap, chaincode)
		}
	}
}

//Stop stops a chaincode if running
func (chaincodeSupport *ChaincodeSupport) Stop(context context.Context, netTag string, cds *pb.ChaincodeDeploymentSpec) error {

	if chaincodeSupport.userRunsCC && cds.GetExecEnv() != pb.ChaincodeDeploymentSpec_SYSTEM {
		return fmt.Errorf("chaincode is user-running and no need to stop")
	}

	chaincode := cds.ChaincodeSpec.ChaincodeID.Name
	if chaincode == "" {
		return fmt.Errorf("chaincode name not set")
	}

	//stop the chaincode
	sir := container.StopImageReq{
		CCID:       ccintf.CCID{ChaincodeSpec: cds.ChaincodeSpec, NetworkID: netTag, PeerID: chaincodeSupport.nodeID},
		Timeout:    0,
		Dontremove: chaincodeSupport.debugCC,
	}

	vmtype, _ := chaincodeSupport.getVMType(cds)

	_, err := container.VMCProcess(context, vmtype, sir)
	if err != nil {
		err = fmt.Errorf("Error stopping container: %s", err)
		//but proceed to cleanup
	}

	return err
}

func checkDeployTx(chaincode string, st ledger.TxExecStates, ledger *ledger.Ledger) (depTx *pb.Transaction, ledgerErr error) {

	var txByte []byte
	if txByte, ledgerErr = st.Get(chaincode, deployTxKey, ledger); ledgerErr != nil {
		return
	} else if txByte == nil {
		chaincodeLogger.Debugf("Deploy tx for chaincoide %s not found, try chaincode name as tx id", chaincode)
		depTx, ledgerErr = ledger.GetTransactionByID(chaincode)
		return
	}

	depTx = new(pb.Transaction)
	if err := proto.Unmarshal(txByte, depTx); err != nil {
		ledgerErr = err
		return
	}

	return
}

func ValidateDeploymentSpec(txe *pb.TransactionHandlingContext, st ledger.TxExecStates, ledger *ledger.Ledger) error {

	if depTx, _ := checkDeployTx(txe.ChaincodeName, st, ledger); depTx != nil {
		return fmt.Errorf("Duplicated deployment for chaincode %s", txe.ChaincodeName)
	}

	//TODO: update cds if it specified a template name

	//a trick: deployment tx has just the deployspec object
	if txtp := txe.GetType(); txtp != pb.Transaction_CHAINCODE_DEPLOY {
		return fmt.Errorf("Is not deploy transaction (%s)", txtp)
	}

	st.Set(codepackCCName, txe.ChaincodeName, txe.Transaction.GetPayload())
	return nil
}

func (chaincodeSupport *ChaincodeSupport) DeployLaunch(ctx context.Context, ledger *ledger.Ledger,
	chaincode string, cds *pb.ChaincodeDeploymentSpec) (error, *chaincodeRTEnv) {

	return chaincodeSupport.Launch2(ctx, ledger, chaincode,
		func() (*pb.ChaincodeDeploymentSpec, *pb.Transaction, error) {
			//notice the cds comes from txhandlingcontext, and the embedded ccid has been
			//set so we don't need to reset it
			return cds, nil, nil
		})
}

func (chaincodeSupport *ChaincodeSupport) Launch(ctx context.Context, ledger *ledger.Ledger,
	st ledger.TxExecStates, chaincode string) (error, *chaincodeRTEnv) {

	getcds := func() (cds *pb.ChaincodeDeploymentSpec, depTx *pb.Transaction, ledgerErr error) {

		depTx, ledgerErr = checkDeployTx(chaincode, st, ledger)
		if ledgerErr != nil {
			return
		} else if depTx == nil {
			return nil, nil, fmt.Errorf("deploy tx for chaincode %s not found", chaincode)
		}

		//cid in deploy tx has ensured to be plain text
		cid := new(pb.ChaincodeID)
		if err := proto.Unmarshal(depTx.GetChaincodeID(), cid); err != nil {
			return nil, nil, fmt.Errorf("decode chaincode ID fail: %s", err)
		}

		ccname, tmn, _ := pb.ParseYFCCName(cid.GetName())
		if tmn == "" {
			tmn = ccname
		}
		chaincodeLogger.Debugf("Extra deploy package with template name <%s>", tmn)

		//sanity check
		if ccname != chaincode {
			chaincodeLogger.Fatalf("Wrong code: we get cc %s's deploy tx with differnt name (%s)", chaincode, ccname)
		}

		specByte, err := st.Get(codepackCCName, tmn, ledger)
		if err != nil {
			return nil, nil, fmt.Errorf("DB error on get ccspec data: %s", err)
		} else if specByte == nil {
			//try to decode data from tx ...
			//we have abandoned the encryption case
			chaincodeLogger.Warningf("try recover chaincode deployment spec from tx [%s]", depTx.GetTxid())
			specByte = depTx.Payload
		}

		cds = new(pb.ChaincodeDeploymentSpec)
		if err = proto.Unmarshal(specByte, cds); err != nil {
			return nil, nil, fmt.Errorf("decode cc deploy spec fail: %s", err)
		}

		if cds.ChaincodeSpec == nil {
			return nil, nil, fmt.Errorf("Invalid deploy spec: no ccspec included")
		}
		//legacy code still use full deploy spec information (include ccid), we must apply cid from tx
		//to deployspec
		cds.ChaincodeSpec.ChaincodeID = cid

		chaincodeLogger.Debugf("Load cds for chaincode %s:%s: %v", chaincode, tmn, cds)
		return
	}

	return chaincodeSupport.Launch2(ctx, ledger, chaincode, getcds)
}

// Launch will launch the chaincode if not running (if running return nil) and will wait for handler of the chaincode to get into FSM ready state.
func (chaincodeSupport *ChaincodeSupport) Launch2(ctx context.Context, ledger *ledger.Ledger,
	chaincode string, getcds func() (*pb.ChaincodeDeploymentSpec, *pb.Transaction, error)) (error, *chaincodeRTEnv) {

	chaincodeSupport.runningChaincodes.Lock()
	defer chaincodeSupport.runningChaincodes.Unlock()

	//the first tx touch the corresponding run-time object is response for the actually
	//launching and other tx just wait
	if chrte, ok := chaincodeSupport.chaincodeHasBeenLaunched(ledger, chaincode); ok {
		if chrte.waitCtx == nil {
			chaincodeLogger.Debugf("chaincode is running(no need to launch) : %s", chaincode)
			return nil, chrte
		}
		//all of us must wait here till the cc is really launched (or failed...)
		chaincodeLogger.Debug("chainicode not in READY state...waiting")

		select {
		case <-chrte.waitCtx.Done():
		case <-ctx.Done():
			return fmt.Errorf("Cancel: %s", ctx.Err()), nil
		}

		chaincodeLogger.Debugf("wait chaincode %s for lauching: [%s]", chaincode, chrte.launchResult)
		if chrte.launchResult == nil {
			if chrte.handler == nil {
				chaincodeLogger.Errorf("handler is not available but lauching [%s(chain:%s,nodeid:%s)] not notify that", chaincode, chaincodeSupport.name, chaincodeSupport.nodeID)
				return fmt.Errorf("internal error"), nil
			} else {
				return nil, chrte
			}
		} else {
			return chrte.launchResult, nil
		}
	}

	//we should first check the deployment information so we avoid spent extra resources
	//on non-existed chaincode
	cds, depTx, err := getcds()
	if err != nil {
		return err, nil
	}

	//the first one create rte and start its adventure ...
	chrte := chaincodeSupport.preLaunchSetup(ledger, chaincode)
	var waitCf context.CancelFunc
	chrte.waitCtx, waitCf = context.WithCancel(ctx)
	chaincodeSupport.runningChaincodes.Unlock()

	//so the launchResult in runtime will be set first
	defer waitCf()
	defer func() {
		chaincodeSupport.runningChaincodes.Lock()
		chaincodeSupport.finishLaunching(ledger, chaincode, err)
	}()

	cLang := cds.ChaincodeSpec.Type
	//launch container if it is a System container or not in dev mode
	if !chaincodeSupport.userRunsCC || cds.ExecEnv == pb.ChaincodeDeploymentSpec_SYSTEM {
		var packrd *runtimeReader
		if cds.ExecEnv != pb.ChaincodeDeploymentSpec_SYSTEM {
			packrd, err = WriteRuntimePackage(cds, chaincodeSupport.clientGuide)
			if err != nil {
				chaincodeLogger.Errorf("WriteRuntimePackage failed %s", err)
				return err, chrte
			}
		}

		wctx, wctxend := context.WithTimeout(ctx, chaincodeSupport.ccDeployTimeout)
		defer wctxend()

		err = chaincodeSupport.launchAndWaitForRegister(wctx, ledger.Tag(), cds, cLang, packrd)
		//first finish and trace the real reason in runtime reading
		if omiterr := packrd.Finish(); omiterr != nil {
			chaincodeLogger.Errorf("WriteRuntimePackage failed, reason was %s", omiterr)
		}

		if err != nil {
			chaincodeLogger.Errorf("launchAndWaitForRegister failed %s", err)
			return err, chrte
		}

		//from here on : if we launch the container and get an error, we need to stop the container
		defer func() {
			if err != nil {
				chaincodeLogger.Infof("stopping due to error while launching: %s", err)
				errIgnore := chaincodeSupport.Stop(ctx, ledger.Tag(), cds)
				if errIgnore != nil {
					chaincodeLogger.Debugf("error on stop %s(%s)", errIgnore, err)
				}
			}
		}()
	}

	wctx, wctxend := context.WithTimeout(ctx, chaincodeSupport.ccStartupTimeout)
	defer wctxend()

	//wait for REGISTER state
	select {
	case err = <-chrte.launchNotify:
	case <-wctx.Done():
		err = fmt.Errorf("Timeout expired while starting chaincode %s(chain:%s,nodeid:%s)", chaincode, chaincodeSupport.name, chaincodeSupport.nodeID)
	}
	if err != nil {
		return err, chrte
	}

	//send ready (if not deploy) for ready state
	if chrte.handler == nil {
		err = fmt.Errorf("handler is not available though lauching [%s(chain:%s,nodeid:%s)] notify ok", chaincode, chaincodeSupport.name, chaincodeSupport.nodeID)
		return err, chrte
	} else if depTx != nil {
		err = chrte.handler.readyChaincode(depTx)
		if err != nil {
			return err, chrte
		}
	}
	chaincodeLogger.Debug("LaunchChaincode complete")
	return nil, chrte
}

//getVMType - just returns a string for now. Another possibility is to use a factory method to
//return a VM executor
func (chaincodeSupport *ChaincodeSupport) getVMType(cds *pb.ChaincodeDeploymentSpec) (string, error) {
	if cds.ExecEnv == pb.ChaincodeDeploymentSpec_SYSTEM {
		return container.SYSTEM, nil
	}
	return container.DOCKER, nil
}

// Register the bidi stream entry point called by chaincode to register with the Peer.
// registerHandler implements ccintf.HandleChaincodeStream for all vms to call with appropriate stream
// It call the main loop in handler for handling the associated Chaincode stream
func (chaincodeSupport *ChaincodeSupport) Register(stream pb.ChaincodeSupport_RegisterServer) error {
	return chaincodeSupport.HandleChaincodeStream(stream.Context(), stream)
}

func (chaincodeSupport *ChaincodeSupport) Name() string { return string(chaincodeSupport.name) }

func (chaincodeSupport *ChaincodeSupport) HandleChaincodeStream(ctx context.Context, stream ccintf.ChaincodeStream) error {
	msg, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("Error in recv [%s]", err)
	}
	var chaincodeID *pb.ChaincodeID
	var ledgerTag string

	switch msg.Type {
	case pb.ChaincodeMessage_REGISTER:
		chaincodeID = new(pb.ChaincodeID)
		err = proto.Unmarshal(msg.Payload, chaincodeID)
	case pb.ChaincodeMessage_REGISTER2:
		regmsg := new(pb.CCRegister)
		err = proto.Unmarshal(msg.Payload, regmsg)
		ledgerTag = regmsg.GetNetworkID()
		chaincodeID = regmsg.GetCcID()
	default:
		return fmt.Errorf("Recv unexpected message type [%s] at the beginning of ccstream", msg.ChaincodeEvent)
	}

	if err != nil {
		return fmt.Errorf("Error in received [%s], could NOT unmarshal registration info: %s", msg.Type, err)
	}

	chaincodeLogger.Debugf("Recv register msg %v, reg [%v]@%s", msg, chaincodeID, ledgerTag)

	handler, ws, err := chaincodeSupport.registerHandler(chaincodeID, ledgerTag, stream)
	if err != nil {
		return fmt.Errorf("Register handler fail: %s", err)
	}

	deadline, ok := ctx.Deadline()
	chaincodeLogger.Debugf("Current context deadline = %s, ok = %v", deadline, ok)
	return ws.processStream(ctx, handler)
}

// Execute executes a transaction and waits for it to complete until a timeout value.
func (chaincodeSupport *ChaincodeSupport) Execute(ctxt context.Context, chrte *chaincodeRTEnv, txe *pb.TransactionHandlingContext, outstate ledger.TxExecStates) (*pb.ChaincodeMessage, error) {

	msg, err := createTransactionMessage(txe.GetType(), txe.GetTxid(), txe.ChaincodeSpec.CtorMsg, txe.SecContex)
	if err != nil {
		return nil, fmt.Errorf("Failed to gen transaction message (%s)", err)
	}

	enc, err := cred.GenDataEncryptor(chrte.handler.deployTXSecContext, txe)
	if err != nil {
		return nil, fmt.Errorf("Failed to gen data encryptor (%s)", err)
	}
	// if err = handler.setChaincodeSecurityContext(tx, msg); err != nil {
	// 	return nil, emptyExState, err
	// }

	wctx, cf := context.WithTimeout(ctxt, chaincodeSupport.ccExecTimeout)
	defer cf()
	return chrte.handler.executeMessage(wctx, msg, enc, outstate)

}

// Executelite executes with minimal data requirement, can used for internal testing, syscc and some other cases
// this method also omit the limit of exectimeout, and CAN NOT be run concurrently
func (chaincodeSupport *ChaincodeSupport) ExecuteLite(ctxt context.Context, chrte *chaincodeRTEnv, ttype pb.Transaction_Type, input *pb.ChaincodeInput, outstate ledger.TxExecStates) (*pb.ChaincodeMessage, error) {

	msg, err := createTransactionMessage(ttype, "lite_execute", input, nil)
	if err != nil {
		return nil, fmt.Errorf("Failed to gen lite transaction message (%s)", err)
	}

	return chrte.handler.executeMessage(ctxt, msg, nil, outstate)

}

func (chaincodeSupport *ChaincodeSupport) ReleaseLedger(l *ledger.Ledger) error {

	chaincodeLogger.Debugf("Release all handler bind to ledger %p", l)

	chaincodeSupport.runningChaincodes.Lock()
	defer chaincodeSupport.runningChaincodes.Unlock()

	for _, rtes := range chaincodeSupport.runningChaincodes.chaincodeMap {
		if rte, ok := rtes[l]; ok {
			//sanity check: a run-time of pending status indicate some execute is running with
			//corresponding ledger, and the caller do execute and releaseledger simultaneously,
			//which should not be allowed
			if rte.handler == nil {
				panic("try to delete a runtime when launching is pending, indicate a malformed, racing code")
			}

			//disconnect all streams
			//DO NOT delete the corresponding handler explicitly, deregisterHandler will do that
			for _, strm := range rte.handler.workingStream {
				//TODO: we should define some message to gracely shutdown the stream
				strm.resp <- &pb.ChaincodeMessage{Type: pb.ChaincodeMessage_ERROR}
			}
		}
	}

	return nil
}
