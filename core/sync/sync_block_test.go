package sync

import (
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/ledger/testutil"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
	"testing"
)

func formTestData(t *testing.T, ledger *ledger.Ledger, height int) {

	//add gensis block
	ledger.BeginTxBatch(0)
	ledger.CommitTxBatch(0, nil, nil, nil)

	fillBlocks(t, ledger, height)

	statehash, err := ledger.GetCurrentStateHash()
	if err != nil {
		t.Fatalf("Get unexpected GetStateDelta result")
	}

	t.Logf("Target statehash: %x", statehash)
}

func fillBlocks(t *testing.T, ledger *ledger.Ledger, height int) {

	offset := int(ledger.GetBlockchainSize())

	for ib := 0; ib < height; ib++ {

		transaction, _ := pb.NewTransaction(pb.ChaincodeID{Path: "testUrl"}, "", "", []string{"param1, param2"})
		dig, _ := transaction.Digest()
		transaction.Txid = pb.TxidFromDigest(dig)

		ledger.BeginTxBatch(ib)
		ledger.TxBegin("txUuid")
		ledger.SetState("chaincode1", "keybase", []byte{byte(ib + offset)})
		ledger.SetState("chaincode2", "keybase", []byte{byte(ib + offset)})
		ledger.SetState("chaincode3", "keybase", []byte{byte(ib + offset)})
		ledger.TxFinished("txUuid", true)
		ledger.CommitTxBatch(ib, []*pb.Transaction{transaction}, nil, []byte("proof1"))
	}
}

func getBlockInfo(t *testing.T, src *ledger.Ledger, h uint64) []byte {
	blk, err := src.GetRawBlockByNumber(h)
	testutil.AssertNoError(t, err, "get target blk")
	testutil.AssertNotNil(t, blk)
	blkh, _ := blk.GetHash()
	testutil.AssertNotNil(t, blkh)
	return blkh

}

func testBlocks(t *testing.T, src, target *ledger.Ledger, h uint64) {
	blk, err := src.GetRawBlockByNumber(h)
	testutil.AssertNoError(t, err, "get target blk")
	testutil.AssertNotNil(t, blk)
	blkh, _ := blk.GetHash()
	testutil.AssertNotNil(t, blkh)

	targetblkh := getBlockInfo(t, target, h)

	testutil.AssertEquals(t, blkh, targetblkh)

	tx, err := target.GetTransactionByID(blk.GetTxids()[0])
	testutil.AssertNoError(t, err, "get target tx")
	testutil.AssertNotNil(t, tx)
}

func filterErr(err error) error {
	switch err.(type) {
	case NormalEnd:
		return nil
	default:
		return err
	}
}

func TestBlockSync_Basic(t *testing.T) {

	testHeight := 5
	syncTargetHeight := uint64(testHeight - 2)

	testLedger := ledger.InitTestLedger(t)
	formTestData(t, testLedger, testHeight)

	targetLedger, endf := ledger.InitSecondaryTestLedger(t)
	defer endf()

	testCtx, endTest := context.WithCancel(context.Background())
	defer endTest()

	handler := newSyncHandler(testCtx, &pb.PeerID{Name: "test1"}, testLedger, DefaultSyncOption())

	cliCore := newFsmHandler()
	cliCore.sessionOpt.maxWindow = DefaultSyncOption().baseSessionWindow

	blockCli := NewBlockSyncClient(testCtx, targetLedger, syncTargetHeight, getBlockInfo(t, testLedger, syncTargetHeight))
	dummyMsg, endDF := NewReceiver(handler.core.syncCore, cliCore, nil)
	defer endDF()
	err := blockCli.handlingFunc(1, dummyMsg, cliCore)

	testutil.AssertNoError(t, filterErr(err), "Full syncing")

	testBlocks(t, testLedger, targetLedger, 2)
	testBlocks(t, testLedger, targetLedger, 3)
}

func TestBlockSync_Large(t *testing.T) {

	testHeight := 200
	syncTargetHeight := uint64(testHeight - 20)

	testLedger := ledger.InitTestLedger(t)
	formTestData(t, testLedger, testHeight)

	targetLedger, endf := ledger.InitSecondaryTestLedger(t)
	defer endf()

	testCtx, endTest := context.WithCancel(context.Background())
	defer endTest()

	handler := newSyncHandler(testCtx, &pb.PeerID{Name: "test1"}, testLedger, DefaultSyncOption())

	cliCore := newFsmHandler()
	cliCore.sessionOpt.maxWindow = DefaultSyncOption().baseSessionWindow

	blockCli := NewBlockSyncClient(testCtx, targetLedger, syncTargetHeight, getBlockInfo(t, testLedger, syncTargetHeight))
	dummyMsg, endDF := NewReceiver(handler.core.syncCore, cliCore, nil)
	defer endDF()
	err := blockCli.handlingFunc(1, dummyMsg, cliCore)

	testutil.AssertNoError(t, filterErr(err), "Full syncing")

	testutil.AssertEquals(t, targetLedger.TestContinuouslBlockRange(), syncTargetHeight)
	testBlocks(t, testLedger, targetLedger, 2)
	testBlocks(t, testLedger, targetLedger, 10)
	testBlocks(t, testLedger, targetLedger, 30)
	testBlocks(t, testLedger, targetLedger, 50)
	testBlocks(t, testLedger, targetLedger, 100)
	testBlocks(t, testLedger, targetLedger, 120)

}

func TestBlockSync_Resume(t *testing.T) {

	testHeight := 50

	testLedger := ledger.InitTestLedger(t)
	formTestData(t, testLedger, testHeight)

	targetLedger, endf := ledger.InitSecondaryTestLedger(t)
	defer endf()

	testCtx, endTest := context.WithCancel(context.Background())
	defer endTest()

	handler := newSyncHandler(testCtx, &pb.PeerID{Name: "test1"}, testLedger, DefaultSyncOption())

	cliCore := newFsmHandler()
	cliCore.sessionOpt.maxWindow = DefaultSyncOption().baseSessionWindow

	//first we try to sync to block 20 ...
	syncTargetHeight := uint64(20)
	blockCli1 := NewBlockSyncClient(testCtx, targetLedger, syncTargetHeight, getBlockInfo(t, testLedger, syncTargetHeight))
	dummyMsg, endDF := NewReceiver(handler.core.syncCore, cliCore, nil)
	defer endDF()
	err := blockCli1.handlingFunc(1, dummyMsg, cliCore)

	testutil.AssertNoError(t, filterErr(err), "syncing phase 1")
	testutil.AssertEquals(t, targetLedger.TestContinuouslBlockRange(), syncTargetHeight)
	testBlocks(t, testLedger, targetLedger, 20)

	//then from 30 to 35 (so 20 to 29 is left vanished)
	syncTargetHeight2 := uint64(35)
	blockCli2 := NewBlockSyncClient(testCtx, targetLedger, syncTargetHeight2, getBlockInfo(t, testLedger, syncTargetHeight2))
	cliImpl := blockCli2.SessionClientImpl.(*blockSyncClient)
	cliImpl.tillHeight = syncTargetHeight2 - 5

	err = blockCli2.handlingFunc(1, dummyMsg, cliCore)
	testutil.AssertNoError(t, filterErr(err), "syncing phase 2")

	testutil.AssertEquals(t, targetLedger.TestContinuouslBlockRange(), syncTargetHeight)
	testutil.AssertEquals(t, targetLedger.GetBlockchainSize(), syncTargetHeight2+1)
	testBlocks(t, testLedger, targetLedger, 30)

	syncTargetHeight3 := uint64(49)
	blockCli3 := NewBlockSyncClient(testCtx, targetLedger, syncTargetHeight3, getBlockInfo(t, testLedger, syncTargetHeight3))
	cliImpl = blockCli3.SessionClientImpl.(*blockSyncClient)
	testutil.AssertEquals(t, cliImpl.tillHeight, uint64(syncTargetHeight)+1)

	err = blockCli3.handlingFunc(1, dummyMsg, cliCore)
	testutil.AssertNoError(t, filterErr(err), "syncing phase 3")
	testutil.AssertEquals(t, len(cliImpl.interrupted), 0)

	testutil.AssertEquals(t, targetLedger.TestContinuouslBlockRange(), syncTargetHeight3)
	testutil.AssertEquals(t, targetLedger.GetBlockchainSize(), syncTargetHeight3+1)
	testBlocks(t, testLedger, targetLedger, 25)
	testBlocks(t, testLedger, targetLedger, 40)
	testBlocks(t, testLedger, targetLedger, 49)
}

func TestBlockSync_WrongData(t *testing.T) {

	testHeight := 5
	syncTargetHeight := uint64(testHeight - 2)

	testLedger := ledger.InitTestLedger(t)
	formTestData(t, testLedger, testHeight)

	targetLedger, endf := ledger.InitSecondaryTestLedger(t)
	defer endf()

	originalBlk, err := testLedger.GetRawBlockByNumber(2)
	testutil.AssertNoError(t, err, "get block in ledger")

	errblk := pb.NewBlock(nil, []byte("wrong data"))
	errblk.PreviousBlockHash = []byte("anyhash")
	errblk.StateHash = []byte("anystatehash")

	err = testLedger.PutRawBlock(errblk, 2)
	testutil.AssertNoError(t, err, "put block in ledger")

	//need to commit one block more to update current snapshot
	fillBlocks(t, testLedger, 1)

	testCtx, endTest := context.WithCancel(context.Background())
	defer endTest()

	handler := newSyncHandler(testCtx, &pb.PeerID{Name: "test1"}, testLedger, DefaultSyncOption())

	cliCore := newFsmHandler()
	cliCore.sessionOpt.maxWindow = DefaultSyncOption().baseSessionWindow

	blockCli := NewBlockSyncClient(testCtx, targetLedger, syncTargetHeight, getBlockInfo(t, testLedger, syncTargetHeight))
	dummyMsg, endDF := NewReceiver(handler.core.syncCore, cliCore, nil)
	defer endDF()
	cliImpl := blockCli.SessionClientImpl.(*blockSyncClient)

	err = blockCli.handlingFunc(1, dummyMsg, cliCore)
	testutil.AssertError(t, filterErr(err), "syncing wrong data")
	testutil.AssertEquals(t, len(cliImpl.interrupted), 1)

	err = testLedger.PutRawBlock(originalBlk, 2)
	testutil.AssertNoError(t, err, "resume block in ledger")
	fillBlocks(t, testLedger, 1)

	err = blockCli.handlingFunc(1, dummyMsg, cliCore)
	testutil.AssertNoError(t, filterErr(err), "syncing resumed data")
	testutil.AssertEquals(t, len(cliImpl.interrupted), 0)
	testBlocks(t, testLedger, targetLedger, 2)

}
