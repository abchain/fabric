package main

import (
	"fmt"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/ledger/statemgmt"
	"github.com/abchain/fabric/core/sync"
	node "github.com/abchain/fabric/node/start"
	pb "github.com/abchain/fabric/protos"
	"github.com/op/go-logging"
	"github.com/spf13/viper"
	"math/rand"
	"os"
	"strconv"
	"time"
)

var logger = logging.MustGetLogger("synctest")

func constructRandomBytes(r *rand.Rand, size int) ([]byte, error) {
	value := make([]byte, size)
	_, err := r.Read(value)
	if err != nil {
		return nil, err
	}
	return value, nil
}

// ConstructRandomStateDelta creates a random state delta for testing
func constructRandomStateDelta(
	chaincodeIDPrefix string,
	numChaincodes int,
	maxKeySuffix int,
	numKeysToInsert int,
	kvSize int) (*statemgmt.StateDelta, error) {
	delta := statemgmt.NewStateDelta()
	s2 := rand.NewSource(time.Now().UnixNano())
	r2 := rand.New(s2)

	for i := 0; i < numKeysToInsert; i++ {
		chaincodeID := chaincodeIDPrefix + "_" + strconv.Itoa(r2.Intn(numChaincodes))
		key := "key_" + strconv.Itoa(r2.Intn(maxKeySuffix))
		valueSize := kvSize - len(key)
		if valueSize < 1 {
			panic(fmt.Errorf("valueSize cannot be less than one. ValueSize=%d", valueSize))
		}
		value, err := constructRandomBytes(r2, valueSize)
		if err != nil {
			return nil, err
		}
		delta.Set(chaincodeID, key, value, nil)
	}

	for ccID, chaincodeDelta := range delta.ChaincodeStateDeltas {
		sortedKeys := chaincodeDelta.GetSortedKeys()
		smallestKey := sortedKeys[0]
		largestKey := sortedKeys[len(sortedKeys)-1]
		logger.Infof("chaincode=%s, numKeys=%d, smallestKey=%s, largestKey=%s", ccID, len(sortedKeys), smallestKey, largestKey)
	}
	return delta, nil
}

func prepareLedger(l *ledger.Ledger, datakeys int, blocks int) error {

	if l.GetBlockchainSize() != 0 {
		return nil
	}
	logger.Info("Testsync init ledger for server running first time ...")

	//add gensis block
	logger.Debugf("prepare for %d keys ...", datakeys)
	delta, err := constructRandomStateDelta("", 8, 125*datakeys, datakeys, 32)
	if err != nil {
		return err
	}
	cid := "bigTx"
	err = l.ApplyStateDelta(cid, delta)
	if err != nil {
		return err
	}
	err = l.CommitTxBatch(cid, nil, nil, []byte("great tx"))
	if err != nil {
		return err
	}

	offset := int(l.GetBlockchainSize())

	logger.Debugf("prepare for %d blocks ...", blocks)
	for ib := 0; ib < blocks; ib++ {

		transaction, _ := pb.NewTransaction(pb.ChaincodeID{Path: "testUrl"}, "", "", []string{"param1, param2"})
		dig, _ := transaction.Digest()
		transaction.Txid = pb.TxidFromDigest(dig)

		l.BeginTxBatch(ib)
		l.TxBegin("txUuid")
		l.SetState("chaincode1", "keybase", []byte{byte(ib + offset)})
		l.SetState("chaincode2", "keybase", []byte{byte(ib + offset)})
		l.SetState("chaincode3", "keybase", []byte{byte(ib + offset)})
		l.TxFinished("txUuid", true)
		l.CommitTxBatch(ib, []*pb.Transaction{transaction}, nil, []byte("proof1"))
	}

	return nil
}

const populateKeysDef = 120

//NOTICE we MUST set the blocks align to the snapshot interval, so we has at least one stable state
//for syncing
const populateAdditionalBlocks = 64

func runAsServer() error {
	logger.Info("Testsync node run as server")
	keys := viper.GetInt("sync.test.serverkeys")
	if keys == 0 {
		keys = populateKeysDef
	}

	blocks := viper.GetInt("sync.test.serverblocks")
	if blocks == 0 {
		blocks = populateAdditionalBlocks
	}

	l, err := ledger.GetLedger()
	if err != nil {
		return err
	}

	err = prepareLedger(l, keys, blocks)
	if err != nil {
		return err
	}

	inf, err := l.GetBlockchainInfo()
	if err != nil {
		return err
	} else if inf.GetHeight() == 0 {
		panic("blockchain is still empty")
	}

	logger.Infof("--------------- Testsync node prepare done ---------------")
	logger.Info("Below is the top block's info of ledger, record it for client!")
	logger.Warningf("%d:%X", inf.GetHeight()-1, inf.GetCurrentBlockHash())
	logger.Infof("--------------- -------------------------- ---------------\n\n\n")

	return nil
}

func runAsClient() {
	logger.Info("Testsync node run as client, start ...")
	blockh := viper.GetString("sync.test.targetblock")
	if blockh == "" {
		logger.Errorf("No hash of block is specified, just quit")
		return
	}
	var targetHash []byte
	var targetHeight uint64
	_, err := fmt.Sscanf(blockh, "%d:%X", &targetHeight, &targetHash)
	if err != nil {
		logger.Errorf("scan hash fail: %s, exit", err)
		return
	}
	logger.Infof("Client will sync to %d:%X", targetHeight, targetHash)
	l := node.GetNode().DefaultLedger()
	sstub := node.GetNode().DefaultPeer().GetStreamStub("sync")
	if sstub == nil {
		panic("no stream stub")
	}

	baseCtx := node.GetNode().DefaultPeer().GetPeerCtx()

	blkAgent := ledger.NewBlockAgent(l)
	blockCli := sync.NewBlockSyncClient(baseCtx, blkAgent.SyncCommitBlock,
		sync.CheckpointToSyncPlan(map[uint64][]byte{targetHeight: targetHash}))

	//left some time and chance to ensure we have connect the server ...
	blockCli.Opts().RetryInterval = 2
	blockCli.Opts().RetryCount = 3

	err = sync.ExecuteSyncTask(blockCli, sstub)
	if err != nil {
		logger.Errorf("syncing blocks fail: %s, exit", err)
		return
	}

	sdetector := sync.NewStateSyncDetector(baseCtx, l, int(targetHeight))
	err = sdetector.DoDetection(sstub)
	if err != nil {
		logger.Errorf("state detect fail: %s, exit", err)
		return
	}

	logger.Infof("can sync to state %X@%d", sdetector.Candidate.State,
		sdetector.Candidate.Height)

	syncer, err := ledger.NewSyncAgent(l, sdetector.Candidate.Height,
		sdetector.Candidate.State)
	if err != nil {
		logger.Errorf("create state syncer fail: %s, exit", err)
		return
	}
	stateCli, endSyncF := sync.NewStateSyncClient(baseCtx, syncer)
	defer endSyncF()

	err = sync.ExecuteSyncTask(stateCli, sstub)
	if err != nil {
		logger.Errorf("create state syncer fail: %s, exit", err)
		return
	}

	err = syncer.FinishTask()
	if err != nil {
		logger.Errorf("finish state task fail: %s, exit", err)
		return
	}

	finalS, err := l.GetCurrentStateHash()
	if err != nil {
		logger.Errorf("get state hash fail: %s, exit", err)
		return
	}
	finalInf, err := l.GetBlockchainInfo()
	if err != nil {
		logger.Errorf("get chain info  fail: %s, exit", err)
		return
	}

	logger.Info(" ---------- Congratulations, sync done, following is the infos --------------")
	logger.Infof("State hash: %X", finalS)
	logger.Infof("Chain top: %d:%X", finalInf.GetHeight()-1, finalInf.GetCurrentBlockHash())
}

func main() {

	ncfg := new(node.NodeConfig)
	if err := ncfg.PreConfig(); err != nil {
		panic(err)
	}

	if viper.GetBool("sync.test.server") {
		ncfg.PostRun = runAsServer
		node.RunNode(ncfg)
	} else {
		ncfg.TaskRun = runAsClient
		//we prompt cleanning data path instead of remove it directly for safety
		dataPath := viper.GetString("peer.fileSystemPath")
		if _, err := os.Stat(dataPath); err == nil {
			panic(fmt.Sprintf("PLEASE CLEAN DATA PATH %s first", dataPath))
		}

		node.RunNode(ncfg)
	}

}
