package sync

import (
	"fmt"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
	"time"
)

type ISyncHandler interface {
	onPush(*pb.SimpleResp)
	onRequest(*pb.SimpleReq) (*pb.SimpleResp, error)
	//handler should fill acceptsession with corresponding data
	onSessionOpen(*pb.OpenSession, *pb.AcceptSession) (ISessionHandler, error)
}

type ISessionHandler interface {
	//an non-zero duration indicate service must be closed after idle
	idleTimeout() time.Duration
	//if error is returned, sessionfailure will be sent automatically
	onSessionRequest(*pb.TransferRequest) error
	onClose()
}

type coreHandlerAdapter struct {
	*syncCore
	srv                <-chan *pb.SyncMsg
	pushOut            <-chan *pb.SyncMsg_Response
	sessionErrExternal chan error
}

func newHandlerAdapter() *coreHandlerAdapter {

	//just a suitable buffer
	srvchn := make(chan *pb.SyncMsg, 8)
	pushOut := make(chan *pb.SyncMsg_Response, 4)

	ret := &coreHandlerAdapter{
		syncCore:           newFsmHandler(),
		srv:                srvchn,
		pushOut:            pushOut,
		sessionErrExternal: make(chan error, 4),
	}

	ret.Init(pushOut, srvchn)
	return ret
}

func (h *coreHandlerAdapter) SessionFailure(err error) error {
	//overwrite SessionFailure so we can catch enough information for main handling routine
	h.sessionErrExternal <- err
	return nil
}

func (h *coreHandlerAdapter) handlerRoutine(ctx context.Context, handler ISyncHandler) {

	var sessionH ISessionHandler
	var idleTimerC <-chan time.Time
	var idleTimer *time.Timer
	idleTimeStart := time.Now()
	idleTimeout := time.Duration(0)

	cleanSession := func() {
		if sessionH != nil {
			sessionH.onClose()
			sessionH = nil
		}
		idleTimerC = nil
	}

	defer cleanSession()

	notifySessionFailAndClean := func(err error) {

		ret := h.syncCore.SessionFailure(err.Error())
		if ret != nil {
			logger.Errorf("send external error [%s] fail: %s", err, ret)
		}
		cleanSession()
	}

	innerH := func() {
		select {
		case pushMsg := <-h.pushOut:
			handler.onPush(pushMsg.GetSimple())
		case reqMsg := <-h.srv:
			switch reqMsg.Type {
			case pb.SyncMsg_CLIENT_SIMPLE:
				req := reqMsg.GetRequest().GetSimple()
				if req == nil {
					h.SendMessage(failStr(reqMsg.GetCorrelationId(), "Invalid message: no request"))
				} else {
					if resp, err := handler.onRequest(req); err != nil {
						h.ResponseFailure(reqMsg, err)
					} else {
						h.Response(reqMsg, resp)
					}
				}

			case pb.SyncMsg_CLIENT_SESSION_OPEN:
				//build accept msg for handler
				accept := new(pb.AcceptSession)
				if srv, err := handler.onSessionOpen(reqMsg.GetRequest().GetHandshake(), accept); err != nil {
					h.ResponseFailure(reqMsg, err)
				} else {
					sessionH = srv
					idleTimeout = sessionH.idleTimeout()
					idleTimeStart = time.Now()
					idleTimer = time.NewTimer(sessionH.idleTimeout())
					idleTimerC = idleTimer.C
					h.AcceptSession(reqMsg, accept)
				}

			case pb.SyncMsg_CLIENT_SESSION, pb.SyncMsg_CLIENT_SESSION_ACK:
				idleTimeStart = time.Now()
				//session message may have been in channel before we made external stop and call sessionfailure
				if sessReq := reqMsg.GetRequest().GetSession(); sessReq != nil && sessionH != nil {
					if err := sessionH.onSessionRequest(sessReq); err != nil {
						logger.Errorf("current session is failed on request [%v]: %s", sessReq, err)
						notifySessionFailAndClean(err)
					}
				}

			case pb.SyncMsg_CLIENT_SESSION_CLOSE:
				cleanSession()
			}
		case nt := <-idleTimerC:
			if expt := idleTimeStart.Add(idleTimeout); expt.After(nt) {
				idleTimer.Reset(expt.Sub(nt))
			} else {
				logger.Infof("current session has timeout for idle")
				notifySessionFailAndClean(fmt.Errorf("Idle expired"))
			}
		case extErr := <-h.sessionErrExternal:
			notifySessionFailAndClean(extErr)
		case <-ctx.Done():
		}

	}

	for ctx.Err() == nil {
		innerH()
	}
	logger.Infof("will exit core handler routine: %s", ctx.Err())
}
