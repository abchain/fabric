package framework

import (
	"fmt"
	cred "github.com/abchain/fabric/core/cred"
	"github.com/abchain/fabric/core/gossip/txnetwork"
	"github.com/abchain/fabric/node"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
)

type deliverToNetwork struct {
	targets []struct {
		net      *txnetwork.TxNetworkEntry
		endorser cred.TxEndorser
	}
}

type ResumedDelivery interface {
	Resume(context.Context) error
}

type pendingDelivery struct {
	txs     []*pb.Transaction
	deliver ConsensusTxDeliver
}

func (pd *pendingDelivery) Resume(ctx context.Context) error {
	return pd.deliver.Send(ctx, pd.txs)
}

type deliverProgress struct {
	lastFailure  error
	resumedTask  ResumedDelivery
	pendingTasks ResumedDelivery
}

func (pd *deliverProgress) Error() string { return pd.lastFailure.Error() }

func (pd *deliverProgress) Resume(ctx context.Context) error {
	if err := pd.resumedTask.Resume(ctx); err == nil {
		if pd.pendingTasks == nil {
			return nil
		} else {
			return pd.pendingTasks.Resume(ctx)
		}

	} else if prog, ok := err.(*deliverProgress); ok {
		prog.pendingTasks = pd.pendingTasks
		return prog
	} else if resumed, ok := err.(ResumedDelivery); ok {
		pd.lastFailure = fmt.Errorf("recursive delivery err: %s", err)
		pd.resumedTask = resumed
		return pd
	} else {
		return err
	}
}

func (d deliverToNetwork) Send(ctx context.Context, txs []*pb.Transaction) error {

	for i, tx := range txs {
		for j, target := range d.targets {

			resp := target.net.ExecuteTransaction(ctx, tx, target.endorser)
			if resp.Status == pb.Response_FAILURE {
				return &deliverProgress{
					lastFailure: fmt.Errorf(string(resp.Msg)),
					resumedTask: &pendingDelivery{
						txs:     []*pb.Transaction{tx},
						deliver: deliverToNetwork{d.targets[j:]},
					},
					pendingTasks: &pendingDelivery{
						txs:     txs[i+1:],
						deliver: d,
					},
				}
			}
		}
	}

	return nil
}

func (d deliverToNetwork) AddPeer(theNode *node.NodeEngine, peerName string) (deliverToNetwork, error) {
	peer, existed := theNode.Peers[peerName]
	if !existed {
		return deliverToNetwork{}, fmt.Errorf("peer %s not exist", peerName)
	}
	return deliverToNetwork{append(d.targets, struct {
		net      *txnetwork.TxNetworkEntry
		endorser cred.TxEndorser
	}{peer.TxNetwork(), nil})}, nil
}

func (d deliverToNetwork) AddPeerWithSec(theNode *node.NodeEngine, peerName, secCtx string, attrs ...string) (deliverToNetwork, error) {
	peer, existed := theNode.Peers[peerName]
	if !existed {
		return deliverToNetwork{}, fmt.Errorf("peer %s not exist", peerName)
	}

	ed, err := theNode.SelectEndorser(secCtx)
	if err != nil {
		return deliverToNetwork{}, fmt.Errorf("Obtain endorser failure: %s", err)
	}

	txed, err := ed.GetEndorser(attrs...)
	if err != nil {
		return deliverToNetwork{}, fmt.Errorf("Create tx endorser failure: %s", err)
	}

	return deliverToNetwork{append(d.targets, struct {
		net      *txnetwork.TxNetworkEntry
		endorser cred.TxEndorser
	}{peer.TxNetwork(), txed})}, nil
}

//create deliver from default peer of the node
func GenDeliverFromNode(theNode *node.NodeEngine, secCtx string, attrs ...string) (deliverToNetwork, error) {

	ret := deliverToNetwork{}
	if secCtx == "" {
		return ret.AddPeer(theNode, "")
	} else {
		return ret.AddPeerWithSec(theNode, "", secCtx, attrs...)
	}

}
