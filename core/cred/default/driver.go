package cred_default

import (
	"crypto/x509"

	"github.com/abchain/fabric/core/config"
	cred "github.com/abchain/fabric/core/cred"
	"github.com/abchain/fabric/core/db"
	"github.com/spf13/viper"
)

func init() {
	cred.DriverImpl["x509"] = driverImpl
	cred.DriverImpl["default"] = driverImpl

}

func driverImpl(vp *viper.Viper, cred *cred.Credentials_PeerDriver) error {

	//trigger init on each TLS impl factory and driver
	initGlobalPool()
	logger.Infof("--- Start x509 driver --- ")

	p := combinePersistor(db.NewPersistor(globalPoolpersistor),
		db.NewPersistor(driverPersistor))

	ld := AddPredefineSchemes(NewLoader(p), globalCertPool)

	//load root pool
	var roocerts []*x509.Certificate
	if viper.IsSet("rootcerts") {
		rootscheme, err := ld.loadScheme(config.SubViper("rootcerts", vp))
		if err != nil {
			logger.Errorf("Load rootcert in peer credential fail: %s", err)
		}
		logger.Infof("--- Peer credential use additional root cert pool with %d certs --- ", len(roocerts))
		roocerts = rootscheme.RootCerts()
	}

	//peer cred ()
	scheme, err := ld.loadScheme(config.SubViper("peer", vp))
	if err != nil {
		return err
	} else if cer, key, err := scheme.getKeyPair(); err != nil {
		return err
	} else if !cer.IsCA {
		//the cert used by peer must be a CA (or it can not used to signed the CSR)
		logger.Errorf("The peer cert used in driver is not a CA")
		return ErrCertificate
	} else {

		peerRoots := append(roocerts, scheme.RootCerts()...)
		if len(peerRoots) <= 1 {
			//the only root is just peer cert itself, indicate that peer will accept
			//self-signed cert
			logger.Infof("--- Init peer credential without rootcerts --- ")
			cred.PeerValidator, err = NewPeerCredential(cer, key, nil)
		} else {
			logger.Infof("--- Init peer credential with %d rootcerts --- ", len(peerRoots))
			cred.PeerValidator, err = NewPeerCredential(cer, key, peerRoots)
		}

		if err != nil {
			return err
		}
	}

	//tx cred
	if vp.IsSet("txnet") {
		txval := NewDefaultTxHandler(vp.GetBool("txnet.disableKDF"))
		cred.TxValidator = txval
		scheme, err := ld.loadScheme(config.SubViper("txnet", vp))
		if err != nil {
			return err
		}

		//when the loaded scheme also include a key, it is used for default endorser,
		//unless drepressed by a switch
		if !vp.GetBool("txnet.noendorser") {
			cer, key, _ := scheme.getKeyPair()
			if key != nil {
				logger.Infof("--- Try use key defined in txnetwork for endorser ---")
				cred.TxEndorserDef, err = NewEndorser(cer, key)
				if err != nil {
					return err
				}
			}
		}

		//we automatically detect if txnet accept self-signed cert or not: if endorser
		//has been inited here, the first CA cert specified in txnet is just peer's
		//cert for txnetwork and do not count
		txRoots := append(roocerts, scheme.RootCerts()...)

		if len(txRoots) > 1 || (cred.TxEndorserDef == nil && len(txRoots) > 0) {
			logger.Infof("--- Init txnetwork's credential with %d rootcerts --- ",
				len(txRoots))
			//TODO: current we only update root ...
			txval.UpdateRootCA(txRoots, nil)
		} else {
			logger.Info("--- Init txnetwork's credential without rootcerts --- ")
		}
	}

	//select endorser
	if vp.IsSet("endorser") {

		//TODO: now we just select one from supplied
		usedEndorser := vp.GetString("endorser")
		if cred.SuppliedEndorser == nil {
			logger.Errorf("No supplied endorsers")
			return ErrNotInitialized
		} else if escheme, ok := cred.SuppliedEndorser[usedEndorser]; !ok {

			logger.Errorf("No supplied endorsers has the specified name: %s", usedEndorser)
			return ErrNotInitialized
		} else {
			if cred.TxEndorserDef != nil {
				logger.Warningf("The setting default endorser will be replace by specified one (%s)", usedEndorser)
			} else {
				logger.Infof("Used specified endorser [%s]", usedEndorser)
			}
			cred.TxEndorserDef = escheme
		}

	}

	return nil
}
