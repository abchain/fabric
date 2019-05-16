package sync

import (
	"fmt"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/ledger/testutil"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
	"testing"
	"time"
)

type testCliFactory struct {
	opt              *clientOpts
	calledCounter    int
	assignedCounter  int
	prefilter        func(int) bool
	workassign       func(int) bool
	doneLat, failLat time.Duration
}

func (cf *testCliFactory) reset() {
	cf.calledCounter = 0
	cf.assignedCounter = 0
}

func (cf *testCliFactory) Tag() string { return "testClis" }
func (cf *testCliFactory) Opts() *clientOpts {
	return cf.opt
}

func (cf *testCliFactory) PreFilter(rledger *pb.LedgerState) bool {

	ret := cf.prefilter(cf.calledCounter)
	cf.calledCounter++
	return ret
}

func (cf *testCliFactory) AssignHandling() func(*pb.StreamHandler, *syncCore) error {

	ret := cf.workassign(cf.assignedCounter)
	cf.assignedCounter++

	if ret {
		return func(*pb.StreamHandler, *syncCore) error {
			time.Sleep(cf.doneLat)
			return nil
		}
	} else {
		return func(*pb.StreamHandler, *syncCore) error {
			time.Sleep(cf.failLat)
			return fmt.Errorf("just fail")
		}
	}

	return nil
}

func TestSyncCli_Schedule(t *testing.T) {

	const dummyClis = 5

	//just made us to see all resouces has been released
	defer time.Sleep(200 * time.Millisecond)

	baseCtx, endAll := context.WithCancel(context.Background())
	defer endAll()

	testLedger := ledger.InitTestLedger(t)
	testBase := &testFactory{t, baseCtx, testLedger, DefaultSyncOption()}

	sstub := testBase.preparePeer("test")
	for i := 0; i < dummyClis; i++ {
		err := sstub.AddDummyPeer(testutil.GenerateID(t), true)
		testutil.AssertNoError(t, err, "add dummy peer")
	}

	testutil.AssertEquals(t, sstub.HandlerCount(), dummyClis)

	testCF := &testCliFactory{
		doneLat: 200 * time.Millisecond,
		failLat: 50 * time.Millisecond,
	}
	//first test, fail first some peer, all worker work, no retry

	opt := DefaultClientOption(baseCtx)
	opt.ConcurrentLimit = 2
	testCF.opt = opt
	testCF.prefilter = func(counter int) bool {
		if counter >= dummyClis {
			t.Fatalf("Called more than current clients (unexpected retry?)")
		}
		return counter >= dummyClis-opt.ConcurrentLimit
	}
	testCF.workassign = func(counter int) bool {
		if counter >= opt.ConcurrentLimit {
			t.Fatalf("Called more than concurrent limit")
		}
		return true
	}

	err1 := ExecuteSyncTask(testCF, sstub.StreamStub)

	testutil.AssertNoError(t, err1, "schedule test 1")

	testCF.reset()
	//only one peer is allowed, need retry but we not allow (so failed)
	testCF.prefilter = func(counter int) bool {
		if counter >= dummyClis {
			t.Fatalf("Called more than current clients (unexpected retry?)")
		}
		return counter%dummyClis == 1
	}
	testCF.workassign = func(counter int) bool {
		//notice: we are, infact, called <error + concurrent limit> times
		if counter >= 2+opt.ConcurrentLimit {
			t.Fatalf("Called more than expected")
		}
		return counter >= 1
	}

	err2 := ExecuteSyncTask(testCF, sstub.StreamStub)

	testutil.AssertError(t, err2, "schedule test 2")

	testCF.reset()
	//we success when allowing retry
	opt.RetryCount = 2
	testCF.prefilter = func(counter int) bool {
		return counter%dummyClis == 1
	}
	err3 := ExecuteSyncTask(testCF, sstub.StreamStub)

	testutil.AssertNoError(t, err3, "schedule test 3")

	testCF.reset()

	//we allow two peers, but will failed one of it
	testCF.prefilter = func(counter int) bool {
		return counter%dummyClis <= 1
	}
	testCF.workassign = func(counter int) bool {
		if counter >= 2+opt.ConcurrentLimit {
			t.Fatalf("Called more than expected")
		}
		return counter != 1
	}

	err4 := ExecuteSyncTask(testCF, sstub.StreamStub)

	testutil.AssertNoError(t, err4, "schedule test 4")

	testCF.reset()
	opt.ConcurrentLimit = 3
	//we allow all, but will fail 3 times
	testCF.prefilter = func(counter int) bool {
		return true
	}
	testCF.workassign = func(counter int) bool {
		switch counter {
		case 1, 3, 4:
			return false
		default:
			return true
		}
	}
	err5 := ExecuteSyncTask(testCF, sstub.StreamStub)

	testutil.AssertNoError(t, err5, "schedule test 5")

}
