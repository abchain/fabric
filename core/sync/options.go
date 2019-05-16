package sync

import (
	"golang.org/x/net/context"
)

type syncOpt struct {
	SyncMsgPrefilter

	txOption struct {
		maxSyncTxCount int
	}

	baseSessionWindow   int
	basePendingReqLimit int
	baseIdleTimeout     int
}

func DefaultSyncOption() *syncOpt {

	const defaultMaxSyncTxCnt = 100
	const defaultSessionWindow = 3
	const defaultPendingRequests = 3
	const defaultIdleTime = 30

	ret := new(syncOpt)

	ret.SyncMsgPrefilter = dummyPreFilter(true)
	ret.txOption.maxSyncTxCount = defaultMaxSyncTxCnt
	ret.baseSessionWindow = defaultSessionWindow
	ret.baseIdleTimeout = defaultIdleTime
	ret.basePendingReqLimit = defaultPendingRequests

	return ret
}

type clientOpts struct {
	//context of this task
	context.Context
	ConcurrentLimit int
	//if we can not finish task when walk-through all streams,
	//how many times we should retry, -1 indicate keep retring
	//and 0 indicate never retry
	RetryCount int
	//wait seconds before retring walking-through streams,
	//notice we force a 10s interval after 3 times of traversal
	RetryInterval int
}

func DefaultClientOption(ctx context.Context) *clientOpts {

	const defaultConcurrentLimit = 1

	ret := new(clientOpts)
	ret.Context = ctx
	ret.ConcurrentLimit = defaultConcurrentLimit

	return ret
}
