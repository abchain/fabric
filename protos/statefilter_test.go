package protos

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func Test_FilterInit(t *testing.T) {

	fl := new(StateFilter)

	if pr := fl.Init(256, 16) * float64(100); pr > 0.05 || pr < 0.04 {
		t.Fatal("wrong possibility: %f", pr)
	} else if fl.HashCounts != 11 || len(fl.Filter) != 32 {
		t.Fatal("unexpected filter data: [%v]", fl)
	}

	if pr := fl.Init(512, 32) * float64(100); pr > 0.05 || pr < 0.04 {
		t.Fatal("wrong possibility: %f", pr)
	} else if fl.HashCounts != 11 || len(fl.Filter) != 64 {
		t.Fatal("unexpected filter data: [%v]", fl)
	}

	if pr := fl.Init(1024, 72) * float64(100); pr > 0.11 || pr < 0.10 {
		t.Fatal("wrong possibility: %f", pr)
	} else if fl.HashCounts != 9 || len(fl.Filter) != 128 {
		t.Fatal("unexpected filter data: [%v]", fl)
	}

	if pr := fl.Init(2048, 136) * float64(100); pr > 0.08 || pr < 0.07 {
		t.Fatal("wrong possibility: %f", pr)
	} else if fl.HashCounts != 10 || len(fl.Filter) != 256 {
		t.Fatal("unexpected filter data: [%v]", fl)
	}

	if pr := fl.Init(2048, 184) * float64(100); pr > 0.48 || pr < 0.47 {
		t.Fatal("wrong possibility: %f", pr)
	} else if fl.HashCounts != 7 || len(fl.Filter) != 256 {
		t.Fatal("unexpected filter data: [%v]", fl)
	}
}

//some edge case
func Test_EdgeCase(t *testing.T) {

	fl := new(StateFilter)

	fl.Init(512, 32)

	shortElement := [5]byte{9, 8, 7, 6}
	normalElement := [10]byte{9, 8, 7, 6, 5, 4}

	fl.Add(shortElement[:])
	fl.Add(normalElement[:])

	sample1 := [3]byte{4, 3, 2}
	if fl.Match(sample1[:]) {
		t.Fatal("Unexpected match")
	}

	sample2 := [6]byte{9, 8, 7, 6, 0, 1}
	if fl.Match(sample2[:]) {
		t.Fatal("Unexpected match")
	}

	//nil bytes is prone to false positive
	if !fl.Match(nil) {
		t.Fatal("Unexpected negative")
	}

	if !fl.Match(shortElement[:]) {
		t.Fatal("False negative!")
	}

	if !fl.Match(normalElement[:]) {
		t.Fatal("False negative!")
	}

	fl.Init(8192, 320)
	fl.Add(shortElement[:])
	if fl.Match(sample1[:]) {
		t.Fatal("Unexpected match")
	}
	if !fl.Match(shortElement[:]) {
		t.Fatal("False negative!")
	}

}

func Test_FalsePositiveRate(t *testing.T) {

	//test an 256 slot, 20 elements case, the false positive rate should less than 0.6%

	//gen 20 element first, each has 20 bytes (7 hashes require just 14 bytes)
	fl := new(StateFilter)
	fl.Init(256, 24)

	var testSets [][]byte
	for i := 0; i < 20; i++ {

		elm := make([]byte, 20)
		if n, err := rand.Read(elm); n != 20 || err != nil {
			t.Fatal("prepare set fail:", n, err)
		}
		fl.Add(elm)
		testSets = append(testSets, elm)
	}

	checkF := func(h []byte) int {
		for i, target := range testSets {
			if bytes.Compare(h, target) == 0 {
				return i
			}
		}

		return -1
	}

	//check hit first
	for i, sample := range testSets {

		if ret := checkF(sample); ret != i {
			t.Fatalf("check sample[@i] fail: get %d", i, ret)
		}

		if !fl.Match(sample) {
			t.Fatalf("sample get false negative!")
		}
	}

	//start testing ... 1,000,000 cases
	//when run by go 1.10, i5-7200u (2.7G), it cost ~0.67s
	collisions := 0
	falsePositive := 0
	totalNum := 1000000

	for i := 0; i < totalNum; i++ {

		sample := make([]byte, 20)
		if n, err := rand.Read(sample); n != 20 || err != nil {
			t.Fatal("prepare example fail:", n, err)
		}
		//collision of 2^160 must be extremely small, we count it
		//and ... (how many should we tolerate?)
		if ret := checkF(sample); ret != -1 {
			t.Logf("sample collide with [@i]", ret)
			collisions++
			continue
		}

		if fl.Match(sample) {
			falsePositive++
		}
	}

	t.Logf("Finished, falsePositive %d (%f), collision %d", falsePositive, float64(falsePositive*100)/float64(totalNum), collisions)

	if collisions > 5 {
		t.Fatalf("imqualified sample: collision larger than 5/1M")
	}

	if falsePositive*100 > totalNum {
		t.Fatalf("fail filter, false positive ratio larger than 1%")
	}
}
