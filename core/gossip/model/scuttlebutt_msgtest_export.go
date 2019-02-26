package gossip_model

import (
	pb "github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/proto"
)

func TestPbToDigest(dig *pb.GossipMsg_Digest) Digest {

	ret := NewscuttlebuttDigest(nil)

	for _, v := range dig.GetPeer().GetPeerD() {
		ret.SetPeerDigest(v.GetPeerName(), testVClock(int(v.Num)))
	}

	return ret

}

func TestDigestToPb(d_in Digest) *pb.GossipMsg_Digest {

	d, ok := d_in.(ScuttlebuttDigest)
	if !ok {
		panic("type error, not ScuttlebuttDigest")
	}

	ret := &pb.GossipMsg_Digest_PeerStates{}

	for _, v := range d.PeerDigest() {
		ret.PeerD = append(ret.PeerD, &pb.GossipMsg_Digest_PeerState{
			PeerName: v.Id,
			Num:      uint64(transVClock(v.V))})
	}

	return &pb.GossipMsg_Digest{D: &pb.GossipMsg_Digest_Peer{Peer: ret}}

}

func TestUpdateEncode(u_in Update, msg *Test_Scuttlebutt) proto.Message {

	u, ok := u_in.(ScuttlebuttUpdate)

	if !ok {
		panic("type error, not ScuttlebuttUpdate")
	}

	msg.Peers = make(map[string]*Test_Scuttlebutt_Peer)

	for _, udata := range u.PeerUpdate() {

		us := transUpdate(udata.U)
		out := &Test_Scuttlebutt_Peer{make(map[string]int32)}
		for k, v := range us.data {
			out.Datas[k] = int32(v)
		}

		msg.Peers[udata.Id] = out

	}

	return msg
}

func TestUpdateDecode(msg_in proto.Message) Update {

	msg, ok := msg_in.(*Test_Scuttlebutt)

	if !ok {
		panic("type error, not Test_Scuttlebutt")
	}

	out := NewscuttlebuttUpdate(nil)

	for id, data := range msg.Peers {

		pu := &testPeerStatus{make(map[string]int)}

		for k, v := range data.Datas {
			pu.data[k] = int(v)
		}

		out.UpdatePeer(id, pu)
	}

	return out

}
