package syncstrategy

import (
	"fmt"
	"github.com/abchain/fabric/core/ledger"
	"github.com/abchain/fabric/core/sync"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
	"sync"
	"time"
)

type txCliFactory struct {
	opt         *clientOpts
	assignedCnt int
	sync.Mutex
	target []string
	txout  []*pb.Transaction
}

func (cf *txCliFactory) Tag() string { return "testTxClis" }
func (cf *txCliFactory) Opts() *clientOpts {
	return cf.opt
}
func (cf *txCliFactory) PreFilter(_ *pb.LedgerState) bool {
	return true
}
func (cf *txCliFactory) AssignHandling() func(context.Context, *pb.StreamHandler, *syncCore) error {

	cf.Lock()
	defer cf.Unlock()
	if len(cf.target) == 0 {
		//done, just assign a empty function
		return func(*pb.StreamHandler, *syncCore) error {
			return NormalEnd{}
		}
	}

	var assignedTask []string
	if cf.assignedCnt >= len(cf.target) {
		assignedTask = cf.target
		cf.target = nil
	} else {
		assignedPos := len(cf.target) - cf.assignedCnt
		cf.target, assignedTask = cf.target[:assignedPos], cf.target[assignedPos:]
	}

	return func(ctx context.Context, h *pb.StreamHandler, c *syncCore) (err error) {

		var ret []*pb.Transaction
		reside := make(map[string]bool)
		for _, id := range assignedTask {
			reside[id] = false
		}
		defer func() {
			var residearr []string
			for k, done := range reside {
				if !done {
					residearr = append(residearr, k)
				}
			}

			if len(residearr) > 0 {
				clilogger.Debugf("tx sync not finished, resident task: %v", residearr)
				err = fmt.Errorf("Not finished (%d of %d)", len(residearr), len(reside))
			}

			cf.Lock()
			defer cf.Unlock()
			cf.target = append(cf.target, residearr...)
			cf.txout = append(cf.txout, ret...)
		}()

		chn, err := c.Request(h, &pb.SimpleReq{
			Req: &pb.SimpleReq_Tx{Tx: &pb.TxQuery{Txid: assignedTask}},
		})
		if err != nil {
			return err
		}

		select {
		case resp := <-chn:
			if serr := resp.GetErr(); serr != nil {
				return fmt.Errorf("resp err %s", serr.GetErrorDetail())
			} else if txblk := resp.GetSimple().GetTx(); txblk == nil {
				return fmt.Errorf("Empty payload")
			} else {
				for _, tx := range txblk.GetTransactions() {
					if tx != nil {
						reside[tx.GetTxid()] = true
						ret = append(ret, tx)
					}
				}
				return NormalEnd{}
			}

		case <-ctx.Done():
			c.CancelRequst(chn)
			return ctx.Err()
		}
	}
}
