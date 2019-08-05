package syncstrategy

import (
	"sync"
	"time"

	stsync "github.com/abchain/fabric/core/sync"
	pb "github.com/abchain/fabric/protos"
)

var defaultInterval = time.Second * 10

type progressIndicator struct {
	sync.Mutex
	progress int64

	counter         int64
	lastActiveTimer time.Time

	title, tag     string
	estimatedMax   int64
	activeInterval time.Duration
}

func newIndicator(title string) *progressIndicator {
	return &progressIndicator{
		title:          title,
		activeInterval: defaultInterval,
	}
}

type blockSyncf func(uint64, *pb.Block) error

func (p *progressIndicator) AdaptBlockSyncing(f blockSyncf) blockSyncf {

	p.tag = "blocks"

	return func(n uint64, blk *pb.Block) error {
		if err := f(n, blk); err == nil {
			p.Count()
			return nil
		} else {
			return err
		}
	}
}

type stateSyncer struct {
	*progressIndicator
	stsync.StateSyncer
}

func (s stateSyncer) ApplySyncData(data *pb.SyncStateChunk) error {

	if err := s.StateSyncer.ApplySyncData(data); err == nil {
		s.Count()
		return nil
	} else {
		return err
	}

}

func (p *progressIndicator) AdaptStateSyncing(syncer stsync.StateSyncer) stateSyncer {

	p.tag = "state"

	return stateSyncer{p, syncer}
}

func (p *progressIndicator) SetLimit(l int64) { p.estimatedMax = l }

func (p *progressIndicator) Reset() {
	if p.estimatedMax > 100 {
		p.counter = p.estimatedMax / 100
	} else {
		// 0 prevent the counter work for active
		p.counter = 0
	}
	p.progress = 0
	p.lastActiveTimer = time.Now().Add(p.activeInterval)
}

func (p *progressIndicator) active(pg int64) {

	if p.estimatedMax > 0 {
		logger.Infof("Syncing <%s>: %d/~%d %s", p.title, pg, p.estimatedMax, p.tag)
	} else {
		logger.Infof("Syncing <%s>: %d/<unknown> %s", p.title, pg, p.tag)
	}

}

func (p *progressIndicator) Count() {
	p.Lock()
	defer p.Unlock()

	p.progress++
	n := time.Now()
	if (p.counter > 0 && p.progress > p.counter) || n.After(p.lastActiveTimer) {
		defer p.active(p.progress)
		p.lastActiveTimer = n.Add(p.activeInterval)
		if p.counter > 0 {
			p.counter += p.estimatedMax / 100
		}
	}
}
