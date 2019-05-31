package litekfk

import (
	"fmt"
	"testing"
	"time"
)

func testConf() *topicConfiguration {

	c := NewDefaultConfig()

	c.batchsize = 3
	c.maxkeep = 3
	c.maxDelay = 1

	return c
}

var defaultTestConfig = testConf()

//all the test only use single writer
func checkbatch(b *batch, expectid int) error {

	if b.series != uint64(expectid) {
		return fmt.Errorf("Id not match: expect %v but get %v", expectid, b.series)
	}

	for i, v := range b.logs {

		if v == nil { //not written yet
			break
		}

		ii, ok := v.(int)
		if !ok {
			return fmt.Errorf("fail type: %v", v)
		}

		expii := expectid*defaultTestConfig.batchsize + i
		if ii != expii {
			return fmt.Errorf("contain not match: expect %v but get %v", expii, ii)
		}
	}

	return nil
}

func dumpbatch(t *testing.T, b *batch) {

	var dumplog []int

	for _, v := range b.logs {
		if v == nil { //not written yet
			break
		}
		ii, ok := v.(int)
		if !ok {
			t.Fatalf("fail type: %v", v)
		}

		dumplog = append(dumplog, ii)
	}

	t.Log("batch: ", b.series, "@", b.wriPos, ":", dumplog, " with", b.readers)
}

func dumpTopic(t *testing.T, title string, u *topicUnit) {

	t.Log(" ------ Dumping topic for phase", title)

	for i := u.data.Front(); i != nil; i = i.Next() {
		dumpbatch(t, i.Value.(*batch))
	}

	t.Log("-------- Pass:", u.clients.passedSeries)

	t.Log("-------- End dumping topic for phase")

}

func checkTopic(t *testing.T, u *topicUnit, tailId int, headId int, startId int) {

	if err := checkbatch(u.getTail().batch(), tailId); err != nil {
		t.Fatal("tail error", err)
	}

	if err := checkbatch(u._head().batch(), headId); err != nil {
		t.Fatal("head error", err)
	}

	if err := checkbatch(u.getStart().batch(), startId); err != nil {
		t.Fatal("start error", err)
	}

}

func TestDryRun(t *testing.T) {

	var counter int

	topic := InitTopic(defaultTestConfig)

	topic.Write(int(counter))
	counter++

	dumpTopic(t, "dryrun phase 1", topic)
	checkTopic(t, topic, 0, 0, 0)

	for ; counter < 10; counter++ {
		topic.Write(int(counter))
	}

	dumpTopic(t, "dryrun phase 2", topic)
	checkTopic(t, topic, 3, 0, 2)

	for ; counter < 20; counter++ {
		topic.Write(int(counter))
	}

	dumpTopic(t, "dryrun phase 3", topic)
	checkTopic(t, topic, 6, 1, 5)

	for ; counter < 39; counter++ {
		topic.Write(int(counter))
	}

	dumpTopic(t, "dryrun phase 4", topic)
	checkTopic(t, topic, 13, 8, 12)
}

type readTester int

func (r readTester) testV(t *testing.T, v interface{}) readTester {
	iv, ok := v.(int)
	if !ok {
		panic(fmt.Sprintf("read type fail: %v", v))
	}

	if int(r) != iv {
		t.Fatalf("read data error in %d, get %d", r, iv)
	}

	return readTester(r + 1)
}

func TestOneReader(t *testing.T) {

	var counter int
	var tester readTester
	var err error

	topic := InitTopic(defaultTestConfig)

	cli1 := topic.Clients().AssignClient()

	var rd1, rd2, rd3 *reader
	rd1, err = cli1.Read(topic, ReadPos_Default)

	if err != nil {
		t.Fatal("cli1 reader 1 fail", err)
	}

	for ; counter < 4; counter++ {
		topic.Write(int(counter))
	}

	var v interface{}
	for v, err = rd1.ReadOne(); err == nil; v, err = rd1.ReadOne() {
		tester = tester.testV(t, v)
	}

	if err != ErrEOF {
		t.Fatal("Not eof error:", err)
	}

	rd1.AutoReset(false)

	for ; counter < 7; counter++ {
		topic.Write(int(counter))
	}

	_, err = rd1.ReadBatch()
	if err != ErrEOF {
		t.Fatal("Not eof error:", err)
	}

	rd1.AutoReset(true)

	var vs []interface{}
	vs, err = rd1.ReadBatch()

	if len(vs) != 2 {
		t.Fatal("Unexpect batch length", len(vs))
	}

	for _, vv := range vs {
		tester = tester.testV(t, vv)
	}

	for v, err = rd1.ReadOne(); err == nil; v, err = rd1.ReadOne() {
		tester = tester.testV(t, v)
	}

	if int(tester) != 7 {
		t.Fatal("Unexpect tester value:", tester)
	}

	for ; counter < 12; counter++ {
		topic.Write(int(counter))
	}

	_, err = cli1.Read(topic, ReadPos_Default)
	if err == nil {
		t.Fatal("Unexpect success on new read 1")
	}

	rd2, err = cli1.Read(topic, ReadPos_ResumeOrDefault)
	if err != nil {
		t.Fatal("cli1 reader 2 fail", err)
	}

	for v, err = rd2.ReadOne(); err == nil; v, err = rd2.ReadOne() {
		tester = tester.testV(t, v)
	}

	if int(tester) != 12 {
		t.Fatal("Unexpect tester value:", tester)
	}

	cli1.UnReg(topic)

	if !topic.dryRun {
		t.Fatal("Dry run is not resume")
	}

	for ; counter < 15; counter++ {
		topic.Write(int(counter))
	}

	_, err = cli1.Read(topic, ReadPos_Resume)
	if err != ErrDropOut {
		t.Fatal("Unexpect success on new read 2")
	}

	rd3, err = cli1.Read(topic, ReadPos_Latest)
	if err != nil {
		t.Fatal("cli1 reader 2 fail", err)
	}

	//tester is wrong, so it will fail if we have any reading
	for v, err = rd3.ReadOne(); err == nil; v, err = rd3.ReadOne() {
		tester = tester.testV(t, v)
	}

	tester = readTester(counter)

	for ; counter < 20; counter++ {
		topic.Write(int(counter))
	}

	for v, err = rd3.ReadOne(); err == nil; v, err = rd3.ReadOne() {
		tester = tester.testV(t, v)
	}

	for ; counter < 33; counter++ {
		topic.Write(int(counter))
		v, err = rd3.ReadOne()
		if err != nil {
			t.Fatal("cli1 reader 3 fail", err)
		}
		tester = tester.testV(t, v)
	}

	for ; counter < 35; counter++ {
		topic.Write(int(counter))
	}

	vs, err = rd3.ReadBatch()

	if len(vs) != 2 {
		t.Fatal("Unexpect batch length in rd3/1", len(vs))
	}

	for _, vv := range vs {
		tester = tester.testV(t, vv)
	}

	for ; counter < 39; counter++ {
		topic.Write(int(counter))
	}

	vs, err = rd3.ReadBatch()

	if len(vs) != 1 {
		t.Fatal("Unexpect batch length in rd3/2", len(vs))
	}

	for _, vv := range vs {
		tester = tester.testV(t, vv)
	}

	vs, err = rd3.ReadBatch()

	if len(vs) != 3 {
		t.Fatal("Unexpect batch length in rd3/3", len(vs))
	}

	for _, vv := range vs {
		tester = tester.testV(t, vv)
	}

	if int(tester) != 39 {
		t.Fatal("Unexpect final tester value:", tester)
	}
}

func TestMutipleReader(t *testing.T) {

	var counter int
	var tester1, tester2, tester3 readTester
	var err error
	var v interface{}

	topic := InitTopic(defaultTestConfig)

	cli1 := topic.Clients().AssignClient()
	cli2 := topic.Clients().AssignClient()
	cli3 := topic.Clients().AssignClient()

	var rd1, rd2, rd3 *reader
	for ; counter < 4; counter++ {
		topic.Write(int(counter))
	}

	rd1, err = cli1.Read(topic, ReadPos_Default)

	if err != nil {
		t.Fatal("cli1 reader fail", err)
	}

	rd2, err = cli2.Read(topic, ReadPos_ResumeOrLatest)

	if err != nil {
		t.Fatal("cli2 reader fail", err)
	}

	for v, err = rd1.ReadOne(); err == nil; v, err = rd1.ReadOne() {
		tester1 = tester1.testV(t, v)
	}

	_, err = rd2.ReadOne()
	if err != ErrEOF {
		t.Fatal("cli2 have unexpected read", err)
	}

	checkTopic(t, topic, 1, 0, 0)
	if passed := topic.clients.passedSeries; passed != 1 {
		t.Fatal("wrong pass position", passed)
	}

	rd3, err = cli3.Read(topic, ReadPos_Default)

	if err != nil {
		t.Fatal("cli3 reader fail", err)
	}

	checkTopic(t, topic, 1, 0, 0)

	if passed := topic.clients.passedSeries; passed != 0 {
		t.Fatal("wrong pass position", passed)
	}

	var vs []interface{}

	for vs, err = rd3.ReadBatch(); err == nil; vs, err = rd3.ReadBatch() {

		for _, vv := range vs {
			tester3 = tester3.testV(t, vv)
		}
	}

	tester2 = tester1

	//mutiple thread read
	endnotify := make(chan error)
	go func() {
		for int(tester3) < 36 {
			v, err = rd3.ReadOne()
			if err == ErrEOF {
				time.Sleep(time.Millisecond)
			} else if err != nil {
				endnotify <- err
			} else {
				tester3 = tester3.testV(t, v)
			}
		}

		endnotify <- nil
	}()

	for ; counter < 36; counter++ {
		topic.Write(int(counter))
	}

	checkTopic(t, topic, 12, 0, 11)

	err = <-endnotify
	if err != nil {
		t.Fatal("cli3 reader in another thread fail", err)
	}

	for v, err = rd1.ReadOne(); err == nil; v, err = rd1.ReadOne() {
		tester1 = tester1.testV(t, v)
	}

	for vs, err = rd2.ReadBatch(); err == nil; vs, err = rd2.ReadBatch() {

		for _, vv := range vs {
			tester2 = tester2.testV(t, vv)
		}
	}

	if int(tester2) != 36 {
		t.Fatal("Unexpect tester2 value:", tester2)
	}
}

func TestUnReg(t *testing.T) {

	var counter int
	var tester1, tester2, tester3, tester4 readTester
	var err error
	var v interface{}

	topic := InitTopic(defaultTestConfig)

	cli1 := topic.Clients().AssignClient()
	cli2 := topic.Clients().AssignClient()
	cli3 := topic.Clients().AssignClient()
	cli4 := topic.Clients().AssignClient()

	var rd1, rd2, rd3, rd4 *reader
	for ; counter < 4; counter++ {
		topic.Write(int(counter))
	}

	rd1, err = cli1.Read(topic, ReadPos_Default)

	if topic.dryRun {
		t.Fatal("unexpected dryrun")
	}

	if err != nil {
		t.Fatal("cli1 reader fail", err)
	}

	rd2, err = cli2.Read(topic, ReadPos_Latest)

	if err != nil {
		t.Fatal("cli2 reader fail", err)
	}

	rd3, err = cli3.Read(topic, ReadPos_Default)

	if err != nil {
		t.Fatal("cli2 reader fail", err)
	}

	var vs []interface{}

	for vs, err = rd3.ReadBatch(); err == nil; vs, err = rd3.ReadBatch() {

		for _, vv := range vs {
			tester3 = tester3.testV(t, vv)
		}
	}

	for v, err = rd1.ReadOne(); err == nil; v, err = rd1.ReadOne() {
		tester1 = tester1.testV(t, v)
	}

	tester2 = tester1

	for ; counter < 36; counter++ {
		topic.Write(int(counter))
	}

	for vs, err = rd3.ReadBatch(); err == nil; vs, err = rd3.ReadBatch() {

		for _, vv := range vs {
			tester3 = tester3.testV(t, vv)
		}
	}

	v, err = rd2.ReadOne()
	if err != nil {
		t.Fatal("cli2 read fail", err)
	}

	tester2.testV(t, v)

	for v, err = rd1.ReadOne(); err == nil; v, err = rd1.ReadOne() {
		tester1 = tester1.testV(t, v)
	}

	checkTopic(t, topic, 12, 0, 11)
	if passed := topic.clients.passedSeries; passed != 1 {
		dumpTopic(t, "wrong pass position 1", topic)
		t.Fatal("wrong pass position:", passed)
	}

	rd4, err = cli4.Read(topic, ReadPos_Default)
	if err != nil {
		t.Fatal("cli4 reader fail", err)
	}

	v, err = rd4.ReadOne()
	if err != nil {
		t.Fatal("cli4 read fail", err)
	}

	tester4 = readTester(33)
	tester4.testV(t, v)

	cli4.UnReg(topic)

	cli2.UnReg(topic)
	checkTopic(t, topic, 12, 9, 11)
	if passed := topic.clients.passedSeries; passed != 12 {
		dumpTopic(t, "wrong pass position 2", topic)
		t.Fatal("wrong pass position:", passed)
	}

	if topic.dryRun {
		t.Fatal("unexpected dryrun")
	}

	for ; counter < 40; counter++ {
		topic.Write(int(counter))
	}

	checkTopic(t, topic, 13, 9, 12)
}

func TestForceDropOut(t *testing.T) {

	var counter int
	var tester1, tester2 readTester
	var err error
	var v interface{}

	dropOutcfg := testConf()
	err = dropOutcfg.MaxBatch(5)
	if err != nil {
		t.Fatal("Set maxbatch fail", err)
	}

	topic := InitTopic(dropOutcfg)

	cli1 := topic.Clients().AssignClient()
	cli2 := topic.Clients().AssignClient()

	var rd1, rd2 *reader

	rd1, err = cli1.Read(topic, ReadPos_Default)

	if err != nil {
		t.Fatal("cli1 reader fail", err)
	}

	rd2, err = cli2.Read(topic, ReadPos_ResumeOrLatest)

	if err != nil {
		t.Fatal("cli2 reader fail", err)
	}

	for ; counter < 30; counter++ {
		topic.Write(int(counter))
		v, err = rd1.ReadOne()
		if err != nil {
			t.Fatal("cli1 reader fail", err)
		}
		tester1 = tester1.testV(t, v)
	}

	checkTopic(t, topic, 10, 7, 9)

	_, err = rd2.ReadOne()
	if err != ErrDropOut {
		t.Fatal("No dropout", err)
	}

	rd2, err = cli2.Read(topic, ReadPos_Resume)
	if err != ErrDropOut {
		t.Fatal("No dropout", err)
	}

	rd2, err = cli2.Read(topic, ReadPos_ResumeOrDefault)
	if err != nil {
		t.Fatal("cli2 reader fail", err)
	}

	var vs []interface{}

	tester2 = readTester(27)

	for vs, err = rd2.ReadBatch(); err == nil; vs, err = rd2.ReadBatch() {

		for _, vv := range vs {
			tester2 = tester2.testV(t, vv)
		}
	}
}
