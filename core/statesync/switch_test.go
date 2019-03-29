package statesync


import (
	pb "github.com/abchain/fabric/protos"
	"math/rand"
	"testing"
	"time"
	"github.com/abchain/fabric/core/db"
	"github.com/spf13/viper"
	"os"
	"io/ioutil"
	"github.com/abchain/fabric/core/util"
	"encoding/hex"
	"github.com/abchain/fabric/core/ledger"
)

var populatedTxCnt = 32



func b2s(bv []byte) string {
	return string(bv)
	return hex.EncodeToString(bv)
}

func s2b(sv string) []byte {
	return []byte(sv)
	bv, _ := hex.DecodeString(sv)
	return bv
}


func setupTestConfig() {
	tempDir, err := ioutil.TempDir("", "fabric-db-test")
	if err != nil {
		panic(err)
	}
	viper.Set("peer.fileSystemPath", tempDir)
	deleteTestDBPath()
}


func newTestSyncer() (sts *syncer) {
	l, _ := ledger.GetLedger()

	sts = &syncer{positionResp: make(chan *pb.SyncStateResp),
		ledger: l,
	}

	return sts
}

func TestSwitchCheckpoint(t *testing.T) {
	return
	viper.Set("peer.fileSystemPath", "/Users/oak/__var_hy/forked")

	db.Start()
	defer db.Stop()

	syncer := newTestSyncer()

	syncer.switchToBestCheckpoint(12)
}

func TestTraverseGS(t *testing.T) {


	db.Start()
	defer deleteTestDBPath()
	defer db.Stop()

	//                                                                  <n1>
	//                                                                   |
	//                            <n11>--<c11>---<c12>---<n12>--<c13>--<b2>---<n13>---<n14>
	//                             |
	//   <stateroot>----<c21>----<b0>----<c22>---<c23>---<n21>--<c24>--<b1>---<n22>---<n23>
	//                            |                                     |
	//                          <n31>                                 <c31>
	//                           |                                     |
	//                          <n41>                                 <b3>---<c41>---<n42>----<c42>--<b4>--<n43>
	//                                                                 |                              |
	//                                                              <n51>                          <n52>

	checkpointsMap := make(map[string]bool)
	checkpointsMap["c11"] = true
	checkpointsMap["c12"] = true
	checkpointsMap["c13"] = true
	checkpointsMap["c21"] = true
	checkpointsMap["c22"] = true
	checkpointsMap["c23"] = true
	checkpointsMap["c24"] = true
	checkpointsMap["c31"] = true
	checkpointsMap["c41"] = true
	checkpointsMap["c42"] = true

	checkpoint2BranchMap := make(map[string]string)
	checkpoint2BranchMap["c11"] = "b2"
	checkpoint2BranchMap["c12"] = "b2"
	checkpoint2BranchMap["c13"] = "b2"
	checkpoint2BranchMap["c21"] = "b0"
	checkpoint2BranchMap["c22"] = "b1"
	checkpoint2BranchMap["c23"] = "b1"
	checkpoint2BranchMap["c24"] = "b1"
	checkpoint2BranchMap["c31"] = "b3"
	checkpoint2BranchMap["c41"] = "b4"
	checkpoint2BranchMap["c42"] = "b4"

	// The adjacent matrix:
	adjm := map[string][]string{
		"stateroot": []string{"c21"},
		"c21":       []string{"b0"},
		"b0":        []string{"n11", "c22", "n31"},
		"n11":       []string{"c11"},
		"c11":       []string{"c12"},
		"c12":       []string{"n12"},
		"n12":       []string{"c13"},
		"c13":       []string{"b2"},
		"b2":        []string{"n1", "n13"},
		"n1":        nil,
		"n13":       []string{"n14"},
		"n14":       nil,
		"n31":       []string{"n41"},
		"n41":       nil,
		"c22":       []string{"c23"},
		"c23":       []string{"n21"},
		"n21":       []string{"c24"},
		"c24":       []string{"b1"},
		"b1":        []string{"n22", "c31"},
		"n22":       []string{"n23"},
		"n23":       nil,
		"c31":       []string{"b3"},
		"b3":        []string{"c41", "n51"},
		"n51":       nil,
		"c41":       []string{"n42"},
		"n42":       []string{"c42"},
		"c42":       []string{"b4"},
		"b4":        []string{"n43", "n52"},
		"n43":       nil,
		"n52":       nil,
	}

	addTask := func(curtasks [][2]string, state string) [][2]string {
		nodes, ok := adjm[state]
		if !ok {
			t.Fatal("Not valid state", state)
		}

		for _, newnode := range nodes {
			curtasks = append(curtasks, [2]string{state, newnode})
		}

		return curtasks
	}

	globalDataDB := db.GetGlobalDBHandle()

	//random populating ...
	err := globalDataDB.PutGenesisGlobalState(s2b("stateroot"))
	if err != nil {
		t.Fatal("Add state fail", err)
	}

	r := rand.New(rand.NewSource(time.Now().Unix()))
	curTasks := addTask(nil, "stateroot")

	dumpCF(t)
	for len(curTasks) != 0 {

		rpos := r.Intn(len(curTasks))
		tsk := curTasks[rpos]
		newstate := tsk[1]

		t.Log("Add state", tsk)

		err = globalDataDB.AddGlobalState(s2b(tsk[0]), s2b(newstate))
		if err != nil {
			t.Fatal("Add state fail", err)
		}

		dumpCF(t)
		//switch with tail ...
		if rpos != len(curTasks)-1 {
			curTasks[rpos][0] = curTasks[len(curTasks)-1][0]
			curTasks[rpos][1] = curTasks[len(curTasks)-1][1]
		}

		curTasks = addTask(curTasks[:len(curTasks)-1], newstate)
	}

	gs := globalDataDB.GetGlobalState(s2b("b0"))
	assertIntEqual(t, len(gs.NextNodeStateHash), 3)

	gs = globalDataDB.GetGlobalState(s2b("b1"))
	assertIntEqual(t, len(gs.NextNodeStateHash), 2)

	gs = globalDataDB.GetGlobalState(s2b("b2"))
	assertIntEqual(t, len(gs.NextNodeStateHash), 2)

	gs = globalDataDB.GetGlobalState(s2b("b3"))
	assertIntEqual(t, len(gs.NextNodeStateHash), 2)

	gs = globalDataDB.GetGlobalState(s2b("b4"))
	assertIntEqual(t, len(gs.NextNodeStateHash), 2)


	branch2CheckpointsMap := traverseGlobalStateGraph(s2b("stateroot"), checkpointsMap, false)

	sanityCheck(t, s2b("b0"), branch2CheckpointsMap, checkpoint2BranchMap)
	sanityCheck(t, s2b("b1"), branch2CheckpointsMap, checkpoint2BranchMap)
	sanityCheck(t, s2b("b2"), branch2CheckpointsMap, checkpoint2BranchMap)
	sanityCheck(t, s2b("b3"), branch2CheckpointsMap, checkpoint2BranchMap)
	sanityCheck(t, s2b("b4"), branch2CheckpointsMap, checkpoint2BranchMap)
}


func sanityCheck(t *testing.T, branchStateHash []byte, branch2CheckpointsMap map[string][][]byte, checkpoint2BranchMap map[string]string) {

	checkpointStateHashList := branch2CheckpointsMap[string(branchStateHash)]

	assertTrue(t, len(checkpointStateHashList) > 0)

	for _, cp := range checkpointStateHashList {
		t.Log("branch->checkpoint:", b2s(branchStateHash), b2s(cp))
		branchStateHashString, ok := checkpoint2BranchMap[b2s(cp)]
		assertTrue(t, ok)
		assertByteEqual(t, branchStateHash, branchStateHashString)
	}
}



func dumpCF(t *testing.T) {
	globalDataDB := db.GetGlobalDBHandle()

	t.Log("------Current CF-----")
	itr := globalDataDB.GetIterator(db.GlobalCF)
	if itr == nil {
		t.Fatal("No iterator!")
	}
	defer itr.Close()

	itr.SeekToFirst()
	for ; itr.Valid(); itr.Next() {

		k := itr.Key()
		v := itr.Value()
		defer k.Free()
		defer v.Free()

		gs, err := pb.UnmarshallGS(v.Data())

		if err != nil {
			t.Fatal("Wrong data in gs")
		}

		t.Log(util.EncodeStatehash(k.Data()), gs)
	}

	t.Log("---------------------")
}

func assertTrue(t *testing.T, v bool) {
	if v {
		return
	}

	t.Fatalf("Get unexpected FALSE result")
}

func assertIntEqual(t *testing.T, v int, exp int) {
	if v == exp {
		return
	}

	t.Fatalf("Value is not equal: get %d, expected %d", v, exp)
}

func assertByteEqual(t *testing.T, v []byte, expected string) {
	if expected == "" && v == nil {
		return
	} else if string(v) == expected {
		return
	}

	t.Fatalf("Value is not equal: get %s, expected %s", string(v), expected)
}


func deleteTestDBPath() {
	dbPath := viper.GetString("peer.fileSystemPath")
	os.RemoveAll(dbPath)
}