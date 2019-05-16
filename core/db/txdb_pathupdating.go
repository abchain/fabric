package db

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/abchain/fabric/protos"
	"github.com/op/go-logging"
	"github.com/tecbot/gorocksdb"
	"golang.org/x/net/context"
)

var taskLogger = logging.MustGetLogger("db/pathUpdating")

type statesEdge [2][]byte

//short index for logging
func (e statesEdge) index() uint64 {
	var s1, s2 uint32
	if len(e[0]) >= 4 {
		s1 = binary.BigEndian.Uint32(e[0][:4])
	}
	if len(e[1]) >= 4 {
		s2 = binary.BigEndian.Uint32(e[1][:4])
	}

	return uint64(s1)<<32 + uint64(s2)
}

func (e statesEdge) String() string {
	if le1, le2 := len(e[0]), len(e[1]); le1 >= 8 && le2 >= 8 {
		return fmt.Sprintf("%X%X", e[0][:8], e[1][:8])
	} else if le2 >= 8 {
		return fmt.Sprintf("%016X%X", e[0], e[1][:8])
	} else if le1 >= 8 {
		return fmt.Sprintf("%X%016X", e[0][:8], e[1])
	} else {
		return fmt.Sprintf("%016X%016X", e[0], e[1])
	}
}

func stateToEdge(gs *protos.GlobalState) statesEdge {
	return statesEdge([2][]byte{gs.LastBranchNodeStateHash, gs.NextBranchNodeStateHash})
}

type pathUpdateTask struct {
	*protos.GlobalStateUpdateTask

	idoffset     uint64
	cancel       context.CancelFunc
	updatingPath []*pathUnderUpdating //the indexs of paths this task are updating (involving)
}

const pathUpdatingTaskPersist = "pathupdate"

var pathUpdatingTaskPersistor = NewPersistor(PersistKeyRegister(pathUpdatingTaskPersist))

func (t *pathUpdateTask) index() string {
	return statesEdge([2][]byte{t.TargetEdgeBeg, t.TargetEdgeEnd}).String()
}

func (t *pathUpdateTask) persist(wb *gorocksdb.WriteBatch, txdb *GlobalDataDB) error {

	bytes, err := t.Bytes()
	if err != nil {
		return err
	}

	pathUpdatingTaskPersistor.StoreOnBatch(wb, []string{t.index()}, bytes)
	return nil
}

func (t *pathUpdateTask) setOffset(o uint64) { t.idoffset = o }

func (t *pathUpdateTask) finalize(txdb *GlobalDataDB) {

	//we have at least one core binded, or we are "detached" task and
	//no need to do any finalize

	txdb.globalStateLock.Lock()
	defer txdb.globalStateLock.Unlock()

	taskLogger.Debugf("Finalize task [%v] (binding tasks %d)", t, len(t.updatingPath))
	ind := t.index()
	leftTask := []*pathUpdateTask{}
	for _, act := range txdb.activing[ind] {
		if act != t {
			leftTask = append(leftTask, act)
		}
	}

	if len(leftTask) == 0 {
		delete(txdb.activing, ind)
	} else {
		txdb.activing[ind] = leftTask
	}

	for _, path := range t.updatingPath {
		delete(path.activingPath, t)
		if len(path.activingPath) == 0 {
			//al task on this path has done, we can clean this entry
			taskLogger.Infof("Clear updating path [%s]", path.index)
			delete(txdb.updating, path.index)
		}
	}
}

//use the task to update a state, just like put op of rocksdb on it
func (t *pathUpdateTask) updateState(gs *protos.GlobalState) (*protos.GlobalState, error) {

	if t == nil {
		return nil, fmt.Errorf("Invalid update task (nil)")
	}

	gs.LastBranchNodeStateHash = t.TargetEdgeBeg
	gs.NextBranchNodeStateHash = t.TargetEdgeEnd
	gs.Count = gs.Count + uint64(t.idoffset)

	return gs, nil
}

var commitBatch = 32
var shouldCommit = errors.New("SHOULD COMMIT")

func (t *pathUpdateTask) toNext(refGS *protos.GlobalState) ([]byte, uint64) {

	if refGS.Branched() {
		return nil, 0
	}

	if t.IsBackward {
		return refGS.ParentNode(), t.TargetId - 1
	} else {
		return refGS.NextNode(), t.TargetId + 1
	}
}

//we walk from t.start (including itself) along the direcion specified by t.isBAckward,
//and updating the nodes on that path
//the walking process will return shouldCommit after op exceed commitBatch,
//and it can be respawned
func (t *pathUpdateTask) updatePath(sn *gorocksdb.Snapshot, wb *gorocksdb.WriteBatch, txdb *GlobalDataDB) error {

	var gs *protos.GlobalState
	opCnt := 0

	for state, id := t.Target, t.TargetId; len(state) > 0; state, id = t.toNext(gs) {

		if opCnt >= commitBatch {
			return shouldCommit
		}

		t.Target = state
		t.TargetId = id

		gs = txdb.mustReadState(sn, t.Target)
		if gs == nil {
			return fmt.Errorf("Target [%x] on update path not exist", t.Target)
		} else {
			//detect forward/backward branch and made a fast stop here
			if t.IsBackward && gs.ForwardBranched() {
				break
			} else if !t.IsBackward && gs.BackwardBranched() {
				break
			}
		}

		wb.MergeCF(txdb.globalCF, t.Target, encodePathUpdate(t.TargetId, t.TargetEdgeBeg, t.TargetEdgeEnd))
		opCnt++
	}

	return nil
}

func (t *pathUpdateTask) executeUpdatePath(ctx context.Context, sn *gorocksdb.Snapshot, txdb *GlobalDataDB) {

	wb := txdb.NewWriteBatch()
	defer wb.Destroy()

	hasmore := true
	for hasmore {
		if err := t.updatePath(sn, wb, txdb); err == nil {
			//check stalled flag?
			hasmore = false
		} else if err != shouldCommit {
			taskLogger.Errorf("Path updating task <%d> has fail: [%s]", t.index, err)
			return
		}

		select {
		case <-ctx.Done():
			taskLogger.Debugf("Path updating task <%d> has exited for ctx done: [%s]", t.index, ctx.Err())
			return
		default:
		}

		if err := txdb.BatchCommit(wb); err != nil {
			taskLogger.Errorf("commit on state [%d:%x] was ruined: [%s]", t.TargetId, t.Target, err)
			return
		}
	}

	taskLogger.Debugf("Path updating task <%d> has exited", t.index)
	return

}
