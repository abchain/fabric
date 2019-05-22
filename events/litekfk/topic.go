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

func (r *readerPos) toCache() *readerPosCache {
	return &readerPosCache{r.batch(), r.logpos}
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
	passed      *readerPos //no reader is behind this position
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

	ret.passed = &readerPos{Element: pos}
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

	if t.data.Back().Value.(*batch).series <= curr {
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

func (t *topicUnit) _position(b *batch, offset int) uint64 {
	return b.series*uint64(t.conf.maxbatch) + uint64(offset)
}

func (t *topicUnit) setDryrun(d bool) {
	t.Lock()
	defer t.Unlock()

	t.dryRun = d
}

func (t *topicUnit) setPassed(n *readerPos) {
	t.Lock()
	defer t.Unlock()

	t.passed = n
}

//only used by Write
func (t *topicUnit) addBatch() (ret *batch) {

	ret = &batch{
		series:  t.data.Back().Value.(*batch).series + 1,
		logs:    make([]interface{}, t.conf.batchsize),
		readers: make(map[client]bool),
	}

	t.data.PushBack(ret)

	//sequence of following steps is subtle!

	//1. in dryrun (no clients are active) mode, we must manage the passed position
	if t.dryRun {
		for t.passed.batch().series+uint64(t.conf.maxDelay*2) < ret.series {
			t.passed.Element = t.passed.Next()
		}
	}

	//2. adjust start
	for t.start.batch().series+uint64(t.conf.maxDelay) < ret.series {
		t.start.Element = t.start.Element.Next()
	}

	//3. purge
	for pos := t._head(); pos.batch().series+uint64(t.conf.maxkeep) < t.passed.batch().series; pos.Element = t.data.Front() {

		t.data.Remove(t.data.Front())
	}

	//4. force clean
	if t.conf.maxbatch != 0 {
		for ; t.conf.maxbatch < t.data.Len(); t.data.Remove(t.data.Front()) {

			pos := t._head()
			//purge or force unreg
			if pos.batch().series >= t.passed.batch().series {
				t.Unlock()
				//force unreg
				for cli, _ := range pos.batch().readers {
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

	tail := t.getTail()

	return t._position(tail.batch(), tail.logpos)
}

func (t *topicUnit) Write(i interface{}) error {

	t.Lock()
	defer t.Unlock()

	blk := t.data.Back().Value.(*batch)
	blk.logs[blk.wriPos] = i
	blk.wriPos++

	if blk.wriPos == t.conf.batchsize {
		t.addBatch()
		defer t.batchSignal.Broadcast()
	}

	return nil
}

func (t *topicUnit) ReleaseWaiting() {
	t.batchSignal.Broadcast()
}
