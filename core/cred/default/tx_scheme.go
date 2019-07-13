package cred_default

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/hmac"
	"github.com/abchain/fabric/core/crypto/primitives"
	"math/big"
)

/*
	crypto schemes for transaction, including the key-derivation of tx signature and encryption of attribution
*/

// the secret algorithm with double HMAC
func derivedSecret(kdfkey []byte, tidx []byte) []byte {

	mac := hmac.New(primitives.GetDefaultHash(), kdfkey)
	mac.Write([]byte{2})
	mac = hmac.New(primitives.GetDefaultHash(), mac.Sum(nil))
	mac.Write(tidx)

	return mac.Sum(nil)
}

var bi_one = new(big.Int).SetInt64(1)

//ECDSA version
func txCertKeyDerivationEC(inpub crypto.PublicKey, kdfkey []byte, tidx []byte) (*ecdsa.PublicKey, error) {

	pub, ok := inpub.(*ecdsa.PublicKey)
	if !ok {
		logger.Errorf("the public key (%v) is not ECDSA", inpub)
		return nil, ErrInvalidKey
	}

	logger.Debugf("Derived pkx %s with kdf %X and tidx %X", pub.X, kdfkey, tidx)

	k := new(big.Int).SetBytes(derivedSecret(kdfkey, tidx))
	k.Mod(k, new(big.Int).Sub(pub.Curve.Params().N, bi_one))
	k.Add(k, bi_one)

	tmpX, tmpY := pub.ScalarBaseMult(k.Bytes())
	txX, txY := pub.Curve.Add(pub.X, pub.Y, tmpX, tmpY)
	return &ecdsa.PublicKey{Curve: pub.Curve, X: txX, Y: txY}, nil
}

func txCertPrivKDerivationEC(inprivk crypto.PrivateKey, kdfkey []byte, tidx []byte) (*ecdsa.PrivateKey, error) {

	privk, ok := inprivk.(*ecdsa.PrivateKey)
	if !ok {
		logger.Errorf("the key (%v) is not ECDSA", inprivk)
		return nil, ErrInvalidKey
	}

	logger.Debugf("Derived key (pkx %s) with kdf %X and tidx %X", privk.PublicKey.X, kdfkey, tidx)

	tempSK := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: privk.Curve,
			X:     new(big.Int),
			Y:     new(big.Int),
		},
		D: new(big.Int),
	}

	var k = new(big.Int).SetBytes(derivedSecret(kdfkey, tidx))
	n := new(big.Int).Sub(privk.Params().N, bi_one)
	k.Mod(k, n)
	k.Add(k, bi_one)

	tempSK.D.Add(privk.D, k)
	tempSK.D.Mod(tempSK.D, privk.Params().N)

	tempX, tempY := privk.PublicKey.ScalarBaseMult(k.Bytes())
	tempSK.PublicKey.X, tempSK.PublicKey.Y =
		tempSK.PublicKey.Add(
			privk.PublicKey.X, privk.PublicKey.Y,
			tempX, tempY,
		)

	// Verify temporary public key is a valid point on the reference curve
	if !tempSK.Curve.IsOnCurve(tempSK.PublicKey.X, tempSK.PublicKey.Y) {
		logger.Errorf("Failed temporary public key IsOnCurve check.")
		return nil, ErrInvalidKey
	}

	return tempSK, nil
}
