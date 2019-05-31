package litekfk

import (
	"container/list"
	"sync"
)

type topicConfiguration struct {
	batchsize int
	maxbatch  int //if 0, not limit the max batch
	maxkeep   int //when batch is passed out, how long it can keep before purge
	//must less than maxbatch (if not 0)
	maxDelay int //client can start, must not larger than 1/2 maxkeep
}

type batch struct {
	series  uint64
	wriPos  int
	logs    []interface{}
	readers map[client]bool
}

type readerPos struct {
	*list.Element
	logpos int
}

func (r *readerPos) batch() *batch {
	b, ok := r.Value.(*batch)
	if !ok {
		panic("wrong type, not batch")
	}

	return b
}

func (r *readerPos) next() *readerPos {
	return &readerPos{Element: r.Next()}
}

func (r *readerPos) clone() *readerPos {

	cp := *r
	return &cp
}

type clientReg struct {
	sync.Mutex
	cliCounter   int
	passedSeries uint64
	readers      map[client]*readerPos
}

func (cr *clientReg) AssignClient() client {
	cr.Lock()
	defer cr.Unlock()

	c := client(cr.cliCounter)
	cr.cliCounter++

	return c
}

type topicUnit struct {
	sync.RWMutex
	conf        *topicConfiguration
	data        *list.List
	dryRun      bool
	start       *readerPos //new reader is adviced to start at this position
	clients     clientReg
	batchSignal *sync.Cond
}

func InitTopic(conf *topicConfiguration) *topicUnit {

	ret := &topicUnit{
		conf:   conf,
		dryRun: true,
		data:   list.New(),
	}

	ret.clients.readers = make(map[client]*readerPos)
	pos := ret.data.PushFront(&batch{
		logs:    make([]interface{}, conf.batchsize),
		readers: make(map[client]bool),
	})

	ret.start = &readerPos{Element: pos}
	ret.batchSignal = sync.NewCond(ret)

	return ret
}

func (t *topicUnit) getStart() *readerPos {
	t.RLock()
	defer t.RUnlock()

	return &readerPos{Element: t.start.Element}
}

func (t *topicUnit) getTail() *readerPos {
	t.RLock()
	defer t.RUnlock()

	return &readerPos{
		Element: t.data.Back(),
		logpos:  t.data.Back().Value.(*batch).wriPos,
	}
}

func (t *topicUnit) getTailAndWait(curr uint64) *readerPos {
	t.Lock()
	defer t.Unlock()

	for t.data.Back().Value.(*batch).series <= curr {
		t.batchSignal.Wait()
	}

	return &readerPos{
		Element: t.data.Back(),
		logpos:  t.data.Back().Value.(*batch).wriPos,
	}
}

func (t *topicUnit) _head() *readerPos {

	return &readerPos{Element: t.data.Front()}
}

func (t *topicUnit) _position(pos *readerPos) uint64 {
	return pos.batch().series*uint64(t.conf.maxbatch) + uint64(pos.logpos)
}

func (t *topicUnit) setDryrun(d bool) {
	t.Lock()
	defer t.Unlock()

	t.dryRun = d
}

//only used by Write
func (t *topicUnit) addBatch() (ret *batch) {

	ret = &batch{
		series:  t.data.Back().Value.(*batch).series + 1,
		logs:    make([]interface{}, t.conf.batchsize),
		readers: make(map[client]bool),
	}

	t.data.PushBack(ret)

	//adjust start
	for t.start.batch().series+uint64(t.conf.maxDelay) < ret.series {
		t.start.Element = t.start.Element.Next()
	}

	//1. in dryrun (no clients are active) mode, we must manage the passed position
	if t.dryRun {
		dryRunDelay := uint64(t.conf.maxDelay*2 + t.conf.maxkeep)
		//3. purge
		for pos := t._head(); pos.batch().series+dryRunDelay < ret.series; pos.Element = t.data.Front() {
			t.data.Remove(t.data.Front())
		}
	}

	//4. force clean
	if t.conf.maxbatch != 0 {
		for ; t.conf.maxbatch < t.data.Len(); t.data.Remove(t.data.Front()) {
			//purge or force unreg
			if batch := t._head().batch(); len(batch.readers) > 0 {
				t.Unlock()
				//force unreg
				for cli, _ := range batch.readers {
					cli.UnReg(t)
				}
				t.Lock()
			}
		}
	}

	return
}

func (t *topicUnit) Clients() *clientReg {
	return &t.clients
}

func (t *topicUnit) CurrentPos() uint64 {
	return t._position(t.getTail())
}

func (t *topicUnit) Write(i interface{}) error {

	t.Lock()
	defer t.Unlock()

	blk := t.data.Back().Value.(*batch)
	blk.logs[blk.wriPos] = i
	blk.wriPos++

	if blk.wriPos == t.conf.batchsize {
		t.addBatch()
		t.batchSignal.Broadcast()
	}

	return nil
}

func (t *topicUnit) DoPurge(passed uint64) {

	t.Lock()
	defer t.Unlock()

	for pos := t._head(); pos.batch().series+uint64(t.conf.maxkeep) < passed; pos.Element = t.data.Front() {
		t.data.Remove(t.data.Front())
	}
}

func (t *topicUnit) ReleaseWaiting() {
	t.batchSignal.Broadcast()
}
