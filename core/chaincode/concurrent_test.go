package chaincode

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/abchain/fabric/core/config"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/util"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
)

func concurrentInvoke(ctxt context.Context, invtx []*pb.Transaction, querytx []*pb.Transaction) (error, <-chan error, <-chan *ExecuteResult) {

	threads := len(invtx) + len(querytx)

	wg := new(sync.WaitGroup)
	wg.Add(threads)

	lock := new(sync.Mutex)
	cond := sync.NewCond(lock)

	l, err := ledger.GetLedger()
	if err != nil {
		return err, nil, nil
	}

	outrest := make(chan *ExecuteResult)
	outerr := make(chan error)

	excthread := func(tx *pb.Transaction, isquery bool) {

		lock.Lock()
		wg.Done()
		cond.Wait()
		lock.Unlock()

		txe, _ := pb.DefaultTxHandler.Handle(pb.NewTransactionHandlingContext(tx))
		tout := ledger.TxExecStates{}
		if isquery {
			tout.InitForQuerying(l)
		} else {
			tout.InitForInvoking(l)
		}
		res, err := Execute2(ctxt, l, GetChain(DefaultChain), txe, tout)

		lock.Lock()
		wg.Done()
		outerr <- err
		if isquery {
			outrest <- res
		}
		lock.Unlock()
	}

	for _, tx := range invtx {
		go excthread(tx, false)
	}

	for _, tx := range querytx {
		go excthread(tx, true)
	}

	wg.Wait()
	wg.Add(threads)
	go func() {
		wg.Wait()
		lock.Lock()
		close(outrest)
		close(outerr)
		lock.Unlock()
	}()

	cond.Broadcast()

	return nil, outerr, outrest

}

func checkConcurrentQuery(resout <-chan *ExecuteResult) error {

	var example *ExecuteResult

	defer func() {
		ok := true
		for ok {
			_, ok = <-resout
		}
	}()

	for res := range resout {

		if example == nil {
			example = new(ExecuteResult)
			example.Resp = res.Resp
		} else {
			if bytes.Compare(example.Resp, res.Resp) != 0 {
				return fmt.Errorf("Different result: %x vs %x", example.Resp, res.Resp)
			}
		}

	}

	return nil
}

func checkConcurrentResult(errout <-chan error) error {

	defer func() {
		ok := true
		for ok {
			_, ok = <-errout
		}
	}()

	for err := range errout {
		if err != nil {
			return err
		}
	}

	return nil
}

func checkConcurrentQueryTill(resout <-chan *ExecuteResult, till int) error {

	var example *ExecuteResult

	for res := range resout {

		if example == nil {
			example = new(ExecuteResult)
			example.Resp = res.Resp
		} else {
			if bytes.Compare(example.Resp, res.Resp) != 0 {
				return fmt.Errorf("Different result: %x vs %x", example.Resp, res.Resp)
			}
		}

		till = till - 1
		if till == 0 {
			return nil
		}
	}

	return nil
}

func checkConcurrentResultTill(errout <-chan error, till int) error {

	for err := range errout {
		if err != nil {
			return err
		}
		till = till - 1
		if till == 0 {
			return nil
		}
	}

	return nil
}

func concurrentInvokeExample02Transaction(ctxt context.Context, cID *pb.ChaincodeID, invoke int, query int) error {

	makeTx := func(spec *pb.ChaincodeSpec, query bool) (*pb.Transaction, error) {
		chaincodeInvocationSpec := &pb.ChaincodeInvocationSpec{ChaincodeSpec: spec}
		uuid := util.GenerateUUID()

		return createTransaction(!query, chaincodeInvocationSpec, uuid)
	}

	var txs, txqs []*pb.Transaction

	invokeg := invoke * 2 / 3
	invoked := invoke - invokeg

	for i := 0; i < invokeg; i++ {

		args := util.ToChaincodeArgs("invoke", "a", "b", "10")
		spec := &pb.ChaincodeSpec{Type: 1, ChaincodeID: cID, CtorMsg: &pb.ChaincodeInput{Args: args}}

		tx, err := makeTx(spec, false)
		if err != nil {
			return err
		}
		txs = append(txs, tx)
	}

	for i := 0; i < invoked; i++ {

		args := util.ToChaincodeArgs("delete", "a")
		spec := &pb.ChaincodeSpec{Type: 1, ChaincodeID: cID, CtorMsg: &pb.ChaincodeInput{Args: args}}

		tx, err := makeTx(spec, false)
		if err != nil {
			return err
		}
		txs = append(txs, tx)
	}

	for i := 0; i < query; i++ {

		args := util.ToChaincodeArgs("query", "a")
		spec := &pb.ChaincodeSpec{Type: 1, ChaincodeID: cID, CtorMsg: &pb.ChaincodeInput{Args: args}}

		tx, err := makeTx(spec, true)
		if err != nil {
			return err
		}
		txqs = append(txqs, tx)
	}

	err, outerr, outr := concurrentInvoke(ctxt, txs, txqs)

	if err != nil {
		return err
	}

	err1 := make(chan error, 1)
	err2 := make(chan error, 1)

	go func() {
		err1 <- checkConcurrentResult(outerr)
	}()

	go func() {
		err2 <- checkConcurrentQuery(outr)
	}()

	if err = <-err1; err != nil {
		return err
	}

	if err = <-err2; err != nil {
		return err
	}

	return nil
}

func concurrentInvokeLongTransaction(ctxt context.Context, cID *pb.ChaincodeID, invoke int, query int) error {

	makeTx := func(spec *pb.ChaincodeSpec, query bool) (*pb.Transaction, error) {
		chaincodeInvocationSpec := &pb.ChaincodeInvocationSpec{ChaincodeSpec: spec}
		uuid := util.GenerateUUID()

		return createTransaction(!query, chaincodeInvocationSpec, uuid)
	}

	var txs, txqs []*pb.Transaction

	invokeg := invoke - 1

	//delete is long (take 10s)
	args := util.ToChaincodeArgs("delete", "a")
	spec := &pb.ChaincodeSpec{Type: 1, ChaincodeID: cID, CtorMsg: &pb.ChaincodeInput{Args: args}}

	tx, err := makeTx(spec, false)
	if err != nil {
		return err
	}
	txs = append(txs, tx)

	for i := 0; i < invokeg; i++ {

		args := util.ToChaincodeArgs("invoke", "a", "b", "10")
		spec := &pb.ChaincodeSpec{Type: 1, ChaincodeID: cID, CtorMsg: &pb.ChaincodeInput{Args: args}}

		tx, err := makeTx(spec, false)
		if err != nil {
			return err
		}
		txs = append(txs, tx)
	}

	for i := 0; i < query; i++ {

		args := util.ToChaincodeArgs("query", "a")
		spec := &pb.ChaincodeSpec{Type: 1, ChaincodeID: cID, CtorMsg: &pb.ChaincodeInput{Args: args}}

		tx, err := makeTx(spec, true)
		if err != nil {
			return err
		}
		txqs = append(txqs, tx)
	}

	err, outerr, outr := concurrentInvoke(ctxt, txs, txqs)

	if err != nil {
		return err
	}

	err1 := make(chan error, 1)
	err2 := make(chan error, 1)
	err3 := make(chan error, 1)

	go func() {
		err1 <- checkConcurrentResultTill(outerr, invokeg)
		fmt.Println("check 1 over")
		go func() {
			err3 <- checkConcurrentResult(outerr)
		}()
	}()

	go func() {
		err2 <- checkConcurrentQueryTill(outr, query)
		go func() {
			checkConcurrentQuery(outr)
		}()
	}()

	if err = <-err1; err != nil {
		return err
	}

	if err = <-err2; err != nil {
		return err
	}

	endT1 := time.Now().Unix()

	if err = <-err3; err != nil {
		return err
	}

	endT2 := time.Now().Unix()

	if endT2-endT1 < 5 {
		return fmt.Errorf("Ending interval is tool small")
	}

	return nil
}

func TestConcurrentExecuteInvokeTransaction(t *testing.T) {
	ledger.InitTestLedger(t)

	vp := config.SubViper("peer")
	lis, err := initPeer(vp)
	if err != nil {
		t.Fail()
		t.Logf("Error starting peer listener %s", err)
		return
	}

	defer finitPeer(lis)

	var ctxt = context.Background()

	url := "github.com/abchain/fabric/examples/chaincode/go/chaincode_example02"
	cID := &pb.ChaincodeID{Path: url}

	argsDeploy := util.ToChaincodeArgs("init", "a", "100", "b", "200")
	spec := &pb.ChaincodeSpec{Type: 1, ChaincodeID: cID, CtorMsg: &pb.ChaincodeInput{Args: argsDeploy}}
	_, err = deploy(ctxt, spec)
	chaincodeID := spec.ChaincodeID.Name
	if err != nil {
		t.Fatalf("Error deploying <%s>: %s", chaincodeID, err)
	}

	time.Sleep(time.Second)

	err = concurrentInvokeExample02Transaction(ctxt, cID, 1, 1)
	if err != nil {
		t.Fail()
		t.Logf("Error invoking transaction: %s", err)
	} else {
		t.Logf("Invoke test 1 passed")
	}

	err = concurrentInvokeExample02Transaction(ctxt, cID, 32, 16)
	if err != nil {
		t.Fail()
		t.Logf("Error invoking transaction: %s", err)
	} else {
		t.Logf("Invoke test 2.1 passed")
	}

	err = concurrentInvokeExample02Transaction(ctxt, cID, 16, 32)
	if err != nil {
		t.Fail()
		t.Logf("Error invoking transaction: %s", err)
	} else {
		t.Logf("Invoke test 2.2 passed")
	}

	err = concurrentInvokeExample02Transaction(ctxt, cID, 64, 32)
	if err != nil {
		t.Fail()
		t.Logf("Error invoking transaction: %s", err)
	} else {
		t.Logf("Invoke test 3.1 passed")
	}

	err = concurrentInvokeExample02Transaction(ctxt, cID, 32, 64)
	if err != nil {
		t.Fail()
		t.Logf("Error invoking transaction: %s", err)
	} else {
		t.Logf("Invoke test 3.2 passed")
	}

	StopChain(ctxt, &pb.ChaincodeDeploymentSpec{ChaincodeSpec: &pb.ChaincodeSpec{ChaincodeID: cID}})

}

func TestConcurrentExecuteInvokeLongTransaction(t *testing.T) {
	ledger.InitTestLedger(t)

	vp := config.SubViper("peer")
	lis, err := initPeer(vp)
	if err != nil {
		t.Fail()
		t.Logf("Error starting peer listener %s", err)
		return
	}

	defer finitPeer(lis)

	var ctxt = context.Background()

	url := "github.com/abchain/fabric/examples/chaincode/go/concurrent"
	cID := &pb.ChaincodeID{Path: url}

	argsDeploy := util.ToChaincodeArgs("init", "a", "100", "b", "200")
	spec := &pb.ChaincodeSpec{Type: 1, ChaincodeID: cID, CtorMsg: &pb.ChaincodeInput{Args: argsDeploy}}
	_, err = deploy(ctxt, spec)
	chaincodeID := spec.ChaincodeID.Name
	if err != nil {
		t.Fatalf("Error deploying <%s>: %s", chaincodeID, err)
	}

	time.Sleep(time.Second)

	err = concurrentInvokeLongTransaction(ctxt, cID, 2, 1)
	if err != nil {
		t.Fail()
		t.Logf("Error invoking transaction: %s", err)
	} else {
		t.Logf("Invoke test 1 passed")
	}

	err = concurrentInvokeLongTransaction(ctxt, cID, 32, 32)
	if err != nil {
		t.Fail()
		t.Logf("Error invoking transaction: %s", err)
	} else {
		t.Logf("Invoke test 2 passed")
	}

	StopChain(ctxt, &pb.ChaincodeDeploymentSpec{ChaincodeSpec: &pb.ChaincodeSpec{ChaincodeID: cID}})

}
