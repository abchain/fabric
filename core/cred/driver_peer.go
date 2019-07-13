package credentials

import (
	"errors"
	"github.com/abchain/fabric/core/config"
	"github.com/spf13/viper"
)

var DriverImpl = map[string]func(*viper.Viper, *Credentials_PeerDriver) error{}

//driver read config and build a custom CredentialCore

type Credentials_PeerDriver struct {
	PeerValidator PeerCreds
	TxValidator   TxHandlerFactory
	TxEndorserDef TxEndorserFactory

	//if config file specified a "custom" endorser and it can be obtained
	//from this field, TxEndorserDef will be set to the corresponding one
	SuppliedEndorser map[string]TxEndorserFactory
}

/*
	configure the peer's credential from config files, if suitable content
	has been found, the corresponding item in driver struct is set and the
	fields can not be configured will be untouched

	when Credentials_PeerCredBase is empty, new Credentials_PeerCredBase is
	created, or if it has been set, driver will try to merge the new content
	into it

	it configue the per-peer creds while a endorser may be also derived from
	the peer credential
*/
func (drv *Credentials_PeerDriver) Drive(vp *viper.Viper) error {
	drv.SuppliedEndorser = make(map[string]TxEndorserFactory)

	credtopic := vp.GetString("credential.topic")
	if credtopic == "" {
		return errors.New("No credential scheme topic")
	}

	if f, ok := DriverImpl[credtopic]; !ok {
		return errors.New("topic not availiable: " + credtopic)
	} else {
		return f(config.SubViper("credential", vp), drv)
	}
}
