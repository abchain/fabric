package protos

import "testing"

func Test_NilDelta(t *testing.T) {

	delta := NewChaincodeStateDelta()
	delta.Set("test", nil, nil)

	if vholder := delta.Get("test"); vholder == nil {
		t.Fatal("Unexpected nil holder")
	} else if vholder.GetValue() != nil {
		t.Fatal("Unexpected value")
	}

	if vholder := delta.Get("notexist"); vholder != nil {
		t.Fatal("Unexpected non-nil holder")
	}

}
