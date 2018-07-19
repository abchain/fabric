package txnetwork

import (
	"github.com/abchain/fabric/core/crypto"
	model "github.com/abchain/fabric/core/gossip/model"
	_ "github.com/abchain/fabric/protos"
	"sync"
)

type txcommonImpl struct {
	secHelper crypto.Peer
	sync.Once
}

var txcommon txcommonImpl

func InitTxNetwork(secHelperFunc func() crypto.Peer) {
	txcommon.Do(func() {
		txcommon.secHelper = secHelperFunc()
	})
}

//a standard vclock use the num field in protos
type standardVClock uint64

func (a standardVClock) Less(b_in model.VClock) bool {
	if b_in == nil {
		return false
	}

	b, ok := b_in.(standardVClock)
	if !ok {
		panic("Wrong type, not standardVClock")
	}

	return a < b
}

//notify remove any peer in a scuttlebutt model
func registerEvictFunc(catname string, m *model.Model) {
	GetNetworkStatus().regNotify(func(ids []string) {

		ru := model.NewscuttlebuttUpdate(nil)
		ru.RemovePeers(ids)

		go func(ru model.Update) {
			err := m.RecvUpdate(ru)
			if err != nil {
				logger.Errorf("Cat %s remove peer fail: %s", catname, err)
			}
		}(ru)

	})
}
