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
	"errors"
	"fmt"
	"time"

	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"

	"github.com/abchain/fabric/core/ledger"
	pb "github.com/abchain/fabric/protos"
)

type ExecuteResult struct {
	State  ledger.TxExecStates
	Events []*pb.ChaincodeEvent
	Resp   []byte
}

//Execute2 - like legacy execute, but not relay the global ledger object and supposed the transaction has
//be make pre-exec
func Execute2(ctxt context.Context, ledgerObj *ledger.Ledger, chain *ChaincodeSupport, te *pb.TransactionHandlingContext, texec ledger.TxExecStates) (*ExecuteResult, error) {

	if te.ChaincodeSpec == nil {
		return nil, fmt.Errorf("Tx handling context has not enough information yet")
	}

	cds := te.ChaincodeDeploySpec

	// YA-fabric: the condition on container side is complex enough so we could not make any assurance for creating image
	// before we actually lauch it (chaincode container side may remove the image, fabric may not execute the deploy tx ...)
	// so we simply skip this step now
	// if t.Type == pb.Transaction_CHAINCODE_DEPLOY {
	// 	if err := chain.Deploy(ctxt, cds); err != nil {
	// 		return nil, fmt.Errorf("Failed to deploy chaincode spec(%s)", err)
	// 	}
	// }

	//will launch if necessary (and wait for ready)
	err, chrte := chain.Launch(ctxt, ledgerObj, te.ChaincodeName, cds)
	if err != nil {
		return nil, fmt.Errorf("Failed to launch chaincode spec(%s)", err)
	}

	txSuccess := false
	defer func() {
		// **** for deploy, we add final checking ****
		if te.GetType() != pb.Transaction_CHAINCODE_DEPLOY {
			return
		}

		if !txSuccess {
			chaincodeLogger.Infof("stopping due to error while final deploy tx")
			errIgnore := chain.Stop(ctxt, ledgerObj.Tag(), te.ChaincodeDeploySpec)
			if errIgnore != nil {
				chaincodeLogger.Debugf("error on stop %s", errIgnore)
			}
		}
	}()

	if te.Type == pb.Transaction_CHAINCODE_DEPLOY {
		err = chain.FinalDeploy(chrte, te, texec)
		if err != nil {
			return nil, fmt.Errorf("Failed to final deployment tx (%s)", err)
		}
	}

	//this should work because it worked above...
	resp, err := chain.Execute(ctxt, chrte, te, texec)

	if err != nil {
		return nil, fmt.Errorf("Failed to execute transaction or query(%s)", err)
	} else if resp == nil {
		return nil, fmt.Errorf("Failed to receive a response for (%s)", te.Txid)
	}

	if resp.Type == pb.ChaincodeMessage_COMPLETED || resp.Type == pb.ChaincodeMessage_QUERY_COMPLETED {
		// Success
		txSuccess = true

		evnts := resp.ChaincodeEvents
		//respect the legacy code: which will pack at most one event in ChaincodeEvent (not -s!) field
		if resp.ChaincodeEvent != nil {
			evnts = append(evnts, resp.ChaincodeEvent)
		}

		for i, _ := range evnts {
			evnts[i].ChaincodeID = te.ChaincodeName
			evnts[i].TxID = te.Txid
		}

		//		chaincodeLogger.Debugf("tx %s exec done: %x, %v", shorttxid(t.Txid), resp.Payload, resp.ChaincodeEvent)
		return &ExecuteResult{texec, evnts, resp.Payload}, nil
	} else if resp.Type == pb.ChaincodeMessage_ERROR || resp.Type == pb.ChaincodeMessage_QUERY_ERROR {
		// Rollback transaction
		return &ExecuteResult{texec, nil, resp.Payload}, fmt.Errorf("Transaction or query returned with failure: %s", string(resp.Payload))
	}
	return nil, fmt.Errorf("receive a response for (%s) but in invalid state(%d)", te.Txid, resp.Type)

}

var legacyHandler = pb.DefaultTxHandler

//Execute - execute transaction or a query
func Execute(ctxt context.Context, chain *ChaincodeSupport, t *pb.Transaction) ([]byte, []*pb.ChaincodeEvent, error) {

	// get a handle to ledger to mark the begin/finish of a tx
	execstate := ledger.TxExecStates{}
	ledger, ledgerErr := ledger.GetLedger()
	if ledgerErr != nil {
		return nil, nil, fmt.Errorf("Failed to get handle to ledger (%s)", ledgerErr)
	}

	te := pb.NewTransactionHandlingContext(t)
	te, err := legacyHandler.Handle(te)
	if err != nil {
		return nil, nil, err
	}

	execstate.InitForInvoking(ledger)

	markTxBegin(ledger, te.Transaction)
	result, err := Execute2(ctxt, ledger, chain, te, execstate)

	defer func(err error) {

		if result != nil && !result.State.IsEmpty() {
			result.State.AppyInvokingResult(ledger)
		}
		txSuccess := err == nil
		markTxFinish(ledger, te.Transaction, txSuccess)
	}(err)

	if result != nil {
		return result.Resp, result.Events, err
	} else {
		return nil, nil, err
	}
}

var emptyEvent = new(pb.ChaincodeEvent)

//The new exec entry for YA-fabric, which evaluating a series of txs (with handling context)
//in sequence and prepare for a TxEvaluateAndCommit,
//the success tx will be returned, respecting the expected behavior in ledger's commitTxBatch
func ExecuteTransactions2(ctxt context.Context, cname ChainName, xacts []*pb.TransactionHandlingContext, agent *ledger.TxEvaluateAndCommit) (succeededTXs []*pb.Transaction, err error) {
	var chain = GetChain(cname)
	if chain == nil {
		// TODO: We should never get here, but otherwise a good reminder to better handle
		panic(fmt.Sprintf("[ExecuteTransactions]Chain %s not found\n", cname))
	}

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("encounter fatal error in exec tx: %v", r)
		}
	}()

	for _, t := range xacts {
		execstate := agent.AssignExecRT()
		result, err := Execute2(ctxt, agent.Ledger(), chain, t, execstate)
		//
		var txResult *pb.TransactionResult
		if err != nil {
			txResult = &pb.TransactionResult{
				Txid:      t.GetTxid(),
				Error:     err.Error(),
				ErrorCode: 1,
			}
		} else {
			txResult = &pb.TransactionResult{
				Txid:            t.GetTxid(),
				Result:          result.Resp,
				ChaincodeEvents: result.Events,
			}
			succeededTXs = append(succeededTXs, t.Transaction)
		}

		agent.AddExecResult(result.State, txResult)
	}

	return
}

//ExecuteTransactions - will execute transactions on the array one by one
//will return an array of errors one for each transaction. If the execution
//succeeded, array element will be nil. returns []byte of state hash or
//error
//YA-fabric: ExecuteTransactions is badly designed, the events has been truncated to first one
//and should be deprecated later
func ExecuteTransactions(ctxt context.Context, cname ChainName, xacts []*pb.Transaction) (succeededTXs []*pb.Transaction, stateHash []byte, ccevents []*pb.ChaincodeEvent, txerrs []error, err error) {
	var chain = GetChain(cname)
	if chain == nil {
		// TODO: We should never get here, but otherwise a good reminder to better handle
		panic(fmt.Sprintf("[ExecuteTransactions]Chain %s not found\n", cname))
	}

	txerrs = make([]error, len(xacts))
	ccevents = make([]*pb.ChaincodeEvent, len(xacts))
	var txEvnt []*pb.ChaincodeEvent
	var succeededTxs = make([]*pb.Transaction, 0)
	for i, t := range xacts {
		_, txEvnt, txerrs[i] = Execute(ctxt, chain, t)
		if len(txEvnt) > 0 {
			//truncate the event array into only one ...
			ccevents[i] = txEvnt[0]
		} else {
			ccevents[i] = emptyEvent
		}
		if txerrs[i] == nil {
			succeededTxs = append(succeededTxs, t)
		}
	}

	var lgr *ledger.Ledger
	lgr, err = ledger.GetLedger()
	if err == nil {
		stateHash, err = lgr.GetTempStateHash()
	}

	return succeededTxs, stateHash, ccevents, txerrs, err
}

var errFailedToGetChainCodeSpecForTransaction = errors.New("Failed to get ChainCodeSpec from Transaction")

func getTimeout(cID *pb.ChaincodeID) (time.Duration, error) {
	ledger, err := ledger.GetLedger()
	if err == nil {
		chaincodeID := cID.Name
		txID, err := ledger.GetState(chaincodeID, "github.com_openblockchain_obc-peer_chaincode_id", true)
		if err == nil {
			tx, err := ledger.GetTransactionByID(string(txID))
			if err == nil {
				chaincodeDeploymentSpec := &pb.ChaincodeDeploymentSpec{}
				proto.Unmarshal(tx.Payload, chaincodeDeploymentSpec)
				chaincodeSpec := chaincodeDeploymentSpec.GetChaincodeSpec()
				timeout := time.Duration(time.Duration(chaincodeSpec.Timeout) * time.Millisecond)
				return timeout, nil
			}
		}
	}

	return -1, errFailedToGetChainCodeSpecForTransaction
}

func markTxBegin(ledger *ledger.Ledger, t *pb.Transaction) {
	if t.Type == pb.Transaction_CHAINCODE_QUERY {
		return
	}
	ledger.TxBegin(t.Txid)
}

func markTxFinish(ledger *ledger.Ledger, t *pb.Transaction, successful bool) {
	if t.Type == pb.Transaction_CHAINCODE_QUERY {
		return
	}
	ledger.TxFinished(t.Txid, successful)
}
