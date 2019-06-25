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

//peer creds also include the endorse entry because it should be sole per-network
type PeerCreds interface {
	PeerPki() []byte
	PeerCred() []byte
	EndorsePeerMsg(msg *pb.Message) (*pb.Message, error)
	VerifyPeerMsg(pki []byte, msg *pb.Message) error
	VerifyPeerCred([]byte) error
}

type TxHandlerFactory interface {
	ValidatePeerStatus(id string, status *pb.PeerTxState) error
	//notify all of the preparing for a specified id (i.e. caches) can be complete released
	RemovePreValidator(id string)
	//tx prevalidator, handle any tx context with peerID being tagged, and fill the security context
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

//represent the most common implement for credential: the cert-base credential
//A certcred object can always act as a TxHandlerFactory, but
//it must contain a privte key to act as PeerCred
//NOTICE: a cert object can act to mutiple role, it deep copy its data to the
//cred object created in "ActAs..." function
type CertificateCred interface {
	HasPrivKey() bool
	ActAsTxHandlerFactory() (TxHandlerFactory, error)
	ActAsPeerCreds() (PeerCreds, error)
}
