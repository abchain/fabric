package node

import (
	cred "github.com/abchain/fabric/core/cred"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/events/litekfk"
	pb "github.com/abchain/fabric/protos"
)

type CCNameTransformer func(string) string

var nullTransformer = CCNameTransformer(func(in string) string { return in })

/*
	ccSpecValidator organize additional interfaces in a hierarchical fashion
	for chaincode name specification. A interfaces specified for one or some
	chaincode will have priority to handle the corresponding txs, child node
	first and then parent.
	filter ensure each tx will be validated by EXACTLY ONE interface

	an transformer for chaincode name is also appliable
*/

func validatorToHandler(txh cred.TxHandlerFactory) pb.TxPreHandler {
	if txh != nil {
		return txh
	}

	return pb.NilValidator
}

type ccSpecValidator struct {
	parent    *ccSpecValidator
	handlers  map[string]pb.TxPreHandler
	nameTrans CCNameTransformer
}

func NewCCSpecValidator(parent *ccSpecValidator) *ccSpecValidator {
	return &ccSpecValidator{parent, make(map[string]pb.TxPreHandler), nullTransformer}
}

func (f *ccSpecValidator) SetHandler(cc string, h pb.TxPreHandler) {
	curH, ok := f.handlers[cc]
	if !ok {
		f.handlers[cc] = h
	} else {
		f.handlers[cc] = pb.MutipleTxHandler(curH, h)
	}
}

func (f *ccSpecValidator) SetNameTransfer(tf CCNameTransformer) (old CCNameTransformer) {
	old = f.nameTrans
	if f == nil {
		panic("Nil transformer is not allowed")
	}
	f.nameTrans = tf
	return
}

func (f *ccSpecValidator) getHandler(ccname string) pb.TxPreHandler {

	if h, ok := f.handlers[ccname]; ok {
		return h
	}

	if f.parent != nil {
		return f.parent.getHandler(ccname)
	}

	return pb.NilValidator
}

func (f *ccSpecValidator) Handle(txe *pb.TransactionHandlingContext) (*pb.TransactionHandlingContext, error) {

	ccname := f.nameTrans(txe.ChaincodeSpec.GetChaincodeID().GetName())

	return f.getHandler(ccname).Handle(txe)
}

//recordHandler write the incoming tx into a msg-streaming topic
type recordHandler struct {
	//bind the topic map in NodeEngine
	txtopic   map[string]litekfk.Topic
	nameTrans CCNameTransformer
}

func (r *recordHandler) Handle(txe *pb.TransactionHandlingContext) (*pb.TransactionHandlingContext, error) {

	ccname := r.nameTrans(txe.ChaincodeSpec.GetChaincodeID().GetName())
	wtopic := ccname

	topic, ok := r.txtopic[wtopic]
	if !ok {
		wtopic = ""
		topic, ok = r.txtopic[wtopic]
		if !ok {
			txlogger.Debugf("tx %s with ccname %s do not write to any topic", txe.GetTxid(), ccname)
			return txe, nil
		}
	}

	if err := topic.Write(txe); err != nil {
		txlogger.Errorf("Tx %s (for %s) write into topic [%s] fail: %s", txe.GetTxid(), ccname, wtopic, err)
		return txe, pb.ValidateInterrupt
	}
	txlogger.Debugf("tx [%s] (for %s) write to topic [%s]", txe.GetTxid(), ccname, wtopic)

	return txe, nil
}

//txPoolHandler write the incoming tx into the txpool of ledger
type txPoolHandler struct {
	l *ledger.Ledger
}

func (t txPoolHandler) Handle(txe *pb.TransactionHandlingContext) (*pb.TransactionHandlingContext, error) {
	t.l.PoolTransaction(txe)
	txlogger.Debugf("tx [%s] has been pooled to ledger <%p>", txe.GetTxid(), t.l)
	return txe, nil
}
