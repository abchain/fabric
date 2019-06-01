package monologue_test

import (
	"github.com/abchain/fabric/consensus/monologue"
	cspb "github.com/abchain/fabric/consensus/protos"
	pb "github.com/abchain/fabric/protos"
	"testing"
)

func newBlock(n uint64, tag string, last *cspb.PurposeBlock) *cspb.PurposeBlock {

	blk := pb.NewBlock(nil, []byte("test"))
	blk.StateHash = []byte(tag)

	if last != nil {

		hash, err := last.GetB().GetHash()
		if err != nil {
			panic(err)
		}
		blk.PreviousBlockHash = hash
	}

	return &cspb.PurposeBlock{N: n, B: blk}
}

func TestInOrder(t *testing.T) {

	core := monologue.NewCore(10)

	blk0 := newBlock(12210, "base", nil)
	blk1 := newBlock(12211, "t", blk0)
	blk2 := newBlock(12212, "t", blk1)
	blk3 := newBlock(12213, "t", blk2)
	blk4 := newBlock(12214, "t", blk3)
	blk5 := newBlock(12215, "t", blk4)

	if ret := core.ConsensusInput(blk0); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.ConsensusInput(blk1); blk0.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.ConsensusInput(blk2); blk1.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.ConsensusInput(blk3); blk2.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.ConsensusInput(blk4); blk3.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.ConsensusInput(blk5); blk4.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}
}

func TestReverseOrder(t *testing.T) {

	core := monologue.NewCore(10)

	blk0 := newBlock(12210, "base", nil)
	blk1 := newBlock(12211, "t", blk0)
	blk2 := newBlock(12212, "t", blk1)
	blk3 := newBlock(12213, "t", blk2)
	blk4 := newBlock(12214, "t", blk3)
	blk5 := newBlock(12215, "t", blk4)

	if ret := core.ConsensusInput(blk5); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.ConsensusInput(blk4); blk4.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.ConsensusInput(blk3); blk3.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.ConsensusInput(blk2); blk2.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.ConsensusInput(blk1); blk1.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.ConsensusInput(blk0); blk0.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}
}

func TestOutofOrder(t *testing.T) {

	core := monologue.NewCore(10)

	blk0 := newBlock(12210, "base", nil)
	blk1 := newBlock(12211, "t", blk0)
	blk2 := newBlock(12212, "t", blk1)
	blk3 := newBlock(12213, "t", blk2)
	blk4 := newBlock(12214, "t", blk3)
	blk5 := newBlock(12215, "t", blk4)

	if ret := core.ConsensusInput(blk3); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.ConsensusInput(blk0); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.ConsensusInput(blk5); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.ConsensusInput(blk2); blk2.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.ConsensusInput(blk1); ret.GetBlocks().Blks[1] != blk1 || ret.GetBlocks().Blks[0] != blk0 {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.ConsensusInput(blk4); ret.GetBlocks().Blks[1] != blk4 || ret.GetBlocks().Blks[0] != blk3 {
		t.Fatal("Not expected block", ret)
	}
}

func TestBranch(t *testing.T) {

	core := monologue.NewCore(10)

	blk0 := newBlock(12210, "base", nil)
	blk1 := newBlock(12211, "t", blk0)
	blk2 := newBlock(12212, "t", blk1)
	blk3 := newBlock(12213, "t", blk2)
	blk4 := newBlock(12214, "t", blk3)
	blk5 := newBlock(12215, "t", blk4)

	blk21 := newBlock(12212, "b1", blk1)
	blk31 := newBlock(12213, "b1", blk2)
	blk32 := newBlock(12213, "b2", blk2)
	blk51 := newBlock(12215, "b1", blk4)

	if ret := core.ConsensusInput(blk31); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.ConsensusInput(blk5); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.ConsensusInput(blk51); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.ConsensusInput(blk4); blk4.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.ConsensusInput(blk21); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.ConsensusInput(blk32); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.ConsensusInput(blk3); blk3.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.ConsensusInput(blk4); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.ConsensusInput(blk21); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.ConsensusInput(blk1); blk1.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.ConsensusInput(blk2); blk2.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.ConsensusInput(blk1); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.ConsensusInput(blk0); blk0.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}

}

func TestOutDatedData(t *testing.T) {

	core := monologue.NewCore(10)

	blk0 := newBlock(12210, "base", nil)
	blk1 := newBlock(12211, "t", blk0)
	blk2 := newBlock(12212, "t", blk1)
	blk3 := newBlock(12213, "t", blk2)
	blk4 := newBlock(12214, "t", blk3)

	blk21 := newBlock(722, "b1", blk1)
	blk22 := newBlock(722, "b2", blk1)
	blk31 := newBlock(723, "b1", blk21)
	blk33 := newBlock(12213, "b2", blk2)

	if ret := core.ConsensusInput(blk21); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.ConsensusInput(blk22); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.ConsensusInput(blk31); blk21.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.ConsensusInput(blk2); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.ConsensusInput(blk33); blk2.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.ConsensusInput(blk4); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.ConsensusInput(blk0); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.ConsensusInput(blk1); ret.GetBlocks().Blks[1] != blk1 || ret.GetBlocks().Blks[0] != blk0 {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.ConsensusInput(blk3); blk3.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}
}

func TestTip(t *testing.T) {

	core := monologue.NewCore(10)

	blk0 := newBlock(12210, "base", nil)
	blk1 := newBlock(12211, "t", blk0)
	blk2 := newBlock(12212, "t", blk1)
	blk3 := newBlock(12213, "t", blk2)
	blk4 := newBlock(12214, "t", blk3)
	blk5 := newBlock(12215, "t", blk4)
	blk41 := newBlock(12214, "b1", blk3)

	if ret := core.ConsensusInput(blk4); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.ConsensusInput(blk41); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.ConsensusInput(blk2); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.Tip(12212, blk3.GetB().GetPreviousBlockHash()); blk2.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.ConsensusInput(blk3); blk3.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.Tip(12214, blk5.GetB().GetPreviousBlockHash()); blk4.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.ConsensusInput(blk5); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}
}

func TestTip2(t *testing.T) {

	core := monologue.NewCore(10)

	blk0 := newBlock(12210, "base", nil)
	blk1 := newBlock(12211, "t", blk0)
	blk2 := newBlock(12212, "t", blk1)
	blk3 := newBlock(12213, "t", blk2)
	blk4 := newBlock(12214, "t", blk3)
	blk5 := newBlock(12215, "t", blk4)
	blk41 := newBlock(12214, "b1", blk3)

	if ret := core.ConsensusInput(blk4); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.ConsensusInput(blk41); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.Tip(12212, blk3.GetB().GetPreviousBlockHash()); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}

	if ret := core.ConsensusInput(blk2); blk2.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.ConsensusInput(blk3); blk3.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.Tip(12214, blk5.GetB().GetPreviousBlockHash()); blk4.GetB() != ret.GetBlock() {
		t.Fatal("Not expected block", ret)
	}

	if ret := core.ConsensusInput(blk5); ret.GetNothing() == nil {
		t.Fatal("Not nothing", ret)
	}
}
