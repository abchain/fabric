package sync

import (
	"github.com/abchain/fabric/core/ledger"
	pb "github.com/abchain/fabric/protos"
	"github.com/op/go-logging"
	"golang.org/x/net/context"
)

var logger = logging.MustGetLogger("sync")
var clilogger = logging.MustGetLogger("sync/cli")

type SyncStub struct {
	ctx            context.Context
	localLedger    *ledger.Ledger
	srvOptTemplate syncOpt
}

func NewSyncStub(ctx context.Context, l *ledger.Ledger) *SyncStub {

	ret := &SyncStub{
		ctx:            ctx,
		localLedger:    l,
		srvOptTemplate: *DefaultSyncOption(),
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
