package sync

import (
	"github.com/abchain/fabric/core/ledger"
	pb "github.com/abchain/fabric/protos"
	"github.com/op/go-logging"
	"golang.org/x/net/context"
	"time"
)

var logger = logging.MustGetLogger("sync")
var clilogger = logging.MustGetLogger("sync/cli")

//different from gossip, syncstub is just a lite wrapper for ledger and
//option templates, responding to create handler for remote peer and
//act as a partial stack for factory of streamhandler, that is because
//sync never trigger a broadcast to each neighbours like gossip and
//any broadcast action can be treated by a sync-client. The later is
//handle in sync/strategy module

type SyncStub struct {
	ctx            context.Context
	localLedger    *ledger.Ledger
	srvOptTemplate syncOpt
}

func NewSyncStub(ctx context.Context, l *ledger.Ledger) *SyncStub {

	opt := DefaultSyncOption()
	opt.Init(l.Tag())

	ret := &SyncStub{
		ctx:            ctx,
		localLedger:    l,
		srvOptTemplate: *opt,
	}

	return ret
}

//accept incoming option as default opt template, except for the prefilter
func (s *SyncStub) SetServerOption(opt *syncOpt) {
	f := s.srvOptTemplate.SyncMsgPrefilter
	s.srvOptTemplate = *opt
	s.srvOptTemplate.SyncMsgPrefilter = f
}

func (s *SyncStub) SetExternalPrefilter(f SyncMsgPrefilter) {
	s.srvOptTemplate.SyncMsgPrefilter = f
}

//also help imply the main entry of stream factory
func (s *SyncStub) NewStreamHandlerImpl(id *pb.PeerID, _ *pb.StreamStub, _ bool) (pb.StreamHandlerImpl, error) {
	theOpt := s.srvOptTemplate
	return newSyncHandler(s.ctx, id, s.localLedger, &theOpt), nil
}

func (s *SyncStub) StubContext() context.Context {
	return s.ctx
}

func (s *SyncStub) BroadcastLedgerStatus(sstub *pb.StreamStub) {

	logger.Debugf("Start broadcast ledger status to neighbours")
	ls := s.localLedger.GetLedgerStatus()

	//must finish in few times to avoiding accumulating
	wctx, endF := context.WithTimeout(context.TODO(), 3*time.Second)
	defer endF()
	chs := sstub.OverAllHandlers(wctx)
	for strm := range chs {
		castedh, ok := strm.Impl().(*syncHandler)
		if !ok {
			panic("Wrong implement, implement is not syncHandler")
		}
		if err := castedh.PushLocalLedgerState(strm.StreamHandler, ls); err != nil {
			logger.Warningf("Push ledger status to neighbour [%s] fail: %s, give up", strm.GetName(), err)
		}
	}

}
