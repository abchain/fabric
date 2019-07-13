package credentials

import (
	"github.com/spf13/viper"
)

//driver read config and build a custom CredentialCore

//try to obtain mutiple endorser's configuration from config files
func DriveEndorsers(vp *viper.Viper) (map[string]TxEndorserFactory, error) {
	return nil, nil
}

//try to obtain confidential's configuration from config files
func DriveConfidentials(vp *viper.Viper) (TxConfidentialityHandler, error) {
	return nil, nil
}
