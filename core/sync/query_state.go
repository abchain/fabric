package sync

import (
	"github.com/abchain/fabric/core/ledger"
	pb "github.com/abchain/fabric/protos"
)

type stateQueryHandler struct {
	*sessionRT
	ledgerSN *ledger.LedgerSnapshot
}

func newStateQueryHandler(root *syncHandler) (*stateQueryHandler, error) {

	sn, _ := root.localLedger.CreateSnapshot()

	return &stateQueryHandler{
		sessionRT: newSessionRT(root, sn.Release),
		ledgerSN:  sn,
	}, nil
}

func (h *stateQueryHandler) onSessionRequest(req *pb.TransferRequest) error {

	qheight := req.GetQuery()

	blk, err := h.ledgerSN.GetRawBlockByNumber(qheight)
	if err != nil {
		return err
	}

	pack := &pb.StateIndex{Height: qheight, Hash: blk.GetStateHash()}
	//we do not do congest controlling

	if err = h.core.SessionSend(&pb.TransferResponse{Index: pack}); err != nil {
		logger.Errorf("send package fail: %s", err)
	}

	return nil
}

type stateQueryClient struct {
	ledger *ledger.Ledger

	//the final query result should be exported (which state we should start from)
	exportedResult []byte
}

func (cli *stateQueryClient) PreFilter(rledger *pb.LedgerState) bool         { return false }
func (cli *stateQueryClient) OnConnected(int, *pb.AcceptSession) error       { return nil }
func (cli *stateQueryClient) Next(int) (*pb.TransferRequest, interface{})    { return nil, nil }
func (cli *stateQueryClient) OnData(interface{}, *pb.TransferResponse) error { return NormalEnd{} }
func (cli *stateQueryClient) OnFail(interface{}, error)                      {}
