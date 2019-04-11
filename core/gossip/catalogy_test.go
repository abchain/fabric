package gossip_test

import (
	"github.com/abchain/fabric/core/gossip"
	model "github.com/abchain/fabric/core/gossip/model"
	//this import is required to init the GossipFactory func
	_ "github.com/abchain/fabric/core/gossip/stub"
	"github.com/abchain/fabric/core/peer"
	pb "github.com/abchain/fabric/protos"
	"golang.org/x/net/context"
	"log"
	"testing"
	"time"
)

type testCatalogy struct {
	id string
	gossip.CatalogPolicies
	handler gossip.CatalogHandler
}

const (
	testCat = "TestCat"
)

func (tc *testCatalogy) Name() string                        { return testCat }
func (tc *testCatalogy) GetPolicies() gossip.CatalogPolicies { return tc.CatalogPolicies }

func (tc *testCatalogy) TransDigestToPb(d_in model.Digest) *pb.GossipMsg_Digest {
	return model.TestDigestToPb(d_in)
}

func (tc *testCatalogy) TransPbToDigest(dig *pb.GossipMsg_Digest) model.Digest {
	return model.TestPbToDigest(dig)
}

func (tc *testCatalogy) TransUpdateToPb(cpo gossip.CatalogPeerPolicies, u model.Update) *pb.GossipMsg_Update {
	return model.TestUpdateEncode(u)
}
func (tc *testCatalogy) TransPbToUpdate(cpo gossip.CatalogPeerPolicies, msg *pb.GossipMsg_Update) (model.Update, error) {
	return model.TestUpdateDecode(msg), nil
}

var testRegisterCat []func(*gossip.GossipStub)

func restoreGossipModule() {
	testRegisterCat = nil
}

func initGossipModule(t *testing.T, pendingCat *testCatalogy, m *model.Model) {

	testRegisterCat = append(testRegisterCat,
		func(stub *gossip.GossipStub) {

			t.Log("Register cat func is called by stub", stub.GetSelf().GetName())

			if pendingCat.id == stub.GetSelf().GetName() {

				t.Logf("Pending catalogy %s is added", pendingCat.id)
				pendingCat.CatalogPolicies = gossip.NewCatalogPolicyDefault()

				if pendingCat.handler != nil {
					t.Fatalf("pending catalogy %s is inited by another stub", pendingCat.id)
				}

				pendingCat.handler = gossip.NewCatalogHandlerImpl(stub.GetSStub(), stub.GetStubContext(), pendingCat, m)

				stub.AddCatalogHandler(pendingCat.handler)
			}
		})

}

func newTestCatalogy(id string) *testCatalogy {
	return &testCatalogy{id: id}
}

func TestCatalogyInit(t *testing.T) {

	defer restoreGossipModule()

	c1 := newTestCatalogy("alice")
	c2 := newTestCatalogy("bob")

	p1 := model.NewTestPeer(t, c1.id)
	p2 := model.NewTestPeer(t, c2.id)

	initGossipModule(t, c1, p1.CreateModel())
	initGossipModule(t, c2, p2.CreateModel())

	newGossip(t, peer.NewPeer(&pb.PeerEndpoint{ID: &pb.PeerID{Name: c1.id}}), testRegisterCat)

	if c1.handler == nil {
		t.Fatal("cat alice is not inited ")
	}

	if c2.handler != nil {
		t.Fatal("cat bob is ghostly inited ")
	}

	newGossip(t, peer.NewPeer(&pb.PeerEndpoint{ID: &pb.PeerID{Name: c2.id}}), testRegisterCat)

	if c2.handler == nil {
		t.Fatal("cat bob is not inited ")
	}
}

func preparePeerWithCatalogy(t *testing.T, cat *testCatalogy) (model.TestPeer, *pb.StreamStub) {

	defer restoreGossipModule()

	tpeer := model.NewTestPeer(t, cat.id)

	initGossipModule(t, cat, tpeer.CreateModel())

	gs := newGossip(t, peer.NewPeer(&pb.PeerEndpoint{ID: &pb.PeerID{Name: cat.id}}), testRegisterCat)

	return tpeer, gs.GetSStub()
}

func groupPeerNetwork(t *testing.T, ctx context.Context,
	ids []string) (cats []*testCatalogy, peers []model.TestPeer) {

	var stubs []*pb.StreamStub

	for _, id := range ids {
		c := newTestCatalogy(id)
		peer, stub := preparePeerWithCatalogy(t, c)
		cats = append(cats, c)
		peers = append(peers, peer)
		stubs = append(stubs, stub)
	}

	for i, stub := range stubs {

		simupeer := pb.NewSimuPeerStub(ids[i], stub)

		//connect with rest ...
		for j, stubTo := range stubs[i+1:] {
			simupeerTo := pb.NewSimuPeerStub(ids[i+j+1], stubTo)

			err, tf := simupeer.ConnectTo(ctx, simupeerTo)
			if err != nil {
				t.Fatal("Connect peer fail", err)
			}

			go func(f func() error, id1 string, id2 string) {

				t.Logf("Start bi-traffic betweeen %s and %s", id1, id2)

				for f() == nil {
					t.Logf("There is one traffic between %s and %s", id1, id2)
				}

			}(tf, ids[i], ids[i+j+1])
		}
	}

	return
}

func TestCatalogyIn2Peer(t *testing.T) {

	wctx, endworks := context.WithCancel(context.Background())
	defer endworks()
	peer.PeerGlobalParentCtx = wctx

	cats, peers := groupPeerNetwork(t, wctx, []string{"alice", "bob"})

	if len(cats) != 2 || len(peers) != 2 {
		log.Fatal("catalogys or peers count not match")
	}

	cAlice := cats[0]
	pAlice := peers[0]
	cBob := cats[1]
	pBob := peers[1]

	//known each other
	cAlice.handler.SelfUpdate()

	time.Sleep(time.Second * 1)

	if len(pAlice.DumpPeers()) < 2 {
		t.Fatal("alice is not known bob?", pAlice.DumpPeers())
	}

	if len(pBob.DumpPeers()) < 2 {
		t.Fatal("bob is not known alice?", pBob.DumpPeers())
	}

	//one update
	pAlice.LocalUpdate([]string{"a1", "a2", "a3"})
	cAlice.handler.SelfUpdate()

	time.Sleep(time.Second * 1)

	if len(pBob.DumpData()) < 3 {
		t.Fatal("bob do not receive enough update from alice", pBob.DumpData())
	}

	//both update
	pBob.LocalUpdate([]string{"b1", "b2"})
	pAlice.LocalUpdate([]string{"a1", "a4"})
	cAlice.handler.SelfUpdate()
	cBob.handler.SelfUpdate()

	time.Sleep(time.Second * 1)

	if len(pAlice.DumpData()) < 6 {
		t.Fatal("alice do not receive enough update from alice", pAlice.DumpData())
	}

	if len(pBob.DumpData()) < 6 {
		t.Fatal("bob do not receive enough update from alice", pBob.DumpData())
	}

	if pBob.DumpData()["a1"] < 3 {
		t.Fatal("bob do not receive updated value from alice", pBob.DumpData())
	}
}
