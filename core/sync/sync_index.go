package sync

import (
	"fmt"
	"github.com/abchain/fabric/core/ledger"
	pb "github.com/abchain/fabric/protos"
)

type syncIndexHandler struct {
	*sessionRT
	ledgerSN *ledger.LedgerSnapshot
	gLedger  *ledger.LedgerGlobal
}

func newSyncIndexHandler(shash []byte, root *syncHandler) (*syncIndexHandler, error) {

	if len(shash) == 0 {
		return nil, fmt.Errorf("Empty state hash is not allowed")
	}

	bkn, err := root.localLedger.GetBlockNumberByState(shash)
	if err != nil {
		return nil, err
	} else if bkn == 0 {
		return nil, fmt.Errorf("Target state is not existed")
	}

	sn, _ := root.localLedger.CreateSnapshot()

	return &syncIndexHandler{
		sessionRT: newSessionRT(root, sn.Release),
		ledgerSN:  sn,
		gLedger:   root.localLedger.LedgerGlobal,
	}, nil
}

func (h *syncIndexHandler) onSessionRequest(req *pb.TransferRequest) error {

	till, err := h.ledgerSN.GetBlockchainSize()
	if err != nil {
		return err
	}

	go h.flushIndexs(req.GetIndex(), till, h.RequestNew())
	return nil
}

func (h *syncIndexHandler) flushIndexs(from, till uint64, waitF func() error) {

	for i := from; i <= till; i++ {
		block, err := h.ledgerSN.GetRawBlockByNumber(i)
		if err != nil {
			logger.Errorf("Error querying indexs for blockNum %d: %s", i, err)
			h.core.SessionFailure(err)
			return
		}

		pack := &pb.StateIndex{
			Height:      i,
			Hash:        block.GetStateHash(),
			CorePayload: h.gLedger.GetConsensusData(block.GetStateHash()),
		}

		if err = h.core.SessionSend(&pb.TransferResponse{Index: pack}); err != nil {
			logger.Errorf("send package fail: %s", err)
			return
		}

		if err = waitF(); err != nil {
			logger.Infof("request interrupted block syncing: %s", err)
			return
		}
	}
}

type indexSyncClient struct {
}

func (cli *indexSyncClient) PreFilter(rledger *pb.LedgerState) bool         { return false }
func (cli *indexSyncClient) OnConnected(int, *pb.AcceptSession) error       { return nil }
func (cli *indexSyncClient) Next(int) (*pb.TransferRequest, interface{})    { return nil, nil }
func (cli *indexSyncClient) OnData(interface{}, *pb.TransferResponse) error { return NormalEnd{} }
func (cli *indexSyncClient) OnFail(interface{}, error)                      {}
