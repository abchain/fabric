package monologue

import (
	"bytes"
	"fmt"
	cspb "github.com/abchain/fabric/consensus/protos"
	pb "github.com/abchain/fabric/protos"
)

type ConsensusCore struct {
	fixed      []*cspb.PurposeBlock
	candidates [][]*cspb.PurposeBlock
	progress   uint64
}

func NewCore(candidateSize int) *ConsensusCore {
	return &ConsensusCore{
		candidates: make([][]*cspb.PurposeBlock, candidateSize),
		fixed:      make([]*cspb.PurposeBlock, candidateSize),
	}
}

func (cc *ConsensusCore) accessFixed(n uint64) (*cspb.PurposeBlock, bool) {
	ind := int(n % uint64(len(cc.fixed)))
	if cc.fixed[ind] == nil {
		return nil, false
	} else if cc.fixed[ind].GetN() != n {
		//out of date data
		return nil, false
	} else {
		return cc.fixed[ind], true
	}
}

func (cc *ConsensusCore) setFixed(pblk *cspb.PurposeBlock) *cspb.PurposeBlock {
	ind := int(pblk.GetN() % uint64(len(cc.fixed)))
	//sanity check
	if nowblk := cc.fixed[ind]; nowblk != nil && nowblk.GetN() == pblk.GetN() {
		panic("duplicated fixed")
	}
	cc.fixed[ind] = pblk
	return pblk
}

func (cc *ConsensusCore) accessCandidate(n uint64) []*cspb.PurposeBlock {
	ind := int(n % uint64(len(cc.candidates)))
	if len(cc.candidates[ind]) == 0 {
		return nil
	} else if cc.candidates[ind][0].GetN() != n {
		//out dated data
		return nil
	} else {
		return cc.candidates[ind]
	}
}

func (cc *ConsensusCore) pushCandidate(pblk *cspb.PurposeBlock) {
	ind := int(pblk.GetN() % uint64(len(cc.candidates)))
	//nothing or out of date array
	if blkArr := cc.candidates[ind]; len(blkArr) == 0 || blkArr[0].GetN() != pblk.GetN() {
		cc.candidates[ind] = []*cspb.PurposeBlock{pblk}
	} else if testh, inh := blkArr[0].GetB().GetPreviousBlockHash(),
		pblk.GetB().GetPreviousBlockHash(); bytes.Compare(testh, inh) == 0 {
		cc.candidates[ind] = append(blkArr, pblk)
	} else {
		//monologue suppose all candidate block with same height has identical parent
		//when this supposing is broken, everything must panic
		panic(fmt.Sprintf("Monologue has fatal error: different parent (cached one for %.16X and new incoming has %.16X",
			testh, inh))
	}
}

//allow an additional tip input to "pop up" the candidate block
func (cc *ConsensusCore) Tip(n uint64, hash []byte) (ret *cspb.ConsensusOutput) {

	if _, existed := cc.accessFixed(n); !existed {
		for _, cpblk := range cc.accessCandidate(n) {
			bhash, _ := cpblk.GetB().GetHash()
			if bytes.Compare(bhash, hash) == 0 {
				out := cc.setFixed(cpblk)
				return &cspb.ConsensusOutput{
					Out:      &cspb.ConsensusOutput_Block{Block: out.GetB()},
					Position: out.GetN(),
				}
			}
		}
		//add tip as an dummy block so tip can be keep effect
		dummyBlk := pb.NewBlock(nil, []byte("dummy"))
		//the only effect field on this block is previous hash
		dummyBlk.PreviousBlockHash = hash

		cc.pushCandidate(&cspb.PurposeBlock{
			N: n + 1,
			B: dummyBlk,
		})
	}

	return &cspb.ConsensusOutput{Out: &cspb.ConsensusOutput_Nothing{Nothing: cspb.Nothing}, Position: cc.progress}
}

//monologue directly output the results
func (cc *ConsensusCore) ConsensusInput(pblk *cspb.PurposeBlock) (ret *cspb.ConsensusOutput) {

	defer func() {
		if ret == nil {
			ret = &cspb.ConsensusOutput{Out: &cspb.ConsensusOutput_Nothing{Nothing: cspb.Nothing}, Position: cc.progress}
		}
	}()

	if blkN := pblk.GetN(); blkN == 0 {
		return &cspb.ConsensusOutput{Out: &cspb.ConsensusOutput_Error{Error: "Wrong input"}}
	} else if blkN+uint64(len(cc.candidates)) < cc.progress {
		//silently engulf out-dated block
		return nil
	}

	//save candidate, and update the progress
	cc.pushCandidate(pblk)

	//we have done, quit
	if _, existed := cc.accessFixed(pblk.GetN()); existed {
		return nil
	}

	if cc.progress < pblk.GetN() {
		cc.progress = pblk.GetN()
	}

	var outBlks []*cspb.PurposeBlock

	//first find the block which incoming may determind
	if _, existed := cc.accessFixed(pblk.GetN() - 1); !existed {
		for _, cpblk := range cc.accessCandidate(pblk.GetN() - 1) {
			bhash, _ := cpblk.GetB().GetHash()
			if bytes.Compare(bhash, pblk.GetB().GetPreviousBlockHash()) == 0 {
				//yes, we found it
				outBlks = append(outBlks, cc.setFixed(cpblk))
				break
			}
		}
	}

	//then decide the block itself
	if candi := cc.accessCandidate(pblk.GetN() + 1); len(candi) > 0 {
		//corrent incoming block can be verify by any candidates of its child
		bhash, err := pblk.GetB().GetHash()
		if err != nil {
			//when we have wrong block we can simply drop it out
			return &cspb.ConsensusOutput{Out: &cspb.ConsensusOutput_Error{Error: err.Error()}}
		}
		if bytes.Compare(candi[0].GetB().GetPreviousBlockHash(), bhash) == 0 {
			//OK! this is the block we expected for!
			outBlks = append(outBlks, cc.setFixed(pblk))
		}
	}

	//now see what result we can out
	if cnt := len(outBlks); cnt == 0 {
		return nil
	} else if cnt == 1 {
		theBlk := outBlks[0]
		return &cspb.ConsensusOutput{
			Out:      &cspb.ConsensusOutput_Block{Block: theBlk.GetB()},
			Position: theBlk.GetN(),
		}
	} else {
		//in fact we have at most two blocks is out and the last one has larger height
		return &cspb.ConsensusOutput{
			Out: &cspb.ConsensusOutput_Blocks{
				Blocks: &cspb.PurposeBlocks{Blks: outBlks},
			},
			Position: outBlks[1].GetN(),
		}
	}

}
