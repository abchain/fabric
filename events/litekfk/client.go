package litekfk

import (
	"fmt"
)

type client int

type reader struct {
	cli       client
	target    *topicUnit
	current   *readerPos
	end       *readerPos
	autoReset bool
}

var ErrDropOut = fmt.Errorf("Client have been dropped out")
var ErrEOF = fmt.Errorf("EOF")

const (
	ReadPos_Empty = iota
	ReadPos_Default
	ReadPos_Latest
)

const (
	ReadPos_Resume = 256 + iota
	ReadPos_ResumeOrDefault
	ReadPos_ResumeOrLatest
)

func (c client) UnReg(t *topicUnit) {
	t.clients.Lock()
	defer t.clients.Unlock()

	pos, ok := t.clients.readers[c]
	if !ok {
		//has been unreg, over
		return
	}

	delete(pos.batch().readers, c)
	delete(t.clients.readers, c)

	if len(t.clients.readers) == 0 {
		t.setDryrun(true)
	} else {
		var oldest *readerPos

		//fix passed position
		for _, p := range t.clients.readers {
			if oldest == nil || oldest.batch().series > p.batch().series {
				oldest = p
			}
		}

		if oldest.batch().series > t.clients.passedSeries {
			t.clients.passedSeries = oldest.batch().series
			purteTo := t.clients.passedSeries
			t.clients.Unlock()
			t.DoPurge(purteTo)
			t.clients.Lock()
		}
	}
}

func (c client) Read(t *topicUnit, beginPos int) (*reader, error) {
	t.clients.Lock()
	defer t.clients.Unlock()

	pos, ok := t.clients.readers[c]
	if !ok {

		switch beginPos & (ReadPos_Resume - 1) {
		case ReadPos_Latest:
			pos = t.getTail()
		case ReadPos_Default:
			pos = t.getStart()
		default:
			return nil, ErrDropOut
		}

		if len(t.clients.readers) == 0 {

			t.clients.passedSeries = pos.batch().series
			t.setDryrun(false)

		} else if pos.batch().series < t.clients.passedSeries {
			t.clients.passedSeries = pos.batch().series
		}

		t.clients.readers[c] = pos

		pos.Value.(*batch).readers[c] = true
	} else {
		if (beginPos & ReadPos_Resume) == 0 {
			return nil, fmt.Errorf("Read options not allow resume")
		}
	}

	return &reader{
		current:   pos.clone(),
		end:       t.getTail(),
		cli:       c,
		target:    t,
		autoReset: true,
	}, nil
}

func (r *reader) endBatch() bool {
	return r.current.batch().series == r.end.batch().series
}

func (r *reader) _eof() bool {
	return r.current.batch().series == r.end.batch().series &&
		r.current.logpos == r.end.logpos
}

func (r *reader) eof() bool {

	if !r._eof() {
		return false
	}

	if r.autoReset {
		r.end = r.target.getTail()
		if !r._eof() {
			return false
		}
	}

	return true
}

func (r *reader) commit() error {
	r.target.clients.Lock()
	defer r.target.clients.Unlock()

	pos, ok := r.target.clients.readers[r.cli]
	if !ok {
		return ErrDropOut
	}

	if commitTo := r.current.batch().series; pos.batch().series < commitTo {
		r.current.batch().readers[r.cli] = true
		delete(pos.batch().readers, r.cli)
		//update passed status
		for ; pos.batch().series < commitTo; pos = pos.next() {
			cur := pos.batch()
			if r.target.clients.passedSeries == cur.series && len(cur.readers) == 0 {
				r.target.clients.passedSeries++
			} else {
				break
			}
		}

		r.target.clients.readers[r.cli] = r.current.clone()
		purteTo := r.target.clients.passedSeries
		r.target.clients.Unlock()
		r.target.DoPurge(purteTo)
		r.target.clients.Lock()
	} else {
		//simple update logpos
		pos.logpos = r.current.logpos
	}

	return nil
}

func (r *reader) readOne() (interface{}, error) {

	if r.eof() {
		return nil, ErrEOF
	}

	v := r.current.batch().logs[r.current.logpos]
	r.current.logpos++

	if r.current.logpos == r.target.conf.batchsize {
		r.current = r.current.next()
	}

	return v, nil
}

func (r *reader) readBatch() (ret []interface{}, e error) {

	if r.eof() {
		e = ErrEOF
		return
	}

	if r.endBatch() {
		ret = r.current.batch().logs[r.current.logpos:r.end.logpos]
		r.current.logpos = r.end.logpos
	} else {
		ret = r.current.batch().logs[r.current.logpos:]
		r.current = r.current.next()
	}

	return
}

func (r *reader) CurrentEnd() *readerPos {
	return r.end
}

func (r *reader) AutoReset(br bool) {
	r.autoReset = br
}

func (r *reader) Position() uint64 {
	return r.target._position(r.current)
}

func (r *reader) Reset() {
	r.end = r.target.getTail()
}

func (r *reader) ReadOne() (interface{}, error) {

	v, err := r.readOne()
	if err == nil {
		return v, r.commit()
	} else {
		return v, err
	}

}

func (r *reader) ReadBatch() ([]interface{}, error) {

	v, err := r.readBatch()
	if err == nil {
		return v, r.commit()
	} else {
		return v, err
	}
}

func (r *reader) TransactionReadOne() interface{} {

	v, err := r.readOne()
	if err == nil {
		return v
	} else {
		return nil
	}

}

func (r *reader) TransactionReadBatch() []interface{} {

	v, err := r.readBatch()
	if err == nil {
		return v
	} else {
		return nil
	}
}

func (r *reader) Commit() error { return r.commit() }

func (r *reader) Rollback() error {
	r.target.clients.Lock()
	defer r.target.clients.Unlock()

	pos, ok := r.target.clients.readers[r.cli]
	if !ok {
		return ErrDropOut
	}

	r.current = pos.clone()
	return nil
}

//try to read full batch (at least one more item has been written after current batch)
//this method will locked and the only way to quit is calling the ReleaseWaiting in topic
func (r *reader) ReadFullBatch() ([]interface{}, error) {

	r.end = r.target.getTailAndWait(r.current.batch().series)
	return r.ReadBatch()
}
