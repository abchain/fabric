package txnetwork

import (
	"fmt"
	"github.com/abchain/fabric/core/gossip"
	model "github.com/abchain/fabric/core/gossip/model"
	"github.com/abchain/fabric/core/gossip/stub"
	pb "github.com/abchain/fabric/protos"
	proto "github.com/golang/protobuf/proto"
)

const (
	syncTxCatName = "synctx"
)

type txsyncCat struct {
	policy       gossip.CatalogPolicies
	tasklist     map[string]bool
	tasklistSent []string
	network      *txNetworkGlobal
}

type txUpdate struct {
	*pb.HotTransactionBlock
}

func (txUpdate) Gossip_IsUpdateIn() bool { return true }

type taskList []string

func (taskList) Gossip_IsUpdateIn() bool { return true }

func init() {
	stub.RegisterCat = append(stub.RegisterCat, initTxSync)
}

func initTxSync(stub *gossip.GossipStub) {

	policy := gossip.NewCatalogPolicyDefault()
	policy.SetPullOnly()

	syncCore := &txsyncCat{
		tasklist: make(map[string]bool),
		policy:   policy,
		network:  getTxNetwork(stub),
	}
	m := model.NewGossipModel(syncCore)

	stub.AddCatalogHandler(gossip.NewCatalogHandlerImpl(stub.GetSStub(),
		stub.GetStubContext(), syncCore, m))

}

//Implement for CatalogHelper
func (c *txsyncCat) Name() string                        { return syncTxCatName }
func (c *txsyncCat) GetPolicies() gossip.CatalogPolicies { return c.policy }

func (c *txsyncCat) GenDigest() model.Digest {

	if len(c.tasklistSent) < len(c.tasklist) {
		c.tasklistSent = make([]string, 0, len(c.tasklist))
		for id, _ := range c.tasklist {
			c.tasklistSent = append(c.tasklistSent, id)
		}
	}

	return taskList(c.tasklistSent)
}

func (c *txsyncCat) Update(u model.Update) error {
	switch ut := u.(type) {
	case txUpdate:
		logger.Debugf("receive %d txs synced", len(ut.Transactions))
		for _, tx := range ut.Transactions {
			delete(c.tasklist, tx.GetTxid())
		}
		c.network.txPool.ledger.PoolTransactions(ut.Transactions)
		c.tasklistSent = nil
	case taskList:
		for _, id := range ut {
			c.tasklist[id] = true
		}
		logger.Debugf("add %d tx tasks, now <%d>", len(ut), len(c.tasklist))
	default:
		return fmt.Errorf("Not recognized type of update: %v", u)
	}

	return nil
}

func (c *txsyncCat) MakeUpdate(d model.Digest) model.Update {
	ld, ok := d.(taskList)
	if !ok {
		panic(fmt.Errorf("Not recognized type of digest: %v", d))
	}

	txblk := new(pb.HotTransactionBlock)

	//sanity check
	if l := c.network.txPool.ledger; l == nil {
		panic(fmt.Errorf("Ledger is not exist"))
	} else {
		for _, txid := range ld {
			if tx, err := l.GetTransactionByID(txid); err != nil {
				logger.Debugf("can not query tx <%s>: %s", txid, err)
				continue
			} else {
				txblk.Transactions = append(txblk.Transactions, tx)
			}

		}
	}

	logger.Debugf("response %d txs from %d request", len(txblk.Transactions), len(ld))

	return txUpdate{txblk}
}

func (c *txsyncCat) TransDigestToPb(d_in model.Digest) *pb.GossipMsg_Digest {

	d, ok := d_in.(taskList)

	if !ok {
		panic("Type error, not string array")
	}

	return &pb.GossipMsg_Digest{
		D:      &pb.GossipMsg_Digest_Tx{Tx: &pb.GossipMsg_Digest_TxStates{TxID: []string(d)}},
		NoResp: true,
	}
}

func (c *txsyncCat) TransPbToDigest(msg *pb.GossipMsg_Digest) model.Digest {

	if s := msg.GetTx(); s == nil {
		return nil
	} else {
		return taskList(s.TxID)
	}
}

func (c *txsyncCat) UpdateMessage() proto.Message { return new(pb.HotTransactionBlock) }

func (c *txsyncCat) TransUpdateToPb(cpo gossip.CatalogPeerPolicies, u_in model.Update) *pb.GossipMsg_Update {

	u, ok := u_in.(txUpdate)

	if !ok {
		panic("Type error, not txUpdate")
	}

	return &pb.GossipMsg_Update{U: &pb.GossipMsg_Update_TxBlock{TxBlock: u.HotTransactionBlock}}
}

func (c *txsyncCat) TransPbToUpdate(cpo gossip.CatalogPeerPolicies, msg_in *pb.GossipMsg_Update) (model.Update, error) {

	m := msg_in.GetTxBlock()
	if m == nil {
		return nil, fmt.Errorf("Msg error, not HotTransactionBlock")
	}

	for _, tx := range m.Transactions {
		if !tx.IsValid() {
			return nil, fmt.Errorf("receive invalid tx <%v>", tx)
		}
	}

	return txUpdate{m}, nil
}
