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

package db

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/abchain/fabric/core/util"
	"github.com/abchain/fabric/protos"
	"github.com/tecbot/gorocksdb"
	"sync"
)

var txDbColumnfamilies = []string{
	TxCF,
	GlobalCF,
	ConsensusCF,
	PersistCF,
}

type txCFs struct {
	txCF        *gorocksdb.ColumnFamilyHandle
	globalCF    *gorocksdb.ColumnFamilyHandle
	consensusCF *gorocksdb.ColumnFamilyHandle
	persistCF   *gorocksdb.ColumnFamilyHandle
}

func (c *txCFs) feed(cfmap map[string]*gorocksdb.ColumnFamilyHandle) {

	c.txCF = cfmap[TxCF]
	c.globalCF = cfmap[GlobalCF]
	c.consensusCF = cfmap[ConsensusCF]
	c.persistCF = cfmap[PersistCF]
}

type pathUnderUpdating struct {
	index         string
	branchedId    uint64
	before, after *pathUpdateTask

	activingPath map[*pathUpdateTask]bool
}

func (ut *pathUnderUpdating) updateGlobalState(gs *protos.GlobalState) (*protos.GlobalState, error) {

	if gs.Count < ut.branchedId {
		return ut.before.updateState(gs)
	} else {
		return ut.after.updateState(gs)
	}
}

func newPathUpdating(txdb *GlobalDataDB, ind string) *pathUnderUpdating {
	ret := &pathUnderUpdating{
		index:        ind,
		activingPath: make(map[*pathUpdateTask]bool),
	}
	return ret
}

type GlobalDataDB struct {
	baseHandler
	txCFs
	globalStateLock sync.Mutex
	updating        map[string]*pathUnderUpdating
	activing        map[string][]*pathUpdateTask

	//caution: destroy option before cf/db is WRONG but just ok only if it was just "default"
	//we must keep object alive if we have custom some options (i.e.: merge operators)
	openDB         sync.Once
	openError      error
	globalStateOpt *gorocksdb.Options
	//not a good implement but we have to compatible with existed keys, so the "virtual" end
	//of key has to be set longer then limit
	globalHashLimit int
}

const (
	update_nextnode   = iota //update node has been depecrated but we keep it for compatible
	update_lastbranch        //update branch has been abandoned (become non-op)
	update_nextbranch
	update_lastnode
	update_path

	prefix_virtualNode = byte('*')
)

type globalstatusMO struct{}

func decodeMergeValue(b []byte) (uint64, []byte, error) {

	u, n := binary.Uvarint(b)
	if n <= 0 {
		return 0, nil, fmt.Errorf("Wrong uvarint data: %d", n)
	}

	return u, b[n:], nil
}

func decodePathInfo(b []byte) ([]byte, []byte, error) {
	var pos int
	if sz := int(b[pos]); sz > len(b) {
		return nil, nil, fmt.Errorf("Wrong size: %X", b)
	} else {
		return b[1:sz], b[sz:], nil
	}
}

func encodeMergeValue(op byte, count uint64, hash []byte) []byte {

	buf := make([]byte, 12)
	return bytes.Join([][]byte{[]byte{op}, buf[:binary.PutUvarint(buf, count)], hash}, nil)
}

func encodePathUpdateInfo(begin, end []byte) []byte {
	return bytes.Join([][]byte{[]byte{byte(len(begin) + 1)}, begin, end}, nil)
}

func encodePathUpdate(count uint64, begin, end []byte) []byte {
	return encodeMergeValue(update_path, count, encodePathUpdateInfo(begin, end))
}

var detachedIdStart = uint64(1<<63 - 1) //it was the max of int64 so we have space for both side (increase and decrease)

func (mo *globalstatusMO) FullMerge(key, existingValue []byte, operands [][]byte) ([]byte, bool) {
	//we are not DARE TO use dblogger in these subroutines so we may just
	//keep all the error silent ...
	//REMEMBER: return false may ruin the record so we must try our best to keep the data robust
	var gs *protos.GlobalState
	var err error
	if existingValue != nil {
		gs, err = protos.UnmarshallGS(existingValue)
		if err != nil {
			return existingValue, true
		}
	} else {
		//this also indicate something wrong for we add an key without valid globalstate ...
		return nil, true
	}

	for _, op := range operands {

		if len(op) < 2 {
			continue //non-op and omit it
		}

		opcode := int(op[0])
		n, h, e := decodeMergeValue(op[1:])
		if e != nil || (n != 0 && n < gs.Count) {
			//skip this op, except 0, which is left for update_node
			//notice it can't not be "<=" because the "equal" case
			//is important
			dbLogger.Warningf("%s have deprecated id %d (current %d)", key, n, gs.Count)
			continue
		}

		switch opcode {
		case update_nextnode:
			gs.NextNodeStateHash = append(gs.NextNodeStateHash, h)
		case update_lastnode:
			gs.ParentNodeStateHash = append(gs.ParentNodeStateHash, h)
		case update_lastbranch, update_nextbranch:
			//simply non-op
		case update_path:
			gs.Count = n
			gs.LastBranchNodeStateHash, gs.NextBranchNodeStateHash, _ = decodePathInfo(h)
		}

	}

	if len(gs.NextNodeStateHash) > 1 {
		gs.NextBranchNodeStateHash = key
	}
	if len(gs.ParentNodeStateHash) > 1 {
		gs.LastBranchNodeStateHash = key
	}

	ret, err := gs.Bytes()
	if err != nil {
		return existingValue, true
	}

	return ret, true
}

func (mo globalstatusMO) PartialMerge(key, leftOperand, rightOperand []byte) ([]byte, bool) {

	//clear the mal-formed operand ...
	if len(leftOperand) < 2 {
		return rightOperand, true
	} else if len(rightOperand) < 2 {
		return leftOperand, true
	}

	//omit all abondanded updatebranch op
	switch int(leftOperand[0]) {
	case update_lastbranch, update_nextbranch:
		return rightOperand, true
	}
	switch int(rightOperand[0]) {
	case update_lastbranch, update_nextbranch:
		return leftOperand, true
	}

	if rightOperand[0] != update_path || leftOperand[0] != update_path {
		return nil, false
	}

	nL, _, _ := decodeMergeValue(leftOperand[1:])
	nR, _, _ := decodeMergeValue(rightOperand[1:])

	//merge updatepath, larger n counts
	if nL > nR {
		return leftOperand, true
	} else if nL < nR {
		return rightOperand, true
	} else {
		return nil, false
	}
}
func (mo *globalstatusMO) Name() string {
	//NOTICE: we give a c-style string so the tailing 0 is IMPORTANT!
	return "globalstateMO\x00"
}

var globalDataDB = new(GlobalDataDB)
var openglobalDBLock sync.Mutex
var stateHashLimit = 64

func (txdb *GlobalDataDB) open(dbpath string) error {

	cfhandlers := txdb.opendb(dbpath, txDbColumnfamilies, txdb.buildOpenDBOptions())

	if len(cfhandlers) != len(txDbColumnfamilies) {
		return errors.New("rocksdb may ruin or not work as expected")
	}

	//feed cfs
	txdb.cfMap = make(map[string]*gorocksdb.ColumnFamilyHandle)
	for i, cfName := range txDbColumnfamilies {
		txdb.cfMap[cfName] = cfhandlers[i]
	}

	txdb.feed(txdb.cfMap)
	txdb.globalHashLimit = stateHashLimit
	txdb.updating = make(map[string]*pathUnderUpdating)
	txdb.activing = make(map[string][]*pathUpdateTask)
	//txdb.pathUpdate = make(map[uint64]*sync.Cond)

	return nil
}

func (txdb *GlobalDataDB) getGlobalStateRaw(statehash []byte) *protos.GlobalState {

	//notice: rocksdb can even accept nil as key
	data, _ := txdb.get(txdb.globalCF, statehash)
	if data != nil {
		gs, err := protos.UnmarshallGS(data)
		if err != nil {
			dbLogger.Errorf("Decode global state of [%x] fail: %s", statehash, err)
			return nil
		}
		return gs
	}

	return nil
}

func (txdb *GlobalDataDB) GetGlobalState(statehash []byte) *protos.GlobalState {

	gs := txdb.getGlobalStateRaw(statehash)
	if gs != nil {
		//cleanup the virtual state
		if txdb.isVirtualState(gs.LastBranchNodeStateHash) {
			gs.LastBranchNodeStateHash = nil
		}

		if txdb.isVirtualState(gs.NextBranchNodeStateHash) {
			gs.NextBranchNodeStateHash = nil
		}
	}

	return gs
}

type StateDuplicatedError struct {
	error
}

func (txdb *GlobalDataDB) isVirtualState(k []byte) bool {
	if len(k) <= txdb.globalHashLimit {
		return false
	}

	return k[0] == prefix_virtualNode
}

func (txdb *GlobalDataDB) innerUpdateState(gs *protos.GlobalState) (*protos.GlobalState, error) {
	var err error
	for ud, ok := txdb.updating[stateToEdge(gs).String()]; ok; ud, ok = txdb.updating[stateToEdge(gs).String()] {
		gs, err = ud.updateGlobalState(gs)
		if err != nil {
			return nil, fmt.Errorf("state was ruined (has no valid update: %s)", err)
		}
	}

	return gs, nil
}

//read state from snapshot, then update it from updating map
//CAUTION: must be executed under the protection of globalStateLock
func (txdb *GlobalDataDB) readState(sn *gorocksdb.Snapshot, key []byte) (*protos.GlobalState, error) {

	if data, err := txdb.getFromSnapshot(sn, txdb.globalCF, key); err != nil {
		return nil, err
	} else if data == nil {
		return nil, nil
	} else {
		if gs, err := protos.UnmarshallGS(data); err != nil {
			return nil, fmt.Errorf("state was ruined (could not decode: %s)", err)
		} else {

			return txdb.innerUpdateState(gs)
		}

	}
}

func (txdb *GlobalDataDB) mustReadState(sn *gorocksdb.Snapshot, key []byte) (gs *protos.GlobalState) {
	var err error
	gs, err = txdb.readState(sn, key)
	if err != nil {
		dbLogger.Errorf("state [%x] not available: %s", key, err)
	}
	return
}

func (txdb *GlobalDataDB) registryTasks(tsk *pathUnderUpdating) error {

	if tsk == nil {
		return fmt.Errorf("empty task")
	}

	//notice: onPath CANNOT be one entry of updating map for we can not obtain reference
	//state within the path of that (it will be updated to one of the paths in that entry)
	if _, ok := txdb.updating[tsk.index]; ok {
		return fmt.Errorf("Duplicated updatine path <%s> when try to registry %v", tsk.index, tsk)
	}

	//clean current activing the task affect ...
	if tactivings, ok := txdb.activing[tsk.index]; ok {
		for _, activingTask := range tactivings {
			dbLogger.Debugf("replace existed active pathupdate task [%s]", tsk.index)
			if activingTask.cancel != nil {
				activingTask.cancel()
			}
			delete(txdb.activing, tsk.index)
			//remove it from all updating path
			for _, udpath := range activingTask.updatingPath {
				delete(udpath.activingPath, activingTask)
			}

			//inherit the affected task's updating path
			for _, newtsk := range []*pathUpdateTask{tsk.before, tsk.after} {
				if newtsk == nil {
					continue
				}
				newtsk.updatingPath = append(newtsk.updatingPath, activingTask.updatingPath...)
				//add newtsk into activing path
				for _, udpath := range newtsk.updatingPath {
					udpath.activingPath[newtsk] = true
				}
			}
			activingTask.updatingPath = nil
		}
	}

	txdb.updating[tsk.index] = tsk
	//finally we add current tasks into activing map
	for _, newtsk := range []*pathUpdateTask{tsk.before, tsk.after} {
		if newtsk == nil {
			continue
		}
		ind := newtsk.index()
		txdb.activing[ind] = append(txdb.activing[ind], newtsk)
	}
	dbLogger.Debugf("registry pathupdate task [%s]/%v", tsk.index, tsk)

	return nil
}

var terminalKeyGen = util.GenerateBytesUUID

func (txdb *GlobalDataDB) genTerminalStateKey() []byte {
	key := terminalKeyGen()
	var padding []byte
	if len(key)+1 <= txdb.globalHashLimit {
		padding = make([]byte, txdb.globalHashLimit-len(key))
	}
	return bytes.Join([][]byte{[]byte{prefix_virtualNode}, key, padding}, nil)
}

func max(a uint64, b uint64) uint64 {
	if a > b {
		return a
	} else {
		return b
	}
}

func (txdb *GlobalDataDB) newPathUpdatedByBranchNode(key []byte, refGS *protos.GlobalState) *pathUnderUpdating {
	path := newPathUpdating(txdb, stateToEdge(refGS).String())
	path.branchedId = refGS.Count

	afterTsk, beforeTsk := &protos.GlobalStateUpdateTask{
		Target:        refGS.NextNode(),
		TargetEdgeBeg: key,
		TargetEdgeEnd: refGS.NextBranchNodeStateHash,
		IsBackward:    false,
		TargetId:      refGS.Count + 2,
	}, &protos.GlobalStateUpdateTask{
		Target:        refGS.ParentNode(),
		TargetEdgeBeg: refGS.LastBranchNodeStateHash,
		TargetEdgeEnd: key,
		IsBackward:    true,
		TargetId:      refGS.Count,
	}
	path.after = &pathUpdateTask{GlobalStateUpdateTask: afterTsk, idoffset: 1}
	path.before = &pathUpdateTask{GlobalStateUpdateTask: beforeTsk, idoffset: 1}

	for _, tsk := range []*pathUpdateTask{path.after, path.before} {
		tsk.updatingPath = append(tsk.updatingPath, path)
		path.activingPath[tsk] = true
	}

	dbLogger.Debugf("build pathupdate task (branch mode) [before %v, after %v]", beforeTsk, afterTsk)

	return path
}

func (txdb *GlobalDataDB) newPathUpdatedByConnected(startGS, refGS *protos.GlobalState, key []byte) *pathUnderUpdating {
	path := newPathUpdating(txdb, stateToEdge(startGS).String())
	tsk := &pathUpdateTask{updatingPath: []*pathUnderUpdating{path}}

	if len(startGS.NextNodeStateHash) == 0 {
		path.branchedId = startGS.Count + 1
		targetId := max(startGS.Count, refGS.Count-1) + 1
		endBranch := refGS.BranchedSelf()
		if len(endBranch) == 0 {
			endBranch = refGS.NextBranchNodeStateHash
		}
		tsk.GlobalStateUpdateTask = &protos.GlobalStateUpdateTask{
			Target:        key,
			TargetEdgeBeg: startGS.LastBranchNodeStateHash,
			TargetEdgeEnd: endBranch,
			IsBackward:    true,
			TargetId:      targetId,
		}
		tsk.idoffset = targetId - startGS.Count
		path.before = tsk
		path.activingPath[tsk] = true

		dbLogger.Debugf("build pathupdate task (connect mode) [before %v]", tsk)

	} else if len(startGS.ParentNodeStateHash) == 0 {
		path.branchedId = startGS.Count
		targetId := max(startGS.Count, refGS.Count+1) + 1
		begBranch := refGS.BranchedSelf()
		if len(begBranch) == 0 {
			begBranch = refGS.LastBranchNodeStateHash
		}
		tsk.GlobalStateUpdateTask = &protos.GlobalStateUpdateTask{
			Target:        key,
			TargetEdgeBeg: begBranch,
			TargetEdgeEnd: startGS.NextBranchNodeStateHash,
			IsBackward:    false,
			TargetId:      targetId,
		}
		tsk.idoffset = targetId - startGS.Count
		path.after = tsk
		path.activingPath[tsk] = true

		dbLogger.Debugf("build pathupdate task (connect mode) [after %v]", tsk)

	} else {
		dbLogger.Errorf("required to build update task for malformed state [%v]", startGS)
		return nil
	}

	return path
}

func (txdb *GlobalDataDB) addGSCritical(parentStateHash []byte, statehash []byte) (sn *gorocksdb.Snapshot, cmt []*pathUpdateTask, retErr error) {

	//critical part must be serialized
	txdb.globalStateLock.Lock()
	defer txdb.globalStateLock.Unlock()

	sn = txdb.NewSnapshot()
	parentgs, newgs := txdb.mustReadState(sn, parentStateHash), txdb.mustReadState(sn, statehash)
	dbLogger.Debugf("add global state core [%x]@[%x]", statehash, parentStateHash)

	connected := parentgs != nil && newgs != nil
	if connected {
		//detect the cyclic case, in this case, parent and newgs will change the same path
		//and lead to a duplicated path error, we must resolve it by change the next/last
		//node into each other, this must ok even one of the node is not really a branched
		//one
		if bytes.Compare(parentgs.NextBranchNodeStateHash, newgs.NextBranchNodeStateHash) == 0 {

			dbLogger.Infof("found cyclic when [%x] is connected to [%x], resolve it", parentStateHash, statehash)
			if len(parentgs.NextNodeStateHash) == 0 && len(newgs.ParentNodeStateHash) == 0 {
				//"pure" cylic, a single path connect its tail from head, we resolve
				//this case by add a dummy node on the tail
				dbLogger.Debugf("resolving pure cyclic")
				newgs.ParentNodeStateHash = append(newgs.ParentNodeStateHash, txdb.genTerminalStateKey())
			}

			if len(parentgs.NextNodeStateHash) > 0 {
				if parentgs.Count > newgs.Count {
					newgs.NextBranchNodeStateHash = parentStateHash
				} else {
					newgs.LastBranchNodeStateHash = parentStateHash
				}
				//we in fact update newgs in advance
				newgs.Count++
				dbLogger.Debugf("update newgs to [%v] using branched parent as base", newgs)
			} else if len(newgs.ParentNodeStateHash) > 0 {
				parentgs.LastBranchNodeStateHash = statehash
				//sanity check, should not happen
				if parentgs.Count <= newgs.Count {
					panic(fmt.Sprintf("unexpected status with parent [%v] and new [%v]", parentgs, newgs))
					//???
					//parentgs.NextBranchNodeStateHash = statehash
				}
				parentgs.Count++
				dbLogger.Debugf("update parent to [%v] using branched ngw as base", parentgs)
			}
			//create new snapshot here so we obtain a state which is the two target
			//node has been updated, which is important in updating a cyclic case
			defer func() {
				if retErr == nil {
					dbLogger.Debug("update snapshot for cyclic case")
					txdb.ReleaseSnapshot(sn)
					sn = txdb.NewSnapshot()
				}
			}()

		}
	}

	//branch node information must be keep here ...
	nbhBackup, lbhBackup := parentgs.GetNextBranchNodeStateHash(), newgs.GetLastBranchNodeStateHash()

	//first update possible change on branch inf., so the following reference will be correct
	if parentgs != nil {
		for _, childHash := range parentgs.NextNodeStateHash {
			if bytes.Compare(statehash, childHash) == 0 {
				//we have a duplicated addition (both current and parent is existed and connected)
				return
			}
		}

		parentgs.NextNodeStateHash = append(parentgs.NextNodeStateHash, statehash)
		if len(parentgs.GetNextNodeStateHash()) == 2 {
			//the "critical" point that is, node will just become branched after
			//this edge is added
			parentgs.NextBranchNodeStateHash = parentStateHash
		}
	}

	if newgs != nil {
		newgs.ParentNodeStateHash = append(newgs.ParentNodeStateHash, parentStateHash)
		if len(newgs.GetParentNodeStateHash()) == 2 {
			newgs.LastBranchNodeStateHash = statehash
		}
	}

	if parentgs == nil {
		dbLogger.Infof("Will create detach state [%x]", parentStateHash)
		parentgs = protos.NewGlobalState()
		parentgs.NextNodeStateHash = [][]byte{statehash}
		if newgs != nil {
			parentgs.Count = newgs.Count - 1
			if newgs.Branched() {
				parentgs.NextBranchNodeStateHash = statehash
				parentgs.LastBranchNodeStateHash = txdb.genTerminalStateKey()
			} else {
				parentgs.NextBranchNodeStateHash = newgs.NextBranchNodeStateHash
				parentgs.LastBranchNodeStateHash = newgs.LastBranchNodeStateHash
			}
		} else {
			parentgs.Count = detachedIdStart
			parentgs.NextBranchNodeStateHash = txdb.genTerminalStateKey()
			parentgs.LastBranchNodeStateHash = txdb.genTerminalStateKey()
		}
	}

	if newgs == nil {
		dbLogger.Debugf("Will create new state [%x]", statehash)
		newgs = protos.NewGlobalState()
		newgs.ParentNodeStateHash = [][]byte{parentStateHash}
		newgs.Count = parentgs.Count + 1
		if parentgs.Branched() {
			newgs.LastBranchNodeStateHash = parentStateHash
			newgs.NextBranchNodeStateHash = txdb.genTerminalStateKey()
		} else {
			newgs.NextBranchNodeStateHash = parentgs.NextBranchNodeStateHash
			newgs.LastBranchNodeStateHash = parentgs.LastBranchNodeStateHash
		}
	}

	//start writing phase ...
	wb := txdb.NewWriteBatch()
	defer wb.Destroy()

	//update task for unbranched state
	//in handling task, we must "restore" the corresponding state into
	//its original status (without connected with another)
	parentgs.NextNodeStateHash = parentgs.NextNodeStateHash[:len(parentgs.NextNodeStateHash)-1]
	if !parentgs.Branched() {
		if len(parentgs.NextNodeStateHash) == 1 {
			//notice, also restore the nextbranch ...
			parentgs.NextBranchNodeStateHash = nbhBackup

			//we must trigger path updating task for critical point
			upath := txdb.newPathUpdatedByBranchNode(parentStateHash, parentgs)
			if err := txdb.registryTasks(upath); err != nil {
				retErr = err
				return
			}
			cmt = append(cmt, upath.before, upath.after)
			//also update branch information, we have no merge op for this state any more
			parentgs.NextBranchNodeStateHash = parentStateHash
			parentgs.Count++
		} else if connected {
			//that is, nextnode is 0, we have a "connected" case
			upath := txdb.newPathUpdatedByConnected(parentgs, newgs, parentStateHash)
			if err := txdb.registryTasks(upath); err != nil {
				retErr = err
				return
			}
			cmt = append(cmt, upath.before)
		}
	}
	parentgs.NextNodeStateHash = append(parentgs.NextNodeStateHash, statehash)

	newgs.ParentNodeStateHash = newgs.ParentNodeStateHash[:len(newgs.ParentNodeStateHash)-1]
	if !newgs.Branched() {
		if len(newgs.ParentNodeStateHash) == 1 {
			newgs.LastBranchNodeStateHash = lbhBackup
			upath := txdb.newPathUpdatedByBranchNode(statehash, newgs)
			if err := txdb.registryTasks(upath); err != nil {
				retErr = err
				return
			}
			cmt = append(cmt, upath.before, upath.after)
			newgs.LastBranchNodeStateHash = statehash
			newgs.Count++
		} else if connected {
			upath := txdb.newPathUpdatedByConnected(newgs, parentgs, statehash)
			if err := txdb.registryTasks(upath); err != nil {
				retErr = err
				return
			}
			cmt = append(cmt, upath.after)
		}
	}
	newgs.ParentNodeStateHash = append(newgs.ParentNodeStateHash, parentStateHash)

	//because mustread always obtain the up-to-date state, write-after-read is possible
	if vbytes, err := parentgs.Bytes(); err != nil {
		retErr = fmt.Errorf("could not encode state [%x]: %s", parentStateHash, err)
	} else {
		wb.PutCF(txdb.globalCF, parentStateHash, vbytes)
	}
	if vbytes, err := newgs.Bytes(); err != nil {
		retErr = fmt.Errorf("could not encode state [%x]: %s", statehash, err)
	} else {
		wb.PutCF(txdb.globalCF, statehash, vbytes)
	}

	retErr = txdb.BatchCommit(wb)
	return

}

func (txdb *GlobalDataDB) AddGlobalState(parentStateHash []byte, statehash []byte) error {

	if bytes.Compare(parentStateHash, statehash) == 0 {
		//never concat same state
		dbLogger.Warningf("self link at [%X] is omitted", statehash)
		return nil
	} else if len(statehash) > txdb.globalHashLimit {
		return fmt.Errorf("add a state whose hash (%x) is longer than limited (%d)", statehash, txdb.globalHashLimit)
	} else if len(parentStateHash) > txdb.globalHashLimit {
		return fmt.Errorf("add a parent state whose hash (%x) is longer than limited (%d)", parentStateHash, txdb.globalHashLimit)
	}

	sn, tsks, err := txdb.addGSCritical(parentStateHash, statehash)
	if sn != nil {
		defer txdb.ReleaseSnapshot(sn)
	}

	if err != nil || len(tsks) == 0 {
		return err
	}

	wb := txdb.NewWriteBatch()
	defer wb.Destroy()

	//execute all tasks ...
	for _, task := range tsks {
		dbLogger.Debugf("Spawn path updating <%d> [%v]", task.index, task)
		defer task.finalize(txdb)
		for task.updatePath(sn, wb, txdb) == shouldCommit {
			if err = txdb.BatchCommit(wb); err != nil {
				dbLogger.Errorf("Commiting updatepath task fail: %s", err)
				return err
			}
			//notice writebatch must be clear after commit!
			wb.Clear()
		}
	}
	//final commiting
	if err = txdb.BatchCommit(wb); err != nil {
		dbLogger.Errorf("Commiting final add global state fail: %s", err)
		return err
	}

	return nil
}

func (txdb *GlobalDataDB) PutTransactions(txs []*protos.Transaction) error {

	//according to rocksdb's wiki, use writebach can do faster writting
	opt := gorocksdb.NewDefaultWriteOptions()
	defer opt.Destroy()

	wb := gorocksdb.NewWriteBatch()
	defer wb.Destroy()

	for _, tx := range txs {
		data, _ := tx.Bytes()
		dbLogger.Debugf("[%s] Write transaction <%s>", printGID, tx.Txid)
		wb.PutCF(txdb.txCF, []byte(tx.Txid), data)
	}

	return txdb.DB.Write(opt, wb)
}

func (txdb *GlobalDataDB) GetTransaction(txid string) (*protos.Transaction, error) {

	txInByte, err := txdb.get(txdb.txCF, []byte(txid))

	if err != nil {
		return nil, err
	}

	if txInByte == nil {
		dbLogger.Debugf("[%s] Transaction %s not found", printGID, txid)
		return nil, nil
	} else {
		tx, err := protos.UnmarshallTransaction(txInByte)
		if err != nil {
			//treat as not found
			dbLogger.Errorf("[%s] Transaction data error: %s", printGID, err)
			return nil, nil
		}
		return tx, nil
	}

}

func (txdb *GlobalDataDB) GetTransactions(txids []string) []*protos.Transaction {

	if txids == nil {
		return nil
	}

	length := len(txids)
	txs := make([]*protos.Transaction, 0, length)

	for _, id := range txids {

		var tx *protos.Transaction
		tx, err := txdb.GetTransaction(id)

		//not try more, just quit
		if err != nil {
			return nil
		} else if tx != nil {
			txs = append(txs, tx)
		}

	}

	return txs
}

func (openchainDB *GlobalDataDB) ListCheckpointsByTag(tag string) (ret [][]byte) {

	ret = make([][]byte, 0)

	it := openchainDB.GetIterator(PersistCF)
	defer it.Close()

	var prefix []byte
	if tag == "" {
		prefix = []byte(checkpointNamePrefix)
	} else {
		prefix = []byte(tag + "." + checkpointNamePrefix)
	}

	for it.Seek([]byte(prefix)); it.ValidForPrefix(prefix); it.Next() {
		//Value/Key() in iterator need not to be Free() but its Data()
		//must be copied
		//ret = append(ret, it.Value().Data()) --- THIS IS WRONG
		ret = append(ret, makeCopy(it.Value().Data()))
	}

	return
}

//list the default db's checkpoints
func (openchainDB *GlobalDataDB) ListCheckpoints() (ret [][]byte) {
	return openchainDB.ListCheckpointsByTag("")
}

func (openchainDB *GlobalDataDB) GetDBVersion() int {
	v, _ := openchainDB.GetValue(PersistCF, []byte(currentGlobalVersionKey))
	if len(v) == 0 {
		dbLogger.Errorf("TxDB version is missed in this db, something wrong")
		return 0
	}

	return int(v[0])
}

func (openchainDB *GlobalDataDB) setDBVersion() error {
	v, _ := openchainDB.GetValue(PersistCF, []byte(currentGlobalVersionKey))
	if len(v) == 0 {
		v = []byte{byte(txDBVersion)}
		if err := openchainDB.PutValue(PersistCF, []byte(currentGlobalVersionKey), v); err != nil {
			return err
		}
	}

	//TODO: in some case we can force the db version?
	dbLogger.Infof("TxDB Version is %d", int(v[0]))
	return nil
}

func (openchainDB *GlobalDataDB) GetIterator(cfName string) *gorocksdb.Iterator {

	cf := openchainDB.cfMap[cfName]

	if cf == nil {
		panic(fmt.Sprintf("Wrong CF Name %s", cfName))
	}

	opt := gorocksdb.NewDefaultReadOptions()
	opt.SetFillCache(true)
	defer opt.Destroy()

	return openchainDB.NewIteratorCF(opt, cf)
}

func (txdb *GlobalDataDB) PutGenesisGlobalState(statehash []byte) error {

	newgs := protos.NewGlobalState()
	newgs.LastBranchNodeStateHash, newgs.NextBranchNodeStateHash = txdb.genTerminalStateKey(), txdb.genTerminalStateKey()
	//notice: we set the Lcount is 0 so it cannot be change

	v, err := newgs.Bytes()
	if err == nil {
		err = txdb.PutValue(GlobalCF, statehash, v)
	}
	return err
}
