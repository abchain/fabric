package credentials

import (
	pb "github.com/abchain/fabric/protos"
	"github.com/op/go-logging"
)

var logger = logging.MustGetLogger("credential")

/*
	just like fabric, this is a module for manufacturing, managing and verifing
	credentials of txs and peers, base on the certificates with x.509 standard,
	and it was mostly the refactoring of crypto module in fabric 0.6
	(and somewhat like the mxing of bccsp & msp in fabric 1.0)
*/

/*

 -- entries for per-peer (per-network)'s credentials ---

*/

type PeerCred interface {
	Cred() []byte
	//the shared secret between the handshaking peer pair, a key-exchange scheme
	//is recommended but it is not enforced to cover the secret in the traffic texts
	Secret() []byte
	VerifyPeerMsg(msg *pb.Message) error
}

//peer creds also include the endorse entry because it should be sole per-network
type PeerCreds interface {
	PeerCred
	Pki() []byte
	//the pki can be nil for creating a PeerCred for "connect" attempt, pki is
	//nil or not indicate different behavior so caller must verify it first
	CreatePeerCred(cred []byte, pki []byte) (PeerCred, error)
	EndorsePeerMsg(msg *pb.Message) (*pb.Message, error)
}

//txhandlerfactory should be thread-safe
type TxHandlerFactory interface {
	SetIdConverter(func([]byte) string)
	ValidatePeerStatus(id string, status *pb.PeerTxState) error
	//notify all of the preparing for a specified id (i.e. caches) can be complete released
	RemovePreValidator(id string)
	//tx prevalidator, handle security relatedcontext in tx and fill the security context
	GetValidator(id string) pb.TxPreHandler
}

// DataEncryptor is used to encrypt/decrypt chaincode's state data
type DataEncryptor interface {
	Encrypt([]byte) ([]byte, error)
	Decrypt([]byte) ([]byte, error)
}

/*
  (YA-fabric 0.9:
  it is supposed to be created from something like a certfication but will not
  get an implement in recent)
*/
type TxConfidentialityHandler interface {
	//tx preexcution, it parse the tx with specified confidentiality and also prepare the
	//execution context for data encryptor
	pb.TxPreHandler

	//---this method is under considering and may be abandoned later---
	GetStateEncryptor(deployTx, executeTx *pb.Transaction) (DataEncryptor, error)
}

/*

 -- entries for per-user's credentials, user can be actived in mutiple networks---

*/
type TxEndorserFactory interface {
	EndorserId() []byte //notice the endorserid is bytes
	//EndorsePeerState need to consider the exist endorsment field and decide update it or not
	EndorsePeerState(*pb.PeerTxState) (*pb.PeerTxState, error)
	GetEndorser(attr ...string) (TxEndorser, error)
}

type TxEndorser interface {
	EndorseTransaction(*pb.Transaction) (*pb.Transaction, error)
	Release()
}
