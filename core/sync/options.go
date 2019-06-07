package sync

import (
	"github.com/abchain/fabric/core/config"
	"github.com/spf13/viper"
)

type syncOpt struct {
	SyncMsgPrefilter

	txOption struct {
		maxSyncTxCount int
		maxSession     int
	}

	baseSessionWindow   int
	basePendingReqLimit int
	baseIdleTimeout     int
}

func DefaultSyncOption() *syncOpt {

	const defaultMaxSyncTxCnt = 100
	const defaultMaxSessionCnt = 10
	const defaultSessionWindow = 3
	const defaultPendingRequests = 3
	const defaultIdleTime = 30

	ret := new(syncOpt)

	ret.SyncMsgPrefilter = dummyPreFilter(true)
	ret.txOption.maxSyncTxCount = defaultMaxSyncTxCnt
	ret.txOption.maxSession = defaultMaxSessionCnt
	ret.baseSessionWindow = defaultSessionWindow
	ret.baseIdleTimeout = defaultIdleTime
	ret.basePendingReqLimit = defaultPendingRequests

	return ret
}

func (s *syncOpt) configure(vp *viper.Viper) {

	if k := "txconcurrent"; vp.IsSet(k) {
		s.txOption.maxSyncTxCount = vp.GetInt(k)
		logger.Debugf("set max tx concurrent to %d", s.txOption.maxSyncTxCount)
	}

	if k := "maxconcurrent"; vp.IsSet(k) {
		s.txOption.maxSession = vp.GetInt(k)
		logger.Debugf("set max session concurrent to %d", s.txOption.maxSession)
	}

	if k := "transportwin"; vp.IsSet(k) {
		s.baseSessionWindow = vp.GetInt(k)
		logger.Debugf("set transporting window to %d", s.baseSessionWindow)
	}

	if k := "idletimeout"; vp.IsSet(k) {
		s.baseIdleTimeout = vp.GetInt(k)
		logger.Debugf("set idle timeout to %d(s)", s.baseIdleTimeout)
	}

	if k := "maxpending"; vp.IsSet(k) {
		s.basePendingReqLimit = vp.GetInt(k)
		logger.Debugf("set max pending request to %d", s.basePendingReqLimit)
	}

}

func (s *syncOpt) Init(tag string) {

	syncPublic := config.SubViper("sync")

	s.configure(syncPublic)

	if tag != "" && syncPublic.IsSet(tag) {
		logger.Infof("Init sync configurations for scheme %s", tag)
		s.configure(config.SubViper(tag))
	}

}

type clientOpts struct {
	ConcurrentLimit int
	//if we can not finish task when walk-through all streams,
	//how many times we should retry, -1 indicate keep retring
	//and 0 indicate never retry
	RetryCount int
	//wait seconds before retring walking-through streams,
	//notice we force a 10s interval after 3 times of traversal
	RetryInterval int
	//retry the sessions even we have failed before
	RetryFail bool
}

func DefaultClientOption() *clientOpts {

	const defaultConcurrentLimit = 1

	ret := new(clientOpts)
	ret.ConcurrentLimit = defaultConcurrentLimit

	return ret
}
