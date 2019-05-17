package util

//a simple sorted range [closed, open) array (not tree), invalid
//range is not allow and PANIC
//it has been tested outside of this projected
type RangeArray struct {
	ranges [][2]uint64
}

//-1 indicate not found
func (ra RangeArray) Find(pos uint64) int {
	for i, r := range ra.ranges {
		//no change, do not find
		if pos < r[0] {
			return -1
		} else if pos < r[1] {
			return i
		}
	}

	return -1
}

func (ra RangeArray) Get() [][2]uint64 { return ra.ranges }

//add or merge
func (ra *RangeArray) Insert(r [2]uint64) {
	//sanity check
	if r[1] <= r[0] {
		panic("invalid range")
	}

	for i, rr := range ra.ranges {
		//no change, do not find
		if r[0] < rr[0] {
			//must insert here
			ra.ranges = append(ra.ranges[:i+1], ra.ranges[i:]...)
			ra.ranges[i] = r

			ra.repair(i)
			return

		} else if r[0] <= rr[1] {
			//must merge here
			if r[1] > rr[1] {
				ra.ranges[i][1] = r[1]
			}
			ra.repair(i)
			return
		}
	}

	ra.ranges = append(ra.ranges, r)
}

//after a range is inserted, the ranges follwing it may be also merged ...
func (ra *RangeArray) repair(pos int) {
	testR := ra.ranges[pos]
	finalPos := pos + 1

	for i, r := range ra.ranges[finalPos:] {

		if r[0] > testR[1] {
			//just a short cut: nothing needs copy and we can exit
			if i == 0 {
				return
			}
			//copy this
			ra.ranges[finalPos] = r
			finalPos++
		} else {
			//merge this
			if r[1] > testR[1] {
				ra.ranges[pos][1] = r[1]
			}

		}
	}

	ra.ranges = ra.ranges[:finalPos]
}
