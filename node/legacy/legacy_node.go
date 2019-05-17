package legacynode

import (
	"fmt"
	"github.com/abchain/fabric/consensus/helper"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/peer"
	"github.com/abchain/fabric/core/util"
	"github.com/abchain/fabric/node"
	"github.com/abchain/fabric/node/start"
	pb "github.com/abchain/fabric/protos"
	proto "github.com/golang/protobuf/proto"
	"github.com/op/go-logging"
)

var logger = logging.MustGetLogger("legacymode")

type LegacyEngineAdapter struct {
	peer.Engine
}

func (e *LegacyEngineAdapter) Init() error {
	defPeer := startnode.GetNode().DefaultPeer()
	var err error

	e.Engine, err = helper.GetEngine(defPeer.Peer, defPeer.Syncer())
	if err != nil {
		return err
	}

	logger.Info("Init legacy engine in old consensus module")
	//IMPORTANT: also notify ledger use legacy mode
	ledger.UseLegacyModeOnLedger()
	return nil
}

func (e *LegacyEngineAdapter) Scheme(thenode *node.NodeEngine) {
	//set node is not pool and topic, all tx is handled by legacy module
	for _, pr := range thenode.Peers {
		pr.TxHandlerOpts.NoPooling = true
	}
	delete(thenode.TxTopic, "")

	legacyHandler := pb.TxFuncAsTxPreHandler(func(transaction *pb.Transaction) (*pb.Transaction, error) {

		logger.Debugf("Marshalling transaction %s to send to local engine", transaction.Type)
		data, err := proto.Marshal(transaction)
		if err != nil {
			return nil, fmt.Errorf("Error sending transaction to local engine: %s", err)
		}

		var response *pb.Response
		msg := &pb.Message{Type: pb.Message_CHAIN_TRANSACTION, Payload: data, Timestamp: util.CreateUtcTimestamp()}
		logger.Debugf("Sending message %s with timestamp %v to local engine", msg.Type, msg.Timestamp)
		response = e.ProcessTransactionMsg(msg, transaction)

		if response.Status == pb.Response_SUCCESS {
			return transaction, nil
		} else {
			return nil, fmt.Errorf("resp from engine fail: %s", string(response.Msg))
		}
	})

	thenode.CustomFilters = append(thenode.CustomFilters, legacyHandler)
	thenode.Options.MakeGenesisForLedger = true
}
