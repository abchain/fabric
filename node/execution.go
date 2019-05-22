package node

import (
	"fmt"
	"github.com/abchain/fabric/core/chaincode"
	cred "github.com/abchain/fabric/core/cred"
	_ "github.com/abchain/fabric/core/gossip/txnetwork"
	"github.com/abchain/fabric/core/ledger"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
)

//passing endorser will be released by function and should not be reused

func (ne *NodeEngine) QueryTransaction(ctx context.Context,
	tx *pb.Transaction, endorser cred.TxEndorser, l *ledger.Ledger, remote ...*PeerEngine) (resp *pb.Response) {

	var err error
	defer func() {
		if err != nil {
			resp = &pb.Response{pb.Response_FAILURE, []byte(err.Error())}
		}
	}()

	if endorser != nil {
		tx, err = endorser.EndorseTransaction(tx)
		if err != nil {
			return
		}
	}

	//TODO: local execute can be disabled
	var ret *chaincode.ExecuteResult
	var txe *pb.TransactionHandlingContext
	txe, err = pb.NewPlainTxHandlingContext(tx)
	tout := ledger.TxExecStates{}
	tout.InitForQuerying(l)
	ret, err = chaincode.Execute2(ctx, l, chaincode.GetDefaultChain(), txe, tout)
	if err == nil {
		resp = &pb.Response{pb.Response_SUCCESS, []byte(ret.Resp)}
		return
	} else {
		txlogger.Infof("local query fail: %s, try remote", err)
	}

	for _, netw := range remote {
		if endorser == nil {
			//try to endorse tx by each default endorser in peer
			if endorser := netw.GenTxEndorser(); endorser != nil {
				tx, err = endorser.EndorseTransaction(tx)
				if err != nil {
					txlogger.Errorf("Can not endorse tx in network [%s]: %s", string(netw.defaultEndorser.EndorserId()), err)
				}
				endorser.Release()
			}
		}
		//TODO: or we can dispatch it to neighbours in specified network
	}

	err = fmt.Errorf("Remote query is not implied yet")
	return
}

func (ne *NodeEngine) ExecuteTransaction(ctx context.Context,
	tx *pb.Transaction, endorser cred.TxEndorser, targets ...*PeerEngine) (resp []*pb.Response) {

	for _, netw := range targets {
		resp = append(resp, netw.txn.ExecuteTransaction(ctx, tx, endorser))
	}

	return
}
