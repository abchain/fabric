package protos

import "math"
import "encoding/binary"

var sqrtLn2 = math.Ln2 * math.Ln2

func estimateSlots(estimatedSize uint) uint {

	//simply calc the slots size by pre-calc table
	if estimatedSize < 24 {
		return 256
	} else if estimatedSize < 48 {
		return 512
	} else if estimatedSize < 96 {
		return 1024
	} else if estimatedSize < 192 {
		return 2048
	} else if estimatedSize < 384 {
		return 4096
	} else {
		return 8192
	}

}

func (f *StateFilter) InitAuto(estimatedSize uint) float64 {
	return f.Init(estimateSlots(estimatedSize), estimatedSize)
}

func (f *StateFilter) Init(slots uint, estimatedSize uint) float64 {
	if slots < 128 || slots%8 != 0 {
		panic("slots must be larger than 128 and aligned on 8")
	}

	f.Filter = make([]byte, slots/8)
	f.HashCounts = uint32(math.Floor(float64(slots) * float64(math.Ln2) / float64(estimatedSize)))
	if f.HashCounts == 0 {
		f.HashCounts = 1
	}

	logger.Debugf("start simple bloomfilter [%d slots, %d hashes] for collection with size [%d]", slots, f.HashCounts, estimatedSize)

	//return the possibility of false positive
	return math.Exp(float64(-1) * float64(slots) * float64(sqrtLn2) / float64(estimatedSize))
}

func (f *StateFilter) ResetFilter() {
	f.Filter = make([]byte, len(f.Filter))
}

func (f *StateFilter) setBits(pos uint16) {

	f.Filter[pos/8] |= byte(1 << (pos % 8))

}

func (f *StateFilter) checkBits(pos uint16) bool {

	return f.Filter[pos/8]&byte(1<<(pos%8)) != 0

}

func (f *StateFilter) Add(h []byte) {

	modv := len(f.Filter) * 8
	if l := len(h); l > 2*int(f.HashCounts) {
		h = h[:2*int(f.HashCounts)]
	} else if l%2 != 0 {
		//append zero to the end of h
		h = append(h, byte(0))
	}

	for hashF := h; len(hashF) > 0; hashF = hashF[2:] {
		f.setBits(uint16(int(binary.BigEndian.Uint16(hashF)) % modv))
	}

}

func (f *StateFilter) Match(h []byte) bool {

	modv := len(f.Filter) * 8
	if l := len(h); l > 2*int(f.HashCounts) {
		h = h[:2*int(f.HashCounts)]
	} else if l%2 != 0 {
		//append zero to the end of h
		h = append(h, byte(0))
	}
	for hashF := h; len(hashF) > 0; hashF = hashF[2:] {
		if !f.checkBits(uint16(int(binary.BigEndian.Uint16(hashF)) % modv)) {
			return false
		}
	}

	return true
}
