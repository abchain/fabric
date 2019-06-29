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

package protos

import (
	bin "encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/abchain/fabric/core/util"
	"github.com/golang/protobuf/proto"
	"hash"
	"strings"
)

func TxidFromDigest(digest []byte) string {

	//truncate the digest which is too long

	if len(digest) > 32 {
		digest = digest[:32]
	}

	return fmt.Sprintf("%x", digest)
}

func normalizedTxFuncWithAlg(h hash.Hash) func(*Transaction) string {

	if h == nil {
		panic("hash alog is nil")
	}

	return func(tx *Transaction) string {
		//we share the hash function for the txs in the whole block
		//and it must be reset at the beginning of each calling
		h.Reset()
		dlg, err := tx.digest(h)
		if err != nil {
			return fmt.Sprintf("###wrongtxdigest [%s]###", err)
		}
		return TxidFromDigest(dlg)
	}
}

func (t *Transaction) IsValid() bool {
	return t.isValid(util.DefaultCryptoHash())
}

func (t *Transaction) IsValidWithAlg(customIDgenAlg string) bool {
	h := util.CryptoHashByAlg(customIDgenAlg)
	if h == nil {
		logger.Errorf("Wrong hash algorithm was given: %s", customIDgenAlg)
		return false
	} else {
		return t.isValid(h)
	}
}

func (t *Transaction) isValid(h hash.Hash) bool {
	if d, err := t.digest(h); err != nil {
		return false
	} else {
		return t.GetTxid() == TxidFromDigest(d)
	}
}

func (t *Transaction) Digest() ([]byte, error) {
	return t.digest(util.DefaultCryptoHash())
}

func (t *Transaction) DigestWithAlg(customIDgenAlg string) ([]byte, error) {

	h := util.CryptoHashByAlg(customIDgenAlg)
	if h == nil {
		return nil, fmt.Errorf("Wrong hash algorithm was given: %s", customIDgenAlg)
	} else {
		return t.digest(h)
	}
}

// Digest generate a solid digest for transaction contents,
// It ensure a bitwise equality to txs regardless the versions or extends in future
// and the cost should be light on both memory and computation
func (t *Transaction) digest(h hash.Hash) ([]byte, error) {

	hash := util.NewHashWriter(h)

	err := hash.Write(t.ChaincodeID).Write(t.Payload).Write(t.Metadata).Write(t.Nonce).Error()

	if err != nil {
		return nil, err
	}

	if t.Timestamp != nil {
		//so we do not digest the nano part in ts ...
		err = bin.Write(h, bin.BigEndian, t.Timestamp.Seconds)
		if err != nil {
			return nil, err
		}
	}

	err = bin.Write(h, bin.BigEndian, int32(t.ConfidentialityLevel))
	if err != nil {
		return nil, err
	}

	//we can add more items for tx in future
	return h.Sum(nil), nil
}

// Bytes returns this transaction as an array of bytes.
func (transaction *Transaction) Bytes() ([]byte, error) {
	data, err := proto.Marshal(transaction)
	if err != nil {
		logger.Errorf("Error marshalling transaction: %s", err)
		return nil, fmt.Errorf("Could not marshal transaction: %s", err)
	}
	return data, nil
}

func (txResult *TransactionResult) Bytes() ([]byte, error) {
	data, err := proto.Marshal(txResult)
	if err != nil {
		logger.Errorf("Error marshalling transaction events result: %s", err)
		return nil, fmt.Errorf("Could not marshal transaction events result : %s", err)
	}
	return data, nil
}

func (transaction *Transaction) ParseChaincodeID() (*ChaincodeID, error) {
	ccid := &ChaincodeID{}
	if err := proto.Unmarshal(transaction.GetChaincodeID(), ccid); err != nil {
		return nil, fmt.Errorf("protobuf decode chaincodeID fail %s", err.Error())
	}
	return ccid, nil
}

func (transaction *Transaction) ParsePayloadAsInvoking() (*ChaincodeInvocationSpec, error) {
	invoke := &ChaincodeInvocationSpec{}
	if err := proto.Unmarshal(transaction.GetPayload(), invoke); err != nil {
		return nil, fmt.Errorf("protobuf decode invoke fail %s", err.Error())
	}
	return invoke, nil
}

func (transaction *Transaction) ParsePayloadAsDeploy() (*ChaincodeDeploymentSpec, error) {
	cds := &ChaincodeDeploymentSpec{}
	if err := proto.Unmarshal(transaction.GetPayload(), cds); err != nil {
		return nil, fmt.Errorf("protobuf decode deployment fail %s", err.Error())
	}
	return cds, nil
}

func UnmarshallTransactionResult(txebyte []byte) (*TransactionResult, error) {
	txe := &TransactionResult{}
	err := proto.Unmarshal(txebyte, txe)
	if err != nil {
		logger.Errorf("Error unmarshalling Transaction event: %s", err)
		return nil, fmt.Errorf("Could not unmarshal Transaction event: %s", err)
	}
	return txe, nil
}

func UnmarshallTransaction(transaction []byte) (*Transaction, error) {
	tx := &Transaction{}
	err := proto.Unmarshal(transaction, tx)
	if err != nil {
		logger.Errorf("Error unmarshalling Transaction: %s", err)
		return nil, fmt.Errorf("Could not unmarshal Transaction: %s", err)
	}
	return tx, nil
}

func toArgs(arguments []string) (ret [][]byte) {

	for _, arg := range arguments {
		ret = append(ret, []byte(arg))
	}

	return
}

// YA-fabric 0.9: deprecated this
// NewTransaction creates a new transaction. It defines the function to call,
// the chaincodeID on which the function should be called, and the arguments
// string. The arguments could be a string of JSON, but there is no strict
// requirement.
func NewTransaction(chaincodeID ChaincodeID, uuid string, function string, arguments []string) (*Transaction, error) {
	data, err := proto.Marshal(&chaincodeID)
	if err != nil {
		return nil, fmt.Errorf("Could not marshal chaincode : %s", err)
	}
	transaction := new(Transaction)
	transaction.ChaincodeID = data
	transaction.Txid = uuid
	transaction.Timestamp = CreateUtcTimestamp()

	// Build the spec
	spec := &ChaincodeSpec{Type: ChaincodeSpec_GOLANG,
		ChaincodeID: &chaincodeID, CtorMsg: &ChaincodeInput{Args: toArgs(arguments)}}

	// Build the ChaincodeInvocationSpec message
	invocation := &ChaincodeInvocationSpec{ChaincodeSpec: spec}

	payloaddata, err := proto.Marshal(invocation)
	if err != nil {
		return nil, fmt.Errorf("Could not marshal payload for chaincode invocation: %s", err)
	}

	transaction.Payload = payloaddata

	return transaction, nil
}

// create a new transaction with its "core" field is specified
func NewTransactionCore(ttype Transaction_Type, chaincodepayload []byte, basepayload []byte) *Transaction {
	transaction := new(Transaction)
	transaction.ChaincodeID = chaincodepayload
	transaction.Timestamp = CreateUtcTimestamp()
	transaction.Payload = basepayload
	transaction.Type = ttype
	transaction.ConfidentialityLevel = ConfidentialityLevel_PUBLIC
	//so we mark this tx is "internal" and should be modified before being delivered to network
	transaction.Txid = "internalTransaction"

	return transaction
}

// A transaction which is applied on a EXISTED single chaincode (i.e.: invoke or query ...)
// (it is not easy to handle deployment in the same entry)
// This is used to replace the deprecated NewTransaction entry
// input chaincode is handled and wrapped into generated chaincodespec field
func NewChaincodeTransaction(action Transaction_Type, chaincodeID *ChaincodeID, function string, arguments [][]byte) (*Transaction, error) {

	chaincodepayload, err := proto.Marshal(chaincodeID)
	if err != nil {
		return nil, fmt.Errorf("Could not marshal chaincode : %s", err)
	}

	ccname, _, _ := ParseYFCCName(chaincodeID.GetName())

	// Build the spec
	// Note: fabric wrap chaincodespec as payload is ambiguous and error-prone, the
	// CtorMsg is the only fields which will be considered while being handled
	// (i.e. in chaincode module, though we still add type and chaincode ID fields
	// for possible compatible cases), we may re-define the corresponding messsage
	// type in the future
	spec := &ChaincodeSpec{Type: ChaincodeSpec_GOLANG,
		ChaincodeID: &ChaincodeID{Name: ccname},
		CtorMsg:     &ChaincodeInput{Args: append([][]byte{[]byte(function)}, arguments...)},
	}

	// Build the ChaincodeInvocationSpec message
	var basepayload []byte
	switch action {
	case Transaction_CHAINCODE_INVOKE, Transaction_CHAINCODE_QUERY:
		basepayload, err = proto.Marshal(&ChaincodeInvocationSpec{ChaincodeSpec: spec})
	default:
		return nil, fmt.Errorf("Could not handle specified chaincode action [%s]", action)
	}

	if err != nil {
		return nil, fmt.Errorf("Could not marshal base payload : %s", err)
	}

	return NewTransactionCore(action, chaincodepayload, basepayload), nil

}

// NewChaincodeDeployTransaction is used to deploy chaincode.
func NewChaincodeDeployTransaction(chaincodeDeploymentSpec *ChaincodeDeploymentSpec, uuid string) (*Transaction, error) {
	transaction := new(Transaction)
	transaction.Type = Transaction_CHAINCODE_DEPLOY
	transaction.Txid = uuid
	transaction.Timestamp = CreateUtcTimestamp()
	cID := chaincodeDeploymentSpec.ChaincodeSpec.GetChaincodeID()
	if cID != nil {
		data, err := proto.Marshal(cID)
		if err != nil {
			return nil, fmt.Errorf("Could not marshal chaincode : %s", err)
		}
		transaction.ChaincodeID = data
	}

	//if chaincodeDeploymentSpec.ChaincodeSpec.GetCtorMsg() != nil {
	//	transaction.Function = chaincodeDeploymentSpec.ChaincodeSpec.GetCtorMsg().Function
	//	transaction.Args = chaincodeDeploymentSpec.ChaincodeSpec.GetCtorMsg().Args
	//}
	if ccspec := chaincodeDeploymentSpec.GetChaincodeSpec(); ccspec != nil {

		ccname, _, _ := ParseYFCCName(cID.GetName())
		ccspec.ChaincodeID = &ChaincodeID{Name: ccname}
		defer func() {
			ccspec.ChaincodeID = cID
		}()
	}
	data, err := proto.Marshal(chaincodeDeploymentSpec)
	if err != nil {
		logger.Errorf("Error mashalling payload for chaincode deployment: %s", err)
		return nil, fmt.Errorf("Could not marshal payload for chaincode deployment: %s", err)
	}
	transaction.Payload = data
	transaction.Metadata = chaincodeDeploymentSpec.ChaincodeSpec.Metadata
	return transaction, nil
}

// NewChaincodeExecute is used to invoke chaincode.
func NewChaincodeExecute(chaincodeInvocationSpec *ChaincodeInvocationSpec, uuid string, typ Transaction_Type) (*Transaction, error) {
	transaction := new(Transaction)
	transaction.Type = typ
	transaction.Txid = uuid
	transaction.Timestamp = CreateUtcTimestamp()
	cID := chaincodeInvocationSpec.ChaincodeSpec.GetChaincodeID()
	if cID != nil {
		data, err := proto.Marshal(cID)
		if err != nil {
			return nil, fmt.Errorf("Could not marshal chaincode : %s", err)
		}
		transaction.ChaincodeID = data
	}

	//notice: chaincodeID is not embedded in spec anymore, but we do not change the argument
	if ccspec := chaincodeInvocationSpec.GetChaincodeSpec(); ccspec != nil {
		ccspec.ChaincodeID = &ChaincodeID{Name: cID.GetName()}
		defer func() {
			ccspec.ChaincodeID = cID
		}()
	}
	data, err := proto.Marshal(chaincodeInvocationSpec)
	if err != nil {
		return nil, fmt.Errorf("Could not marshal payload for chaincode invocation: %s", err)
	}
	transaction.Payload = data
	transaction.Metadata = chaincodeInvocationSpec.ChaincodeSpec.Metadata
	return transaction, nil
}

type strArgs struct {
	Function string
	Args     []string
}

// UnmarshalJSON converts the string-based REST/JSON input to
// the []byte-based current ChaincodeInput structure.
func (c *ChaincodeInput) UnmarshalJSON(b []byte) error {
	sa := &strArgs{}
	err := json.Unmarshal(b, sa)
	if err != nil {
		return err
	}
	allArgs := sa.Args
	if sa.Function != "" {
		allArgs = append([]string{sa.Function}, sa.Args...)
	}
	c.Args = util.ToChaincodeArgs(allArgs...)
	return nil
}

/*
  YA-fabric 0.9：
  We define a struct for transaction which is passed in a pipeline
  handling it, the data can be completed progressively along the
  pipeline and finally be delivered for executing. It mainly contain
  fidentiality-releated contents currently and may add more or customed
  fields
*/
type TransactionHandlingContext struct {
	*Transaction //the original transaction
	//first fields which will be tagged from outside, if PeerID is not set,
	//it will be considered as "self peer" in network
	NetworkID, PeerID string
	//every fields can be readout from transaction (unlessed covered by the confidentiality)
	ChaincodeName, ChaincodeTemplate string
	TargetLedgers                    []string
	CanonicalName                    *ChaincodeID
	ChaincodeSpec                    *ChaincodeSpec
	ChaincodeDeploySpec              *ChaincodeDeploymentSpec
	SecContex                        *ChaincodeSecurityContext
	CustomFields                     map[string]interface{}
}

func NewTransactionHandlingContext(t *Transaction) *TransactionHandlingContext {
	ret := new(TransactionHandlingContext)
	ret.Transaction = t
	return ret
}

/*
   read a unencrypted tx and try to fill possible fields
*/

func parsePlainTx(tx *TransactionHandlingContext) (ret *TransactionHandlingContext) {

	ret = tx
	if tx.GetConfidentialityLevel() != ConfidentialityLevel_PUBLIC {
		return
	}

	switch tx.Type {
	case Transaction_CHAINCODE_DEPLOY:
		cds := &ChaincodeDeploymentSpec{}
		if err := proto.Unmarshal(tx.Payload, cds); err != nil {
			logger.Errorf("Unmarshal payload in deploy tx: %s", err)
			return
		}
		ret.ChaincodeDeploySpec = cds
		ret.ChaincodeSpec = cds.GetChaincodeSpec()
	case Transaction_CHAINCODE_INVOKE, Transaction_CHAINCODE_QUERY:
		ci := &ChaincodeInvocationSpec{}

		if err := proto.Unmarshal(tx.Payload, ci); err != nil {
			logger.Errorf("Unmarshal payload in invoking tx: %s", err)
			return
		}
		ret.ChaincodeSpec = ci.GetChaincodeSpec()
	default:
		return
	}

	//replace the embedded chaincodeID field, avoiding misuse (or malice code
	//which set another different cc name trying to bypass the validations)
	ret.ChaincodeSpec.ChaincodeID = ret.CanonicalName
	return
}

func mustParsePlainTx(tx *TransactionHandlingContext) (ret *TransactionHandlingContext, err error) {
	if tx.GetConfidentialityLevel() != ConfidentialityLevel_PUBLIC {
		err = fmt.Errorf("Can't not parse non-public (level:%s) transaction", tx.GetConfidentialityLevel())
		return
	} else if tx.CanonicalName == nil {
		err = fmt.Errorf("Chaincode name has not be parsed yet")
		return
	}

	ret = parsePlainTx(tx)
	if ret.ChaincodeSpec == nil {
		err = fmt.Errorf("Can't not read payload in tx")
	}
	//verify the embedded chaincodeID and replace

	return
}

//parse the chaincode name in YA-fabric 0.9's standard form: [templateName:]ccName[@LedgerName]
func ParseYFCCName(ccfullName string) (ccName, templateName string, ledgerName []string) {
	parsed := strings.Split(ccfullName, ":")

	if len(parsed) >= 2 {
		templateName = parsed[0]
		if len(parsed[1:]) > 1 {
			ccfullName = strings.Join(parsed[1:], "")
		} else {
			ccfullName = parsed[1]
		}
	}

	parsed = strings.Split(ccfullName, "@")
	if len(parsed) >= 2 {
		ledgerName = strings.Split(parsed[1], ",")
	}

	ccName = parsed[0]

	return
}

func parseChaincodeName(tx *TransactionHandlingContext) (ret *TransactionHandlingContext, err error) {

	ret = tx
	if len(tx.GetChaincodeID()) == 0 {
		//empty ccid is not consider as error (may just fail in later handling)
		return
	}

	fccid := new(ChaincodeID)
	if err = proto.Unmarshal(tx.GetChaincodeID(), fccid); err != nil {
		return
	}

	ret.CanonicalName = fccid
	if yfCCName := fccid.GetName(); yfCCName == "" {
		err = fmt.Errorf("Spec has an empty chaincodeName")
	} else {
		ret.ChaincodeName, ret.ChaincodeTemplate, ret.TargetLedgers = ParseYFCCName(fccid.GetName())
	}

	return
}

/*
  Also define the handling pipeline interface
*/

type TxPreHandler interface {
	Handle(*TransactionHandlingContext) (*TransactionHandlingContext, error)
}

type FailureHandler struct {
	Err error
}

func (e FailureHandler) Handle(*TransactionHandlingContext) (*TransactionHandlingContext, error) {
	return nil, e.Err
}

//convert a function to a prehandler interface
type FuncAsTxPreHandler func(*TransactionHandlingContext) (*TransactionHandlingContext, error)

func (f FuncAsTxPreHandler) Handle(tx *TransactionHandlingContext) (*TransactionHandlingContext, error) {
	return f(tx)
}

//a function filter the pure tx can also be converted to a prehandler interface
type TxFuncAsTxPreHandler func(*Transaction) (*Transaction, error)

func (f TxFuncAsTxPreHandler) Handle(txe *TransactionHandlingContext) (*TransactionHandlingContext, error) {
	tx, err := f(txe.Transaction)
	if err != nil {
		return nil, err
	}

	txe.Transaction = tx
	return txe, nil
}

var NilValidator = TxFuncAsTxPreHandler(func(tx *Transaction) (*Transaction, error) { return tx, nil })
var PlainTxHandler = FuncAsTxPreHandler(mustParsePlainTx)

//name handler should be the very first one before any working handling (Except simple tagging)
//start, so handling specfied by ccname can work
var YFCCNameHandler = FuncAsTxPreHandler(parseChaincodeName)

//default handler provided a base routine for picking a tx into execution enviroment again
//(e.g. when we need execute a tx read from the txdb)
var DefaultTxHandler = MutipleTxHandler(YFCCNameHandler, PlainTxHandler)

/*
  Mutiple handler
*/

type mutiTxPreHandler []TxPreHandler

type interruptErr struct{}

func (interruptErr) Error() string {
	return "prehandling interrupted"
}

//allowing interrupt among a prehandler array and set the whold result as correct
var ValidateInterrupt = interruptErr{}

func MutipleTxHandler(m ...TxPreHandler) TxPreHandler {
	var flattedM []TxPreHandler
	//"flat" the recursive mutiple txhandler
	for _, mh := range m {
		if mh == nil {
			continue
		}
		if mmh, ok := mh.(mutiTxPreHandler); ok {
			flattedM = append(flattedM, mmh...)
		} else {
			flattedM = append(flattedM, mh)
		}
	}

	switch len(flattedM) {
	case 0:
		return nil
	case 1:
		return flattedM[0]
	default:
		return mutiTxPreHandler(flattedM)
	}
}

func (m mutiTxPreHandler) Handle(tx *TransactionHandlingContext) (*TransactionHandlingContext, error) {
	var err error
	for _, h := range m {
		tx, err = h.Handle(tx)
		if err == ValidateInterrupt {
			return tx, nil
		} else if err != nil {
			return tx, err
		}
	}
	return tx, nil
}
