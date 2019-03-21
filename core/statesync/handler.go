package statesync

import (
	pb "github.com/abchain/fabric/protos"
	"github.com/looplab/fsm"
)

var syncPhase = []string{"synclocating", "syncdelta", "syncblock", "syncsnapshot", "syncstate"}

var enterGetBlock = "GetBlock"
var enterGetSnapshot = "GetSnapshot"
var enterGetDelta = "GetDelta"
var enterSyncBegin = "SyncBegin"
var enterSyncFinish = "SyncFinish"
var enterGetState = "GetState"
var enterServe = "Serve"

var enterGetDelta2 = "enterGetDelta2"


func newFsmHandler(h ISyncHandler) *fsm.FSM {

	return fsm.NewFSM(
		"idle",
		fsm.Events{
			{Name: pb.SyncMsg_SYNC_STATE_NOTIFY.String(), Src: []string{"idle"}, Dst: "idle"},
			{Name: pb.SyncMsg_SYNC_STATE_OPT.String(),       Src: []string{"idle"}, Dst: "idle"},
			{Name: pb.SyncMsg_SYNC_QUERY_LEDGER.String(),  Src: []string{"idle"}, Dst: "idle"},
			{Name: pb.SyncMsg_SYNC_QUERY_LEDGER_ACK.String(), Src: []string{"idle"}, Dst: "idle"},

			//serving phase
			{Name: pb.SyncMsg_SYNC_SESSION_START.String(),        Src: []string{"idle"},  Dst: "serve"},
			{Name: pb.SyncMsg_SYNC_SESSION_QUERY.String(),        Src: []string{"serve"}, Dst: "serve"},
			//{Name: pb.SyncMsg_SYNC_SESSION_GET_BLOCKS.String(),   Src: []string{"serve"}, Dst: "serve"},
			//{Name: pb.SyncMsg_SYNC_SESSION_GET_SNAPSHOT.String(), Src: []string{"serve"}, Dst: "serve"},
			{Name: pb.SyncMsg_SYNC_SESSION_DELTAS.String(),   Src: []string{"serve"}, Dst: "serve"},
			{Name: pb.SyncMsg_SYNC_SESSION_SYNC_MESSAGE.String(),   Src: []string{"serve"}, Dst: "serve"},
			{Name: pb.SyncMsg_SYNC_SESSION_END.String(),          Src: []string{"serve"}, Dst: "idle"},

			//client phase
			{Name: pb.SyncMsg_SYNC_SESSION_START_ACK.String(), Src: []string{"synchandshake"}, Dst: "synclocating"},
			{Name: pb.SyncMsg_SYNC_SESSION_QUERY_ACK.String(),  Src: []string{"synclocating"},  Dst: "synclocating"},
			//{Name: pb.SyncMsg_SYNC_SESSION_BLOCKS.String(),    Src: []string{"syncblock"},     Dst: "syncblock"},
			//{Name: pb.SyncMsg_SYNC_SESSION_SNAPSHOT.String(),  Src: []string{"syncsnapshot"},  Dst: "syncsnapshot"},
			{Name: pb.SyncMsg_SYNC_SESSION_DELTAS_ACK.String(),    Src: []string{"syncdelta"},     Dst: "syncdelta"},
			{Name: pb.SyncMsg_SYNC_SESSION_SYNC_MESSAGE_ACK.String(),    Src: []string{"synclocating", "syncdelta", "syncstate"},     Dst: "syncstate"},

			// server
			{Name: enterServe,       Src: []string{"idle"}, Dst: "serve"},

			// client
			{Name: enterSyncBegin,   Src: []string{"idle"}, Dst: "synchandshake"},
			{Name: enterGetDelta2,   Src: []string{"idle"}, Dst: "syncdelta"},
			{Name: enterGetBlock,    Src: syncPhase,        Dst: "syncblock"},
			{Name: enterGetSnapshot, Src: syncPhase,        Dst: "syncsnapshot"},
			{Name: enterGetDelta,    Src: syncPhase,        Dst: "syncdelta"},
			{Name: enterGetState,    Src: syncPhase,        Dst: "syncstate"},
			{Name: enterSyncFinish,  Src: syncPhase,        Dst: "idle"},

		},
		fsm.Callbacks{
			// for both server and client
			"leave_idle":                                          func(e *fsm.Event) { h.leaveIdle(e) },
			"enter_idle":                                          func(e *fsm.Event) { h.enterIdle(e) },

			"before_" + pb.SyncMsg_SYNC_QUERY_LEDGER.String():        func(e *fsm.Event) { h.beforeQueryLedger(e) },
			"before_" + pb.SyncMsg_SYNC_QUERY_LEDGER_ACK.String():    func(e *fsm.Event) { h.beforeQueryLedgerResponse(e) },

			// server
			"before_" + pb.SyncMsg_SYNC_SESSION_START.String():        func(e *fsm.Event) { h.beforeSyncStart(e) },
			"before_" + pb.SyncMsg_SYNC_SESSION_QUERY.String():        func(e *fsm.Event) { h.getServer().beforeQuery(e) },
			//"before_" + pb.SyncMsg_SYNC_SESSION_GET_BLOCKS.String():   func(e *fsm.Event) { h.getServer().beforeGetBlocks(e) },
			"before_" + pb.SyncMsg_SYNC_SESSION_DELTAS.String():   func(e *fsm.Event) { h.getServer().beforeGetDeltas(e) },
			"before_" + pb.SyncMsg_SYNC_SESSION_END.String():          func(e *fsm.Event) { h.getServer().beforeSyncEnd(e) },
			"before_" + pb.SyncMsg_SYNC_SESSION_SYNC_MESSAGE.String():    func(e *fsm.Event) { h.getServer().beforeSyncMessage(e) },

			"leave_serve":                                             func(e *fsm.Event) { h.getServer().leaveServe(e) },
			"enter_serve":                                             func(e *fsm.Event) { h.getServer().enterServe(e) },

			// client
			"after_" + pb.SyncMsg_SYNC_SESSION_START_ACK.String(): func(e *fsm.Event) { h.getClient().afterSyncStartResponse(e) },
			"after_" + pb.SyncMsg_SYNC_SESSION_QUERY_ACK.String():  func(e *fsm.Event) { h.getClient().afterQueryResponse(e) },
			//"after_" + pb.SyncMsg_SYNC_SESSION_BLOCKS.String():    func(e *fsm.Event) { h.getClient().afterSyncBlocks(e) },
			//"after_" + pb.SyncMsg_SYNC_SESSION_DELTAS_ACK.String():    func(e *fsm.Event) { h.getClient().afterSyncStateDeltas(e) },
			"after_" + pb.SyncMsg_SYNC_SESSION_SYNC_MESSAGE_ACK.String():    func(e *fsm.Event) { h.getClient().afterSyncMessage(e) },

			"leave_synclocating":                                  func(e *fsm.Event) { h.getClient().leaveSyncLocating(e) },
			"leave_syncblock":                                     func(e *fsm.Event) { h.getClient().leaveSyncBlocks(e) },
			"leave_syncsnapshot":                                  func(e *fsm.Event) { h.getClient().leaveSyncStateSnapshot(e) },
			"leave_syncdelta":                                     func(e *fsm.Event) { h.getClient().leaveSyncStateDeltas(e) },

			"enter_synclocating":                                  func(e *fsm.Event) { h.getClient().enterSyncLocating(e) },
			"enter_syncblock":                                     func(e *fsm.Event) { h.getClient().enterSyncBlocks(e) },
			"enter_syncsnapshot":                                  func(e *fsm.Event) { h.getClient().enterSyncStateSnapshot(e) },
			"enter_syncdelta":                                     func(e *fsm.Event) { h.getClient().enterSyncStateDeltas(e) },
		},
	)

}
